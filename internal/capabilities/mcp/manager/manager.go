package manager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

const (
	defaultConnectBatchSize = 3
	defaultHealthInterval   = 5 * time.Second
	defaultFailBackoff      = 30 * time.Second
	defaultPingTimeout      = 5 * time.Second
	ensurePollInterval      = 100 * time.Millisecond
)

// Options 控制 ConnectionManager 行为。
type Options struct {
	Factory          contract.TransportFactory
	ConnectBatchSize int
	HealthInterval   time.Duration
	FailBackoff      time.Duration
	ApprovalStore    contract.ApprovalStore
}

// Manager 是单一 MCP 连接编排器（对齐 Codex McpConnectionManager，避免 Kode 双路径）。
type Manager struct {
	factory       contract.TransportFactory
	batchSize     int
	healthEvery   time.Duration
	failBackoff   time.Duration
	approvalStore contract.ApprovalStore

	mu         sync.RWMutex
	sessions   map[string]*managedSession
	wanted     map[string]model.McpServerDefinition
	listeners  []contract.StateListener
	bgCancel   context.CancelFunc
	bgWG       sync.WaitGroup
	connecting map[string]bool // 正在 Dial 的 server，避免并发重复连接

	healthCancel context.CancelFunc
	healthWG     sync.WaitGroup
	closed       bool
}

type managedSession struct {
	def            model.McpServerDefinition
	session        *session
	state          model.ServerState
	lastAttempt    time.Time
	lastHealth     time.Time
	toolsVersion   int
	refreshPending bool
}

// New 创建 Manager。
func New(opts Options) (*Manager, error) {
	if opts.Factory == nil {
		return nil, fmt.Errorf("mcp manager 需要 TransportFactory")
	}
	batch := opts.ConnectBatchSize
	if batch <= 0 {
		batch = defaultConnectBatchSize
	}
	health := opts.HealthInterval
	if health <= 0 {
		health = defaultHealthInterval
	}
	backoff := opts.FailBackoff
	if backoff <= 0 {
		backoff = defaultFailBackoff
	}
	m := &Manager{
		factory:       opts.Factory,
		batchSize:     batch,
		healthEvery:   health,
		failBackoff:   backoff,
		approvalStore: opts.ApprovalStore,
		sessions:      make(map[string]*managedSession),
		wanted:        make(map[string]model.McpServerDefinition),
		connecting:    make(map[string]bool),
	}
	hctx, cancel := context.WithCancel(context.Background())
	m.healthCancel = cancel
	m.healthWG.Add(1)
	go m.healthLoop(hctx)
	return m, nil
}

// Subscribe 注册生命周期监听器。
func (m *Manager) Subscribe(listener contract.StateListener) {
	if listener == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, listener)
}

// Sync 按 catalog 定义阻塞式批量连接/断开/重连。
func (m *Manager) Sync(ctx context.Context, defs []model.McpServerDefinition) ([]model.ServerState, error) {
	pending, err := m.applyCatalog(ctx, defs)
	if err != nil {
		return nil, err
	}
	m.connectPending(ctx, pending)
	return m.States(), nil
}

// SyncAsync 应用 catalog 后立即返回，连接在后台进行（不阻塞产品启动）。
func (m *Manager) SyncAsync(ctx context.Context, defs []model.McpServerDefinition) ([]model.ServerState, error) {
	pending, err := m.applyCatalog(ctx, defs)
	if err != nil {
		return nil, err
	}
	m.startBackgroundConnect(pending)
	return m.States(), nil
}

func (m *Manager) applyCatalog(ctx context.Context, defs []model.McpServerDefinition) ([]model.McpServerDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("mcp manager 已关闭")
	}
	m.mu.Unlock()

	wanted := make(map[string]model.McpServerDefinition, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Config.Name)
		if name == "" {
			continue
		}
		def.Config.Name = name
		def = m.applyProjectApproval(ctx, def)
		wanted[name] = def
	}

	// 关闭不再需要或配置变更的连接。
	m.mu.Lock()
	m.wanted = wanted
	toClose := make([]*managedSession, 0)
	for name, ms := range m.sessions {
		def, ok := wanted[name]
		if !ok || !def.Config.Enabled || def.DisabledReason != "" || ms.state.ConfigKey != def.ConfigKey {
			toClose = append(toClose, ms)
			delete(m.sessions, name)
		}
	}
	m.mu.Unlock()
	for _, ms := range toClose {
		m.closeManaged(ctx, ms, model.EventServerClosed)
	}

	pending := make([]model.McpServerDefinition, 0)
	m.mu.Lock()
	for name, def := range wanted {
		if !def.Config.Enabled || def.DisabledReason != "" {
			if _, ok := m.sessions[name]; ok {
				continue
			}
			st := model.ServerState{
				Name:      name,
				Status:    model.ServerStatusDisabled,
				Origin:    def.Origin,
				Required:  def.Config.Required,
				Error:     def.DisabledReason,
				ConfigKey: def.ConfigKey,
			}
			if st.Error == "" && !def.Config.Enabled {
				st.Error = "disabled"
			}
			m.sessions[name] = &managedSession{def: def, state: st}
			continue
		}
		if ms, ok := m.sessions[name]; ok && ms.state.Status == model.ServerStatusReady && ms.session != nil {
			continue
		}
		// 先登记 starting，让管理面立刻可见，避免启动等待 Dial。
		if ms, ok := m.sessions[name]; !ok || ms.state.Status != model.ServerStatusStarting {
			st := model.ServerState{
				Name:      name,
				Status:    model.ServerStatusStarting,
				Origin:    def.Origin,
				Required:  def.Config.Required,
				ConfigKey: def.ConfigKey,
			}
			m.sessions[name] = &managedSession{def: def, state: st}
		}
		pending = append(pending, def)
	}
	m.mu.Unlock()
	return pending, nil
}

func (m *Manager) startBackgroundConnect(pending []model.McpServerDefinition) {
	if len(pending) == 0 {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if m.bgCancel != nil {
		cancel := m.bgCancel
		m.mu.Unlock()
		cancel()
		m.bgWG.Wait()
		m.mu.Lock()
	}
	if m.closed {
		m.mu.Unlock()
		return
	}
	bgCtx, cancel := context.WithCancel(context.Background())
	m.bgCancel = cancel
	m.bgWG.Add(1)
	m.mu.Unlock()

	go func(defs []model.McpServerDefinition) {
		defer m.bgWG.Done()
		m.connectPending(bgCtx, defs)
	}(append([]model.McpServerDefinition(nil), pending...))
}

func (m *Manager) connectPending(ctx context.Context, pending []model.McpServerDefinition) {
	for i := 0; i < len(pending); i += m.batchSize {
		if err := ctx.Err(); err != nil {
			return
		}
		end := i + m.batchSize
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[i:end]
		var wg sync.WaitGroup
		for _, def := range batch {
			wg.Add(1)
			go func(d model.McpServerDefinition) {
				defer wg.Done()
				m.connectOne(ctx, d)
			}(def)
		}
		wg.Wait()
	}
}

func (m *Manager) connectOne(ctx context.Context, def model.McpServerDefinition) {
	name := def.Config.Name

	// 每次连接前复核审批，防止 Sync 与后台 Dial 并发时撤销被绕过。
	def = m.applyProjectApproval(ctx, def)
	if !def.Config.Enabled || def.DisabledReason != "" {
		m.setDisabled(def, disabledReason(def))
		return
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if existing, ok := m.sessions[name]; ok {
		if existing.state.Status == model.ServerStatusReady && existing.session != nil {
			m.mu.Unlock()
			return
		}
		// Kode：failed 且距上次尝试 < 30s → 不重试。
		if existing.state.Status == model.ServerStatusFailed && time.Since(existing.lastAttempt) < m.failBackoff {
			m.mu.Unlock()
			return
		}
	}
	if m.connecting[name] {
		m.mu.Unlock()
		return
	}
	m.connecting[name] = true
	ms := &managedSession{
		def:         def,
		lastAttempt: time.Now(),
		state: model.ServerState{
			Name:      name,
			Status:    model.ServerStatusStarting,
			Origin:    def.Origin,
			Required:  def.Config.Required,
			ConfigKey: def.ConfigKey,
		},
	}
	m.sessions[name] = ms
	listeners := append([]contract.StateListener(nil), m.listeners...)
	m.mu.Unlock()
	m.emit(ctx, listeners, model.LifecycleEvent{Kind: model.EventServerStarting, Server: name, State: ms.state, At: time.Now()})
	defer func() {
		m.mu.Lock()
		delete(m.connecting, name)
		m.mu.Unlock()
	}()

	startupCtx := ctx
	var cancel context.CancelFunc
	if def.Config.StartupTimeout > 0 {
		startupCtx, cancel = context.WithTimeout(ctx, def.Config.StartupTimeout)
		defer cancel()
	}

	tr, err := m.factory.Build(startupCtx, def.Config)
	if err != nil {
		m.setFailed(def, err, isAuthError(err))
		return
	}
	dialed, err := tr.Dial(startupCtx, contract.ConnectOptions{
		OnToolsChanged: func() { m.markToolsChanged(name) },
	})
	if err != nil {
		m.setFailed(def, err, isAuthError(err))
		return
	}
	sess, err := newSession(name, dialed, def.Config)
	if err != nil {
		m.setFailed(def, err, false)
		return
	}
	tools, err := sess.ListTools(startupCtx)
	if err != nil {
		_ = sess.Close(context.Background())
		m.setFailed(def, err, false)
		return
	}
	tools = filterTools(tools, def.Config)

	m.mu.Lock()
	ms = m.sessions[name]
	if ms == nil || m.closed {
		m.mu.Unlock()
		_ = sess.Close(context.Background())
		return
	}
	// 配置已变更则丢弃本次连接。
	if want, ok := m.wanted[name]; !ok || want.ConfigKey != def.ConfigKey || !want.Config.Enabled || want.DisabledReason != "" {
		m.mu.Unlock()
		_ = sess.Close(context.Background())
		return
	}
	ms.session = sess
	ms.state = model.ServerState{
		Name:          name,
		Status:        model.ServerStatusReady,
		Origin:        def.Origin,
		Required:      def.Config.Required,
		ToolCount:     len(tools),
		Tools:         tools,
		ConfigKey:     def.ConfigKey,
		LastConnected: time.Now(),
	}
	ms.lastHealth = time.Now()
	listeners = append([]contract.StateListener(nil), m.listeners...)
	state := ms.state
	m.mu.Unlock()
	m.emit(ctx, listeners, model.LifecycleEvent{Kind: model.EventServerReady, Server: name, State: state, At: time.Now()})
}

// applyProjectApproval 将审批决定转为定义禁用状态，使 refresh 能撤销已有连接。
func (m *Manager) applyProjectApproval(ctx context.Context, def model.McpServerDefinition) model.McpServerDefinition {
	if def.Origin != model.OriginProject || m.approvalStore == nil || def.DisabledReason != "" || !def.Config.Enabled {
		return def
	}
	decision, ok, err := m.approvalStore.Get(ctx, def.Config.Name)
	if err != nil {
		def.Config.Enabled = false
		def.DisabledReason = fmt.Sprintf("读取 project MCP 预连接审批失败: %v", err)
		return def
	}
	if ok && decision == contract.ApprovalApproved {
		return def
	}
	def.Config.Enabled = false
	if decision == contract.ApprovalRejected {
		def.DisabledReason = "project 来源 MCP server 已被拒绝"
	} else {
		def.DisabledReason = "project 来源 MCP server 未批准连接"
	}
	return def
}

func disabledReason(def model.McpServerDefinition) string {
	if def.DisabledReason != "" {
		return def.DisabledReason
	}
	return "disabled"
}

// EnsureConnected 等待或触发单个 server 连接完成。
func (m *Manager) EnsureConnected(ctx context.Context, server string) error {
	server = strings.TrimSpace(server)
	if server == "" {
		return fmt.Errorf("mcp server name 不能为空")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.mu.RLock()
		ms := m.sessions[server]
		def, hasDef := m.wanted[server]
		connecting := m.connecting[server]
		closed := m.closed
		m.mu.RUnlock()
		if closed {
			return fmt.Errorf("mcp manager 已关闭")
		}
		if ms != nil && ms.state.Status == model.ServerStatusReady && ms.session != nil {
			return nil
		}
		if ms != nil {
			switch ms.state.Status {
			case model.ServerStatusFailed, model.ServerStatusNeedsAuth, model.ServerStatusDisabled, model.ServerStatusCancelled:
				msg := ms.state.Error
				if msg == "" {
					msg = string(ms.state.Status)
				}
				return fmt.Errorf("mcp server %q 不可用: %s", server, msg)
			}
		}
		if !hasDef {
			return fmt.Errorf("mcp server %q 不在当前 catalog", server)
		}
		if !def.Config.Enabled || def.DisabledReason != "" {
			reason := def.DisabledReason
			if reason == "" {
				reason = "disabled"
			}
			return fmt.Errorf("mcp server %q 不可用: %s", server, reason)
		}
		if !connecting {
			go m.connectOne(context.Background(), def)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ensurePollInterval):
		}
	}
}

// WaitRequired 等待 Required server 离开 starting 态。
func (m *Manager) WaitRequired(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.mu.RLock()
		pending := 0
		var fatal error
		for name, def := range m.wanted {
			if !def.Config.Required || !def.Config.Enabled || def.DisabledReason != "" {
				continue
			}
			ms := m.sessions[name]
			if ms == nil || ms.state.Status == model.ServerStatusStarting {
				pending++
				continue
			}
			if ms.state.Fatal || ms.state.Status == model.ServerStatusFailed || ms.state.Status == model.ServerStatusNeedsAuth {
				fatal = fmt.Errorf("required mcp server %q 连接失败: %s", name, ms.state.Error)
				break
			}
		}
		m.mu.RUnlock()
		if fatal != nil {
			return fatal
		}
		if pending == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ensurePollInterval):
		}
	}
}

func (m *Manager) setFailed(def model.McpServerDefinition, err error, needsAuth bool) {
	status := model.ServerStatusFailed
	if needsAuth {
		status = model.ServerStatusNeedsAuth
	}
	st := model.ServerState{
		Name:      def.Config.Name,
		Status:    status,
		Origin:    def.Origin,
		Required:  def.Config.Required,
		Error:     err.Error(),
		ConfigKey: def.ConfigKey,
		Fatal:     def.Config.Required,
	}
	m.mu.Lock()
	m.sessions[def.Config.Name] = &managedSession{
		def:         def,
		state:       st,
		lastAttempt: time.Now(),
	}
	listeners := append([]contract.StateListener(nil), m.listeners...)
	m.mu.Unlock()
	m.emit(context.Background(), listeners, model.LifecycleEvent{Kind: model.EventServerFailed, Server: def.Config.Name, State: st, At: time.Now()})
}

func (m *Manager) setDisabled(def model.McpServerDefinition, reason string) {
	st := model.ServerState{
		Name:      def.Config.Name,
		Status:    model.ServerStatusDisabled,
		Origin:    def.Origin,
		Required:  def.Config.Required,
		Error:     reason,
		ConfigKey: def.ConfigKey,
	}
	m.mu.Lock()
	m.sessions[def.Config.Name] = &managedSession{def: def, state: st}
	m.mu.Unlock()
}

// SessionFor 返回存活会话。
func (m *Manager) SessionFor(server string) (contract.Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ms := m.sessions[strings.TrimSpace(server)]
	if ms == nil || ms.session == nil || ms.state.Status != model.ServerStatusReady {
		return nil, false
	}
	return ms.session, true
}

// States 返回全部 server 状态快照。
func (m *Manager) States() []model.ServerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.ServerState, 0, len(m.sessions))
	for _, ms := range m.sessions {
		out = append(out, ms.state)
	}
	return out
}

// Close 关闭所有会话与健康检查。
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	if m.bgCancel != nil {
		m.bgCancel()
	}
	if m.healthCancel != nil {
		m.healthCancel()
	}
	all := make([]*managedSession, 0, len(m.sessions))
	for _, ms := range m.sessions {
		all = append(all, ms)
	}
	m.sessions = make(map[string]*managedSession)
	m.wanted = make(map[string]model.McpServerDefinition)
	m.mu.Unlock()

	m.bgWG.Wait()
	for _, ms := range all {
		m.closeManaged(ctx, ms, model.EventServerClosed)
	}
	m.healthWG.Wait()
	return nil
}

func (m *Manager) closeManaged(ctx context.Context, ms *managedSession, kind model.EventKind) {
	if ms == nil {
		return
	}
	if ms.session != nil {
		_ = ms.session.Close(ctx)
	}
	st := ms.state
	st.Status = model.ServerStatusDisabled
	m.mu.RLock()
	listeners := append([]contract.StateListener(nil), m.listeners...)
	m.mu.RUnlock()
	m.emit(ctx, listeners, model.LifecycleEvent{Kind: kind, Server: st.Name, State: st, At: time.Now()})
}

func (m *Manager) markToolsChanged(server string) {
	// 仅标记待刷新；等 refreshChangedTools 完成 ListTools 后再发 EventToolsChanged，避免投影陈旧快照。
	m.mu.Lock()
	defer m.mu.Unlock()
	ms := m.sessions[server]
	if ms != nil {
		ms.refreshPending = true
		ms.toolsVersion++
	}
}

func (m *Manager) emit(ctx context.Context, listeners []contract.StateListener, event model.LifecycleEvent) {
	for _, l := range listeners {
		if l != nil {
			l.OnMCPEvent(ctx, event)
		}
	}
}

func filterTools(tools []model.ToolSnapshot, cfg model.McpServerConfig) []model.ToolSnapshot {
	if len(cfg.EnabledTools) == 0 && len(cfg.DisabledTools) == 0 {
		return tools
	}
	enabled := toSet(cfg.EnabledTools)
	disabled := toSet(cfg.DisabledTools)
	out := make([]model.ToolSnapshot, 0, len(tools))
	for _, t := range tools {
		if len(enabled) > 0 {
			if _, ok := enabled[t.Name]; !ok {
				continue
			}
		}
		if _, ok := disabled[t.Name]; ok {
			continue
		}
		out = append(out, t)
	}
	return out
}

func toSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out[item] = struct{}{}
		}
	}
	return out
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthorized") || strings.Contains(msg, "401") || strings.Contains(msg, "auth")
}

var _ contract.Manager = (*Manager)(nil)
