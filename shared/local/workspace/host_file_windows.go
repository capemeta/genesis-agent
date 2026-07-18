//go:build windows

package workspace

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

type hostFileIdentity struct {
	VolumeSerial uint32 `json:"volume_serial"`
	FileIndex    uint64 `json:"file_index"`
}

func (i hostFileIdentity) empty() bool { return i.VolumeSerial == 0 && i.FileIndex == 0 }

func hostIdentityFromOpenFile(file *os.File, _ os.FileInfo) (hostFileIdentity, error) {
	var attributes syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &attributes); err != nil {
		return hostFileIdentity{}, err
	}
	return hostFileIdentity{VolumeSerial: attributes.VolumeSerialNumber, FileIndex: uint64(attributes.FileIndexHigh)<<32 | uint64(attributes.FileIndexLow)}, nil
}

func openHostFileNoFollow(path string) (*os.File, os.FileInfo, hostFileIdentity, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	var attributes syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &attributes); err != nil {
		_ = syscall.CloseHandle(handle)
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	if attributes.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = syscall.CloseHandle(handle)
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("Host resource 是 reparse point"))
	}
	file := os.NewFile(uintptr(handle), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("Host resource 不是普通文件: %v", err))
	}
	identity := hostFileIdentity{VolumeSerial: attributes.VolumeSerialNumber, FileIndex: uint64(attributes.FileIndexHigh)<<32 | uint64(attributes.FileIndexLow)}
	return file, info, identity, nil
}

func unsafeHostPathComponent(path string) (bool, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attributes, _, callErr := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileAttributesW").Call(uintptr(unsafe.Pointer(name)))
	if attributes == uintptr(syscall.INVALID_FILE_ATTRIBUTES) {
		return false, callErr
	}
	return uint32(attributes)&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}
