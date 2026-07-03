//go:build !linux

package bubblewrap

import "context"

// Detect 在非 Linux 平台返回不可用（stub）。
func Detect(_ context.Context, _ string, _ HelperTrustOptions) (string, bool, string) {
	return "", false, "bubblewrap is only available on Linux"
}

// CheckUserNS 在非 Linux 平台返回不可用（stub）。
func CheckUserNS() (bool, string) {
	return false, "user namespaces only available on Linux"
}

// DetectWSLVersion 在非 Linux 平台返回 WSLNone（stub）。
func DetectWSLVersion() WSLVersion {
	return WSLNone
}
