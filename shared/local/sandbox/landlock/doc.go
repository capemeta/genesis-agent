// Package landlock 预留 Linux Landlock 适配位置。
package landlock

import "errors"

// errUnsupported 在非 Linux 平台表示 Landlock 不可用。
var errUnsupported = errors.New("Landlock is only available on Linux")
