// Package http 提供 genesis-sandbox HTTP API 的产品无关客户端适配。
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	"genesis-agent/internal/runtime/progress"
)

const (
	defaultQueueWaitSeconds = 10
	// 与 genesis-sandbox/sdks/go/sandbox 默认心跳参数对齐。
	defaultRenewInterval = 30 * time.Second
	defaultRenewExtend   = 90 * time.Second
	defaultPollStart     = 500 * time.Millisecond
	defaultPollMax       = 3 * time.Second
	defaultCloseTimeout  = 30 * time.Second
	defaultRenewTimeout  = 15 * time.Second
	defaultSessionTTL    = 300
)

// Config 描述 genesis-sandbox HTTP client 配置。
type Config struct {
	BaseURL       string
	APIKey        string
	Timeout       time.Duration
	Client        *http.Client
	RenewInterval time.Duration
	RenewExtend   time.Duration
	PollStart     time.Duration
	PollMax       time.Duration
	CloseTimeout  time.Duration
}

// Client 实现 sandbox CommandClient。
type Client struct {
	baseURL       *url.URL
	apiKey        string
	httpClient    *http.Client
	renewInterval time.Duration
	renewExtend   time.Duration
	pollStart     time.Duration
	pollMax       time.Duration
	closeTimeout  time.Duration
}

// New 创建 HTTP sandbox client。
func New(cfg Config) (*Client, error) {
	rawBaseURL := strings.TrimSpace(cfg.BaseURL)
	if rawBaseURL == "" {
		return nil, fmt.Errorf("sandbox http base_url不能为空")
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("解析sandbox http base_url失败: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("sandbox http base_url必须包含scheme和host")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	renewInterval := cfg.RenewInterval
	if renewInterval <= 0 {
		renewInterval = defaultRenewInterval
	}
	renewExtend := cfg.RenewExtend
	if renewExtend <= 0 {
		renewExtend = defaultRenewExtend
	}
	pollStart := cfg.PollStart
	if pollStart <= 0 {
		pollStart = defaultPollStart
	}
	pollMax := cfg.PollMax
	if pollMax <= 0 {
		pollMax = defaultPollMax
	}
	closeTimeout := cfg.CloseTimeout
	if closeTimeout <= 0 {
		closeTimeout = defaultCloseTimeout
	}
	return &Client{
		baseURL:       parsed,
		apiKey:        strings.TrimSpace(cfg.APIKey),
		httpClient:    httpClient,
		renewInterval: renewInterval,
		renewExtend:   renewExtend,
		pollStart:     pollStart,
		pollMax:       pollMax,
		closeTimeout:  closeTimeout,
	}, nil
}

// RunCommand 按 genesis-sandbox SDK 的生产语义执行：lease、续租、提交 job、轮询、下载产物、释放资源。
func (c *Client) RunCommand(ctx context.Context, req sandboxcontract.CommandRequest) (result *execmodel.Result, err error) {
	if c == nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox http client未初始化"))
	}
	progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseStart, Component: "genesis-sandbox", Name: string(req.Sandbox.RuntimeProfile), Summary: "申请 sandbox 租约"})
	lease, err := c.leaseSandbox(ctx, req)
	if err != nil {
		progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelError, Component: "genesis-sandbox", Summary: "sandbox 租约申请失败", Detail: err.Error()})
		return nil, err
	}
	progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseComplete, Component: "genesis-sandbox", Name: lease.SandboxID, Summary: "sandbox 租约已就绪"})
	renewCtx, cancelRenew := context.WithCancel(context.Background())
	go c.renewLoop(renewCtx, lease.SandboxID, nil)
	defer func() {
		cancelRenew()
		progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseStart, Component: "genesis-sandbox", Name: lease.SandboxID, Summary: "释放 sandbox 租约"})
		if closeErr := c.closeSandbox(lease.SandboxID); closeErr != nil {
			if result != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("sandbox释放异常: %v", closeErr))
			} else if err == nil {
				err = closeErr
			}
			progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelWarn, Component: "genesis-sandbox", Name: lease.SandboxID, Summary: "sandbox 租约释放异常", Detail: closeErr.Error()})
			return
		}
		progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseComplete, Component: "genesis-sandbox", Name: lease.SandboxID, Summary: "sandbox 租约已释放"})
	}()

	payload := execJobRequestFromCommand(req)
	payload.SandboxID = lease.SandboxID
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox job请求失败: %w", err))
	}
	var job jobResult
	progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseStart, Component: "genesis-sandbox", Name: lease.SandboxID, Summary: "提交 sandbox job"})
	if err := c.doJSON(ctx, http.MethodPost, "/v1/jobs", &body, &job); err != nil {
		progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelError, Component: "genesis-sandbox", Name: lease.SandboxID, Summary: "sandbox job 提交失败", Detail: err.Error()})
		return nil, err
	}
	progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseProgress, Component: "genesis-sandbox", Name: job.JobID, Summary: "等待 sandbox job 完成"})
	finalJob, err := c.finalJob(ctx, job, requestTimeout(req))
	if err != nil {
		progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelError, Component: "genesis-sandbox", Name: job.JobID, Summary: "sandbox job 执行失败", Detail: err.Error()})
		return nil, err
	}
	progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseComplete, Component: "genesis-sandbox", Name: finalJob.JobID, Summary: "sandbox job 已完成"})
	return c.resultFromJob(ctx, req, *finalJob), nil
}

// OpenSession 打开一个可复用 /workspace 状态的 sandbox 长会话。
func (c *Client) OpenSession(ctx context.Context, opts sandboxcontract.SessionOptions) (sandboxcontract.SandboxSession, error) {
	if c == nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox http client未初始化"))
	}
	progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseStart, Component: "genesis-sandbox", Name: string(opts.Sandbox.RuntimeProfile), Summary: "创建 sandbox session"})
	payload := createSessionRequestFromOptions(opts)
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox session请求失败: %w", err))
	}
	var record sessionRecord
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sessions", &body, &record); err != nil {
		progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelError, Component: "genesis-sandbox", Summary: "sandbox session创建失败", Detail: err.Error()})
		return nil, err
	}
	if strings.TrimSpace(record.SessionID) == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session响应缺少session_id"))
	}
	if strings.TrimSpace(record.ActiveSandboxID) == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session %s缺少active_sandbox_id", record.SessionID))
	}
	if record.ExpiresAt.IsZero() || !record.ExpiresAt.After(time.Now()) {
		_ = c.doJSON(context.Background(), http.MethodDelete, "/v1/sessions/"+url.PathEscape(record.SessionID), nil, nil)
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session %s未返回有效 expires_at", record.SessionID))
	}
	workspace := opts.Workspace
	if strings.TrimSpace(workspace.ID) == "" && strings.TrimSpace(record.WorkspaceID) != "" {
		workspace.ID = record.WorkspaceID
	}
	if workspace.Provider == "" {
		workspace.Provider = "genesis-sandbox"
	}
	session := &Session{
		client:    c,
		sessionID: record.SessionID,
		sandboxID: record.ActiveSandboxID,
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
	progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseComplete, Component: "genesis-sandbox", Name: record.SessionID, Summary: "sandbox session已就绪"})
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
func (s *Session) Workspace() sandboxcontract.WorkspaceRef {
	metadata := map[string]string{
		"session_id":   s.sessionID,
		"sandbox_id":   s.sandboxID,
		"workspace_id": s.workspace.ID,
	}
	for k, v := range s.workspace.Metadata {
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
	duration := time.Since(started)
	result := &execmodel.Result{
		Command:         req.Command.Command,
		Cwd:             sessionCwd,
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

func (c *Client) resultFromJob(ctx context.Context, req sandboxcontract.CommandRequest, job jobResult) *execmodel.Result {
	workspace := sandboxWorkspace(req)
	workingDir := sandboxWorkingDir(req.Command.Cwd, workspace)
	result := &execmodel.Result{
		Command:         req.Command.Command,
		Cwd:             workingDir,
		Shell:           req.Command.Shell,
		Environment:     execmodel.EnvironmentSandbox,
		SandboxProvider: req.Sandbox.Provider,
		ExitCode:        job.ExitCode,
		Stdout:          job.Stdout,
		Stderr:          job.Stderr,
		DurationMS:      job.DurationMS,
		OutputTruncated: job.StdoutTruncated || job.StderrTruncated,
		Error:           firstNonEmpty(job.Error, job.ErrorMessage, job.ErrorCode),
		OutputObjects:   outputObjectsFromJob(job.OutputArtifacts),
	}
	if job.Status == "timed_out" || job.ErrorCode == "EXEC_TIMEOUT" {
		result.TimedOut = true
	}
	if req.Options.OutputDiscoveryPolicy == execmodel.OutputDiscoveryDeclared && job.ExitCode == 0 && len(result.OutputObjects) == 0 {
		result.Warnings = append(result.Warnings,
			"NO_OUTPUT_ARTIFACTS: 代码执行成功，但没有在OUTPUT_DIR发现可回传成果物；请把最终文件写入环境变量OUTPUT_DIR指向的目录",
		)
	}
	return result
}

func (c *Client) leaseSandbox(ctx context.Context, req sandboxcontract.CommandRequest) (*sandboxLease, error) {
	payload := leaseRequestFromCommand(req)
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox lease请求失败: %w", err))
	}
	var lease sandboxLease
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sandboxes:lease", &body, &lease); err != nil {
		return nil, err
	}
	if strings.TrimSpace(lease.SandboxID) == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox lease响应缺少sandbox_id"))
	}
	return &lease, nil
}

func (c *Client) finalJob(ctx context.Context, job jobResult, timeout time.Duration) (*jobResult, error) {
	if strings.TrimSpace(job.JobID) == "" {
		return &job, nil
	}
	if !isTerminalJobStatus(job.Status) {
		return c.pollJob(ctx, job.JobID, timeout)
	}
	return c.getJob(ctx, job.JobID)
}

func (c *Client) getJob(ctx context.Context, jobID string) (*jobResult, error) {
	var result jobResult
	if err := c.doJSON(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(jobID), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) pollJob(ctx context.Context, jobID string, timeout time.Duration) (*jobResult, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	interval := c.pollStart
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		interval = minDuration(time.Duration(float64(interval)*1.5), c.pollMax)
		result, err := c.getJob(ctx, jobID)
		if err != nil {
			return nil, err
		}
		if isTerminalJobStatus(result.Status) {
			return result, nil
		}
	}
	return nil, execcontract.NewError(execcontract.ErrCodeTimeout, fmt.Errorf("sandbox job %s timed out after %s", jobID, timeout))
}

// renewLoop 仅用于一次性 Job 路径的 sandbox lease 心跳。
// Session 长会话必须走 sessionRenewLoop / RenewSession，避免只续 lease 不续 session。
func (c *Client) renewLoop(ctx context.Context, sandboxID string, onRenew func(time.Time)) {
	ticker := time.NewTicker(c.renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(context.Background(), defaultRenewTimeout)
			expiresAt, err := c.RenewSandbox(renewCtx, sandboxID)
			cancel()
			if err == nil && onRenew != nil {
				onRenew(expiresAt)
			}
		}
	}
}

// sessionRenewLoop 对齐 SDK heartbeatLoop：周期调用 RenewSession。
// renew 的 timeout 挂在心跳 ctx 上，Close 取消时能打断 in-flight 请求。
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
			expiresAt, err := c.RenewSession(renewCtx, session.sessionID)
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
			if expiresAt.IsZero() {
				continue
			}
			session.mu.Lock()
			session.expiresAt = expiresAt
			session.mu.Unlock()
		}
	}
}

// RenewSession 续期命名 Session（同时延长底层 sandbox lease），对齐 SDK Client.RenewSession。
func (c *Client) RenewSession(ctx context.Context, sessionID string) (time.Time, error) {
	if c == nil {
		return time.Time{}, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox http client未初始化"))
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return time.Time{}, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("session id不能为空"))
	}
	extendSeconds := int(c.renewExtend.Seconds())
	if extendSeconds <= 0 {
		extendSeconds = int(defaultRenewExtend.Seconds())
	}
	payload := map[string]int{"extend_seconds": extendSeconds}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return time.Time{}, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox session renew请求失败: %w", err))
	}
	var record sessionRecord
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/renew", &body, &record); err != nil {
		return time.Time{}, err
	}
	if record.ExpiresAt.IsZero() {
		return time.Time{}, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session renew 未返回 expires_at"))
	}
	return record.ExpiresAt, nil
}

// RenewSandbox 续租指定 sandbox，返回服务端确认的新到期时间。
// 仅用于 Job 路径；Session 路径请使用 RenewSession。
func (c *Client) RenewSandbox(ctx context.Context, sandboxID string) (time.Time, error) {
	if c == nil {
		return time.Time{}, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox http client未初始化"))
	}
	sandboxID = strings.TrimSpace(sandboxID)
	if sandboxID == "" {
		return time.Time{}, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("sandbox id不能为空"))
	}
	extendSeconds := int(c.renewExtend.Seconds())
	if extendSeconds <= 0 {
		extendSeconds = int(defaultRenewExtend.Seconds())
	}
	payload := map[string]int{"extend_seconds": extendSeconds}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return time.Time{}, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox renew请求失败: %w", err))
	}
	var lease sandboxLease
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(sandboxID)+"/renew", &body, &lease); err != nil {
		return time.Time{}, err
	}
	return lease.ExpiresAt, nil
}

func (c *Client) closeSandbox(sandboxID string) error {
	closeCtx, cancel := context.WithTimeout(context.Background(), c.closeTimeout)
	err := c.closeSandboxWithContext(closeCtx, sandboxID)
	cancel()
	return err
}

func (c *Client) closeSandboxWithContext(ctx context.Context, sandboxID string) error {
	err := c.doJSON(ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(sandboxID)+"/release", nil, nil)
	if err == nil {
		return nil
	}
	destroyErr := c.doJSON(ctx, http.MethodDelete, "/v1/sandboxes/"+url.PathEscape(sandboxID), nil, nil)
	if destroyErr != nil {
		return fmt.Errorf("release失败: %w; destroy也失败: %v", err, destroyErr)
	}
	return fmt.Errorf("release失败，已destroy回收: %w", err)
}

// DownloadArtifact 下载 sandbox artifact 原始字节。
func (c *Client) DownloadArtifact(ctx context.Context, artifactID string) ([]byte, error) {
	if strings.TrimSpace(artifactID) == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("artifact id不能为空"))
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: strings.TrimRight(c.baseURL.Path, "/") + "/v1/artifacts/" + url.PathEscape(artifactID)})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("创建artifact下载请求失败: %w", err))
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("下载sandbox artifact失败: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mapHTTPError(resp)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("读取artifact响应失败: %w", err))
	}
	return data, nil
}

func (c *Client) uploadJobFile(ctx context.Context, jobID, name string, content io.Reader) (*artifactRecord, error) {
	if strings.TrimSpace(jobID) == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("job id不能为空"))
	}
	if strings.TrimSpace(name) == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("上传文件名不能为空"))
	}
	if content == nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("上传文件内容不能为空"))
	}
	query := url.Values{}
	query.Set("name", name)
	endpoint := c.baseURL.ResolveReference(&url.URL{
		Path:     strings.TrimRight(c.baseURL.Path, "/") + "/v1/jobs/" + url.PathEscape(jobID) + "/files",
		RawQuery: query.Encode(),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), content)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("创建sandbox输入上传请求失败: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/octet-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("上传sandbox输入文件失败: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mapHTTPError(resp)
	}
	var artifact artifactRecord
	if err := json.NewDecoder(resp.Body).Decode(&artifact); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("解析sandbox输入artifact失败: %w", err))
	}
	return &artifact, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body io.Reader, out any) error {
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: strings.TrimRight(c.baseURL.Path, "/") + path})
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("创建sandbox http请求失败: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("调用sandbox http失败: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapHTTPError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("解析sandbox http响应失败: %w", err))
	}
	return nil
}

func mapHTTPError(resp *http.Response) error {
	var apiErr errorResponse
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if len(data) > 0 {
		_ = json.Unmarshal(data, &apiErr)
	}
	message := strings.TrimSpace(apiErr.Message)
	if message == "" {
		message = strings.TrimSpace(string(data))
	}
	if message == "" {
		message = resp.Status
	}
	code := strings.TrimSpace(apiErr.Code)
	switch {
	case resp.StatusCode == http.StatusBadRequest || code == "INVALID_ARGUMENT":
		return execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("%s", message))
	case resp.StatusCode == http.StatusForbidden || code == "POLICY_DENIED" || code == "NETWORK_DENIED":
		return execcontract.NewError(execcontract.ErrCodePermissionDenied, fmt.Errorf("%s", message))
	case resp.StatusCode == http.StatusTooManyRequests || code == "QUEUE_FULL" || code == "QUOTA_EXCEEDED":
		return execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("%s", message))
	case resp.StatusCode == http.StatusServiceUnavailable || code == "RUNTIME_UNAVAILABLE":
		return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("%s", message))
	case code == "EXEC_TIMEOUT":
		return execcontract.NewError(execcontract.ErrCodeTimeout, fmt.Errorf("%s", message))
	default:
		return execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("%s", message))
	}
}

type execJobRequest struct {
	SandboxID               string            `json:"sandbox_id,omitempty"`
	WorkspaceID             string            `json:"workspace_id,omitempty"`
	RuntimeProfile          string            `json:"runtime_profile,omitempty"`
	TaskType                string            `json:"task_type,omitempty"`
	Operation               string            `json:"operation,omitempty"`
	RiskLevel               string            `json:"risk_level,omitempty"`
	Language                string            `json:"language,omitempty"`
	Command                 []string          `json:"command,omitempty"`
	ExecTimeoutSeconds      int64             `json:"exec_timeout_seconds,omitempty"`
	QueueWaitTimeoutSeconds int               `json:"queue_wait_timeout_seconds,omitempty"`
	Metadata                map[string]string `json:"metadata,omitempty"`
	Spec                    sandboxSpec       `json:"spec,omitempty"`
	InputArtifactIDs        []string          `json:"input_artifact_ids,omitempty"`
}

type execSessionRequest struct {
	Command  []string `json:"command,omitempty"`
	Code     string   `json:"code,omitempty"`
	Language string   `json:"language,omitempty"`
}

type execSessionResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

type sandboxSpec struct {
	WorkingDir string            `json:"working_dir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

type jobResult struct {
	JobID           string           `json:"job_id"`
	SandboxID       string           `json:"sandbox_id"`
	WorkspaceID     string           `json:"workspace_id"`
	Status          string           `json:"status"`
	ExitCode        int              `json:"exit_code"`
	Stdout          string           `json:"stdout"`
	Stderr          string           `json:"stderr"`
	ErrorCode       string           `json:"error_code"`
	Error           string           `json:"error"`
	ErrorMessage    string           `json:"error_message"`
	DurationMS      int64            `json:"duration_ms"`
	StdoutTruncated bool             `json:"stdout_truncated"`
	StderrTruncated bool             `json:"stderr_truncated"`
	OutputArtifacts []artifactRecord `json:"output_artifacts"`
}

type artifactRecord struct {
	ArtifactID  string    `json:"artifact_id"`
	WorkspaceID string    `json:"workspace_id"`
	JobID       string    `json:"job_id"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	MIME        string    `json:"mime"`
	CreatedAt   time.Time `json:"created_at"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type leaseRequest struct {
	WorkspaceID    string            `json:"workspace_id,omitempty"`
	RuntimeProfile string            `json:"runtime_profile,omitempty"`
	TaskType       string            `json:"task_type,omitempty"`
	Operation      string            `json:"operation,omitempty"`
	Language       string            `json:"language,omitempty"`
	RiskLevel      string            `json:"risk_level,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Spec           sandboxSpec       `json:"spec,omitempty"`
}

type sandboxLease struct {
	SandboxID       string            `json:"sandbox_id"`
	LeaseID         string            `json:"lease_id"`
	WorkspaceID     string            `json:"workspace_id,omitempty"`
	RuntimeProfile  string            `json:"runtime_profile"`
	Status          string            `json:"status"`
	ExpiresAt       time.Time         `json:"expires_at"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	ResourceVersion int64             `json:"resource_version"`
}

func leaseRequestFromCommand(req sandboxcontract.CommandRequest) leaseRequest {
	job := execJobRequestFromCommand(req)
	return leaseRequest{
		WorkspaceID:    job.WorkspaceID,
		RuntimeProfile: job.RuntimeProfile,
		TaskType:       job.TaskType,
		Operation:      job.Operation,
		Language:       job.Language,
		RiskLevel:      job.RiskLevel,
		Metadata:       job.Metadata,
		Spec:           job.Spec,
	}
}

func execJobRequestFromCommand(req sandboxcontract.CommandRequest) execJobRequest {
	metadata := map[string]string{}
	for k, v := range req.Sandbox.Metadata {
		metadata[k] = v
	}
	if req.Command.Cwd != "" && strings.HasPrefix(filepath.ToSlash(req.Command.Cwd), "/workspace") {
		metadata["cwd"] = req.Command.Cwd
	} else if req.Command.Cwd != "" {
		metadata["cwd_kind"] = "host_or_logical"
	}
	for k, v := range req.Workspace.Metadata {
		if _, exists := metadata[k]; !exists {
			metadata[k] = v
		}
	}
	timeoutSeconds := int64(req.Options.Timeout.Seconds())
	if timeoutSeconds <= 0 {
		timeoutSeconds = int64(60)
	}
	language := req.Sandbox.Language
	if language == "shell" {
		language = ""
	}
	workspace := sandboxWorkspace(req)
	workingDir := sandboxWorkingDir(req.Command.Cwd, workspace)
	inputArtifactIDs := make([]string, 0, len(req.Options.StagedInputs))
	for _, artifact := range req.Options.StagedInputs {
		if strings.TrimSpace(artifact.ID) != "" {
			inputArtifactIDs = append(inputArtifactIDs, artifact.ID)
		}
	}
	return execJobRequest{
		WorkspaceID:             firstNonEmpty(req.Workspace.ID, req.Sandbox.WorkspaceID),
		RuntimeProfile:          string(req.Sandbox.RuntimeProfile),
		TaskType:                string(req.Sandbox.TaskType),
		Operation:               string(req.Sandbox.Operation),
		RiskLevel:               string(req.Sandbox.RiskLevel),
		Language:                language,
		Command:                 commandArgv(req.Command),
		ExecTimeoutSeconds:      timeoutSeconds,
		QueueWaitTimeoutSeconds: defaultQueueWaitSeconds,
		Metadata:                metadata,
		Spec: sandboxSpec{
			WorkingDir: workingDir,
			Env:        sandboxEnv(req.Command.Env, workspace),
		},
		InputArtifactIDs: inputArtifactIDs,
	}
}

func sandboxWorkspace(req sandboxcontract.CommandRequest) execmodel.ExecutionWorkspace {
	workspace := execmodel.ExecutionWorkspace{
		WorkDir:   "/workspace",
		InputDir:  "/workspace/input",
		OutputDir: "/workspace/output",
		TmpDir:    "/workspace/tmp",
	}
	if strings.TrimSpace(req.Options.Workspace.SkillDir) != "" {
		workspace.SkillDir = "/workspace"
	}
	return workspace
}

func sandboxWorkingDir(commandCwd string, workspace execmodel.ExecutionWorkspace) string {
	cwd := strings.TrimSpace(filepath.ToSlash(commandCwd))
	workDir := strings.TrimRight(firstNonEmpty(filepath.ToSlash(workspace.WorkDir), "/workspace"), "/")
	if cwd == workDir || strings.HasPrefix(cwd, workDir+"/") {
		return cwd
	}
	return workDir
}
func sandboxEnv(userEnv map[string]string, workspace execmodel.ExecutionWorkspace) map[string]string {
	env := make(map[string]string, len(userEnv)+5)
	for k, v := range userEnv {
		env[k] = v
	}
	env["WORK_DIR"] = workspace.WorkDir
	env["INPUT_DIR"] = workspace.InputDir
	env["OUTPUT_DIR"] = workspace.OutputDir
	env["TMPDIR"] = workspace.TmpDir
	if strings.TrimSpace(workspace.SkillDir) != "" {
		env["SKILL_DIR"] = workspace.SkillDir
	}
	env["GENESIS_WORKSPACE"] = workspace.WorkDir
	return env
}

func requestTimeout(req sandboxcontract.CommandRequest) time.Duration {
	if req.Options.Timeout > 0 {
		return req.Options.Timeout
	}
	return 60 * time.Second
}

func isTerminalJobStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "failed", "cancelled", "timeout", "timed_out":
		return true
	default:
		return false
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func outputObjectsFromJob(records []artifactRecord) []execmodel.ExecutorOutputObject {
	if len(records) == 0 {
		return nil
	}
	out := make([]execmodel.ExecutorOutputObject, 0, len(records))
	for _, record := range records {
		version := strings.TrimSpace(record.SHA256)
		if version != "" && !strings.HasPrefix(strings.ToLower(version), "sha256:") {
			version = "sha256:" + version
		}
		out = append(out, execmodel.ExecutorOutputObject{
			ID:          record.ArtifactID,
			WorkspaceID: record.WorkspaceID,
			JobID:       record.JobID,
			Name:        record.Name,
			Size:        record.Size,
			SHA256:      record.SHA256,
			MediaType:   record.MIME,
			Version:     version,
		})
	}
	return out
}

func commandArgv(cmd execmodel.Command) []string {
	raw := strings.TrimSpace(cmd.Command)
	if raw == "" {
		return nil
	}
	switch cmd.Shell {
	case execmodel.ShellPowerShell:
		return []string{"pwsh", "-NoLogo", "-NoProfile", "-Command", raw}
	case execmodel.ShellCmd:
		return []string{"cmd", "/C", raw}
	case execmodel.ShellBash:
		return []string{"bash", "-lc", raw}
	case execmodel.ShellSh, execmodel.ShellSystem, execmodel.ShellAuto, "":
		return []string{"sh", "-lc", raw}
	default:
		return []string{string(cmd.Shell), "-lc", raw}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var unsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9._ -]+`)

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	name = unsafeFilenameChars.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	if name == "" {
		return "artifact.bin"
	}
	return name
}

func sanitizePathPart(value string) string {
	value = unsafeFilenameChars.ReplaceAllString(strings.TrimSpace(value), "_")
	value = strings.Trim(value, ". ")
	if value == "" {
		return "default"
	}
	return value
}

type createSessionRequest struct {
	WorkspaceID    string            `json:"workspace_id,omitempty"`
	RuntimeProfile string            `json:"runtime_profile,omitempty"`
	StatePolicy    string            `json:"state_policy,omitempty"`
	TTLSeconds     int               `json:"ttl_seconds,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
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

type workspaceFileInfo struct {
	Path          string    `json:"path"`
	ContainerPath string    `json:"container_path,omitempty"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	Size          int64     `json:"size,omitempty"`
	MIME          string    `json:"mime,omitempty"`
	ModTime       time.Time `json:"mod_time,omitempty"`
}

type workspaceListResult struct {
	Path      string              `json:"path"`
	Entries   []workspaceFileInfo `json:"entries"`
	Truncated bool                `json:"truncated"`
	Limit     int                 `json:"limit"`
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
	return createSessionRequest{
		WorkspaceID:    firstNonEmpty(opts.Workspace.ID, opts.Sandbox.WorkspaceID),
		RuntimeProfile: string(opts.Sandbox.RuntimeProfile),
		StatePolicy:    "session",
		TTLSeconds:     ttl,
		Metadata:       metadata,
	}
}

func (c *Client) ReadFile(ctx context.Context, req sandboxcontract.FileRequest, opts fscontract.ReadOptions) ([]byte, error) {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	resp, err := c.doRaw(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/files", query, "", nil)
	if err != nil {
		return nil, fsErrorFromExec(err, workspacePath)
	}
	defer resp.Body.Close()
	reader := io.Reader(resp.Body)
	if opts.MaxBytes > 0 {
		reader = io.LimitReader(resp.Body, opts.MaxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fscontract.NewError(fscontract.ErrCodeInvalidInput, workspacePath, fmt.Errorf("读取sandbox session文件失败: %w", err))
	}
	if opts.MaxBytes > 0 && int64(len(data)) > opts.MaxBytes {
		return nil, fscontract.NewError(fscontract.ErrCodeTooLarge, workspacePath, fmt.Errorf("文件超过读取上限: %d", opts.MaxBytes))
	}
	return data, nil
}

func (c *Client) WriteFile(ctx context.Context, req sandboxcontract.WriteFileRequest) error {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return err
	}
	if !req.Options.Overwrite {
		if _, err := c.Stat(ctx, sandboxcontract.FileRequest{Workspace: req.Workspace, Path: req.Path}); err == nil {
			return fscontract.NewError(fscontract.ErrCodeAlreadyExists, workspacePath, fmt.Errorf("目标文件已存在"))
		} else if fscontract.CodeOf(err) != fscontract.ErrCodeNotFound {
			return err
		}
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	resp, err := c.doRaw(ctx, http.MethodPut, "/v1/sessions/"+url.PathEscape(sessionID)+"/files", query, "application/octet-stream", bytes.NewReader(req.Content))
	if err != nil {
		return fsErrorFromExec(err, workspacePath)
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) ListDir(ctx context.Context, req sandboxcontract.ListDirRequest) ([]fsmodel.DirEntry, error) {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	query.Set("recursive", "false")
	if req.Options.MaxEntries > 0 {
		query.Set("limit", strconv.Itoa(req.Options.MaxEntries))
	}
	var result workspaceListResult
	if err := c.doSessionJSON(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/files:list", query, nil, &result); err != nil {
		return nil, fsErrorFromExec(err, workspacePath)
	}
	return dirEntriesFromWorkspace(result.Entries), nil
}

func (c *Client) Walk(ctx context.Context, req sandboxcontract.WalkRequest) (*fsmodel.WalkOutcome, error) {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	query.Set("recursive", "true")
	if req.Options.MaxEntries > 0 {
		query.Set("limit", strconv.Itoa(req.Options.MaxEntries))
	}
	var result workspaceListResult
	if err := c.doSessionJSON(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/files:list", query, nil, &result); err != nil {
		return nil, fsErrorFromExec(err, workspacePath)
	}
	entries := dirEntriesFromWorkspace(result.Entries)
	out := &fsmodel.WalkOutcome{Root: workspacePath, Entries: entries, Truncated: result.Truncated}
	for _, entry := range entries {
		if entry.Type == fsmodel.EntryTypeDir {
			out.DirsSeen++
		} else {
			out.FilesSeen++
			out.BytesSeen += entry.Size
		}
	}
	if result.Truncated {
		out.LimitCause = "max_entries"
	}
	return out, nil
}

func (c *Client) Stat(ctx context.Context, req sandboxcontract.FileRequest) (*fsmodel.FileStat, error) {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	var info workspaceFileInfo
	if err := c.doSessionJSON(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/files:stat", query, nil, &info); err != nil {
		return nil, fsErrorFromExec(err, workspacePath)
	}
	stat := fileStatFromWorkspace(info, req.Path)
	return &stat, nil
}

func (c *Client) MkdirAll(ctx context.Context, req sandboxcontract.MkdirRequest) error {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	if req.Options.Parents {
		query.Set("parents", "true")
	}
	if err := c.doSessionJSON(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/dirs", query, nil, nil); err != nil {
		return fsErrorFromExec(err, workspacePath)
	}
	return nil
}

func (c *Client) Remove(ctx context.Context, req sandboxcontract.RemoveRequest) error {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	query.Set("recursive", strconv.FormatBool(req.Options.Recursive))
	if err := c.doSessionJSON(ctx, http.MethodDelete, "/v1/sessions/"+url.PathEscape(sessionID)+"/files", query, nil, nil); err != nil {
		return fsErrorFromExec(err, workspacePath)
	}
	return nil
}

func (c *Client) doSessionJSON(ctx context.Context, method, apiPath string, query url.Values, body io.Reader, out any) error {
	resp, err := c.doRaw(ctx, method, apiPath, query, "application/json", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("解析sandbox session文件响应失败: %w", err))
	}
	return nil
}

func (c *Client) doRaw(ctx context.Context, method, apiPath string, query url.Values, contentType string, body io.Reader) (*http.Response, error) {
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: strings.TrimRight(c.baseURL.Path, "/") + apiPath, RawQuery: query.Encode()})
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("创建sandbox http请求失败: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("调用sandbox http失败: %w", err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, mapHTTPError(resp)
	}
	return resp, nil
}

func sessionFileTarget(workspace sandboxcontract.WorkspaceRef, resolved fsmodel.ResolvedPath) (string, string, error) {
	sessionID := strings.TrimSpace(workspace.Metadata["session_id"])
	if sessionID == "" {
		sessionID = strings.TrimSpace(workspace.ID)
	}
	if sessionID == "" {
		return "", "", fscontract.NewError(fscontract.ErrCodeInvalidInput, "", fmt.Errorf("sandbox session id不能为空"))
	}
	workspacePath, err := cleanWorkspacePath(firstNonEmpty(resolved.WorkspaceRel, resolved.BackendPath, resolved.DisplayPath, resolved.RawPath))
	if err != nil {
		return "", "", err
	}
	return sessionID, workspacePath, nil
}

func cleanWorkspacePath(raw string) (string, error) {
	p := strings.TrimSpace(filepath.ToSlash(raw))
	if p == "" || p == "." {
		return ".", nil
	}
	if strings.HasPrefix(p, "/workspace/") {
		p = strings.TrimPrefix(p, "/workspace/")
	} else if p == "/workspace" {
		return ".", nil
	} else if strings.HasPrefix(p, "/") {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, fmt.Errorf("sandbox session文件路径必须是workspace相对路径或/workspace内路径"))
	}
	if strings.Contains(p, ":") {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, fmt.Errorf("sandbox session文件路径不能是宿主绝对路径"))
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return "", fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, fmt.Errorf("sandbox session文件路径不能包含.."))
		}
	}
	clean := path.Clean("/" + p)
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." {
		return ".", nil
	}
	return clean, nil
}
func dirEntriesFromWorkspace(in []workspaceFileInfo) []fsmodel.DirEntry {
	out := make([]fsmodel.DirEntry, 0, len(in))
	for _, info := range in {
		out = append(out, fsmodel.DirEntry{
			Name:       firstNonEmpty(info.Name, path.Base(info.Path)),
			Path:       info.Path,
			Type:       entryTypeFromKind(info.Kind),
			Size:       info.Size,
			ModifiedAt: info.ModTime,
		})
	}
	return out
}

func fileStatFromWorkspace(info workspaceFileInfo, resolved fsmodel.ResolvedPath) fsmodel.FileStat {
	return fsmodel.FileStat{
		Path:       resolved,
		Type:       entryTypeFromKind(info.Kind),
		Size:       info.Size,
		ModifiedAt: info.ModTime,
	}
}

func entryTypeFromKind(kind string) fsmodel.EntryType {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "file":
		return fsmodel.EntryTypeFile
	case "dir", "directory":
		return fsmodel.EntryTypeDir
	case "symlink", "link":
		return fsmodel.EntryTypeSymlink
	default:
		return fsmodel.EntryTypeOther
	}
}

func fsErrorFromExec(err error, workspacePath string) error {
	if err == nil {
		return nil
	}
	if isNotFoundExecError(err) {
		return fscontract.NewError(fscontract.ErrCodeNotFound, workspacePath, err)
	}
	switch execcontract.CodeOf(err) {
	case execcontract.ErrCodePermissionDenied:
		return fscontract.NewError(fscontract.ErrCodePermissionDenied, workspacePath, err)
	case execcontract.ErrCodeInvalidInput:
		return fscontract.NewError(fscontract.ErrCodeInvalidPath, workspacePath, err)
	default:
		return fscontract.NewError(fscontract.ErrCodeInvalidInput, workspacePath, err)
	}
}

func isNotFoundExecError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such") ||
		strings.Contains(msg, "could not find the file") ||
		strings.Contains(msg, "cannot find the file") ||
		strings.Contains(msg, "404")
}
