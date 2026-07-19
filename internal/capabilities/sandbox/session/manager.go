package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
)

const (
	defaultIdleTTL         = 10 * time.Minute
	defaultCacheTTL        = 24 * time.Hour
	defaultCleanupInterval = time.Minute
)

// Manager 管理可复用的远程执行 Session。Workspace 身份可持久化，容器 Session 仅在内存中缓存。
// 同一个逻辑执行会话串行执行，避免多个命令并发修改同一远程 Workspace。
type Manager struct {
	sessions     sandboxcontract.SessionClient
	files        sandboxcontract.FileSystemClient
	workspace    sandboxcontract.WorkspaceRef
	store        sandboxcontract.RemoteSessionBinder
	log          logger.Logger
	idleTTL      time.Duration
	cacheTTL     time.Duration
	cleanupEvery time.Duration

	mu               sync.Mutex
	entries          map[string]*managedEntry
	runKeys          map[string]map[string]struct{}
	closed           bool
	active           sync.WaitGroup
	backgroundCancel context.CancelFunc
	backgroundWG     sync.WaitGroup
	closeOnce        sync.Once
	closeErr         error
}

type managedEntry struct {
	execMu           sync.Mutex
	session          *Session
	workspace        sandboxcontract.WorkspaceRef
	lastUsed         time.Time
	inUse            int
	stateLoaded      bool
	statePersisted   bool
	runtimeSuspended bool // idle 后已 Suspend Runtime，Session 仍保留
}

// ManagerDeps 是远程 Session Manager 的产品无关依赖。
type ManagerDeps struct {
	Sessions        sandboxcontract.SessionClient
	Files           sandboxcontract.FileSystemClient
	Workspace       sandboxcontract.WorkspaceRef
	Store           sandboxcontract.RemoteSessionBinder
	Logger          logger.Logger
	IdleTTL         time.Duration
	CacheTTL        time.Duration
	CleanupInterval time.Duration
}

// AcquireRequest 描述一次逻辑远程执行会话的获取请求。
type AcquireRequest struct {
	Key       string
	RunID     string
	Binding   execmodel.ExecutionBinding
	Workspace execmodel.ExecutionWorkspace
	Sandbox   execmodel.SandboxProfile
}

// Handle 独占一个逻辑远程执行会话，调用方必须 Close。
type Handle struct {
	manager *Manager
	entry   *managedEntry
	session *Session
	once    sync.Once
}

// NewManager 创建远程执行 Session Manager。
func NewManager(deps ManagerDeps) (*Manager, error) {
	if deps.Sessions == nil || deps.Files == nil || deps.Store == nil {
		return nil, fmt.Errorf("sandbox session manager 缺少 sessions/files/store")
	}
	if deps.Logger == nil {
		deps.Logger = logger.NewNop()
	}
	if deps.IdleTTL <= 0 {
		deps.IdleTTL = defaultIdleTTL
	}
	if deps.CacheTTL <= 0 {
		deps.CacheTTL = defaultCacheTTL
	}
	if deps.CleanupInterval <= 0 {
		deps.CleanupInterval = defaultCleanupInterval
	}
	backgroundCtx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		sessions: deps.Sessions, files: deps.Files, workspace: deps.Workspace, store: deps.Store,
		log: deps.Logger, idleTTL: deps.IdleTTL, cacheTTL: deps.CacheTTL, cleanupEvery: deps.CleanupInterval,
		entries: make(map[string]*managedEntry), runKeys: make(map[string]map[string]struct{}), backgroundCancel: cancel,
	}
	m.backgroundWG.Add(1)
	go m.cleanupLoop(backgroundCtx)
	return m, nil
}

// ExecutionKey 构造跨 Run 可复用、跨租户/用户/项目隔离的逻辑执行会话键。
// 缺少可信 SessionID 时退化为 Run 级隔离，禁止意外合并不同请求。
func ExecutionKey(ctx context.Context, binding execmodel.ExecutionBinding, sandbox execmodel.SandboxProfile, workspace sandboxcontract.WorkspaceRef) string {
	owner := binding.Owner
	tenantID := firstNonEmpty(owner.TenantID, contextString(ctx, contextutil.GetTenantID))
	userID := firstNonEmpty(owner.UserID, contextString(ctx, contextutil.GetUserID))
	sessionID := firstNonEmpty(owner.SessionID, contextString(ctx, contextutil.GetSessionID))
	if sessionID == "" {
		sessionID = "run:" + strings.TrimSpace(owner.RunID)
	}
	parts := []string{
		tenantID, userID, sessionID, strings.TrimSpace(owner.ProjectID), strings.TrimSpace(owner.AgentAppID),
		strings.TrimSpace(owner.AgentAppVersion), strings.TrimSpace(owner.SubAgentInstanceID), strings.TrimSpace(owner.MemberID),
		string(binding.Mode), string(binding.Access), string(binding.PathPolicy), strings.TrimSpace(sandbox.Provider),
		string(sandbox.RuntimeProfile), string(sandbox.TaskType), string(sandbox.Operation), string(sandbox.RiskLevel),
		strings.TrimSpace(sandbox.Language), strings.TrimSpace(workspace.ID),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

// Acquire 获取并独占一个可用 Session；只在业务命令执行前安全地重建失效容器。
func (m *Manager) Acquire(ctx context.Context, req AcquireRequest) (*Handle, error) {
	if m == nil {
		return nil, fmt.Errorf("sandbox session manager未配置")
	}
	if err := req.Binding.Validate(); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeExecutionBindingRequired, err)
	}
	if err := req.Workspace.ValidateFor(req.Binding); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeExecutionBindingConflict, err)
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		key = ExecutionKey(ctx, req.Binding, req.Sandbox, m.workspace)
	}
	runID := firstNonEmpty(req.RunID, req.Binding.Owner.RunID)

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session manager已关闭"))
	}
	entry := m.entries[key]
	if entry == nil {
		entry = &managedEntry{lastUsed: time.Now()}
		m.entries[key] = entry
	}
	entry.inUse++
	entry.lastUsed = time.Now()
	m.active.Add(1)
	if runID != "" {
		if m.runKeys[runID] == nil {
			m.runKeys[runID] = make(map[string]struct{})
		}
		m.runKeys[runID][key] = struct{}{}
	}
	m.mu.Unlock()

	entry.execMu.Lock()
	if err := m.ensureSession(ctx, key, entry, req); err != nil {
		entry.execMu.Unlock()
		m.release(entry)
		return nil, err
	}
	entry.runtimeSuspended = false
	if err := m.bindExecution(ctx, entry.session, req.Binding); err != nil {
		entry.execMu.Unlock()
		m.release(entry)
		return nil, err
	}
	return &Handle{manager: m, entry: entry, session: entry.session}, nil
}

// Session 返回当前独占的远程 Session。
func (h *Handle) Session() *Session {
	if h == nil {
		return nil
	}
	return h.session
}

// RefreshBinding 使用最新权威租约刷新 execution binding，产物登记前应调用。
func (h *Handle) RefreshBinding(ctx context.Context, binding execmodel.ExecutionBinding) error {
	if h == nil || h.manager == nil || h.session == nil {
		return fmt.Errorf("sandbox session handle无效")
	}
	return h.manager.bindExecution(ctx, h.session, binding)
}

// Close 释放逻辑会话独占权；不会立即删除持久 Workspace。
func (h *Handle) Close() error {
	if h == nil {
		return nil
	}
	h.once.Do(func() {
		if h.entry != nil {
			h.entry.execMu.Unlock()
		}
		if h.manager != nil {
			h.manager.release(h.entry)
		}
	})
	return nil
}

// ReleaseRun 释放 Run 对逻辑会话的引用；idle TTL 后 Suspend Runtime，更长 cacheTTL 后再 Close Session。
// durable Workspace 映射始终保留在 store 中。
func (m *Manager) ReleaseRun(_ context.Context, prepared workmodel.PreparedRun) {
	m.ReleaseRunID(prepared.Manifest.RunID)
}

// ReleaseRunID 供持有 Run ID 的编排器执行同样的幂等释放。
func (m *Manager) ReleaseRunID(runID string) {
	if m == nil || strings.TrimSpace(runID) == "" {
		return
	}
	m.mu.Lock()
	delete(m.runKeys, strings.TrimSpace(runID))
	m.mu.Unlock()
}

// Close 停止新执行，等待活跃命令完成并关闭所有短命 Session。
func (m *Manager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()
		m.backgroundCancel()
		m.backgroundWG.Wait()
		m.active.Wait()

		m.mu.Lock()
		entries := make([]*managedEntry, 0, len(m.entries))
		for _, entry := range m.entries {
			entries = append(entries, entry)
		}
		m.entries = make(map[string]*managedEntry)
		m.runKeys = make(map[string]map[string]struct{})
		m.mu.Unlock()
		for _, entry := range entries {
			entry.execMu.Lock()
			if entry.session != nil {
				if err := entry.session.Close(ctx); err != nil && m.closeErr == nil {
					m.closeErr = err
				}
				entry.session = nil
			}
			entry.execMu.Unlock()
		}
	})
	return m.closeErr
}

func (m *Manager) ensureSession(ctx context.Context, key string, entry *managedEntry, req AcquireRequest) error {
	if entry.session == nil {
		if err := m.openSession(ctx, key, entry, req); err != nil {
			return err
		}
	}
	if err := ensureWorkspace(ctx, entry.session, req.Workspace); err == nil {
		return nil
	} else if !sessionUnavailable(err) {
		return err
	}
	broken := entry.session
	entry.session = nil
	if closeErr := broken.Close(context.Background()); closeErr != nil {
		m.log.Warn("关闭失效远程 session 失败", "execution_session", key, "error", closeErr)
	}
	if err := m.openSession(ctx, key, entry, req); err != nil {
		return fmt.Errorf("sandbox_unavailable: 重建远程执行 session: %w", err)
	}
	if err := ensureWorkspace(ctx, entry.session, req.Workspace); err != nil {
		return fmt.Errorf("sandbox_unavailable: 初始化重建后的远程 workspace: %w", err)
	}
	return nil
}

func (m *Manager) openSession(ctx context.Context, key string, entry *managedEntry, req AcquireRequest) error {
	if !entry.stateLoaded {
		persisted, ok, err := m.store.LoadExecutionSession(ctx, key)
		if err != nil {
			return fmt.Errorf("加载 execution session workspace: %w", err)
		}
		if ok {
			entry.workspace = persisted
			entry.statePersisted = true
		} else {
			entry.workspace = m.workspace
		}
		entry.stateLoaded = true
	}
	sess, err := Open(ctx, Deps{Sessions: m.sessions, Files: m.files}, Options{
		Workspace: entry.workspace,
		Sandbox:   req.Sandbox,
		Run:       execcontract.RunOptions{Binding: req.Binding, Workspace: req.Workspace},
	})
	if err != nil {
		if entry.statePersisted && durableWorkspaceUnavailable(err) {
			if deleteErr := m.store.DeleteExecutionSession(context.Background(), key); deleteErr != nil {
				m.log.Warn("删除失效 execution workspace 映射失败", "execution_session", key, "error", deleteErr)
			}
			entry.workspace = m.workspace
			entry.statePersisted = false
			sess, err = Open(ctx, Deps{Sessions: m.sessions, Files: m.files}, Options{
				Workspace: entry.workspace,
				Sandbox:   req.Sandbox,
				Run:       execcontract.RunOptions{Binding: req.Binding, Workspace: req.Workspace},
			})
			if err != nil {
				return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("持久 workspace 失效后重建 execution session: %w", err))
			}
		} else {
			return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("打开 genesis-sandbox session: %w", err))
		}
	}
	durable, err := durableWorkspace(sess.Workspace(), entry.workspace)
	if err != nil {
		_ = sess.Close(context.Background())
		return err
	}
	if err := m.store.SaveExecutionSession(ctx, key, durable); err != nil {
		_ = sess.Close(context.Background())
		return fmt.Errorf("保存 execution session workspace: %w", err)
	}
	entry.workspace = durable
	entry.session = sess
	entry.statePersisted = true
	return nil
}

func (m *Manager) bindExecution(ctx context.Context, sess *Session, binding execmodel.ExecutionBinding) error {
	if sess == nil {
		return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("远程 session为空"))
	}
	expiresAt := sess.ExpiresAt()
	if expiresAt.IsZero() || !expiresAt.After(time.Now()) {
		return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("远程 session 未返回有效租约"))
	}
	return m.store.BindRemoteSession(ctx, binding.Owner.TenantID, binding.Owner.RunID, binding.ID, sess.Workspace(), expiresAt)
}

func (m *Manager) release(entry *managedEntry) {
	if entry == nil {
		return
	}
	m.mu.Lock()
	if entry.inUse > 0 {
		entry.inUse--
	}
	entry.lastUsed = time.Now()
	m.mu.Unlock()
	m.active.Done()
}

func (m *Manager) cleanupLoop(ctx context.Context) {
	defer m.backgroundWG.Done()
	ticker := time.NewTicker(m.cleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.cleanup(ctx)
		}
	}
}

func (m *Manager) cleanup(ctx context.Context) {
	now := time.Now()
	type candidate struct {
		key   string
		entry *managedEntry
		close bool // true=Close+驱逐；false=Suspend Runtime
	}
	var candidates []candidate
	m.mu.Lock()
	for key, entry := range m.entries {
		if entry == nil || entry.inUse > 0 || m.referencedLocked(key) {
			continue
		}
		idleFor := now.Sub(entry.lastUsed)
		switch {
		case entry.session != nil && idleFor > m.cacheTTL:
			candidates = append(candidates, candidate{key: key, entry: entry, close: true})
		case entry.session != nil && idleFor > m.idleTTL && !entry.runtimeSuspended:
			candidates = append(candidates, candidate{key: key, entry: entry, close: false})
		case entry.session == nil && idleFor > m.cacheTTL:
			delete(m.entries, key)
		}
	}
	m.mu.Unlock()
	for _, candidate := range candidates {
		candidate.entry.execMu.Lock()
		m.mu.Lock()
		if candidate.entry.inUse > 0 || m.referencedLocked(candidate.key) {
			m.mu.Unlock()
			candidate.entry.execMu.Unlock()
			continue
		}
		idleFor := time.Since(candidate.entry.lastUsed)
		if candidate.close {
			if candidate.entry.session == nil || idleFor <= m.cacheTTL {
				m.mu.Unlock()
				candidate.entry.execMu.Unlock()
				continue
			}
			sess := candidate.entry.session
			candidate.entry.session = nil
			candidate.entry.runtimeSuspended = false
			delete(m.entries, candidate.key)
			m.mu.Unlock()
			if err := sess.Close(ctx); err != nil {
				m.log.Warn("关闭超长空闲远程 session 失败", "execution_session", candidate.key, "error", err)
			}
			candidate.entry.execMu.Unlock()
			continue
		}
		if candidate.entry.session == nil || candidate.entry.runtimeSuspended || idleFor <= m.idleTTL {
			m.mu.Unlock()
			candidate.entry.execMu.Unlock()
			continue
		}
		sess := candidate.entry.session
		m.mu.Unlock()
		if err := sess.Suspend(ctx); err != nil {
			m.log.Warn("Suspend 空闲远程 Runtime 失败", "execution_session", candidate.key, "error", err)
		} else {
			m.mu.Lock()
			candidate.entry.runtimeSuspended = true
			m.mu.Unlock()
		}
		candidate.entry.execMu.Unlock()
	}
}

func (m *Manager) referencedLocked(key string) bool {
	for _, keys := range m.runKeys {
		if _, ok := keys[key]; ok {
			return true
		}
	}
	return false
}

func ensureWorkspace(ctx context.Context, sess *Session, workspace execmodel.ExecutionWorkspace) error {
	for _, dir := range []string{workspace.WorkDir, workspace.InputDir, workspace.OutputDir, workspace.TmpDir, workspace.SkillDir} {
		rel := RelativePath(dir, "")
		if rel == "" || rel == "." {
			continue
		}
		if err := sess.MkdirAll(ctx, rel, fscontract.MkdirOptions{Parents: true}); err != nil {
			return fmt.Errorf("创建远程执行目录 %s: %w", dir, err)
		}
	}
	return nil
}

// RelativePath 把 /workspace 下的执行路径转换成 Session File API 相对路径。
func RelativePath(root, child string) string {
	root = strings.ReplaceAll(strings.TrimSpace(root), `\`, "/")
	root = strings.TrimPrefix(root, "/workspace/")
	root = strings.TrimPrefix(root, "/")
	child = strings.ReplaceAll(strings.TrimSpace(child), `\`, "/")
	child = strings.TrimPrefix(child, "./")
	child = strings.TrimPrefix(child, "/")
	if child == "" {
		return path.Clean(root)
	}
	return path.Join(root, child)
}

func durableWorkspace(live, fallback sandboxcontract.WorkspaceRef) (sandboxcontract.WorkspaceRef, error) {
	workspaceID := firstNonEmpty(live.Metadata["workspace_id"], fallback.ID)
	if workspaceID == "" {
		return sandboxcontract.WorkspaceRef{}, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("session 未返回 durable workspace_id"))
	}
	provider := firstNonEmpty(live.Provider, fallback.Provider, "genesis-sandbox")
	return sandboxcontract.WorkspaceRef{ID: workspaceID, Provider: provider, Metadata: map[string]string{"workspace_id": workspaceID}}, nil
}

func sessionUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"sandbox_unavailable", "not_found", "not found", "no such file", "no such container", "session expired", "workspace unavailable", "connection refused", "connection reset"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func durableWorkspaceUnavailable(err error) bool {
	message := strings.ToLower(fmt.Sprint(err))
	return strings.Contains(message, "workspace") && (strings.Contains(message, "not_found") || strings.Contains(message, "not found") || strings.Contains(message, "unavailable"))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func contextString(ctx context.Context, getter func(context.Context) (string, bool)) string {
	value, _ := getter(ctx)
	return value
}
