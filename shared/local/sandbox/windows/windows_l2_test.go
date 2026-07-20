//go:build windows

package windowssandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestGetWorkspaceCapabilitySID(t *testing.T) {
	path1 := `C:\work\project-a`
	path2 := `C:\work\project-A` // spelling differences (case)
	path3 := `C:/work/project-a` // spelling differences (slash)
	path4 := `C:\work\project-b`

	sid1, err := GetWorkspaceCapabilitySID(path1)
	if err != nil {
		t.Fatalf("failed to get SID 1: %v", err)
	}
	if !strings.HasPrefix(sid1, "S-1-5-21-") {
		t.Errorf("expected S-1-5-21- prefix, got %q", sid1)
	}

	sid2, err := GetWorkspaceCapabilitySID(path2)
	if err != nil {
		t.Fatalf("failed to get SID 2: %v", err)
	}
	if sid1 != sid2 {
		t.Errorf("expected deterministic SID for same path with case diff: %q vs %q", sid1, sid2)
	}

	sid3, err := GetWorkspaceCapabilitySID(path3)
	if err != nil {
		t.Fatalf("failed to get SID 3: %v", err)
	}
	if sid1 != sid3 {
		t.Errorf("expected deterministic SID for same path with slash diff: %q vs %q", sid1, sid3)
	}

	sid4, err := GetWorkspaceCapabilitySID(path4)
	if err != nil {
		t.Fatalf("failed to get SID 4: %v", err)
	}
	if sid1 == sid4 {
		t.Errorf("expected different SIDs for different paths: %q vs %q", sid1, sid4)
	}
}

func TestReadinessAndSetup(t *testing.T) {
	oldDir := sandboxDirOverride
	SetSandboxDirOverride(t.TempDir())
	defer SetSandboxDirOverride(oldDir)
	t.Cleanup(func() {
		cwd, _ := os.Getwd()
		_ = os.RemoveAll(filepath.Join(cwd, ".genesis"))
	})

	// Backup existing readiness if any
	dir := sandboxDir()
	readinessPath := filepath.Join(dir, "readiness.json")
	var backupData []byte
	backupExists := false
	if _, err := os.Stat(readinessPath); err == nil {
		backupData, _ = os.ReadFile(readinessPath)
		backupExists = true
		_ = os.Remove(readinessPath)
	}

	defer func() {
		// Restore backup
		if backupExists {
			_ = os.MkdirAll(dir, 0755)
			_ = os.WriteFile(readinessPath, backupData, 0644)
		} else {
			_ = os.Remove(readinessPath)
		}
	}()

	// Verify not ready initially
	if IsWindowsSetupReady() {
		t.Error("expected setup to be not ready initially")
	}

	// Run setup
	err := RunWindowsSetup()
	if err != nil {
		t.Fatalf("RunWindowsSetup failed: %v", err)
	}

	// Verify ready
	if !IsWindowsSetupReady() {
		t.Error("expected setup to be ready after RunWindowsSetup")
	}
}

func TestL2SandboxEnforcement(t *testing.T) {
	tempRootDir, err := os.MkdirTemp("", "genesis-l2-test-*")
	if err != nil {
		t.Fatalf("failed to create temp root dir: %v", err)
	}
	defer os.RemoveAll(tempRootDir)

	workspacePath := filepath.Join(tempRootDir, "workspace")
	deniedPath := filepath.Join(tempRootDir, "denied")

	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	if err := os.MkdirAll(deniedPath, 0755); err != nil {
		t.Fatalf("failed to create denied folder: %v", err)
	}

	sid, err := GetWorkspaceCapabilitySID(workspacePath)
	if err != nil {
		t.Fatalf("failed to get SID: %v", err)
	}

	// Apply ACLs to workspacePath (grant modify)
	err = ApplyWorkspaceACLs(workspacePath, []string{workspacePath}, nil)
	if err != nil {
		t.Fatalf("failed to apply ACLs: %v", err)
	}

	// 1. Verify we CAN write inside the workspace under restricted token
	targetFileAllowed := filepath.Join(workspacePath, "allowed.txt")
	cmdAllowed := exec.Command("cmd.exe", "/c", "echo hello > "+targetFileAllowed)
	cmdAllowed.Env = append(os.Environ(), "GENESIS_SANDBOX_CAP_SIDS="+sid)

	afterStart, cleanup, err := PrepareRestrictedCommand(cmdAllowed)
	if err != nil {
		t.Fatalf("PrepareRestrictedCommand failed for allowed write: %v", err)
	}
	defer cleanup()

	if err := cmdAllowed.Start(); err != nil {
		t.Fatalf("cmdAllowed.Start failed: %v", err)
	}
	if err := afterStart(cmdAllowed); err != nil {
		t.Fatalf("afterStart failed: %v", err)
	}
	if err := cmdAllowed.Wait(); err != nil {
		t.Fatalf("cmdAllowed failed: %v", err)
	}

	if _, err := os.Stat(targetFileAllowed); err != nil {
		t.Errorf("expected allowed file to be created, but got err: %v", err)
	}

	// 2. Verify we CANNOT write inside the denied folder under restricted token
	targetFileDenied := filepath.Join(deniedPath, "denied.txt")
	cmdDenied := exec.Command("cmd.exe", "/c", "echo hello > "+targetFileDenied)
	cmdDenied.Env = append(os.Environ(), "GENESIS_SANDBOX_CAP_SIDS="+sid)

	afterStart2, cleanup2, err := PrepareRestrictedCommand(cmdDenied)
	if err != nil {
		t.Fatalf("PrepareRestrictedCommand failed for denied write: %v", err)
	}
	defer cleanup2()

	if err := cmdDenied.Start(); err != nil {
		t.Fatalf("cmdDenied.Start failed: %v", err)
	}
	if err := afterStart2(cmdDenied); err != nil {
		t.Fatalf("afterStart2 failed: %v", err)
	}
	
	// Wait should fail because cmd.exe will exit with error due to ACCESS_DENIED on redirection
	err = cmdDenied.Wait()
	if err == nil {
		t.Error("expected cmdDenied to fail writing to denied directory, but it succeeded")
	}
	
	if _, err := os.Stat(targetFileDenied); err == nil {
		t.Error("expected denied file NOT to be created, but it exists")
	}
}

func TestDPAPISecrets(t *testing.T) {
	oldDir := sandboxDirOverride
	SetSandboxDirOverride(t.TempDir())
	defer SetSandboxDirOverride(oldDir)

	testPassword := "GenesisSuperSecure123!@#"
	
	// Write secret
	err := WriteSecret(testPassword)
	if err != nil {
		t.Fatalf("WriteSecret failed: %v", err)
	}
	
	// Read secret and verify
	decrypted, err := ReadSecret()
	if err != nil {
		t.Fatalf("ReadSecret failed: %v", err)
	}
	
	if decrypted != testPassword {
		t.Errorf("decrypted password = %q, want %q", decrypted, testPassword)
	}
}

func TestLocalUserNetworkIsolation(t *testing.T) {
	if !IsWindowsNetworkSetupReady() {
		t.Skip("Windows local platform network sandbox is not set up (GenesisSandboxUser or firewall rules missing)")
	}

	// 验证本地沙箱账户凭证是否有效（可能由于之前未隔离的测试被覆盖或损坏），若无效则优雅跳过测试并提示修复
	password, err := ReadSecret()
	if err != nil {
		t.Skipf("无法读取沙箱密码 (请运行 windows-setup --network 修复): %v", err)
	}
	token, err := logonUser(GetSandboxUsername(), ".", password)
	if err != nil {
		t.Skipf("本地沙箱账户登录失败 (凭证可能已损坏，请运行 windows-setup --network 重新初始化): %v", err)
	}
	_ = windows.CloseHandle(windows.Handle(token))

	// 1. Test loopback connection (ping localhost) -> should SUCCEED
	cmdLocal := exec.Command("ping", "127.0.0.1", "-n", "1")
	cmdLocal.Env = append(os.Environ(), "GENESIS_SANDBOX_USER="+GetSandboxUsername())
	
	afterStart1, cleanup1, err := PrepareRestrictedCommand(cmdLocal)
	if err != nil {
		t.Fatalf("PrepareRestrictedCommand failed for local ping: %v", err)
	}
	defer cleanup1()
	
	if err := cmdLocal.Start(); err != nil {
		t.Fatalf("cmdLocal.Start failed: %v", err)
	}
	if err := afterStart1(cmdLocal); err != nil {
		t.Fatalf("afterStart1 failed: %v", err)
	}
	if err := cmdLocal.Wait(); err != nil {
		t.Errorf("expected localhost ping to succeed, but got error: %v", err)
	}

	// 2. Test internet connection (ping external IP 8.8.8.8) -> should FAIL due to Firewall Block
	cmdExternal := exec.Command("ping", "8.8.8.8", "-n", "1")
	cmdExternal.Env = append(os.Environ(), "GENESIS_SANDBOX_USER="+GetSandboxUsername())
	
	afterStart2, cleanup2, err := PrepareRestrictedCommand(cmdExternal)
	if err != nil {
		t.Fatalf("PrepareRestrictedCommand failed for external ping: %v", err)
	}
	defer cleanup2()
	
	if err := cmdExternal.Start(); err != nil {
		t.Fatalf("cmdExternal.Start failed: %v", err)
	}
	if err := afterStart2(cmdExternal); err != nil {
		t.Fatalf("afterStart2 failed: %v", err)
	}
	
	err = cmdExternal.Wait()
	if err == nil {
		t.Error("expected external ping to be blocked by firewall and fail, but it succeeded")
	}
}


