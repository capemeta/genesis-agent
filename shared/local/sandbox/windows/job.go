//go:build windows

package windowssandbox

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

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
