//go:build windows

package execution

import (
	"context"
	"os/exec"
	"strings"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	windowssandbox "genesis-agent/shared/local/sandbox/windows"
)

func (r *Runner) RunArgvProcessConstrained(ctx context.Context, command ArgvCommand, opts execcontract.RunOptions) (*execmodel.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	display := command.DisplayCommand
	if display == "" {
		display = strings.Join(command.Argv, " ")
	}
	factory := func(factoryCtx context.Context, argv []string) (*exec.Cmd, afterStartFunc, func(), error) {
		cmd := newCommandContext(factoryCtx, argv)
		afterStart, cleanup, err := windowssandbox.PrepareRestrictedCommand(cmd)
		if err != nil {
			return nil, nil, nil, err
		}
		return cmd, afterStart, cleanup, nil
	}
	return r.runArgvWithFactory(ctx, command.Argv, execResultMeta{Command: display, Cwd: command.Cwd, Shell: command.Shell, Env: command.Env, Stdin: command.Stdin}, opts, factory)
}
