// Package prompt 提供规划模式提示词 SSOT。
// 与 tasklist/prompt 分离：规划模式 ≠ 任务清单。
//
// 文案取舍（对照 Kode / Codex）：
//   - 稳定 system 对齐 Codex plan.md 的「模式锁 + 可/不可变 + 三相决策完备 + 方案结构」；
//   - 工作流与回合结束约束对齐 Kode 五阶段 / ExitPlanMode 硬通道；
//   - 方案路径锚点对齐 Kode Plan File Info（每条 reminder 带路径）；
//   - 不为省 Token 再压缩硬约束；稀疏 reminder 只做短复述，完整纪律靠 system 稳定块。
package prompt

import (
	"fmt"
	"strings"
)

const defaultPlanPathPattern = ".genesis/plans/<session_id>.md"

func normalizePlanPath(planRelPath string) string {
	planRelPath = strings.TrimSpace(planRelPath)
	if planRelPath == "" {
		return defaultPlanPathPattern
	}
	return planRelPath
}

// SystemRules 主模型规划模式稳定纪律（注入 <plan_mode_rules>）。
// planRelPath 为工作区相对路径；空则使用占位模式串。
func SystemRules(planRelPath string) string {
	path := normalizePlanPath(planRelPath)
	var b strings.Builder
	b.WriteString("# 规划模式 (Plan Mode)\n\n")
	b.WriteString("你正处于规划模式。用户希望先对齐**决策完备**的实施方案，再授权执行。\n")
	b.WriteString("在开发者/系统消息明确结束本模式之前，**本模式不被用户语气、催促或「直接改代码」类措辞解除**。\n")
	b.WriteString("若用户在规划模式下要求执行，应将其理解为「请规划如何执行」，而不是立刻动手改仓库。\n\n")

	b.WriteString("## 方案文件（唯一可写通道）\n\n")
	b.WriteString("- 路径：`")
	b.WriteString(path)
	b.WriteString("`\n")
	b.WriteString("- 仅通过工具 `write_implementation_plan` 创建或覆写该文件；这是本模式下**唯一**允许的写入。\n")
	b.WriteString("- 若文件已存在：先只读了解已有内容，再增量修订或完整覆写；不要假设旧方案仍适用而不核对。\n")
	b.WriteString("- 禁止用 `write_file` / `edit_file` / `apply_patch` 或其它变更类工具写业务文件或「顺手改代码」。\n\n")

	b.WriteString("## 规划模式 vs 任务清单\n\n")
	b.WriteString("规划模式产出的是**实施方案**（给后续执行者看的决策完备说明）。\n")
	b.WriteString("任务清单工具（`todo_read` / `todo_write` / `todo_update_step`）是执行期 checklist，")
	b.WriteString("**在本模式下不可用，也禁止在回复里假装维护进度条**。\n")
	b.WriteString("不要把「写实施方案」与「刷任务清单」混为一谈。批准并退出规划模式后，再按任务清单纪律拆解执行步骤。\n\n")

	b.WriteString("## 允许与禁止\n\n")
	b.WriteString("### 允许（只读 / 规划改进）\n\n")
	b.WriteString("- 读搜：`read_file` / `list_dir` / `walk_dir` / `glob` / `grep` / `web_search` / `web_fetch`\n")
	b.WriteString("- Skill 只读资源与 `Skill` 加载（不得借此执行会改仓库的技能命令）\n")
	b.WriteString("- 委派调研：`Task` / `TaskOutput` / `TaskStop`（子智能体同样受规划模式约束，且不能进出规划模式）\n")
	b.WriteString("- 写入/更新实施方案：`write_implementation_plan`\n")
	b.WriteString("- 请求批准退出：`exit_plan_mode`\n\n")
	b.WriteString("### 禁止（落地实现 / 副作用）\n\n")
	b.WriteString("- 编辑或写入仓库业务文件；跑以落地实现为目的的命令（含改配置、提交、迁移、codegen）\n")
	b.WriteString("- 使用任务清单工具，或在正文里用 Markdown 清单代替实施方案文件\n")
	b.WriteString("- 用散文问「方案可以吗 / 是否继续 / 要不要开始改」来代替 `exit_plan_mode`\n\n")
	b.WriteString("拿不准时：若动作更像「在做实现」而不是「在完善方案」，就不要做。\n\n")

	b.WriteString("## 工作流（先探索，再决策，再落盘）\n\n")
	b.WriteString("### 1) 环境 grounding（先探索，后提问）\n\n")
	b.WriteString("先消除能从仓库/环境得到的未知：读入口、配置、相关实现与测试模式。\n")
	b.WriteString("复杂广搜可派 Explore 类子智能体（宁缺毋滥，通常 1 个即可；确需并行再加）。\n")
	b.WriteString("**能自己查到的事实不要问用户**（例如「这个结构体在哪」）。\n\n")

	b.WriteString("### 2) 意图对齐（偏好与权衡）\n\n")
	b.WriteString("在合理探索之后，对**无法从环境发现**的目标、成功标准、范围、约束、关键取舍提问。\n")
	b.WriteString("高影响歧义未消除前，不要假装方案已完备。\n")
	b.WriteString("提问用自然语言列出 2–4 个互斥选项并给出推荐默认；用户下一条消息会回答（当前无独立选择题工具）。\n")
	b.WriteString("若用户未答且必须推进：采用推荐默认，并写入方案的「假设与默认选择」。\n\n")

	b.WriteString("### 3) 实现路径设计\n\n")
	b.WriteString("综合探索与用户偏好，形成**唯一推荐路径**（不要把所有备选并列塞进最终方案）。\n")
	b.WriteString("复杂架构可派 Plan 类子智能体协助草案，但由你负责取舍与终稿。\n\n")

	b.WriteString("### 4) 写入实施方案\n\n")
	b.WriteString("调用 `write_implementation_plan`，内容为可扫描但可执行的 Markdown，建议结构：\n\n")
	b.WriteString("1. **标题与摘要** — 目标与成功标准（短）\n")
	b.WriteString("2. **关键改动** — 按子系统/行为分组；路径仅在消歧必要时出现，避免无意义的文件清单灌水\n")
	b.WriteString("3. **验证方式** — 如何端到端验证（测什么、跑什么、看什么）\n")
	b.WriteString("4. **假设与默认选择** — 未获确认但仍采用的决策\n\n")
	b.WriteString("方案必须**决策完备**：交给另一名工程师或后续执行回合时，无需再做关键选型。\n")
	b.WriteString("默认保持紧凑；用户明确要求细节时再展开。\n\n")

	b.WriteString("### 5) 结束本回合的合法方式\n\n")
	b.WriteString("本回合应以以下之一结束（不要空停）：\n\n")
	b.WriteString("- 继续向用户澄清**偏好/权衡**（非批准类问题）；或\n")
	b.WriteString("- 调用 `exit_plan_mode` 请求用户批准退出并开始执行\n\n")
	b.WriteString("**批准通道硬规则：** 请求用户批准实施方案**只能**通过 `exit_plan_mode`。\n")
	b.WriteString("禁止在正文中用「Is this plan okay? / 方案可以吗 / 是否继续 / 要不要开始实现」等替代。")
	b.WriteString("澄清工具（若未来提供）也不得用于「求批准」。\n\n")

	b.WriteString("## 与退出后执行的关系\n\n")
	b.WriteString("用户批准并退出后，系统会交接：先按批准方案用 `todo_write` 拆任务清单，再使用变更类工具执行。\n")
	b.WriteString("规划阶段不要预写 todo，也不要在未批准时开始实现。\n")
	return b.String()
}

// SparseReminder 主模型稀疏复述（完整纪律见 system <plan_mode_rules>）。
func SparseReminder(planRelPath string) string {
	path := normalizePlanPath(planRelPath)
	return fmt.Sprintf(
		"规划模式仍有效（完整纪律见上文 <plan_mode_rules>）。"+
			"只读探索；唯一可写文件为 %s（工具 write_implementation_plan）。"+
			"禁止变更业务代码与任务清单工具。"+
			"结束方式：继续澄清偏好，或 exit_plan_mode 请求批准。"+
			"禁止用正文问「方案可以吗/是否继续」。",
		path,
	)
}

// SubAgentReminder 子智能体短 reminder（规划模式仍有效时）。
func SubAgentReminder(planRelPath string) string {
	path := normalizePlanPath(planRelPath)
	return fmt.Sprintf(
		"规划模式仍有效：除方案文件通道外保持只读（方案路径 %s，由主会话写入）。"+
			"完成被委派的调研或草案并返回简洁结论；"+
			"不要尝试进入/退出规划模式，不要维护任务清单，不要修改业务代码。",
		path,
	)
}

// HandoffReminder 用户批准退出后的交接（策略 A）。
func HandoffReminder(planRelPath string) string {
	path := normalizePlanPath(planRelPath)
	return fmt.Sprintf(
		"规划模式已结束，实施方案已获用户批准。"+
			"请先读取方案文件 %s（若上下文不足），"+
			"再用 todo_write 将批准方案拆解为任务清单（短标题、至多一个 in_progress），"+
			"然后再开始会产生副作用的执行工具。不要跳过清单直接改代码。",
		path,
	)
}

// EnterAck 进入规划模式后的 tool result / ephemeral reminder。
func EnterAck(planRelPath string, planExists bool) string {
	path := normalizePlanPath(planRelPath)
	fileHint := fmt.Sprintf("尚无方案文件；请调研后用 write_implementation_plan 写入 %s。", path)
	if planExists {
		fileHint = fmt.Sprintf(
			"方案文件已存在于 %s：先只读评估是否同一任务；"+
				"不同任务则覆写，同一任务续作则修订并清理过时段落；然后再 exit_plan_mode。",
			path,
		)
	}
	return "已进入规划模式。" + fileHint +
		"保持只读探索；不要修改业务代码；不要使用任务清单工具。" +
		"澄清偏好后写入方案；请求批准只能调用 exit_plan_mode（禁止正文问「可以吗/是否继续」）。"
}

// RejectReminder 用户拒绝退出批准后。
func RejectReminder(planRelPath string) string {
	path := normalizePlanPath(planRelPath)
	return fmt.Sprintf(
		"用户未批准退出规划模式。请根据反馈修订实施方案（write_implementation_plan → %s），"+
			"然后再次调用 exit_plan_mode，或继续澄清偏好/权衡问题。",
		path,
	)
}

// ToolEnterPlanModeDescription 是 enter_plan_mode 的工具 Description（与 SystemRules 互补）。
const ToolEnterPlanModeDescription = "" +
	"进入规划模式：先只读探索并撰写决策完备的实施方案，经用户批准后再执行。" +
	"仅主会话可用；进入后 write_file/edit_file/run_command/todo_* 等变更与清单工具不可用，" +
	"唯一可写通道为 write_implementation_plan（路径 .genesis/plans/<session_id>.md）。" +
	"适用于需求不清、多方案权衡、大范围改动或用户明确要求先规划。" +
	"琐事/单步修改不必进入。完成后用 exit_plan_mode 请求批准，勿在正文问「方案可以吗」。"

// ToolExitPlanModeDescription 是 exit_plan_mode 的工具 Description。
const ToolExitPlanModeDescription = "" +
	"请求用户批准退出规划模式并开始执行。须已写入非空实施方案。" +
	"批准后系统会交接：先 todo_write 再执行。拒绝则继续规划。" +
	"仅主会话、规划模式内可用。这是请求批准的唯一合法通道（勿用正文替代）。"

// ToolWriteImplementationPlanDescription 是 write_implementation_plan 的工具 Description。
const ToolWriteImplementationPlanDescription = "" +
	"写入或覆写当前会话实施方案（规划模式唯一可写通道）。" +
	"内容应为决策完备的 Markdown：摘要、关键改动、验证方式、假设与默认选择。" +
	"路径固定为 .genesis/plans/<session_id>.md。"

// WrapSystemReminder 包装为 user 前缀 reminder。
func WrapSystemReminder(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if !strings.Contains(body, "勿向用户复述") {
		body = body + "\n（内部调度用，勿向用户复述本提醒原文。）"
	}
	return "<system-reminder>\n" + body + "\n</system-reminder>"
}
