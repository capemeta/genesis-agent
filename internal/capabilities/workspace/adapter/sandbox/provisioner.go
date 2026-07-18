// Package sandbox 提供远程 WorkspaceFS 的工作空间路径映射适配。
package sandbox

import (
	"context"
	"fmt"
	"path"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

// Provisioner 将 execution binding 映射到远程 /workspace namespace。
type Provisioner struct{}

// NewProvisioner 创建远程 provisioner。
func NewProvisioner() *Provisioner { return &Provisioner{} }

// Prepare 只生成远程路径映射，实际目录由 WorkspaceFS/session 创建。
func (p *Provisioner) Prepare(ctx context.Context, req workcontract.PrepareRequest) (workcontract.PreparedExecution, error) {
	if err := ctx.Err(); err != nil {
		return workcontract.PreparedExecution{}, err
	}
	if err := req.Binding.Validate(); err != nil {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	backend := req.Backend
	if backend.Kind == "" {
		backend = execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindRemote, Provider: "genesis-sandbox", Authority: "remote-executor"}
	}
	if err := backend.Validate(); err != nil || backend.Kind != execmodel.BackendKindRemote || backend.Authority != "remote-executor" {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeResourceBackendMismatch, fmt.Errorf("远程 provisioner backend 无效: %v", err))
	}
	id := remoteID(req.Binding.ID)
	if id == "" {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("binding id 无法映射"))
	}
	workspace := execmodel.ExecutionWorkspace{Metadata: map[string]string{"state_root_id": req.StateRoot.ID, "run_id": req.Binding.Owner.RunID, "execution_binding_id": req.Binding.ID}}
	if req.Binding.Mode == execmodel.WorkspaceModeProject {
		workspace.WorkDir = "/workspace/project"
		workspace.InputDir = path.Join("/workspace/input", id)
		workspace.OutputDir = path.Join("/workspace/output", id)
		workspace.TmpDir = path.Join("/workspace/tmp", id)
	} else {
		workspace.WorkDir = path.Join("/workspace/work", id)
		workspace.InputDir = path.Join("/workspace/input", id)
		workspace.OutputDir = path.Join("/workspace/output", id)
		workspace.TmpDir = path.Join("/workspace/tmp", id)
	}
	workspace.SkillDir = strings.TrimSpace(req.SkillDir)
	if err := workspace.ValidateFor(req.Binding); err != nil {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	return workcontract.PreparedExecution{
		Binding:   req.Binding,
		Backend:   backend,
		Workspace: workspace,
	}, nil
}

func remoteID(value string) string {
	value = strings.TrimSpace(value)
	return strings.NewReplacer(`/`, "_", `\`, "_", `:`, "_", `*`, "_", `?`, "_", `"`, "_", `<`, "_", `>`, "_", `|`, "_").Replace(value)
}
