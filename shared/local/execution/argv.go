package execution

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// ArgvCommand 描述已经完成 shell/platform 转换后的结构化命令。
type ArgvCommand struct {
	Argv           []string
	Cwd            string
	Env            map[string]string
	Stdin          []byte
	DisplayCommand string
	Shell          execmodel.ShellKind
}

// RunArgv 执行结构化 argv。调用方必须保证 argv 已经完成 shell 选择和 sandbox 包裹。
func (r *Runner) RunArgv(ctx context.Context, command ArgvCommand, opts execcontract.RunOptions) (*execmodel.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(command.Argv) == 0 || command.Argv[0] == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("argv不能为空"))
	}
	display := command.DisplayCommand
	if display == "" {
		display = strings.Join(command.Argv, " ")
	}
	return r.runArgv(ctx, command.Argv, execResultMeta{Command: display, Cwd: command.Cwd, Shell: command.Shell, Env: command.Env, Stdin: command.Stdin}, opts)
}

func newCommandContext(ctx context.Context, argv []string) *exec.Cmd {
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

func defaultCommandFactory(ctx context.Context, argv []string) (*exec.Cmd, afterStartFunc, func(), error) {
	return newCommandContext(ctx, argv), nil, nil, nil
}

// ShellArgv 返回平台 shell 的结构化 argv，不执行命令。
func ShellArgv(shell execmodel.ShellKind, command string) ([]string, execmodel.ShellKind, error) {
	shell = normalizeShell(shell)
	switch shell {
	case execmodel.ShellCmd:
		return []string{windowsShell(), "/d", "/c", command}, shell, nil
	case execmodel.ShellPowerShell:
		return nil, shell, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("当前本地runner未启用PowerShell，请注入专用PowerShell runner"))
	case execmodel.ShellBash:
		return []string{"bash", "-lc", command}, shell, nil
	case execmodel.ShellSh:
		return []string{"sh", "-lc", command}, shell, nil
	case execmodel.ShellZsh:
		return []string{"zsh", "-lc", command}, shell, nil
	default:
		return nil, shell, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("未知shell类型: %s", shell))
	}
}
