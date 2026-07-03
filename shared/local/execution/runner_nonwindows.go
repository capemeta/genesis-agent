//go:build !windows

package execution

import (
	"context"
	"fmt"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func (r *Runner) RunArgvProcessConstrained(ctx context.Context, command ArgvCommand, opts execcontract.RunOptions) (*execmodel.Result, error) {
	return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("Windows process constrained sandbox仅支持Windows"))
}
