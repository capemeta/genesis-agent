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
	"os"
	"path/filepath"
	"regexp"
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
	defaultRenewInterval    = 30 * time.Second
	defaultRenewExtend      = 5 * time.Minute
	defaultPollStart        = 500 * time.Millisecond
	defaultPollMax          = 3 * time.Second
	defaultCloseTimeout     = 30 * time.Second
	defaultRenewTimeout     = 15 * time.Second
)

// Config 描述 genesis-sandbox HTTP client 配置。
type Config struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	Client  *http.Client
	// LocalArtifactRoot 为空时只返回远程 artifact 元数据；非空时自动下载到该目录。
	LocalArtifactRoot string
	RenewInterval     time.Duration
	RenewExtend       time.Duration
	PollStart         time.Duration
	PollMax           time.Duration
	CloseTimeout      time.Duration
}

// Client 实现 sandbox CommandClient。
type Client struct {
	baseURL       *url.URL
	apiKey        string
	httpClient    *http.Client
	artifactRoot  string
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
		artifactRoot:  strings.TrimSpace(cfg.LocalArtifactRoot),
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
	go c.renewLoop(renewCtx, lease.SandboxID)
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
	req := sandboxcontract.CommandRequest{
		Workspace: opts.Workspace,
		Sandbox:   opts.Sandbox,
		Options:   opts.Options,
	}
	lease, err := c.leaseSandbox(ctx, req)
	if err != nil {
		return nil, err
	}
	renewCtx, cancelRenew := context.WithCancel(context.Background())
	session := &Session{
		client:      c,
		lease:       lease,
		workspace:   opts.Workspace,
		sandbox:     opts.Sandbox,
		options:     opts.Options,
		cancelRenew: cancelRenew,
	}
	go c.renewLoop(renewCtx, lease.SandboxID)
	return session, nil
}

// Session 实现 sandbox 长会话端口。
type Session struct {
	client      *Client
	lease       *sandboxLease
	workspace   sandboxcontract.WorkspaceRef
	sandbox     execmodel.SandboxProfile
	options     execcontract.RunOptions
	cancelRenew context.CancelFunc

	mu         sync.Mutex
	closed     bool
	stageJobID string
}

// StageInput 上传输入文件，返回可传给后续 Run 的 input artifact。
func (s *Session) StageInput(ctx context.Context, req sandboxcontract.StageInputRequest) (*sandboxcontract.StageInputResult, error) {
	if s == nil || s.client == nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session未初始化"))
	}
	jobID, err := s.ensureStageJob(ctx)
	if err != nil {
		return nil, err
	}
	artifact, err := s.client.uploadJobFile(ctx, jobID, req.Name, req.Content)
	if err != nil {
		return nil, err
	}
	return &sandboxcontract.StageInputResult{Artifact: inputArtifactFromRecord(*artifact)}, nil
}

// Run 在当前 sandbox lease 内执行一个 job，保留 /workspace 根目录状态。
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
	payload := execJobRequestFromCommand(req)
	payload.SandboxID = s.lease.SandboxID
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox session job请求失败: %w", err))
	}
	var job jobResult
	if err := s.client.doJSON(ctx, http.MethodPost, "/v1/jobs", &body, &job); err != nil {
		return nil, err
	}
	finalJob, err := s.client.finalJob(ctx, job, requestTimeout(req))
	if err != nil {
		return nil, err
	}
	return s.client.resultFromJob(ctx, req, *finalJob), nil
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
	if out.Workspace.WorkDir == "" && out.Workspace.InputDir == "" && out.Workspace.OutputDir == "" && out.Workspace.TmpDir == "" && out.Workspace.Mode == "" && out.Workspace.PathPolicy == "" {
		out.Workspace = base.Workspace
	}
	if len(out.InputArtifacts) == 0 {
		out.InputArtifacts = base.InputArtifacts
	}
	if out.ArtifactCollectionPolicy == "" {
		out.ArtifactCollectionPolicy = base.ArtifactCollectionPolicy
	}
	return out
}

// Close 停止续租并释放 sandbox；release 失败时 fallback destroy。
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
	s.mu.Unlock()
	if cancelRenew != nil {
		cancelRenew()
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), s.client.closeTimeout)
	defer closeCancel()
	return s.client.closeSandboxWithContext(closeCtx, s.lease.SandboxID)
}

func (s *Session) ensureStageJob(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox session已关闭"))
	}
	if s.stageJobID != "" {
		jobID := s.stageJobID
		s.mu.Unlock()
		return jobID, nil
	}
	s.mu.Unlock()

	req := sandboxcontract.CommandRequest{
		Workspace: s.workspace,
		Command:   execmodel.Command{Command: "true", Shell: execmodel.ShellSh},
		Sandbox:   s.sandbox,
		Options:   s.options,
	}
	payload := execJobRequestFromCommand(req)
	payload.SandboxID = s.lease.SandboxID
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return "", execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox staging job请求失败: %w", err))
	}
	var job jobResult
	if err := s.client.doJSON(ctx, http.MethodPost, "/v1/jobs", &body, &job); err != nil {
		return "", err
	}
	finalJob, err := s.client.finalJob(ctx, job, requestTimeout(req))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(finalJob.JobID) == "" {
		return "", execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("sandbox staging job缺少job_id"))
	}
	s.mu.Lock()
	if s.stageJobID == "" {
		s.stageJobID = finalJob.JobID
	}
	jobID := s.stageJobID
	s.mu.Unlock()
	return jobID, nil
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
		Artifacts:       c.artifactsFromJob(job.OutputArtifacts),
	}
	if c.artifactRoot != "" && len(result.Artifacts) > 0 {
		if warnings := c.materializeArtifacts(ctx, result.Artifacts); len(warnings) > 0 {
			result.Warnings = append(result.Warnings, warnings...)
		}
	}
	if job.Status == "timed_out" || job.ErrorCode == "EXEC_TIMEOUT" {
		result.TimedOut = true
	}
	if req.Options.ArtifactCollectionPolicy == execmodel.ArtifactCollectionOutputOnly && job.ExitCode == 0 && len(result.Artifacts) == 0 {
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

func (c *Client) renewLoop(ctx context.Context, sandboxID string) {
	ticker := time.NewTicker(c.renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(context.Background(), defaultRenewTimeout)
			_ = c.renewSandbox(renewCtx, sandboxID)
			cancel()
		}
	}
}

func (c *Client) renewSandbox(ctx context.Context, sandboxID string) error {
	payload := map[string]int{"extend_seconds": int(c.renewExtend.Seconds())}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("编码sandbox renew请求失败: %w", err))
	}
	var lease sandboxLease
	return c.doJSON(ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(sandboxID)+"/renew", &body, &lease)
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

func (c *Client) materializeArtifacts(ctx context.Context, artifacts []execmodel.Artifact) []string {
	warnings := make([]string, 0)
	for i := range artifacts {
		if artifacts[i].ID == "" {
			continue
		}
		progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseStart, Component: "genesis-sandbox", Name: artifacts[i].Name, Summary: "下载 sandbox artifact"})
		data, err := c.DownloadArtifact(ctx, artifacts[i].ID)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("artifact %s 下载失败: %v", artifacts[i].ID, err))
			progress.Emit(ctx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelWarn, Component: "genesis-sandbox", Name: artifacts[i].Name, Summary: "artifact 下载失败", Detail: err.Error()})
			continue
		}
		localPath, err := c.writeArtifact(artifacts[i], data)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("artifact %s 写入本地失败: %v", artifacts[i].ID, err))
			progress.Emit(ctx, progress.Event{Kind: progress.KindFile, Phase: progress.PhaseError, Level: progress.LevelWarn, Component: "artifact", Name: artifacts[i].Name, Summary: "artifact 写入本地失败", Detail: err.Error()})
			continue
		}
		artifacts[i].LocalPath = localPath
		progress.Emit(ctx, progress.Event{Kind: progress.KindFile, Phase: progress.PhaseComplete, Component: "artifact", Name: artifacts[i].Name, Summary: "artifact 已写入本地", Detail: localPath})
	}
	return warnings
}

func (c *Client) writeArtifact(artifact execmodel.Artifact, data []byte) (string, error) {
	root, err := filepath.Abs(c.artifactRoot)
	if err != nil {
		return "", err
	}
	group := firstNonEmpty(artifact.WorkspaceID, artifact.JobID, "default")
	dir := filepath.Join(root, sanitizePathPart(group))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := sanitizeFilename(firstNonEmpty(artifact.Name, artifact.ID, "artifact.bin"))
	target := filepath.Join(dir, name)
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("artifact路径越界: %s", target)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", err
	}
	return target, nil
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
	inputArtifactIDs := make([]string, 0, len(req.Options.InputArtifacts))
	for _, artifact := range req.Options.InputArtifacts {
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
	workspace := req.Options.Workspace
	if strings.TrimSpace(workspace.WorkDir) == "" {
		workspace.WorkDir = "/workspace"
	}
	if strings.TrimSpace(workspace.InputDir) == "" {
		workspace.InputDir = strings.TrimRight(workspace.WorkDir, "/") + "/input"
	}
	if strings.TrimSpace(workspace.OutputDir) == "" {
		workspace.OutputDir = strings.TrimRight(workspace.WorkDir, "/") + "/output"
	}
	if strings.TrimSpace(workspace.TmpDir) == "" {
		workspace.TmpDir = strings.TrimRight(workspace.WorkDir, "/") + "/tmp"
	}
	if workspace.Mode == "" {
		workspace.Mode = execmodel.WorkspaceModeSandboxSess
	}
	if workspace.PathPolicy == "" {
		workspace.PathPolicy = execmodel.PathPolicyStrictWorkspace
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

func inputArtifactFromRecord(record artifactRecord) execmodel.InputArtifactRef {
	return execmodel.InputArtifactRef{
		ID:          record.ArtifactID,
		Name:        record.Name,
		WorkspaceID: record.WorkspaceID,
		JobID:       record.JobID,
		Size:        record.Size,
		SHA256:      record.SHA256,
		MIME:        record.MIME,
	}
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

func (c *Client) artifactsFromJob(records []artifactRecord) []execmodel.Artifact {
	if len(records) == 0 {
		return nil
	}
	out := make([]execmodel.Artifact, 0, len(records))
	for _, record := range records {
		remoteURL := ""
		if record.ArtifactID != "" {
			remoteURL = c.baseURL.ResolveReference(&url.URL{Path: strings.TrimRight(c.baseURL.Path, "/") + "/v1/artifacts/" + url.PathEscape(record.ArtifactID)}).String()
		}
		out = append(out, execmodel.Artifact{
			ID:          record.ArtifactID,
			WorkspaceID: record.WorkspaceID,
			JobID:       record.JobID,
			Name:        record.Name,
			Size:        record.Size,
			SHA256:      record.SHA256,
			MIME:        record.MIME,
			RemoteURL:   remoteURL,
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

func unsupportedFileSystem(err string) error {
	return fscontract.NewError(fscontract.ErrCodeInvalidInput, "", fmt.Errorf("genesis-sandbox HTTP workspace file API尚未暴露: %s", err))
}

func (c *Client) ReadFile(context.Context, sandboxcontract.FileRequest, fscontract.ReadOptions) ([]byte, error) {
	return nil, unsupportedFileSystem("read_file")
}

func (c *Client) WriteFile(context.Context, sandboxcontract.WriteFileRequest) error {
	return unsupportedFileSystem("write_file")
}

func (c *Client) ListDir(context.Context, sandboxcontract.ListDirRequest) ([]fsmodel.DirEntry, error) {
	return nil, unsupportedFileSystem("list_dir")
}

func (c *Client) Walk(context.Context, sandboxcontract.WalkRequest) (*fsmodel.WalkOutcome, error) {
	return nil, unsupportedFileSystem("walk")
}

func (c *Client) Stat(context.Context, sandboxcontract.FileRequest) (*fsmodel.FileStat, error) {
	return nil, unsupportedFileSystem("stat")
}

func (c *Client) MkdirAll(context.Context, sandboxcontract.MkdirRequest) error {
	return unsupportedFileSystem("mkdir")
}

func (c *Client) Remove(context.Context, sandboxcontract.RemoveRequest) error {
	return unsupportedFileSystem("remove")
}
