package service

import (
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// RiskLevel 描述命令的评估风险等级。
type RiskLevel string

const (
	// RiskLevelLocalSafe 适合在本地 OS 进程沙箱 (Process Confinement) 中高效率运行。
	RiskLevelLocalSafe RiskLevel = "local_safe"
	// RiskLevelUntrustedRemote 涉及高危依赖、网络下载或未知代码，建议通过子 Agent 路由到远程容器沙箱。
	RiskLevelUntrustedRemote RiskLevel = "untrusted_remote"
)

// CommandRiskEvaluator 评估命令的风险级别，指导沙箱路由策略。
type CommandRiskEvaluator struct{}

// NewCommandRiskEvaluator 创建命令风险评估器。
func NewCommandRiskEvaluator() *CommandRiskEvaluator {
	return &CommandRiskEvaluator{}
}

// EvaluateRisk 分析给定的命令及当前任务/Skill作用域的粘性状态，返回适宜的 RiskLevel。
// 注意：taskElevatedToRemote 作用域限定在当前子任务/Skill 生命周期内，防止污染父 Session 后续不相干的本地命令。
func (e *CommandRiskEvaluator) EvaluateRisk(cmd execmodel.Command, taskElevatedToRemote bool) RiskLevel {
	if taskElevatedToRemote {
		return RiskLevelUntrustedRemote
	}

	raw := strings.TrimSpace(cmd.Command)
	if raw == "" {
		return RiskLevelLocalSafe
	}

	// 提取首个 Command Token / 可执行程序名，防范 grep "pip install" 文本误杀
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return RiskLevelLocalSafe
	}

	firstCmd := strings.ToLower(fields[0])

	// 过滤 grep / find / cat 等查询命令里的文本误杀
	if firstCmd == "grep" || firstCmd == "ripgrep" || firstCmd == "rg" || firstCmd == "cat" || firstCmd == "echo" {
		return RiskLevelLocalSafe
	}

	lower := strings.ToLower(raw)

	// 高危操作、网络依赖安装或 Pipe 提权脚本模式判断
	highRiskKeywords := []string{
		"pip install",
		"pip3 install",
		"npm install",
		"npm i ",
		"yarn add",
		"pnpm add",
		"curl ",
		"wget ",
		"python -c",
		"python3 -c",
		"node -e",
		"sh -c",
		"bash -c",
		"powershell -c",
		"| bash",
		"| sh",
	}

	for _, kw := range highRiskKeywords {
		if strings.Contains(lower, kw) {
			return RiskLevelUntrustedRemote
		}
	}

	// 默认为本地安全的增量构建/测试命令
	return RiskLevelLocalSafe
}
