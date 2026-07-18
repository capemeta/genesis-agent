// Package workspace 提供 CLI/Desktop 共享的本地工作空间适配。
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

// Provisioner 在显式 state root 下创建本地 execution 目录。
type Provisioner struct{}

// NewProvisioner 创建本地工作空间 provisioner。
func NewProvisioner() *Provisioner { return &Provisioner{} }

// Prepare 实现 workspace Provisioner。
func (p *Provisioner) Prepare(ctx context.Context, req workcontract.PrepareRequest) (workcontract.PreparedExecution, error) {
	if err := ctx.Err(); err != nil {
		return workcontract.PreparedExecution{}, err
	}
	if err := req.Binding.Validate(); err != nil {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	backend := req.Backend
	if backend.Kind == "" {
		backend = execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Provider: "local-host", Authority: "host"}
	}
	if err := backend.Validate(); err != nil || (backend.Kind != execmodel.BackendKindHost && backend.Kind != execmodel.BackendKindLocalSandbox) || backend.Authority != "host" {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeResourceBackendMismatch, fmt.Errorf("本地 provisioner backend 无效: %v", err))
	}
	root := strings.TrimSpace(req.StateRoot.Path)
	if root == "" {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, fmt.Errorf("本地 state root 缺少物理路径"))
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, err)
	}
	workspace := execmodel.ExecutionWorkspace{Metadata: map[string]string{"state_root_id": req.StateRoot.ID, "run_id": req.Binding.Owner.RunID, "execution_binding_id": req.Binding.ID, "os": runtime.GOOS}}
	runRoot := filepath.Join(root, "runtime", "runs", safeID(req.Binding.Owner.RunID))
	if req.Binding.Mode == execmodel.WorkspaceModeProject {
		projectDir := strings.TrimSpace(req.ProjectDir)
		if projectDir == "" {
			return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("project workspace 缺少已授权项目根"))
		}
		workspace.WorkDir, err = filepath.Abs(projectDir)
		if err != nil {
			return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
		}
		workspace.InputDir = filepath.Join(runRoot, "input", safeID(req.Binding.ID))
		workspace.OutputDir = filepath.Join(runRoot, "output", safeID(req.Binding.ID))
		workspace.TmpDir = filepath.Join(runRoot, "tmp", safeID(req.Binding.ID))
	} else {
		workspace.WorkDir = filepath.Join(runRoot, "work", safeID(req.Binding.ID))
		workspace.InputDir = filepath.Join(runRoot, "input", safeID(req.Binding.ID))
		workspace.OutputDir = filepath.Join(runRoot, "output", safeID(req.Binding.ID))
		workspace.TmpDir = filepath.Join(runRoot, "tmp", safeID(req.Binding.ID))
	}
	workspace.SkillDir = strings.TrimSpace(req.SkillDir)
	// 预先创建顶层 4 个根目录（work, input, output, tmp）以及 WorkDir，具体的 binding 子目录延迟按需创建
	topDirs := uniqueDirs(
		filepath.Join(runRoot, "work"),
		filepath.Join(runRoot, "input"),
		filepath.Join(runRoot, "output"),
		filepath.Join(runRoot, "tmp"),
		workspace.WorkDir,
	)
	for _, dir := range topDirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("创建 %s 失败: %w", dir, err))
		}
	}
	if err := workspace.ValidateFor(req.Binding); err != nil {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	return workcontract.PreparedExecution{
		Binding:   req.Binding,
		Backend:   backend,
		Workspace: workspace,
	}, nil
}

func safeID(value string) string {
	value = strings.TrimSpace(value)
	return strings.NewReplacer(`/`, "_", `\`, "_", `:`, "_", `*`, "_", `?`, "_", `"`, "_", `<`, "_", `>`, "_", `|`, "_").Replace(value)
}

func uniqueDirs(values ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(value))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
