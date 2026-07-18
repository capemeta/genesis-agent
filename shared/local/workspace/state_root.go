package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// StateRootResolver 是由产品显式配置的本地状态根解析器。
type StateRootResolver struct {
	ProjectStateDir string
	UserStateDir    string
}

// ResolveStateRoot 在 Run 创建时解析固定 state root，绝不读取进程 cwd。
func (r StateRootResolver) ResolveStateRoot(ctx context.Context, req workcontract.StateRootRequest) (workmodel.StateRoot, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.StateRoot{}, err
	}
	var root string
	if req.Mode == execmodel.WorkspaceModeProject && req.ProjectRoot != nil && strings.TrimSpace(r.ProjectStateDir) != "" {
		root = r.ProjectStateDir
	} else {
		root = r.UserStateDir
	}
	if strings.TrimSpace(root) == "" {
		return workmodel.StateRoot{}, workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, fmt.Errorf("产品未配置 state root"))
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return workmodel.StateRoot{}, workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, err)
	}
	return workmodel.StateRoot{ID: "local:" + filepath.ToSlash(abs), Authority: "host", Path: abs, Scope: req.Scope}, nil
}
