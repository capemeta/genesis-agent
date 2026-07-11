# Skill / Tool 协议边界设计

> 状态：Phase 2 已实现（fork/subagent 延后）  
> 日期：2026-07-09  
> 触发问题：模型把 `office-ppt`（Skill）当成 Tool 直接调用，被 Profile 拒绝。  
> 参考：Kode-CLI `SkillTool`、Codex `core-skills` / `ext/skills`、现有 `docs/Skills设计.md` / `docs/Office能力与Skills设计.md`（仅作参照，本文件以最佳实践为准）。  
> 三方调用原理对照（提示词 / 模型返回 / 执行）：`docs/Skill调用原理对比-Kode-Codex-Genesis.md`。  
> 触发模式（`/` / `$mention` / Agent 自判）与未完成项实现路径：`docs/superpowers/specs/2026-07-11-skill-trigger-modes-design.md`。

---

## 一、第一性原理

### 1.1 要解决的真实问题

不是“报错文案不够友好”，而是：

1. **协议层未硬隔离**：Skill 名出现在提示词目录中，Tool 名出现在 function schema 中，模型容易把两者当成同一类可调用对象。
2. **运行时缺少纠偏契约**：未知 tool 名若恰好是 skill 名，只得到 Profile 拒绝，没有结构化引导。
3. **加载后治理不完整**：`allowed-tools` 收窄路径未统一走 `Gateway.FilterInfos`；ToolResult 与 injection 双份正文；Catalog 动态描述能力缺失。Mention/fork 等增强能力未接线（属后续阶段，不是本故障的根因）。

### 1.2 不变量（Invariants）

| ID | 不变量 |
| --- | --- |
| I1 | Skill 是任务知识/流程包，不是可执行原语。 |
| I2 | Tool 是可执行原语；LLM function schema 中只能出现 Tool 名。 |
| I3 | Skill 名称**永远**不得作为 LLM function / tool name 注册或执行。 |
| I4 | 加载 Skill 只能通过固定网关工具（本设计定为 `Skill`），参数携带 skill 名。 |
| I5 | Catalog（元数据）与 Body（`SKILL.md`）分阶段披露；默认不把 body 塞进初始上下文。 |
| I6 | `allowed-tools` / `dependencies` 只能声明需求或收窄能力，**不得**静默扩大产品 Profile 授权。 |
| I7 | Skill 加载与资源读取必须走 Approval / ToolGateway / Audit，不绕过治理。 |
| I8 | 产品边界不变：`internal/capabilities/skill` 不感知 CLI/Desktop/Enterprise UI；产品 bootstrap 注入 Source/Approval/Profile。 |

### 1.3 失败条件（什么叫设计失败）

- 模型仍能成功“调用”`office-ppt` 这类 skill 名作为 tool。
- Skill 被注册进 Tool Registry，或 marketplace tool capability 与 skill 同名冲突时无隔离。
- 加载 skill 后静默获得 Profile 未授权工具。
- Catalog 挤占过多上下文，导致主任务失败。
- Enterprise 路径把宿主机绝对路径暴露给模型。

### 1.4 成功标准

- 对 tool-calling 模型：只能通过 `Skill(skill=..., args=...)` 加载技能正文。
- 误把 skill 名当 tool 名时：得到 `skill_tool_collision`（可配置自动改写），且**不得**再以“未被 Profile 允许”作为该场景的对外错误语义。
- Office 场景：`Skill(office-ppt)` → 注入流程 → `run_command`/`write_file` 等原语落地。
- 与 Kode 协议对齐、吸收 Codex mention/预算/多 source（mention 属 Phase 2）；保留 Genesis 依赖预检与 Approval。

### 1.5 非目标（本设计不覆盖）

- 不把每个 Office 操作做成细粒度 Tool（仍见 Office 能力文档）。
- 不在 Phase 1 实现多 Agent fork 执行。
- 不在 Phase 1 实现 `$skill` mention 自动注入。
- 不改变 Sandbox / PathResolver / 文件工具的宿主机路径禁令。
- 不引入“只靠 read_file 读 SKILL.md”作为主加载路径（避免绕过 Approval）。

---

## 二、参考结论与取舍

### 2.1 Kode-CLI（主协议参考）

| 点 | Kode | Genesis 取舍 |
| --- | --- | --- |
| 唯一网关 | 工具名固定 `Skill`，参数 `skill`/`args` | **采用**：LLM 可见名改为 `Skill`（见 §3.2 命名决策） |
| Catalog 位置 | 放在 `Skill` 工具的 `prompt()`/description | **采用**：catalog 主通道进网关工具描述；system 只留短策略 |
| Schema 隔离 | skill 名从不进 function schema | **必须采用** |
| 加载后 | inline `newMessages` 或 `context:fork`；`commandAllowedTools` 进权限引擎 | **采用**：inline 一期；fork 二期；allowlist 合并进 runtime 权限上下文 |
| 权限键 | `Skill(name)` / `Skill(ns:*)` | **采用**：Approval resource 参数化 |
| validate | 未知 skill 拒绝 | **采用** |

### 2.2 Codex（能力与 UX 参考）

| 点 | Codex | Genesis 取舍 |
| --- | --- | --- |
| 主路径 | mention / 结构化选择注入，多数不是 tool | **吸收（Phase 2）**：接线 `SelectForTurn`；不替代网关工具 |
| Catalog 预算 | ~2% context / 截断策略 | **采用**字符+token 双预算 |
| 多 source | Host/Executor/Orchestrator + opaque id | **保留并强化**现有 Authority/Resource |
| 元工具命名空间 | `skills.list` / `skills.read`（远程） | **二期**：marketplace/remote 用 `skills.*` 命名空间，避免与网关 `Skill` 混淆 |
| 依赖 | MCP 安装，无 per-skill tool 白名单 | **不照搬**：Genesis 保留 `allowed-tools` 强制收窄 |

### 2.3 相对旧文档的变更（以本文件为准）

旧 `docs/Skills设计.md` §4.5 曾建议工具名用 `load_skill` 而非 `Skill`。  
**本设计推翻该建议**：对 LLM 暴露名对齐 Kode 的 `Skill`；不保留 `load_skill` 对外别名或旧参数兼容。实现位于 `tool/skill`，schema / 日志 / Profile / 提示词只认 `Skill`。

理由：

1. 模型生态与 Kode/Claude 习惯一致，降低“skill 名当 tool 名”的先验错误率。
2. `Skill` 作为唯一网关名，语义上与 `Bash`/`Read` 同级，catalog 可挂在该工具 description 上动态刷新。
3. Genesis 的 snake_case 约定适用于**原语工具**；元工具允许 PascalCase 专名（与 Kode 一致）。

---

## 三、目标架构

### 3.1 分层

```text
LLM
  ├─ function schema: 仅 Tools（含唯一网关 Skill；其 description 含 catalog）
  └─ prompt: 仅短策略硬规则（无完整 skills 列表）

Skill 网关工具 Skill(skill, args)
  -> SkillService.Resolve / Load / 依赖预检 / Approval
  -> Runtime: inline 注入 <skill_injection> 或 fork 子 Run
  -> RuntimeContext: merge AllowedTools（收窄，不扩权）

原语 Tools: read_file / write_file / run_command / web_* / ...
  -> ToolGateway(Profile) -> Authorizer -> Execute
```

### 3.2 命名与协议

#### LLM 可见工具

| 工具名 | 作用 | 是否进 schema |
| --- | --- | --- |
| `Skill` | 加载技能主体并注入上下文 | 是（唯一 skill 入口） |
| `read_skill_resource` | 读 skill 包内资源 | 是 |
| `search_skill_resources` | 搜索 skill 包内资源 | 是 |
| `list_skill_resources` | 列出 skill 包资源 | 是（默认 Profile 必须启用） |
| 其他原语 | 文件/命令/网络等 | 按 Profile |

**禁止**：任何 skill 的 `name` / `qualified_name` 注册为 Tool。

#### `Skill` 输入 schema

```json
{
  "type": "object",
  "properties": {
    "skill": {
      "type": "string",
      "description": "Skill 名称或 qualified_name。必须来自 <available_skills>，例如 office-ppt。"
    },
    "args": {
      "type": "string",
      "description": "可选自由文本参数或任务上下文。"
    },
    "resource": {
      "type": "string",
      "description": "可选不透明 resource id，用于同名消歧。"
    }
  },
  "required": ["skill"]
}
```

兼容：若模型仍传旧字段 `name`，网关在解析层映射为 `skill`（一期兼容，二期删除）。

#### Catalog 动态描述（关键实现约束）

当前 `tool.Info.Description` 是静态字符串，无法像 Kode 的 `tool.prompt()` 那样每轮刷新 catalog。Phase 1 必须扩展工具契约，二选一（推荐 A）：

| 方案 | 做法 | 取舍 |
| --- | --- | --- |
| **A（推荐）** | `tool.Info` 增加可选 `DescriptionFunc func(ctx) (string, error)`；`ListInfos`/LLM 绑定前解析 | 与 Kode 同构；Gateway 无 skill 依赖 |
| B | 每轮由 React Loop 重写 `Skill` 的 Info 副本再传给 LLM | 不改契约，但 Loop 更重 |

`Skill` 工具的最终 description = 固定短说明 + `RenderAvailableSkills` XML。  
System prompt 仅保留 3–5 行硬规则（禁止把 skill 名当 tool 名；必须经 `Skill` 加载），**不再**重复完整 `<available_skills>` 列表，避免双份膨胀。

Catalog XML 格式：

```xml
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

预算：默认 8KB 字符；可按模型 context window 取约 2% token 上限，取更严者。超出截断并标注 `Showing X of Y`。  
`location` 对模型只暴露 opaque locator（如 `embedded:office-ppt` / `skill://...`），**禁止**宿主机绝对路径。

### 3.3 误调用防线（核心新增）

**放置边界（必须遵守）：**

- `ToolGateway` **不**直接依赖 `SkillService`（保持 tool 能力域不反向依赖 skill）。
- CollisionGuard 作为 **React Loop 执行前钩子**（或注入到 Gateway 的可选 `UnresolvedNameHandler` port，由产品 bootstrap 注入 skill 实现）。
- Guard 持有本 turn 的 **Catalog 快照**（与 `Skill` description 同源，一次 Catalog 调用）。

**判定算法（精确顺序）：**

```text
收到 tool_call(name=X, args=...)
  A. 规范化：trim(X)
  B. 若 Gateway.Get(X) != nil（Registry 有且 Profile 允许且可执行）
       → 正常 Execute
  C. 若 X 命中本 turn Skill Catalog（name / qualified_name，大小写敏感按现有 skill 名规则）
       → 视为 skill_tool_collision：
         - 默认：返回 ToolResult JSON（见下），不调用 Profile 拒绝文案
         - auto_rewrite_skill_collision=true：改写为 Skill(skill=规范名, args=可选)，写 audit 后走 B
  D. 若 Registry 有但 Profile 禁用 → 保持“未被 Profile 允许”
  E. 否则 → “工具未注册”
```

结构化 ToolResult：

```json
{
  "type": "skill_tool_collision",
  "requested": "office-ppt",
  "message": "office-ppt 是 Skill，不是 Tool。请调用 Skill(skill=\"office-ppt\")。",
  "suggested_tool": "Skill",
  "suggested_args": {"skill": "office-ppt"}
}
```

规则：

- 当前故障路径是 `isAllowed` 先失败（Enabled 白名单无 `office-ppt`），因此 **C 必须在对外暴露“Profile 未允许”之前完成**，否则用户/模型仍看到假原因。
- 同名若同时存在 Tool 与 Skill：走 B（Tool 优先）；Skill 只能通过 `Skill(skill=...)`；启动/安装时 warning audit。
- Marketplace 安装的 tool capability 禁止占用已有 skill 名；校验失败则拒绝安装。
- auto_rewrite 默认 **开启**；开启时丢弃伪造业务 JSON 参数，仅保留 `skill` 名（若原参数是纯字符串则作为 `args`）。可通过 `WithAutoRewriteSkillCollision(false)` 关闭。

### 3.4 加载与注入

#### Inline（一期必做）

```text
Skill.call
  -> Resolve(ModelCall=true)
  -> checkDependencies(EnabledTools ∩ Profile)
  -> Approval(Skill(skill=name) / 外部依赖)
  -> Load body
  -> ToolResult: 短确认 JSON（name/qualified_name/truncated/allowed_tools 等元数据；**不含**完整 SKILL.md body）
  -> 追加独立消息片段 <skill_injection> 承载完整 body（与 Kode newMessages 同构；清理现状“ToolResult 与 injection 双份正文”）
  -> 更新 RuntimeContext.AllowedTools（见下方语义）
  -> 下一轮 LLM schema = Gateway.FilterInfos(AllowedTools)
```

**`allowed_tools` 语义（必须写死，避免空列表误伤）：**

| skill.allowed_tools | 行为 |
| --- | --- |
| 空 / 未声明 | **不收窄**：保持当前 turn 可见工具集 |
| 非空 | `new = intersect(current, allowed_tools)`；若求交为空则失败并提示，不静默回退全量 |
| 网关自身 | 非空收窄时仍并入：`Skill`；若 Profile 已启用则并入 `read_skill_resource` / `search_skill_resources` / `list_skill_resources` |

约束：

- `Skill` 与其他 tool call 同轮出现时：只执行 `Skill`，其余返回“Skill 加载必须独占本轮”。
- `dependencies.tools` 缺失 → Load 失败并提示，不静默跳过。
- 注入去重：同一 opaque resource 已注入则跳过 body，返回 `already_loaded`。

#### Fork / Subagent（延后；仅保留语义挂点）

`context: fork` → 目标是创建规范化子 agent / 子 Run：独立上下文、继承收窄后的 AllowedTools（以及后续允许的 skills/MCP 集）、主线程不注入 body、结果摘要回传。

**当前实现**：解析字段并在 Load 时明确拒绝，提示改用 `inline`。  
**不做半吊子子 Run**，避免与后续「主 agent 按需启子 agent / 多 agent」设计冲突。后续设计必须覆盖：

1. 给子 agent 的上下文裁剪（任务目标、必要文件/资源 id、禁止宿主机绝对路径）。
2. 子 agent 可调用的 tools / skills / MCP 白名单（默认继承收窄集，不可扩权）。
3. 结果回传契约（摘要、产物路径/resource id、错误）。
4. 审批与审计边界（子调用仍走 Gateway/Approval）。

#### Mention / SelectForTurn（Phase 2 已接线）

1. 解析 `$office-ppt`、`[$name](skill://...)`。
2. 调用已有 `SelectForTurn`。
3. 对命中技能自动 `Load` 并 **system 注入**（受 `disable-model-invocation`、Approval、`AllowImplicit` 约束）；不伪造 tool_call。
4. Plain name 仅当全局唯一且允许隐式调用时解析。

### 3.5 权限模型

| 场景 | 决策键 | 默认 |
| --- | --- | --- |
| 加载普通内置 skill | `Skill(office-ppt)` | allow（产品可改为 ask） |
| `disable-model-invocation` | — | deny（模型路径） |
| 外部依赖 mcp/command/url | `Skill(name)+dependencies` | ask |
| 扩大 Profile 外工具 | — | deny（不允许通过 skill 扩权） |
| 收窄工具 | RuntimeContext | allow |

`allowed-tools` 语义明确为：**激活后的工具可见上限（与当前可见集及 Profile 求交）**，不是扩权申请单。Phase 1 通过 RuntimeContext + `Gateway.FilterInfos` 落实；Phase 2 再对齐 Kode 式 `Skill(name)` 权限键到 Approval。

### 3.6 Profile 与依赖对齐

CLI 默认 Profile：

- Tools.Enabled 只包含 `Skill`（不保留 `load_skill`）。
- 必须包含：`list_skill_resources`、`read_skill_resource`、`search_skill_resources`。
- Office skill frontmatter 的 `allowed-tools` 与 Profile 对齐；CI 增加校验：embedded skill 声明的 tool 必须在默认 Profile 或明确标记 optional。

### 3.7 目录与模块边界

```text
internal/capabilities/skill/
  tool/skill/            # 实现包；LLM 可见工具名固定为 Skill
  collision/                  # Catalog 匹配与 collision 结果构造（无 Gateway 依赖）

internal/capabilities/tool/
  contract/                   # Info 增加可选 DescriptionFunc
  gateway/                    # 可选 UnresolvedNameHandler port；默认不依赖 skill

internal/runtime/strategy/react/
  # 执行 tool 前调用 Handler；Skill 独占轮；AllowedTools → FilterInfos
  # Phase 2：Turn 开始 SelectForTurn

products/cli/bootstrap/
  # 注入 Handler + Skill 工具 DescriptionFunc 所需 CatalogRequest
products/cli/internal/profile/
  # DefaultProfile：Skill + list_skill_resources
```

不新增跨产品目录；不把 UI mention picker 放进 `internal`。  
Gateway 若提供 `UnresolvedNameHandler`，实现由 bootstrap 注入，**禁止** `gateway` 包 import `skill` 实现。

---

## 四、数据流

### 4.1 正确路径（Office PPT）

```text
User: 生成对比 PPT
  -> Catalog 含 office-ppt
  -> Model: Skill(skill="office-ppt", args="...")
  -> Approval / deps OK
  -> Inject SKILL.md
  -> AllowedTools 收窄为 skill 声明 ∩ Profile
  -> Model: run_command(python scripts/...) / write_file
  -> 产出 .pptx 到 OUTPUT_DIR
```

### 4.2 错误路径（当前故障）→ 纠偏后

```text
Model: tool_call(name="office-ppt", arguments={action:create,...})
  -> CollisionGuard 命中 Catalog
  -> ToolResult: skill_tool_collision + suggested Skill(...)
  -> 下一轮 Model: Skill(skill="office-ppt")
  -> 正常路径
```

若开启 auto_rewrite：同轮直接改写并执行 `Skill`，原错误参数进入 `args` 字符串或丢弃（默认丢弃非字符串业务参数，避免把伪造 schema 当 skill args）。

---

## 五、与现有实现的差距

| 项 | 现状 | 目标 |
| --- | --- | --- |
| 网关工具名 | `load_skill` | `Skill`（无别名） |
| ToolResult 正文 | JSON 含完整 content + 再 injection | 短确认 + 单份 injection |
| Catalog 位置 | system injector 全文 | 主通道：`Skill` description；system 短规则 |
| 误调用 | Profile 拒绝文案 | CollisionGuard 结构化纠错 |
| SelectForTurn | 已实现未接线 | **Phase 2** 接线 |
| Description 动态 | 静态 Description | DescriptionFunc / 每轮解析 |
| list_skill_resources | 未进默认 Profile | Phase 1 启用 |
| allowed_tools | 空则不收窄（现状）；非空收窄 | 明确空=不收窄；非空求交失败则报错；经 FilterInfos |
| CollisionGuard | 无 | Phase 1：Loop/Handler，Gateway 不依赖 skill |
| fork | 字段存在 | Phase 2 |
| 同名 Tool/Skill | 无校验 | 安装/启动校验 + Tool 优先 |
| 文档 | 曾推荐 load_skill | 以本文为准修订 Skills/Office 文档 |

---

## 六、分期落地

### Phase 1（根治误调用 + 协议对齐）

1. 扩展 `tool.Info` 支持动态 Description；`Skill` 工具 schema（无 `load_skill` / `name` 兼容）。
2. CollisionGuard（Loop 钩子或 Gateway UnresolvedNameHandler）+ `skill_tool_collision`。
3. Catalog 主通道写入 `Skill` description；system 仅短规则。
4. Profile：`Skill` + `list_skill_resources`；embedded skill `allowed-tools` 与 Profile CI 校验。
5. 收窄逻辑经 `Gateway.FilterInfos`；明确空 allowed_tools 语义。
6. 单测：碰撞优先于 Profile 文案、依赖对齐、DescriptionFunc。
7. 修订 `docs/Skills设计.md`、`docs/Office能力与Skills设计.md` 冲突段落。

### Phase 2（Codex 能力；不含规范化 subagent）

1. 接线 `SelectForTurn` + mention 消歧与自动注入（system injection，不伪造 tool_call）。
2. Catalog 字节 + 近似 token 双预算与截断策略。
3. 注入去重 `InjectedSkillSet` / `already_loaded`。
4. Approval 键对齐 `Skill(name)` / `Skill(name)+dependencies`。
5. CollisionGuard **默认** `auto_rewrite`：同轮改写为 `Skill(skill=规范名)` 并执行（伪造业务 JSON 参数丢弃）。
6. **`context:fork` 真执行延后**：字段语义保留，运行时继续明确拒绝；待「主 agent 按需启子 agent」设计完成后再实现。fork/subagent 需单独设计：子上下文裁剪、允许的 tools/skills/MCP、结果回传契约。

### Phase 3（远程/市场）

1. `skills.list` / `skills.read` 命名空间元工具（仅 remote/orchestrator）。
2. Marketplace 安装禁止 skill/tool 名冲突。
3. Enterprise opaque resource 全路径。

---

## 七、测试与验收

### 7.1 单测

- Guard：`office-ppt` → collision；`read_file` → 正常；同名 tool+skill → tool 优先。
- `Skill`：未知 skill 拒绝；`disable-model-invocation` 拒绝；依赖缺失拒绝。
- 注入后 schema 仅含 allowed ∩ profile。
- 参数仅接受 `skill` / `resource` / `args`；拒绝旧 `name` 字段。

### 7.2 集成 / 行为

- 回放本次故障参数：不得再出现“未被 Profile 允许”作为最终语义；必须是 collision 或成功加载。
- `Skill(office-ppt)` 后可调用 `run_command` 执行 embedded 脚本（沙箱策略按现有配置）。

### 7.3 文档验收

- Skills/Office 文档与本文 I1–I8、工具名、误调用语义一致。
- AGENTS.md 不重复长文，仅在必要时加一句“Skill 只能经 Skill 网关加载”。

---

## 八、风险与残留

| 风险 | 缓解 |
| --- | --- |
| 改名 `load_skill`→`Skill` 影响已有会话/提示 | 开发阶段不兼容旧会话；提示词与 schema 只暴露 `Skill` |
| auto_rewrite 误伤同名未来 tool | 默认关闭；同名冲突启动告警 |
| Catalog 进 tool description 过长 | 预算截断；system 不再重复全文；DescriptionFunc 失败时回退静态短说明并打 error 日志 |
| DescriptionFunc 每轮开销 | Catalog 结果按现有 SkillService 缓存键复用 |
| Fork 未实现期间模型请求 fork | 明确错误，回退建议 inline |
| 模型仍忽略纠错 | 纠错 JSON + 短自然语言；必要时开启 auto_rewrite |

残留（可接受）：

- 模型可能多一轮才能纠正；不以静默成功掩盖协议错误（除非显式开启 rewrite）。
- Codex 式“只靠 filesystem 读 SKILL.md”不作为主路径，避免与 Gateway/Approval 双轨。

---

## 九、决策摘要

1. **采用混合方案**：对外 Kode 式 `Skill` 网关；对内吸收 Codex mention/预算/多 source（mention 为 Phase 2）；保留 Genesis 依赖预检与 Approval。  
2. **Skill 名永不进 function schema。**  
3. **新增 CollisionGuard 作为协议层硬防线（Loop/Handler，Gateway 不依赖 skill）。**  
4. **旧文档中“优先 load_skill 命名”的建议废止，以本文为准并回写修订。**  
5. **实现按 Phase 1→2→3；Phase 1 即可关闭本次 `office-ppt` 误调用类故障。**
6. **Phase 1 必须扩展动态 Description，并把 Skill body 改为单份 injection。**

---

## 十、实现时禁止事项

- 不要为每个 skill 生成一个 Tool。
- 不要在 skill 加载时把 Profile 外工具临时 Register 进全局 Registry。
- 不要在 tool 内直接 `os` 读 skill 文件绕过 Source。
- 不要把产品 UI、Wails、企业 RBAC 实现塞进 `internal/capabilities/skill`。
- 不要为兼容旧错误语义而保留“Profile 未允许”作为 skill 碰撞的唯一错误。
