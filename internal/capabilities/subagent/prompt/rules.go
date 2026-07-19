// Package prompt 提供子智能体委派提示词的单一事实来源（SSOT）。
//
// system 稳定块（delegation）与 Task 工具 Description 必须从此包派生，
// 禁止在 runtime/prompt 与 tool/task 中各自维护互不相同的文案。
//
// 设计取舍（对照 Kode / Codex，见 docs/子智能体设计.md §6.1.2）：
//   - system 只保留「何时用/不用 + 硬纪律」，对齐 Kode Tool usage policy 短规则；
//   - Catalog / When NOT to use / 并行建议放 Description；
//   - delegation_posture 切换 Kode 式 proactive 与 Codex 式 explicit_request_only。
package prompt

import (
	"fmt"
	"strings"
)

// Posture 是委派姿态。
type Posture string

const (
	// PostureProactive 对齐 Kode：匹配 description / 广搜时主动 Task。
	PostureProactive Posture = "proactive"
	// PostureExplicitRequestOnly 对齐 Codex V1：除非明确要求否则不 spawn。
	PostureExplicitRequestOnly Posture = "explicit_request_only"
)

// AgentSummary 是 Description 中列出的 Catalog 摘要。
type AgentSummary struct {
	Name        string
	Description string
	WhenToUse   string
}

// DescriptionOptions 控制 Task 工具动态描述。
type DescriptionOptions struct {
	Posture       Posture
	MaxConcurrent int
}

// NormalizePosture 规范化姿态；空值与未知值回落 proactive。
func NormalizePosture(raw string) Posture {
	switch Posture(strings.ToLower(strings.TrimSpace(raw))) {
	case PostureExplicitRequestOnly:
		return PostureExplicitRequestOnly
	default:
		return PostureProactive
	}
}

// SystemRules 注入主模型 system 的委派纪律（Task 可用时）。
func SystemRules(posture Posture) string {
	switch NormalizePosture(string(posture)) {
	case PostureExplicitRequestOnly:
		return "" +
			"# 子智能体委派纪律 (Delegation)\n" +
			"\n" +
			"除非用户、项目指令（如 AGENTS.md）或已加载的 Skill **明确要求**委派、子智能体或并行 Agent 工作，否则不要调用 `Task`。\n" +
			"「要深入」「要彻底研究」「详细分析代码库」**不算授权**。\n" +
			"\n" +
			"## 已获授权时\n" +
			"- 必须使用 `Task(subagent_type=...)`，禁止把 agent 名当作工具名调用。\n" +
			"- 从 available_agents 选择类型；已知精确路径 / 单文件 needle 仍优先直接 `read_file`/`grep`，不要为小事 spawn。\n"
	default:
		return "" +
			"# 子智能体委派纪律 (Delegation)\n" +
			"\n" +
			"主线程可通过固定网关工具 `Task` 委派独立子智能体；禁止把 agent 名当作工具名调用。\n" +
			"\n" +
			"## 何时使用\n" +
			"- 文件搜索 / 非 needle 的代码库探索：优先 `Task(subagent_type=explore)`，以节省主上下文。\n" +
			"- 任务匹配某 agent 的 description / when_to_use（含自定义）：应主动使用 `Task`，无需用户点名。\n" +
			"- 可并行的独立子任务：在同一条 assistant 消息内发起多个 `Task`（受并发上限约束）。\n" +
			"\n" +
			"## 何时不要使用\n" +
			"- 已知精确路径 / 单文件或 2–3 个文件的 needle 查询：直接 `read_file`/`grep`/`glob`，不要为小事 spawn。\n"
	}
}

// RenderToolDescription 渲染 Task 工具动态 Description。
func RenderToolDescription(agents []AgentSummary, opts DescriptionOptions) (string, error) {
	posture := NormalizePosture(string(opts.Posture))
	maxConcurrent := opts.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}

	var b strings.Builder
	b.WriteString("委派独立子智能体执行任务。必须调用 Task(subagent_type=...)，禁止把 agent 名当作工具名调用。\n")
	b.WriteString("resume 可基于已完成 agent_id 发起后续任务；run_in_background=true 时立即返回 agent_id，再用 TaskOutput 取结果。\n\n")

	switch posture {
	case PostureExplicitRequestOnly:
		b.WriteString("Authorization: Do not spawn sub-agents unless the user or applicable AGENTS.md/skill instructions explicitly ask for sub-agents, delegation, or parallel agent work. ")
		b.WriteString("Requests for depth, thoroughness, research, or detailed codebase analysis do not count as permission to spawn.\n\n")
	default:
		b.WriteString("Usage posture: when an agent description matches the task, use Task proactively without waiting for the user to name the agent. ")
		b.WriteString("For broad, non-needle codebase exploration prefer subagent_type=explore.\n\n")
	}

	b.WriteString("When NOT to use the Task tool:\n")
	b.WriteString("- Known exact path / single-file needle: use read_file or glob/grep directly.\n")
	b.WriteString("- Searching for a specific symbol in 1–3 known files: use grep/read_file instead of Task.\n")
	b.WriteString("- Tasks unrelated to any available agent description.\n\n")

	fmt.Fprintf(&b, "Parallelism: launch independent Tasks in one assistant message when useful; hard limit max_concurrent=%d (tool rejects excess).\n\n", maxConcurrent)

	b.WriteString("<available_agents>\n")
	for _, item := range agents {
		fmt.Fprintf(&b, "<agent name=%q when_to_use=%q>%s</agent>\n", item.Name, item.WhenToUse, item.Description)
	}
	b.WriteString("</available_agents>")
	return b.String(), nil
}
