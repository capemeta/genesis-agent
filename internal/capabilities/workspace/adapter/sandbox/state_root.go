package sandbox

import (
	"context"
	"fmt"
	"strings"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// StateRootResolver 返回产品装配时注入的远程状态根引用。
type StateRootResolver struct{ Root workmodel.StateRoot }

func (r StateRootResolver) ResolveStateRoot(ctx context.Context, req workcontract.StateRootRequest) (workmodel.StateRoot, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.StateRoot{}, err
	}
	if strings.TrimSpace(r.Root.ID) == "" || strings.TrimSpace(r.Root.Authority) == "" {
		return workmodel.StateRoot{}, fmt.Errorf("远程 state root 未配置")
	}
	root := r.Root
	root.Scope = req.Scope
	return root, nil
}
