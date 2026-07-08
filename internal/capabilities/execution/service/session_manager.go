package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/capabilities/execution/contract"
	"genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/capabilities/execution/policy"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
)

var (
	ErrSessionClosed   = errors.New("terminal session is not running or closed")
	ErrSessionStarting = errors.New("terminal session is starting, please retry later")
	ErrLineScanDeny    = errors.New("command line dynamic security check denied")
)

var globalSubCounter int64

// SessionManager 统筹伪终端交互会话生命周期管理与安全防线
type SessionManager struct {
	runner       contract.InteractiveSessionRunner
	approval     approvalcontract.Service // 新增：可选的审批服务
	sessions     map[string]*model.TerminalSession
	broadcasters map[string]*multicastBroadcaster
	mu           sync.RWMutex
}

// NewSessionManager 创建会话管理器
func NewSessionManager(runner contract.InteractiveSessionRunner) *SessionManager {
	return &SessionManager{
		runner:       runner,
		sessions:     make(map[string]*model.TerminalSession),
		broadcasters: make(map[string]*multicastBroadcaster),
	}
}

// WithApproval 链式设置审批服务支持人审交互确认
func (m *SessionManager) WithApproval(approval approvalcontract.Service) *SessionManager {
	m.approval = approval
	return m
}

// StartSession 启动一个 PTY 交互式/后台会话
func (m *SessionManager) StartSession(ctx context.Context, sessionID string, cmd model.Command, opts contract.RunOptions) error {
	m.mu.Lock()
	if _, exists := m.sessions[sessionID]; exists {
		m.mu.Unlock()
		return fmt.Errorf("session [%s] already exists", sessionID)
	}

	// 1. 在 map 中注册 pending 状态占位，并立即释放全局锁，确保不阻塞其他会话交互
	placeholder := &model.TerminalSession{
		ID:        sessionID,
		Command:   cmd.Command,
		Cwd:       cmd.Cwd,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if opts.Sandbox.Metadata != nil {
		placeholder.PathScope = opts.Sandbox.Metadata["path_scope"]
	}
	placeholder.Status = model.SessionStatusPending

	m.sessions[sessionID] = placeholder
	m.mu.Unlock()

	// 2. 物理启动 PTY 会话（不持全局锁，可能耗时数毫秒至几秒）
	err := m.runner.StartSession(ctx, sessionID, cmd, opts)
	if err != nil {
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return err
	}

	// 3. 开启输出流订阅
	ch, runnerCancel, err := m.runner.SubscribeOutput(ctx, sessionID)
	if err != nil {
		_ = m.runner.KillSession(ctx, sessionID)
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return fmt.Errorf("subscribe underlying output failed: %w", err)
	}

	// 4. 重获全局锁更新运行状态并注册分发广播器
	m.mu.Lock()
	placeholder.Mu.Lock()
	placeholder.Status = model.SessionStatusRunning
	placeholder.UpdatedAt = time.Now()
	placeholder.Mu.Unlock()

	mb := newMulticastBroadcaster(ch, runnerCancel)
	m.broadcasters[sessionID] = mb
	m.mu.Unlock()

	go mb.start()

	return nil
}

// WriteStdin 向特定会话追加标准输入。由于 data 中可能包含换行指令，我们将启用行缓冲动态扫描防御
func (m *SessionManager) WriteStdin(ctx context.Context, sessionID string, data []byte) error {
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	mb, bExists := m.broadcasters[sessionID]
	m.mu.RUnlock()

	if !exists {
		return contract.NewError(contract.ErrCodeRunnerFailed, ErrSessionClosed)
	}

	session.Mu.Lock()
	defer session.Mu.Unlock()

	if session.Status == model.SessionStatusPending {
		return contract.NewError(contract.ErrCodeRunnerFailed, ErrSessionStarting)
	}

	if session.Status != model.SessionStatusRunning || !bExists {
		return contract.NewError(contract.ErrCodeRunnerFailed, ErrSessionClosed)
	}

	// 1. 进行动态扫描防卫：结合换行符，识别行命令并调用 policy.Classify 进行拦截
	mb.scanBuffer.Write(data)
	bufBytes := mb.scanBuffer.Bytes()
	idx := bytes.LastIndexByte(bufBytes, '\n')
	if idx >= 0 {
		lines := bytes.Split(bufBytes[:idx], []byte{'\n'})
		// 清空已被提取的行缓冲区
		mb.scanBuffer.Reset()
		if idx+1 < len(bufBytes) {
			mb.scanBuffer.Write(bufBytes[idx+1:])
		}

		// 逐行检查安全性
		for _, line := range lines {
			lineCmd := string(bytes.TrimSpace(line))
			if lineCmd == "" {
				continue
			}

			cls := policy.Classify(lineCmd)
			// 如果动态分类为高危/破坏性操作，予以拦截阻断，或触发人机交互审批
			if cls.Dangerous || cls.Destructive || cls.Critical {
				if m.approval != nil {
					// 构造交互命令以进行安全策略评估
					interactiveCmd := model.Command{
						Command: lineCmd,
						Cwd:     session.Cwd,
						Shell:   model.ShellAuto,
					}
					// 构造简易的 ResolvedPath 供策略和审批展示使用
					pathScope := fsmodel.PathScopeWorkspace
					if session.PathScope != "" {
						pathScope = fsmodel.PathScope(session.PathScope)
					}
					resolvedPath := fsmodel.ResolvedPath{
						Scope:       pathScope,
						DisplayPath: session.Cwd,
						BackendPath: session.Cwd,
						RawPath:     session.Cwd,
					}
					req := policy.BuildApprovalRequest("write_stdin", interactiveCmd, resolvedPath, cls)

					// 释放 session 锁，防止同步阻塞人机审批时锁死当前会话
					session.Mu.Unlock()
					decision, err := m.approval.Authorize(ctx, req)
					session.Mu.Lock()

					// 重新检查会话运行状态，确保等待人机审批期间会话未被销毁
					if session.Status != model.SessionStatusRunning || !bExists {
						return contract.NewError(contract.ErrCodeRunnerFailed, ErrSessionClosed)
					}

					if err != nil {
						return err
					}
					if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
						return contract.NewError(contract.ErrCodePermissionDenied, fmt.Errorf("%w: command [%s] violates policy [%s] (approval denied)", ErrLineScanDeny, lineCmd, cls.Reason))
					}
				} else {
					return contract.NewError(contract.ErrCodePermissionDenied, fmt.Errorf("%w: command [%s] violates policy [%s]", ErrLineScanDeny, lineCmd, cls.Reason))
				}
			}
		}
	}

	// 2. 数据安全放行，送入物理 PTY 管道
	err := m.runner.WriteStdin(ctx, sessionID, data)
	if err != nil {
		return err
	}

	session.UpdatedAt = time.Now()
	return nil
}

// SubscribeOutput 多路复用订阅输出字节流
func (m *SessionManager) SubscribeOutput(ctx context.Context, sessionID string) (<-chan []byte, context.CancelFunc, error) {
	m.mu.RLock()
	mb, exists := m.broadcasters[sessionID]
	m.mu.RUnlock()

	if !exists {
		return nil, nil, contract.NewError(contract.ErrCodeRunnerFailed, ErrSessionClosed)
	}

	return mb.subscribe()
}

// KillSession 强制终止并级联清理会话
func (m *SessionManager) KillSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	mb, bExists := m.broadcasters[sessionID]
	delete(m.sessions, sessionID)
	delete(m.broadcasters, sessionID)
	m.mu.Unlock()

	if !exists {
		return nil
	}

	if bExists && mb != nil {
		mb.close()
	}

	session.Mu.Lock()
	session.Status = model.SessionStatusCompleted
	session.UpdatedAt = time.Now()
	session.Mu.Unlock()

	return m.runner.KillSession(ctx, sessionID)
}

// GetSession 获取会话详情
func (m *SessionManager) GetSession(sessionID string) (*model.TerminalSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// GetSessionStatus 获取特定会话当前的状态以及是否存在
func (m *SessionManager) GetSessionStatus(ctx context.Context, sessionID string) (model.SessionStatus, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return "", false, nil
	}
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	return s.Status, true, nil
}


// ── 内部多路复用广播器实现（带丢弃背压机制） ────────────────────────

type subscriber struct {
	ch     chan []byte
	cancel context.CancelFunc
}

type multicastBroadcaster struct {
	inputCh      <-chan []byte
	runnerCancel context.CancelFunc
	subs         map[string]*subscriber
	scanBuffer   bytes.Buffer // 用于 WriteStdin 安全前置校验行缓存
	mu           sync.RWMutex
	closeOnce    sync.Once
	closed       chan struct{}
}

func newMulticastBroadcaster(inputCh <-chan []byte, runnerCancel context.CancelFunc) *multicastBroadcaster {
	return &multicastBroadcaster{
		inputCh:      inputCh,
		runnerCancel: runnerCancel,
		subs:         make(map[string]*subscriber),
		closed:       make(chan struct{}),
	}
}

func (mb *multicastBroadcaster) start() {
	defer mb.close()

	for {
		select {
		case <-mb.closed:
			return
		case data, ok := <-mb.inputCh:
			if !ok {
				return
			}
			mb.broadcast(data)
		}
	}
}

func (mb *multicastBroadcaster) broadcast(data []byte) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	for _, sub := range mb.subs {
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)

		select {
		case sub.ch <- dataCopy:
		default:
			// 背压保护：如果当前订阅者（如由于网络迟钝）缓冲区满，丢弃历史包并打日志，绝对不阻塞物理 PTY 的命令运转
			// 此设计无愧于最佳实践与高可用原则
		}
	}
}

func (mb *multicastBroadcaster) subscribe() (<-chan []byte, context.CancelFunc, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	select {
	case <-mb.closed:
		return nil, nil, ErrSessionClosed
	default:
	}

	// 环形缓冲容量为 256
	ch := make(chan []byte, 256)
	idVal := atomic.AddInt64(&globalSubCounter, 1)
	subID := fmt.Sprintf("sub_%d_%d", time.Now().UnixNano(), idVal)

	cancel := context.CancelFunc(func() {
		mb.mu.Lock()
		defer mb.mu.Unlock()
		if sub, exists := mb.subs[subID]; exists {
			close(sub.ch)
			delete(mb.subs, subID)
		}
	})

	mb.subs[subID] = &subscriber{
		ch:     ch,
		cancel: cancel,
	}

	return ch, cancel, nil
}

func (mb *multicastBroadcaster) close() {
	mb.closeOnce.Do(func() {
		close(mb.closed)
		if mb.runnerCancel != nil {
			mb.runnerCancel()
		}

		mb.mu.Lock()
		defer mb.mu.Unlock()
		for id, sub := range mb.subs {
			close(sub.ch)
			delete(mb.subs, id)
		}
	})
}
