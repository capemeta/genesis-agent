//go:build windows

package pathutil

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	getFinalPathNameByHandle = kernel32.NewProc("GetFinalPathNameByHandleW")
)

// platformClean 在 Windows 上调用 GetFinalPathNameByHandle 解析：
//   - Reparse point / Junction（符号链接、目录联接）
//   - 8.3 short path
//   - 大小写规范化（NTFS 不区分大小写）
//   - UNC 路径
//
// 若 Win32 API 调用失败（如文件不存在），fallback 到 filepath.Clean + 小写规范化。
func platformClean(path string) string {
	path = filepath.Clean(path)
	resolved, err := resolveViaNativeAPI(path)
	if err != nil {
		// fallback：至少做 Windows 不区分大小写的规范化
		return strings.ToLower(path)
	}
	// GetFinalPathNameByHandle 返回 \\?\-前缀路径，转换为普通路径
	resolved = stripExtendedPrefix(resolved)
	return resolved
}

// resolveViaNativeAPI 打开路径句柄并调用 GetFinalPathNameByHandle 获取规范路径。
func resolveViaNativeAPI(path string) (string, error) {
	// 将路径转换为 UTF-16
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	// 以 FILE_FLAG_BACKUP_SEMANTICS 打开（允许打开目录）
	handle, err := syscall.CreateFile(
		pathPtr,
		0, // GENERIC_READ 最小权限
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS, // 允许打开目录
		0,
	)
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(handle)

	// 调用 GetFinalPathNameByHandle，VOLUME_NAME_DOS = 0x0
	buf := make([]uint16, 4096)
	r1, _, e1 := getFinalPathNameByHandle.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		0, // VOLUME_NAME_DOS
	)
	if r1 == 0 {
		return "", e1
	}
	return syscall.UTF16ToString(buf[:r1]), nil
}

// stripExtendedPrefix 去除 GetFinalPathNameByHandle 返回的 \\?\ 前缀。
func stripExtendedPrefix(path string) string {
	if strings.HasPrefix(path, `\\?\UNC\`) {
		return `\\` + path[8:]
	}
	if strings.HasPrefix(path, `\\?\`) {
		return path[4:]
	}
	return path
}
