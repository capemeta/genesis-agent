# Hook 机制设计方案

> 状态：设计稿（Phase 1B）
> 适用范围：`genesis-agent` Runtime 内核、Tool/Skill 网关、Approval 与观测体系
> 关联文档：`docs/agent loop设计.md`（SkillHook / Runtime Middleware 目标）、`docs/项目目录与边界说明.md`（目录与依赖边界）、`docs/文件系统设计方案.md`（Gateway Authorizer）、`docs/子智能体设计.md`（Subagent 生命周期）、`docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`（Skill/Tool 边界）

本方案定义 genesis-agent 的通用 Hook（钩子）机制：在 Agent 运行生命周期的关键点，允许用户/企业通过**外部命令**或**内置 Go 处理器**观测、注入上下文、改写工具输入、放行或阻断执行，而无需修改内核代码。

---

## 0. 结论先行（TL;DR）

1. **新增能力域 `internal/capabilities/hook`**，按 `contract / model / service / adapter` 分层，产品无关。
2. **事件模型**借鉴 Kode-CLI / codex 的 Claude-style Lifecycle Hooks，落地 genesis 专属事件集（含 `PreSkillUse` / `PostSkillUse`）。
3. **双通道决策**：`exit code`（快速阻断，脚本友好）+ `stdout JSON`（细粒度：allow/deny/ask、改写输入、注入上下文）。
4. **接入点复用现有扩展位**：
   - `PreToolUse` / `PostToolUse` → `Tool Gateway`（复用现有 `Authorizer` 扩展位，`gateway.go:156-168`）。
   - `RunStart` / `UserPromptSubmit` / `Stop` / `RunComplete` → `ReactLoopEngine`（`react_loop.go`），通过 context 注入 `hook.Dispatcher`（对齐 `progress.Sink` / `ApprovalGrantedHook` 模式）。
   - `PreSkillUse` / `PostSkillUse` → Skill 网关工具与 `run_skill_command` 服务。
   - `PermissionRequest` 语义与 `approval.Service` 串联（deny 短路）。
5. **配置**采用 YAML（与项目 viper 体系一致），多层 merge + per-hook `enabled` / `trusted_hash` 治理，支持企业 `managed-only`。
6. **观测**复用现有 `progress.Sink` / `audit.Sink` / `usage.Sink` / `trace.Tracer`，Hook 运行态可推送 UI。
7. **产品形态统一内核、差异只在 bootstrap 注入**（§6.5）：CLI/Desktop 走宿主机执行；**Enterprise 禁止宿主任意 command hook**，只允许 builtin 或 managed+sandbox，默认 `allow_managed_only=true`；Hook 纳入统一能力适用范围（scope）过滤。

---

## 1. 背景与目标

### 1.1 现状（以代码为事实）

genesis-agent 目前**没有统一 Hook 框架**。最接近扩展点的只有：

| 现有扩展位 | 位置 | 局限 |
|---|---|---|
| `ApprovalGrantedHook` | `internal/platform/contextutil/context.go:25-45` | 仅审批通过回调，单一用途 |
| `gateway.Authorizer` | `internal/capabilities/tool/gateway/gateway.go:55-58` | 仅工具前布尔授权，未接生产，无改写/注入能力 |
| `prompt.ContextInjector` | `internal/runtime/prompt/interface.go:25-28` | 仅 Run 前系统提示注入 |
| `progress.Sink` | `internal/runtime/progress/progress.go:65-66` | 只读观测，不能阻断 |
| `audit.Sink` / `usage.Sink` | `internal/capabilities/{audit,usage}/contract/sink.go` | 只读观测 |

`AGENTS.md §核心原则` 将 **Hook 列入 Phase 1B**；`docs/agent loop设计.md` 已规划 `SkillHook`（`on_run_start` / `on_step_start` / `on_step_complete` / `on_run_complete`）与 `Runtime Middleware`，但均未落地。

### 1.2 目标

- **可扩展**：用户/企业无需改内核即可拦截生命周期；事件集可增量扩展。
- **策略无关**：Hook 挂在 `RunEngine` 接口层与 `Gateway` 层，不绑死 ReAct（Plan-Execute/Coding/RAG 未来复用）。
- **安全可治理**：外部命令须经信任校验；企业可强制 managed-only；可全局/单条禁用。
- **可观测**：Hook 执行进入 trace/audit/usage 与 progress 事件流。
- **兼容心智**：payload/决策字段尽量对齐 Claude Code / codex 生态，降低脚本迁移成本。

### 1.3 非目标（本期不做）

- Prompt 型 Hook（LLM 生成决策）——`schema` 预留，Phase 2 实现。
- 插件（plugin bundle）来源的 Hook 发现——预留 `HookSource` 扩展点。
- Hook 可视化管理 UI（对齐 Kode `/hooks` 界面）——预留 `hooks/list` 契约，前端后续接。

---

## 2. 参考源码清单（开发时查阅）

> 以下为本方案实际借鉴的两个项目的关键文件，路径为绝对路径，便于开发时直接对照阅读。

### 2.1 Kode-CLI（`D:\workspace\go\go-project\Kode-CLI`）

配置驱动的 command/prompt hook，最贴近本方案的“外部命令 + JSON 契约”形态。

| 主题 | 文件 | 关键位置 |
|---|---|---|
| 事件枚举、Hook/Matcher/Outcome 类型、JSON 字段解析 | `packages/core/src/hooks/types.ts` | L1-L9（事件）、L11-L47（类型）、L49-L165（Outcome 与解析）、L100-L121（decision 归一化） |
| 配置加载、matcher 匹配、多层 merge | `packages/core/src/hooks/registry.ts` | L69-L85、L143-L256、L185-L200（`matcherMatchesTool`） |
| settings 文件路径 | `packages/config/src/files.ts` | L44-L65 |
| 命令执行器（spawn / stdin / 超时 / JSON 解析 / 并行） | `packages/core/src/hooks/executor.ts` | L21-L93（`runCommandHook`）、L123-L162（超时）、L227-L239（JSON 提取）、L334-L392（并行调度） |
| Hook 环境变量注入 | `packages/core/src/compat/hookEnv.ts` | L3-L29 |
| PreToolUse / PostToolUse runner 与结果聚合 | `packages/core/src/hooks/tool.ts` | L225-L318（Pre 聚合）、L356-L367（Post payload）、L61-L141（transcript） |
| 生命周期事件 runner（Stop/UserPromptSubmit/PreCompact/SessionEnd） | `packages/core/src/hooks/lifecycle/events.ts` | L30-L38（matcher）、L78-L85（exit 语义）、L192-L424（各事件 payload） |
| SessionStart（仅插件、串行、env file 副作用） | `packages/core/src/hooks/lifecycle/sessionStart.ts` | L29-L39、L128-L228 |
| 内置守卫（非配置 hook） | `packages/core/src/hooks/builtin/preToolUse.ts` | 全文 |
| 全局禁用开关 | `packages/core/src/hooks/disableAllHooks.ts` | L8-L52 |
| 主流程接入：工具前后 | `packages/core/src/engine/pipeline/tool-call.ts` | L78-L93、L111-L183、L290-L317 |
| 主流程接入：用户 prompt / stop | `packages/core/src/engine/message-pipeline.ts` | L199-L276、L367-L422 |
| 主流程接入：SessionStart 注入 system prompt | `packages/core/src/constants/prompts.ts` | L419、L564-L566 |
| 行为契约测试（最佳阅读入口） | `packages/core/src/test/unit/hooks-*.test.ts` | exit code / permissionDecision / stop / plugin 各场景 |

### 2.2 codex（`D:\workspace\go\go-project\codex`）

first-class Lifecycle Hooks（Rust crate），额外提供**审批（approval）与 hook 串联**、**运行态事件广播**、**信任治理**，是本方案“治理 + 审批桥接”的主要借鉴。

| 主题 | 文件 | 关键位置 |
|---|---|---|
| 事件常量（10 种） | `codex-rs/hooks/src/lib.rs` | L19-L30、L84-L97 |
| 协议层事件枚举 / 运行态类型 | `codex-rs/protocol/src/protocol.rs` | L1469-L1482（`HookEventName`）、L1559-L1594（`HookRunSummary`） |
| Hook registry / 公共 API | `codex-rs/hooks/src/registry.rs` | L29-L206 |
| 命令执行（Windows/Unix shell 包装、stdin JSON） | `codex-rs/hooks/src/engine/command_runner.rs` | L24-L69、L103-L135 |
| Handler 并行调度 + 配置保序 | `codex-rs/hooks/src/engine/dispatcher.rs` | L27-L154（`execute_handlers`） |
| 多来源 discovery + precedence | `codex-rs/hooks/src/engine/discovery.rs` | L63-L174 |
| matcher 匹配（regex/精确/`\|` 别名/全匹配） | `codex-rs/hooks/src/events/common.rs` | L129-L139 |
| PreToolUse（exit2 / permissionDecision / updatedInput） | `codex-rs/hooks/src/events/pre_tool_use.rs` | L22-L142、L188-L303 |
| PermissionRequest（审批路径 hook，deny 优先） | `codex-rs/hooks/src/events/permission_request.rs` | L1-L170 |
| I/O JSON Schema（契约） | `codex-rs/hooks/src/schema.rs` | L86-L96（universal 输出）、L239-L260（PreToolUse 输出） |
| 生成的 JSON Schema fixtures | `codex-rs/hooks/schema/generated/*.json` | 20 个输入/输出 schema |
| 遗留 notify（argv 末参 JSON、fire-and-forget） | `codex-rs/hooks/src/legacy_notify.rs` | L12-L70 |
| Core hook 运行时（emit started/completed） | `codex-rs/core/src/hook_runtime.rs` | L51-L256、L433-L498、L617-L644、L739-L746 |
| 工具执行前拦截 PreToolUse | `codex-rs/core/src/tools/registry.rs` | L495-L538 |
| 审批 hook 优先级（PermissionRequest → guardian → user） | `codex-rs/core/src/tools/orchestrator.rs` | L510-L575 |
| 用户审批 suspend（oneshot + Op 回传） | `codex-rs/core/src/session/mod.rs` | L2120-L2191、L4026-L4035 |
| 配置 schema（handler 类型、matcher 组、trust state、managed） | `codex-rs/config/src/hook_config.rs` | L27-L167 |
| config.toml 字段（notify / approval_policy / hooks） | `codex-rs/config/src/config_toml.rs` | L174、L216、L449 |
| Feature flag | `codex-rs/features/src/lib.rs` | L84-L88 |
| app-server 事件转发 / hooks 列表 | `codex-rs/app-server/src/bespoke_event_handling.rs` L1041-L1059；`.../catalog_processor.rs` L584-L649 |
| 配置示例（测试即文档） | `codex-rs/config/src/hooks_tests.rs` | L13-L113 |

### 2.3 借鉴取舍小结

| 维度 | Kode-CLI | codex | genesis 取舍 |
|---|---|---|---|
| 事件定义 | 8 种 | 10 种（含 PermissionRequest/PostCompact/Subagent） | 采用分层事件集，**增补 `PreSkillUse`/`PostSkillUse`**（genesis Skill 是一等公民） |
| Handler | command / prompt | command / prompt / agent | Phase 1 只做 **command + builtin(Go)**，prompt 预留 |
| 阻断通道 | exit2 + stdout JSON | exit2 + stderr reason + stdout JSON | **两者都要**：exit2+stderr（脚本），stdout JSON（结构化） |
| 传参 | stdin JSON + 少量 env | stdin JSON + env | stdin JSON 为主，env 传元信息 |
| 审批桥接 | permissionDecision=ask | 独立 `PermissionRequest` + oneshot 审批 | **`PreToolUse` 的 ask/deny 桥接现有 `approval.Service`** |
| 治理 | disableAllHooks + 三层 settings | trust hash + managed-only + per-hook state | **全都要**：全局开关 + trust hash + managed-only |
| 运行态广播 | 无显式 | HookStarted/Completed 事件 | 复用 `progress.Sink`（KindSystem/新增 KindHook） |
| 配置格式 | JSON | TOML | **YAML**（与 genesis viper 体系一致） |

---

## 3. 概念与术语

- **Hook Event（事件）**：生命周期上的一个触发点，如 `PreToolUse`。
- **Matcher（匹配器）**：决定某条 Hook 是否对当前事件生效的规则（按工具名/技能名/触发原因匹配，支持精确、`|` 别名、glob、正则、`*`）。
- **Handler（处理器）**：一条 Hook 的执行体，`command`（外部进程）或 `builtin`（内置 Go 函数）。
- **Payload（输入负载）**：内核以 JSON（stdin）传给 command handler 的事件数据。
- **Decision（决策）**：Handler 返回给内核的结构化结果（放行/阻断/询问/改写输入/注入上下文）。
- **Dispatcher（调度器）**：内核侧统一入口，负责选中 matcher、并行执行 handler、聚合决策。
- **Aggregation（聚合）**：多条 Hook 的决策合并策略（deny 优先，updatedInput 顺序覆盖）。

---

## 4. Hook 事件模型

### 4.1 事件枚举

| 事件 | 触发时机 | 可阻断 | 可改写 | 可注入上下文 | Matcher 对象 | 状态 |
|---|---|---|:---:|:---:|---|---|
| `RunStart` | Run 开始、构建 system prompt 前 | 否 | — | ✅（additionalContext 注入 system） | 无 | Phase 1 |
| `UserPromptSubmit` | 用户输入进入循环前 | ✅ | — | ✅ | 无 | Phase 1 |
| `PreToolUse` | 工具执行前（Gateway 内、锁前） | ✅ | ✅（tool_input） | ✅ | 工具名 | Phase 1 |
| `PostToolUse` | 工具执行后（拿到结果） | 否 | — | ✅ | 工具名 | Phase 1 |
| `PreSkillUse` | `Skill(skill=...)` 加载前 | ✅ | — | ✅ | 技能名 | Phase 1 |
| `PostSkillUse` | Skill 注入完成后 | 否 | — | ✅ | 技能名 | Phase 1 |
| `Stop` | 本轮产出最终回答、Run 结束前 | ✅（要求继续） | — | ✅ | 无 | Phase 1 |
| `RunComplete` | Run 完成后（通知型，fire-and-forget） | 否 | — | 否 | 无 | Phase 1 |
| `PreCompact` | 记忆压缩前 | ✅ | — | ✅ | manual/auto | 预留（依赖压缩落地） |
| `SubagentStart` | 子智能体启动 | ✅ | — | ✅ | 子 agent 名 | 预留（依赖子智能体） |
| `SubagentStop` | 子智能体结束 | ✅ | — | ✅ | 子 agent 名 | 预留 |
| `PermissionRequest` | 审批路径上（PreToolUse 之后、人工 UI 之前） | ✅（allow/deny 短路） | 否 | 否 | 工具名 | Phase 1.5（审批桥接） |

> 设计要点：
> - genesis 用 `RunStart` 对应 Kode/codex 的 `SessionStart`；genesis 当前 Session 只有 ID、无持久会话启动 hook，`RunStart` 更贴合内核事实（`react_loop.go:89`）。
> - `PreSkillUse` / `PostSkillUse` 为 genesis 专属，源于 Skill 一等公民定位（`docs/superpowers/...skill-tool-protocol-boundary-design.md`）。
> - `PreCompact` / `Subagent*` 先进枚举、`model` 与 `contract` 就位，接入点等对应内核能力落地后再打开。

### 4.2 事件-能力矩阵（可选返回字段）

| 返回字段 | RunStart | UserPromptSubmit | PreToolUse | PostToolUse | PreSkillUse | Stop |
|---|:---:|:---:|:---:|:---:|:---:|:---:|
| `continue`（false=停止后续 hook 与动作） | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `systemMessage`（面向用户/日志的提示） | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `additionalContext`（注入下一轮 system） | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `permissionDecision`（allow/deny/ask） | — | — | ✅ | — | ✅ | — |
| `updatedInput`（改写工具入参） | — | — | ✅ | — | — | — |
| `decision`（block + reason，要求继续） | — | ✅ | — | — | — | ✅ |

---

## 5. 数据契约（Payload / Decision Schema）

### 5.1 通用输入字段（所有 command handler stdin JSON）

```json
{
  "hook_event_name": "PreToolUse",
  "schema_version": "1",
  "run_id": "run-1783812390645714000",
  "session_id": "sess-abc",
  "tenant_id": "t-001",
  "agent_name": "default",
  "cwd": "workspace-relative-or-sandbox-path",
  "permission_mode": "default",
  "model": "gpt-5.5"
}
```

> 路径遵守边界：Docker/genesis-sandbox 模式下 `cwd` **只给 workspace-relative / sandbox path / resource id**，不泄露宿主机绝对路径（见 `docs/项目目录与边界说明.md §沙箱与路径展示边界`）。

### 5.2 各事件扩展输入字段

| 事件 | 追加字段 |
|---|---|
| `PreToolUse` | `tool_name`、`tool_input`（object）、`tool_use_id` |
| `PostToolUse` | `tool_name`、`tool_input`、`tool_use_id`、`tool_result`（string/obj）、`success`（bool）、`duration_ms` |
| `PreSkillUse` / `PostSkillUse` | `skill_name`、`invocation`（model/mention/explicit）、（Post）`injected`（bool） |
| `UserPromptSubmit` | `user_prompt` |
| `Stop` | `final_answer`、`iterations`、`stop_active`（bool，防重入死循环标记） |
| `RunComplete` | `status`（completed/failed）、`final_answer`、`total_tokens`、`incomplete`（bool） |
| `PreCompact`（预留） | `trigger`（manual/auto）、`token_count_before` |
| `Subagent*`（预留） | `subagent_name`、`parent_run_id` |

### 5.3 输出决策（stdout JSON）

```json
{
  "continue": true,
  "systemMessage": "已通过安全校验",
  "hookSpecificOutput": {
    "permissionDecision": "allow",
    "permissionDecisionReason": "命令在白名单内",
    "updatedInput": { "command": "ls -la" },
    "additionalContext": "当前处于只读演示环境"
  }
}
```

归一化规则（对齐 Kode `normalizePermissionDecision`，`types.ts:100-121`）：

- `permissionDecision`：`allow|approve` → allow；`deny|block` → deny；`ask` → ask；`passthrough|continue` → passthrough(不表态)。
- `decision`：`approve|allow` → 放行；`block|deny` → 阻断（Stop/UserPromptSubmit 用）。

### 5.4 Exit Code 语义（command handler）

| exit code | 含义 | 说明 |
|---|---|---|
| `0` | 成功 | 解析 stdout JSON 决策；无 JSON 则视为放行 |
| `2` | 阻断 | 对可阻断事件立即阻断；`stderr` 作为阻断原因（对齐 codex `pre_tool_use.rs:254-269`） |
| 其他非 0 | 警告 | 记 warning，不阻断（对齐 Kode `tool.ts:270-272`） |

> **双通道**：脚本用 `exit 2` 快速阻断即可；需要改写输入/注入上下文/ask 审批时用 `exit 0 + stdout JSON`。

### 5.5 决策聚合（多条 Hook）

顺序遍历“按配置顺序保序”的结果（并行执行、保序聚合，对齐 codex `dispatcher.rs`）：

1. 任一 `exit 2` 或 `permissionDecision=deny` 或 `decision=block` → **立即阻断**，收集 reason。
2. 任一 `permissionDecision=ask` → 标记需人工审批（桥接 `approval.Service`）。
3. `updatedInput` → 浅合并，后者覆盖前者（对齐 Kode `tool.ts` 合并逻辑）。
4. `additionalContext` / `systemMessage` → 追加进注入队列。
5. 无人反对 → 放行（可能携带合并后的 input 与注入内容）。

---

## 6. 架构与目录落地

### 6.1 新增能力域 `internal/capabilities/hook`

遵循 `docs/项目目录与边界说明.md` 的能力域 `contract / model / service / adapter` 结构：

```text
internal/capabilities/hook/
  contract/
    dispatcher.go      # Dispatcher 接口（内核唯一依赖入口）
    handler.go         # Handler / Runner 接口
    loader.go          # Loader（从配置构建 Registry）/ HookSource（预留 plugin）
  model/
    event.go           # EventName 常量、Payload、Decision、AggregateResult
    config.go          # HookConfig / MatcherGroup / HandlerSpec / TrustState
  service/
    dispatcher.go      # 默认 Dispatcher：选 matcher + 并行执行 + 聚合
    matcher.go         # matcher 匹配（精确/别名/glob/regex/*）
    registry.go        # 事件→matcher 组索引；enabled/trust 过滤
    aggregate.go       # 决策聚合
    loader.go          # 多层配置 merge、trust 校验
  adapter/
    command/           # 外部命令 handler：只负责 Hook I/O 协议（stdin JSON 组装 + stdout/exit 解析 + 超时映射）
      runner.go        # 委托 execution.ExecutionRunner 真正 spawn；不含 os/exec、不含 shell 选择
    builtin/           # 内置 Go handler 注册（如 git 分支守卫）
      registry.go
```

> shell 选择（`cmd.exe` / `$SHELL`）、宿主 vs sandbox 由 `execution` 能力的 runner 负责（`Command.Shell=auto`），**不在 hook 内重复实现**，避免与 `execution` 边界冲突（详见 §6.4）。

依赖边界：
- `hook/contract`、`hook/model` 不依赖任何具体实现，可被 `internal/runtime`、`Gateway`、Skill 服务安全引用。
- `hook/service` 依赖 `contract` / `model` / `platform`（logger、config、correl）。
- `hook/adapter/command` 依赖 `execution/contract`（`ExecutionRunner`）与 `execution/model`，不直接依赖 `os/exec`。**执行边界取舍见 §6.4**。

### 6.2 内核依赖方向（关键）

`internal/runtime` 不得依赖具体能力实现（`项目目录与边界说明.md §internal 目录`），但可依赖 **contract**（现状：runtime 已 import `tool/contract`、`llm/contract`、`trace/contract`）。因此：

- `internal/runtime` 只依赖 `hook/contract.Dispatcher`（接口）。
- 具体 `hook/service` 实现由产品 `bootstrap` 装配后，经 **context 注入**（对齐 `progress.Sink` / `ApprovalGrantedHook`）传入运行时。

**已定（决策 B，见 §12）：采用 context 注入**（无侵入、跨策略通用），与 `progress`/`approval` 完全一致；不采用 `ReactLoopEngine` 依赖字段方案。

```text
products/<product>/bootstrap
  -> hook/service.NewDispatcher(loader.Load(cfg.Hooks, scope), runner, sinks...)  // 详见 §6.5.5
  -> app.RunOnce 时 hook.WithDispatcher(ctx, dispatcher)   // 类比 progress.WithSink
internal/runtime & Gateway & Skill service
  -> hook/contract.FromContext(ctx).Dispatch(...)
```

### 6.3 核心接口（Go 签名草案）

> 更完整的 model / service / adapter / 接入点骨架代码见 **§13.4 关键骨架代码**。

```go
// internal/capabilities/hook/contract/dispatcher.go
package contract

// Dispatcher 是内核触发 Hook 的唯一入口。实现必须并发安全、快速返回。
type Dispatcher interface {
    // Dispatch 触发一次事件的所有匹配 Hook，聚合决策后返回。
    // 对不可阻断事件，实现应保证即便 handler 失败也不影响主流程（fail-open）。
    Dispatch(ctx context.Context, ev model.Event) (model.AggregateResult, error)
}

// context 注入（对齐 progress.WithSink / FromContext）
func WithDispatcher(ctx context.Context, d Dispatcher) context.Context
func FromContext(ctx context.Context) Dispatcher   // 无注入时返回 no-op
```

```go
// internal/capabilities/hook/model/event.go
package model

type EventName string

const (
    EventRunStart         EventName = "RunStart"
    EventUserPromptSubmit EventName = "UserPromptSubmit"
    EventPreToolUse       EventName = "PreToolUse"
    EventPostToolUse      EventName = "PostToolUse"
    EventPreSkillUse      EventName = "PreSkillUse"
    EventPostSkillUse     EventName = "PostSkillUse"
    EventStop             EventName = "Stop"
    EventRunComplete      EventName = "RunComplete"
    // 预留
    EventPreCompact    EventName = "PreCompact"
    EventSubagentStart EventName = "SubagentStart"
    EventSubagentStop  EventName = "SubagentStop"
    EventPermissionRequest EventName = "PermissionRequest"
)

// Event 是一次事件的完整上下文（内核构造）。
type Event struct {
    Name       EventName
    MatchKey   string            // matcher 匹配对象：工具名/技能名/trigger，空则仅 * 生效
    Payload    map[string]any    // 序列化为 stdin JSON 的扩展字段
    // 通用字段由 Dispatcher 从 context（correl）自动补齐：run_id/session_id/tenant_id...
}

// Decision 单条 Hook 的归一化结果。
type Decision struct {
    Continue           bool
    PermissionDecision string // allow|deny|ask|"" 
    Reason             string
    UpdatedInput       map[string]any
    AdditionalContext  string
    SystemMessage      string
    ExitCode           int
    Err                error
}

// AggregateResult 聚合后的最终决策。
type AggregateResult struct {
    Blocked           bool
    BlockReason       string
    NeedApproval      bool     // 存在 ask，需桥接 approval.Service
    UpdatedInput      map[string]any
    AdditionalContext []string // 注入下一轮 system
    SystemMessages    []string
    Warnings          []string
}
```

```go
// internal/capabilities/hook/contract/handler.go
package contract

// Runner 执行单个 handler，返回归一化 Decision。
type Runner interface {
    Run(ctx context.Context, spec model.HandlerSpec, inputJSON []byte) model.Decision
    Kind() string // "command" | "builtin" | "prompt"
}
```

### 6.4 命令执行与边界取舍（重要）

`docs/项目目录与边界说明.md §能力域边界` 规定“工具层不得直接 `exec.Command`，命令执行走 `execution` 能力的 `CommandRunner`/`SandboxRunner`”。Hook command handler 需要 spawn 子进程并向其 **stdin 写 JSON**、读 **stdout/exit**，取舍如下：

- **统一走 `execution` 能力的 `ExecutionRunner`**（`internal/capabilities/execution/contract/runner.go:83`）：由它负责 shell 选择（`Command.Shell=auto`）、宿主/sandbox 选择、超时（`RunOptions.Timeout`）、输出上限（`RunOptions.MaxOutputBytes`）。`hook/service` / `hook/adapter/command` **不直接依赖 `os/exec`**，也不重复 shell 逻辑。
- **执行环境由产品注入的 runner 决定，而非 hook 内分支**：
  - CLI/Desktop bootstrap 注入宿主机 `CommandRunner`（`shared/local/execution`）。
  - Enterprise bootstrap 注入 `SandboxRunner` 组合（见 §6.5.3），**不引入 `shared/local` 宿主执行**。
- **前置依赖（阻塞项，已决策 A，见 §12）**：当前 `execution/model.Command`（`model.go:168-176`）**无 stdin 字段**，`Result` 也不区分 payload 通道。Hook 协议要求 stdin 传 payload。**已定：扩展 `execution` 契约**——`Command` 增加 `Stdin []byte`，`CommandRunner`/`SandboxRunner` 实现将其写入子进程 stdin。这是跨能力域改动，实现前须与 execution owner 对齐（P2 前置）。不采用“临时文件 + env 指路径”备选。
- `hooks.execution`（`host|sandbox`）为**声明式意图**，实际由 bootstrap 注入的 runner 落实；schema 默认 `host`，**Enterprise bootstrap 覆盖为 `sandbox`**（见 §6.5.2）。

### 6.5 产品形态：CLI / Desktop / Enterprise 的统一与独立

Hook 遵循 `AGENTS.md §核心原则`「系统装配集中在 bootstrap；主流程保持通用」。因此**内核统一、产品差异只在 Composition Root（`products/<product>/bootstrap`）注入**，不在内核里出现产品分支。

#### 6.5.1 统一层（shared kernel，三产品共用，不含产品语义）

以下全部落在 `internal/*`，三产品完全复用，**不感知**运行在哪个产品：

| 统一项 | 位置 |
|---|---|
| 事件模型 / payload / decision schema / exit-stdout 语义 | `hook/model`、`hook/contract` |
| Dispatcher / matcher / aggregate / loader（多层 merge、trust 校验） | `hook/service` |
| 内核接入点（Gateway、Engine、Skill、approval、observability） | §7 |
| 配置 schema（`HooksConfig`） | `internal/platform/config` |
| builtin 守卫注册表 | `hook/adapter/builtin` |
| 注入方式（`app.RunOnce` 统一 `hook.WithDispatcher(ctx, ...)`，与 `progress.WithSink` 同处） | `internal/app/run_service.go` |

> 注入点统一在 `internal/app` 应用层：CLI 从 TUI/命令、Desktop 从 Wails、Enterprise 从 HTTP/SSE 触发 `RunOnce`，三者共用同一段 `hook.WithDispatcher`，无需各自改内核。

#### 6.5.2 独立层（各产品 bootstrap 注入的差异）

产品差异只体现在“注入什么实现 + 默认策略 + 边界约束”，Dispatcher 行为不变：

| 维度 | CLI | Desktop | Enterprise |
|---|---|---|---|
| **command 执行 runner** | 宿主机 `CommandRunner`（`shared/local/execution`） | 宿主机 `CommandRunner`（`shared/local/execution`），可选本机 sandbox | **不引入 `shared/local` 宿主执行**；只允许 `SandboxRunner` 或 builtin（见 6.5.3） |
| **配置来源** | `configs/*.yaml` + `~/.genesis-agent/cli/config.yaml` + 项目 `.genesis/hooks` | 同 CLI（`~/.genesis-agent/desktop/config.yaml`）+ 桌面设置 UI | 中心化 managed requirements + 租户策略下发；默认忽略本地任意 command hook |
| **默认策略** | `enabled=true, execution=host, allow_managed_only=false` | 同 CLI | `execution=sandbox, allow_managed_only=true`（默认关闭任意宿主 command） |
| **信任确认交互** | 终端交互式提示（`products/cli/internal` 接入面） | 桌面弹窗 dialog | 无交互；managed hooks 由企业签名/`trusted_hash` 预信任，非 managed command 默认禁用或走管理员审批 |
| **审批（ask 桥接）** | 终端确认 | 桌面 UI 确认 | 企业审批流（policy engine + human intervention API），异步、`tenant_id` 维度 |
| **观测 sink** | 文件（`.genesis/logs`） | 文件（+可选本地 DB） | 中心化 audit/usage，查询必须带 `tenant_id` |
| **管理 UI（`hooks/list` 预留）** | `/hooks` TUI（后续） | 设置面板（后续） | 企业控制台（后续） |
| **目录归属** | `products/cli/bootstrap`+`internal` | `products/desktop/bootstrap`+`internal` | `products/enterprise/bootstrap`+`internal` |

#### 6.5.3 Enterprise 硬边界（安全关键）

`docs/项目目录与边界说明.md §产品边界` 规定「Enterprise 默认不引入 `shared/local` 的本地主机执行、文件系统实现」。因此 **Enterprise 绝不允许在服务宿主机执行任意用户 command hook**（既违反目录边界，也是多租户 RCE 风险面）。Enterprise 的 command hook 只能是以下三选一：

1. **builtin（Go）**：编译进内核的受信守卫，无任意代码执行面。
2. **managed + sandbox**：企业下发、经 `trusted_hash`/签名校验、在 `SandboxRunner` 内执行。
3. **完全禁用 command**：仅保留 builtin + 观测型 hook。

**已定（决策 F，见 §12）**：Enterprise command hook 的**最终形态为「managed + sandbox」（第 2 项）**，非永久禁用；但该能力**后续阶段实现**（§11 P7）。在其落地前，Enterprise **暂按第 3 项运行**（command 不启用，仅 builtin + 观测），以最小化早期多租户 RCE 面。

Dispatcher/loader 提供 `execution` 与 `allow_managed_only` 开关即可覆盖以上三态切换，无需 Enterprise 专属内核代码。

#### 6.5.4 与统一能力适用范围（scope）的关系

`AGENTS.md §核心原则` 要求「Tool / MCP / Skill / Sandbox 使用统一能力适用范围配置，按接入端、租户、项目、Agent、用户、角色、运行环境过滤」。**Hook 应纳入同一适用范围过滤体系**：`loader.Load` 时按当前 scope（由 bootstrap 从 `profile` / 租户上下文注入）过滤事件与 handler，避免另造一套过滤逻辑。这样 Enterprise 的租户/项目/角色隔离对 Hook 天然生效，CLI/Desktop 使用退化的单租户 scope。

#### 6.5.5 装配示意（各产品 bootstrap）

```text
# CLI / Desktop（shared/local host runner）
products/<cli|desktop>/bootstrap:
  runner := localexec.NewHostCommandRunner(...)          // shared/local/execution
  disp   := hooksvc.NewDispatcher(hookloader.Load(cfg.Hooks, scope), runner, sinks...)
  // RunOnce 时 hook.WithDispatcher(ctx, disp)  ← 与 progress.WithSink 同处（internal/app）

# Enterprise（sandbox runner，managed-only）
products/enterprise/bootstrap:
  cfg.Hooks.Execution = "sandbox"; cfg.Hooks.AllowManagedOnly = true
  runner := sandboxexec.NewSandboxCommandRunner(sandboxClient)   // 经 SandboxRunner
  disp   := hooksvc.NewDispatcher(hookloader.LoadManaged(policy, tenantScope), runner, centralSinks...)
```

---

## 7. 与现有系统的接入点（精确到文件/行）

### 7.1 PreToolUse / PostToolUse → Tool Gateway

复用 Gateway 现有 `Authorizer` 扩展位所在位置（`gateway.go:118-176`），在其前后插入 Hook：

```text
gateway.Execute(ctx, name, params):
  isAllowed / registry.Get / isExecutable        # 现有
  record start + trace start (defer finish)        # 现有 gateway.go:135-154
  ── [新增] PreToolUse:
      res := hook.FromContext(ctx).Dispatch(PreToolUse{tool_name:name, tool_input:params})
      if res.Blocked  -> return error(res.BlockReason)
      if res.NeedApproval -> 走 approval.Service（见 7.4）
      if res.UpdatedInput -> params = merge(params, res.UpdatedInput)
      注入 res.AdditionalContext -> 经 context 传回 runtime（见 7.5）
  authz.AuthorizeTool (可选，现有 gateway.go:156-168)  # 保留
  acquire lock / t.Execute                          # 现有
  ── [新增] PostToolUse:
      hook.Dispatch(PostToolUse{tool_name, tool_result, success, duration_ms})
```

理由：Gateway 是**唯一**工具执行必经通道（ReAct `registry` 实际是 Gateway），挂在此处对所有策略生效，且已有 trace/audit/correl 基础设施可复用。

### 7.2 RunStart / UserPromptSubmit / Stop / RunComplete → ReactLoopEngine

| 事件 | 接入位置（`internal/runtime/strategy/react/react_loop.go`） |
|---|---|
| `RunStart` | `Start` 中，紧邻 `progress.Emit(KindRun,start)` 之后、`loop` 之前（约 L110-L129）；`additionalContext` 汇入 `prompt.BuildRequest` 或经 context 注入到 system 构建（L166） |
| `UserPromptSubmit` | `loop` 中构造用户消息前（约 L179-L180）；`Blocked` 则直接以提示结束本轮 |
| `Stop` | 得到最终回答、准备 return 前；`decision=block` 则要求继续（追加提示继续循环，参考 Kode 递归重入，**最多 5 次**防死循环，用 `stop_active` 标记；决策 C） |
| `RunComplete` | `Start` 尾部 `progress.Emit(KindRun,complete/error)` 之后（L143-L149），fire-and-forget（对齐 codex legacy notify） |

注入方式：`app.RunOnce`（`internal/app/run_service.go:22-24`）在 `progress.WithSink` 旁增加 `hook.WithDispatcher(ctx, ...)`。

> **策略无关性**：`RunStart` / `RunComplete` 是 Run 级事件，建议**上提到 `app.RunOnce`（`runEngine.Start` 前后触发）**，这样对 ReAct / Plan-Execute / Coding / RAG 等所有策略天然统一，不必每个 Engine 各写一遍（呼应 §1.2 目标）。`UserPromptSubmit` / `Stop` 属循环内语义，无法上提，需由每个 Engine 在其循环内自行触发；为此可在 `hook/contract` 之上提供一个薄 helper（如 `hook.DispatchStop(ctx, ...)`）供各策略复用，减少重复。表中 `react_loop.go` 行号仅为 ReAct 的落点示例。

### 7.3 PreSkillUse / PostSkillUse → Skill 网关

- `Skill(skill=...)` 工具（`internal/capabilities/skill/tool/skill/tool.go` 的 `load`）：**仅在 model 调用路径**（`invocation="model"`）在 `Service.Resolve` 后、`Service.Load` 前触发 `PreSkillUse`（可阻断加载）；注入完成后触发 `PostSkillUse`。mention/explicit 加载暂不拦截（决策 E）。
- `run_skill_command` 服务（`internal/capabilities/skill/script/service/service.go`）作为普通工具已被 Gateway 的 `PreToolUse`/`PostToolUse` 覆盖，无需重复挂。

### 7.4 PermissionRequest / ask 桥接 → approval.Service

- `PreToolUse`/`PreSkillUse` 返回 `permissionDecision=ask` 或聚合 `NeedApproval` → 调用现有 `approval.Service`（`internal/capabilities/approval/service/service.go:57-116`）走人工审批。
- `permissionDecision=deny` → 直接阻断（等价 codex `PermissionRequest` deny 短路，`orchestrator.rs:510-558`）。
- `permissionDecision=allow` → 可跳过后续策略/审批（等价 Kode allow）。
- 审批通过后复用现有 `ApprovalGrantedHook`（`react_loop.go:122-128`）清 Repeat Guard。

### 7.5 additionalContext 注入回 system

对齐 Kode `drainHookSystemPromptAdditions`（`message-pipeline.ts:273-276`）：Hook 产出的 `additionalContext` 不直接改对话，而是**入队**，下一轮构建 system prompt 时经 `prompt.ContextInjector`（`internal/runtime/prompt/interface.go:25-28`）注入。队列可挂在 `RunContext`（`internal/runtime/context.go`）。

> **并发安全**：ReAct 对只读且 `ConcurrencySafe` 的同级工具会并行执行（`scheduler.Queue`，`react_loop.go:467-478`），因此多个 `PreToolUse`/`PostToolUse` 的 Hook 可能并发向该队列写入。注入队列必须并发安全（加锁或 channel 收敛到主循环），且下一轮消费顺序需确定（按入队序）。改写 `tool_input`（`updatedInput`）只作用于当前工具调用，天然隔离，不入共享队列。

### 7.6 观测复用

- Hook 执行进 `trace.Tracer`（span `hook.dispatch` / `hook.run`）。
- Hook 决策进 `audit.Sink`（category=`hook`，action=`{event}.{decision}`），复用 `correl.Enrich`（`gateway.go:266-302` 同款）。
- Hook 运行态经 `progress.Emit`：新增 `progress.KindHook`（`progress.go:12-20` 增常量），phase start/complete/error，供 UI 显示“正在运行 hook: xxx”（对齐 codex `HookStarted/Completed`）。

---

## 8. 配置设计（YAML）

### 8.1 配置位置与优先级（复用 viper 多层，`config.go:3-8`）

1. `configs/config.yaml`（默认，提交版本库，通常空/示例）
2. `configs/config.local.yaml`（本地覆盖）
3. `~/.genesis-agent/<product>/config.yaml`（用户级）
4. 企业 managed：`requirements`（`allow_managed_only=true` 时忽略上面三层的 hooks）
5. `AGENT_` 环境变量（仅开关类，如 `AGENT_HOOKS_ENABLED`）

多层 hooks **追加合并**（对齐 Kode，非覆盖）；`enabled`/`trusted_hash` 就近覆盖。

### 8.2 配置结构（新增顶层 `hooks:`，映射 `config.Config`）

```yaml
hooks:
  enabled: true                 # 全局开关（对齐 Kode disableAllHooks 反义）
  execution: host               # host | sandbox（Enterprise bootstrap 覆盖为 sandbox，见 §6.4/§6.5）
  allow_managed_only: false     # 企业：仅 managed hooks 生效
  default_timeout: 30s          # command handler 默认超时
  events:
    PreToolUse:
      - matcher: "run_command|run_skill_command"   # 精确/别名；支持 glob 与 /regex/
        handlers:
          - type: command
            command: "python .genesis/hooks/guard_bash.py"
            command_windows: "python .genesis\\hooks\\guard_bash.py"
            timeout: 10s
            async: false
            status_message: "安全校验中"
            env:
              GUARD_MODE: strict
      - matcher: "*"
        handlers:
          - type: builtin
            name: git_branch_guard          # 内置 Go handler（§9.2）
    PostToolUse:
      - matcher: "write_file|edit_file"
        handlers:
          - type: command
            command: "node .genesis/hooks/format.js"
    UserPromptSubmit:
      - handlers:                            # 无 matcher = 全匹配
          - type: command
            command: "python .genesis/hooks/inject_context.py"
    RunComplete:
      - handlers:
          - type: command
            command: "python .genesis/hooks/notify.py"
            async: true                      # fire-and-forget
  state:                                     # per-hook 治理（对齐 codex hooks.state）
    # key = 稳定身份指纹（event + matcher + handler 规范化后 SHA-256 前 12 位），
    # 不用位置索引，避免配置重排后 state 错位。
    "PreToolUse:sha256:9f2a1c7b3e4d":
      enabled: true
      trusted_hash: "sha256:..."            # 命令内容指纹，防篡改
```

> **state key 稳定性**：不采用 `event:组序号:handler序号` 这类位置索引（配置增删/重排即错位、信任被张冠李戴）。key 由 `event + matcher + handler(type/command/name)` 规范化后哈希得到，随内容而非位置变化；命令改动时 `trusted_hash` 自然失配需重新确认。

### 8.3 对应 Go 配置模型（`internal/platform/config/config.go`）

```go
// Config 增加字段
type Config struct {
    // ... 现有字段
    Hooks HooksConfig `mapstructure:"hooks"`
}

type HooksConfig struct {
    Enabled          bool                          `mapstructure:"enabled"`
    Execution        string                        `mapstructure:"execution"` // host|sandbox
    AllowManagedOnly bool                          `mapstructure:"allow_managed_only"`
    DefaultTimeout   time.Duration                 `mapstructure:"default_timeout"`
    Events           map[string][]HookMatcherGroup `mapstructure:"events"`
    State            map[string]HookState          `mapstructure:"state"`
}

type HookMatcherGroup struct {
    Matcher  string            `mapstructure:"matcher"`
    Handlers []HookHandlerSpec `mapstructure:"handlers"`
}

type HookHandlerSpec struct {
    Type           string            `mapstructure:"type"`   // command|builtin|prompt
    Name           string            `mapstructure:"name"`   // builtin 名
    Command        string            `mapstructure:"command"`
    CommandWindows string            `mapstructure:"command_windows"`
    Timeout        time.Duration     `mapstructure:"timeout"`
    Async          bool              `mapstructure:"async"`
    StatusMessage  string            `mapstructure:"status_message"`
    Env            map[string]string `mapstructure:"env"`
}

type HookState struct {
    Enabled     *bool  `mapstructure:"enabled"`
    TrustedHash string `mapstructure:"trusted_hash"`
}
```

### 8.4 Matcher 规则（`hook/service/matcher.go`）

对齐 codex `matches_matcher`（`common.rs:129-139`）+ Kode `minimatch`：

1. 空/缺省 matcher → 全匹配（仅无 MatchKey 事件或视为 `*`）。
2. `*` / `all` → 全匹配。
3. 含 `|` → 按别名精确匹配任一。
4. `/pattern/` → 正则。
5. 其他 → 先精确，再 glob（`path.Match` 风格）。

---

## 9. 安全与治理

### 9.1 信任模型（trust）

- **首次发现**外部 command hook 默认 **untrusted**，需用户确认或配置 `trusted_hash` 后才执行（对齐 codex `HookTrustStatus` / `trusted_hash`）。
- `trusted_hash` = 对 `type+command(+command_windows)` 规范化后的 SHA-256；命令被改动则失配 → 视为 modified → 需重新确认（防供应链/配置篡改）。
- `bypass_hook_trust`（仅开发/CI 显式开）跳过校验。

### 9.2 内置守卫（builtin，非配置外部命令）

在 `hook/adapter/builtin` 内以 Go 实现、随内核编译、**先于**外部 command hook 执行（对齐 Kode builtin guard，`tool-call.ts:78`）。示例：

- `git_branch_guard`：拦截 `git switch/checkout` 切分支类危险命令。
- `secret_path_guard`：拦截读取 `.env`/凭据文件的 PreToolUse（与 filesystem binarygate 协同）。

内置守卫默认启用，可经 `state` 关闭。

### 9.3 企业 managed-only

`allow_managed_only=true` 时忽略 user/project/local 层 hooks，仅执行企业下发的 managed hooks（对齐 codex `allow_managed_hooks_only`）。由 `products/enterprise/bootstrap` 注入 managed 来源。

### 9.4 失败策略（fail-open vs fail-closed）

- **可阻断事件**（PreToolUse/PreSkillUse/Stop/UserPromptSubmit）：handler 自身执行错误（spawn 失败/超时）默认 **fail-open**（记 warning 放行），避免 hook 故障锁死 Agent；可按 handler 配 `fail_closed: true` 改为阻断（高安全场景）。
- **通知型事件**（RunComplete/PostToolUse）：始终 fail-open。
- `exit 2` / 明确 `deny`：一律阻断（这是显式意图，非故障）。

### 9.5 超时、并行、隔离

- 每 handler 独立超时（默认 `default_timeout`，可覆盖），超时 kill 子进程（`kill_on_drop` 语义，对齐 codex `command_runner.rs`）。
- 同事件多 handler **并行执行、按配置顺序聚合**（对齐 codex `dispatcher.rs`）。
- `async: true` 的 handler fire-and-forget，不阻塞主流程（用于通知）。
- 子进程 `stdin` 写 payload JSON、`stdout` 收决策、`stderr` 收原因/日志；工作目录=业务 cwd；env 注入元信息（`GENESIS_RUN_ID`、`GENESIS_PROJECT_DIR` 等，兼容 `CLAUDE_*`/`KODE_*` 便于脚本复用）。

---

## 10. 执行时序（PreToolUse 为例）

```text
ReactLoopEngine.runToolCall
  -> Gateway.Execute(ctx, name, params)
       -> hook.FromContext(ctx).Dispatch(PreToolUse{name, params})
            service.Dispatcher:
              1. registry 取 PreToolUse 的 matcher 组
              2. matcher.Match(name) 过滤 + state(enabled/trust) 过滤
              3. 序列化通用+扩展字段为 stdin JSON
              4. 并行 Runner.Run（command: spawn / builtin: 调 Go func）
              5. aggregate 聚合 -> AggregateResult
       -> Blocked?      -> 返回 error（工具不执行，结果回灌 LLM）
       -> NeedApproval? -> approval.Service.Authorize（人工）
       -> UpdatedInput? -> params = merge
       -> emit progress(KindHook) / audit(category=hook) / trace(span)
       -> authz / acquire / t.Execute
       -> hook.Dispatch(PostToolUse{...})
```

---

## 11. 分阶段实施计划

| 阶段 | 内容 | 交付 |
|---|---|---|
| **P1：内核骨架** | `hook/contract`+`model`；no-op Dispatcher；context 注入；`progress.KindHook` | 编译通过，无行为变化 |
| **P2：command adapter** | command handler I/O 协议（组装 stdin JSON、解析 exit/stdout、超时映射）委托 `execution.ExecutionRunner`；并行执行 + 决策聚合；builtin registry | 单测覆盖 exit0/1/2、deny/allow/updatedInput、超时、聚合 |
| **P3：接入 Gateway** | PreToolUse/PostToolUse 接 `gateway.Execute`；audit/trace 打点 | Gateway 集成测试 |
| **P4：接入 Engine** | RunStart/RunComplete 上提到 `app.RunOnce`；UserPromptSubmit/Stop（重入≤5）接 `react_loop`；additionalContext 注入队列（并发安全） | ReAct 集成测试 |
| **P5：Skill + 审批桥接** | PreSkillUse（仅 model 调用）/PostSkillUse；ask/deny 桥接 `approval.Service` | Skill/审批集成测试 |
| **P6：治理 + 产品装配** | trust hash（内容指纹）、全局/单条禁用、managed-only、内置守卫（git/secret）；CLI/Desktop 注入宿主 runner；**Enterprise 暂按「command 禁用、仅 builtin + 观测」装配**（决策 F）+ managed-only + tenant scope（§6.5）；纳入统一能力适用范围过滤 | 治理测试 + `check_product_isolation.ps1` |
| **P7（后续）** | **Enterprise command hook（managed + sandbox，决策 F）**；prompt handler；PreCompact/Subagent 事件；`hooks/list` 管理契约与 UI；plugin HookSource | — |

> **P2 前置（阻塞）**：需先与 execution owner 落地 `Command.Stdin` 契约扩展（§6.4 / 决策 A）；未落地前 P2 先仅实现 builtin handler，command handler 待契约就绪。

每阶段验证（Windows / 仓库缓存）：

```powershell
$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'; $env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'; go test ./internal/capabilities/hook/...
```

涉及产品隔离时追加：`.\scripts\check_product_isolation.ps1`。

---

## 12. 已确认决策

> 以下为设计评审结论（2026-07-12 定），后续实现以此为准。

- **A. Hook command 执行契约** —— **定：扩展 `execution` 契约加 `Command.Stdin`**。统一经 `execution.ExecutionRunner`，由其负责 shell/宿主 vs sandbox/超时/输出上限；不采用“临时文件 + env 指路径”备选。该改动跨能力域，实现前需与 execution owner 对齐落地 `Command.Stdin`（P2 前置）。
- **B. Dispatcher 注入方式** —— **定：context 注入**（对齐 `progress.Sink` / `ApprovalGrantedHook`，跨策略通用）。不采用 `ReactLoopEngine` 依赖字段方案。
- **C. Stop 重入上限** —— **定：默认 5 次**（对齐 Kode），超限强制结束，用 `stop_active` 标记防死循环。
- **D. 事件命名** —— **定：保留 Claude 生态英文事件名**（`PreToolUse` 等），最大化脚本复用。
- **E. `PreSkillUse` 粒度** —— **定：仅拦截 model 调用的 `Skill(...)`**；mention/explicit 加载暂不拦截（后续需要再评估）。
- **F. Enterprise command hook 策略** —— **定：最终形态为 managed + sandbox**（6.5.3 第 2 项），非“永久禁用”。**但该能力后续阶段实现**（见 §11 P7）；在其落地前，Enterprise command hook 暂不启用，早期仅 `builtin` + 观测型 hook 生效。

---

## 13. 附录

### 13.1 hook 脚本示例（PreToolUse，阻断危险命令）

```python
# .genesis/hooks/guard_bash.py
import sys, json
data = json.load(sys.stdin)
cmd = (data.get("tool_input") or {}).get("command", "")
if "rm -rf /" in cmd:
    print(json.dumps({
        "hookSpecificOutput": {"permissionDecision": "deny",
                                "permissionDecisionReason": "禁止全盘删除"}
    }))
    sys.exit(0)          # 结构化 deny
# 或简易阻断：sys.stderr.write("blocked"); sys.exit(2)
print(json.dumps({"continue": True}))
```

### 13.2 hook 脚本示例（RunComplete 通知，异步）

```python
# .genesis/hooks/notify.py
import sys, json
d = json.load(sys.stdin)
# 发企业 IM / webhook；stdout 被忽略（async fire-and-forget）
```

### 13.3 环境变量（注入 command handler）

| genesis 变量 | 兼容别名 | 含义 |
|---|---|---|
| `GENESIS_RUN_ID` | — | 当前 Run ID |
| `GENESIS_SESSION_ID` | — | 当前 Session ID |
| `GENESIS_PROJECT_DIR` | `CLAUDE_PROJECT_DIR` / `KODE_PROJECT_DIR` | 业务工作目录（沙箱模式为 workspace-relative） |
| `GENESIS_HOOK_EVENT` | — | 事件名 |

### 13.4 关键骨架代码（开发起步参考）

> 以下为**设计草案骨架**，非最终实现；函数体以关键流程 + `TODO` 标注为主，正式开发时补全。全部遵循 §6 的目录与依赖边界、§12 的决策 A–F。Go 代码、UTF-8、中文注释。

#### 13.4.1 `internal/capabilities/hook/model` — 数据模型

```go
// model/event.go
package model

// EventName 事件枚举（保留 Claude 生态英文名，决策 D）。
type EventName string

const (
	EventRunStart         EventName = "RunStart"
	EventUserPromptSubmit EventName = "UserPromptSubmit"
	EventPreToolUse       EventName = "PreToolUse"
	EventPostToolUse      EventName = "PostToolUse"
	EventPreSkillUse      EventName = "PreSkillUse"
	EventPostSkillUse     EventName = "PostSkillUse"
	EventStop             EventName = "Stop"
	EventRunComplete      EventName = "RunComplete"
	// 预留（接入点未落地）
	EventPreCompact        EventName = "PreCompact"
	EventSubagentStart     EventName = "SubagentStart"
	EventSubagentStop      EventName = "SubagentStop"
	EventPermissionRequest EventName = "PermissionRequest"
)

// Blockable 返回该事件是否允许 Hook 阻断。
func (e EventName) Blockable() bool {
	switch e {
	case EventUserPromptSubmit, EventPreToolUse, EventPreSkillUse, EventStop,
		EventPreCompact, EventSubagentStart, EventSubagentStop, EventPermissionRequest:
		return true
	default: // RunStart / PostToolUse / PostSkillUse / RunComplete
		return false
	}
}

// Event 一次事件的完整上下文（内核构造）。
type Event struct {
	Name     EventName
	MatchKey string         // matcher 匹配对象：工具名/技能名/trigger；空则仅 "*" 生效
	Payload  map[string]any // 事件扩展字段（序列化进 stdin JSON 的 hookSpecificInput）
	Async    bool           // RunComplete 等 fire-and-forget
}

// Decision 单条 Hook 的归一化结果。
type Decision struct {
	Continue           bool           // false = 停止后续 hook 与动作
	PermissionDecision string         // allow|deny|ask|""（仅 Pre* 有意义）
	Reason             string
	UpdatedInput       map[string]any // 仅 PreToolUse
	AdditionalContext  string
	SystemMessage      string
	ExitCode           int
	Warning            string // 非 0/2 退出或 spawn 失败时的告警
}

// AggregateResult 多条 Hook 聚合后的最终决策。
type AggregateResult struct {
	Blocked           bool
	BlockReason       string
	NeedApproval      bool // 存在 ask，需桥接 approval.Service
	UpdatedInput      map[string]any
	AdditionalContext []string
	SystemMessages    []string
	Warnings          []string
}
```

```go
// model/config.go —— 与 internal/platform/config 的 HooksConfig 对齐（§8.3）
package model

import "time"

type HandlerSpec struct {
	Type           string            // command|builtin|prompt
	Name           string            // builtin 名
	Command        string
	CommandWindows string
	Timeout        time.Duration
	Async          bool
	FailClosed     bool // 高安全场景：handler 故障视为阻断（默认 fail-open）
	Env            map[string]string
	// Identity 由 event+matcher+handler 规范化哈希得到，作为 trust/state 的稳定 key（§8.2）。
	Identity string
}

type MatcherGroup struct {
	Matcher  string
	Handlers []HandlerSpec
}

// EventRules 事件 -> matcher 组（loader 展开多层配置后的运行时索引）。
type EventRules map[EventName][]MatcherGroup

// TrustState per-handler 治理（key = HandlerSpec.Identity）。
type TrustState struct {
	Enabled     *bool
	TrustedHash string
}
```

#### 13.4.2 `internal/capabilities/hook/contract` — 内核依赖接口

```go
// contract/dispatcher.go
package contract

import (
	"context"

	"genesis-agent/internal/capabilities/hook/model"
)

// Dispatcher 内核触发 Hook 的唯一入口。实现必须并发安全、快速返回。
type Dispatcher interface {
	// Dispatch 触发一次事件的所有匹配 Hook，聚合决策后返回。
	// 不可阻断事件即便 handler 失败也不得影响主流程（fail-open）。
	Dispatch(ctx context.Context, ev model.Event) (model.AggregateResult, error)
}

type dispatcherKey struct{}

// WithDispatcher 注入 Dispatcher（对齐 progress.WithSink）。
func WithDispatcher(ctx context.Context, d Dispatcher) context.Context {
	if d == nil {
		return ctx
	}
	return context.WithValue(ctx, dispatcherKey{}, d)
}

// FromContext 取出 Dispatcher；未注入时返回 no-op，调用方无需判空。
func FromContext(ctx context.Context) Dispatcher {
	if ctx != nil {
		if d, ok := ctx.Value(dispatcherKey{}).(Dispatcher); ok && d != nil {
			return d
		}
	}
	return nopDispatcher{}
}

type nopDispatcher struct{}

func (nopDispatcher) Dispatch(context.Context, model.Event) (model.AggregateResult, error) {
	return model.AggregateResult{}, nil
}
```

```go
// contract/handler.go
package contract

import (
	"context"

	"genesis-agent/internal/capabilities/hook/model"
)

// Runner 执行单个 handler，返回归一化 Decision。command / builtin / prompt 各一实现。
type Runner interface {
	Run(ctx context.Context, spec model.HandlerSpec, inputJSON []byte) model.Decision
	Kind() string // "command" | "builtin" | "prompt"
}
```

#### 13.4.3 `internal/capabilities/hook/service` — Dispatcher / matcher / aggregate

```go
// service/dispatcher.go
package service

import (
	"context"
	"encoding/json"
	"sync"

	"genesis-agent/internal/capabilities/hook/contract"
	"genesis-agent/internal/capabilities/hook/model"
	"genesis-agent/internal/platform/logger/correl"
)

type Dispatcher struct {
	rules   model.EventRules
	trust   map[string]model.TrustState        // key = HandlerSpec.Identity
	runners map[string]contract.Runner         // kind -> Runner
	enabled bool
}

func NewDispatcher(rules model.EventRules, trust map[string]model.TrustState, runners map[string]contract.Runner, enabled bool) *Dispatcher {
	return &Dispatcher{rules: rules, trust: trust, runners: runners, enabled: enabled}
}

func (d *Dispatcher) Dispatch(ctx context.Context, ev model.Event) (model.AggregateResult, error) {
	if !d.enabled {
		return model.AggregateResult{}, nil
	}
	// 1. 选出匹配 + 启用 + 受信的 handler
	handlers := d.selectHandlers(ev)
	if len(handlers) == 0 {
		return model.AggregateResult{}, nil
	}
	// 2. 组装 stdin JSON（通用字段由 correl 从 ctx 补齐 run_id/session_id/tenant_id）
	inputJSON := d.buildInput(ctx, ev)

	// 3. 并行执行、按配置顺序保序收集（决策：并行 + 保序聚合）
	decisions := make([]model.Decision, len(handlers))
	var wg sync.WaitGroup
	for i, h := range handlers {
		if h.Async { // fire-and-forget（RunComplete 等）
			go d.runOne(ctx, h, inputJSON)
			continue
		}
		wg.Add(1)
		go func(idx int, spec model.HandlerSpec) {
			defer wg.Done()
			decisions[idx] = d.runOne(ctx, spec, inputJSON)
		}(i, h)
	}
	wg.Wait()

	// 4. 聚合
	res := aggregate(ev, handlers, decisions)
	// 5. 观测：trace/audit/progress（略，见 §7.6）
	_ = correl.Enrich // 占位：补齐关联键后写 audit
	return res, nil
}

func (d *Dispatcher) runOne(ctx context.Context, spec model.HandlerSpec, in []byte) model.Decision {
	r := d.runners[spec.Type]
	if r == nil {
		return model.Decision{Continue: true, Warning: "no runner for kind " + spec.Type}
	}
	return r.Run(ctx, spec, in)
}

func (d *Dispatcher) selectHandlers(ev model.Event) []model.HandlerSpec {
	var out []model.HandlerSpec
	for _, g := range d.rules[ev.Name] {
		if !matchMatcher(g.Matcher, ev.MatchKey) {
			continue
		}
		for _, h := range g.Handlers {
			if st, ok := d.trust[h.Identity]; ok && st.Enabled != nil && !*st.Enabled {
				continue // 单条禁用
			}
			// TODO: command 类型校验 trust（TrustedHash 与内容指纹一致），失配则跳过并告警
			out = append(out, h)
		}
	}
	return out
}

func (d *Dispatcher) buildInput(ctx context.Context, ev model.Event) []byte {
	runID, sessionID, _ := correl.Enrich(ctx, "", "", nil)
	payload := map[string]any{
		"hook_event_name": ev.Name,
		"schema_version":  "1",
		"run_id":          runID,
		"session_id":      sessionID,
		// TODO: tenant_id / agent_name / cwd / permission_mode / model
	}
	for k, v := range ev.Payload {
		payload[k] = v
	}
	b, _ := json.Marshal(payload)
	return b
}
```

```go
// service/matcher.go —— 对齐 codex matches_matcher + Kode glob（§8.4）
package service

import (
	"path"
	"regexp"
	"strings"
)

func matchMatcher(matcher, key string) bool {
	m := strings.TrimSpace(matcher)
	if m == "" || m == "*" || m == "all" {
		return true
	}
	if strings.Contains(m, "|") { // 别名精确匹配
		for _, c := range strings.Split(m, "|") {
			if strings.TrimSpace(c) == key {
				return true
			}
		}
		return false
	}
	if strings.HasPrefix(m, "/") && strings.HasSuffix(m, "/") && len(m) > 1 { // /regex/
		if re, err := regexp.Compile(m[1 : len(m)-1]); err == nil {
			return re.MatchString(key)
		}
		return false
	}
	if m == key {
		return true
	}
	ok, _ := path.Match(m, key) // glob
	return ok
}
```

```go
// service/aggregate.go —— deny 优先；updatedInput 顺序覆盖（§5.5）
package service

import "genesis-agent/internal/capabilities/hook/model"

func aggregate(ev model.Event, handlers []model.HandlerSpec, decisions []model.Decision) model.AggregateResult {
	var res model.AggregateResult
	for i, dec := range decisions {
		// exit 2 或明确 deny -> 立即阻断（仅可阻断事件）
		if ev.Name.Blockable() && (dec.ExitCode == 2 || dec.PermissionDecision == "deny" || !dec.Continue) {
			res.Blocked = true
			if res.BlockReason == "" {
				res.BlockReason = firstNonEmpty(dec.Reason, dec.SystemMessage, "blocked by hook")
			}
		}
		if dec.PermissionDecision == "ask" {
			res.NeedApproval = true
		}
		if len(dec.UpdatedInput) > 0 { // 浅合并，后者覆盖
			if res.UpdatedInput == nil {
				res.UpdatedInput = map[string]any{}
			}
			for k, v := range dec.UpdatedInput {
				res.UpdatedInput[k] = v
			}
		}
		if dec.AdditionalContext != "" {
			res.AdditionalContext = append(res.AdditionalContext, dec.AdditionalContext)
		}
		if dec.SystemMessage != "" {
			res.SystemMessages = append(res.SystemMessages, dec.SystemMessage)
		}
		if dec.Warning != "" {
			// fail-closed 的可阻断事件：告警升级为阻断
			if handlers[i].FailClosed && ev.Name.Blockable() {
				res.Blocked = true
				if res.BlockReason == "" {
					res.BlockReason = dec.Warning
				}
			}
			res.Warnings = append(res.Warnings, dec.Warning)
		}
	}
	return res
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
```

#### 13.4.4 `internal/capabilities/hook/adapter/command` — 命令 handler（委托 ExecutionRunner）

```go
// adapter/command/runner.go
// 只负责 Hook I/O 协议：组装 stdin、解析 exit/stdout；真正 spawn 委托 execution.ExecutionRunner。
// 依赖决策 A：execution/model.Command 已扩展 Stdin 字段。
package command

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/capabilities/hook/model"
)

type Runner struct {
	exec           execcontract.ExecutionRunner
	defaultTimeout execmodel.SandboxProfile // 占位：sandbox/host 由注入的 runner 决定
}

func New(exec execcontract.ExecutionRunner) *Runner { return &Runner{exec: exec} }

func (r *Runner) Kind() string { return "command" }

func (r *Runner) Run(ctx context.Context, spec model.HandlerSpec, inputJSON []byte) model.Decision {
	cmdline := spec.Command
	if runtime.GOOS == "windows" && strings.TrimSpace(spec.CommandWindows) != "" {
		cmdline = spec.CommandWindows
	}
	res, err := r.exec.Run(ctx, execmodel.Command{
		Command: cmdline,
		Shell:   execmodel.ShellAuto, // shell 选择归 runner
		Env:     spec.Env,
		Stdin:   inputJSON, // 决策 A：payload 走 stdin
	}, execcontract.RunOptions{Timeout: spec.Timeout})
	if err != nil || res == nil {
		return model.Decision{Continue: true, Warning: "hook 执行失败: " + errStr(err)} // fail-open（除非 FailClosed，见 aggregate）
	}
	return parseDecision(res.ExitCode, res.Stdout, res.Stderr)
}

// parseDecision 双通道：exit code + stdout JSON（§5.3/§5.4）。
func parseDecision(exitCode int, stdout, stderr string) model.Decision {
	dec := model.Decision{Continue: true, ExitCode: exitCode}
	switch exitCode {
	case 0: // 解析 stdout JSON；无 JSON 则默认放行
		if obj := extractFirstJSON(stdout); obj != nil {
			applyJSON(&dec, obj)
		}
	case 2: // 快速阻断，stderr 作原因
		dec.Continue = false
		dec.PermissionDecision = "deny"
		dec.Reason = strings.TrimSpace(stderr)
	default: // 其他非 0 -> 告警不阻断
		dec.Warning = strings.TrimSpace(stderr)
	}
	return dec
}

// applyJSON 把 hook 输出 JSON 映射到 Decision（含 permissionDecision 归一化，§5.3）。
func applyJSON(dec *model.Decision, obj map[string]any) {
	// TODO: 读取 continue / systemMessage / hookSpecificOutput.{permissionDecision,
	//       permissionDecisionReason, updatedInput, additionalContext}；归一化 allow|deny|ask
}

func extractFirstJSON(s string) map[string]any { // 容忍 stdout 前后杂文本（对齐 Kode）
	i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if i < 0 || j <= i {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(s[i:j+1]), &m) != nil {
		return nil
	}
	return m
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
```

#### 13.4.5 `internal/capabilities/hook/adapter/builtin` — 内置 Go 守卫

```go
// adapter/builtin/registry.go
package builtin

import (
	"context"

	"genesis-agent/internal/capabilities/hook/model"
)

// Guard 内置守卫函数：入参为已反序列化的事件 payload。
type Guard func(ctx context.Context, payload map[string]any) model.Decision

type Runner struct{ guards map[string]Guard }

func New() *Runner {
	r := &Runner{guards: map[string]Guard{}}
	r.guards["git_branch_guard"] = gitBranchGuard
	// r.guards["secret_path_guard"] = secretPathGuard
	return r
}

func (r *Runner) Kind() string { return "builtin" }

func (r *Runner) Run(ctx context.Context, spec model.HandlerSpec, inputJSON []byte) model.Decision {
	g, ok := r.guards[spec.Name]
	if !ok {
		return model.Decision{Continue: true, Warning: "unknown builtin guard: " + spec.Name}
	}
	// TODO: json.Unmarshal(inputJSON) -> payload
	return g(ctx, nil)
}

// gitBranchGuard 拦截 git 切分支类危险命令（对齐 Kode 内置守卫）。
func gitBranchGuard(ctx context.Context, payload map[string]any) model.Decision {
	// TODO: 从 payload.tool_input.command 判断 "git switch|checkout"，命中则 deny
	return model.Decision{Continue: true}
}
```

#### 13.4.6 接入点片段（Gateway / app.RunOnce）

```go
// internal/capabilities/tool/gateway/gateway.go —— Execute 内新增 Pre/PostToolUse（§7.1）
// ...（现有 record start / trace start / defer finish 之后）
pre, _ := hook.FromContext(ctx).Dispatch(ctx, model.Event{
	Name: model.EventPreToolUse, MatchKey: name,
	Payload: map[string]any{"tool_name": name, "tool_input": json.RawMessage(params)},
})
if pre.Blocked {
	return "", fmt.Errorf("工具 [%s] 被 Hook 阻断: %s", name, pre.BlockReason)
}
if pre.NeedApproval {
	// TODO: 桥接 approval.Service（§7.4）
}
if len(pre.UpdatedInput) > 0 {
	params = mergeParams(params, pre.UpdatedInput) // unmarshal+merge+marshal
}
// TODO: 把 pre.AdditionalContext 经 context 回传 runtime 注入队列（§7.5）

// ...（authz / acquire / t.Execute 之后，defer finish 前）
result, err = t.Execute(ctx, params)
hook.FromContext(ctx).Dispatch(ctx, model.Event{
	Name: model.EventPostToolUse, MatchKey: name,
	Payload: map[string]any{"tool_name": name, "success": err == nil},
})
```

```go
// internal/app/run_service.go —— 注入 Dispatcher + Run 级事件上提（决策 B，§7.2）
func (s *AgentService) RunOnce(ctx context.Context, req domain.StartRunRequest) (*domain.Run, error) {
	ctx = progress.WithSink(ctx, req.OnProgress)
	ctx = hook.WithDispatcher(ctx, s.hookDispatcher) // 与 progress 同处注入

	s.hookDispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventRunStart})
	run, err := s.runEngine.Start(ctx, req)
	s.hookDispatcher.Dispatch(ctx, hookmodel.Event{
		Name:  hookmodel.EventRunComplete,
		Async: true, // fire-and-forget
		Payload: map[string]any{"status": statusOf(run, err)},
	})
	return run, err
}
```

---

（关键设计决策 A–F 已于 §12 确认；实现前唯一外部依赖为决策 A 的 `execution.Command.Stdin` 契约扩展。骨架代码见 §13.4。）

