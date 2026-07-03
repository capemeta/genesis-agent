//go:build !linux

package landlock

// BuildOptions 控制 Landlock plan 构造（非 Linux stub）。
type BuildOptions struct {
	WritableRoots []string
}

// Plan stub for non-Linux platforms.
type Plan struct {
	ApplyFn func() error
}

// Build 在非 Linux 平台返回不可用错误。
func Build(_ BuildOptions) (*Plan, error) {
	return nil, errUnsupported
}
