//go:build windows

package windowssandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ElevationResult 描述提权子进程的执行结果（写入文件后由父进程读取）。
type ElevationResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// elevationResultPath 返回提权结果文件路径（写在工作目录下的 .genesis/logs/ 目录）。
func elevationResultPath(cwd string) string {
	return filepath.Join(cwd, ".genesis", "logs", "sandbox_elevation_result.json")
}

// WriteElevationResult 由提权子进程调用，将执行结果写入文件供父进程读取。
// 注意：确保 .genesis/logs 目录存在后再写入，否则在全新环境中写入会静默失败。
func WriteElevationResult(cwd string, result ElevationResult) {
	dir := filepath.Join(cwd, ".genesis", "logs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	data, _ := json.Marshal(result)
	_ = os.WriteFile(elevationResultPath(cwd), data, 0644)
}

// ReadElevationResult 由父进程调用，读取提权子进程的执行结果。
func ReadElevationResult(cwd string) (*ElevationResult, error) {
	path := elevationResultPath(cwd)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r ElevationResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ClearElevationResult 清理提权结果文件。
func ClearElevationResult(cwd string) {
	_ = os.Remove(elevationResultPath(cwd))
}

// shellExecuteW 使用 Win32 ShellExecuteExW API 以 runas 动词提权运行目标程序。
//
// 设计对标 Codex（codex-rs/windows-sandbox-rs/src/setup.rs run_setup_exe）：
//   - 不依赖 PowerShell，比 Start-Process -Verb RunAs 更可靠、更底层
//   - nShow = SW_HIDE(0)：提权子进程静默后台运行，不弹出额外终端窗口
//   - SEE_MASK_NOCLOSEPROCESS：获取进程句柄以便 WaitForSingleObject 阻塞等待
//
// 结构体布局说明（重要）：
// SHELLEXECUTEINFOW 的 C 布局在 64-bit 下，Go 编译器会在 NShow(int32) 后和
// DwHotKey(uint32) 后自动插入 4 字节 padding 以对齐后继指针字段。
// 不需要手工添加空白字段 — Go 自然对齐规则与 Windows SDK 结构完全吻合。
// 验证：unsafe.Sizeof(SHELLEXECUTEINFOW{}) == 112（与 SDK 定义一致）。
func shellExecuteW(exe, params, workDir string) (uint32, error) {
	const (
		seeMaskNoCloseProcess uint32 = 0x00000040
		swHide                int32  = 0
		waitInfinite          uint32 = 0xFFFFFFFF
	)

	shell32 := windows.NewLazySystemDLL("shell32.dll")
	shellExecEx := shell32.NewProc("ShellExecuteExW")
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	waitForSingleObject := kernel32.NewProc("WaitForSingleObject")
	getExitCodeProcess := kernel32.NewProc("GetExitCodeProcess")

	// SHELLEXECUTEINFOW 结构体（与 Windows SDK shellapi.h 布局完全一致）。
	// 64-bit 对齐：Go 编译器自动在 NShow 后、DwHotKey 后各插入 4 字节 padding，
	// 不需要手工添加任何 _ 填充字段，结构体总大小为 112 字节。
	type SHELLEXECUTEINFOW struct {
		CbSize         uint32  // +0
		FMask          uint32  // +4
		HWnd           uintptr // +8
		LpVerb         *uint16 // +16
		LpFile         *uint16 // +24
		LpParameters   *uint16 // +32
		LpDirectory    *uint16 // +40
		NShow          int32   // +48  ← Go 自动在此后插入 4 字节 padding
		HInstApp       uintptr // +56
		LpIDList       uintptr // +64
		LpClass        *uint16 // +72
		HkeyClass      uintptr // +80
		DwHotKey       uint32  // +88  ← Go 自动在此后插入 4 字节 padding
		HIconOrMonitor uintptr // +96
		HProcess       uintptr // +104
	} // total: 112 bytes

	verbW, _ := windows.UTF16PtrFromString("runas")
	exeW, _ := windows.UTF16PtrFromString(exe)
	paramsW, _ := windows.UTF16PtrFromString(params)
	dirW, _ := windows.UTF16PtrFromString(workDir)

	sei := SHELLEXECUTEINFOW{
		FMask:        seeMaskNoCloseProcess,
		LpVerb:       verbW,
		LpFile:       exeW,
		LpParameters: paramsW,
		LpDirectory:  dirW,
		NShow:        swHide,
	}
	sei.CbSize = uint32(unsafe.Sizeof(sei))

	ret, _, err := shellExecEx.Call(uintptr(unsafe.Pointer(&sei)))
	if ret == 0 {
		var errno windows.Errno
		if errors.As(err, &errno) && errno == windows.ERROR_CANCELLED {
			return 1, fmt.Errorf("用户拒绝了 UAC 提权授权，无法自动完成 Windows 沙箱环境初始化")
		}
		return 1, fmt.Errorf("ShellExecuteExW 失败: %w", err)
	}
	if sei.HProcess == 0 {
		// SEE_MASK_NOCLOSEPROCESS 要求操作系统返回进程句柄，若为 0 则异常
		return 1, fmt.Errorf("ShellExecuteExW 未返回进程句柄")
	}

	// 等待提权子进程执行完成（INFINITE 超时适用于沙箱初始化场景）
	waitForSingleObject.Call(sei.HProcess, uintptr(waitInfinite))
	var exitCode uint32
	getExitCodeProcess.Call(sei.HProcess, uintptr(unsafe.Pointer(&exitCode)))
	_ = windows.CloseHandle(windows.Handle(sei.HProcess))

	return exitCode, nil
}

// RunElevatedWindowsSetup 使用 Win32 ShellExecuteExW 进行 UAC 提权并运行 windows-setup。
//
// 参数：
//   - exeOrGoRun: 要执行的程序路径（编译后为 genesis-cli.exe，开发时为 "go"）
//   - args: 命令行参数列表（如 ["sandbox", "windows-setup", "--network", "--appdata", dir]）
//   - workDir: 工作目录，也是 sandbox_elevation_result.json 的读写路径
func RunElevatedWindowsSetup(exeOrGoRun string, args []string, workDir string) error {
	params := quoteWindowsArgs(args)

	ClearElevationResult(workDir)

	exitCode, err := shellExecuteW(exeOrGoRun, params, workDir)
	if err != nil {
		return fmt.Errorf("UAC 提权启动失败: %w", err)
	}

	// 读取子进程写入的结构化结果文件，获取详细成功/失败信息
	result, readErr := ReadElevationResult(workDir)
	if exitCode != 0 {
		if readErr == nil && result != nil && !result.Success && result.Error != "" {
			return fmt.Errorf("提权子进程执行失败: %s", result.Error)
		}
		return fmt.Errorf("提权子进程退出码异常: %d", exitCode)
	}
	// exitCode == 0 但结果文件报告失败（防御性保护）
	if readErr == nil && result != nil && !result.Success {
		return fmt.Errorf("提权子进程报告失败: %s", result.Error)
	}
	return nil
}

// quoteWindowsArgs 将参数数组拼接为 Windows 格式的命令行字符串。
// 规则：含空格/制表符/换行/双引号的参数用双引号包裹，双引号字符转义为 \"。
// 对 ShellExecuteExW lpParameters，此规则与 Windows CRT 命令行解析兼容。
func quoteWindowsArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t\r\n\"") {
			arg = `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
		}
		quoted[i] = arg
	}
	return strings.Join(quoted, " ")
}
