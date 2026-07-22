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
			"`Task` 委派下方 <available_agents> 中的子智能体；`Skill(skill=...)` 加载技能。二者网关不同，不可互替（MUST）。\n" +
			"\n" +
			"除非用户、项目指令（如 AGENTS.md）或已加载的 Skill **明确要求**委派、子智能体或并行 Agent 工作，否则不要调用 `Task`。\n" +
			"「要深入」「要彻底研究」「详细分析代码库」**不算授权**。\n" +
			"\n" +
			"## 已获授权时\n" +
			"- 必须使用 `Task(subagent_type=...)`，禁止把 agent 名当作工具名调用。\n" +
			"- 已知精确路径 / 1–3 个文件的局部确认：直接使用 `read_file`/`grep`/`glob`，不要为小事 spawn。\n"
	default:
		return "" +
			"# 子智能体委派纪律 (Delegation)\n" +
			"\n" +
			"`Task` 委派下方 <available_agents> 中的子智能体；`Skill(skill=...)` 加载技能。二者网关不同，不可互替（MUST）。\n" +
			"\n" +
			"## 委派决策（按顺序判断，主动委派）\n" +
			"1. **范围否决**：已知精确路径、或仅涉及 1–3 个文件的局部读取/确认/修改 → 直接 `read_file`/`grep`/`glob`/`apply_patch`，不要为小事 spawn。\n" +
			"2. **隔离与匹配委派**：排除①后，命中以下条件之一即主动使用 `Task`，无需用户点名：\n" +
			"   - 属于宽泛代码库探索、非精确 needle 搜索、多方案对比等产生大量中间噪音的摸排 → 使用 `Task(subagent_type=explore)`。\n" +
			"   - 任务匹配 <available_agents> 中某 agent 的专长领域 → 使用对应的 `Task(subagent_type=\"<agent_name>\")`。\n" +
			"3. **并发修饰**：当触发②且存在 ≥2 个相互独立、不依赖彼此中间结果的子任务（如不同模块摸排）→ 必须在同一条回复中并发发起多个 `Task(..., run_in_background=true)`。\n" +
			"\n" +
			"不得用 `Task` 代替 `Skill` 调用；纯格式转换、简单润色等主线程自身可低成本完成的工作，不委派（MUST NOT）。\n" +
			"\n" +
			"## 后台并发与参数纪律\n" +
			"- **后台模式**：设置 `run_in_background=true` 立即返回 `agent_id`，主线程可继续处理其他工作，需要结果时用 `TaskOutput(agent_id=...)` 阻塞获取。\n" +
			"- **并发要求**：多个子任务并发必须在同一条回复中一次性发起，禁止分轮串行发起。\n" +
			"- **Prompt 完备性**：给 `Task` 的 `prompt` 必须提供自包含的上下文与明确的交付目标。\n" +
			"\n" +
			"## 产出去向\n" +
			"子智能体的产出是**中间工作上下文**，用于支撑主线程后续的判断/写作/修改，不应原样呈现给用户；主线程需自行整合并转化为对用户有意义的结论或下一步行动。\n"
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

	b.WriteString("<examples>\n")
	b.WriteString("<example>\n")
	b.WriteString("user: \"看下 pkg/auth/jwt.go 里的 ValidateToken 函数怎么写的\"\n")
	b.WriteString("commentary: 已知精确路径和具体函数，属于单文件针尖查询，命中\"1. 范围否决\" → 直接 read_file，严禁 spawn。\n")
	b.WriteString("</example>\n\n")
	b.WriteString("<example>\n")
	b.WriteString("user: \"帮我梳理一下整个项目中多租户 (multi-tenant) 隔离逻辑都是怎么实现的\"\n")
	b.WriteString("commentary: 无精确路径，涉及跨模块宽泛摸排，命中\"2. 隔离与匹配委派\" → 发起单个 Task(subagent_type=\"explore\") 保护主上下文。\n")
	b.WriteString("</example>\n\n")
	b.WriteString("<example>\n")
	b.WriteString("user: \"帮我同时摸排一下数据库层 (db) 和缓存层 (cache) 的瓶颈问题\"\n")
	b.WriteString("commentary: 存在 2 个相互独立的探索目标，命中\"3. 并发修饰\" → 同一回复中一次性发起 2 个后台 Task(run_in_background=true)。\n")
	b.WriteString("assistant_tool_calls: [\n")
	b.WriteString("  Task(subagent_type=\"explore\", run_in_background=true, prompt=\"探索 db 模块的瓶颈与慢查询处理...\"),\n")
	b.WriteString("  Task(subagent_type=\"explore\", run_in_background=true, prompt=\"探索 cache 模块的缓存失效与内存使用...\")\n")
	b.WriteString("]\n")
	b.WriteString("</example>\n\n")
	b.WriteString("<example>\n")
	b.WriteString("user: \"我想为用户订阅模块设计一套全新的 RESTful API\"\n")
	b.WriteString("commentary: 任务匹配到专门的 API 设计专家 api-designer，命中\"2. 领域匹配\" → 主动委派 api-designer 执行。\n")
	b.WriteString("assistant_tool_calls: [\n")
	b.WriteString("  Task(subagent_type=\"api-designer\", prompt=\"设计用户订阅模块的 RESTful API 规范、错误码与数据结构...\")\n")
	b.WriteString("]\n")
	b.WriteString("</example>\n")
	b.WriteString("</examples>\n\n")

	b.WriteString("<available_agents>\n")
	for _, item := range agents {
		fmt.Fprintf(&b, "<agent name=%q when_to_use=%q>%s</agent>\n", item.Name, item.WhenToUse, item.Description)
	}
	b.WriteString("</available_agents>")
	return b.String(), nil
}
