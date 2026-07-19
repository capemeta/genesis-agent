// Package collab 提供会话级协作模式（规划模式 / 执行中）状态与工具闸门。
// 规划模式是 Runtime 协作层能力，不是任务清单（tasklist）的别名。
package collab

import "strings"

// Mode 会话协作模式。
type Mode string

const (
	// ModeDefault 执行中：可使用任务清单与变更类工具。
	ModeDefault Mode = "default"
	// ModePlan 规划模式：只读调研 + 写实施方案；禁用 todo_* 与变更类工具。
	ModePlan Mode = "plan_mode"
)

// Normalize 规范化模式；空或未知回落 default。
func Normalize(m Mode) Mode {
	switch Mode(strings.TrimSpace(string(m))) {
	case ModePlan:
		return ModePlan
	default:
		return ModeDefault
	}
}

// IsPlan 是否处于规划模式。
func IsPlan(m Mode) bool {
	return Normalize(m) == ModePlan
}

// DisplayName 返回面向用户的中文模式名。
func DisplayName(m Mode) string {
	if IsPlan(m) {
		return "规划模式"
	}
	return "执行中"
}
