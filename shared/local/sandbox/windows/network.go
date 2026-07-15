//go:build windows

package windowssandbox

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrFirewallUnsupported = errors.New("firewall user-based rules are not supported on this Windows edition (Home Edition)")
var ErrFirewallServiceDisabled = errors.New("Windows Firewall service is disabled or stopped")

// GetSandboxUsername returns the name of the offline local user account used for sandboxing
func GetSandboxUsername() string {
	return "GenesisSandboxUser"
}

// IsWindowsNetworkSetupReady checks if the network sandboxing setup has been completed successfully
func IsWindowsNetworkSetupReady() bool {
	return IsWindowsSetupReady() && isNetworkReadyInReadiness()
}

// isFirewallServiceRunning checks if the Windows Firewall service is running
func isFirewallServiceRunning() bool {
	out, err := exec.Command("sc", "query", "mpssvc").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "RUNNING")
}

// SetupNetworkFirewallRule creates the persistent outbound block firewall rule for GenesisSandboxUser
func SetupNetworkFirewallRule() error {
	// First check if the GenesisSandboxUser exists
	cmdCheck := exec.Command("net", "user", GetSandboxUsername())
	if err := cmdCheck.Run(); err != nil {
		return fmt.Errorf("local user account %s does not exist, run sandbox setup first", GetSandboxUsername())
	}

	// Check if Windows Firewall service is running
	if !isFirewallServiceRunning() {
		return ErrFirewallServiceDisabled
	}

	// 1. Remove old firewall rule if present
	cmdRemove := exec.Command("powershell", "-Command", "Remove-NetFirewallRule -Name 'GenesisSandboxNetworkBlock' -ErrorAction SilentlyContinue")
	_ = cmdRemove.Run()

	// 2. Add new firewall rule to block all outbound connections for GenesisSandboxUser
	cmdAdd := exec.Command("powershell", "-Command",
		"New-NetFirewallRule -Name 'GenesisSandboxNetworkBlock' -DisplayName 'Genesis Sandbox Network Block' -Direction Outbound -Action Block -LocalUser 'GenesisSandboxUser' -Enabled True")
	if output, err := cmdAdd.CombinedOutput(); err != nil {
		outStr := string(output)
		if strings.Contains(outStr, "0x80070057") || strings.Contains(outStr, "CimException") || strings.Contains(outStr, "无法为用户名授权本地组") || strings.Contains(outStr, "ûȨ") {
			return ErrFirewallUnsupported
		}
		return fmt.Errorf("failed to create firewall rule (ensure you run as Administrator): %v (output: %s)", err, outStr)
	}

	return nil
}
