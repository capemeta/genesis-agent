# Plan & TodoList 能力域最佳设计方案

本方案定义 `genesis-agent` 项目中“计划与待办事项 (Plan / TodoList)”能力域的架构与工具链实现。旨在为 AI 运行时（Agent Runtime）提供有条不紊的任务流拆解、跟进和反馈机制，防止长链任务中的记忆漂移（Memory Drift）。

---

## 1. 目标与定位

在 AI 编程与长任务执行中，Agent 需要具备“做计划、看计划、改计划”的闭环能力：
- **AI 侧**：提供全局覆写工具 `todo_write` 与 Delta 状态滚动工具 `todo_update_step`，让 Agent 自主拆分任务、降低出站 Token 开销并极速更新进度。
- **Runtime 侧**：在执行 Step 的循环（Agent Loop）中，定期把未完成任务通过 System Prompt / Context 被动提醒 AI，督促其持续推进。
- **用户侧 (UI)**：在 CLI 端渲染为优雅 TUI 清单，在 Desktop / Enterprise 渲染为富美学（Rich Aesthetics）的可视化面板或看板，并融入人工审批干预拦截状态。

定位：
- 它是 **Agent Runtime 核心能力域之一**，目录落点为 `internal/capabilities/plan/`。
- 它遵循“契约（Port）与适配器（Adapter）分层解耦”原则。
- 解决 CLI、Desktop、Enterprise 三个产品在数据存储、权限过滤及 UI 表现上的统分平衡。

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

#### A. 规划模式锁定工具 (借鉴自 Kode-CLI)
* **对应源码**：
  * [EnterPlanModeTool.tsx](file:///D:/workspace/go/go-project/Kode-CLI/packages/tools/src/tools/interaction/PlanModeTool/EnterPlanModeTool.tsx)
  * [ExitPlanModeTool.tsx](file:///D:/workspace/go/go-project/Kode-CLI/packages/tools/src/tools/interaction/PlanModeTool/ExitPlanModeTool.tsx)
* **实现细节**：`EnterPlanMode` 执行后，Agent 会进入规划模式，核心运行时会对除 `Read/Grep/todo_write` 以外的所有“变更类、执行类”工具（如 `Edit`、`Write` 、`Bash` 等）进行执行拦截或限制，迫使 Agent 仅进行只读调研和计划拟定。确认计划后，通过 `ExitPlanMode` 解锁工具执行权限。
* **Genesis 折中决策**：第一阶段在 Capabilities 工具层我们不增加 `EnterPlanMode` 这种侵入式命令。我们将工具锁定设计为 **Runtime 层的策略机制**（在 `internal/runtime/strategy` 中通过 Runtime 限制大模型可用的工具列表，而非将状态放在 capabilities 中），以此获得更高的灵活性。

#### B. 任务追踪系列工具 (借鉴自 Kode-CLI)
* **对应源码**：
  * [TaskCreateTool.tsx](file:///D:/workspace/go/go-project/Kode-CLI/packages/tools/src/tools/interaction/TaskCreateTool/TaskCreateTool.tsx)
  * [TaskListTool.tsx](file:///D:/workspace/go/go-project/Kode-CLI/packages/tools/src/tools/interaction/TaskListTool/TaskListTool.tsx)
  * [TaskGetTool.tsx](file:///D:/workspace/go/go-project/Kode-CLI/packages/tools/src/tools/interaction/TaskGetTool/TaskGetTool.tsx)
  * [TaskUpdateTool.tsx](file:///D:/workspace/go/go-project/Kode-CLI/packages/tools/src/tools/interaction/TaskUpdateTool/TaskUpdateTool.tsx)
* **实现细节**：在 Agent 内部构建了一套轻量级的 Issue/Task Tracker。Agent 可以自行分裂子 Task，并通过 REST 式的微型增删改查工具独立管理。
* **Genesis 折中决策**：对于单个 Session 内串行工作的代码 Agent 来说，维护两套待办任务系统（Todo 和 Task Tracker）会导致 LLM 产生工具调用混淆。我们在 Phase 1 将两者的职责进行了**合并简化**：采用带 `ParentID` 的 Steps 结构（见 5.1 节），大模型只需调用 `todo_read` 和 `todo_write` 便可完成任务列表的管理，既降低了 Context 损耗，又保持了接口一致性。

#### C. 目标与 SaaS 成本防御工具 (借鉴自 Codex)
* **对应源码**：
  * [codex-rs/ext/goal/src/tool.rs](file:///D:/workspace/go/go-project/codex/codex-rs/ext/goal/src/tool.rs)
  * [codex-rs/ext/goal/src/spec.rs](file:///D:/workspace/go/go-project/codex/codex-rs/ext/goal/src/spec.rs)
* **实现细节**：Codex 提供了 `create_goal`、`get_goal`、`update_goal` 目标管理工具，并且在此维度中集成了 `token_budget` (Token 限制预算) 与 `time_used_seconds` (执行耗时) 记账。若 Agent 运行超出预算，目标状态将强制变为 `BudgetLimited` 并限制执行，防止大模型死循环浪费高额账单。
* **Genesis 折中决策**：这一机制在 Enterprise 多租户环境下具有重大防御价值。我们在 `Plan` 领域模型中特意保留了 `Version` 和 `LatestExplanation` 扩展支持，并在 Phase 2 演进计划中，将利用此特性对接 `internal/capabilities/usage` (用量审计域)，在 `service.UpdatePlan` 校验流中实现 Token 预算防线。

---

## 3. 架构设计与职责边界

为了在三线产品中取得“内核统一、接入独立”的平衡，我们将架构分为四层：

```text
+--------------------------------------------------------------+
|                cmd/ (cli, desktop, enterprise)               |
+--------------------------------------------------------------+
                               | (Bootstrap 注入 Repository/UI/Broadcaster)
                               v
+--------------------------------------------------------------+
|            internal/capabilities/tool/adapter/               |
|            - todo_read / todo_write / todo_update_step       |
+--------------------------------------------------------------+
                               |
                               v
+--------------------------------------------------------------+
|            internal/capabilities/plan/service/               |
|            - service.go (防幻觉对齐、防冲突合并、被动提醒)  |
+--------------------------------------------------------------+
             /                                     \
            v                                       v
+-------------------------------------+   +----------------------------+
| internal/capabilities/plan/contract/ |   | plan/contract/             |
| - repository.go (Port: 存储契约接口) |   | - broadcaster.go (Port)    |
+-------------------------------------+   +----------------------------+
       /           |           \                        |
      v            v            v                       v
+----------+ +-----------+ +----------+   +----------------------------+
|shared/   | |desktop/   | |enterprise|   | - CLI: No-op               |
|local/    | |internal/  | |/adapters/|   | - Desktop: Wails Event     |
|file_repo | |memory_repo| |db_repo   |   | - Enterprise: Redis/SSE    |
+----------+ +-----------+ +----------+   +----------------------------+
```

### 职责边界说明
- `internal/capabilities/plan/model/` & `contract/`：
  - 定义计划（Plan）、步骤（Step）数据结构，以及存储层 `Repository` 和服务层 `Service` 的接口。
  - **绝对不能**直接依赖 Wails、Docker SDK、PostgreSQL、或 HTTP 框架。
- `internal/capabilities/plan/service/`：
  - 核心逻辑处理。包括 ID 稳定对齐（相似度模糊对齐）、人机协同冲突合并、死循环停滞检测以及被动提醒 Prompt 生成。
- `internal/capabilities/tool/`：
  - 注册 `todo_read` / `todo_write` / `todo_update_step` 模型工具，注入 Traits 元数据供 `PermissionEngine` 进行权限过滤。其底层仅依赖 `PlanService` 接口。
- `shared/local/` / `products/enterprise/internal/adapters/`：
  - 提供各自具体的 `Repository` 与 `EventBroadcaster` 适配层。为了防止 JSON 读写膨胀风险，变更历史在 Enterprise 库以只写追加表的物理方式与 Plan 主状态分离存储。

---

## 4. 目录与相关文件列表

为便于后续开发，定义新目录与核心关联文件如下：

| 文件/目录路径 | 状态 | 职责 |
| :--- | :--- | :--- |
| `internal/capabilities/plan/model/plan.go` | [NEW] | 基础领域模型：`Plan`、`Step`、`StepStatus` 等 |
| `internal/capabilities/plan/contract/repository.go` | [NEW] | 存储层 Port 接口（包括 Snapshot 与 Revision 追加接口） |
| `internal/capabilities/plan/contract/broadcaster.go` | [NEW] | 事件通知广播 Port 接口 |
| `internal/capabilities/plan/contract/service.go` | [NEW] | 核心服务层 Port 接口 |
| `internal/capabilities/plan/service/service.go` | [NEW] | ID 稳定相似度对齐、防冲突合并、被动提醒停滞抑制等核心服务实现 |
| `shared/local/plan/file_repo.go` | [NEW] | 本地 JSON 格式的文件存储适配器（用于 CLI / Desktop） |
| `products/enterprise/internal/adapters/plan/db_repo.go` | [NEW - Phase 2 暂缓] | 企业版 PostgreSQL 多租户存储适配器（主状态与历史日志表级分离，支持多 DB 路由。当前环境未引入多租户 DB，暂缓开发） |
| `internal/capabilities/tool/adapter/builtin/todo_read.go` | [NEW] | 对接 AI 的只读工具（支持已完成项智能过滤归档） |
| `internal/capabilities/tool/adapter/builtin/todo_write.go` | [NEW] | 对接 AI 的全量更新/写入工具 |
| `internal/capabilities/tool/adapter/builtin/todo_update_step.go` | [NEW] | 对接 AI 的 Delta 局部状态修改工具（大幅降低 Token 与延时） |
| `products/cli/internal/tui/plan_renderer.go` | [NEW] | 命令行下的树形进度列表渲染（高对比度 TUI） |
| `products/enterprise/web/src/components/PlanCard/` | [NEW - Phase 2 暂缓] | 网页端毛玻璃、动效任务面板（Desktop/Enterprise 前端初始化后开发） |

---

## 5. 核心模型与契约 (Go 语言定义)

### 5.1 领域模型：`plan.go`
```go
package model

import "time"

type StepStatus string

const (
	StepStatusPending             StepStatus = "pending"
	StepStatusInProgress          StepStatus = "in_progress"
	StepStatusCompleted           StepStatus = "completed"
	StepStatusBlockedByApproval   StepStatus = "blocked_by_approval" // 引入人工审批阻断状态，支持 HiTL (Human-in-the-loop) 绑定
)

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

type Step struct {
	ID          string     `json:"id"`
	ParentID    string     `json:"parent_id,omitempty"` // 引入可选的父 ID，支持层级任务展示（如甘特图），不破坏 AI 的扁平列表传入
	Title       string     `json:"title"`
	Status      StepStatus `json:"status"`
	Priority    Priority   `json:"priority,omitempty"`
	Assignee    string     `json:"assignee,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type RevisionLog struct {
	Version     int64     `json:"version"`
	Explanation string    `json:"explanation,omitempty"`
	Operator    string    `json:"operator"` // "agent" 或 "user"，用于审计人机协同变更
	Timestamp   time.Time `json:"timestamp"`
}

type Plan struct {
	SessionID         string    `json:"session_id"`
	Steps             []Step    `json:"steps"`
	LatestExplanation string    `json:"latest_explanation,omitempty"`
	Version           int64     `json:"version"` // 乐观锁版本
	UpdatedAt         time.Time `json:"updated_at"`
}
```

### 5.2 契约接口：`repository.go` & `broadcaster.go`

#### repository.go
```go
package contract

import (
	"context"
	"genesis/internal/capabilities/plan/model"
)

type Repository interface {
	// GetPlan 获取指定 Session 的最新计划快照
	GetPlan(ctx context.Context, sessionID string) (*model.Plan, error)
	// SavePlan 保存或更新计划快照，且需以 Append-only 追加审计日志（解耦大 JSON 读写膨胀风险）
	SavePlan(ctx context.Context, plan *model.Plan, revision *model.RevisionLog) error
	// GetHistory 获取变更日志记录
	GetHistory(ctx context.Context, sessionID string) ([]model.RevisionLog, error)
}
```

#### broadcaster.go
```go
package contract

import (
	"context"
	"genesis/internal/capabilities/plan/model"
)

type EventType string

const (
	EventPlanCreated EventType = "plan_created"
	EventPlanUpdated EventType = "plan_updated"
)

type PlanEvent struct {
	Type        EventType   `json:"type"`
	SessionID   string      `json:"session_id"`
	Plan        *model.Plan `json:"plan"`
	Explanation string      `json:"explanation,omitempty"`
	Timestamp   time.Time   `json:"timestamp"`
}

type EventBroadcaster interface {
	Broadcast(ctx context.Context, event PlanEvent) error
}
```

### 5.3 核心服务契约：`service.go`
```go
package contract

import (
	"context"
	"genesis/internal/capabilities/plan/model"
)

type Service interface {
	GetPlan(ctx context.Context, sessionID string) (*model.Plan, error)
	// UpdatePlan 全量更新计划
	UpdatePlan(ctx context.Context, sessionID string, steps []model.Step, explanation string, operator string) (*model.Plan, error)
	// UpdateStepStatus 局部（Delta）状态更新，提供极致低推理延迟
	UpdateStepStatus(ctx context.Context, sessionID string, stepID string, status model.StepStatus, explanation string, operator string) (*model.Plan, error)
	GeneratePromptReminder(ctx context.Context, sessionID string, currentStep int) (string, bool, error)
}
```

---

## 6. 核心业务逻辑、防重排 ID 模糊对齐与局部更新

### 6.1 `service.go` 核心校验、莱文斯坦模糊对齐与差量状态转移
```go
package service

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"genesis/internal/capabilities/plan/contract"
	"genesis/internal/capabilities/plan/model"
)

type PlanService struct {
	repo                contract.Repository
	broadcaster         contract.EventBroadcaster
	remindIntervalSteps int
}

func NewPlanService(repo contract.Repository, broadcaster contract.EventBroadcaster, remindIntervalSteps int) *PlanService {
	if remindIntervalSteps <= 0 {
		remindIntervalSteps = 3
	}
	return &PlanService{
		repo:                repo,
		broadcaster:         broadcaster,
		remindIntervalSteps: remindIntervalSteps,
	}
}

func (s *PlanService) GetPlan(ctx context.Context, sessionID string) (*model.Plan, error) {
	return s.repo.GetPlan(ctx, sessionID)
}

// levenshtein 计算两个字符串的编辑距离，用于防御 AI 重新拟订计划时微调 Title 造成的 ID 错位
func levenshtein(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	len1, len2 := len(r1), len(r2)
	column := make([]int, len1+1)
	for y := 1; y <= len1; y++ {
		column[y] = y
	}
	for x := 1; x <= len2; x++ {
		column[0] = x
		lastkey := x - 1
		for y := 1; y <= len1; y++ {
			oldkey := column[y]
			incr := 0
			if r1[y-1] != r2[x-1] {
				incr = 1
			}
			column[y] = int(math.Min(math.Min(float64(column[y]+1), float64(column[y-1]+1)), float64(lastkey+incr)))
			lastkey = oldkey
		}
	}
	return column[len1]
}

// isSimilar 判定语义是否相似（相似度大于 80%）
func isSimilar(title1, title2 string) bool {
	t1, t2 := strings.TrimSpace(title1), strings.TrimSpace(title2)
	if t1 == t2 {
		return true
	}
	maxLen := math.Max(float64(len(t1)), float64(len(t2)))
	if maxLen == 0 {
		return true
	}
	dist := levenshtein(t1, t2)
	similarity := 1.0 - (float64(dist) / maxLen)
	return similarity >= 0.8
}

// generateStableID 根据 Title 文本生成序号无关的唯一稳定 ID
func generateStableID(title string) string {
	cleaned := strings.TrimSpace(strings.ToLower(title))
	hash := md5.Sum([]byte(cleaned))
	return fmt.Sprintf("task_%x", hash[:4])
}

// MergeCollaborativePlan 人机协同智能三方合并
func MergeCollaborativePlan(oldPlan *model.Plan, incomingSteps []model.Step, operator string) []model.Step {
	if oldPlan == nil {
		return incomingSteps
	}

	mergedMap := make(map[string]model.Step)
	for _, step := range incomingSteps {
		mergedMap[step.ID] = step
	}

	for _, oldStep := range oldPlan.Steps {
		incomingStep, exists := mergedMap[oldStep.ID]
		if !exists {
			continue
		}
		// 若 AI 提交，而用户已经标记完成，保留用户高权威标记
		if operator == "agent" && oldStep.Status == model.StepStatusCompleted && incomingStep.Status == model.StepStatusPending {
			incomingStep.Status = model.StepStatusCompleted
		}
		mergedMap[oldStep.ID] = incomingStep
	}

	result := make([]model.Step, 0, len(incomingSteps))
	for _, step := range incomingSteps {
		result = append(result, mergedMap[step.ID])
	}
	return result
}

func (s *PlanService) UpdatePlan(ctx context.Context, sessionID string, steps []model.Step, explanation string, operator string) (*model.Plan, error) {
	// 1. 唯一进行中校验
	inProgressCount := 0
	for _, step := range steps {
		if step.Status == model.StepStatusInProgress {
			inProgressCount++
		}
	}
	if inProgressCount > 1 {
		return nil, errors.New("仅允许同时将 1 个待办事项标记为 'in_progress'")
	}

	// 2. 读取现有 plan
	oldPlan, err := s.repo.GetPlan(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("读取旧计划失败: %w", err)
	}

	var nextVersion int64 = 1
	if oldPlan != nil {
		nextVersion = oldPlan.Version + 1
	}

	// 3. 防重排的 ID 稳定性对齐（剔除 index 权值，引入莱文斯坦模糊对齐）
	now := time.Now()
	for i, step := range steps {
		stableID := generateStableID(step.Title)
		steps[i].ID = stableID

		if oldPlan != nil {
			matched := false
			// 优先根据稳定 ID 检索
			for _, oldStep := range oldPlan.Steps {
				if oldStep.ID == stableID {
					steps[i].CreatedAt = oldStep.CreatedAt
					matched = true
					break
				}
			}
			// 稳定 ID 丢失（AI 修改了微小字符），启动 Levenshtein 模糊对齐
			if !matched {
				for _, oldStep := range oldPlan.Steps {
					if isSimilar(oldStep.Title, step.Title) {
						steps[i].ID = oldStep.ID // 锁定为旧 ID，防止抖动
						steps[i].CreatedAt = oldStep.CreatedAt
						matched = true
						break
					}
				}
			}
		}

		if steps[i].CreatedAt.IsZero() {
			steps[i].CreatedAt = now
		}
		steps[i].UpdatedAt = now
	}

	// 4. 三方合并
	finalSteps := MergeCollaborativePlan(oldPlan, steps, operator)

	// 5. 生成只写追加变更日志
	revision := &model.RevisionLog{
		Version:     nextVersion,
		Explanation: explanation,
		Operator:    operator,
		Timestamp:   now,
	}

	// 6. 存储主快照并追加审计日志
	plan := &model.Plan{
		SessionID:         sessionID,
		Steps:             finalSteps,
		LatestExplanation: explanation,
		Version:           nextVersion,
		UpdatedAt:         now,
	}
	if err := s.repo.SavePlan(ctx, plan, revision); err != nil {
		return nil, fmt.Errorf("保存计划失败: %w", err)
	}

	// 7. 广播
	s.broadcastUpdate(ctx, sessionID, plan, explanation, oldPlan == nil)
	return plan, nil
}

func (s *PlanService) UpdateStepStatus(ctx context.Context, sessionID string, stepID string, status model.StepStatus, explanation string, operator string) (*model.Plan, error) {
	// Delta 状态修改服务，避免全量读写，大幅降低推理延迟
	oldPlan, err := s.repo.GetPlan(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if oldPlan == nil {
		return nil, errors.New("计划尚未创建，无法进行局部状态更新")
	}

	// 检查 status
	if status == model.StepStatusInProgress {
		for _, step := range oldPlan.Steps {
			if step.ID != stepID && step.Status == model.StepStatusInProgress {
				return nil, errors.New("仅允许同时将 1 个待办事项标记为 'in_progress'")
			}
		}
	}

	now := time.Now()
	stepFound := false
	for i, step := range oldPlan.Steps {
		if step.ID == stepID {
			oldPlan.Steps[i].Status = status
			oldPlan.Steps[i].UpdatedAt = now
			stepFound = true
			break
		}
	}
	if !stepFound {
		return nil, fmt.Errorf("未找到 ID 为 [%s] 的步骤", stepID)
	}

	nextVersion := oldPlan.Version + 1
	revision := &model.RevisionLog{
		Version:     nextVersion,
		Explanation: explanation,
		Operator:    operator,
		Timestamp:   now,
	}

	plan := &model.Plan{
		SessionID:         sessionID,
		Steps:             oldPlan.Steps,
		LatestExplanation: explanation,
		Version:           nextVersion,
		UpdatedAt:         now,
	}
	if err := s.repo.SavePlan(ctx, plan, revision); err != nil {
		return nil, err
	}

	s.broadcastUpdate(ctx, sessionID, plan, explanation, false)
	return plan, nil
}

func (s *PlanService) broadcastUpdate(ctx context.Context, sessionID string, plan *model.Plan, explanation string, isNew bool) {
	eventType := contract.EventPlanUpdated
	if isNew {
		eventType = contract.EventPlanCreated
	}
	_ = s.broadcaster.Broadcast(ctx, contract.PlanEvent{
		Type:        eventType,
		SessionID:   sessionID,
		Plan:        plan,
		Explanation: explanation,
		Timestamp:   time.Now(),
	})
}

func (s *PlanService) GeneratePromptReminder(ctx context.Context, sessionID string, currentStep int) (string, bool, error) {
	plan, err := s.repo.GetPlan(ctx, sessionID)
	if err != nil {
		return "", false, err
	}

	if plan == nil || len(plan.Steps) == 0 {
		prompt := "【系统提醒】当前任务尚未规划待办事项列表 (TodoList)。请使用 `todo_write` 工具建立结构化计划，这能有效防止您的思维漂移并帮助您把控进度。"
		return prompt, true, nil
	}

	var activeSteps []model.Step
	for _, step := range plan.Steps {
		if step.Status != model.StepStatusCompleted {
			activeSteps = append(activeSteps, step)
		}
	}

	if len(activeSteps) == 0 {
		return "", false, nil
	}

	if currentStep > 0 && currentStep%s.remindIntervalSteps == 0 {
		var sb strings.Builder
		sb.WriteString("【系统提醒】当前任务进度跟进（未完成项）：\n")
		for i, step := range activeSteps {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("... 还有 %d 项未完成\n", len(activeSteps)-5))
				break
			}
			
			statusLabel := string(step.Status)
			if step.Status == model.StepStatusInProgress {
				statusLabel = "★ IN_PROGRESS"
				if step.Notes != "" {
					sb.WriteString(fmt.Sprintf("%d. [%s] %s (备注: %s)\n", i+1, statusLabel, step.Title, step.Notes))
					continue
				}
			}
			sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, statusLabel, step.Title))
		}
		sb.WriteString("\n若当前步骤已完成，请务必使用 `todo_update_step` 将其标记为 completed 并启动下一步。避免在同一子任务内陷入死循环。")
		return sb.String(), true, nil
	}

	return "", false, nil
}
```

### 6.2 运行时中间件挂载 (Runtime Middleware Hook)
在 `internal/runtime/strategy/` 执行流中挂载：
```go
func PromptInjectorMiddleware(planSvc contract.Service) runtime.Middleware {
	return func(next runtime.Handler) runtime.Handler {
		return runtime.HandlerFunc(func(ctx context.Context, session *runtime.Session) (*runtime.Result, error) {
			reminder, needed, err := planSvc.GeneratePromptReminder(ctx, session.ID, session.StepCount)
			if err == nil && needed {
				session.AppendSystemMessage(reminder)
			}
			return next.Serve(ctx, session)
		})
	}
}
```

---

## 7. 三大产品的“统”与“分”平衡

### 7.1 本地隔离存储适配器 (CLI/Desktop FileRepository)
`shared/local/plan/file_repo.go` 实现本地 JSON 读写（直接本地追加日志写以保持简易性）。

### 7.2 企业 SaaS 多租户、并发安全与动态多 DB 路由 (Enterprise) [Phase 2 暂缓开发]
在 `products/enterprise/internal/adapters/plan/db_repo.go` 中，为了应对超大型企业 SaaS 物理隔离安全要求，设计了 **动态数据源路由器（Dynamic DataSource Router）** 机制：

#### [NEW] [context.go](file:///d:/workspace/go/genesis-agent/internal/platform/contextutil/context.go)
为了让 Repository 能通过隐式 Context 提取 `tenant_id` 和 `user_id`，建议在项目中新建此文件：

```go
package contextutil

import "context"

type contextKey string

const (
	tenantIDKey  contextKey = "tenant_id"
	userIDKey    contextKey = "user_id"
	sessionIDKey contextKey = "session_id"
)

func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

func GetTenantID(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(tenantIDKey).(string)
	return val, ok
}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

func GetUserID(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(userIDKey).(string)
	return val, ok
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

func GetSessionID(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(sessionIDKey).(string)
	return val, ok
}
```

接下来，在 `products/enterprise/internal/adapters/plan/db_repo.go` 中，为了应对超大型企业 SaaS 物理隔离安全要求，设计了 **动态数据源路由器（Dynamic DataSource Router）** 机制：

```go
package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	
	"genesis/internal/capabilities/plan/model"
	"genesis/internal/platform/contextutil"
)

type DbRepository struct {
	dbRouter DbRouter
}

type DbRouter interface {
	GetConnection(ctx context.Context, tenantID string) (*sql.DB, error)
}

func NewDbRepository(router DbRouter) *DbRepository {
	return &DbRepository{dbRouter: router}
}

func (r *DbRepository) GetPlan(ctx context.Context, sessionID string) (*model.Plan, error) {
	tenantID, ok := contextutil.GetTenantID(ctx)
	if !ok || tenantID == "" {
		return nil, errors.New("context 缺失 tenant_id，拒绝操作")
	}

	db, err := r.dbRouter.GetConnection(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("路由物理数据库连接失败: %w", err)
	}

	var stepsData []byte
	var latestExplanation string
	var version int64
	var updatedAt time.Time

	// 仅读取主表中的 Steps 最新快照，响应开销恒定，绝无膨胀风险
	err = db.QueryRowContext(ctx,
		"SELECT steps, latest_explanation, version, updated_at FROM agent_plans WHERE tenant_id = $1 AND session_id = $2",
		tenantID, sessionID,
	).Scan(&stepsData, &latestExplanation, &version, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var steps []model.Step
	if err := json.Unmarshal(stepsData, &steps); err != nil {
		return nil, err
	}

	return &model.Plan{
		SessionID:         sessionID,
		Steps:             steps,
		LatestExplanation: latestExplanation,
		Version:           version,
		UpdatedAt:         updatedAt,
	}, nil
}

func (r *DbRepository) SavePlan(ctx context.Context, plan *model.Plan, revision *model.RevisionLog) error {
	tenantID, ok := contextutil.GetTenantID(ctx)
	if !ok || tenantID == "" {
		return errors.New("context 缺失 tenant_id，拒绝操作")
	}
	userID, _ := contextutil.GetUserID(ctx)

	db, err := r.dbRouter.GetConnection(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("路由物理数据库连接失败: %w", err)
	}

	stepsData, err := json.Marshal(plan.Steps)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. 悲观锁锁定并发状态
	var currentVersion int64
	err = tx.QueryRowContext(ctx, 
		"SELECT version FROM agent_plans WHERE tenant_id = $1 AND session_id = $2 FOR UPDATE",
		tenantID, plan.SessionID,
	).Scan(&currentVersion)

	if err == sql.ErrNoRows {
		// 插入最新快照
		_, err = tx.ExecContext(ctx,
			`INSERT INTO agent_plans (session_id, tenant_id, user_id, steps, latest_explanation, version, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
			plan.SessionID, tenantID, userID, stepsData, plan.LatestExplanation, plan.Version,
		)
	} else if err == nil {
		// 乐观版本校验
		if plan.Version <= currentVersion {
			return fmt.Errorf("并发锁冲突: 当前版本 %d 已被他人修改，请刷新后重试", currentVersion)
		}
		// 更新最新快照
		_, err = tx.ExecContext(ctx,
			`UPDATE agent_plans 
			 SET steps = $1, latest_explanation = $2, version = $3, updated_at = NOW()
			 WHERE tenant_id = $4 AND session_id = $5`,
			stepsData, plan.LatestExplanation, plan.Version, tenantID, plan.SessionID,
		)
	}
	if err != nil {
		return err
	}

	// 2. 将变更历史以 Append-only 方式写入独立审计表 (Outbox/Audit Separation)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO agent_plan_revisions (session_id, tenant_id, version, explanation, operator, timestamp)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		plan.SessionID, tenantID, revision.Version, revision.Explanation, revision.Operator, revision.Timestamp,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *DbRepository) GetHistory(ctx context.Context, sessionID string) ([]model.RevisionLog, error) {
	tenantID, ok := contextutil.GetTenantID(ctx)
	if !ok || tenantID == "" {
		return nil, errors.New("context 缺失 tenant_id，拒绝操作")
	}

	db, err := r.dbRouter.GetConnection(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx,
		`SELECT version, explanation, operator, timestamp 
		 FROM agent_plan_revisions 
		 WHERE tenant_id = $1 AND session_id = $2 
		 ORDER BY version ASC`,
		tenantID, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []model.RevisionLog
	for rows.Next() {
		var rev model.RevisionLog
		err := rows.Scan(&rev.Version, &rev.Explanation, &rev.Operator, &rev.Timestamp)
		if err != nil {
			return nil, err
		}
		history = append(history, rev)
	}
	return history, nil
}
```

### 7.3 沙箱隔离与工具穿越 (Cross-Boundary Sandbox)
宿主机 Host 统一拦截。

---

## 8. 工具定义与 Token 开销限制 (Tool Definitions & Token Control)

在为大模型注册工具时，需针对长序列执行做 Token 开销限制，设计防膨胀机制。

### 8.1 `todo_read` 工具：已完成任务过滤归档
大模型使用该工具读取待办列表。默认只下发未完成项和少量已完成参考项。

### 8.2 `todo_update_step` 工具：局部差量状态更新
大模型仅需传入 ID 和 status 即可快速变更进度，无须输出庞大的全量 steps。
```go
package builtin

import (
	"context"
	"encoding/json"
	
	"genesis/internal/capabilities/plan/contract"
	"genesis/internal/capabilities/tool/contract"
)

type TodoUpdateStepTool struct {
	planSvc plancontract.Service
}

func (t *TodoUpdateStepTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "todo_update_step",
		Description: "差量更新待办步骤状态。在步骤完成或启动时调用，参数轻量，出站 Token 小，能有效降低生成延时。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Required: []string{"id", "status"},
			Properties: map[string]*tool.ParameterSchema{
				"id":          {Type: "string", Description: "待变更为新状态的步骤唯一 ID"},
				"status":      {Type: "string", Enum: []string{"pending", "in_progress", "completed"}},
				"explanation": {Type: "string", Description: "说明本次状态转移的原因（可选）"},
			},
		},
		Traits: tool.ToolTraits{
			Exposure:        tool.ToolExposureDirect,
			ReadOnly:        false,
			ConcurrencySafe: false,
			NeedsPermission: true,
		},
	}
}

func (t *TodoUpdateStepTool) Execute(ctx context.Context, params string) (string, error) {
	var args struct {
		ID          string           `json:"id"`
		Status      model.StepStatus `json:"status"`
		Explanation string           `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return "", err
	}

	sessionID, _ := contextutil.GetSessionID(ctx)
	// 调用服务执行 Delta 状态滚动
	_, err := t.planSvc.UpdateStepStatus(ctx, sessionID, args.ID, args.Status, args.Explanation, "agent")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("步骤 [%s] 状态已成功更新为 %s", args.ID, args.Status), nil
}
```

---

## 9. 极致美学与前端展现 (Rich Aesthetics)

TodoList 在用户侧的表现是决定产品体验高级感（Premium Feeling）的关键。

### 9.1 CLI TUI 的树状美化渲染
在控制台交互中，CLI 会在 AI 动作发生时捕获 `todo_write` 事件，打印出格式化树形：

```text
 ⚙️  [10:24:05] Agent 规划了新路径:
 ├── [✔] 1. 分析目标工程目录结构 (Completed)
 └── [⏵] 2. 编写 internal/capabilities/plan 领域契约 (In Progress)
     └── 💡 备注: 需要严格隔离 CLI/Enterprise
 └── [ ] 3. 集成 todo_write 触发机制 (Pending)
```
- **色彩与字形策略**：
  - `✔` 和 `Completed` 采用 **淡翠绿 (Muted Emerald)** `#10B981`
  - `⏵` 和 `In Progress` 采用 **极光青 (Aurora Cyan)** `#06B6D4` 并加粗
  - `[ ]` 和 `Pending` 采用 **烟尘灰 (Ash Gray)** `#6B7280`

### 9.2 Desktop / Enterprise Web 的玻璃态卡片与微动效 [Phase 2 暂缓开发]
在 Web 端（React / Next.js）和桌面端（Wails / Svelte/React），通过毛玻璃质感组件来展示。

#### A. 核心设计 CSS 样式定义
```css
/* 渐变磨砂背景 */
.todo-panel-container {
  background: radial-gradient(circle at top left, rgba(79, 70, 229, 0.05), transparent),
              rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(20px);
  border: 1px solid rgba(255, 255, 255, 0.08);
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.3);
  border-radius: 20px;
}

/* 进行中任务的霓虹发光动效 */
.task-item-active {
  border-left: 4px solid #00f2fe;
  background: linear-gradient(90deg, rgba(0, 242, 254, 0.06) 0%, transparent 100%);
  animation: glowPulse 2.5s infinite alternate ease-in-out;
}

@keyframes glowPulse {
  0% { box-shadow: inset 4px 0 10px rgba(0, 242, 254, 0.1); border-color: #00f2fe; }
  100% { box-shadow: inset 4px 0 20px rgba(0, 242, 254, 0.3); border-color: #4f46e5; }
}

/* 进度条炫彩渐变 */
.progress-bar-fill {
  background: linear-gradient(90deg, #4f46e5 0%, #06b6d4 50%, #10b981 100%);
  transition: width 0.4s cubic-bezier(0.25, 0.8, 0.25, 1);
}
```

#### B. 核心微动效 (Micro-animations)
1. **状态切换弹性动效 (Elastic state change)**：
   勾选框在被按下或 AI 标记完成时，执行 `scale(0.85)` 到 `scale(1)` 的过渡，带有 `cubic-bezier(0.175, 0.885, 0.32, 1.275)` 弹性物理感。
2. **文本淡出与划掉 (Strikethrough wipe-out)**：
   当状态变为 Completed，划掉线条使用 CSS `background-size` 属性做从左到右滑过的遮罩效果，字体颜色同时在 300ms 内变淡，给用户极佳的“完成任务获得感”。
3. **列表卡片位置调整 FLIP 动画**：
   若 AI 调整了 Todo 列表顺序，底层组件基于虚拟 DOM 库 of Layout Transition 保持平滑的位置移位过渡（而非突兀重绘闪烁）。

---

## 10. 渐进式集成与验证计划

### 10.1 自动化测试验证
- **单元测试**：
  编写 `internal/capabilities/plan/service/service_test.go`。覆盖 `in_progress` 校验、人机协同合并冲突处理、防幻觉 ID 自动对齐稳定测试、以及被动 Reminder 在停滞和多步骤下的生成验证。
- **并发与存储测试**：
  在 `shared/local/plan/file_repo_test.go` 中，模拟多个 Agent 并发读写各自 session file，确保无死锁、无文件冲突。

### 10.2 手动集成验证
1. 启动 `cmd/genesis-cli`。
2. 构造一个需要 3 步才能完成的任务，要求 AI 进行拆解并监控终端输出的树状图。
3. 手动修改 `_plan.json` 触发异常，查看校验提示是否友好。

---

## 11. 通用场景适配与标准系统提示词规范

### 11.1 场景通用性设计：非编码场景的自然适配
本方案所设计的 `Plan` 与 `Goal` 数据结构纯粹基于**元描述（Title/Status/Priority/Notes）**定义，不与任何特定代码库、Git 分支或文件系统 API 深度绑定。这使它天然成为跨垂直业务的通用任务调度工具：

* **业务流程办理 (如：社保公积金跨省转移)**：
  * `Goal` = `"为用户办理公积金跨省转移"`
  * `Steps` = `[提取个人电子凭证(Completed), 向转入地提交申请表(InProgress), 财务对账打款(Pending), 短信通知用户(Pending)]`
* **智能运维与故障自愈 (如：CPU 异常暴涨排查)**：
  * `Goal` = `"诊断生产环境 8080 端口 CPU 暴涨并恢复服务"`
  * `Steps` = `[抓取 jstack 线程栈(Completed), 分析 Top 慢 SQL 占用(InProgress), 下发临时阻断限流规则(Pending), 重启实例并回归(Pending)]`

---

### 11.2 标准系统提示词规范 (System Prompt Specification)

为了保证大模型在运行中能够自觉、规范、闭环地使用待办计划体系，必须在 Agent 运行时初始化时注入以下标准系统提示词（System Prompt Rules）。

本提示词规范深度融合并对齐了参考项目中的真实系统指令设定：
* **Kode-CLI 全局任务规范源码**：[packages/core/src/constants/prompts.ts:444](file:///D:/workspace/go/go-project/Kode-CLI/packages/core/src/constants/prompts.ts#L444)
* **Codex 全局规划模式规范源码**：[codex-rs/protocol/src/prompts/base_instructions/default.md:267](file:///D:/workspace/go/go-project/codex/codex-rs/protocol/src/prompts/base_instructions/default.md#L267)
* **Codex 免重复打印渲染约束源码**：[codex-rs/protocol/src/prompts/base_instructions/default.md:58](file:///D:/workspace/go/go-project/codex/codex-rs/protocol/src/prompts/base_instructions/default.md#L58)

#### 提示词模板 (可作为平台标准配置加载)
```text
# 任务规整与待办清单纪律 (Task Discipline & TodoList Rules)

当您处理用户交付的、需要多个步骤才能完成的复杂任务（如：系统重构、多文件分析、复杂业务办理、跨流程排查等）时，您必须严格遵守以下执行纪律：

1. 规划优先原则 (Planning First):
   在您执行任何会产生副作用的工具（如写文件、改动数据库、发送外部网络请求、执行命令等）之前，您必须首先调用 `todo_write` 工具。将您构想的战术路径拆解为可量化的结构化步骤，并予以提交。

2. 步骤精炼原则 (Step Brevity):
   当您使用 `todo_write` 创建步骤时，请保持每个步骤描述极度精炼（每个步骤为一句简单句，字数控制在 5 - 10 个词以内），禁止在步骤描述中撰写冗长段落。

3. 串行聚焦原则 (Focus Discipline):
   - 在您的待办步骤列表中，同一时刻仅允许有 1 个步骤的状态为 `in_progress`。
   - 您的精力和工具调用必须百分之百聚焦在当前 `in_progress` 的子任务上。在当前子任务完成或被阻断（Blocked）前，禁止擅自跳转去执行其他 Pending 步骤。

4. 实时同步原则 (Progress Synchronization):
   - 当您完成当前 `in_progress` 的步骤后，必须首先调用 `todo_write` 工具，将该步骤状态更新为 `completed`，再将下一个步骤更新为 `in_progress`，以确保前台进度状态能够实时滚动。
   - 在整个任务生命周期中，不要堆积完成状态。

5. 界面免复述原则 (UI Render Suppression):
   - 当您调用 `todo_write` 后，系统前端（TUI 渲染层 / 网页面板）会自动截获事件并在用户屏幕上实时渲染更新后的 Todo 列表。
   - 因此，您**绝对不能在您给用户的文本回复中，使用 Markdown 格式重新手动复述、背诵或重复打印整个待办清单列表**！这会导致用户界面发生刷屏。在调用工具后，您只需在正文中用 1-2 句话客观总结刚才的进度变更及接下来的行动即可。

6. 备注说明原则 (Context Enrichment):
   在更新步骤时，请在 `steps.notes` 字段中详细记录当前步骤的重点依赖、潜在风险，这有利于在出现并发或回滚时，帮助协同方（人或其他 Agent）快速理解上下文。

7. 提醒不可复述原则 (Anti-Leakage Restriction):
   如果在执行过程中，系统向您发出了“【系统提醒】当前任务进度跟进...”的被动待办提示，请立即在您内部调整任务调度，但您**绝不能在回复给用户的最终文本中直接复述、背诵或提及任何关于系统提醒字样或该提醒的原始 Prompt 文本**。保持人机交互的纯净与高级感。
```
