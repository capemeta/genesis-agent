//go:build windows

package windowssandbox

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procLogonUserW = windows.NewLazySystemDLL("advapi32.dll").NewProc("LogonUserW")

const (
	logon32LogonInteractive = 2
	logon32ProviderDefault   = 0
)

func logonUser(username, domain, password string) (windows.Token, error) {
	usernamePtr, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return 0, err
	}
	domainPtr, err := windows.UTF16PtrFromString(domain)
	if err != nil {
		return 0, err
	}
	passwordPtr, err := windows.UTF16PtrFromString(password)
	if err != nil {
		return 0, err
	}

	var token windows.Token
	r1, _, e1 := procLogonUserW.Call(
		uintptr(unsafe.Pointer(usernamePtr)),
		uintptr(unsafe.Pointer(domainPtr)),
		uintptr(unsafe.Pointer(passwordPtr)),
		logon32LogonInteractive,
		logon32ProviderDefault,
		uintptr(unsafe.Pointer(&token)),
	)
	if r1 == 0 {
		if e1 != syscall.Errno(0) {
			return 0, e1
		}
		return 0, syscall.EINVAL
	}
	return token, nil
}

// PreparedCommandCleanup releases the restricted token and job object handle.
type PreparedCommandCleanup func()

// PrepareRestrictedCommand duplicates the process token, applies restriction SIDs (for L2) if requested,
// and assigns the spawned process to a kill-on-close job object.
func PrepareRestrictedCommand(cmd *exec.Cmd) (func(*exec.Cmd) error, PreparedCommandCleanup, error) {
	if cmd == nil {
		return nil, nil, fmt.Errorf("cmd不能为空")
	}

	// 1. Extract capability SIDs and sandbox user from cmd.Env if passed
	var capabilitySids []string
	var sandboxUser string
	var newEnv []string
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "GENESIS_SANDBOX_CAP_SIDS=") {
			sidsStr := strings.TrimPrefix(entry, "GENESIS_SANDBOX_CAP_SIDS=")
			if sidsStr != "" {
				for _, part := range strings.Split(sidsStr, ",") {
					part = strings.TrimSpace(part)
					if part != "" {
						capabilitySids = append(capabilitySids, part)
					}
				}
			}
		} else if strings.HasPrefix(entry, "GENESIS_SANDBOX_USER=") {
			sandboxUser = strings.TrimPrefix(entry, "GENESIS_SANDBOX_USER=")
		} else {
			newEnv = append(newEnv, entry)
		}
	}
	cmd.Env = newEnv

	var token windows.Token
	var err error

	if sandboxUser != "" {
		// L3 (Network isolation path using logon offline sandbox user)
		password, err := ReadSecret()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read sandbox password: %w", err)
		}

		token, err = logonUser(sandboxUser, ".", password)
		if err != nil {
			return nil, nil, fmt.Errorf("LogonUser failed for %s: %w (请运行 windows-setup --network 初始化账户)", sandboxUser, err)
		}
	} else {
		// L1/L2 (Process constrained or filesystem isolation using restricted token)
		token, err = createRestrictedPrimaryToken(capabilitySids)
		if err != nil {
			return nil, nil, fmt.Errorf("CreateRestrictedToken failed: %w", err)
		}
	}

	// 3. Create job object for life-cycle management
	job, err := createKillOnCloseJob()
	if err != nil {
		_ = token.Close()
		return nil, nil, fmt.Errorf("CreateJobObject failed: %w", err)
	}

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Token = syscall.Token(token)

	cleanup := func() {
		_ = windows.CloseHandle(job)
		_ = token.Close()
	}

	afterStart := func(started *exec.Cmd) error {
		if started == nil || started.Process == nil {
			return fmt.Errorf("sandboxed process未启动")
		}
		handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(started.Process.Pid))
		if err != nil {
			return fmt.Errorf("OpenProcess failed: %w", err)
		}
		defer windows.CloseHandle(handle)
		if err := windows.AssignProcessToJobObject(job, handle); err != nil {
			return fmt.Errorf("AssignProcessToJobObject failed: %w", err)
		}
		return nil
	}

	return afterStart, cleanup, nil
}
