//go:build !windows

package artifact

import "os"

func replaceLedgerFile(source, destination string) error {
	return os.Rename(source, destination)
}
