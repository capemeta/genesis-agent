//go:build linux

package landlock

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// BuildOptions 控制 Landlock plan 构造。
// 仅支持：全盘读 + 指定可写目录，不支持 deny-read/网络/mount-view。
type BuildOptions struct {
	// WritableRoots 是允许写入的目录列表（绝对路径）。
	WritableRoots []string
}

// Plan 是 Landlock 构造结果：不改写 argv，只在进程内 apply ruleset。
type Plan struct {
	// ApplyFn 在进程启动后调用以应用 Landlock 规则；sandbox builder 将其注入 runner。
	// 设计为函数而非直接在 Build 时调用，使调用方可以在 fork 前完成规则集构造，
	// 在 exec 后（或 clone/thread 边界处）再 restrict_self，符合 Landlock 最佳实践。
	ApplyFn func() error
}

// Build 构造 Landlock plan。返回可在当前进程中应用文件系统限制的 Plan。
// 注意：Landlock restrict_self 只影响当前进程及其子进程，不影响父进程。
func Build(opts BuildOptions) (*Plan, error) {
	// 构造 landlock_ruleset_attr：只声明 LANDLOCK_ACCESS_FS_WRITE_FILE 等写相关操作
	// ABI v1+ 支持 handled_access_fs 含以下写权限
	const (
		landlock_access_fs_write_file    = 1 << 1
		landlock_access_fs_remove_dir    = 1 << 2
		landlock_access_fs_remove_file   = 1 << 3
		landlock_access_fs_make_char     = 1 << 4
		landlock_access_fs_make_dir      = 1 << 5
		landlock_access_fs_make_reg      = 1 << 6
		landlock_access_fs_make_sock     = 1 << 7
		landlock_access_fs_make_fifo     = 1 << 8
		landlock_access_fs_make_block    = 1 << 9
		landlock_access_fs_make_sym      = 1 << 10
		landlock_access_fs_refer         = 1 << 11 // ABI v2+
	)

	// 受限写权限集合（全部写相关操作都由我们管理）
	handledWrite := uint64(
		landlock_access_fs_write_file |
			landlock_access_fs_remove_dir |
			landlock_access_fs_remove_file |
			landlock_access_fs_make_char |
			landlock_access_fs_make_dir |
			landlock_access_fs_make_reg |
			landlock_access_fs_make_sock |
			landlock_access_fs_make_fifo |
			landlock_access_fs_make_block |
			landlock_access_fs_make_sym,
	)

	writableRoots := opts.WritableRoots

	applyFn := func() error {
		// 创建 ruleset
		// struct landlock_ruleset_attr { __u64 handled_access_fs; }
		type landlockRulesetAttr struct {
			HandledAccessFS uint64
		}
		attr := landlockRulesetAttr{HandledAccessFS: handledWrite}
		attrSize := unsafe.Sizeof(attr)
		rulesetFD, _, errno := unix.Syscall(
			unix.SYS_LANDLOCK_CREATE_RULESET,
			uintptr(unsafe.Pointer(&attr)),
			attrSize,
			0, // flags = 0
		)
		if errno != 0 {
			return fmt.Errorf("landlock_create_ruleset: %w", errno)
		}
		defer unix.Close(int(rulesetFD))

		// 为每个可写路径添加规则
		// struct landlock_path_beneath_attr { __u64 allowed_access; __s32 parent_fd; }
		type landlockPathBeneathAttr struct {
			AllowedAccess uint64
			ParentFD      int32
			_             [4]byte // padding
		}
		for _, root := range writableRoots {
			if root == "" {
				continue
			}
			fd, err := unix.Open(root, unix.O_PATH|unix.O_RDONLY, 0)
			if err != nil {
				return fmt.Errorf("landlock open %s: %w", root, err)
			}
			pathAttr := landlockPathBeneathAttr{
				AllowedAccess: handledWrite,
				ParentFD:      int32(fd),
			}
			_, _, addErrno := unix.Syscall(
				unix.SYS_LANDLOCK_ADD_RULE,
				rulesetFD,
				unix.LANDLOCK_RULE_PATH_BENEATH,
				uintptr(unsafe.Pointer(&pathAttr)),
			)
			unix.Close(fd)
			if addErrno != 0 {
				return fmt.Errorf("landlock_add_rule %s: %w", root, addErrno)
			}
		}

		// 应用 no_new_privs（Landlock 要求）
		if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
			return fmt.Errorf("prctl PR_SET_NO_NEW_PRIVS: %w", err)
		}

		// restrict_self 将规则集应用到当前进程
		_, _, errno = unix.Syscall(
			unix.SYS_LANDLOCK_RESTRICT_SELF,
			rulesetFD,
			0, // flags = 0
			0,
		)
		if errno != 0 {
			return fmt.Errorf("landlock_restrict_self: %w", errno)
		}
		return nil
	}

	return &Plan{ApplyFn: applyFn}, nil
}
