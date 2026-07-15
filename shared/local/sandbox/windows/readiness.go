//go:build windows

package windowssandbox

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type readinessData struct {
	Ready               bool `json:"ready"`
	Version             int  `json:"version"`
	NetworkReady        bool `json:"network_ready,omitempty"`
	FirewallUnsupported bool `json:"firewall_unsupported,omitempty"`
}

// IsElevated checks if the current process is running with Administrator privileges
func IsElevated() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	var currentToken windows.Token
	err = windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &currentToken)
	if err != nil {
		return false
	}
	defer currentToken.Close()

	member, err := currentToken.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

var sandboxDirOverride string

// SetSandboxDirOverride sets an override for the sandbox configuration directory.
// This is critical when running under UAC elevation to allow writing to the real user's AppData.
func SetSandboxDirOverride(dir string) {
	sandboxDirOverride = dir
}

// sandboxDir returns the platform-specific sandbox directory path
func sandboxDir() string {
	if sandboxDirOverride != "" {
		return sandboxDirOverride
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		return filepath.Join(localAppData, "genesis-agent", "sandbox")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".genesis-agent", "sandbox")
}

// IsWindowsSetupReady checks if the sandbox setup readiness file exists and is valid
func IsWindowsSetupReady() bool {
	readinessPath := filepath.Join(sandboxDir(), "readiness.json")
	if _, err := os.Stat(readinessPath); err != nil {
		return false
	}
	data, err := os.ReadFile(readinessPath)
	if err != nil {
		return false
	}
	var r readinessData
	if err := json.Unmarshal(data, &r); err != nil {
		return false
	}
	return r.Ready && r.Version >= 1
}

func isNetworkReadyInReadiness() bool {
	readinessPath := filepath.Join(sandboxDir(), "readiness.json")
	data, err := os.ReadFile(readinessPath)
	if err != nil {
		return false
	}
	var r readinessData
	if err := json.Unmarshal(data, &r); err != nil {
		return false
	}
	return r.NetworkReady
}

// RunWindowsSetup creates the readiness file without network setup
func RunWindowsSetup() error {
	return RunWindowsSetupWithFlags(false)
}

// RunWindowsSetupWithFlags creates the readiness file, and optionally creates the GenesisSandboxUser and network firewall rules
func RunWindowsSetupWithFlags(setupNetwork bool) (err error) {
	cwd, _ := os.Getwd()
	defer func() {
		result := ElevationResult{Success: err == nil}
		logDir := filepath.Join(cwd, ".genesis", "logs")
		setupErrLog := filepath.Join(logDir, "sandbox_setup_error.log")
		if err != nil {
			result.Error = err.Error()
			_ = os.MkdirAll(logDir, 0755)
			_ = os.WriteFile(setupErrLog, []byte(err.Error()), 0644)
		} else {
			_ = os.Remove(setupErrLog)
		}
		WriteElevationResult(cwd, result)
	}()

	dir := sandboxDir()
	if err = os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create sandbox dir: %w", err)
	}

	networkReady := false
	firewallUnsupported := false
	if setupNetwork {
		fmt.Printf("开始配置 Windows 本地沙箱隔离账户与网络规则...\n")
		// 1. Generate random password
		password := generateRandomPassword()

		// 2. Create the GenesisSandboxUser account
		username := GetSandboxUsername()
		// Try to add the user
		cmdAdd := exec.Command("net", "user", username, password, "/add", "/active:yes", "/passwordchg:no")
		if err := cmdAdd.Run(); err != nil {
			// If it fails, the user might already exist. Let's try to update its password.
			cmdUpdate := exec.Command("net", "user", username, password)
			if err := cmdUpdate.Run(); err != nil {
				return fmt.Errorf("failed to create or update local user %s: %w (请确保使用管理员特权终端运行)", username, err)
			}
			fmt.Printf("本地用户账户 %s 已存在，已更新其访问密码。\n", username)
		} else {
			fmt.Printf("成功创建本地隔离账户：%s\n", username)
		}

		// 3. Add user to Users group
		cmdGroup := exec.Command("net", "localgroup", "Users", username, "/add")
		_ = cmdGroup.Run()

		// 4. Save password securely via DPAPI
		if err := WriteSecret(password); err != nil {
			return fmt.Errorf("failed to save sandbox password: %w", err)
		}
		fmt.Printf("已通过 Windows DPAPI 加密并安全存储隔离密码。\n")

		// 5. Install the WFP firewall rules
		if err := SetupNetworkFirewallRule(); err != nil {
			if errors.Is(err, ErrFirewallUnsupported) {
				fmt.Printf("[警告] 当前 Windows 版本（家庭版）不支持基于用户的防火墙规则。将跳过防火墙网络隔离，但保留文件与 Token 隔离环境。\n")
				firewallUnsupported = true
				networkReady = true
			} else if errors.Is(err, ErrFirewallServiceDisabled) {
				fmt.Printf("[警告] Windows 防火墙服务已禁用（mpssvc 未运行）。将跳过防火墙网络隔离，但保留文件与 Token 隔离环境。\n建议：如需网络隔离沙箱，请重新启用 Windows 防火墙服务后再初始化。\n")
				firewallUnsupported = true
				networkReady = true
			} else {
				return fmt.Errorf("failed to configure network firewall: %w", err)
			}
		} else {
			fmt.Printf("出站阻断网络防火墙规则创建成功。\n")
			networkReady = true
		}
	}

	readinessPath := filepath.Join(dir, "readiness.json")
	var oldReadiness readinessData
	if data, err := os.ReadFile(readinessPath); err == nil {
		_ = json.Unmarshal(data, &oldReadiness)
	}

	r := readinessData{
		Ready:               true,
		Version:             1,
		NetworkReady:        oldReadiness.NetworkReady || networkReady,
		FirewallUnsupported: oldReadiness.FirewallUnsupported || firewallUnsupported,
	}
	
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("failed to marshal readiness data: %w", err)
	}
	if err := os.WriteFile(readinessPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write readiness file: %w", err)
	}
	fmt.Printf("Windows 本地平台沙箱初始化成功！\nReadiness 标记已写入: %s\n", readinessPath)
	return nil
}

// IsFirewallUnsupported checks if the firewall rule setup is unsupported on this Windows edition
func IsFirewallUnsupported() bool {
	readinessPath := filepath.Join(sandboxDir(), "readiness.json")
	data, err := os.ReadFile(readinessPath)
	if err != nil {
		return false
	}
	var r readinessData
	if err := json.Unmarshal(data, &r); err != nil {
		return false
	}
	return r.FirewallUnsupported
}

func generateRandomPassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%"
	b := make([]byte, 14)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}
