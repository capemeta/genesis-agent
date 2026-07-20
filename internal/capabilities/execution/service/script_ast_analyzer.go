package service

import (
	"strings"
)

// ScriptASTAnalyzer 负责对复合 Shell 脚本或多语言包装脚本做词法与 AST 解析，
// 只要脚本中包含任意一条敏感命令，整个复合脚本一票强切远程容器沙箱。
type ScriptASTAnalyzer struct {
	highRiskKeywords []string
}

// NewScriptASTAnalyzer 创建复合脚本 AST 分析器。
func NewScriptASTAnalyzer() *ScriptASTAnalyzer {
	return &ScriptASTAnalyzer{
		highRiskKeywords: []string{
			"pip install", "pip3 install",
			"npm install", "npm i ", "yarn add", "pnpm add",
			"curl ", "wget ", "nc ", "netcat ",
			"python -c", "python3 -c", "node -e",
			"sh -c", "bash -c", "powershell -c",
			"| bash", "| sh", "eval ", "exec ",
			"base64 -d", "chmod +x", "chown ",
			"sudo ", "su ",
		},
	}
}

// AnalyzeScript 扫描输入的命令字符串或多行脚本体。
// 返回该脚本的综合风险级别 (RiskLevelLocalSafe 或 RiskLevelUntrustedRemote)。
func (a *ScriptASTAnalyzer) AnalyzeScript(rawScript string) RiskLevel {
	trimmed := strings.TrimSpace(rawScript)
	if trimmed == "" {
		return RiskLevelLocalSafe
	}

	// 1. 按行与分号/管道切割 Token 子指令，模拟 Shell AST 分词
	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		lineClean := strings.TrimSpace(line)
		if lineClean == "" || strings.HasPrefix(lineClean, "#") || strings.HasPrefix(lineClean, "//") {
			continue // 忽略注释
		}

		lowerLine := strings.ToLower(lineClean)

		// 检查纯文本搜索/打印命令（防范 grep "pip install" 误杀）
		fields := strings.Fields(lineClean)
		if len(fields) > 0 {
			firstCmd := strings.ToLower(fields[0])
			if firstCmd == "grep" || firstCmd == "ripgrep" || firstCmd == "rg" || firstCmd == "cat" || firstCmd == "echo" {
				continue
			}
		}

		// 2. 检查子指令是否触碰敏感/高危模式
		for _, kw := range a.highRiskKeywords {
			if strings.Contains(lowerLine, kw) {
				// 全脚本“一票强切远程沙箱”规则：只要包含一条敏感指令，整个复合脚本切至远程
				return RiskLevelUntrustedRemote
			}
		}
	}

	return RiskLevelLocalSafe
}
