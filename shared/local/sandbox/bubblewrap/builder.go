// Package bubblewrap 提供 Linux bubblewrap plan builder。
package bubblewrap

import (
	"errors"
	"os"
	"path"
	"strings"
)

var ErrInvalidCommand = errors.New("bubblewrap command argv不能为空")

// NetworkMode 描述 bwrap 网络模式。
type NetworkMode string

const (
	NetworkFullAccess NetworkMode = "full_access"
	NetworkDisabled   NetworkMode = "disabled"
	NetworkProxyOnly  NetworkMode = "proxy_only"
	NetworkLoopback   NetworkMode = "loopback_only"
)

// PathMaskKind 描述不可读路径的 mask 类型。
type PathMaskKind string

const (
	PathMaskAuto PathMaskKind = "auto"
	PathMaskFile PathMaskKind = "file"
	PathMaskDir  PathMaskKind = "dir"
)

// PathMask 描述 bwrap deny-read mask。
type PathMask struct {
	Path string
	Kind PathMaskKind
}

// BuildOptions 控制 bwrap argv 构造。
type BuildOptions struct {
	BwrapPath     string
	Command       []string
	Cwd           string
	WritableRoots []string
	ReadOnlyRoots []string
	DenyReadPaths []PathMask
	Network       NetworkMode
	MountProc     bool

	// NoNewPrivs 为 true 时追加 --no-new-privs，防止子进程获得新 privilege。
	// 注意：setuid bwrap 需先完成 mount view 再由内部应用 no_new_privs；
	// 若 bwrap 以 setuid 运行，请在两阶段 helper 完成后再应用，而非在 bwrap argv 里传递。
	NoNewPrivs bool

	// ProxyEndpoint 为 proxy_only 网络模式预留的 proxy bridge 地址（如 "127.0.0.1:8118"）。
	// 当前 builder 不将其写入 argv；proxy bridge 进程由上层 runner 负责管理。
	// 后续实现时：在 bwrap network namespace 内通过 socat/slirp4netns 接入此端点。
	ProxyEndpoint string
}

// Plan 是 bwrap 构造结果。
type Plan struct {
	Program string
	Args    []string
}

// Build 构造 bwrap argv。它只包裹结构化 argv，不解释命令内容。
func Build(opts BuildOptions) (*Plan, error) {
	if len(opts.Command) == 0 || opts.Command[0] == "" {
		return nil, ErrInvalidCommand
	}
	if opts.BwrapPath == "" {
		opts.BwrapPath = "bwrap"
	}
	args := []string{
		"--new-session",
		"--die-with-parent",
		"--unshare-user",
		"--unshare-pid",
		"--unshare-uts",
		"--unshare-ipc",
	}
	if opts.Network == NetworkDisabled || opts.Network == NetworkProxyOnly || opts.Network == NetworkLoopback {
		args = append(args, "--unshare-net")
	}
	if opts.NoNewPrivs {
		args = append(args, "--no-new-privs")
	}
	args = append(args, "--ro-bind", "/", "/")
	args = append(args, "--dev", "/dev")
	if opts.MountProc {
		args = append(args, "--proc", "/proc")
	}
	if opts.Cwd != "" {
		args = append(args, "--chdir", opts.Cwd)
	}
	for _, root := range cleanList(opts.WritableRoots) {
		args = append(args, "--bind", root, root)
	}
	for _, root := range cleanList(opts.ReadOnlyRoots) {
		args = append(args, "--ro-bind", root, root)
	}
	for _, mask := range opts.DenyReadPaths {
		p := cleanPath(mask.Path)
		if p == "" || strings.HasPrefix(p, "/dev/") {
			continue
		}
		kind := mask.Kind
		if kind == PathMaskAuto {
			kind = detectMaskKind(p)
		}
		if kind == PathMaskDir {
			args = append(args, "--tmpfs", p)
		} else {
			args = append(args, "--ro-bind", "/dev/null", p)
		}
	}
	args = append(args, "--")
	args = append(args, opts.Command...)
	return &Plan{Program: opts.BwrapPath, Args: args}, nil
}

func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		cleaned := cleanPath(value)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func cleanPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		return ""
	}
	return path.Clean(value)
}

func detectMaskKind(p string) PathMaskKind {
	info, err := os.Stat(p)
	if err == nil && info.IsDir() {
		return PathMaskDir
	}
	if strings.HasSuffix(p, "/") {
		return PathMaskDir
	}
	return PathMaskFile
}
