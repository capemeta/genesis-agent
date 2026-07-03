package bubblewrap

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// HelperTrustOptions 控制 helper 信任检查。
type HelperTrustOptions struct {
	WorkspaceRoots []string
	WritableRoots  []string
	TempRoots      []string
	HomeDir        string
}

// IsTrustedHelperPath 判断 bwrap/helper 是否位于可信路径。
func IsTrustedHelperPath(path string, opts HelperTrustOptions) (bool, string) {
	if path == "" {
		return false, "helper path为空"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err.Error()
	}
	cleaned := filepath.Clean(abs)
	if !filepath.IsAbs(cleaned) {
		return false, "helper path必须是绝对路径"
	}
	blocked := append([]string{}, opts.WorkspaceRoots...)
	blocked = append(blocked, opts.WritableRoots...)
	blocked = append(blocked, opts.TempRoots...)
	if opts.HomeDir != "" {
		blocked = append(blocked, filepath.Join(opts.HomeDir, ".local"), filepath.Join(opts.HomeDir, "bin"), filepath.Join(opts.HomeDir, "go", "bin"))
	}
	for _, root := range blocked {
		if root == "" {
			continue
		}
		inside, err := pathInside(cleaned, root)
		if err == nil && inside {
			return false, "helper path位于workspace、writable root、临时目录或用户可写目录: " + cleaned
		}
	}
	return true, ""
}

func pathInside(path, root string) (bool, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	path = filepath.Clean(path)
	rootAbs = filepath.Clean(rootAbs)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		rootAbs = strings.ToLower(rootAbs)
	}
	if path == rootAbs {
		return true, nil
	}
	rel, err := filepath.Rel(rootAbs, path)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

// DefaultTempRoots 返回常见临时目录。
func DefaultTempRoots() []string {
	roots := []string{}
	if tmp := os.TempDir(); tmp != "" {
		roots = append(roots, tmp)
	}
	if runtime.GOOS != "windows" {
		roots = append(roots, "/tmp", "/var/tmp")
	}
	return roots
}
