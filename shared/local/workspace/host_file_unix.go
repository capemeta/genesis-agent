//go:build !windows

package workspace

import (
	"fmt"
	"os"
	"syscall"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

type hostFileIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

func (i hostFileIdentity) empty() bool { return i.Device == 0 && i.Inode == 0 }

func hostIdentity(info os.FileInfo) (hostFileIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return hostFileIdentity{}, fmt.Errorf("无法读取 Unix 文件身份")
	}
	return hostFileIdentity{Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, nil
}

func hostIdentityFromOpenFile(_ *os.File, info os.FileInfo) (hostFileIdentity, error) {
	return hostIdentity(info)
}

func openHostFileNoFollow(path string) (*os.File, os.FileInfo, hostFileIdentity, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("Host resource 不是普通文件"))
	}
	identity, err := hostIdentity(info)
	if err != nil {
		_ = file.Close()
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	return file, info, identity, nil
}

func unsafeHostPathComponent(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}
