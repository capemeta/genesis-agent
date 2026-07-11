# Skill 调用原理对比：Kode / Codex / Genesis

> 日期：2026-07-11  
> 目的：说明大模型如何「发现 → 选择 → 加载 → 执行」技能，对比 Kode-CLI、Codex 与本项目（Genesis）三条实现路径。  
> 协议细节以 `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md` 为准；本文侧重原理与对照，不替代专项设计。  
> 触发模式（显式 / 意图 / Agent 自判）与缺口实现路径：`docs/superpowers/specs/2026-07-11-skill-trigger-modes-design.md`。

---

## 一、先分清：Tool 调用 ≠ Skill 调用

| | 普通 Tool | Skill |
| --- | --- | --- |
| 本质 | 可执行原语（读文件、跑命令、写文件…） | 任务知识 / 流程包（`SKILL.md` + references/scripts） |
| LLM 初始看到什么 | function schema（工具名 + 参数） | **目录**（name + description + locator），正文默认不进 schema |
| 模型「调用」后发生什么 | 运行时执行副作用，回 ToolResult | 把说明书注入上下文；真正干活仍靠原语 Tool |
| 失败时常见误解 | — | 把 `office-ppt` 当成 Tool 名直接调用 |

三条实现都遵守 **渐进披露（progressive disclosure）**：先目录、后正文。差别在于「模型用什么协议表达『我要用这个技能』」。

---

## 二、总览对比

| 维度 | Kode-CLI | Codex | Genesis（本项目） |
| --- | --- | --- | --- |
| 主入口 | 专用工具 `Skill` | **无**专用 Skill 工具；读 `SKILL.md` 或 `$mention` 注入 | 专用工具 `Skill`（对齐 Kode） |
| Catalog 挂载位置 | `Skill` 工具 `prompt()` / description | developer 上下文片段 `<skills_instructions>` | `Skill` 工具 `DescriptionFunc` + system 短硬规则 |
| 模型如何表达「要用技能」 | `tool_call: Skill(skill, args)` | 用户 `$SkillName` / 结构化 mention；或模型自己 `read` 路径 | `tool_call: Skill(skill, args[, resource])` |
| 正文如何进对话 | 工具执行后 `newMessages`（inline）或 fork 子 Agent | mention 时宿主注入 `<skill>`；或模型读文件结果 | Runtime 单份 `<skill_injection>`；ToolResult 只短确认 |
| 误把 skill 名当 tool | 依赖提示词约束 | 依赖提示词 + 读文件路径 | **CollisionGuard**（可 `auto_rewrite`） |
| 权限 / 审批 | `Skill(name)` / `Skill(ns:*)` | 偏文件系统与策略层 | Approval + ToolGateway + `allowed-tools` 收窄 |
| 依赖治理 | `allowed-tools` | MCP 安装提示为主 | 依赖预检 + `install_skill_dependencies` + Profile 求交 |
| Fork / 子 Agent | `context: fork` 已支持 | 子 Agent 可读技能但主路径不委托读说明书 | `context=fork` 字段预留，当前拒绝半吊子实现 |
| Mention UX | slash `/skill` 与 Skill 工具并存 | `$skill` 一等公民 | `SelectForTurn` / `$skill` 已接线，**不替代** `Skill` 网关 |
| 路径暴露 | 可含本地 `location` | 常暴露 `file:` 绝对路径或 alias | **禁止**宿主机绝对路径；opaque locator（如 `embedded:office-ppt`） |
| 参考源码 | `Kode-CLI/.../SkillTool/SkillTool.tsx` | `codex-rs/core-skills`、`available_skills_instructions.rs` | `internal/capabilities/skill/tool/skill/tool.go`、`react_loop.go` |

**Genesis 选型一句话**：协议主路径对齐 Kode（唯一网关 `Skill`）；吸收 Codex 的 catalog 预算、mention、多 source；并强化 Approval、依赖预检与 CollisionGuard。

---

## 三、端到端链路（三方对照）

```text
┌─────────────────────────────────────────────────────────────────┐
│ 1. 发现（Catalog）                                                │
│    Kode / Genesis: 挂在 Skill 工具 description 的 <available_skills> │
│    Codex:          挂在 developer 消息的 ## Skills 列表            │
└───────────────────────────────┬─────────────────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. 选择（模型或用户）                                              │
│    Kode / Genesis: 模型发 Skill(skill="...")                      │
│    Codex:          用户 $mention，或模型决定去读 SKILL.md          │
└───────────────────────────────┬─────────────────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. 加载（宿主）                                                    │
│    读 SKILL.md → 注入对话（inline）/ fork（Kode）/ mention 注入（Codex）│
│    Genesis: Resolve → 依赖预检 → Approval → Load → <skill_injection> │
└───────────────────────────────┬─────────────────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. 执行（仍是原语 Tool）                                           │
│    模型按说明书调用 read_file / run_command / run_skill_script …   │
│    Skill 调用本身 ≠ 脚本已执行                                     │
└─────────────────────────────────────────────────────────────────┘
```

---

## 四、Kode-CLI：专用 `Skill` 工具

### 4.1 提示词如何引导

`SkillTool.prompt()` 动态拼装说明 + 目录（约 8KB 字符预算）。

**英文原文（节选）与中文：**

| 英文 | 中文 |
| --- | --- |
| Execute a skill within the main conversation | 在主对话中执行一个技能 |
| When users ask you to perform tasks, check if any of the available skills below can help complete the task more effectively. Skills provide specialized capabilities and domain knowledge. | 用户提出任务时，先检查下面可用技能是否能更有效完成。技能提供专门能力与领域知识。 |
| When users ask you to run a "slash command" or reference "/\<something\>" … they are referring to a skill. Use this tool to invoke the corresponding skill. | 用户说 slash 命令或 `/xxx` 时，指的就是技能；请用本工具调用。 |
| When a skill is relevant, you must invoke this tool IMMEDIATELY as your first action | 技能相关时，必须立刻调用本工具作为第一步 |
| NEVER just announce or mention a skill in your text response without actually calling this tool | 禁止只口头提技能而不真正调用本工具 |
| Only use skills listed in `<available_skills>` below | 只能使用下面 `<available_skills>` 中列出的技能 |
| Do not invoke a skill that is already running | 不要调用已在运行的技能 |

目录条目形态：

```xml
<skill>
  <name>pdf</name>
  <description>...</description>
  <location>/path/to/SKILL.md</location>
</skill>
```

### 4.2 模型如何返回

与普通 function calling 相同，工具名固定为 `Skill`：

```json
{
  "name": "Skill",
  "arguments": {
    "skill": "pdf",
    "args": "-m 'Fix bug'"
  }
}
```

**不会**把 `pdf` 注册成独立 tool name。

### 4.3 返回后如何执行

1. `validateInput`：技能存在、允许模型调用、类型为 prompt 技能。  
2. `getPromptForCommand(args)`：读 `SKILL.md`，前缀 `Base directory for this skill: …`，替换 `$ARGUMENTS`。  
3. 两条路径：  
   - **inline**：`newMessages` 把正文当 user 消息注入当前对话；可合并 `allowedTools` / model。  
   - **fork**：把正文交给 Task/子 Agent 跑完再回传。  
4. 下一轮模型按说明书调用 Bash / Read / 跑 `scripts/` 等。

关键实现：`packages/tools/src/tools/interaction/SkillTool/SkillTool.tsx`。

---

## 五、Codex：目录注入 + 读文件 / `$mention`

### 5.1 提示词如何引导

会话注入 developer 片段（`AvailableSkillsInstructions`），结构大致为：

```text
## Skills
（简介：技能是 SKILL.md 中的一组说明…）
### Skill roots   （可选 alias 表）
### Available skills
- pdf: 处理 PDF… (file: /.../pdf/SKILL.md)
### How to use skills
（触发规则 + 渐进披露步骤）
```

**How to use（英文要点 → 中文）：**

| 英文要点 | 中文 |
| --- | --- |
| Discovery: list above is skills available (name + description + locator) | 发现：上面列表是本会话可用技能（名 + 描述 + 定位符） |
| Trigger: If the user names a skill (`$SkillName` or plain text) OR the task clearly matches a skill's description, you must use that skill for that turn | 触发：用户点名或任务明显匹配描述 → 本回合必须用该技能 |
| Do not carry skills across turns unless re-mentioned | 除非再次提及，不要跨回合自动延续技能 |
| After deciding to use a skill, the main agent must read its `SKILL.md` completely before taking task actions | 决定使用后，主 Agent 必须先完整读完 `SKILL.md` 再做任务动作 |
| Prefer running or patching provided scripts instead of retyping large code blocks | 优先运行或修补已有脚本，不要重打大段代码 |
| Do not delegate reading/summarizing skill instructions to a subagent | 不要把读/总结技能说明书委托给子 Agent |
| Progressive disclosure applies to selecting relevant files, not partially reading a selected instruction file | 渐进披露用于选择相关文件，不是对已选说明书只读一半 |

关键实现：`codex-rs/core-skills/src/render.rs`（`SKILLS_HOW_TO_USE_*`）、`core/src/context/available_skills_instructions.rs`。

### 5.2 模型如何「返回 / 使用」

Codex **没有** `Skill(skill=...)` function call。两条主路径：

**A. 用户显式 `$pdf` / 结构化 `UserInput::Skill`**  
宿主在 turn 开始读 `SKILL.md`，注入：

```xml
<skill>
  <name>pdf</name>
  <path>/.../SKILL.md</path>
  （正文）
</skill>
```

模型本回合开头已看到正文。

**B. 模型自行判断匹配**  
按 How to use：用普通读文件工具打开列表中的路径（或 orchestrator 的 `skills.read`），读完再按正文执行。

### 5.3 返回后如何执行

正文进入上下文后，模型用 shell / 文件等工具按 `SKILL.md` 步骤工作。Skill 仍不是可执行原语。

关键实现：`codex-rs/core-skills/src/injection.rs`、`core/src/session/turn.rs`。

---

## 六、Genesis（本项目）：`Skill` 网关 + 治理增强

### 6.1 提示词如何引导

**双通道，避免双份膨胀：**

1. **System 短硬规则**（产品 bootstrap 注入，约数行），例如 CLI：  
   > Skills 是任务流程包，不是可执行工具。加载技能必须调用 `Skill(skill=...)`；禁止把 `office-ppt` 等技能名当作独立工具调用。用户输入中的 `$skill` 或 `skill://` 引用会在回合开始自动注入。可用技能列表见 Skill 工具描述中的 `<available_skills>`。…

2. **Catalog 主通道**：挂在 `Skill` 工具的 `DescriptionFunc`，每轮绑定 LLM 前刷新：

```text
加载已发现 Skill 的完整说明。参数 skill 必须来自本工具 description 中的 <available_skills>。禁止把技能名当作独立工具调用。

<skills_instructions>
当任务匹配可用技能时，必须先调用 Skill 工具加载该技能，再使用原语工具执行。
禁止把技能名当作独立工具名调用。例如禁止调用 office-ppt；正确做法是 Skill(skill="office-ppt")。
</skills_instructions>

<available_skills>
  <skill>
    <name>office-ppt</name>
    <description>...</description>
    <location>embedded:office-ppt</location>
  </skill>
</available_skills>
```

预算：默认约 8KB 字符，可与约 2% context token 取更严者；超出截断并标注 `Showing X of Y`。`location` 只暴露 opaque locator。

关键实现：`internal/capabilities/skill/tool/skill/tool.go`（`renderDescription`）、`products/cli/bootstrap/container.go`（短规则）。

### 6.2 模型如何返回

```json
{
  "name": "Skill",
  "arguments": {
    "skill": "office-ppt",
    "args": "做一份竞品对比 PPT",
    "resource": "可选，同名消歧"
  }
}
```

function schema 中**只有** Tool 名（含网关 `Skill`），**没有** `office-ppt`。

若模型仍误调用 `office-ppt`：

- CollisionGuard 识别为 `skill_tool_collision`；  
- 默认可 `auto_rewrite` 为 `Skill(skill="office-ppt")`；  
- 不得仅返回「未被 Profile 允许」这类假原因。

### 6.3 返回后如何执行

```text
Skill.Execute
  → Resolve(ModelCall=true)
  → 依赖预检（tools / 外部依赖；与 EnabledTools ∩ Profile）
  → Approval(Skill(name) / 外部依赖)
  → Load(SKILL.md + args)
  → ToolResult：短确认 JSON（name / allowed_tools / dependencies…；模型侧不依赖完整 body）
  → React Loop：追加单份 <skill_injection> 承载正文
  → 若 allowed_tools 非空：与当前可见集求交后 Gateway.FilterInfos（只收窄不扩权）
  → 同轮若混有其他 tool call：Skill 独占轮，其余跳过
  → 下一轮：模型按说明书调用 write_file / run_command / run_skill_script …
```

补充行为：

| 行为 | 说明 |
| --- | --- |
| `already_loaded` | 同轮/已注入技能再次 `Skill` 时去重，避免重复灌正文 |
| Mention / `SelectForTurn` | 用户 `$skill` 可在回合开始注入；**不替代**网关，模型主动加载仍走 `Skill` |
| `context=fork` | 元数据可声明；当前明确报错，待规范化 subagent 运行时 |
| 脚本执行 | 走 `run_skill_script` 等原语，带 failure_kind / 依赖闭环，与「加载 Skill」分离 |

关键实现：`tool/skill/tool.go`、`runtime/strategy/react/react_loop.go`、`collision/collision.go`、`mention_selector.go`。

### 6.4 与 Kode / Codex 的增量

相对 Kode，Genesis 额外强调：

- ToolResult 与 injection **单份正文**（避免双份膨胀）；  
- CollisionGuard + 可选自动改写；  
- 依赖预检 / 安装闭环 / Approval 与多产品 Profile；  
- opaque locator（企业/沙箱不暴露宿主机路径）；  
- fork 不半吊子落地。

相对 Codex，Genesis **不**把「模型自己 read_file 读 SKILL.md」当作主加载路径（避免绕过 Approval）；主路径必须经 `Skill` 网关。

---

## 七、同一场景下的行为差异（示例）

用户：「帮我做一份 PPT」

| 步骤 | Kode | Codex | Genesis |
| --- | --- | --- | --- |
| 发现 | 模型在 `Skill` description 看到 `office-ppt`（或同类） | 模型在 developer Skills 列表看到条目 | 同 Kode：`<available_skills>` 含 `office-ppt` |
| 选择 | `Skill(skill="office-ppt")` | 可能 `$office-ppt`，或 `read` 列表中的 path | `Skill(skill="office-ppt")`；误调 `office-ppt` 可被改写 |
| 加载 | 注入 SKILL.md 到对话 | mention 注入或读文件结果进上下文 | Approval + Load → `<skill_injection>` |
| 执行 | 按说明书用原语工具 | 同左 | 同左；脚本走 `run_skill_script` 等治理路径 |

---

## 八、设计取舍小结

| 问题 | Genesis 答案 | 主要参考 |
| --- | --- | --- |
| Skill 要不要进 function schema？ | 不要；只进 Catalog | Kode I2/I3 |
| 唯一加载入口？ | `Skill(skill=...)` | Kode |
| Catalog 放哪？ | `Skill` DescriptionFunc；system 只留短规则 | Kode + 防双份 |
| Mention？ | 增强 UX，不替代网关 | Codex（吸收） |
| 模型自读 SKILL.md 当主路径？ | 否（绕过治理） | 相对 Codex 的刻意差异 |
| 误调用 skill 名？ | CollisionGuard | Genesis 自研增强 |
| 正文如何披露？ | 加载后单份 injection | Kode inline + Genesis 去双份 |

---

## 九、相关文档与源码

| 文档 / 代码 | 用途 |
| --- | --- |
| `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md` | Skill/Tool 协议边界（权威） |
| `docs/superpowers/specs/2026-07-11-skill-trigger-modes-design.md` | 触发模式对照 + Genesis 缺口与实现顺序 |
| `docs/Skills设计.md` | Skills 系统总设计 |
| `docs/Skill三模式执行与依赖闭环设计.md` | 脚本执行与依赖闭环 |
| `internal/capabilities/skill/tool/skill/tool.go` | Genesis `Skill` 网关 |
| `internal/runtime/strategy/react/react_loop.go` | injection / CollisionGuard / 独占轮 |
| `Kode-CLI/.../SkillTool/SkillTool.tsx` | Kode 参考实现 |
| `codex-rs/core-skills/src/render.rs` | Codex How to use / catalog 渲染 |
| `codex-rs/core-skills/src/injection.rs` | Codex mention 注入 |
