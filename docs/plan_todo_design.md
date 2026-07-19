# 规划模式与任务清单能力域设计方案

本方案定义 `genesis-agent` 中与「先对齐方案再动手」以及「执行中动态跟踪步骤」相关的产品能力、运行时闸门、工具链与提示词规范。目标是防止长链任务记忆漂移，并与 Kode-CLI / Codex 的双轨设计对齐：**规划模式 ≠ 任务清单**。

> 历史名称「Plan / TodoList」易把两套能力混为一谈。本文统一使用产品名 **规划模式** 与 **任务清单**；代码能力域为 `internal/capabilities/tasklist`，工具名保持 `todo_*`。

---

## 0. 产品双模式：规划模式与任务清单

### 0.1 产品命名（定稿）

| 产品名（中文） | 内部名 | 用户一句话理解 | UI 短文案 | 对齐参考 |
| :--- | :--- | :--- | :--- | :--- |
| **规划模式** | `plan_mode` | 先想清楚再动手：调研 → 写方案 → 你批准 → 才执行 | 模式：`规划模式`；面板：`实施方案` | Kode Plan Mode / Codex Plan Mode |
| **任务清单** | `task_list` | 干活时的动态进度条：拆步骤、标完成、防跑偏 | 模式：`执行中`（默认）；面板：`任务清单` | Kode Task\* / Codex Default 的 `update_plan` |

**术语纪律**

- 「计划 / 方案」只指 **规划模式** 产出的实施方案（decision-complete 的设计文档）。
- 「任务 / 步骤 / 清单」只指 **任务清单** 中的执行进度条目。
- 禁止在提示词或 UI 中用「Plan」同时指代两套能力。

### 0.2 边界总览

```text
┌─────────────────── 规划模式 (plan_mode) ───────────────────┐
│ 目标：产出「决策完备」的实施方案，供人批准                       │
│ 主模型：编排调研 / 提问 / 写方案                                 │
│ 子智能体：可被派去 Explore/Plan，但不能 Enter/Exit 规划模式       │
│ 产出：实施方案（专用文件或结构化提案块）                         │
│ 禁止：用任务清单当进度条；禁止写业务代码/改仓库（方案文件除外）     │
└────────────────────────────────────────────────────────────┘
                              │ 用户批准后退出
                              ▼
┌─────────────────── 任务清单 (task_list) ───────────────────┐
│ 目标：执行阶段动态跟踪步骤，防记忆漂移                           │
│ 主模型：todo_write（结构）+ todo_update_step（进度）             │
│ 提示词：稳定 system「任务清单纪律」+ 低频 user 进度提醒            │
│ 琐事：单步/纯问答 → 不建清单                                     │
└────────────────────────────────────────────────────────────┘
```

### 0.3 退出规划模式后的任务清单策略（定稿）

采用 **策略 A（推荐）**：用户批准并退出规划模式后，系统向主模型注入固定交接提示：

> 方案已批准。请先用 `todo_write` 将已批准的实施方案拆解为可执行的任务清单，再开始产生副作用的工具调用。

不在 Runtime 侧自动从方案生成清单（避免静默错拆）；也不允许退出后完全依赖模型自觉（缺少交接 nudge 时易直接开写）。

### 0.4 与代码落点的映射

| 产品能力 | 主要落点 | 当前状态 |
| :--- | :--- | :---: |
| 任务清单数据/工具/广播 | `internal/capabilities/tasklist/*`、`todo_read` / `todo_write` / `todo_update_step` | 后端可用；`task_management` 已挂载（SSOT） |
| 规划模式闸门与提示词 | `internal/runtime/collab`、`planmode/prompt`、`enter/exit/write_implementation_plan` | Runtime MVP 已落地 |
| 提示词分层 | L0 `task_management` / `plan_mode_rules` 互斥；handoff / sparse reminder | 双模式侧已对齐 |

### 0.5 规划模式进入 / 退出 / 产物（产品规则定稿）

| 规则 | 定稿 |
| :--- | :--- |
| **谁可进入** | 仅主会话。入口优先级：① 用户显式切换（CLI/UI「规划模式」）；② 主模型调用 `enter_plan_mode`（若已暴露）；③ 产品策略强制（如 `plan_mode_required`，对齐 Kode 环境开关）。子智能体禁止进入/退出。 |
| **谁可退出** | 仅主会话经用户批准后退出（`exit_plan_mode` 或等价 UI 确认）。用户拒绝则保持规划模式，允许继续改实施方案。 |
| **Phase 1 产物默认** | **方案文件**（会话级相对路径，如 `.genesis/plans/<session_or_slug>.md`）。结构化提案块（`<proposed_plan>`）列为 Phase 1.5 可选增强，不作为默认双轨并行，避免两套渲染与批准路径。 |
| **与任务清单互斥** | 规划模式期间任务清单工具对模型不可见（首选）或调用即失败；退出交接见 §0.3。 |
| **与 `blocked_by_approval` 的关系** | 任务清单步骤的审批阻断属于**执行期 HiTL**；规划模式批准属于**方案期 HiTL**。二者状态机分离，禁止复用同一字段表达两种语义。 |

### 0.6 命名演进（已落地）

- 产品层：「规划模式 / 任务清单 / 实施方案」。
- 代码层已完成：`capabilities/tasklist`、`model.TaskList`、`domain.TaskList`、`task_list_snapshot` / `KindTaskList`。
- 工具名保持 `todo_*`（模型侧稳定）；UI 文案用「任务清单」。
- 注意：沙箱构造结果类型 `sandbox.Plan` **不是**任务清单，禁止再混名重构。

---



## 1. 目标与定位

在 AI 编程与长任务执行中，Agent 需要两套互补能力：

**规划模式（先对齐）**
- 只读调研、澄清意图、产出可交给他人/后续回合直接执行的实施方案。
- 主会话专属进入/退出；子智能体只做调研或草案协助，不能切换模式。

**任务清单（执行跟踪）**
- **AI 侧**：`todo_write` 全量改结构，`todo_update_step` 差量滚进度，降低出站 Token。
- **Runtime 侧**：按频率把未完成项以 user 前缀提醒注入（勿每轮刷 system）。
- **用户侧 (UI)**：CLI TUI / Desktop / Enterprise 渲染任务清单卡片，并支持审批阻断态。

定位：
- 任务清单是 **Agent Runtime 核心能力域之一**，目录落点为 `internal/capabilities/tasklist/`。
- 规划模式是 **Runtime 协作模式**（限工具 + 专用提示词 + 方案产物），不是任务清单的别名。
- 遵循契约（Port）与适配器分层；三端在存储与广播上统分平衡。

---


## 2. 深度借鉴：Kode-CLI 与 Codex 源码分析

通过对 `Kode-CLI` 和 `Codex` 源码的研究，可提炼出以下核心最佳实践与对应源码位置：

### 2.1 任务聚焦与防错机制 (借鉴自 Kode-CLI)

- **唯一进行中约束 (Single In-Progress Constraint)**
  * **源码位置**：[todo_write/index.ts](file:///D:/workspace/go/go-project/Kode-CLI/kode-agent-sdk/src/tools/todo_write/index.ts)
  * **实现细节**：在 `TodoWrite` 工具执行中，对传入的待办步骤过滤 `status === 'in_progress'` 的数量，若大于 1 则直接抛出校验错误。
  * **借鉴点**：AI 执行任务应专注于当前单点。这能强迫 Agent 完成当前目标后再去接手下一个，而不是将多个任务全部变为“进行中”导致状态混乱。

- **被动提醒机制 (Step-based Passive Reminder)**
  * **源码位置**：[todo-manager.ts](file:///D:/workspace/go/go-project/Kode-CLI/kode-agent-sdk/src/core/agent/todo-manager.ts)
  * **实现细节**：在 `TodoManager` 内部声明 `stepsSinceReminder`。在 `onStep()` 时增加计数，达到 `remindIntervalSteps` 阈值则调用 `remind` 回调，向 AI 被动发送未完成项提醒；在 `handleStartup()` 时，若未完成 Todo 为 0，发送空列表引导。
  * **借鉴点**：这解决了长上下文下 AI 容易忽略系统初始 Prompt 的问题，形成动态纠偏。

- **数据模型与自动归档限制**
  * **源码位置**：[packages/core/src/todo/types.ts](file:///D:/workspace/go/go-project/Kode-CLI/packages/core/src/todo/types.ts) 和 [storage.ts](file:///D:/workspace/go/go-project/Kode-CLI/packages/core/src/todo/storage.ts)
  * **实现细节**：定义了包含 `maxTodos` 与 `autoArchiveCompleted` 选项的 `TodoStorageConfig`；在持久化 `setTodos` 时过滤已完成项以防止数据膨胀。
  * **借鉴点**：这是我们在 `todo_read` 工具中进行 Token 自动归档优化的灵感来源。

### 2.2 优雅的表现力 (借鉴自 Codex)

- **多阶段 Cell 与 TUI 树状美化渲染**
  * **源码位置**：[plans.rs](file:///D:/workspace/go/go-project/codex/codex-rs/tui/src/history_cell/plans.rs)
  * **实现细节**：在 `PlanUpdateCell::display_lines` 方法中，解析 `UpdatePlanArgs`，使用 `"  └ "`、`"    "` 缩进与 `✔` (Completed)、`□` (InProgress/Pending) 符号拼接渲染树状分支。同时对已完成、进行中、待办等设置高对比度的 ANSI 样式（如进行中使用 `cyan().bold()`，已完成使用 `dim().crossed_out()` 划掉线效果）。
  * **借鉴点**：我们的 CLI 产品和 Web/Desktop 交互必须避免纯文本堆砌，将计划状态序列化为结构化数据，提供高品质的可视化感知。

- **结构化计划模型契约**
  * **源码位置**：[plan_tool.rs](file:///D:/workspace/go/go-project/codex/codex-rs/protocol/src/plan_tool.rs)
  * **实现细节**：定义了 Rust 中的 `StepStatus`（`Pending`、`InProgress`、`Completed`）与 `UpdatePlanArgs` 接口，以便通过 JSON 序列化保证协议在前后台传输的一致性。
  * **借鉴点**：这是我们设计通用领域模型的基础契约参考。

### 2.3 规划与任务管理工具全貌比对与 Genesis 设计折中

除 `todo_read` / `todo_write` 基础工具外，对参考项目的底层全量检索表明它们还包含以下更丰富的规划管控命令：

#### A. 规划模式 (Plan Mode) — 与任务清单严格分离

* **Kode**：`EnterPlanMode` / `ExitPlanMode`；主模型拿长 reminder（Explore → Plan 子智能体 → 写 plan 文件 → Exit）；子智能体禁止 Enter/Exit；方案写在专用文件。
* **Codex**：Collaboration Mode=`Plan` 注入 `plan.md` developer 指令；产出 `<proposed_plan>`；**明确禁止**在 Plan Mode 使用 `update_plan`（checklist 工具）。
* **Genesis 定稿**：
  * 产品名：**规划模式**（§0）。
  * 闸门放在 **Runtime 协作模式**（限制可写/可执行工具可见性与执行），Capabilities 层可不先暴露同名工具；后续可加 `enter_plan_mode` / `exit_plan_mode` 作为显式入口。
  * 规划模式期间 **禁用任务清单工具**（`todo_read` / `todo_write` / `todo_update_step`），避免模型把「写方案」和「滚进度」混用。
  * 提示词见 §11.5；进入/退出/产物见 §0.5；退出后交接见 §0.3。

#### B. 任务清单 (Task List) — 执行期动态进度

* **Kode**：主推 `TaskCreate` / `TaskUpdate` / `TaskList` / `TaskGet`（差量 CRUD）；legacy `TodoWrite` 默认关闭。System 内 `# Task Management` 仅在工具可用时拼接。
* **Codex Default**：单一全量工具 `update_plan` + base instructions 中稳定的 `## Planning`（何时用/不用、勿复述 UI、恰好一个 `in_progress`）。
* **Genesis 定稿**：
  * 产品名：**任务清单**（§0）。
  * Phase 1 不引入 Kode 第二套 Task\* API，继续用 `todo_read` + `todo_write`（结构全量）+ `todo_update_step`（进度差量），避免双清单混淆。
  * `ParentID`：服务端与存储已支持树/环检测；**Phase 1 不对模型暴露 `parent_id` 入参**（保持扁平清单，降低工具复杂度）。树形展示若需要，由服务端/UI 派生，或 Phase 2 再开放。
  * 稳定提示词块 `task_management` 已挂进主模型 system（`<task_management>`）；细则见 §11.4。

#### C. 目标与 SaaS 成本防御 (借鉴自 Codex，Phase 2+)
* Codex `goal` + `token_budget` 是**独立目标域**，不是 checklist 的别名。
* **Genesis 定稿**：不把 `Version`/`LatestExplanation` 假装成预算系统。任务清单的 `Version` 仅用于乐观并发与审计。Token/时长预算若做，应落在 `usage` / Goal 独立契约，由 Runtime 在超限时熔断，而不是塞进 `UpdatePlan` 校验的边角逻辑。


#### D. 提示词给主模型还是子智能体（对照结论）

| 能力 | 主模型 | 子智能体 |
| :--- | :--- | :--- |
| 任务清单纪律 + `todo_*` | **主要消费者**；稳定 system 块必给 | 可不强制维护会话级清单，避免多 Agent 抢写同一 Session |
| 规划模式完整工作流 | **唯一编排者**（进入/退出/提交批准） | 仅短 reminder；禁止 Enter/Exit；可被派去调研/写草案 |

详见 §11.3。

---


## 3. 架构设计与职责边界

为了在三线产品中取得“内核统一、接入独立”的平衡，任务清单走 Capabilities；规划模式走 Runtime 协作层：

```text
+------------------------------------------------------------------+
|              products: cli / desktop / enterprise                |
|   UI: 规划模式切换 · 实施方案面板 · 任务清单卡片 · 批准流         |
+------------------------------------------------------------------+
                | bootstrap 注入
                v
+------------------------------------------------------------------+
| internal/runtime                                                 |
|  - 协作模式 plan_mode | default(执行中)                          |
|  - 工具可见性闸门 · plan_mode_rules / task_management 提示词     |
|  - 退出交接 plan_mode_handoff · 方案文件路径约定                 |
+------------------------------------------------------------------+
        | 执行期暴露 todo_*              | 规划期隐藏 todo_*
        v                                v
+---------------------------+   +------------------------------+
| tool: todo_read/write/    |   | 只读工具 + 方案文件 Write/Edit|
|       update_step         |   | (+ 可选 enter/exit_plan_mode)|
+---------------------------+   +------------------------------+
                |
                v
+---------------------------+     +----------------------------+
| capabilities/tasklist/svc | --> | contract: repo/broadcaster |
| ID 对齐·合并·提醒文案      |     +----------------------------+
+---------------------------+           |
        +-------------------------------+
        v                 v             v
  shared/local/tasklist  (desktop)  enterprise adapters (P2)
```

### 职责边界说明
- `internal/capabilities/tasklist/model/` & `contract/`：
  - 定义**任务清单**快照（`TaskList`/`Step`）、存储 `Repository` 与服务 `Service` 契约。
  - **绝对不能**直接依赖 Wails、Docker SDK、PostgreSQL、或 HTTP 框架。
  - **不承载**规划模式闸门；规划模式状态与方案产物由 Runtime / 产品层拥有。
- `internal/capabilities/tasklist/service/` + `prompt/`：
  - 业务逻辑与提示词 SSOT（§5.2）。
- `internal/capabilities/tool/`：
  - 注册 `todo_*`；Description 引用 `tasklist/prompt`；仅依赖 `tasklist.Service`。
  - 规划模式下这些工具对模型不可见（首选）或调用失败；**非主会话**写会话级任务清单应拒绝或忽略。
- `internal/runtime/`：
  - 规划模式协作状态、工具可见性、`plan_mode_rules` / `task_management` 挂载、退出交接、方案文件根路径。
- `shared/local/tasklist` / Enterprise adapters：
  - 存储与广播；Enterprise Phase 2。


---

## 4. 目录与相关文件列表

| 文件/目录路径 | 状态 | 职责 |
| :--- | :--- | :--- |
| `internal/capabilities/tasklist/model/tasklist.go` | 已有 | `TaskList` / `Step` |
| `internal/capabilities/tasklist/contract/*` | 已有 | Port |
| `internal/capabilities/tasklist/service/service.go` | 已有 | 对齐、合并、提醒 |
| `internal/capabilities/tasklist/prompt/rules.go` | 已有 | 提示词 SSOT |
| `shared/local/tasklist/file_repo.go` | 已有 | CLI/Desktop 文件存储 |
| `internal/capabilities/tool/adapter/builtin/todo_*.go` | 已有 | 工具；Description ← SSOT |
| `internal/runtime/prompt/builder.go` | 已有 | 注入 `SystemRules` |
| `products/cli/internal/tui/tasklist_renderer.go` | 已有 | 任务清单 TUI |
| `internal/runtime/collab` | 已有 | Mode/Store/ToolGate/Authorizer |
| `internal/capabilities/planmode/prompt` | 已有 | 规划提示词 SSOT |
| `shared/local/collab` | 已有 | CLI ModeStore + 方案文件读写 |
| CLI `/plan` `/execute` | 已有 | TUI 模式切换与批准退出 |
| Enterprise tasklist adapters | Phase 2 | 多租户 DB / SSE / Web 面板 |

---

## 5. 现行契约（以代码为准）

> 历史大段 Go/SQL 草稿已删除，避免与 §0/§11 定稿叠床架屋。实现细节以仓库源码为唯一契约。

### 5.1 包与类型命名（已落地）

| 产品概念 | 代码落点 |
| :--- | :--- |
| 任务清单能力域 | `internal/capabilities/tasklist/` |
| 领域快照 | `tasklist/model.TaskList` + `Step` |
| 会话/UI 投影 | `domain.TaskList` / `domain.TaskListItem` |
| 消息 Kind | `task_list_snapshot`（`MessageKindTaskListSnapshot`） |
| Progress Kind | `task_list`（`progress.KindTaskList`） |
| 本地存储 | `shared/local/tasklist` |
| 工具名（保持） | `todo_read` / `todo_write` / `todo_update_step` |

服务契约要点：`GetTaskList` / `UpdateTaskList` / `UpdateStepStatus` / `GeneratePromptReminder`，见 `tasklist/contract/service.go`。

### 5.2 提示词单一事实来源（SSOT）

| 产物 | 源文件 | 消费者 |
| :--- | :--- | :--- |
| `task_management` 稳定块 | `internal/capabilities/tasklist/prompt/rules.go` → `SystemRules` | `runtime/prompt/builder.go`（`todo_write`+`todo_update_step` 可用时） |
| 工具 Description | 同文件 `ToolTodoWriteDescription` 等 | `builtin/todo_*.go` |

**禁止**在工具与 builder 中另写一份互不相同的纪律文案；改规则只改 `tasklist/prompt`。

### 5.3 业务逻辑要点（索引）

实现见 `tasklist/service/service.go`（勿在文档重复粘贴）：

- 唯一 `in_progress`
- Title 模糊对齐（Levenshtein）与稳定 ID
- 人机合并（用户 completed 优先）
- 重大结构重构 → `blocked_by_approval`
- ParentID 环检测（模型入参 Phase 1 不暴露）
- 提醒：无清单不催；有未完成项且 step%N==0 才提醒

### 5.4 运行时挂载

- 稳定纪律（互斥）：
  - 执行中：`BuildSystem` → `<task_management>`（`todo_*` 可用时）
  - 规划模式：`BuildSystem` → `<plan_mode_rules>`（`CollaborationMode=plan_mode`），**不**注入 `task_management`
- 任务清单动态提醒：`withTaskListReminder`（仅非规划模式）
- 规划稀疏/交接提醒：`withPlanModeReminders`（user `<system-reminder>`）
- 工具闸门：`collab.FilterToolNames` + `collab.Authorizer`；CLI `/plan` `/execute` `/plan cancel`

---

## 6. 三端存储与广播（摘要）

| 端 | 存储 | 广播 |
| :--- | :--- | :--- |
| CLI / Desktop | `shared/local/tasklist` 文件仓库 | `tasklist/adapter/progress` → `KindTaskList` |
| Enterprise | Phase 2：多租户 DB（主快照与 revision 分离） | Phase 2：SSE/MQ |

沙箱：任务清单元数据在宿主机会话侧；不把宿主机绝对路径当业务路径展示。

规划模式闸门与方案文件属 Runtime（§0.5 / §3），不在本能力域存储。

---

## 7. （已合并）实现附录说明

原 §6–§7 中的完整 service/file_repo/SQL 草稿已移除。需要细节时直接打开对应 Go 源文件；Phase 2 Enterprise schema 在立项时另开设计小节，不在本文堆积。

---

## 8. 任务清单工具定义与 Token 开销限制

规划模式不使用 `todo_*`（§0 / §11.5）。实现与 Description 见源码，**不在本文粘贴**：

| 工具 | 源码 | Description 源 |
| :--- | :--- | :--- |
| `todo_read` | `builtin/todo_read.go` | `tasklist/prompt.ToolTodoReadDescription` |
| `todo_write` | `builtin/todo_write.go` | `tasklist/prompt.ToolTodoWriteDescription` |
| `todo_update_step` | `builtin/todo_update_step.go` | `tasklist/prompt.ToolTodoUpdateStepDescription` |

`todo_read` 过滤已完成项以省 Token；进度滚动优先 `todo_update_step`。

---

## 9. 用户侧展现（摘要）

### 9.1 CLI TUI
订阅 progress.KindTaskList；渲染见 products/cli/internal/tui/tasklist_renderer.go 与 chat 侧任务清单卡片。文案使用「任务清单」。

### 9.2 Desktop / Enterprise Web [Phase 2]
「任务清单」与「实施方案」分面板；具体 CSS/动效在前端立项时单独设计，不在本文堆积样式表。

---

## 10. 渐进式集成与验证计划

### 10.1 自动化测试验证
- **单元测试**：
  编写 `internal/capabilities/tasklist/service/service_test.go`。覆盖 `in_progress` 校验、人机协同合并、ID 对齐与 Reminder 策略。
- **并发与存储测试**：
  在 `shared/local/tasklist/file_repo_test.go` 中，模拟多个 Agent 并发读写各自 session file，确保无死锁、无文件冲突。

### 10.2 手动集成验证

**任务清单（执行期）**
1. 启动 CLI，构造需要 ≥3 步的任务；确认出现 `todo_write`，随后进度滚动优先见 `todo_update_step`。
2. 确认 UI/TUI 渲染「任务清单」，且助手回复未 Markdown 复述整表。
3. 单步琐事（如「现在几点」）不应强制建清单。
4. 校验：两个 `in_progress` 被拒绝；手动破坏存储文件时错误可读。

**规划模式（落地后）**
1. 进入规划模式后无法调用 `todo_*`、无法改业务文件。
2. 仅能更新实施方案；退出批准后出现交接提醒，并先 `todo_write` 再执行。

---

## 11. 通用场景适配与双模式提示词规范

提示词是两套产品能力能否被主模型自觉使用的关键。参考来源：

* Kode Task Management / TodoWrite：`packages/core/src/constants/prompts.ts`、`TodoWriteTool/prompt.ts`
* Kode Plan Mode：`packages/core/src/plan/mode/reminders.ts`、`PlanModeTool/prompt.ts`
* Codex Default Planning：`codex-rs/protocol/src/prompts/base_instructions/default.md`（`## Planning` / `## update_plan`）
* Codex Plan Mode：`codex-rs/collaboration-mode-templates/templates/plan.md`
* 本仓库分层：`docs/提示词分层设计方案.md`（L0 `task_management`、L4 reminder → user 前缀）

### 11.1 场景通用性：任务清单不绑定编码场景

任务清单领域模型基于元描述（Title/Status/Priority/Notes），不绑定 Git/文件系统 API，可跨垂直业务：

* **业务流程办理**：步骤如「提取凭证 → 提交申请 → 对账打款 → 短信通知」
* **智能运维**：步骤如「抓线程栈 → 分析慢 SQL → 临时限流 → 重启回归」
* **编码重构**：步骤如「读入口 → 改接口 → 补测试 → 跑构建」

规划模式同样通用：任何需要「先对齐再动手」的非琐事任务均可进入。

---

### 11.2 投放通道与注入条件

| 块名 | 模式 | Role | 缓存性 | 注入条件 |
| :--- | :--- | :--- | :--- | :--- |
| `task_management` | 任务清单 | system 稳定段 | 高 | `todo_write` 与 `todo_update_step` 已注册且启用，且**当前非规划模式** |
| `plan_mode_rules` | 规划模式 | system / developer 稳定段 | 高（模式期间不变） | 会话处于规划模式 |
| `task_list_reminder` | 任务清单 | user_prefix `<system-reminder>` | 低 | 已有未完成步骤且 step 计数达频率；**无清单时不每轮催促** |
| `plan_mode_reminder` | 规划模式 | user_prefix | 低 | 稀疏复述（full / sparse 交替）；主/子文案分流 |
| `plan_mode_handoff` | 退出交接 | user_prefix 或 tool result | 低 | 用户批准并退出规划模式后注入一次（§0.3） |

**反模式（禁止）**

* 把「尚未建立任务清单」写进**每轮** system（破坏缓存且对琐事过吵）。
* 把规划模式长文与任务清单纪律同时注入（两套目标冲突）。
* 在规划模式仍暴露 `todo_*` 工具。

**实现挂载点（现行）**

* 稳定块：`internal/runtime/prompt/builder.go` 的 `writeTaskManagementBlock` → `<task_management>`，按 `AvailableTools` 条件拼接。
* 动态提醒：`react.WithTaskListReminder` + `withTaskListReminder` → 每轮 `MessageKindReminder`（`<system-reminder>`），不进 system。
* 工具说明：`todo_*` Description 与 system 块同源（`tasklist/prompt`），互补而非重复堆砌。

---

### 11.3 受众：主模型 vs 子智能体

| 提示词 / 工具 | 主模型（`agentId=main`） | 子智能体 |
| :--- | :--- | :--- |
| `task_management` + `todo_*` | 必须注入（非规划模式） | 默认不注入完整纪律；一般不要求维护会话级任务清单 |
| `plan_mode_rules` 完整工作流 | 必须注入（规划模式） | 不注入完整五阶段；仅短 reminder |
| Enter / Exit 规划模式 | 允许 | **禁止** |
| 派 Explore / Plan 类子智能体 | 规划模式主模型可派 | 子智能体不可再嵌套派发（沿用现有 Task 深度限制） |

---

### 11.4 任务清单提示词（`task_management`）

对齐 Codex Default `## Planning` + Kode `# Task Management`：纪律给**主模型**，在**执行期（非规划模式）**注入。

**SSOT**：下列模板的权威副本在 `internal/capabilities/tasklist/prompt/rules.go` 的 `SystemRules`；文档仅作可读副本，改规则只改 Go 常量。

#### 11.4.1 稳定 system 模板

权威正文见 `tasklist/prompt/rules.go` 的 `SystemRules`（下列为可读摘要，勿双份维护长文）：

```text
# 任务清单纪律 (Task List)
…执行期 checklist，不是规划模式实施方案…
## 何时使用 / 何时不要使用
## 硬规则（4 条）
标题精炼；唯一 in_progress + 优先 todo_update_step；结构变更用 todo_write+explanation；勿复述清单/reminder
```

**相对 Kode / Codex 的体量取舍**：Kode system 约 4 条硬规则 + 工具侧长 PROMPT；Codex `## Planning` 含大段正反例。Genesis 取中间偏 Kode：system 短纪律 + Description 互补；不把 Codex 式 example 段塞进稳定前缀。

#### 11.4.2 动态提醒策略（`task_list_reminder`）

**「琐事」判定（Phase 1）**：不引入独立分类器。由 `task_management` 提示词约束模型自觉跳过；Runtime **仅**在「已有未完成清单」时按步数间隔提醒。无清单时默认不催（避免误伤琐事）。若未来要硬 nudge，再用启发式（本轮已调用写文件/命令且仍无清单）作为可选开关，默认关闭。

| 场景 | 是否提醒 | 内容要点 |
| :--- | :---: | :--- |
| 无清单（默认） | 否 | 依赖 system 纪律，不每轮催 |
| 无清单 + 可选启发式开关开启 + 已出现副作用工具仍无清单 | 可偶尔 nudge | 建议建立任务清单 |
| 历史有 `task_list_snapshot`（`ForModel`） | 是（紧凑） | 仅进度 + 当前 `in_progress`；**不**每轮刷全表 |
| 有未完成步骤 + step 计数达 `remindIntervalSteps` | 是（较完整） | 列出少量未完成项，点名优先 `todo_update_step` |
| 全部完成 | 否 | — |

提醒一律 `<system-reminder>` + 「勿向用户复述」脚注（对齐 Kode）。

#### 11.4.3 工具 Description 要点（与 system 互补）

* `todo_write`：强调「结构变更 / 全量覆写」；提及唯一 `in_progress`；勿用于只改一个状态。
* `todo_update_step`：强调「完成或启动下一步时首选」；需要 `id`（来自 `todo_read` 或上下文）。
* `todo_read`：强调自动过滤已完成项以省 Token。

---

### 11.5 规划模式提示词（`plan_mode_rules`）

对齐 Kode Plan Mode reminder + Codex `plan.md`：纪律给**主模型**；与任务清单互斥。

**SSOT**：权威正文在 `internal/capabilities/planmode/prompt/rules.go`（`SystemRules` / `SparseReminder` / `HandoffReminder` / `EnterAck` 等）。文档只保留结构说明，**勿双份维护长文**。

#### 11.5.1 稳定 system 块结构（主模型，刻意加厚）

注入标签：`<plan_mode_rules>`。`BuildSystem` 传入会话方案路径（`.genesis/plans/<session_id>.md`）。

建议章节（与代码一致）：

1. **模式锁** — 不被用户语气解除；「要求执行」=「规划如何执行」
2. **方案文件路径锚点** — 唯一可写通道 `write_implementation_plan`
3. **vs 任务清单** — 硬互斥 `todo_*`
4. **允许 / 禁止** — 只读与委派调研 vs 落地变更
5. **工作流** — grounding → 意图 → 设计 → 落盘 → `exit_plan_mode`
6. **批准硬通道** — 禁止正文「方案可以吗 / 是否继续」
7. **退出后交接关系** — 批准后再 `todo_write`

体量：稳定块对齐 Codex/Kode **硬约束完整度**，不为省 Token 再压缩；稀疏 reminder 只做短复述。

#### 11.5.2 子智能体短 reminder

`SubAgentReminder(planPath)`：只读调研、禁 enter/exit、禁任务清单；带方案路径（由主会话写入）。

#### 11.5.3 稀疏复述与退出交接

* **稀疏 reminder**：每 N=5 iteration；含方案路径 + 批准硬通道短句。
* **进入确认**：`enter_plan_mode` 工具结果 `EnterAck(path, planExists)`（含再进入时先读旧方案）。
* **退出交接**：`HandoffReminder(path)` + exit 工具结果附路径；要求先读方案 → `todo_write` → 再执行。

#### 11.5.4 实施方案产物形态（产品约定）

- **Phase 1 默认**：**方案文件**（§0.5），规划模式唯一可写路径；退出时 UI/工具读取该文件供用户批准。
- **Phase 1.5 可选**：流式结构化提案块（对齐 Codex `<proposed_plan>`）作为展示增强，不替代文件真相源。
- UI 标题一律「实施方案」，不用「任务清单」。

---

### 11.6 旧版 §11.2 差异说明（避免实现回潮）

| 旧表述 | 定稿 |
| :--- | :--- |
| 副作用前必须先 `todo_write` | 仅多步执行需要；琐事跳过；规划模式禁用清单 |
| 进度滚动也用 `todo_write` | **优先** `todo_update_step`；结构变更才 `todo_write` |
| 无清单时每轮 system 催促 | 禁止；改为条件 nudge + user 前缀 |
| 单一「TodoList 纪律」覆盖一切 | 拆成 `task_management` 与 `plan_mode_rules` |

### 11.7 对照 Kode / Codex 的辩证评估（提示词）

两套成熟框架都支持「规划协作」与「执行 checklist」，但拆法不同；Genesis 必须对照**同名能力**，不要把 Kode 的 Task* 当成 Codex Plan Mode。

| 维度 | Kode-CLI | Codex | Genesis 定稿与评价 |
| :--- | :--- | :--- | :--- |
| **执行 checklist** | `# Task Management` 短 system（~4 硬规则）+ Task*/TodoWrite 工具长 PROMPT；变更/空列表 → user 前缀 `<system-reminder>` | base `## Planning`（含正反例，偏长）+ 短 `update_plan` Description；**无**步数催促 | 取 Kode 的「system 短 + Description 互补」；保留 Codex 的「何时用/不用」；**不**照搬 Codex 长 example（稳定前缀成本高） |
| **规划协作** | Plan permission mode；主模型 full/sparse reminder（五阶段）；**允许**在规划期用 Task* | `plan.md` developer 指令 + `<proposed_plan>`；handler **硬禁** `update_plan` | **对齐 Codex 互斥**：规划模式不暴露 `todo_*`，避免「写实施方案」与「刷进度条」抢注意力；退出后 `plan_mode_handoff` 再 `todo_write`（策略 A） |
| **退出交接** | ExitPlanMode 结果明确「先 TaskCreate/Update」 | UI 切 Default + “Implement the plan.” / 清上下文粘贴方案 | 采用策略 A（显式交接 nudge），不静默自动拆清单 |
| **是否规则过多** | TodoWrite 工具 PROMPT 与 Plan 主 reminder 可能过重 | Default Planning 正反例 + plan.md ~9KB 叠层 | 任务清单侧保持短纪律；**规划模式稳定块刻意加厚**（模式锁/路径锚点/可禁列表/决策完备结构/批准硬通道），稀疏 reminder 仅短复述，避免每轮重灌全文 |

**结论（辩证）**：成熟框架「效果好」不等于「规则越多越好」。Kode 的强项是分层与节流；Codex 的强项是双通道硬隔离与 Planning 可操作性。Genesis：**任务清单偏短、规划模式稳定块对齐 Codex/Kode 硬约束并不再压缩**；Runtime 闸门/互斥/交接已落地。勿在执行期再堆长 example 去「追平」Codex Planning 字数。

---


## 12. 审计结论：设计与实现对齐 (Code-Doc Alignment)

映射本文（含 §0 / §11 双模式定稿）与当前代码：

### 12.1 对齐概览表

| 需求/设计要点 | 设计文档位置 | 代码实现位置 (Evidence) | 状态 | 偏差与说明 |
| :--- | :--- | :--- | :---: | :--- |
| **产品双模式命名与边界** | §0 | `runtime/collab` + CLI 状态栏 | `Implemented` | Desktop/Enterprise UI 仍仅契约兼容。 |
| **规划模式进入/退出/方案文件默认** | §0.5 | enter/exit 工具 + `/plan` `/execute` + `.genesis/plans/` | `Implemented` | 退出经 Approval `plan.exit_approve` 或 `/execute`。 |
| **ParentID 不对模型暴露（P1）** | §2.3B | model 有字段；todo_write schema 无 parent_id | `Aligned (intentional)` | 服务端可校验树；LLM 扁平写入，避免工具过载。 |
| **任务清单：唯一进行中约束** | §2.1, §11.4 | `tasklist/service/service.go` | `Implemented` | `UpdateTaskList` / `UpdateStepStatus` 限制 `in_progress` ≤ 1。 |
| **任务清单：todo_* 工具与三端启用** | §1, §8 | `builder.go`、各端 `default_profile` | `Implemented` | 工具已注册且 Profile 启用。 |
| **任务清单：稳定 system 块 `task_management`** | §5.2, §11.4 | `tasklist/prompt/rules.go` + `builder.go` | `Implemented` | SSOT 已挂载为 `<task_management>`；与 `plan_mode_rules` 按 `CollaborationMode` 互斥。 |
| **提示词 SSOT（Description=System 同源）** | §5.2 | `tasklist/prompt` → tools + builder | `Implemented` | — |
| **命名去债 tasklist** | §0.6 | `capabilities/tasklist`、`domain.TaskList` | `Implemented` | 工具名仍 `todo_*`。 |
| **任务清单：进度优先 `todo_update_step`** | §11.4 | SystemRules + Tool Description | `Implemented` | — |
| **任务清单提醒策略（勿每轮催无清单）** | §11.4.2 | `ForModel` 紧凑进度 + `withTaskListReminder` | `Implemented` | 无清单不催；snapshot 仅紧凑进度；完整未完成项按 iteration 间隔。 |
| **规划模式 Runtime 闸门** | §0, §2.3A, §11.5 | `collab.ToolGate` + Authorizer | `Implemented` | allowlist 硬闸；todo_* 与变更工具不可见。 |
| **规划模式提示词主/子分流** | §11.3, §11.5 | `planmode/prompt` + react reminders | `Implemented` | 主模型 full/sparse；子智能体短 reminder；Depth 禁 enter/exit。 |
| **退出规划模式 → todo_write 交接** | §0.3, §11.5.3 | HandoffPending + HandoffReminder | `Implemented` | 策略 A；批准后下一次成功 LLM 调用前 ephemeral 注入（含同 Run exit 后下一 iteration）。 |
| **已完成任务过滤归档** | §8.1 | `todo_read.go` | `Implemented` | — |
| **稳定 ID / 人机合并 / 重构审批** | §5.3 | `tasklist/service` | `Implemented` | — |
| **ParentID 树与环检测** | §5.3 | `tasklist/model` + `service` | `Implemented` | 模型入参 P1 不暴露。 |
| **事件广播契约** | §6 | `tasklist/adapter/progress` | `Implemented` | Enterprise SSE Phase 2。 |
| **本地文件存储** | §6 | `shared/local/tasklist` | `Implemented` | — |
| **CLI TUI 任务清单渲染** | §9.1 | `tasklist_renderer.go` + chat | `Implemented` | — |
| **企业多租户 DB / Web 双面板** | §6, §9.2 | — | `Unimplemented (Phase 2)` | 按规划暂缓。 |

### 12.2 Gaps & 反思说明

- **任务清单纪律已挂载**；是否被模型遵循仍取决于基座模型与场景复杂度。
- **两套能力互斥已落地**：`BuildSystem` 按 `CollaborationMode` 注入 `plan_mode_rules` 或 `task_management`，不同时出现。
- **提醒通道**：任务清单与规划 sparse/handoff 均走 user reminder（`<system-reminder>`）。
- **Desktop/Enterprise**：工具 Profile、CollabStore/PlanDocuments Port 与 `plan.exit_approve` 拒绝自动批准已接线；双面板 UI / 真审批队列仍属 Phase 2。

### 12.3 下一步行动计划 (Actionable Next Steps)

1. **[P2] Enterprise / Desktop 双面板 UI**  
   - 任务清单 / 实施方案分面板；复用 `KindCollaborationMode` / `KindPlanDocument`。

2. **[P2+] 通用结构化选择工具**（非规划专用）  
   - `AskUserQuestion` 类；规划 allowlist 预留扩展位。
