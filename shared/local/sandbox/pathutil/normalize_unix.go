//go:build !windows

package pathutil

import "path/filepath"

// platformClean 在非 Windows 平台只做基础 filepath.Clean。
func platformClean(path string) string {
	return filepath.Clean(path)
}
