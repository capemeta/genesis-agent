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
	"path/filepath"
	"strings"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	"genesis-agent/internal/runtime/progress"
)

func (c *Client) RunCommand(ctx context.Context, req sandboxcontract.CommandRequest) (result *execmodel.Result, err error) {
	if c == nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("sandbox http client未初始化"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Client.Timeout 恒为 0；整条 Job 链路超时只认 RunOptions.Timeout / 调用方 ctx。
	timeout := requestTimeout(req)
	callCtx := ctx
	cancelCall := func() {}
	if timeout > 0 {
		callCtx, cancelCall = context.WithTimeout(ctx, timeout)
	}
	defer cancelCall()

	progress.Emit(callCtx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseStart, Component: "genesis-sandbox", Name: string(req.Sandbox.RuntimeProfile), Summary: "申请 sandbox 租约"})
	lease, err := c.leaseSandbox(callCtx, req)
	if err != nil {
		progress.Emit(callCtx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelError, Component: "genesis-sandbox", Summary: "sandbox 租约申请失败", Detail: err.Error()})
		return nil, err
	}
	progress.Emit(callCtx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseComplete, Component: "genesis-sandbox", Name: lease.SandboxID, Summary: "sandbox 租约已就绪"})
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
	progress.Emit(callCtx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseStart, Component: "genesis-sandbox", Name: lease.SandboxID, Summary: "提交 sandbox job"})
	if err := c.doJSON(callCtx, http.MethodPost, "/v1/jobs", &body, &job); err != nil {
		progress.Emit(callCtx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelError, Component: "genesis-sandbox", Name: lease.SandboxID, Summary: "sandbox job 提交失败", Detail: err.Error()})
		return nil, err
	}
	progress.Emit(callCtx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseProgress, Component: "genesis-sandbox", Name: job.JobID, Summary: "等待 sandbox job 完成"})
	finalJob, err := c.finalJob(callCtx, job, timeout)
	if err != nil {
		progress.Emit(callCtx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseError, Level: progress.LevelError, Component: "genesis-sandbox", Name: job.JobID, Summary: "sandbox job 执行失败", Detail: err.Error()})
		return nil, err
	}
	progress.Emit(callCtx, progress.Event{Kind: progress.KindSandbox, Phase: progress.PhaseComplete, Component: "genesis-sandbox", Name: finalJob.JobID, Summary: "sandbox job 已完成"})
	return c.resultFromJob(callCtx, req, *finalJob), nil
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
