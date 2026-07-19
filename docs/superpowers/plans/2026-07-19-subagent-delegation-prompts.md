# SubAgent 委派提示词与 delegation_posture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按 `docs/子智能体设计.md` §6.1.2，补齐主模型 Kode 级委派提示（system 短规则 + 加厚 Task Description），并接入可配置 `delegation_posture`（CLI/Desktop=proactive，Enterprise=explicit_request_only）。

**Architecture:** 文案 SSOT 放在 `internal/capabilities/subagent/prompt`（对齐 `tasklist/prompt`）。`runtime/prompt` 在 `Task` 可用时注入 `<delegation>` 稳定块；`Task` 工具 `DescriptionFunc` 渲染 available_agents + posture 相关 Usage notes。产品经 `bootstrap.BuildOptions` 注入姿态与 `max_concurrent`（提示层并行建议，非第二套计数器）。

**Tech Stack:** Go；现有 prompt Builder / Task Catalog；产品 bootstrap。

**非目标（本计划不做）：** Plan 模式 reminder（L2）、swarm、完整 frontmatter 治理字段。

**后续已补齐（另轮实现，不在本计划 checkbox 内）：** 子侧 `ChildBase`/`ComposeChildSystem`、`Delegator`、`Skill(context=fork)`、`@agent` L4 mention、`AudienceSubAgent` 裁剪——见 `docs/子智能体设计.md` §1.4。

---

## 文件映射

| 文件 | 职责 |
| --- | --- |
| `internal/capabilities/subagent/prompt/rules.go` | Posture 常量、SystemRules、RenderToolDescription |
| `internal/capabilities/subagent/prompt/rules_test.go` | 文案与姿态分支单测 |
| `internal/capabilities/subagent/service/catalog.go` | `RenderDescription` 委托 SSOT |
| `internal/capabilities/subagent/tool/task/tool.go` | Deps 增加 Posture / MaxConcurrent |
| `internal/runtime/prompt/{interface,builder}.go` | BuildRequest/Builder 携带 posture；注入 `<delegation>`；Task 可用时调整文件查找规则 |
| `internal/bootstrap/builder.go` | BuildOptions + 创建 Task/Prompt 时注入 |
| `products/{cli,desktop,enterprise}/bootstrap/container.go` | 产品默认姿态 |

## 验收标准

1. `Task` 在 AvailableTools 中时，system 含 `<delegation>` 与姿态对应硬规则；无 `Task` 时不注入。
2. proactive：含「优先 explore / 匹配 description 主动 Task / needle 勿 spawn / 可并行」。
3. explicit_request_only：含「除非用户、AGENTS.md 或 Skill 明确要求，否则不 spawn；『要深入』不算授权」。
4. Task Description 含 `<available_agents>`、When NOT to use、并行建议（含 max_concurrent）。
5. CLI/Desktop 默认 proactive；Enterprise 默认 explicit_request_only。
6. `go test` 覆盖 `subagent/prompt`、`runtime/prompt`、`subagent/tool/task` 相关包通过。

---

### Task 1: SSOT 包与失败测试

**Files:**
- Create: `internal/capabilities/subagent/prompt/rules.go`
- Create: `internal/capabilities/subagent/prompt/rules_test.go`

- [x] **Step 1: 写失败测试**（SystemRules / RenderToolDescription 姿态分支）
- [x] **Step 2: 实现 SSOT 使测试通过**

### Task 2: Catalog / Task 工具接线

**Files:**
- Modify: `service/catalog.go`、`tool/task/tool.go`、相关测试

- [x] **Step 1: 扩展 RenderDescription 选项；Task Deps 传入 posture/maxConcurrent**
- [x] **Step 2: 更新/补充 tool 或 catalog 测试**

### Task 3: System prompt Builder

**Files:**
- Modify: `internal/runtime/prompt/interface.go`、`builder.go`、`environment_test.go`

- [x] **Step 1: 写 BuildSystem 注入 `<delegation>` 的失败测试**
- [x] **Step 2: 实现注入与 Task 场景下文件查找规则消歧**

### Task 4: Bootstrap 产品默认

**Files:**
- Modify: `internal/bootstrap/builder.go`、三端 `container.go`

- [x] **Step 1: BuildOptions 增加 `SubAgentDelegationPosture`**
- [x] **Step 2: CLI/Desktop=proactive，Enterprise=explicit_request_only；传入 Prompt Builder 与 Task Deps**

### Task 5: 验证

- [x] **Step 1: 跑相关 `go test`（带仓库 GOCACHE/GOMODCACHE）**
- [x] **Step 2: 对照验收标准勾选**
