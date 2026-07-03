//go:build !darwin && !linux && !windows

package sandbox

func defaultPlatformBackend() platformBackend {
	return unavailableBackend{reason: "unsupported platform"}
}
