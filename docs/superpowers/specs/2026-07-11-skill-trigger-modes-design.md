# Skill 触发机制设计：显式 / 意图 / Agent 自判

> 状态：设计落地中（对照 Kode / Codex；标出 Genesis 已实现与缺口）  
> 日期：2026-07-11  
> 目的：把「Skill 怎么被触发」拆成清晰模式，对照参考实现，并给出 Genesis 未完成项的实现路径。  
> 协议权威：`docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`（I1–I8 不变）  
> 调用原理对照：`docs/Skill调用原理对比-Kode-Codex-Genesis.md`  
> 参考源码：`D:\workspace\go\go-project\Kode-CLI`、`D:\workspace\go\go-project\codex`

---

## 一、先分清三种触发模式

业界口语里常混用「自动触发」「意图匹配」「命令行触发」。本设计统一为三种**正交**模式：

| 模式 | 触发方 | 宿主是否预注入正文 | 本质 |
| --- | --- | --- | --- |
| **T1 用户显式** | 用户（`/`、`$`、结构化选择） | 是（推荐） | UX / 产品入口 |
| **T2 意图匹配** | 宿主或独立召回服务 | 可选 | **服务端**按规则/向量选 skill |
| **T3 Agent 自判** | 模型（读 catalog 后决定） | 否（模型再请求加载） | Prompt + 工具协议 |

关键澄清：

1. **Kode / Codex 都没有真正的 T2 引擎**（无 embedding 分类器、无独立 intent router）。所谓「任务匹配 description」是写在 prompt 里的 **T3 指令**，不是宿主硬匹配。
2. Genesis **主加载路径必须经 `Skill` 网关**（I4）；禁止把「模型 `read_file` 读 SKILL.md」当主路径（绕过 Approval）。
3. T1 可以「不伪造 tool_call」直接 **user 注入** `<skill_injection>`，但**加载动作仍应走同一套 Resolve → 依赖预检 → Approval → Load**，只是 `ModelCall=false`。
4. T1 的实现不应复用 LLM Tool 执行入口来“假装模型调用”；应抽一个 runtime 内部显式加载端口（如 `ExplicitSkillLoader`），由 Skill 网关实现复用相同加载核心，调用方只传 `invocation=explicit` / `ModelCall=false`。

```text
用户输入 / 选择
        │
        ├─ T1 显式（$mention / slash / UI 选择）
        │     → SelectForTurn / CommandExpand / Explicit[]
        │     → ExplicitSkillLoader.Load(ModelCall=false) → <skill_injection>
        │
        ├─ T2 意图匹配（可选增强，当前不做）
        │     → Ranker/Router → 候选 skill
        │     → 仍建议经 Load 注入，或仅提升 catalog 排序
        │
        └─ T3 Agent 自判（主路径）
              → Catalog 在 Skill.description
              → 模型 tool_call Skill(skill=...)
              → Load(ModelCall=true) → <skill_injection>
```

---

## 二、Kode-CLI 怎么做

### 2.1 注册

Skill 与自定义命令统一为 `CustomCommand`（`type: 'prompt'`），从 `.kode/skills/`、`~/.kode/skills/`、内置包、插件等扫描 `SKILL.md`。

关键文件：`apps/cli/src/services/customCommands/discovery.ts`、`loader.ts`、`commands/registry.ts`。

### 2.2 T1 用户显式：`/skill-name`

```text
用户 "/pdf args"
  → processUserInput（识别 slash）
  → getPromptForCommand()
  → 直接展开为 user messages（不经 Tool 权限门）
```

- Skills 默认 `isHidden: true`（不进 tab 补全），手动 `/name` 仍可用。
- `disable-model-invocation: true` **不阻止**用户 slash，只阻止模型 Tool。

### 2.3 T3 Agent 自判：`Skill` 工具

- Catalog 嵌在 `SkillTool.prompt()` 的 `<available_skills>`。
- 引导文案要求：任务相关时 **IMMEDIATELY** 调用 `Skill`。
- `disable-model-invocation` 的 skill **不进** catalog，调用时直接拒绝。
- 另有 `SlashCommand` 工具；引导上「用户说 run /commit」优先走 `Skill`。

关键文件：`packages/tools/.../SkillTool/SkillTool.tsx`。

### 2.4 T2

`when_to_use` 会解析进对象，但 **不参与** skill 自动选择。无独立 intent 引擎。

### 2.5 Kode 一句话

**同一 skill = prompt 包**；用户用 `/`，模型用 `Skill` tool；「仅用户可触发」靠 `disable-model-invocation`。

---

## 三、Codex 怎么做

### 3.1 注册

`codex-rs/core-skills` 多根扫描 `SKILL.md`，可选 `agents/openai.yaml`（含 `allow_implicit_invocation`）。

### 3.2 T1 用户显式：`$SkillName` / mention

```text
TUI $ / 弹窗 → UserInput::Skill
  → collect_explicit_skill_mentions()
  → 宿主读 SKILL.md → 注入 <skill> 全文
```

- `/skills` 只是**管理菜单**，不加载某个 skill 正文。
- 没有 Kode 式「每个 skill 一个 `/name` slash」。

### 3.3 T3 Agent 自判：prompt + 自读文件

线程启动注入 developer `<skills_instructions>`（name/description/path），含 Trigger rules：

> 用户点名（`$SkillName` 或纯文本）**或**任务明显匹配 description → 必须使用该 skill。

正文**不**自动注入；模型用文件系统工具读 `SKILL.md`。读到/跑到 skill 脚本只发 **Implicit 遥测**，不再注入。

### 3.4 T2

无独立召回；「匹配 description」仍是 T3 prompt 规则。

### 3.5 Codex 一句话

**无通用 `Skill(...)` 网关**；显式靠 `$mention` 宿主预注入，自判靠读文件；Genesis **刻意不照搬**「自读当主路径」。

---

## 四、Genesis 现状（以代码为准）

| 模式 | 状态 | 证据 |
| --- | --- | --- |
| **T3 Agent 自判** | **已实现** | `tool/skill/tool.go`：`Skill` 网关 + `DescriptionFunc` catalog；`Resolve(ModelCall=true)`；短 ToolResult + `<skill_injection>`；CollisionGuard |
| **T1 `$mention`** | **核心已接线，有缺口** | `SelectForTurn` + `extractMentions`；`react/skill_support.go::injectMentionedSkills`；CLI/Enterprise bootstrap 注入 `MentionSelector` |
| **T1 slash `/name`** | **未实现** | 产品侧无 Kode 式 slash 展开；CLI 仅有 skill scaffold 等管理命令 |
| **T1 UI 结构化选择** | **契约有、产品 UX 弱** | 协议允许 `SkillSelection` / mention binding；Enterprise/Web 选择器未作为一等入口验收；产品化优先级应高于 CLI slash |
| **T2 意图匹配引擎** | **不做（与 Kode/Codex 一致）** | 仅靠 catalog description + skills_instructions；无 Ranker |
| **仅用户可触发** | **字段已有，行为不完整** | `disable-model-invocation` 在 `Resolve(ModelCall=true)` 生效；**catalog 未过滤**；**mention 仍走 ModelCall=true** |

### 4.1 已实现链路（T3）

```text
Catalog → Skill.description(<available_skills>)
  → 模型 Skill(skill="office-ppt")
  → Resolve(ModelCall=true) → 依赖预检 → Approval(Skill(name))
  → Load → ToolResult 短确认 + <skill_injection>
  → allowed_tools ∩ 可见集 → Gateway.FilterInfos
```

### 4.2 已实现链路（T1 mention）

```text
用户文本含 $office-ppt / [name](skill://...)
  → injectMentionedSkills（首轮 LLM 前）
  → SelectForTurn
  → registry.Execute("Skill", ...)   // 当前实现
  → user <skill_injection>（不伪造 tool_call）
```

### 4.3 已知缺口（实现时必须修）

| ID | 缺口 | 影响 | 建议修复 |
| --- | --- | --- | --- |
| G1 | `RenderAvailableSkills` 只看 `PromptVisible`，**不过滤** `DisableModelInvocation` | 「仅用户可触发」的 skill 仍出现在模型 catalog | 渲染 catalog 时跳过 `DisableModelInvocation=true`（对齐 Kode） |
| G2 | mention 经 `Skill.Execute`，内部 **硬编码 `ModelCall=true`** | 用户 `$mention` 也无法加载「仅用户」skill | 用户显式路径 `ModelCall=false`；或 Load API 增加 `Invoker: user\|model` |
| G3 | 无 slash `/skill-name` 产品入口 | CLI 用户无法像 Kode 一样「只打命令不讲意图」 | 见 §5.2；可选，优先级低于 G1/G2 |
| G4 | 无 T2 硬匹配 | 长 catalog 时模型漏选 | **本期不做**；若要做，见 §5.4（仅增强，不替代网关） |
| G5 | Enterprise/Web mention picker UX | 结构化选择未产品化 | 产品层 `$`/`@` 弹窗 → `UserInput.Skill` 或文本 `$name` |
| G6 | `context=fork` | 已明确拒绝 | 等子 Agent 设计，不在本触发文档范围 |

---

## 五、怎么实现（按优先级）

### 5.0 不变量（实现时不得破坏）

1. LLM function schema **只**出现 Tool 名；skill 名永不注册为 tool。
2. 加载正文的主路径仍是 **Resolve → 依赖 → Approval → Load → 单份 injection**。
3. mention / slash **不替代** `Skill` 网关；模型主动加载仍走 T3。
4. 不引入「模型 read_file 读 SKILL.md」作为主加载路径。
5. `allowed-tools` 只收窄、不扩权。

---

### 5.1 P0：补齐「仅用户可触发」（G1 + G2）

**目标**：对齐 Kode 的 `disable-model-invocation` 语义。

| 路径 | `ModelCall` | catalog 可见 | 能否加载 |
| --- | --- | --- | --- |
| 模型 `Skill(...)` | true | 否（过滤） | 否 |
| 用户 `$mention` / slash / UI 选择 | false | N/A | 是 |
| 未来纯文本同名且唯一（隐式 mention） | false，但受 `AllowsImplicitInvocation` 与产品开关 | — | 仅允许隐式时；当前 `$name` 不走该限制 |

**实现要点**：

1. **`RenderAvailableSkills`**：`DisableModelInvocation == true` 则跳过（可与 `PromptVisible` 并列）。
2. **拆分调用方**：
   - `Skill` 工具 `Execute`：保持 `ModelCall=true`。
   - `injectMentionedSkills`：**不要**再假装成模型 Tool 调用；改为调用 runtime 内部 `ExplicitSkillLoader.LoadExplicitSkill(...)`，该端口复用 `Skill` 网关核心逻辑并设置 `ModelCall=false` / `invocation=explicit`。
   - `ExplicitSkillLoader` 的实现应保留同一套依赖预检、Approval、`context=fork` 拒绝和 `allowed_tools` 输出；React runtime 只负责把返回的 injection 变成 system message，不复制治理逻辑。
3. **测试**：
   - frontmatter `disable-model-invocation: true` → catalog 不含该 skill；`Skill` tool 调用失败。
   - 同 skill 文本 `$name` → 成功注入。
4. **禁止**：在 LLM schema 中增加内部-only 参数来区分用户/模型；显式路径必须通过 Go 端口传递调用方身份，避免污染模型可见协议。

**涉及文件（预期）**：

- `internal/capabilities/skill/service/service.go`（catalog 过滤）
- `internal/runtime/strategy/react/skill_support.go`（mention 改走 Load）
- `internal/capabilities/skill/tool/skill/tool.go`（保持 ModelCall=true）
- 对应 `*_test.go`

---

### 5.2 P1：Slash `/skill-name`（G3，可选）

**目标**：CLI 对齐 Kode「命令即技能」；**不**做成每个 skill 一个独立 Tool。产品优先级低于 Enterprise/Web 结构化选择，因为 slash 只能表达字符串，结构化选择可以携带 opaque `resource` 并天然解决同名冲突。

**推荐方案（产品层展开，不经 LLM）**：

```text
CLI 输入以 "/" 开头且匹配已注册 skill
  → 展开为：用户消息（可选保留原命令）+ 同 T1 Load(ModelCall=false) 注入
  → 进入正常 ReAct（模型已看到说明书）
```

**不推荐**：为每个 skill 注册 slash Tool（污染 schema，与 I3 冲突）。

**实现要点**：

1. 能力域提供 `ExpandSlash(text) (skill, args, ok)` 或复用 `SelectForTurn` 的命令解析（注意 `/` 与 `$` 语法分离）。
2. **产品边界**：slash 解析放在 `products/cli`（或共享 UI 输入预处理），`internal/capabilities/skill` 只提供「按名 Load」。
3. `isHidden`：若要对齐 Kode，默认 skill 不进补全，但 `/准确名` 仍可触发；补全列表来自 Catalog API。
4. 与现有 `/skills` 管理命令命名空间冲突时：内置管理命令优先，skill 名冲突则拒绝注册或加前缀。

**验收**：

- `/office-ppt 做竞品对比` → 首轮前已有 `<skill_injection>`，且未出现伪造的 `Skill` tool_call。
- `disable-model-invocation` skill 可用 slash，不可被模型 `Skill` 拉取。

---

### 5.3 P1：结构化选择 / Mention UX（G5）

**目标**：Desktop/Enterprise 用户点选 skill，不依赖模型「猜」。这是 Phase B 的首选产品路径；CLI slash 是命令行 ergonomics 增强，不应阻塞结构化选择落地。

**协议**（已有方向，落地产品）：

```text
UserInput:
  - Text: "帮我做 PPT"          // 可选
  - Skills: [{ name, resource }] // 结构化
或
  - Text: "帮我做 PPT $office-ppt"
```

**实现要点**：

1. Runtime 在 `injectMentionedSkills` 之外增加 **结构化 `SelectionRequest.Explicit[]`**（若尚未有）：不解析文本，直接按 resource/name 加载。
2. Web/CLI 弹窗只负责生成 Explicit 或插入 `$name`；**禁止** UI 层直接读宿主机 `SKILL.md` 绕过 Service。
3. 同名冲突：弹窗必须带 opaque `resource` / qualified_name。
4. 普通文本同名不等同于显式选择：只有 `$name`、slash、UI `Explicit[]` 才算 explicit；未来若支持纯文本隐式命中，必须同时满足 `allow_implicit_invocation=true` 和产品配置允许，不能借此绕过 `disable-model-invocation` 的模型侧限制。

---

### 5.4 P2（可选增强）：轻量「意图匹配」（G4）

**明确：不是本期必做。** Kode/Codex 都靠 T3。若 catalog 变长、漏选明显，再考虑：

| 方案 | 做法 | 利弊 |
| --- | --- | --- |
| A. Prompt 加强 | description 写清 when；skills_instructions 强调 IMMEDIATELY | 零基础设施；已是现状 |
| B. Catalog 重排 | 用关键词/embedding 把 top-K 排到 catalog 前部，仍由模型 `Skill` | 不破坏 I4；实现成本中 |
| C. 宿主硬注入 | 匹配成功则自动 Load | 易误触发；需强确认 UX；与「progressive disclosure」张力大 |

**推荐若要做**：只做 **B**（排序/截断偏好），不做 C 的静默全文注入。  
`when_to_use` 若引入 frontmatter，仅用于 B 的排序特征，**不**单独做路由器。

---

### 5.5 与 Approval / 遥测的对应

| 触发 | Approval.ModelCall | Approval 键 | Analytics（建议） |
| --- | --- | --- | --- |
| T3 `Skill` tool | true | `Skill(name)` | `invocation=model` |
| T1 mention/slash/UI | false | `Skill(name)`（可同键，策略区分） | `invocation=explicit` |
| 未来 T2 硬注入 | false | 同上 | `invocation=router` |

Codex 的 Implicit 遥测（检测到读 SKILL.md / 跑 scripts）Genesis **可不做主路径**；若要统计「模型绕过网关读资源」，可在 `read_skill_resource` / `run_skill_command` 上打点，而不是放开 read_file 主路径。

验收上必须检查 audit / usage 事件，而不只检查注入成功：

- `Skill` tool 路径记录 `invocation=model`，且 `ModelCall=true` 的 `disable-model-invocation` 拒绝可观测。
- mention / slash / UI 路径记录 `invocation=explicit`，审批资源键仍为 `Skill(name)`。
- 未来 router 路径若启用，必须记录 `invocation=router` 并可配置关闭。

---

## 六、推荐落地顺序

```text
Phase A（立刻，行为正确性）
  1. G1 catalog 过滤 DisableModelInvocation
  2. G2 mention 改 ModelCall=false 的 Load 路径
  3. 单测覆盖「仅用户 / 仅模型」矩阵

Phase B（产品体验）
  4. Web/Enterprise 结构化 Explicit 选择（G5）优先；CLI slash 展开（G3）随后
  5. SelectionRequest 结构化 Explicit 字段补齐（若缺）

Phase C（可选）
  6. Catalog 重排式「弱意图」（G4-B）
  7. fork / 子 Agent（另文档）
```

**不做**：独立 intent 微服务、把 skill 注册成 tool、以 read_file(SKILL.md) 替代网关。

---

## 七、验收清单

### T3（已有，回归）

- [ ] `Skill` description 含 `<available_skills>`，无宿主机绝对路径
- [ ] `Skill(skill="office-ppt")` → 单份 `<skill_injection>`，ToolResult 无完整 body 依赖
- [ ] 误调 `office-ppt` → CollisionGuard（可 auto_rewrite）

### T1 mention（修 G1/G2 后）

- [ ] `$office-ppt` 首轮前注入成功
- [ ] `disable-model-invocation: true`：catalog 不可见；模型 `Skill` 失败；`$name` 成功
- [ ] 同名歧义：纯 `$name` 不注入；`skill://` / qualified 可注入
- [ ] mention 加载不经过 `registry.Execute("Skill", ...)`，而是经 `ExplicitSkillLoader` 设置 `ModelCall=false`
- [ ] audit / usage 可区分 `invocation=explicit` 与 `invocation=model`

### T1 slash（若做 Phase B）

- [ ] `/office-ppt args` 等价于显式加载 + 参数进入 injection
- [ ] 未知 `/foo` 不误伤为普通聊天或错误展开策略明确

### T2

- [ ] 文档与代码一致：**无**硬路由；若有 catalog 重排，须可配置关闭

---

## 八、相关文档与代码索引

| 资源 | 用途 |
| --- | --- |
| `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md` | Skill/Tool 协议边界（权威） |
| `docs/Skill调用原理对比-Kode-Codex-Genesis.md` | 三方发现→选择→加载→执行 |
| `docs/Skills设计.md` | Skills 总设计 |
| `internal/capabilities/skill/tool/skill/tool.go` | T3 网关 |
| `internal/capabilities/skill/service/service.go` | Catalog / SelectForTurn / Resolve |
| `internal/runtime/strategy/react/skill_support.go` | mention 注入 |
| `internal/runtime/strategy/react/mention_selector.go` | SelectForTurn 适配 |
| Kode `SkillTool.tsx` / `processUserInput.tsx` | T3 + `/` 参考 |
| Codex `core-skills/injection.rs` / `render.rs` | `$mention` + Trigger rules 参考 |

---

## 九、结论

1. **三种模式里，Genesis 已把 T3（Agent 自判 + `Skill` 网关）做成主路径**，这是相对 Codex 更可治理的选择。  
2. **T1 `$mention` 已接线，但「仅用户可触发」语义不完整（G1/G2）——应作为下一刀修复。**  
3. **T1 slash / UI picker 是产品体验缺口，不是协议缺口。**  
4. **T2 硬意图匹配 Kode/Codex 都没有；Genesis 默认也不做**；若要做，只做 catalog 重排，不静默灌正文。  
5. 实现时继续遵守：mention/slash **增强 UX，不替代** `Skill` 网关；正文单份 injection；Approval 不绕过。



