//go:build windows

package windowssandbox

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	restrictedTokenDisableMaxPrivilege = 0x00000001
	restrictedTokenLua                 = 0x00000004

	restrictedTokenAccess = windows.TOKEN_ASSIGN_PRIMARY |
		windows.TOKEN_DUPLICATE |
		windows.TOKEN_QUERY |
		windows.TOKEN_ADJUST_DEFAULT |
		windows.TOKEN_ADJUST_SESSIONID
)

var procCreateRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

// PreparedCommandCleanup 释放 restricted token 和 job object。
type PreparedCommandCleanup func()

// PrepareRestrictedCommand 为 exec.Cmd 注入 Restricted Token，并返回 JobObject 绑定 hook。
func PrepareRestrictedCommand(cmd *exec.Cmd) (func(*exec.Cmd) error, PreparedCommandCleanup, error) {
	if cmd == nil {
		return nil, nil, fmt.Errorf("cmd不能为空")
	}
	token, err := createRestrictedPrimaryToken()
	if err != nil {
		return nil, nil, fmt.Errorf("CreateRestrictedToken failed: %w", err)
	}
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

func createRestrictedPrimaryToken() (windows.Token, error) {
	var current windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), restrictedTokenAccess, &current); err != nil {
		return 0, err
	}
	defer current.Close()
	var restricted windows.Token
	flags := uintptr(restrictedTokenDisableMaxPrivilege)
	r1, _, e1 := procCreateRestrictedToken.Call(
		uintptr(current),
		flags,
		0, 0,
		0, 0,
		0, 0,
		uintptr(unsafe.Pointer(&restricted)),
	)
	if r1 == 0 {
		if e1 != syscall.Errno(0) {
			return 0, e1
		}
		return 0, syscall.EINVAL
	}
	return restricted, nil
}

func createKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		windows.JOB_OBJECT_LIMIT_DIE_ON_UNHANDLED_EXCEPTION
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	ui := windows.JOBOBJECT_BASIC_UI_RESTRICTIONS{
		UIRestrictionsClass: windows.JOB_OBJECT_UILIMIT_WRITECLIPBOARD |
			windows.JOB_OBJECT_UILIMIT_READCLIPBOARD |
			windows.JOB_OBJECT_UILIMIT_SYSTEMPARAMETERS |
			windows.JOB_OBJECT_UILIMIT_DISPLAYSETTINGS,
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectBasicUIRestrictions,
		uintptr(unsafe.Pointer(&ui)),
		uint32(unsafe.Sizeof(ui)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
