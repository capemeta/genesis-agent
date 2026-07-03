// Package bubblewrap 提供 Linux bubblewrap plan builder 及 user namespace 能力探测。
package bubblewrap

// WSLVersion 描述 WSL 环境版本。
type WSLVersion int

const (
	WSLNone WSLVersion = 0 // 非 WSL 环境（原生 Linux 或其他平台）
	WSL1    WSLVersion = 1 // WSL1：不支持 user namespace
	WSL2    WSLVersion = 2 // WSL2：支持 user namespace，行为与原生 Linux 接近
)
