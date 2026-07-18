package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"path"
	"sort"
	"strings"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxsession "genesis-agent/internal/capabilities/sandbox/session"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

const (
	maxSandboxOutputEntries = 1000
	maxSandboxOutputBytes   = int64(1024 * 1024 * 1024)
)

// SandboxCommandRunner 是 sandbox_exec 工具依赖的远程执行端口。
type SandboxCommandRunner interface {
	RunSandboxCommand(ctx context.Context, req SandboxCommandRequest) (*SandboxCommandResult, error)
}

// SandboxCommandRequest 是已经完成权限解析和输入快照后的远程命令请求。
type SandboxCommandRequest struct {
	Command   execmodel.Command
	Binding   execmodel.ExecutionBinding
	Workspace execmodel.ExecutionWorkspace
	Sandbox   execmodel.SandboxProfile
	Inputs    workmodel.InputManifest
	Timeout   time.Duration
}

// SandboxCommandResult 返回模型可见执行事实和已登记的稳定产物身份。
type SandboxCommandResult struct {
	Result          *execmodel.Result
	Workspace       execmodel.ExecutionWorkspace
	StagedInputs    []string
	Produced        []workmodel.ProducedResourceDescriptor
	StagingTimeMS   int64
	ExecutionTimeMS int64
}

// SandboxCommandService 编排通用远程命令、输入投影和 OUTPUT_DIR 产物登记。
type SandboxCommandService struct {
	sessions *sandboxsession.Manager
	inputs   workcontract.InputSnapshotReader
	produced workcontract.ProducedResourceRegistrar
}

// NewSandboxCommandService 创建通用远程命令服务。
func NewSandboxCommandService(sessions *sandboxsession.Manager, inputs workcontract.InputSnapshotReader, produced workcontract.ProducedResourceRegistrar) (*SandboxCommandService, error) {
	if sessions == nil || inputs == nil || produced == nil {
		return nil, fmt.Errorf("sandbox command service 缺少 sessions/inputs/produced registrar")
	}
	return &SandboxCommandService{sessions: sessions, inputs: inputs, produced: produced}, nil
}

// RunSandboxCommand 在同一逻辑 Session 中串行执行；只在命令前恢复失效容器，不重试业务命令。
func (s *SandboxCommandService) RunSandboxCommand(ctx context.Context, req SandboxCommandRequest) (*SandboxCommandResult, error) {
	if strings.TrimSpace(req.Command.Command) == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("sandbox command不能为空"))
	}
	if err := req.Binding.Validate(); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeExecutionBindingRequired, err)
	}
	if err := req.Workspace.ValidateFor(req.Binding); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeExecutionBindingConflict, err)
	}
	handle, err := s.sessions.Acquire(ctx, sandboxsession.AcquireRequest{
		RunID: req.Binding.Owner.RunID, Binding: req.Binding, Workspace: req.Workspace, Sandbox: req.Sandbox,
	})
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	sess := handle.Session()

	stagingStarted := time.Now()
	staged, err := stageSandboxInputs(ctx, s.inputs, req.Binding, req.Inputs, sess, req.Workspace.WorkDir)
	stagingMS := time.Since(stagingStarted).Milliseconds()
	if err != nil {
		return nil, err
	}
	cmd := req.Command
	cmd.Cwd = req.Workspace.WorkDir
	cmd.Env = sandboxWorkspaceEnv(cmd.Env, req.Workspace)
	executionStarted := time.Now()
	result, err := sess.Run(ctx, cmd, execcontract.RunOptions{
		Timeout: req.Timeout, Sandbox: req.Sandbox, Binding: req.Binding, Workspace: req.Workspace,
	})
	executionMS := time.Since(executionStarted).Milliseconds()
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("sandbox command返回空结果"))
	}
	result.Environment = execmodel.EnvironmentSandbox
	result.SandboxProvider = req.Sandbox.Provider
	out := &SandboxCommandResult{Result: result, Workspace: req.Workspace, StagedInputs: staged, StagingTimeMS: stagingMS, ExecutionTimeMS: executionMS}
	if result.ExitCode != 0 {
		return out, nil
	}

	after, err := snapshotSandboxFiles(ctx, sess, req.Workspace.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("执行后扫描 sandbox 输出目录: %w", err)
	}
	outputs := sandboxFilePaths(after)
	if len(outputs) == 0 {
		return out, nil
	}
	if err := handle.RefreshBinding(ctx, req.Binding); err != nil {
		return nil, err
	}
	expiresAt := sess.ExpiresAt()
	for _, rel := range outputs {
		fingerprint := after[rel]
		logical := "run:/work/" + req.Binding.ID + "/" + rel
		observed := workmodel.WorkspacePath(sandboxsession.RelativePath(req.Workspace.OutputDir, rel))
		descriptor, err := s.produced.RegisterProducedResource(ctx, workcontract.RegisterProducedResourceRequest{
			TenantID: req.Binding.Owner.TenantID, RunID: req.Binding.Owner.RunID, BindingID: req.Binding.ID,
			LogicalRef: logical, ObservedPath: observed, ObservedName: path.Base(rel),
			MediaType: mime.TypeByExtension(path.Ext(rel)), Size: fingerprint.Size,
			Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expiresAt,
		})
		if err != nil {
			return nil, fmt.Errorf("登记 sandbox 产物 %s: %w", rel, err)
		}
		out.Produced = append(out.Produced, descriptor)
	}
	return out, nil
}

func stageSandboxInputs(ctx context.Context, reader workcontract.InputSnapshotReader, binding execmodel.ExecutionBinding, manifest workmodel.InputManifest, sess *sandboxsession.Session, destRoot string) ([]string, error) {
	if len(manifest.Inputs) == 0 {
		return nil, nil
	}
	if manifest.RunID != binding.Owner.RunID || manifest.BindingID != binding.ID {
		return nil, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("InputManifest 与 sandbox ExecutionBinding 不匹配"))
	}
	staged := make([]string, 0, len(manifest.Inputs))
	for _, input := range manifest.Inputs {
		if err := input.Alias.Validate(); err != nil {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, err)
		}
		content, err := readVerifiedInput(ctx, reader, input)
		if err != nil {
			return nil, err
		}
		alias := string(input.Alias)
		if err := sess.WriteFile(ctx, sandboxsession.RelativePath(destRoot, alias), content, fscontract.WriteOptions{CreateParents: true, Overwrite: true, Atomic: true}); err != nil {
			return nil, fmt.Errorf("上传 sandbox 输入 %s: %w", alias, err)
		}
		staged = append(staged, alias)
	}
	return staged, nil
}

func readVerifiedInput(ctx context.Context, reader workcontract.InputSnapshotReader, input workmodel.InputRef) ([]byte, error) {
	stream, err := reader.OpenSnapshot(ctx, input.StagedPath)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	limited := &io.LimitedReader{R: stream, N: input.Size + 1}
	content, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != input.Size {
		return nil, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("输入快照 %s 大小已变化", input.ID))
	}
	digest := sha256.Sum256(content)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), input.SHA256) {
		return nil, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("输入快照 %s hash 已变化", input.ID))
	}
	return content, nil
}

type sandboxFileFingerprint struct {
	Size int64
}

func snapshotSandboxFiles(ctx context.Context, sess *sandboxsession.Session, root string) (map[string]sandboxFileFingerprint, error) {
	rootRel := sandboxsession.RelativePath(root, "")
	walk, err := sess.Walk(ctx, rootRel, fscontract.WalkOptions{
		MaxEntries: maxSandboxOutputEntries, MaxBytes: maxSandboxOutputBytes, FollowSymlinks: false,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]sandboxFileFingerprint)
	if walk == nil {
		return out, nil
	}
	if walk.Truncated {
		return nil, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("sandbox 输出扫描被截断: %s", walk.LimitCause))
	}
	if len(walk.Errors) > 0 {
		return nil, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("sandbox 输出扫描包含 %d 个错误", len(walk.Errors)))
	}
	prefix := strings.TrimRight(rootRel, "/") + "/"
	for _, entry := range walk.Entries {
		if entry.Type != fsmodel.EntryTypeFile {
			continue
		}
		entryPath := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(entry.Path), `\`, "/"), "/workspace/")
		if !strings.HasPrefix(entryPath, prefix) {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("sandbox Walk 返回输出目录外路径: %s", entry.Path))
		}
		rel := strings.TrimPrefix(entryPath, prefix)
		workspacePath := workmodel.WorkspacePath(rel)
		if err := workspacePath.Validate(); err != nil {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, err)
		}
		out[rel] = sandboxFileFingerprint{Size: entry.Size}
	}
	return out, nil
}

func sandboxFilePaths(files map[string]sandboxFileFingerprint) []string {
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	return paths
}

func sandboxWorkspaceEnv(user map[string]string, workspace execmodel.ExecutionWorkspace) map[string]string {
	env := make(map[string]string, len(user)+6)
	for key, value := range user {
		env[key] = value
	}
	env["WORK_DIR"] = workspace.WorkDir
	env["INPUT_DIR"] = workspace.InputDir
	env["OUTPUT_DIR"] = workspace.OutputDir
	env["TMPDIR"] = workspace.TmpDir
	env["TMP_DIR"] = workspace.TmpDir
	env["GENESIS_WORKSPACE"] = workspace.WorkDir
	return env
}

var _ SandboxCommandRunner = (*SandboxCommandService)(nil)
