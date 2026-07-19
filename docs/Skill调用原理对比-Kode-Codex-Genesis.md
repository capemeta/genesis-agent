# Skill 调用原理对比：Kode / Codex / Genesis

> 日期：2026-07-14（初版 2026-07-11）  
> 目的：说明大模型如何「发现 → 选择 → 加载 → 执行」技能，对比 Kode-CLI、Codex（`go-project/codex`）与本项目（Genesis）三条实现路径；并对照 DeepSeek 网页版问答给出的「强制 `tool_choice` + 彻底替换 System Prompt」方案，说明其与真实实现的差异。  
> 协议细节以 `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md` 为准；本文侧重原理、报文级行为与字面提示词，不替代专项设计。  
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

## 二、对照方案：DeepSeek 网页版问答给出的「强制路由 + 替换 System」链路

> **来源**：DeepSeek 网页版问答给出的 Skill 调用方案（教学/示意性报文闭合链路，非 Kode / Codex / Genesis 源码实现）。  
> **用途**：下文用该方案作对照基线，核对真实产品是否按此实现。

该方案的核心主张：

1. System Prompt 若不强制「必须以工具形式返回」，模型可以自由闲聊。  
2. 因此 Round1 必须用 `tool_choice` 锁死唯一路由工具（方案中名为 `use_skill`）。  
3. 步骤「加载」是编排器本地内存操作（读 `SKILL.md`、注册新工具），无 LLM 网络请求。  
4. Round3（重入）**彻底替换 System Prompt** 为完整说明书，追加业务工具（如 `calculate`），并取消强制 `tool_choice`。  
5. Round4 执行业务工具，回 ToolResult，再出自然语言。

摘要步骤：

1. Round1：`tool_choice` 强制 `use_skill`，System 里只有技能菜单（name + description）。  
2. 本地读 `SKILL.md`、注册新工具（无报文）。  
3. Round3：**彻底替换 System Prompt** 为完整说明书，取消强制 `tool_choice`。  
4. Round4：执行业务工具并回 ToolResult。

方案侧工具 Schema 示意（OpenAI Function Calling）：

```json
{
  "tools": [{
    "type": "function",
    "function": {
      "name": "use_skill",
      "description": "激活并加载一个技能（Skill）来处理用户的问题。",
      "parameters": {
        "type": "object",
        "properties": {
          "skill_name": {
            "type": "string",
            "enum": ["calculator", "weather"],
            "description": "要激活的技能名称"
          }
        },
        "required": ["skill_name"]
      }
    }
  }],
  "tool_choice": { "type": "function", "function": { "name": "use_skill" } }
}
```

方案侧轮次对照表：

| 轮次 | 请求中的 System Prompt | 请求中的 Tools | Tool Choice | 响应内容 |
| --- | --- | --- | --- | --- |
| 第1轮 | 只有技能菜单（name+desc） | `[use_skill]` | **强制** `use_skill` | `tool_calls: use_skill(skill_name=...)` |
| 第3轮 | **完整替换**为 SKILL.md 指令 | 业务工具（如 `[calculate]`） | **无**（自由选择） | `tool_calls: calculate(...)` |
| 第4轮 | 同上（完整说明书） | 同上 | **无** | 自然语言 `content` |

### 2.1 DeepSeek 方案 vs 三处真实实现

| 关键点 | DeepSeek 网页版方案 | Kode-CLI | Codex（go-project 主运行时） | Genesis（本项目） |
| --- | --- | --- | --- | --- |
| 网关工具名 | `use_skill` | `Skill` | **无** host 网关；orchestrator 用 `skills.list` / `skills.read` | `Skill`（`load_skill` 已移除） |
| Round1 强制 | `tool_choice` 锁死路由工具 | `tool_choice: auto` | `tool_choice: "auto"` | 配置可有 `auto`，**LLM 适配层当前未传** `tool_choice` |
| Catalog 位置 | System 技能菜单 | Anthropic：工具 `description`；OpenAI 路径常退化成短描述 | **developer** 消息 `<skills_instructions>` | `Skill` 工具 `DescriptionFunc` + System **短规则** |
| SKILL.md 进上下文 | **替换** System | **追加 user** 消息 | **追加 user** `<skill>` | **追加 user** `<skill_injection>` |
| 原 System / base instructions | 被扔掉 | **保留** | **保留** `base_instructions` | **保留** `BuildSystem` 结果 |

**结论**：DeepSeek 网页版方案在报文教学上自洽，但与 Kode / Codex / Genesis **都不一致**。尤其「第三步彻底替换 System Prompt」：三处实现均为 **NO**——原人格 / 基线 system 始终保留，说明书以**追加消息**进入上下文。

### 2.2 Genesis 真实报文视角（意图匹配路径）

```text
Round N:
  messages = [初始 system, user, ...]
  tools    = [Skill, write_file, run_skill_command, ...]
  // 无 forced tool_choice；是否调 Skill 靠 prompt + 工具 description
→ model: tool_calls Skill(skill="office-ppt")

宿主本地:
  Resolve → 依赖预检 → Approval → Load(SKILL.md)
  // 无「替换 system」；追加 tool 短确认 + user <skill_injection>
  // 可按 allowed_tools 收窄本轮可见 tools

Round N+1:
  messages = [初始 system（未改）, ..., assistant(Skill), tool(短确认), user(<skill_injection>), ...]
  tools    = 可能已收窄
→ model: 再调 write_file / run_skill_command / ...
```

Mention 路径（`$office-ppt` / `skill://...`）：首轮 LLM **之前**直接追加 `<skill_injection>` **user** 消息，**不伪造** `tool_call` / `tool_result`。

---

## 三、总览对比

| 维度 | Kode-CLI | Codex | Genesis（本项目） |
| --- | --- | --- | --- |
| 主入口 | 专用工具 `Skill` | **无**专用 Skill 工具；读 `SKILL.md` 或 `$mention` 注入 | 专用工具 `Skill`（对齐 Kode） |
| Catalog 挂载位置 | `Skill` 工具 `prompt()` / description | developer 上下文片段 `<skills_instructions>` | `Skill` 工具 `DescriptionFunc` + system 短硬规则 |
| 模型如何表达「要用技能」 | `tool_call: Skill(skill, args)` | 用户 `$SkillName` / 结构化 mention；或模型自己 `read` 路径 | `tool_call: Skill(skill, args[, resource])` |
| 正文如何进对话 | 工具执行后 `newMessages`（inline）或 fork 子 Agent | mention 时宿主注入 `<skill>`；或模型读文件结果 | Runtime 单份 `<skill_injection>`（`role=user` + **`Kind=skill_injection`**）；ToolResult 只短确认 |
| 语义标记 / UI | 无独立 Kind；正文常可见 | contextual user：进模型、默认不进聊天气泡 | **`Message.Kind`** + `ForUI`/`ForModel`（对齐 Codex 意图，见会话管理方案 §6.2） |
| 误把 skill 名当 tool | 依赖提示词约束 | 依赖提示词 + 读文件路径 | **CollisionGuard**（可 `auto_rewrite`） |
| 权限 / 审批 | `Skill(name)` / `Skill(ns:*)` | 偏文件系统与策略层 | Approval + ToolGateway + `allowed-tools` 收窄 |
| 依赖治理 | `allowed-tools` | MCP 安装提示为主 | 依赖预检 + `install_skill_dependencies` + Profile 求交 |
| Fork / 子 Agent | `context: fork` 已支持 | 子 Agent 可读技能但主路径不委托读说明书 | `context=fork` 经 `Delegator`→Controller；`skill_isolated`；见子智能体设计 §4.4 |
| Mention UX | slash `/skill` 与 Skill 工具并存 | `$skill` 一等公民 | `SelectForTurn` / `$skill` 已接线，**不替代** `Skill` 网关 |
| 路径暴露 | 可含本地 `location` | 常暴露 `file:` 绝对路径或 alias | **禁止**宿主机绝对路径；opaque locator（如 `embedded:office-ppt`） |
| 参考源码 | `Kode-CLI/.../SkillTool/SkillTool.tsx` | `codex-rs/core-skills`、`available_skills_instructions.rs` | `internal/capabilities/skill/tool/skill/tool.go`、`react_loop.go`、`skill_support.go` |

**Genesis 选型一句话**：协议主路径对齐 Kode（唯一网关 `Skill`）；吸收 Codex 的 catalog 预算、mention、contextual 语义；说明书以 **user + `Kind=skill_injection`** 追加，不换掉人格 system；短硬规则仍在初始 system。

---

## 三.1 Genesis `Message.Kind`（与会话/记忆方案对齐）

> 权威契约：`docs/会话管理与记忆管理设计方案.md` §6.2。实现：`internal/domain/message.go`。

| Kind | Role | 进模型（ForModel） | 默认聊天气泡（ForUI） |
| --- | --- | --- | --- |
| `user_turn` | user | ✅ | ✅ |
| `skill_injection` | user | ✅ | ❌ |
| `tool_result` | tool | ✅ | ❌（默认可展开） |
| `assistant` | assistant | ✅ | ✅ |
| `system` | system | ✅ | ❌ |

- Skill 网关路径：`Source=skill_gateway`；mention 路径：`Source=skill_mention`。  
- **不**采用必填 `Origin`（与 Kind 冗余）。  
- 多 Skill 同 Run 累加：本阶段允许，后续靠会话压缩回收。

---

## 四、端到端链路（三方对照）

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
│ 3. 加载（宿主）——追加注入，不替换基线 System                        │
│    Kode:    newMessages → user 消息 + ToolResult「Launching…」   │
│    Codex:   user 消息 <skill>…</skill>                           │
│    Genesis: tool 短确认 + user <skill_injection>（Kind=skill_injection）│
└───────────────────────────────┬─────────────────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. 执行（仍是原语 Tool）                                           │
│    模型按说明书调用 read_file / run_command / run_skill_command …   │
│    Skill 调用本身 ≠ 脚本已执行                                     │
└─────────────────────────────────────────────────────────────────┘
```

---

## 五、Kode-CLI：专用 `Skill` 工具

### 5.1 Catalog / 工具 prompt（字面量）

源码：`Kode-CLI/packages/tools/src/tools/interaction/SkillTool/SkillTool.tsx`（`prompt()`）。

```text
Execute a skill within the main conversation

<skills_instructions>
When users ask you to perform tasks, check if any of the available skills below can help complete the task more effectively. Skills provide specialized capabilities and domain knowledge.

When users ask you to run a "slash command" or reference "/<something>" (e.g., "/commit", "/review-pr"), they are referring to a skill. Use this tool to invoke the corresponding skill.

<example>
User: "run /commit"
Assistant: [Calls Skill tool with skill: "commit"]
</example>

How to invoke:
- Use this tool with the skill name and optional arguments
- Examples:
  - `skill: "pdf"` - invoke the pdf skill
  - `skill: "commit", args: "-m 'Fix bug'"` - invoke with arguments
  - `skill: "review-pr", args: "123"` - invoke with arguments
  - `skill: "ms-office-suite:pdf"` - invoke using fully qualified name

Important:
- When a skill is relevant, you must invoke this tool IMMEDIATELY as your first action
- NEVER just announce or mention a skill in your text response without actually calling this tool
- This is a BLOCKING REQUIREMENT: invoke the relevant Skill tool BEFORE generating any other response about the task
- Only use skills listed in <available_skills> below
- Do not invoke a skill that is already running
- Do not use this tool for built-in CLI commands (like /help, /clear, etc.)
</skills_instructions>

<available_skills>
{动态菜单，约 8KB 字符预算}
</available_skills>
```

目录条目形态：

```xml
<skill>
<name>pdf</name>
<description>...</description>
<location>/path/to/SKILL.md</location>
</skill>
```

**注意**：Anthropic native 路径把完整 `prompt()` 放进工具 description；OpenAI Chat Completions 路径常用短描述 `"Execute a skill"`，菜单可能丢失。

主 System Prompt（`packages/core/src/constants/prompts.ts` 的 `getSystemPrompt()`）**不含** `<available_skills>`，与 Skill 加载无关。

### 5.2 模型如何返回

```json
{
  "name": "Skill",
  "arguments": {
    "skill": "pdf",
    "args": "-m 'Fix bug'"
  }
}
```

`tool_choice` 始终为 `auto`，「必须立刻调用」仅是 prompt 软约束。

### 5.3 返回后如何执行

1. `validateInput`：技能存在、允许模型调用、类型为 prompt 技能。  
2. `getPromptForCommand(args)`：读 `SKILL.md`，前缀 `Base directory for this skill: …`，替换 `$ARGUMENTS`。  
3. 两条路径：  
   - **inline**：`newMessages` 把正文当 **user** 消息注入；可合并 `allowedTools` / model。  
   - **fork**：把正文交给 Task/子 Agent 跑完再回传。  
4. Tool result 给模型：`Launching skill: {commandName}`（fork 另有完成摘要）。  
5. 下一轮模型按说明书调用 Bash / Read / 跑 `scripts/` 等；**System Prompt 不变**。

用户直接 `/skill`：可绕过 `Skill` 工具，在 `processUserInput` 路径展开 meta + SKILL.md user 消息。

---

## 六、Codex：目录注入 + 读文件 / `$mention`

`go-project` 主 Agent 运行时在 `codex/codex-rs`（Rust），不是 Kode-CLI。全仓库无 `use_skill`。

### 6.1 Catalog（developer 消息，字面量要点）

源码：`codex-rs/core-skills/src/render.rs`。

Intro（绝对路径模式）：

```text
A skill is a set of instructions provided through a `SKILL.md` source. Below is the list of skills that can be used. Each entry includes a name, description, and source locator. `file` locators are on the host filesystem, `environment resource` locators are owned by an execution environment, `orchestrator resource` locators are opaque non-filesystem resources, and `custom resource` locators use their provider's access mechanism.
```

组装形态：

```text
<skills_instructions>
## Skills
{intro}
### Skill roots   （可选 alias 表）
### Available skills
- {name}: {description} (file: {path})
</skills_instructions>
```

可选「How to use skills」（多数模型默认 `include_skills_usage_instructions=false`）关键句：

```text
- Trigger rules: If the user names a skill (with `$SkillName` or plain text) OR the task clearly matches a skill's description shown above, you must use that skill for that turn. ...
- How to use a skill (progressive disclosure):
  1) After deciding to use a skill, the main agent must read its `SKILL.md` completely before taking task actions. ...
  ...
```

`tool_choice` 在 `codex-rs/core/src/client.rs` 固定为 `"auto"`。`base_instructions` 与 skill 无关，**不被替换**。

### 6.2 模型如何「返回 / 使用」

Codex **没有** `Skill(skill=...)` function call（host 路径）。两条主路径：

**A. 用户显式 `$pdf` / 结构化 `UserInput::Skill`**  
宿主在 turn 开始读 `SKILL.md`，以 **user** 角色注入：

```xml
<skill>
<name>pdf</name>
<path>/.../SKILL.md</path>
{SKILL.md 全文}
</skill>
```

**B. 模型自行判断匹配**  
按 How to use：用普通读文件工具打开列表中的路径；orchestrator skill 走 `skills.list` / `skills.read`。

### 6.3 Orchestrator 工具描述（字面量）

- `skills.list`：`List enabled skills owned by the requested authority. Only orchestrator-owned skills are currently supported. ...`  
- `skills.read`：`Read one complete resource from an enabled skill. ...`

源码：`codex-rs/ext/skills/src/tools/`。

---

## 七、Genesis（本项目）：`Skill` 网关 + 治理增强

### 7.1 初始 System Prompt（字面结构）

源码：`internal/runtime/prompt/builder.go` + `shared/skillstack/stack.go`（CLI/共享栈同类注入）。

Run 开始构建**一次**；Skill 加载后**不改写**该条消息。

```text
当前时间: {2006年01月02日 15:04:05}

{Agent.SystemPrompt；配置默认类似：}
你是一个有帮助、有能力的AI助手。善用提供的工具来准确回答用户的问题。回答时请简洁清晰，直接给出答案。

<skills_instructions>
Skills 是任务流程包，不是可执行工具。加载技能必须调用 Skill(skill=...)；禁止把 office-ppt 等技能名当作独立工具调用。用户输入中的 $skill 或 skill:// 引用会在回合开始自动注入。可用技能列表见 Skill 工具描述中的 <available_skills>。若 run_skill_command 返回 failure_kind=dependency_missing：调用 install_skill_dependencies（须审批，仅装 runtime 白名单包）后，用相同参数再跑命令（安装成功会清零重复失败计数）；sandbox_violation 勿当成缺包。收到 failure_kind=repeated_failure：禁止再次提交相同调用，必须改参或改策略。收到 failure_kind=no_progress：必须总结阻塞或询问用户，禁止继续空转。

Run 文件落点：中间脚本/临时文件用 write_file("$WORK_DIR/...")；最终交付进 $OUTPUT_DIR；禁止写到仓库根目录。
本 Run：WORK_DIR=... OUTPUT_DIR=... INPUT_DIR=...   ← 有 workspace 时追加
</skills_instructions>

## 行为规则
- 思考时请清晰说明你的推理过程
- 使用工具前先判断是否必要
- 工具结果需要结合上下文给出完整回答
- 直接回答用户的问题，不要重复工具的原始输出
- 收到 failure_kind=repeated_failure：禁止再次提交相同调用，必须改参、先完成 suggested_action，或向用户说明阻塞
- 收到 failure_kind=no_progress：必须总结阻塞或询问用户，禁止继续微调无效调用
```

### 7.2 Catalog 主通道（`Skill` 工具 DescriptionFunc）

源码：`internal/capabilities/skill/tool/skill/tool.go`。

静态描述：

```text
加载已发现 Skill 的完整说明。参数 skill 必须来自本工具 description 中的 <available_skills>。禁止把技能名当作独立工具调用。
```

完整 description 模板：

```text
{静态描述}

<skills_instructions>
当任务匹配可用技能时，必须先调用 Skill 工具加载该技能，再使用原语工具执行。
禁止把技能名当作独立工具名调用。例如禁止调用 office-ppt；正确做法是 Skill(skill="office-ppt")。
</skills_instructions>

<available_skills>
<skill>
<name>
office-ppt
</name>
<description>
{oneLine(description)，约 240 字符上限}
</description>
<location>
embedded:office-ppt
</location>
</skill>
...
<!-- Showing X of Y skills due to catalog budget -->  ← 超预算时
</available_skills>
```

预算：默认约 8KB 字符 / 约 2000 tokens 取更严者。`DisableModelInvocation` 的 skill 不进 catalog。`location` 只暴露 opaque locator。

### 7.3 模型如何返回

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

### 7.4 加载后注入（追加，不替换）

源码：`internal/runtime/strategy/react/skill_support.go`、`react_loop.go`（`renderSkillToolAck` / `renderSkillInjection` / `renderSkillScriptBridge`）；`internal/domain/message.go`（`NewSkillInjectionMessage`）。

模型路径消息：`tool`（`Kind=tool_result`）短确认 → `user`（`Kind=skill_injection`，`Source=skill_gateway`）全文。  
Mention 路径：直接 `user`（`Kind=skill_injection`，`Source=skill_mention`），不伪造 tool_call。

**ToolResult 短确认（模型可见，不含全文）：**

```json
{
  "type": "skill_loaded",
  "qualified_name": "office-ppt",
  "truncated": false,
  "allowed_tools": ["..."],
  "message": "Skill loaded. Follow <skill_injection> instructions and use primitive tools."
}
```

去重时：

```json
{
  "type": "already_loaded",
  "qualified_name": "...",
  "resource": "...",
  "message": "Skill already injected in this run; skipped duplicate body."
}
```

**追加的 user 消息模板（`role=user`，`Kind=skill_injection`；不改写初始 system）：**

```xml
<skill_injection>
Skill: {qualified_name}

{可选: Base directory for this skill: ...}
{可选: Arguments: ...}
{SKILL.md 正文，已剥 frontmatter}

<skill_runtime_bridge>
该 Skill 的 Markdown 是可移植规范，不要求其中出现 Genesis 专用工具名。
当说明要求执行包内脚本或命令（例如 python scripts/foo.py、python3 scripts/foo.py、node scripts/foo.js、python -m some_module、npm 脚本包装 scripts/foo.js）时，必须改用 run_skill_command：skill="{name}"，command 直接填写原始命令行。
按技能文档示例选择解释器：文档是 require()/'.js'/node 则用 node；是 python/python -m 则用 python。不要把 Node 包当成 python -m <pkg> 执行。
需要执行带引号或多语句的 JS/Python 时，先写入工作区脚本文件，再 node script.js / python script.py；避免用 node -e / python -c 塞长串内联代码（嵌套引号在部分 shell 下会失败）。短探测若失败，先区分语法/引号问题与真正的缺模块，再决定是否 install_skill_dependencies。
SKILL 中写明须先 Read 的链接文档（如 *.md），以及 QA Required / Content QA 中的校验命令，必须实际执行，不可跳过。
不要把 script resource id、args 拆成旧模型字段；运行时会先 materialize 完整 Skill 包，再在受控工作目录或远端 session workspace 中执行 command。
如需把现有文件交给脚本处理，使用 run_skill_command.inputs 传入工作区内文件；运行时会自动 stage 到本次 Skill 工作目录。你用 write_file 写出的中间脚本应放在 $WORK_DIR/foo.ext，再以 inputs=["$WORK_DIR/foo.ext"] 传给 run_skill_command，command 使用相对文件名（例如 node foo.ext）。
run_skill_command 返回的 artifacts[].path 是受控交付路径（宿主落在 .genesis/runs/<run>/output/<skill>/；远程 session 回收到同等本地 output）。成功生成后直接以此为交付结果，不要再 copy/cp/write_file 搬到 $OUTPUT_DIR 或仓库根；禁止用 write_file 伪造 .pptx/.docx/.xlsx/.pdf。
不要用 run_skill_command 执行 npm install、pip install、python -m pip install 等依赖安装命令。运行期依赖由 Skill 声明的 runtime/profile 提供；若工具结果明确返回 dependency_missing 和 suggested_install，再使用 install_skill_dependencies 或报告 profile 需要补齐。
脚本执行期可通过 WORK_DIR、INPUT_DIR、OUTPUT_DIR、TMP_DIR、SKILL_DIR 访问受控目录（执行 cwd 内）；最终交付以返回的 artifacts 为准，不要假设 $OUTPUT_DIR 等于 runs/.../output。
不要改写第三方 SKILL.md 或 references 才能运行；适配由运行时完成。
</skill_runtime_bridge>

{截断时追加提示：须用 read_skill_resource / search_skill_resources 读齐链接文档并完成 QA}
</skill_injection>
```

执行管线摘要：

```text
Skill.Execute
  → Resolve(ModelCall=true)
  → 依赖预检（tools / 外部依赖；与 EnabledTools ∩ Profile）
  → Approval(Skill(name) / 外部依赖)
  → Load(SKILL.md + args)
  → ToolResult：短确认 JSON
  → React Loop：追加单份 <skill_injection>
  → 若 allowed_tools 非空：与当前可见集求交后 Gateway.FilterInfos（只收窄不扩权）
  → 同轮若混有其他 tool call：Skill 独占轮，其余跳过
  → 下一轮：模型按说明书调用 write_file / run_command / run_skill_command …
```

补充行为：

| 行为 | 说明 |
| --- | --- |
| `already_loaded` | 同 Run 已注入技能再次 `Skill` 时去重，避免重复灌正文 |
| Mention / `SelectForTurn` | 用户 `$skill` 可在回合开始注入；**不替代**网关，模型主动加载仍走 `Skill` |
| `context=fork` | 经固定 Task/`Delegator` 子 Run；无 agent 合成 `skill-fork:` Definition；主线程不注入 body |
| 脚本执行 | 走 `run_skill_command` 等原语，带 failure_kind / 依赖闭环，与「加载 Skill」分离 |

### 7.5 与 Kode / Codex 的增量

相对 Kode，Genesis 额外强调：

- ToolResult 与 injection **单份正文**（避免双份膨胀）；  
- CollisionGuard + 可选自动改写；  
- 依赖预检 / 安装闭环 / Approval 与多产品 Profile；  
- opaque locator（企业/沙箱不暴露宿主机路径）；  
- fork 不半吊子落地；  
- 正文以 **user `<skill_injection>`** 追加（对齐 Kode `newMessages` / Codex `<skill>`）；初始 system 与 `<skills_instructions>` 短硬规则不变。

相对 Codex，Genesis **不**把「模型自己 read_file 读 SKILL.md」当作主加载路径（避免绕过 Approval）；主路径必须经 `Skill` 网关。

---

## 八、同一场景下的行为差异（示例）

用户：「帮我做一份 PPT」

| 步骤 | Kode | Codex | Genesis |
| --- | --- | --- | --- |
| 发现 | 模型在 `Skill` description 看到 `office-ppt`（或同类） | 模型在 developer Skills 列表看到条目 | 同 Kode：`<available_skills>` 含 `office-ppt` |
| 选择 | `Skill(skill="office-ppt")` | 可能 `$office-ppt`，或 `read` 列表中的 path | `Skill(skill="office-ppt")`；误调 `office-ppt` 可被改写 |
| 加载 | 注入 SKILL.md 到 **user** 消息；System 不变 | mention 注入 **user** `<skill>`；`base_instructions` 不变 | Tool 短确认 + **user** `<skill_injection>`；初始 System 与短硬规则不变 |
| 执行 | 按说明书用原语工具 | 同左 | 同左；脚本走 `run_skill_command` 等治理路径 |

---

## 九、设计取舍小结

| 问题 | Genesis 答案 | 主要参考 |
| --- | --- | --- |
| Skill 要不要进 function schema？ | 不要；只进 Catalog | Kode I2/I3 |
| 唯一加载入口？ | `Skill(skill=...)` | Kode |
| Catalog 放哪？ | `Skill` DescriptionFunc；system 只留短规则 | Kode + 防双份 |
| `tool_choice` 强制 Skill？ | **否**；靠 prompt 软约束 | 与 Kode/Codex 一致 |
| 加载后替换 System？ | **否**；追加 **user** `<skill_injection>`（`Kind=skill_injection`） | 对齐 Kode/Codex；短硬规则仍在初始 system |
| 如何区分「非用户话」？ | **`Message.Kind`**（非 Origin）；`ForUI` 藏 skill | Codex contextual；会话管理方案 §6.2 |
| Mention？ | 增强 UX，不替代网关 | Codex（吸收） |
| 模型自读 SKILL.md 当主路径？ | 否（绕过治理） | 相对 Codex 的刻意差异 |
| 误调用 skill 名？ | CollisionGuard | Genesis 自研增强 |
| 正文如何披露？ | 加载后单份 injection + Kind 打标 | Kode inline + Genesis 去双份 |

---

## 十、相关文档与源码

| 文档 / 代码 | 用途 |
| --- | --- |
| `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md` | Skill/Tool 协议边界（权威） |
| `docs/superpowers/specs/2026-07-11-skill-trigger-modes-design.md` | 触发模式对照 + Genesis 缺口与实现顺序 |
| `docs/Skills设计.md` | Skills 系统总设计 |
| `docs/Skill三模式执行与依赖闭环设计.md` | 脚本执行与依赖闭环 |
| `docs/会话管理与记忆管理设计方案.md` §6.2 | `Message.Kind` / ForUI·ForModel 契约 |
| `internal/domain/message.go` | MessageKind、工厂、`ForUI`/`ForModel` |
| `shared/skillstack/stack.go` | System 短规则 injector + 工具栈装配 |
| `internal/runtime/prompt/builder.go` | 初始 System 组装 |
| `internal/capabilities/skill/tool/skill/tool.go` | Genesis `Skill` 网关与 catalog description |
| `internal/runtime/strategy/react/react_loop.go` | injection 模板 / CollisionGuard / 独占轮 |
| `internal/runtime/strategy/react/skill_support.go` | user+Kind 注入、mention、去重 |
| `Kode-CLI/.../SkillTool/SkillTool.tsx` | Kode 参考实现 |
| `codex-rs/core-skills/src/render.rs` | Codex How to use / catalog 渲染 |
| `codex-rs/core-skills/src/injection.rs` | Codex mention 注入 |
| `codex-rs/core/src/client.rs` | Codex `tool_choice: "auto"` |
