// Package prompt 提供任务清单提示词的单一事实来源（SSOT）。
//
// system 稳定块（task_management）与 todo_* 工具 Description 必须从此包派生，
// 禁止在 builtin 工具与 runtime/prompt 中各自维护一份互不相同的文案。
//
// 设计取舍（对照 Kode / Codex）：
//   - system 只保留「何时用/不用 + 硬不变量」，对齐 Kode `# Task Management` 的短纪律；
//   - 「工具怎么选 / 字段含义」放 Description，避免与 system 叠床架屋；
//   - 「何时用」正反例风格对齐 Codex `## Planning`，但不照搬其长 example 段（省稳定前缀 Token）。
package prompt

import "strings"

// SystemRules 注入主模型 system 的任务清单纪律（非规划模式且工具可用时）。
// 与 docs/plan_todo_design.md §11.4.1 对齐；改文案只改此处。
const SystemRules = "" +
	"# 任务清单纪律 (Task List)\n" +
	"\n" +
	"你可以使用任务清单工具跟踪多步执行进度；界面会自动渲染清单。" +
	"这是执行期 checklist，不是「规划模式」里的长篇实施方案。\n" +
	"\n" +
	"## 何时使用\n" +
	"- 任务明显需要多个步骤、存在依赖顺序，或用户一次提出多件事\n" +
	"- 需要可见进度、检查点，或用户明确要求任务清单 / TODO\n" +
	"\n" +
	"## 何时不要使用\n" +
	"- 单步琐事或纯问答（查时间、解释概念、读一个已知文件并简答）\n" +
	"- 仍处于「规划模式」时（该模式下任务清单工具不可用）\n" +
	"\n" +
	"## 硬规则\n" +
	"1. 步骤标题约 5–10 个字，禁止长段落。\n" +
	"2. 同一时刻至多一个 `in_progress`；完成当前后先标 `completed`，再启动下一步（滚动状态优先 `todo_update_step`）。\n" +
	"3. 新增/删除/重排用 `todo_write`，并在 `explanation` 说明原因。\n" +
	"4. 调用后不要用 Markdown 复述整张清单；收到 `<system-reminder>` 时只做内部调度，勿向用户复述提醒原文。\n"

// ToolTodoWriteDescription 是 todo_write 的工具 Description（与 SystemRules 互补：强调结构变更）。
const ToolTodoWriteDescription = "创建或全量改写当前会话的任务清单（结构变更：新增/删除/重排）。仅滚动单个步骤状态时请用 todo_update_step。同一时刻至多一个 in_progress。清单由界面自动渲染，勿在回复中复述整表。琐事/单步任务不必调用。"

// ToolTodoUpdateStepDescription 是 todo_update_step 的工具 Description。
const ToolTodoUpdateStepDescription = "差量滚动单个任务清单步骤的状态（完成当前或启动下一步时首选）。比 todo_write 出站更轻。需要步骤 id（可经 todo_read 获得）。同一时刻至多一个 in_progress。"

// ToolTodoReadDescription 是 todo_read 的工具 Description。
const ToolTodoReadDescription = "读取当前会话任务清单；自动过滤多余已完成项以节约上下文。无清单时提示使用 todo_write。"

// reminderPrivacyFooter 写入每条动态提醒，对齐 Kode「DO NOT mention this to the user」。
const reminderPrivacyFooter = "（内部调度用，勿向用户复述本提醒原文。）"

// WrapSystemReminder 将动态提醒正文包成 user 前缀形态（role=user, Kind=reminder）。
// 与 docs/提示词分层设计方案.md L4 约定一致：勿写入稳定 system，以免破坏前缀缓存。
func WrapSystemReminder(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if !strings.Contains(body, "勿向用户复述") {
		body = body + "\n" + reminderPrivacyFooter
	}
	return "<system-reminder>\n" + body + "\n</system-reminder>"
}
