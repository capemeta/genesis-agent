//go:build !windows

package fs_backend

import "os"

func replaceFile(src string, dst string) error {
	return os.Rename(src, dst)
}
