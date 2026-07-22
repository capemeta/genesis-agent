// Package http 提供 genesis-sandbox HTTP API 的产品无关客户端适配。
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	"genesis-agent/internal/runtime/progress"
)

func (c *Client) OpenSession(ctx context.Context, opts sandboxcontract.SessionOptions) (sandboxcontract.SandboxSession, error) {
	if c == nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox http client未初始化"))
	}
	progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseStart, Component: "genesis-sandbox", Name: string(opts.Sandbox.RuntimeProfile), Summary: "创建远程容器沙箱 session (genesis-sandbox)"})
	payload := createSessionRequestFromOptions(opts)
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox session请求失败: %w", err))
	}
	var record sessionRecord
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sessions", &body, &record); err != nil {
		progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelError, Component: "genesis-sandbox", Summary: "远程容器沙箱 session 创建失败", Detail: err.Error()})
		return nil, err
	}
	if strings.TrimSpace(record.SessionID) == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session响应缺少session_id"))
	}
	if record.ExpiresAt.IsZero() || !record.ExpiresAt.After(time.Now()) {
		c.deleteSessionBestEffort(record.SessionID)
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session %s未返回有效 expires_at", record.SessionID))
	}
	workspace := opts.Workspace
	workspaceID := firstNonEmpty(record.WorkspaceID, workspace.ID)
	if workspaceID == "" {
		c.deleteSessionBestEffort(record.SessionID)
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session %s未返回 workspace_id", record.SessionID))
	}
	workspace.ID = workspaceID
	if workspace.Provider == "" {
		workspace.Provider = "genesis-sandbox"
	}
	// 调用方 metadata 不得污染 ephemeral sandbox_id。
	if len(workspace.Metadata) > 0 {
		cleaned := make(map[string]string, len(workspace.Metadata))
		for k, v := range workspace.Metadata {
			if k == "sandbox_id" {
				continue
			}
			cleaned[k] = v
		}
		workspace.Metadata = cleaned
	}
	// active_sandbox_id 可为空：休眠 Session，首次 Exec 懒启 Runtime。
	session := &Session{
		client:    c,
		sessionID: record.SessionID,
		sandboxID: strings.TrimSpace(record.ActiveSandboxID),
		workspace: workspace,
		sandbox:   opts.Sandbox,
		options:   opts.Options,
		expiresAt: record.ExpiresAt,
	}
	// 对齐 SDK SandboxSession：心跳调用 POST /v1/sessions/{id}/renew，
	// 由服务端同时延长 Session 与底层 sandbox lease，再回写本地 expiresAt。
	renewCtx, cancelRenew := context.WithCancel(context.Background())
	session.cancelRenew = cancelRenew
	session.renewWG.Add(1)
	go func() {
		defer session.renewWG.Done()
		c.sessionRenewLoop(renewCtx, session)
	}()
	progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseComplete, Component: "genesis-sandbox", Name: record.SessionID, Summary: "远程容器沙箱 session 已就绪（genesis-sandbox）"})
	return session, nil
}

// Session 实现 sandbox 长会话端口。
type Session struct {
	client      *Client
	sessionID   string
	sandboxID   string
	workspace   sandboxcontract.WorkspaceRef
	sandbox     execmodel.SandboxProfile
	options     execcontract.RunOptions
	expiresAt   time.Time
	cancelRenew context.CancelFunc
	renewWG     sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

// Workspace 返回 session scoped WorkspaceFS 引用；ID 是 session_id，不是 workspace_id。
// sandbox_id 可为空（休眠态）；Exec 后会回填。
func (s *Session) Workspace() sandboxcontract.WorkspaceRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	metadata := map[string]string{
		"session_id":   s.sessionID,
		"workspace_id": s.workspace.ID,
	}
	// sandbox_id 只以本地权威字段为准；休眠/Suspend 后为空，禁止从输入 metadata 回灌。
	if s.sandboxID != "" {
		metadata["sandbox_id"] = s.sandboxID
	}
	for k, v := range s.workspace.Metadata {
		if k == "sandbox_id" || k == "session_id" || k == "workspace_id" {
			continue
		}
		if _, exists := metadata[k]; !exists {
			metadata[k] = v
		}
	}
	return sandboxcontract.WorkspaceRef{ID: s.sessionID, Provider: "genesis-sandbox", Metadata: metadata}
}

// ExpiresAt 返回服务端 session record 的权威 lease 截止时间。
func (s *Session) ExpiresAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.expiresAt
}

// Run 在当前 server-side Session 内执行命令，复用 session scoped /workspace。
func (s *Session) Run(ctx context.Context, req sandboxcontract.CommandRequest) (*execmodel.Result, error) {
	if s == nil || s.client == nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session未初始化"))
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session已关闭"))
	}
	if req.Workspace.ID == "" && req.Workspace.Provider == "" {
		req.Workspace = s.workspace
	}
	if req.Sandbox.RuntimeProfile == "" && req.Sandbox.TaskType == "" && req.Sandbox.Operation == "" {
		req.Sandbox = s.sandbox
	}
	req.Options = mergeRunOptions(s.options, req.Options)
	sessionCwd := sandboxSessionWorkingDir(req.Command.Cwd, sandboxWorkspace(req))
	argv := sessionCommandArgv(req)
	if len(argv) == 0 {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("sandbox session command不能为空"))
	}
	payload := execSessionRequest{Command: argv}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox session exec请求失败: %w", err))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx := ctx
	cancel := func() {}
	if timeout := requestTimeout(req); timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	started := time.Now()
	var execResult execSessionResult
	if err := s.client.doJSON(callCtx, http.MethodPost, "/v1/sessions/"+url.PathEscape(s.sessionID)+"/exec", &body, &execResult); err != nil {
		return nil, err
	}
	s.recordExecution(execResult)
	if strings.TrimSpace(execResult.WorkspaceID) != "" {
		s.mu.Lock()
		s.workspace.ID = strings.TrimSpace(execResult.WorkspaceID)
		s.mu.Unlock()
	}
	duration := time.Since(started)
	cwd := sessionCwd
	if strings.TrimSpace(execResult.Cwd) != "" {
		if cleaned := cleanSandboxAbsolutePath(execResult.Cwd); cleaned != "" {
			cwd = cleaned
		}
	}
	result := &execmodel.Result{
		Command:         req.Command.Command,
		Cwd:             cwd,
		Shell:           req.Command.Shell,
		Environment:     execmodel.EnvironmentSandbox,
		SandboxProvider: firstNonEmpty(req.Sandbox.Provider, "genesis-sandbox"),
		ExitCode:        execResult.ExitCode,
		Stdout:          execResult.Stdout,
		Stderr:          execResult.Stderr,
		Duration:        duration,
		DurationMS:      duration.Milliseconds(),
	}
	if execResult.ExitCode != 0 && strings.TrimSpace(execResult.Stderr) != "" {
		result.Error = execResult.Stderr
	}
	return result, nil
}

func (s *Session) recordExecution(result execSessionResult) {
	if s == nil {
		return
	}
	sandboxID := strings.TrimSpace(result.SandboxID)
	if sandboxID == "" {
		return
	}
	s.mu.Lock()
	s.sandboxID = sandboxID
	s.mu.Unlock()
}

// Suspend 释放 ephemeral Runtime，保留 Session 与 Workspace；心跳继续续 Session TTL。
func (s *Session) Suspend(ctx context.Context) error {
	if s == nil || s.client == nil {
		return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session未初始化"))
	}
	s.mu.Lock()
	closed := s.closed
	sessionID := s.sessionID
	s.mu.Unlock()
	if closed {
		return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session已关闭"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var record sessionRecord
	if err := s.client.doJSON(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/suspend", nil, &record); err != nil {
		return err
	}
	s.mu.Lock()
	s.sandboxID = strings.TrimSpace(record.ActiveSandboxID)
	if strings.TrimSpace(record.WorkspaceID) != "" {
		s.workspace.ID = strings.TrimSpace(record.WorkspaceID)
	}
	if !record.ExpiresAt.IsZero() {
		s.expiresAt = record.ExpiresAt
	}
	s.mu.Unlock()
	return nil
}

func mergeRunOptions(base, override execcontract.RunOptions) execcontract.RunOptions {
	out := override
	if out.Timeout == 0 {
		out.Timeout = base.Timeout
	}
	if out.MaxOutputBytes == 0 {
		out.MaxOutputBytes = base.MaxOutputBytes
	}
	if out.Sandbox.Mode == "" && out.Sandbox.Provider == "" && out.Sandbox.RuntimeProfile == "" && out.Sandbox.TaskType == "" && out.Sandbox.Operation == "" {
		out.Sandbox = base.Sandbox
	}
	if out.Binding.ID == "" {
		out.Binding = base.Binding
	}
	if out.Workspace.WorkDir == "" && out.Workspace.InputDir == "" && out.Workspace.OutputDir == "" && out.Workspace.TmpDir == "" && out.Workspace.SkillDir == "" {
		out.Workspace = base.Workspace
	}
	if len(out.StagedInputs) == 0 {
		out.StagedInputs = base.StagedInputs
	}
	if out.OutputDiscoveryPolicy == "" {
		out.OutputDiscoveryPolicy = base.OutputDiscoveryPolicy
	}
	return out
}

// Close 删除服务端 Session，并释放其 active sandbox。
// 对齐 SDK：先停心跳并等待退出，再 DeleteSession。
func (s *Session) Close(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cancelRenew := s.cancelRenew
	s.cancelRenew = nil
	s.mu.Unlock()
	if cancelRenew != nil {
		cancelRenew()
		s.renewWG.Wait()
	}
	baseCtx := context.Background()
	if ctx != nil {
		baseCtx = ctx
	}
	closeCtx, closeCancel := context.WithTimeout(baseCtx, s.client.closeTimeout)
	defer closeCancel()
	return s.client.doJSON(closeCtx, http.MethodDelete, "/v1/sessions/"+url.PathEscape(s.sessionID), nil, nil)
}

func (c *Client) sessionRenewLoop(ctx context.Context, session *Session) {
	if c == nil || session == nil {
		return
	}
	ticker := time.NewTicker(c.renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(ctx, defaultRenewTimeout)
			record, err := c.renewSessionRecord(renewCtx, session.sessionID)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				progress.Emit(ctx, progress.Event{
					Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelWarn,
					Component: "genesis-sandbox", Name: session.sessionID,
					Summary: "sandbox session 心跳续期失败，将重试", Detail: err.Error(),
				})
				continue
			}
			session.applyRecord(record)
		}
	}
}

// RenewSession 续期命名 Session（同时延长底层 sandbox lease），对齐 SDK Client.RenewSession。
func (c *Client) RenewSession(ctx context.Context, sessionID string) (time.Time, error) {
	record, err := c.renewSessionRecord(ctx, sessionID)
	if err != nil {
		return time.Time{}, err
	}
	return record.ExpiresAt, nil
}

func (c *Client) renewSessionRecord(ctx context.Context, sessionID string) (sessionRecord, error) {
	if c == nil {
		return sessionRecord{}, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox http client未初始化"))
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sessionRecord{}, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("session id不能为空"))
	}
	extendSeconds := int(c.renewExtend.Seconds())
	if extendSeconds <= 0 {
		extendSeconds = int(defaultRenewExtend.Seconds())
	}
	payload := map[string]int{"extend_seconds": extendSeconds}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return sessionRecord{}, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox session renew请求失败: %w", err))
	}
	var record sessionRecord
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/renew", &body, &record); err != nil {
		return sessionRecord{}, err
	}
	if record.ExpiresAt.IsZero() {
		return sessionRecord{}, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session renew 未返回 expires_at"))
	}
	return record, nil
}

func (s *Session) applyRecord(record sessionRecord) {
	if s == nil || record.ExpiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	s.expiresAt = record.ExpiresAt
	// Suspend 后 active_sandbox_id 为空，必须同步清空本地缓存。
	s.sandboxID = strings.TrimSpace(record.ActiveSandboxID)
	if strings.TrimSpace(record.WorkspaceID) != "" {
		s.workspace.ID = strings.TrimSpace(record.WorkspaceID)
	}
	s.mu.Unlock()
}

type execSessionRequest struct {
	Command  []string `json:"command,omitempty"`
	Code     string   `json:"code,omitempty"`
	Language string   `json:"language,omitempty"`
}

type execSessionResult struct {
	ExitCode    int    `json:"exit_code"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Environment string `json:"environment,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	SandboxID   string `json:"sandbox_id,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
}

type createSessionRequest struct {
	WorkspaceID         string            `json:"workspace_id,omitempty"`
	RuntimeProfile      string            `json:"runtime_profile,omitempty"`
	StatePolicy         string            `json:"state_policy,omitempty"`
	TTLSeconds          int               `json:"ttl_seconds,omitempty"`
	WorkspaceRetention  string            `json:"workspace_retention,omitempty"`
	WorkspaceTTLSeconds int               `json:"workspace_ttl_seconds,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
}

type sessionRecord struct {
	SessionID       string            `json:"session_id"`
	TenantID        string            `json:"tenant_id"`
	UserID          string            `json:"user_id,omitempty"`
	WorkspaceID     string            `json:"workspace_id"`
	RuntimeProfile  string            `json:"runtime_profile"`
	StatePolicy     string            `json:"state_policy"`
	ActiveSandboxID string            `json:"active_sandbox_id,omitempty"`
	Status          string            `json:"status"`
	CreatedAt       time.Time         `json:"created_at"`
	ExpiresAt       time.Time         `json:"expires_at"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

func createSessionRequestFromOptions(opts sandboxcontract.SessionOptions) createSessionRequest {
	ttl := defaultSessionTTL
	if opts.Options.Timeout > 0 {
		seconds := int(opts.Options.Timeout.Seconds())
		if seconds > ttl {
			ttl = seconds
		}
	}
	metadata := map[string]string{}
	for k, v := range opts.Sandbox.Metadata {
		metadata[k] = v
	}
	for k, v := range opts.Workspace.Metadata {
		if _, exists := metadata[k]; !exists {
			metadata[k] = v
		}
	}
	retention := firstNonEmpty(metadata["workspace_retention"], "explicit_delete")
	delete(metadata, "workspace_retention")
	delete(metadata, "sandbox_id")
	delete(metadata, "session_id")
	workspaceTTL := 0
	if raw := strings.TrimSpace(metadata["workspace_ttl_seconds"]); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			workspaceTTL = n
		}
		delete(metadata, "workspace_ttl_seconds")
	}
	return createSessionRequest{
		WorkspaceID:         firstNonEmpty(opts.Workspace.ID, opts.Sandbox.WorkspaceID),
		RuntimeProfile:      string(opts.Sandbox.RuntimeProfile),
		StatePolicy:         "session",
		TTLSeconds:          ttl,
		WorkspaceRetention:  retention,
		WorkspaceTTLSeconds: workspaceTTL,
		Metadata:            metadata,
	}
}

func sessionCommandArgv(req sandboxcontract.CommandRequest) []string {
	raw := strings.TrimSpace(req.Command.Command)
	if raw == "" {
		return nil
	}
	workspace := sandboxWorkspace(req)
	cwd := sandboxSessionWorkingDir(req.Command.Cwd, workspace)
	env := sandboxEnv(req.Command.Env, workspace)
	script := sessionShellScript(raw, cwd, env)
	return []string{"sh", "-lc", script}
}

func sandboxSessionWorkingDir(commandCwd string, workspace execmodel.ExecutionWorkspace) string {
	workDir := cleanSandboxAbsolutePath(firstNonEmpty(workspace.WorkDir, "/workspace"))
	if workDir == "" {
		workDir = "/workspace"
	}
	cwd := cleanSandboxAbsolutePath(commandCwd)
	if cwd == workDir || strings.HasPrefix(cwd, workDir+"/") {
		return cwd
	}
	return workDir
}

func cleanSandboxAbsolutePath(value string) string {
	value = strings.TrimSpace(filepath.ToSlash(value))
	if value == "" {
		return ""
	}
	if value == "/workspace" || strings.HasPrefix(value, "/workspace/") {
		clean := path.Clean(value)
		if clean == "." || clean == "/" {
			return ""
		}
		return clean
	}
	return ""
}

func sessionShellScript(command, cwd string, env map[string]string) string {
	parts := make([]string, 0, len(env)+2)
	if cwd != "" {
		parts = append(parts, "cd "+shellQuote(cwd))
	}
	for _, key := range sortedValidEnvKeys(env) {
		parts = append(parts, "export "+key+"="+shellQuote(env[key]))
	}
	parts = append(parts, command)
	return strings.Join(parts, " && ")
}

func sortedValidEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		if isValidShellEnvKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func isValidShellEnvKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r == '_':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	first := rune(key[0])
	return first == '_' || (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
