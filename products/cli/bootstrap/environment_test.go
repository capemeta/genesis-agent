package bootstrap

import (
	"context"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	clisandbox "genesis-agent/products/cli/internal/sandbox"
)

type fixedShellCapabilities struct {
	capabilities execmodel.ShellCapabilities
}

func (f fixedShellCapabilities) ShellCapabilities(context.Context) execmodel.ShellCapabilities {
	return f.capabilities
}

func TestCLIEnvironmentContextUsesDeclaredLocalCapabilities(t *testing.T) {
	environment := cliEnvironmentContext(context.Background(), `D:\workspace`, execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled}, fixedShellCapabilities{
		capabilities: execmodel.ShellCapabilities{
			Default:   execmodel.ShellInfo{Kind: execmodel.ShellPowerShell, Path: `C:\pwsh.exe`},
			Supported: []execmodel.ShellInfo{{Kind: execmodel.ShellPowerShell}, {Kind: execmodel.ShellCmd}},
		},
	})
	if environment.Cwd != `D:\workspace` || environment.DefaultShell != "powershell" || len(environment.SupportedShells) != 2 {
		t.Fatalf("environment = %+v", environment)
	}
}

func TestCLIEnvironmentContextDoesNotLeakHostIntoRemoteSandbox(t *testing.T) {
	environment := cliEnvironmentContext(context.Background(), `D:\host\workspace`, execmodel.SandboxProfile{
		Mode:     execmodel.SandboxRequired,
		Provider: clisandbox.ProviderGenesisSandbox,
	}, nil)
	if environment.OS != "" || environment.Cwd != "/workspace" || environment.DefaultShell != "" || len(environment.SupportedShells) != 0 {
		t.Fatalf("environment = %+v", environment)
	}
}
