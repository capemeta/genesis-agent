# Skill 三模式执行与依赖闭环 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 打通「Skill 脚本三模式可跑 + 缺依赖结构化回传 + 显式安装后再执行」闭环，使 Agent 能看见 `dependency_missing` 并经审批安装后重跑同一脚本。

**Architecture:** 统一入口仍是 `run_skill_command` → `SkillScriptService`；三模式只换 backend。依赖闭环不采用隐藏 auto-pip：先保证失败 JSON 到达模型（修 ReAct/调度丢 stdout），再落地 `install_skill_dependencies`（build profile + 审批），由 Agent 显式二次调用脚本。设计真源：`docs/Skill三模式执行与依赖闭环设计.md`（尤其 §6、§9、§14）。

**Tech Stack:** Go；既有 `skill/script`、`react`、`execution`、`approval`。

**真源与参考根目录：**

- 设计：`docs/Skill三模式执行与依赖闭环设计.md`（尤其 **§14 编码前必读**）
- Codex：`D:\workspace\go\go-project\codex`
- Kode-CLI：`D:\workspace\go\go-project\Kode-CLI`

### 硬性门禁：必须先读源码再写代码

> 与设计 §14.1 / §14.5 / §14.6 一致。**未完成对应 Phase 的精读勾选，禁止开写该 Phase 业务代码**（含「凭摘要 / 凭记忆 / 只看过设计文档表头」）。

```text
1. 选 Gate（A/B/C）
2. 打开本计划「精读」Task，按绝对路径通读参考源码（至少读到关键函数体）
3. 对照「读什么 → Genesis 落点」写下 1～3 句笔记
4. 勾选 §14.6 / 本计划清单
5. 再写测试 / 改 Genesis 代码
```

**禁止抄：** 静默 auto-pip；Skill 脚本随意 Bash；用 `dangerouslyDisableSandbox` 当装包默认；把 MCP 安装原样当 pip（只借鉴确认→落盘→刷新→去重形态）。详见设计 §14.5。

**Windows 验证命令模板：**

```powershell
$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'; $env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'
go test ./path/to/pkg -count=1
```

---

## 两轮深度反思（已并入下方任务顺序）

### 反思 Round 1 — 勿把独立子系统绑死在一个 PR

| 问题 | 修正 |
| --- | --- |
| Phase A 若同时含「失败回传 + Enterprise 全三模式 + 远程联调」，单 PR 过大、难验收 | **Gate A0** 只交付「模型看得见失败」；三模式接线分 Task，远程/Enterprise 可 stop 后开下一里程碑 |
| 丢 stdout 有两处：`runToolCall` 返回 error，以及 `scheduler` 结果处理用 `Err` 覆盖 `Output` | Task 1 必须改 **两处**（约 `react_loop.go:450-454` 与 `:484-490` / `:564-584`） |
| `run_skill_command` 已是 `(json, err)` 双返回，数据在；根因是 ReAct 丢弃 | Task 1 **先**验证「双返回被保留」，再扩 `failure_kind` |
| genesis-sandbox 镜像预装属外部仓 | Phase C 镜像项标 **External**，本仓只做契约/文档/本地 scope |

### 反思 Round 2 — 因果顺序与 YAGNI

| 问题 | 修正 |
| --- | --- |
| 先做 `failure_kind` 但模型仍看不见 → 白做 | **严格顺序：Task1 回传 → Task2 结构化字段 → Task3 文档/SKILL → 再 install** |
| Phase B 若并行做 `auto_retry_after_install` | **本计划不做自动二次 Run**（默认 false，留 Phase C opt-in） |
| `narrowToolNames` 未纳入 install 工具则装不了 | Task（install）完成后 **立刻** 把 meta 工具并入收窄白名单 |
| 「一个大计划」vs 可独立交付 | **同一文档，三门禁**：完成 Gate A / B / C 可分别合并；Gate A 自测即可停 |
| 计划仅摘要源码路径、易跳过真读 | **每个 Gate 专设精读 Task**（0 / 5 / 10.5），嵌设计 §14 全表绝对路径；未勾选禁止编码 |

---

## 文件结构（将创建 / 修改）

| 文件 | 职责 |
| --- | --- |
| `internal/runtime/strategy/react/react_loop.go` | 工具失败时保留 `Output` JSON |
| `internal/capabilities/skill/script/contract/runner.go` | `RunResult` 增 `failure_kind` / `missing` / `suggested_*` |
| `internal/capabilities/skill/script/service/service.go` | 解析 hint/stderr → 填充失败字段；optional 降级对齐（后续 Task） |
| `internal/capabilities/skill/tool/run_skill_command/tool.go` | 保证 `ok=false` 仍返回完整 JSON + error |
| `internal/capabilities/skill/tool/install_skill_dependencies/` | **新建** 安装工具 |
| `internal/capabilities/skill/model/model.go` + `parser/markdown.go` | `dependencies.runtime` |
| `internal/runtime/strategy/react/react_loop.go`（narrow） | meta 工具含 install |
| `products/cli/bootstrap/container.go` | 注册 install 工具；SharedScriptsFS 已有 |
| `products/enterprise/bootstrap/` + `shared/skillstack/` | 可配置 sandbox/SessionClient（Gate A 可选 / Gate B+） |
| `docs/Skill三模式执行与依赖闭环设计.md` | 已定稿；实现时只改状态勾选 |
| office-ppt `SKILL.md` | 缺依赖 → install → 再跑 |

---

## Gate A — 模型看得见失败（P0，可独立合并）

### Task 0: Phase A 源码精读（强制，零业务改动）

**前置：** 未完成本 Task 全部勾选 → **禁止**进入 Task 1。对照设计 `docs/Skill三模式执行与依赖闭环设计.md` §14.2 / §14.6。

**参考根目录：** Codex=`D:\workspace\go\go-project\codex`；Kode=`D:\workspace\go\go-project\Kode-CLI`。

#### Step 0a — Codex / Kode 必读（按绝对路径打开文件）

| 勾选 | 优先级 | 绝对路径 | 读什么 | Genesis 落点 |
| --- | --- | --- | --- | --- |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\core\src\tools\mod.rs` | `format_exec_output_for_model`：exit + stdout/stderr 如何拼给模型 | `react_loop.go` 失败时保留 ToolResult JSON |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\core\src\tools\events.rs` | 非零 exit → `RespondToModel(content)`（内容仍完整） | 禁止只回 `工具执行失败: …` |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\skills\src\assets\samples\imagegen\scripts\image_gen.py` | `_dependency_hint` / `ImportError` → 安装命令 + exit 1 | 脚本 `hint=dependency_missing`；Service 填 `failure_kind` |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\skills\src\assets\samples\imagegen\scripts\remove_chroma_key.py` | Pillow 缺失同类模式 | Python 脚本侧约定 |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\prompts\templates\permissions\approval_policy\on_request.md` | 依赖失败 → 直接申请 escalation/网络，勿先闲聊 | Skill 硬规则 `install_then_retry` |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\prompts\templates\permissions\approval_policy\on_request_rule_request_permission.md` | `with_additional_permissions` 优先于盲目提权 | 安装走 build profile + 网络 allowlist |
| [ ] | P1 | `D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\system\BashTool\BashTool.tsx` | `renderResultForAssistant`：stdout+stderr 原样拼接 | ToolResult 透传 |
| [ ] | P1 | `D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\system\BashTool\executeForeground.tsx` | 非零 exit 写入 stderr 的方式 | `RunResult.Stderr` / `ExitCode` |
| [ ] | P1 | `D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\system\BashTool\prompt.ts` | 沙箱失败 vs 其他失败分流 | `sandbox_violation` vs `dependency_missing` |
| [ ] | P1 | `D:\workspace\go\go-project\Kode-CLI\kode-agent-sdk\src\tools\scripts.ts` | `execute_script` 失败结构 `{ok,error,data:{stdout,stderr}}` | `script/contract.RunResult` |
| [ ] | P1 | `D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\interaction\SkillTool\SkillTool.tsx` | Skill 只注入、不执行脚本 | `Skill` ≠ `run_skill_command` |
| [ ] | P1 | `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\customCommands\discovery.ts` | `Base directory for this skill` 注入 | 保持 `SKILL_DIR`；勿退回宿主机 embed 路径 |

#### Step 0b — 本仓库落点先读

- [ ] `internal/runtime/strategy/react/react_loop.go`（`runToolCall` / `executeOneToolCall` / scheduler 结果，约 450–490、564–585）
- [ ] `internal/capabilities/skill/script/service/service.go`
- [ ] `internal/capabilities/skill/script/contract/runner.go`
- [ ] `internal/capabilities/skill/tool/run_skill_command/tool.go`（约 109–116）
- [ ] `products/cli/bootstrap/container.go`（sandbox / SessionClient）
- [ ] `docs/执行工作空间与Sandbox文件路径契约.md`

#### Step 0c — 精读产出（写进 PR/笔记，不可空）

- [ ] 断点一句话：*`run_skill_command` 返回 JSON+err，但 scheduler/`executeOneToolCall` 在 Err!=nil 时丢掉 Output*
- [ ] 从 Codex/Kode 各记一条「要借鉴」+ 一条「不要抄」（对齐 §14.5）
- [ ] 勾选设计 §14.6 Phase A 四项

---

### Task 1: 修复工具失败回传（TDD）

> **Blocked until Task 0 完成。** 实现对照：Codex `format_exec_output_for_model` + Kode `renderResultForAssistant`。

**Files:**
- Modify: `internal/runtime/strategy/react/react_loop.go`
- Test: `internal/runtime/strategy/react/`（新建或扩展现有 `*_test.go`）

- [ ] **Step 1: 写失败测试** — 模拟 tool Execute 返回 `("{\"ok\":false,\"failure_kind\":\"dependency_missing\"}", err)`，断言进入消息的 `Content` **包含**该 JSON，而不是仅 `工具执行失败: ...`

```go
// 伪代码意图：content 必须含 ok=false JSON；可同时含失败前缀，但不得丢弃 JSON
if !strings.Contains(got.Content, `"ok":false`) {
    t.Fatalf("content discarded json: %q", got.Content)
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'; $env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'
go test ./internal/runtime/strategy/react/ -count=1 -run FailureContent
```

- [ ] **Step 3: 改两处逻辑**
  1. `scheduler` 结果循环（约 450–454）：`Err != nil` 时若 `Output != ""`，使用 Output（或 `Output + "\n" + 失败摘要`），**禁止**整段替换成只有 error 字符串。
  2. `executeOneToolCall`（约 486–490）：同样保留 `runToolCall` 在 error 路径返回的 content（需让 `runToolCall` 在 toolErr 时 **仍 return result, toolErr**，不要 `return "", toolErr`）。
- [ ] **Step 4: 跑测试通过**；补一条「Output 为空时仍回失败摘要」回归
- [ ] **Step 5: Commit** — `fix(react): preserve tool output JSON on execution errors`

---

### Task 2: RunResult 结构化失败字段

> **对照精读：** Codex `image_gen.py` `_dependency_hint`；Kode `scripts.ts` 失败结构。未重读这两处 → 禁止改契约字段名。

**Files:**
- Modify: `internal/capabilities/skill/script/contract/runner.go`
- Modify: `internal/capabilities/skill/script/service/service.go`
- Test: `internal/capabilities/skill/script/service/service_test.go` 或新建 `failure_kind_test.go`

- [ ] **Step 1: 扩展契约**（JSON 字段名与设计 §6.3 对齐）

```go
FailureKind     string            `json:"failure_kind,omitempty"`
Missing         []MissingDep      `json:"missing,omitempty"`
SuggestedAction string            `json:"suggested_action,omitempty"`
Retryable       bool              `json:"retryable,omitempty"`
// SuggestedInstall 可用 map 或专用 struct；先最小字段
```

- [ ] **Step 2: 写测试** — 伪造脚本 stdout：

```json
{"ok":false,"hint":"dependency_missing","dependency":"pptxgenjs"}
```

断言 `FailureKind=="dependency_missing"`、`Missing` 含 npm/pptxgenjs、`SuggestedAction=="install_then_retry"`（此时 install 工具可尚不存在，`suggested_install.tool` 可先写目标名）。

- [ ] **Step 3: 在 `Service.Run` 成功收集 stdout 后**解析 hint / 常见 stderr（`ModuleNotFoundError`、`Cannot find module`）；填充字段；保持既有 `OK=false` + gate 逻辑
- [ ] **Step 4: 确认 `run_skill_command` tool 仍 `return string(data), fmt.Errorf(msg)`**，使 Task1 能把完整 data 送出
- [ ] **Step 5: 跑** `go test ./internal/capabilities/skill/script/... ./internal/capabilities/skill/tool/... -count=1`
- [ ] **Step 6: Commit** — `feat(skill-script): classify dependency_missing failures`

---

### Task 3: office-ppt SKILL 硬规则 + 系统提示锚点

> **对照精读：** Codex `on_request.md`（勿先闲聊）；Kode `prompt.ts`（sandbox vs 其它失败分流文案）。

**Files:**
- Modify: `internal/capabilities/skill/adapter/embedded/skills/office-ppt/SKILL.md`
- Modify: CLI skill prompt injector 或短硬规则（若有集中 skills_instructions；`products/cli/bootstrap/container.go` / `shared/skillstack`）

- [ ] **Step 1:** SKILL 增加：若返回 `failure_kind=dependency_missing` → 安装通道（写明即将落地的工具名）→ **相同参数**再 `run_skill_command`；区分 `sandbox_violation`
- [ ] **Step 2:** 硬规则一句写入 skills_instructions（对齐 Codex on_request「不要先闲聊」）
- [ ] **Step 3:** Commit — `docs(office-ppt): install_then_retry guidance`

---

### Task 4: Gate A 验证清单

- [x] **Step 1:** 单测：react 丢 JSON 用例 + failure_kind 解析用例全绿（2026-07-10）
- [ ] **Step 2:** 手工或集成：缺 pptxgenjs 时跑 `create_pptx.js`，确认 Tool 消息里能看到 JSON（即使 install 工具未上）
- [x] **Step 3:** **Gate A Done（代码）** — 可合并；**暂停点**：未要求 Enterprise 远程齐套；Gate B/C 另开会话

---

## Gate B — 安装通道（P1）

### Task 5: Phase B 源码精读（强制，零业务改动）

**前置：** 未完成本 Task → **禁止**进入 Task 6–9。对照设计 §14.3 / §14.6 Phase B。

#### Step 5a — Codex / Kode 必读

| 勾选 | 优先级 | 绝对路径 | 读什么 | Genesis 落点 |
| --- | --- | --- | --- | --- |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\core\src\mcp_skill_dependencies.rs` | 缺失收集 → 用户确认 → 持久化 → refresh；会话去重 | `install_skill_dependencies` 审批/去重/审计形态（装的是包不是 MCP） |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\core\src\session\turn.rs` | turn 入口何时调用依赖安装 | 不在业务脚本里装；装完再跑脚本 |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\core-skills\src\loader.rs` | `resolve_dependencies`：tools 元数据（mcp/cli） | `dependencies.tools` + 扩展 `runtime` |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\protocol\src\protocol.rs` | `SkillDependencies` / `SkillToolDependency` | `internal/capabilities/skill/model` |
| [ ] | P0 | `D:\workspace\go\go-project\codex\codex-rs\features\src\lib.rs` | `SkillMcpDependencyInstall`；已移除 EnvVar prompt | 对话期装包独立 config，默认谨慎 |
| [ ] | P1 | `D:\workspace\go\go-project\codex\codex-rs\core\src\tools\handlers\request_permissions.rs` | 显式申请 network | 安装通道网络审批 |
| [ ] | P1 | `D:\workspace\go\go-project\codex\codex-rs\core\src\tools\network_approval.rs` | 出站网络审批 | build profile 网络 allowlist |
| [ ] | P1 | `D:\workspace\go\go-project\codex\codex-rs\skills\src\assets\samples\imagegen\SKILL.md` | 文档如何写依赖安装步骤 | office-ppt `SKILL.md` |
| [ ] | P1 | `D:\workspace\go\go-project\codex\codex-rs\skills\src\assets\samples\imagegen\references\codex-network.md` | 网络与审批配置说明 | CLI/沙箱文档 |
| [ ] | P1 | `D:\workspace\go\go-project\Kode-CLI\packages\core\src\permissions\policies\bash.ts` | `SAFE_COMMANDS`；`npm install` 不在白名单 → ask | install 默认 ask + 命令模板白名单 |
| [ ] | P1 | `D:\workspace\go\go-project\Kode-CLI\packages\core\src\permissions\bash\engine.ts` | Bash 权限决策 | approval / policy 接线 |
| [ ] | P1 | `D:\workspace\go\go-project\Kode-CLI\packages\core\src\utils\ripgrep.ts` | `ensureRipgrepReady` Fix 文案（**非** Skill 装包） | preflight / `suggested_install` 可读 Fix |
| [ ] | P2 | `D:\workspace\go\go-project\Kode-CLI\kode-agent-sdk\src\core\agent.ts` | 工具错误 recommendations | 可借鉴结构，勿空泛照搬 |
| [ ] | P2 | `D:\workspace\go\go-project\Kode-CLI\packages\core\src\agent\builtin.ts` | Explore 等只读 agent **禁止** npm install | 只读/Plan 禁用 install |

#### Step 5b — 本仓库落点先读

- [ ] `internal/capabilities/skill/tool/skill/tool.go`（`checkDependencies`）
- [ ] `internal/capabilities/skill/model/model.go`（`Dependencies`）
- [ ] `internal/capabilities/skill/parser/markdown.go`
- [ ] `internal/capabilities/execution/tool/run_command/`（审批与 Dangerous）
- [ ] `internal/capabilities/execution/model/model.go`（`SandboxOperationBuildDependencies`、`skill-build-polyglot`）
- [ ] `docs/沙箱API对接与Profile选择规则.md` §5 / §7
- [ ] `docs/统一配置权限与审批治理设计.md`（若改审批元数据）

#### Step 5c — 精读产出

- [ ] 明确：借鉴 Codex 的「确认→落盘→刷新→去重」，**不**把 MCP 安装当 pip
- [ ] 明确：Kode `npm install` 不在 SAFE → Genesis install **默认 ask**
- [ ] 勾选设计 §14.6 Phase B 四项

---

### Task 6: `dependencies.runtime` 模型与解析

> **Blocked until Task 5。** 对照：Codex `loader.rs` + `protocol.rs` 依赖字段形态。

**Files:**
- Modify: `internal/capabilities/skill/model/model.go`
- Modify: `internal/capabilities/skill/parser/markdown.go`
- Test: `parser/markdown_test.go`

- [ ] **Step 1: 写测试** — frontmatter 含 `runtime.node: [{name: pptxgenjs, require: pptxgenjs}]` 能解析
- [ ] **Step 2: 实现结构**（向后兼容：无 runtime 不报错）
- [ ] **Step 3: 跑 parser 测试；Commit** — `feat(skill): parse dependencies.runtime`

---

### Task 7: 实现 `install_skill_dependencies`（TDD，先本地 workspace）

> **对照精读（实现前再扫一眼）：** `mcp_skill_dependencies.rs`；Kode `bash.ts` / `engine.ts`；Codex `network_approval.rs`。§14.5：**禁止**静默 pip；**禁止**默认 disable sandbox。

**Files:**
- Create: `internal/capabilities/skill/tool/install_skill_dependencies/tool.go`
- Create: `internal/capabilities/skill/tool/install_skill_dependencies/tool_test.go`
- Create:（可选）`internal/capabilities/skill/script/deps/` 或 `service` 内 installer 助手 — **保持 capabilities/skill 边界，禁止 tool 直接 os/exec 而无审批**
- Modify: CLI `bootstrap/container.go` + profile `default_profile.go` 启用工具名
- Modify: `shared/skillstack` 若 Enterprise 共用注册

- [ ] **Step 1: 写失败测试** — 未审批 / 包不在 runtime 白名单 → deny
- [ ] **Step 2: 写成功测试** — fake runner 捕获命令为白名单模板（如 `npm install pptxgenjs`），profile/metadata 含 `skill_dep_install`、`build_dependencies`
- [ ] **Step 3: 实现工具** — Approve → 选 scope=`workspace` → ExecutionRunner + **网络 allowlist / build profile**（若 build profile 尚未被 Runner 消费，本 Task **最小可用**：本地 `SandboxDisabled` + 审批 + 命令白名单，并在 metadata 标记 TODO build-profile，**不得**静默 disable 安全策略却宣称已完成 Phase C）
- [ ] **Step 4:** 包白名单 = Skill `dependencies.runtime` ∪ 显式传入且通过更高风险策略（第一期可 **仅允许声明列表**）
- [ ] **Step 5: 接线 Profile Enabled + skillstack/CLI**
- [ ] **Step 6: Commit** — `feat(skill): add install_skill_dependencies tool`

---

### Task 8: Meta 工具收窄白名单

**Files:**
- Modify: `internal/runtime/strategy/react/react_loop.go`（`narrowToolNames`）
- Test: 既有 skill narrow 测试或新建

- [ ] **Step 1: 测试** — 加载 office-ppt 后可见 `install_skill_dependencies` 与 `run_skill_command`
- [ ] **Step 2: 实现并跑测；Commit** — `fix(react): keep install_skill_dependencies under skill narrow`

---

### Task 9: suggested_install 指向真工具 + office SKILL 验收路径

- [ ] **Step 1:** Task2 的 `suggested_install.tool` 与真实工具名一致
- [ ] **Step 2:** 更新 `create_pptx.js` / SKILL 示例调用
- [ ] **Step 3:** 单测或脚本级：mock 缺依赖 → install fake ok → 第二次 run（可用假 Runner 编造两段调用，**不必**真装 npm）
- [ ] **Step 4: Commit**

---

### Task 10: Gate B 验证

- [x] 白名单外包装被拒
- [x] 审批拒绝无法装
- [x] 收窄后仍能调 install
- [x] **不做**同回合自动二次 Run
- [x] **Gate B Done**（2026-07-10；session/user/image 完整落点留 Gate C）

---

## Gate C — 三模式齐 + 预装契约（P2，可分子 PR）

### Task 10.5: Phase C 源码精读（强制，零业务改动）

**前置：** 未完成本 Task → **禁止**进入 Task 11–14 中涉及 materialize / 镜像 / 启动预装的改动。对照设计 §14.4 / §14.6 Phase C。

| 勾选 | 优先级 | 绝对路径 | 读什么 | Genesis 落点 |
| --- | --- | --- | --- | --- |
| [x] | P0 | `D:\workspace\go\go-project\codex\codex-rs\skills\src\lib.rs` | `install_system_skills` + fingerprint 跳过重复写入 | embed materialize / 系统 Skill 落盘 |
| [x] | P1 | `D:\workspace\go\go-project\codex\codex-rs\core-skills\src\service.rs` | 启动时调用 system skills 安装 | CLI/Enterprise bootstrap |
| [x] | P1 | `D:\workspace\go\go-project\codex\.devcontainer\Dockerfile.secure` | 镜像预装的是**工具链**不是 per-skill pip | `office-basic` 镜像职责边界 |
| [x] | P2 | `D:\workspace\go\go-project\codex\sdk\python\_runtime_setup.py` | SDK 自举 pip（与 Skill 依赖无关） | **不要**把 Skill 依赖装成 SDK 自举 |
| [x] | P2 | `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\skillMarketplace\plugins\install.ts` | Plugin/Skill **文件**安装 | marketplace ≠ `install_skill_dependencies` |

**本仓库先读：**

- [x] `internal/capabilities/skill/script/materialize/`
- [x] `internal/capabilities/skill/adapter/embedded/`
- [x] `shared/skillstack/`
- [x] `docs/沙箱API对接与Profile选择规则.md`（远端 profile；镜像清单属 sandbox 仓）

**精读产出：**

- [x] 已区分「Skill **文件**预装」与「第三方 **包**预装」
- [x] 勾选设计 §14.6 Phase C 两项

---

### Task 11: CLI Skill 远程 / optional 降级对齐

> **Blocked until Task 10.5（至少扫过 materialize + Dockerfile.secure 职责边界）。**

**Files:**
- Modify: `internal/capabilities/skill/script/service/service.go`（`useRemoteSession` / 失败路径）
- Test: mock SessionClient；断言嵌套 StageInput name `skills/<pkg>/scripts/...`
- Read first: 设计 §4、§14.2 本仓路径契约；`docs/沙箱API对接与Profile选择规则.md`

- [x] **Step 1: 写 mock 测试** — StageInput 路径含 `office/unpack.py` 相对段
- [x] **Step 2:** optional 不可用时与 `run_command` 一致降级或明确错误（按设计 I6；**不得** required 静默降级）
- [ ] **Step 3: Commit** — `test(skill-script): remote nested stageinput + optional policy`（仅用户要求时提交）

---

### Task 12: Enterprise 可配置执行栈（非写死 disabled）

**Files:**
- Modify: `products/enterprise/bootstrap/container.go`
- Modify: `shared/skillstack/stack.go`（ExecStack 已有字段则接线配置）
- Test: `container_test.go` 临时配置仍 Init 成功

- [x] **Step 1:** 配置项驱动 `SandboxProfile` / 可选 SessionClient（无配置则保持本地 disabled）
- [x] **Step 2:** 文档一句：生产 headless ask 审批为过渡
- [ ] **Step 3: Commit**（仅用户要求时提交）

---

### Task 13: Preflight（可选）+ opt-in auto_retry（默认关）

- [x] **Step 1:** 对 `runtime` 声明做 LookPath / `node -e require`；失败返回同一 `failure_kind`（npm/pip 硬失败；system 仅 warning）
- [x] **Step 2:** `skills.auto_retry_after_install` 默认 false；true 时最多 1 次，metadata `auto_retried=true`
- [ ] **Step 3: Commit**（仅用户要求时提交）

---

### Task 14: External — office-basic 镜像包列表

> **对照：** Codex `Dockerfile.secure`（工具链边界）；**勿**抄 `_runtime_setup.py`。

**Files / Repo:** genesis-sandbox（本仓仅文档）

- [x] **Step 1:** 在 `docs/沙箱API对接与Profile选择规则.md` 或闭环设计 §10 列出 **应对齐的包**（pptxgenjs、Pillow、soffice、python）
- [x] **Step 2:** 本仓 DoD 勾选「文档已列」；镜像构建在 sandbox 仓另开任务
- [x] **Step 3:** 更新闭环设计实现状态表（Office 专文可后续同步）

---

### Task 15: Gate C / 全计划收尾

- [x] 跑：`go test ./internal/runtime/strategy/react/ ./internal/capabilities/skill/... ./shared/skillstack/ ./products/cli/bootstrap/ ./products/enterprise/bootstrap/ -count=1`
- [x] 对照设计 §10 DoD 勾选（本仓可测项；远程联调/镜像构建属残余）
- [ ] 若开 PR：描述写明「借鉴 Codex 回传与 Kode 透传；安装为显式工具；默认无自动重跑」

---

## 执行注意事项

1. **每个 Gate 开工前**完成对应「源码精读」Task（Task 0 / 5 / 10.5）；绝对路径以设计 §14.2–14.4 为准，本计划已嵌同表。**禁止**只读设计摘要不开源码文件。
2. **每个实现 Task** 表头「对照精读」未读 → 不得提交该 Task 代码。
3. **单文件不超 1000 行**；install 逻辑过大则拆 `script/deps` 或 `service` 子文件。
4. **禁止**在 Skill 脚本内 `subprocess` 装包（§14.5）。
5. Commit 仅在用户要求时创建；本计划中的 Commit step 表示「逻辑提交点」。
6. 停损：Gate A 完成后可停，再开 Gate B。

---

## 附录：任务 ↔ 设计章节 / 参考源码

| Task | 设计 | 必须先读的参考（摘要） |
| --- | --- | --- |
| 0 | §14.2、§14.6 A | Codex tools/mod+events；imagegen；Kode Bash+scripts |
| 1–3 | §6.2–6.4、§6.6、I4 | 同 Task 0；落点 react/script |
| 5 | §14.3、§14.6 B | mcp_skill_dependencies；permissions；Kode bash SAFE |
| 6–10 | §5、§6.5、§7 | Task 5 清单 |
| 10.5 | §14.4、§14.6 C | install_system_skills；Dockerfile.secure |
| 11–14 | §4、§5.3、§6.7–6.9、I6 | Task 10.5 + 本仓 materialize |

