# Agent Loop 设计：通用事件驱动 Agent Runtime

> 目标：设计一个**通用、可扩展、生产级**的 Agent Runtime。  
> 适合多种业务场景（RAG 问答、代码 Agent、业务办理、监控告警等），  
> 支持外部系统方便接入，内置完整的扩展点机制。  
> 兼容将来的多 Agent 协作、长期执行、@Agent 协作空间等模式。

---

## 目录

1. [设计原则](#1-设计原则)
2. [整体架构](#2-整体架构)
3. [核心概念定义](#3-核心概念定义)
   - 3.0 审计字段与 DB 映射约定
   - 3.8 RunContext：Loop 内部状态（新增）
4. [Run Engine：核心执行引擎](#4-run-engine核心执行引擎)
   - 4.6 流式输出协议（SSE 事件规范）
     - 4.6.1 设计原则
     - 4.6.2 SSE 基础格式与公共字段
     - 4.6.3 Content Block 模型
     - 4.6.4 Block 类型全表
     - 4.6.5 完整事件类型规范
     - 4.6.6 典型执行序列示例
     - 4.6.7 工具授权 / 干预流程序列
     - 4.6.8 富文本输出（A2UI Widget）
     - 4.6.9 断连续流机制
     - 4.6.10 前端响应授权 / 干预
     - 4.6.11 Stream Profile（面向不同消费端）
     - 4.6.12 自定义重试配置（RetryPolicy）
   - 4.7 错误分类与重试策略
   - 4.8 Context Window 管理策略
5. [Agentic RAG：自主检索循环](#5-agentic-rag自主检索循环)
6. [Tool Gateway：工具执行与授权](#6-tool-gateway工具执行与授权)
   - 6.4 动态权限检查（PermissionChecker）
   - 6.5 Human Intervention：五种人工干预类型
   - 6.6 工具沙盒
   - 6.7 外部系统注册工具
7. [Skill Runtime：技能驱动执行](#7-skill-runtime技能驱动执行)
8. [扩展点设计：插件式架构](#8-扩展点设计插件式架构)
9. [Trace / Observability：标准可观测性](#9-trace--observability标准可观测性)
10. [Token 消耗追踪](#10-token-消耗追踪)
11. [记忆系统（Memory System）](#11-记忆系统memory-system)
    - 11.1 四种记忆类型对比
    - 11.2 工作记忆（Working Memory）
    - 11.3 短期记忆（ShortTermMemory）
    - 11.4 长期记忆（LongTermMemory）
    - 11.5 注入式上下文（StaticContextProvider）
    - 11.6 MemoryConsolidator：记忆整合器
    - 11.7 记忆注入顺序
    - 11.8 用户画像（UserProfile）
    - 11.9 记忆更新、衰退与用户可编辑
    - 11.10 从记忆沉淀为 Skill Candidate
12. [外部系统接入与授权](#12-外部系统接入与授权)
13. [数据模型](#13-数据模型)
14. [语言实现参考](#14-语言实现参考)
15. [分期实施计划](#15-分期实施计划)
16. [Redis 使用策略与最佳实践](#16-redis-使用策略与最佳实践)
    - 16.1 整体判断原则
    - 16.2 各层 Redis 必要性分析（Session缓存 / SSE分发 / 分布式锁 / 任务队列 / 限流 / 心跳 / 幂等）
    - 16.3 分期落地建议
    - 16.4 引入后整体架构
    - 16.5 Key 命名规范
    - 16.6 无 Redis 时的降级方案

> **关于语言选型**：本文档的架构设计、接口定义、数据模型均与语言无关。  
> 第 14 章给出 **Go（Eino）** 和 **Python（LangGraph / LangChain）** 两种实现参考，  
> 读者可根据项目情况选择任一语言，核心设计不变。

---

## 1. 设计原则

### 原则一：Run Engine 是内核，Loop 只是一种策略

Loop 不等于 Agent 产品，真正的内核是 Run Engine。  
Loop / Plan-Execute / ReAct 只是 Run Engine 支持的不同运行策略（Strategy Pattern）。

### 原则二：扩展点优先，硬编码最少

所有可能因业务不同而变化的部分，都定义为接口：

```
工具 → ToolProvider 接口
技能 → SkillProvider 接口
记忆 → MemoryStore 接口
提示词 → PromptBuilder 接口
日志 → Logger 接口
Trace → Tracer 接口（兼容 OpenTelemetry）
Token 计量 → UsageMeter 接口
授权 → AuthorizationProvider 接口
```

### 原则三：所有触发走 Event，所有等待可恢复

Run Engine 不阻塞线程等待，任何需要等待的动作（审批、用户输入、子 Agent）都通过 Event 恢复执行。

### 原则四：外部系统是一等公民

任何外部系统（CRM、工单系统、ERP、其他 AI 服务）都可以通过标准接口触发、订阅、授权、查询 Agent。

### 原则五：租户隔离贯穿所有层

所有数据库操作强制携带 `tenant_id`，所有 API 鉴权在 Gateway 层完成。

---

## 2. 整体架构

```text
┌─────────────────────────────────────────────────────────┐
│  接入层 Channel / Gateway                                 │
│  HTTP API · WebSocket · Webhook · CLI · 飞书 · 其他系统   │
└───────────────────────────┬─────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│  协作层 Workspace / MessageBus                           │
│  @Agent 路由 · 消息分发 · 协作空间（二期）               │
└───────────────────────────┬─────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│  调度层 Task / Event / Scheduler                         │
│  Task Queue · Event Bus · 定时任务 · 状态管理            │
└───────────────────────────┬─────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│  执行层 Agent Run Engine（核心）                         │
│  Strategy: ReAct · Plan-Execute · Loop · Reflection      │
│  Planner · Action Executor · Evaluator · Replanner       │
└──────────┬────────────────┬────────────────┬────────────┘
           ↓                ↓                ↓
┌──────────────┐  ┌─────────────────┐  ┌────────────────┐
│ Tool Gateway │  │ Agentic RAG Exec│  │  Agent Protocol│
│ 工具执行授权 │  │ 并行检索·多轮决策│  │  子 Agent 调用 │
└──────────────┘  └─────────────────┘  └────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│  治理层 Governance                                       │
│  Permission · Approval · Watcher · Sandbox · Audit       │
└─────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│  基础设施 Infrastructure                                  │
│  Memory · Trace(OTel) · Token Meter · Prompt Builder     │
│  Logger · Config · Secret · DB · Cache                   │
└─────────────────────────────────────────────────────────┘
```

---

## 3. 核心概念定义

### 3.0 审计字段与 DB 映射约定

领域模型与第 13 章 SQL 表**一一对应**。审计字段通过嵌入结构体复用，Repository 层负责列映射；`RunContext` 等纯内存对象不包含审计字段。

```go
// ResourceAudit 资源表标准审计字段
// 用于：agent / agent_instance / agent_session / agent_task / agent_webhook
type ResourceAudit struct {
    OwnerID   string    // 当前拥有者（可转让，权限控制）
    OwnerName string    // 拥有者名称冗余，避免 JOIN
    CreatedBy string    // 最初创建人 user_id，不可变
    UpdatedBy string    // 最后修改人 user_id
    CreatedAt time.Time
    UpdatedAt time.Time
}

// RuntimeAudit 运行时流水表标准审计字段
// 用于：agent_run / agent_step / agent_intervention / agent_memory / agent_usage
type RuntimeAudit struct {
    OwnerID   string    // 继承自父实体，用于按用户过滤与报表
    OwnerName string    // 仅 run / usage 等报表表使用，其他表可为空
    CreatedAt time.Time
    UpdatedAt time.Time // event / message 等只写表在 DB 无此列，领域层为零值
}
```

| 实体 | 嵌入审计类型 | 说明 |
|------|-------------|------|
| Agent / AgentInstance / Session / Task | `ResourceAudit` | 与资源表 SQL 完全对齐 |
| Run / Step / Intervention / Memory | `RuntimeAudit` | 与流水表 SQL 对齐 |
| Event / Message | `OwnerID` + `CreatedAt` | 只写表，无 `updated_at` |
| RunContext | 无 | 不持久化 |

> `owner_id` vs `created_by`：创建时相同；所有权转让后 `OwnerID` 变更，`CreatedBy` 不变。  
> Task 的 `CreatedByID` + `CreatedByType` 表示发起者类型（user/agent/scheduler），与 `ResourceAudit.CreatedBy`（user_id）并存，语义不同。

### 3.1 Agent（配置模板）

Agent 是一种配置模板，定义角色和能力边界，**不持有运行状态**：

```go
type Agent struct {
    ID              string
    TenantID        string
    Name            string
    Description     string
    Type            AgentType       // react_loop / plan_execute / skill_based / coding / rag
    DefaultModel    string
    SystemPrompt    string
    Tools           []ToolRef       // 允许使用的工具列表
    Skills          []SkillRef      // 绑定的技能列表
    RAGConfig       RAGConfig       // 知识库配置
    MCPConfig       MCPConfig       // MCP 配置
    PermissionPolicy PermissionPolicy
    RuntimePolicy   RuntimePolicy   // 最大迭代、Token 预算、超时等
    MemoryConfig    MemoryConfig    // 记忆范围配置
    OutputSchema    *Schema         // 结构化输出约束
    Status          string          // active / inactive
    ResourceAudit                   // owner / created_by / updated_by / timestamps
}
```

### 3.2 AgentInstance（运行实例）

同一个 Agent 在不同 Workspace 或用户空间里的具体成员，拥有独立的记忆和状态：

```go
type AgentInstance struct {
    ID            string
    TenantID      string
    AgentID       string
    WorkspaceID   string      // 所属协作空间
    Name          string
    Role          string
    Status        InstanceStatus
    MemoryScope   string      // 记忆隔离范围
    WorkspacePath string      // 文件工作目录（代码 Agent 使用）
    PermissionScope []string  // 实例级权限覆盖
    ResourceAudit               // owner / created_by / updated_by / timestamps
}
```

### 3.3 Session（对话上下文）

一次对话或任务的上下文窗口，一个 Session 可包含多个 Run：

```go
type Session struct {
    ID              string
    TenantID        string
    AgentInstanceID string
    UserID          string      // 发起会话的用户，通常等于 ResourceAudit.OwnerID
    WorkspaceID     string
    Title           string
    Status          SessionStatus
    ContextWindow   []Message    // 当前上下文消息列表（运行时加载，非 DB 列）
    ResourceAudit                // owner / created_by / updated_by / timestamps
}
```

### 3.4 Run（一次执行）

Agent 的一次自主执行过程，可能因审批、等待子 Agent 而暂停后恢复：

```go
type Run struct {
    ID              string
    TenantID        string
    TaskID          string
    SessionID       string
    AgentInstanceID string
    RuntimeMode     RuntimeMode   // react_loop / plan_execute / agentic_rag / coding_loop
    Status          RunStatus     // created / running / paused / waiting_approval / waiting_event / completed / failed / cancelled
    PauseReason     string
    TotalTokens     int64
    TotalCost       float64
    StartedAt       *time.Time    // 业务执行开始时间
    FinishedAt      *time.Time    // 业务执行结束时间
    Steps           []Step        // 运行时加载，非 DB 列
    RuntimeAudit                  // owner / owner_name / created_at / updated_at
}
```

### 3.5 Step（执行步骤）

Run 内部的每一步动作，是 Trace 和恢复的最小单位：

```go
type Step struct {
    ID            string
    TenantID      string
    RunID         string
    StepIndex     int
    ActionType    ActionType    // think / rag_search / tool_call / mcp_call / agent_call / approval_request / final_answer
    ActionPayload json.RawJSON
    Observation   json.RawJSON
    TokenUsage    TokenUsage
    Status        StepStatus
    StartedAt     *time.Time
    FinishedAt    *time.Time
    RuntimeAudit                // owner_id 冗余自 run，created_at / updated_at
}
```

### 3.6 Task（业务任务）

要完成的业务目标，可以等待、暂停、重试、派发给子 Agent：

```go
type Task struct {
    ID              string
    TenantID        string
    WorkspaceID     string
    SessionID       string
    ParentTaskID    string      // 子 Task 支持树形结构
    AssigneeID      string      // AgentInstance ID
    CreatedByType   string      // user / agent / scheduler / webhook
    CreatedByID     string      // 发起者 ID（含 agent/scheduler 类型区分）
    TriggerEventID  string
    Title           string
    Payload         json.RawJSON
    Status          TaskStatus  // pending / running / waiting_approval / waiting_event / completed / failed / cancelled
    Priority        int
    Deadline        *time.Time
    RetryCount      int
    MaxRetry        int
    ResourceAudit                 // owner / created_by / updated_by / timestamps
}
```

### 3.7 Event（触发事件）

所有触发动作都通过 Event 统一路由，支持恢复等待中的 Run：

```go
type Event struct {
    ID          string
    TenantID    string
    WorkspaceID string
    EventType   EventType     // user_message / approval_granted / approval_rejected / webhook / scheduler / agent_completed / file_uploaded
    SourceType  string
    SourceID    string
    Payload     json.RawJSON
    Status      EventStatus   // pending / processing / processed / failed
    OwnerID     string        // 事件触发者；系统/调度触发时为 empty
    CreatedAt   time.Time     // 只写表，无 UpdatedAt
}
```

### 3.8 RunContext（Loop 执行期间的内存状态）

RunContext 是 Loop 每轮执行时的**内存对象，不持久化**，**不含 §3.0 审计字段**。  
它是 ContextBuilder / ActionExecutor / Evaluator 之间共享的上下文载体。  
Run 启动时创建，Loop 结束或 Resume 时根据 DB 中的 Steps 重建。

```text
RunContext（只存内存，Loop 结束后释放）

  # 基础信息
  run_id, tenant_id
  agent             # 已加载的 Agent 配置
  agent_instance    # 已加载的 AgentInstance
  session           # 当前 Session

  # Loop 执行状态
  iteration         # 当前迭代轮次（从 0 开始）
  steps_so_far      # 已完成的 Step 列表（含 action + observation）
  current_plan      # Plan-Execute 模式下的当前计划步骤列表
  plan_step_index   # 当前执行到第几个 Plan Step

  # 工作记忆（临时变量，Loop 内跨步骤共享）
  working_memory    # key-value，存 tool 中间结果、RAG 检索结果、LLM 判断结论
  retrieved_chunks  # 本次 Run 已检索到的 RAG 结果（去重后缓存，避免重复检索）
  injected_memories # Run 启动时注入的长期记忆（缓存，不重复查）

  # 资源追踪
  token_used        # 已消耗 Token（实时累加）
  cost_used         # 已产生费用
  tool_call_count   # 已调用工具次数

  # Skill 状态
  active_skill      # 当前激活的 Skill
  skill_state       # Skill 内部状态（由 SkillHook 维护）

  # Human Intervention 状态
  pending_intervention  # 当前挂起的人工干预请求（approval / user_input 等）
  step_by_step_mode     # 是否开启逐步确认模式
```

**Resume 时的 RunContext 重建：**

```text
Resume(run_id, event):
  1. 从 agent_step 表加载该 Run 的所有历史 Steps，重建 steps_so_far
  2. 从 agent_usage 表重建 token_used / cost_used
  3. 将 ResumeEvent（如 approval_result / user_input_text）
     注入为最后一个 waiting Step 的 Observation
  4. 标记该 Step 为 completed，从下一轮 Loop 继续
```

---

## 4. Run Engine：核心执行引擎

### 4.1 Run Engine 接口

**接口语义（与语言无关）：**

```text
RunEngine
  start(request)     → Run           # 创建并启动一次 Run
  resume(run_id, event)              # 从暂停状态恢复（审批通过、用户输入等）
  pause(run_id, reason)              # 暂停
  cancel(run_id)                     # 取消
  get_state(run_id)  → RunState      # 查询当前状态

StartRunRequest
  tenant_id, agent_instance_id, session_id, task_id
  user_input, attachments
  runtime_mode       # 不传则由 Agent 配置决定
  caller_info        # 外部系统调用时携带（来源标识、授权信息）
```

**Go 实现：**

```go
type RunEngine interface {
    Start(ctx context.Context, req StartRunRequest) (*Run, error)
    Resume(ctx context.Context, runID string, event ResumeEvent) error
    Pause(ctx context.Context, runID string, reason string) error
    Cancel(ctx context.Context, runID string) error
    GetState(ctx context.Context, runID string) (*RunState, error)
}
```

**Python 实现：**

```python
from typing import Protocol

class RunEngine(Protocol):
    async def start(self, req: StartRunRequest) -> Run: ...
    async def resume(self, run_id: str, event: ResumeEvent) -> None: ...
    async def pause(self, run_id: str, reason: str) -> None: ...
    async def cancel(self, run_id: str) -> None: ...
    async def get_state(self, run_id: str) -> RunState: ...

@dataclass
class StartRunRequest:
    tenant_id: str
    agent_instance_id: str
    session_id: str
    task_id: str | None = None
    user_input: str = ""
    attachments: list[Attachment] = field(default_factory=list)
    runtime_mode: RuntimeMode | None = None
    caller_info: CallerInfo | None = None
```

### 4.2 ReAct Loop 执行流程

```text
┌─────────────────────────────────────────────────────────┐
│ 创建 Run                                                  │
│  - 加载 AgentInstance + Agent Profile                     │
│  - 构建初始上下文（Session History + Memory + Skill 注入） │
│  - 初始化 StepRecorder / Tracer / UsageMeter              │
└─────────────────────────┬───────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│ LOOP（每轮）                                              │
│                                                          │
│  1. ContextBuilder：拼装 messages（含上轮 Observation）   │
│  2. LLM 推理：输出 Think + Action（或 Final Answer）      │
│  3. Skill Evaluator 钩子（检查 Skill 完成/分支条件）      │
│  4. Policy Guard 检查（权限、预算、迭代上限）             │
│  5. ActionExecutor 分派：                                 │
│     - tool_call     → Tool Gateway                        │
│     - rag_search    → Agentic RAG Executor                │
│     - agent_call    → Agent Protocol                      │
│     - mcp_call      → MCP Gateway                        │
│     - final_answer  → 结束 Loop                          │
│  6. 记录 Step（含 Token 用量）                            │
│  7. 检查停止条件（max_iter / max_tokens / timeout）       │
│                                                          │
└─────────────────────────┬───────────────────────────────┘
                          ↓
             完成 / 暂停等审批 / 错误 / 超限停止
```

### 4.3 Run Engine 内部结构

所有依赖均为接口，通过依赖注入组装，Go 和 Python 结构对称：

```text
RunEngine 依赖组件（语言无关）：
  context_builder   # 构建 LLM 上下文（历史 + 记忆 + 提示词）
  planner           # Plan-Execute 模式下的规划器
  action_executor   # 动作分派执行器（按 action_type 路由）
  evaluator         # 判断 Run 是否完成/继续
  replanner         # Plan-Execute 下的重新规划
  stop_controller   # 边界控制（迭代/Token/时间）
  state_persister   # Run 状态持久化
  trace_recorder    # 步骤 Trace 记录
  usage_meter       # Token 计量
  skill_hooks       # Skill 钩子列表（评估、注入）
  policy_guard      # 权限/策略检查
  memory_store      # 长期记忆读写
  prompt_builder    # 系统提示词构建
```

**Go：**

```go
type runEngine struct {
    contextBuilder  ContextBuilder
    planner         Planner
    actionExecutor  ActionExecutor
    evaluator       RunEvaluator
    replanner       Replanner
    stopController  StopController
    statePersister  StatePersister
    traceRecorder   TraceRecorder
    usageMeter      UsageMeter
    skillHooks      []SkillHook
    policyGuard     PolicyGuard
    memoryStore     MemoryStore
    promptBuilder   PromptBuilder
}
```

**Python：**

```python
@dataclass
class RunEngineImpl:
    context_builder: ContextBuilder
    planner: Planner
    action_executor: ActionExecutor
    evaluator: RunEvaluator
    replanner: Replanner
    stop_controller: StopController
    state_persister: StatePersister
    trace_recorder: TraceRecorder
    usage_meter: UsageMeter
    skill_hooks: list[SkillHook]
    policy_guard: PolicyGuard
    memory_store: MemoryStore
    prompt_builder: PromptBuilder
```

### 4.4 运行策略（Strategy Pattern）

```go
// RunStrategy 决定 Loop 的执行方式
type RunStrategy interface {
    Execute(ctx context.Context, runCtx *RunContext) (*RunResult, error)
}

// 已实现的策略
type ReactLoopStrategy struct{}       // 标准 ReAct 循环
type PlanExecuteStrategy struct{}     // 先规划后执行
type AgenticRAGStrategy struct{}      // 纯 RAG 自主检索
type CodingLoopStrategy struct{}      // 代码开发专用循环
type ReflectionStrategy struct{}      // 带自我反思的循环
```

### 4.5 停止条件（受控边界）

```go
type RuntimePolicy struct {
    MaxIterations    int           // 最大 Loop 轮数，默认 20
    MaxToolCalls     int           // 最大工具调用次数
    MaxRAGRounds     int           // 最大 RAG 检索轮数
    MaxTokens        int64         // Token 预算上限
    MaxDuration      time.Duration // 最长执行时间
    MaxCost          float64       // 最大费用（USD）
    MaxConsecutiveFail int         // 连续失败次数上限
    StopConditions   []string      // 业务级停止条件（由 Evaluator 解析）
}
```

### 4.6 流式输出协议（SSE 事件规范）

参考：docs\agent loop设计-SSE与重试策略设计.md

### 4.7 错误分类与重试策略

Loop 中的错误分四类，处理方式不同：

```text
错误类型              触发条件                         处理策略
────────────────────────────────────────────────────────────────────────
retriable_error       LLM API 限流 / 网络超时          遵循 RetryPolicy 配置（§4.6.12）
                      429 / 502 / 503 响应             默认：指数退避，最多 3 次，失败后 error

tool_error            工具调用失败（业务错误）           遵循 Tool 的 RetryPolicy；
                      外部 API 返回非 2xx              重试耗尽后依 on_final_failure 处理
                      沙盒执行异常                      fallback_text 模式不中断 Run

fatal_error           鉴权失败 / 权限拒绝              立即停止 Run，status=failed
                      预算/迭代超限                     记录终止原因

partial_complete      超过 max_iterations              以当前最佳结果结束
                      超过 max_tokens / max_duration   status=completed，result.incomplete=true
                                                       前端标注"结果可能不完整"
```

**LLM API 错误重试伪代码（遵循 RetryPolicy）：**

```text
policy = resolve_retry_policy(target="llm", run_overrides=run.retry_policy_overrides)
for attempt in range(1, policy.max_retries + 2):  # +2 = 初始执行 + 重试
    try:
        response = await llm.chat(messages)
        return response
    except (RateLimitError, NetworkError) as e:
        if attempt > policy.max_retries:
            # 重试耗尽，根据 on_final_failure 决定行为
            if policy.on_final_failure.action == "fallback_text":
                return FallbackResult(text=policy.on_final_failure.fallback_text)
            elif policy.on_final_failure.action == "intervention":
                raise InterventionRequired(...)
            else:
                raise FatalError(f"LLM API 持续失败，已重试 {policy.max_retries} 次")
        wait_ms = calc_backoff(policy, attempt)
        emit(error.retriable, retry_attempt=attempt, retry_after_ms=wait_ms)
        await sleep(wait_ms / 1000)
```

---

### 4.8 Context Window 管理策略

**问题：** 长会话（50+ 轮）消息累积会超过 LLM 上下文窗口（如 128K tokens）。  
**解决：** ContextBuilder 在拼装 messages 时做裁剪，策略由 Agent 配置决定。

```text
Strategy: sliding_window（默认）
  保留最近 N 条消息（N 由 context_window_size 配置，默认 50）
  超出部分丢弃，但 System Prompt + 第一条 Human Message 始终保留

Strategy: summary（推荐长会话）
  当 messages token 数超过阈值（如 80K）时：
    调用 LLM 对前 M 条消息生成摘要（约 500 token）
    将摘要注入 system prompt："以下是本次对话历史摘要：..."
    丢弃被摘要的原始消息
    摘要存入 ShortTermMemory，下次 Run 可复用

Strategy: selective（RAG Agent 推荐）
  始终保留 System Prompt + 最近 10 条消息
  对更早的历史做语义检索，选取与当前 query 最相关的 K 条补充进上下文
```

**ContextBuilder 拼装顺序：**

```text
messages 构成（按顺序拼装）:
  [1] system        = PromptBuilder 生成（角色 + Skill 指令 + 静态注入上下文）
  [2] memory_inject = LongTermMemory 检索结果（Run 启动时检索一次，缓存在 RunContext）
  [3] history       = ShortTermMemory 按策略裁剪后的历史消息
  [4] working_obs   = RunContext.steps_so_far 的 Observation（当前 Run 内的观察结果）
  [5] user          = 当前用户输入
```

---

## 5. Agentic RAG：自主检索循环

### 5.1 设计目标

- LLM 自己分析任务，**提取关键词列表**
- 多个关键词**并行检索**（goroutine fan-out）
- 根据检索结果评估覆盖度，**决定是否换词再检索**
- 最多 N 轮（受 `max_rag_rounds` 控制）
- 结果注入到 Loop 上下文

### 5.2 执行流程

```text
接收 RAG 搜索 Action
  ↓
① LLM 提取关键词列表
  例：["施工质量验收规范", "混凝土浇筑标准", "GB/T 50204"]
  ↓
② 并行检索（goroutine fan-out）
  ┌─────────┬──────────┬──────────────────┐
  │ kw1 检索 │ kw2 检索  │    kw3 检索       │
  └────┬────┴────┬─────┴─────────┬────────┘
       ↓         ↓               ↓
      结果1     结果2            结果3
  ↓
③ 汇总去重，计算覆盖度分数
  ↓
④ 覆盖度评估（LLM 或规则）
  ├── 足够 → 返回检索结果给 Loop
  └── 不够 → 分析缺口，生成新关键词 → 回到②（受 max_rag_rounds 控制）
  ↓
⑤ 超过轮数限制 → 返回已有结果 + 标注"不完整"
```

### 5.3 接口定义

```go
// AgenticRAGExecutor 负责自主 RAG 检索
type AgenticRAGExecutor interface {
    Execute(ctx context.Context, req RAGSearchRequest) (*RAGSearchResult, error)
}

type RAGSearchRequest struct {
    RunID       string
    StepID      string
    Query       string          // 原始查询意图
    KnowledgeIDs []string       // 知识库列表
    MaxRounds   int             // 最大检索轮数
    TopK        int             // 每次检索 Top-K
    Strategy    RAGStrategy     // keyword / semantic / hybrid
}

type RAGSearchResult struct {
    Chunks      []RetrievedChunk
    Keywords    []string        // 最终使用的关键词
    Rounds      int             // 实际执行的轮数
    Coverage    float64         // 覆盖度评分（0~1）
    Incomplete  bool            // 是否因轮数限制而不完整
}
```

### 5.4 并行检索实现要点

**Go（goroutine + channel）：**

```go
func (e *agenticRAGExecutor) parallelSearch(
    ctx context.Context,
    keywords []string,
    req RAGSearchRequest,
) []RetrievedChunk {
    type result struct {
        chunks []RetrievedChunk
        err    error
    }
    results := make(chan result, len(keywords))
    for _, kw := range keywords {
        go func(keyword string) {
            chunks, err := e.retriever.Retrieve(ctx, keyword, req.TopK, req.KnowledgeIDs)
            results <- result{chunks, err}
        }(kw)
    }
    var allChunks []RetrievedChunk
    for range keywords {
        r := <-results
        if r.err == nil {
            allChunks = append(allChunks, r.chunks...)
        }
    }
    return deduplicateChunks(allChunks)
}
```

**Python（asyncio.gather，效果等同）：**

```python
async def parallel_search(
    self,
    keywords: list[str],
    req: RAGSearchRequest,
) -> list[RetrievedChunk]:
    tasks = [
        self.retriever.retrieve(kw, req.knowledge_ids, top_k=req.top_k)
        for kw in keywords
    ]
    # asyncio.gather 并发执行所有检索，return_exceptions 避免单个失败影响整体
    results = await asyncio.gather(*tasks, return_exceptions=True)
    all_chunks = [
        chunk
        for result in results
        if not isinstance(result, Exception)
        for chunk in result
    ]
    return deduplicate_chunks(all_chunks)
```

---

## 6. Tool Gateway：工具执行与授权

### 6.1 授权级别

Tool Gateway 支持三种授权级别，配置在 Agent 的 `PermissionPolicy` 里：

```go
type ToolAuthLevel string

const (
    AuthLevelNone     ToolAuthLevel = "none"      // 直接执行，无需授权
    AuthLevelConfirm  ToolAuthLevel = "confirm"   // 需要用户确认后执行
    AuthLevelApproval ToolAuthLevel = "approval"  // 需要管理员审批后执行
)
```

### 6.2 工具执行状态机

```text
Loop 决定调用工具
  ↓
Tool Gateway 检查工具权限策略
  │
  ├── AuthLevel = none
  │     ↓
  │   是否需要沙盒？
  │   ├── 是 → 沙盒执行 → 结果审查 → 返回 Observation
  │   └── 否 → 直接执行 → 返回 Observation
  │
  ├── AuthLevel = confirm
  │     ↓
  │   创建 Approval 记录（risk_level=low）
  │     ↓
  │   Run 状态 → waiting_approval
  │     ↓
  │   通知用户（WebSocket / 飞书 / API 回调）
  │     ↓
  │   用户操作
  │   ├── 确认 → 发布 approval_granted Event → Resume Run → 执行工具
  │   └── 拒绝 → 发布 approval_rejected Event → Resume Run → 跳过/终止
  │
  └── AuthLevel = approval
        ↓
      创建 Approval 记录（risk_level=high）
        ↓
      Run 状态 → waiting_approval
        ↓
      通知审批人（邮件/IM/平台通知）
        ↓
      审批人操作
      ├── 批准 → 发布 approval_granted Event → Resume Run → 执行工具
      └── 拒绝 → 发布 approval_rejected Event → Resume Run → LLM 重新规划
```

### 6.3 ToolProvider 接口（扩展点）

**接口语义：**

```text
ToolProvider（所有工具实现，支持运行时热注册）
  definition()         → ToolDefinition   # LLM Function Calling schema
  execute(params)      → ToolResult       # 执行工具
  auth_level(params)   → AuthLevel        # 支持基于参数动态判断风险级别
  requires_sandbox()   → bool             # 是否需要沙盒隔离

ToolRegistry
  register(tool)
  unregister(tool_name)
  get(tool_name)       → ToolProvider
  list(tenant_id)      → list[ToolDefinition]
```

**Go：**

```go
type ToolProvider interface {
    Definition() ToolDefinition
    Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error)
    AuthLevel(params json.RawMessage) ToolAuthLevel
    RequiresSandbox() bool
}

type ToolRegistry interface {
    Register(tool ToolProvider) error
    Unregister(toolName string) error
    Get(toolName string) (ToolProvider, error)
    List(tenantID string) []ToolDefinition
}
```

**Python：**

```python
class ToolProvider(Protocol):
    def definition(self) -> ToolDefinition: ...
    async def execute(self, params: dict) -> ToolResult: ...
    def auth_level(self, params: dict) -> AuthLevel: ...
    def requires_sandbox(self) -> bool: ...

class ToolRegistry(Protocol):
    def register(self, tool: ToolProvider) -> None: ...
    def unregister(self, tool_name: str) -> None: ...
    def get(self, tool_name: str) -> ToolProvider: ...
    def list(self, tenant_id: str) -> list[ToolDefinition]: ...
```

### 6.4 动态权限检查（PermissionChecker）

**设计升级**：`auth_level()` 返回静态枚举无法支持"调用外部权限 API"的场景。  
改为 `PermissionChecker` 接口，支持多种实现可链式组合。

**接口定义（语言无关）：**

```text
PermissionChecker
  check(req: PermissionCheckRequest) → PermissionResult

PermissionCheckRequest
  tenant_id, user_id, agent_id
  tool_name, params          # 工具名和参数（用于动态风险评估）
  run_id, step_id            # 追踪上下文
  caller_scopes              # 调用方已有的权限 scope 列表

PermissionResult
  allowed: bool              # 是否允许执行
  requires_confirmation: bool # 允许，但需要用户点击确认（低风险）
  requires_approval: bool     # 允许，但需要管理员审批（高风险）
  requires_sandbox: bool      # 需要在沙盒中执行
  deny_reason: str           # 拒绝原因（展示给用户）
  risk_level: str            # low / medium / high（用于审批界面展示）
  approval_display: dict     # 审批界面展示的额外信息（参数摘要、风险说明等）
  ttl: int                   # 权限缓存时间（秒），0 表示不缓存
```

**内置实现（可组合）：**

```text
StaticRuleChecker       基于 Agent 配置的静态规则（tool_policy 字段）
APIPermissionChecker    调用外部权限系统 API（如企业 IAM / RBAC 服务）
DynamicRiskChecker      基于参数内容评估风险（如"发送对象是否内部邮件"）
CompositeChecker        链式组合多个 Checker（按顺序，任一 deny 则拒绝）
CachedPermissionChecker 包装任意 Checker，添加 Redis 缓存（避免频繁调用外部 API）
```

**APIPermissionChecker 示例（调用外部权限 API）：**

```python
class APIPermissionChecker:
    """调用外部权限系统校验，适合接入企业 IAM"""

    def __init__(self, permission_api_url: str, api_key: str):
        self.url = permission_api_url
        self.api_key = api_key

    async def check(self, req: PermissionCheckRequest) -> PermissionResult:
        resp = await http_post(
            f"{self.url}/check",
            headers={"Authorization": f"Bearer {self.api_key}"},
            json={
                "tenant_id": req.tenant_id,
                "user_id": req.user_id,
                "resource": f"tool:{req.tool_name}",
                "action": "execute",
                "context": {"params": req.params},
            },
            timeout=2.0,  # 权限校验必须快，超时则 fallback 到静态规则
        )
        return PermissionResult(
            allowed=resp["allowed"],
            requires_confirmation=resp.get("requires_confirmation", False),
            risk_level=resp.get("risk_level", "low"),
        )
```

### 6.5 工具沙盒

```text
Sandbox 接口（可接入 gVisor / Firecracker / WASM / Docker 进程隔离）
  execute(req: SandboxRequest) → SandboxResult

SandboxRequest
  runtime: str         # python / node / shell / go
  code: str
  stdin: str
  env: dict
  timeout: duration
  memory_limit: bytes
  network_policy:      # none（无网络）/ allowlist（白名单）/ full

SandboxResult
  stdout, stderr
  exit_code
  execution_time
  memory_used
```

### 6.6 Human Intervention（人工干预）类型

**重要设计**：Human Intervention 不只是"审批"一种，共有五种类型，数据结构和前端交互各不同。

```text
干预类型                触发场景                          用户操作              Run 恢复后行为
──────────────────────────────────────────────────────────────────────────────────────────
tool_approval           工具需要授权执行                   approve / reject      approve → 执行工具
                        PermissionResult.requires_approval                      reject → LLM 重新规划

tool_confirmation       低风险工具需用户确认               confirm / cancel      confirm → 执行工具
                        requires_confirmation=true                               cancel → 跳过，LLM继续

user_input_required     LLM 需要用户澄清问题               提供文本输入           文本作为 Observation 注入
                        LLM 输出 action=ask_user                                Loop 继续下一轮

manual_override         用户强制提供最终答案               提供文本              直接作为 Final Answer 结束 Run
                        用户主动触发（不是 LLM 请求）                            不经过 LLM

step_by_step            用户开启逐步确认模式               每步 continue/stop    continue → 执行下一步
                        调用时传 step_by_step=true                               stop → 暂停等待下次 continue
```

**InterventionRequest 数据模型：**

```text
InterventionRequest
  id                      唯一 ID
  run_id, step_id         关联的执行
  type                    见上表五种类型
  tenant_id, user_id
  tool_name               (tool_approval / tool_confirmation 时有值)
  params                  工具参数（用于展示给用户）
  risk_level              展示风险等级
  display_info            前端展示信息（工具描述、参数摘要、风险说明）
  llm_question            (user_input_required 时有值，LLM 的问题文本)
  status                  pending / resolved / expired / cancelled
  expires_at              超时时间（超时后自动按默认策略处理）
  default_on_timeout      approve / reject / cancel（超时后的自动行为）
  created_at, resolved_at
  resolved_by             user_id（谁处理的）
  resolution              { action: "approve/reject/confirm/...", input: "...", reason: "..." }
```

**超时自动处理（Scheduler 扫描）：**

```text
每 30 秒扫描 status=pending 且 expires_at < now 的 InterventionRequest：
  → 按 default_on_timeout 自动处理
  → 发布对应 Event（如 tool_approval.expired → 视为 reject）
  → Resume 对应 Run
```

### 6.7 外部系统注册工具

外部系统可通过 HTTP API 动态注册工具，无需修改 Runtime 代码：

```http
POST /api/v1/tools/register
Authorization: Bearer {tenant_api_key}

{
  "name": "create_work_order",
  "description": "在工单系统创建一个工单",
  "parameters": { ... },
  "callback_url": "https://your-system.com/tool-execute",
  "permission_check_url": "https://your-system.com/check-permission",
  "requires_sandbox": false,
  "default_on_timeout": "reject",
  "timeout_seconds": 300
}
```

Tool Gateway 调用该工具时：
1. 先调用 `permission_check_url`（如配置）校验权限
2. 根据 PermissionResult 决定直接执行 / 确认 / 审批
3. 审批通过后向 `callback_url` 发 HTTP 请求（同步/异步两种）

---

### 6.8 能力适用范围（Tool / MCP / Skill / Sandbox 通用）

不同接入端、运行环境和安全上下文允许暴露的能力不同。文件系统、代码执行、本地进程、桌面自动化等能力可能适合 CLI / 桌面端，但不适合普通 HTTP API；某些 MCP Server 或 Skill 也可能只允许特定端、特定租户、特定 Agent 使用。因此能力暴露需要在 **注册层、Agent 配置层、运行时上下文层、执行网关层** 四处共同控制。

能力适用范围不等于权限系统：

- `CapabilityScope` 解决“这个能力在当前端、运行环境、Agent、租户下是否应该可见/可用”。
- `PermissionPolicy / PermissionChecker` 解决“当前身份是否允许执行，以及是否需要确认/审批”。
- `RateLimit / Quota` 解决“允许使用多少次、多少 Token、多少并发”。

#### 6.8.1 适用对象

适用范围不是 Tool 独有配置，应统一覆盖以下能力：

| 能力类型 | 示例 | 为什么需要范围控制 |
|---|---|---|
| Tool | calculator、current_time、file_read、send_email | 不同工具风险不同，文件/外发/写操作需要限制端和权限 |
| MCP Server / MCP Tool | filesystem MCP、browser MCP、database MCP | MCP Server 往往一次暴露一组工具，必须按端、租户、Agent、运行环境做可见性过滤 |
| Skill | coding_skill、finance_approval_skill、rag_qa_skill | Skill 会注入提示词、工具、MCP 和流程规则，也需要按场景暴露 |
| Sandbox / Code Executor | python、node、shell、go test | 代码执行能力强，通常只允许 CLI、桌面端或受控任务使用 |
| Sub Agent / Agent-as-Tool | code_agent、research_agent、ops_agent | 子 Agent 可能继承或扩大能力边界，需要按调用来源和授权链控制 |

#### 6.8.2 统一能力范围模型

```go
type ChannelType string

const (
    ChannelCLI       ChannelType = "cli"
    ChannelHTTPAPI   ChannelType = "api"
    ChannelWeb       ChannelType = "web"
    ChannelDesktop   ChannelType = "desktop"
    ChannelWebhook   ChannelType = "webhook"
    ChannelScheduler ChannelType = "scheduler"
    ChannelAgent     ChannelType = "agent"     // 子 Agent / Agent-as-Tool 调用
)

type CapabilityKind string

const (
    CapabilityTool     CapabilityKind = "tool"
    CapabilityMCP      CapabilityKind = "mcp"
    CapabilitySkill    CapabilityKind = "skill"
    CapabilitySandbox  CapabilityKind = "sandbox"
    CapabilitySubAgent CapabilityKind = "sub_agent"
)

type CapabilityScope struct {
    AllowedChannels []ChannelType // 为空表示不按接入端限制
    DeniedChannels  []ChannelType // 显式拒绝优先级最高

    TenantIDs  []string // 为空表示所有租户可见，实际仍需租户隔离校验
    ProjectIDs []string // 为空表示不按项目限制
    AgentIDs   []string // 为空表示不按 Agent 限制
    UserIDs    []string // 为空表示不按用户限制
    Roles      []string // 预留 RBAC，Phase 2.5 接入

    RequiresLocalWorkspace bool // 需要本地工作区，如文件系统/代码执行
    RequiresDesktopRuntime bool // 需要桌面端能力，如本地应用自动化
    RequiresSandbox        bool // 必须经沙箱执行
    NetworkPolicy          string // none / allowlist / full，主要用于 sandbox/code/mcp
}

type RuntimeCapabilityContext struct {
    Channel     ChannelType
    TenantID    string
    UserID      string
    ProjectID   string
    WorkspaceID string
    AgentID     string
    Roles       []string

    HasLocalWorkspace bool
    HasDesktopRuntime bool
}
```

能力定义应携带 `CapabilityScope`：

```go
type ToolDefinition struct {
    Name        string
    Description string
    Parameters  *Schema
    Scope       CapabilityScope
}

type MCPServerDefinition struct {
    Name      string
    Endpoint  string
    ToolNames []string
    Scope     CapabilityScope
}

type SkillDefinition struct {
    ID           string
    Name         string
    Description  string
    Scope        CapabilityScope
    Tools        []ToolRef
    MCPServers   []MCPRef
    Instructions string
}

type SandboxRuntimeDefinition struct {
    Name  string // python / node / shell / go / wasm
    Scope CapabilityScope
}
```

#### 6.8.3 过滤与校验流程

能力范围需要在两个时机生效：

```text
Run 启动
  → 构建 RuntimeCapabilityContext(channel, tenant_id, user_id, project_id, agent_id, workspace)
  → ToolRegistry / MCPRegistry / SkillRegistry / SandboxRegistry 按 CapabilityScope 过滤可见能力
  → SkillRuntime 注入当前上下文允许的提示词、工具、MCP 能力
  → 只把当前上下文允许的 Tool / MCP / Skill 暴露给 LLM

Action 执行
  → ActionExecutor 路由 tool_call / mcp_call / agent_call / code_exec
  → 对应 Gateway 再次校验 CapabilityScope
  → PermissionChecker 校验权限策略（auto / confirm / approval / deny）
  → 需要沙箱则进入 Sandbox Executor
  → 执行并返回 Observation
```

> 注册层过滤是为了减少 LLM 看到不适用能力的概率；执行前校验是安全兜底，不能只依赖提示词或工具列表。

#### 6.8.4 不同端的典型策略

| 能力 | CLI | HTTP API | 桌面端 | Webhook/Scheduler |
|---|---:|---:|---:|---:|
| calculator / current_time | 允许 | 允许 | 允许 | 允许 |
| 只读知识库检索 | 允许 | 允许 | 允许 | 允许 |
| 本地文件读取 | 可允许 | 默认禁止 | 可允许 | 默认禁止 |
| 本地文件写入 | confirm / approval | 默认禁止 | confirm / approval | 默认禁止 |
| shell / 代码执行 | 沙箱 + confirm | 默认禁止 | 沙箱 + confirm | 受控任务可允许 |
| 浏览器/桌面自动化 | 不适用 | 禁止 | 可允许 | 禁止 |
| 外部系统写操作 | confirm / approval | confirm / approval | confirm / approval | approval |
| MCP filesystem server | 可允许 | 默认禁止 | 可允许 | 默认禁止 |
| MCP database server | approval | approval | approval | approval |
| coding skill | 可允许 | 默认禁止或受限 | 可允许 | 受控任务可允许 |

#### 6.8.5 Tool / MCP / Skill 是否需要区分

三者需要共用 `CapabilityScope`，但执行网关要区分：

- Tool：单个函数能力，走 `ToolGateway`，重点是参数校验、权限策略、沙箱和外部回调。
- MCP：协议型能力集合，走 `MCPGateway`，重点是 server 级可见性、tool list 过滤、会话生命周期、远程连接安全。
- Skill：流程能力包，走 `SkillRuntime`，重点是匹配、Hook 注入、可用工具/MCP 子集、停止条件。
- Code/Sandbox：不是普通 Tool 的附属品，而是可被 Tool、MCP、Skill、Coding Strategy 共同使用的隔离执行能力。

#### 6.8.6 与 RBAC / 限流的关系

```text
CapabilityScope  能力适用范围：端、租户、项目、Agent、运行环境
RBAC / Policy    身份权限：角色、资源、动作、数据范围
RateLimit        用量治理：调用次数、并发数、Token、套餐、有效期
```

RBAC、租户/用户/项目级权限、SaaS 多级限流、套餐等平台化治理放在 Phase 2.5；Phase 1B 先实现工具权限策略 `auto / confirm / approval / deny`，并在模型上预留 `Scope / Roles` 字段。

---

## 7. Skill Runtime：技能驱动执行

### 7.1 Skill 定义

Skill 不只是提示词，是影响 Loop 行为的完整规则包：

```go
type Skill struct {
    ID              string
    TenantID        string
    Name            string
    Description     string
    Instructions    string          // 注入系统提示词的指令
    Tools           []ToolRef       // 该 Skill 开放的工具子集
    KnowledgeIDs    []string        // 该 Skill 使用的知识库
    PlanningRules   string          // 规划约束（如"必须先检索再回答"）
    ToolPolicy      map[string]ToolAuthLevel  // 工具级授权覆盖
    OutputSchema    *Schema         // 结构化输出约束
    StopConditions  []StopCondition // Skill 级完成判断条件
    RuntimePolicy   RuntimePolicy   // 覆盖 Agent 默认策略
    EntryCondition  string          // 触发该 Skill 的条件表达式
}
```

### 7.2 Skill 在 Loop 中的干预点

Skill 通过 Hook 机制介入 Loop，不侵入 Run Engine 核心代码：

**四个钩子点（语言无关）：**

```text
on_run_start(run_ctx)          # Run 启动时：注入 system prompt、初始配置
on_step_start(step_ctx)        # 每轮 Loop 开始前：可修改上下文或追加指令
on_step_complete(step_result)  # 每轮 Action 执行后：评估是否满足 Skill 完成条件
                               # → 返回 SkillDecision（是否停止/覆盖下步 Action）
on_run_complete(run_result)    # Run 结束时：后处理（写记忆、发通知等）
```

**Go：**

```go
type SkillHook interface {
    OnRunStart(ctx context.Context, runCtx *RunContext) error
    OnStepStart(ctx context.Context, step *StepContext) error
    OnStepComplete(ctx context.Context, step *StepResult) (SkillDecision, error)
    OnRunComplete(ctx context.Context, result *RunResult) error
}

type SkillDecision struct {
    ShouldStop     bool
    StopReason     string
    OverrideAction *Action
}
```

**Python：**

```python
class SkillHook(Protocol):
    async def on_run_start(self, run_ctx: RunContext) -> None: ...
    async def on_step_start(self, step_ctx: StepContext) -> None: ...
    async def on_step_complete(self, step_result: StepResult) -> SkillDecision: ...
    async def on_run_complete(self, run_result: RunResult) -> None: ...

@dataclass
class SkillDecision:
    should_stop: bool = False
    stop_reason: str = ""
    override_action: Action | None = None
```

### 7.3 Skill 自动匹配

```go
// SkillMatcher 根据用户输入自动匹配最合适的 Skill
type SkillMatcher interface {
    Match(ctx context.Context, input string, availableSkills []Skill) (*Skill, float64, error)
}
```

---

## 8. 扩展点设计：插件式架构

所有扩展点都是接口，通过依赖注入组装。外部系统只需实现对应接口。

### 8.1 PromptBuilder（提示词构建）

业务方注入自定义实现，实现多租户个性化提示词。

**Go：**

```go
type PromptBuilder interface {
    BuildSystem(ctx context.Context, req PromptBuildRequest) (string, error)
}
```

**Python：**

```python
class PromptBuilder(Protocol):
    async def build_system(self, req: PromptBuildRequest) -> str: ...

@dataclass
class PromptBuildRequest:
    agent: Agent
    skill: Skill | None
    session: Session
    user_input: str
    memory: list[MemoryEntry]
    extra_context: dict[str, Any] = field(default_factory=dict)
```

### 8.2 MemoryStore（长期记忆）

见 [第 11 章](#11-长期记忆)。

### 8.3 Logger（日志）

```go
// Logger 接口，兼容 zap / slog / logrus
type Logger interface {
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Debug(msg string, fields ...Field)
    With(fields ...Field) Logger
}

// 内置结构化字段：run_id, step_id, tenant_id, agent_id, action_type
```

### 8.4 EventHandler（事件处理扩展）

外部系统或业务层可以订阅 Agent 运行事件，异步处理，不阻塞主循环。

**标准事件类型（语言无关）：**

```text
run.started           # Run 启动
run.completed         # Run 正常完成
run.failed            # Run 失败
step.completed        # 每步完成
approval.created      # 需要用户审批
approval.granted      # 审批通过
tool.executed         # 工具执行完成
rag.searched          # RAG 检索完成
memory.updated        # 记忆更新
```

**Go：**

```go
type EventHandler interface {
    EventTypes() []EventType
    Handle(ctx context.Context, event *AgentEvent) error
}
```

**Python：**

```python
class EventHandler(Protocol):
    def event_types(self) -> list[EventType]: ...
    async def handle(self, event: AgentEvent) -> None: ...

@dataclass
class AgentEvent:
    tenant_id: str
    run_id: str
    event_type: EventType
    payload: dict
    occurred_at: datetime
```

### 8.5 运行时注册表（统一组装点）

所有扩展通过注册表集中组装，保持向后兼容、易于测试替换。

**Go（functional options）：**

```go
type RuntimeRegistry struct {
    ToolRegistry    ToolRegistry
    SkillRegistry   SkillRegistry
    MemoryStore     MemoryStore
    PromptBuilder   PromptBuilder
    Tracer          Tracer
    UsageMeter      UsageMeter
    Logger          Logger
    EventHandlers   []EventHandler
    AuthProvider    AuthorizationProvider
    SandboxProvider Sandbox
}

func NewRunEngine(registry *RuntimeRegistry, opts ...EngineOption) RunEngine
```

**Python（dataclass + 依赖注入）：**

```python
@dataclass
class RuntimeRegistry:
    tool_registry: ToolRegistry
    skill_registry: SkillRegistry
    memory_store: MemoryStore
    prompt_builder: PromptBuilder
    tracer: Tracer
    usage_meter: UsageMeter
    logger: Logger
    event_handlers: list[EventHandler]
    auth_provider: AuthorizationProvider
    sandbox_provider: Sandbox

def create_run_engine(registry: RuntimeRegistry, **kwargs) -> RunEngine:
    return RunEngineImpl(registry=registry, **kwargs)
```

---

## 9. Trace / Observability：标准可观测性

### 9.1 设计目标

- 兼容 **OpenTelemetry** 标准（可对接 Jaeger / Tempo / Datadog / SkyWalking）
- 每个 Run 对应一条 Trace
- 每个 Step 对应一个 Span
- 工具调用、RAG 检索、LLM 调用各有子 Span
- 支持自定义 Attribute 注入（业务字段）

### 9.2 Span 层级结构

```text
Trace: run/{run_id}
  ├── Span: run.start
  ├── Span: step[0].think
  │     └── Span: llm.chat_completion
  │           Attributes: model, prompt_tokens, completion_tokens
  ├── Span: step[1].rag_search
  │     ├── Span: rag.keyword_extract
  │     ├── Span: rag.parallel_retrieve[kw1]
  │     ├── Span: rag.parallel_retrieve[kw2]
  │     └── Span: rag.evaluate_coverage
  ├── Span: step[2].tool_call[create_work_order]
  │     ├── Span: tool.auth_check
  │     ├── Span: tool.approval_request    (如需审批)
  │     └── Span: tool.execute
  ├── Span: step[3].think
  └── Span: run.complete
```

### 9.3 Tracer 接口

```go
// Tracer 接口，默认实现基于 OpenTelemetry
type Tracer interface {
    StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, Span)
}

type Span interface {
    SetAttribute(key string, value any)
    RecordError(err error)
    End()
}

// 内置 Attribute Key（标准化）
const (
    AttrRunID       = "agent.run_id"
    AttrStepID      = "agent.step_id"
    AttrTenantID    = "agent.tenant_id"
    AttrAgentID     = "agent.agent_id"
    AttrActionType  = "agent.action_type"
    AttrModel       = "llm.model"
    AttrPromptTokens     = "llm.prompt_tokens"
    AttrCompletionTokens = "llm.completion_tokens"
    AttrToolName    = "tool.name"
    AttrRAGKeyword  = "rag.keyword"
    AttrRAGRound    = "rag.round"
)
```

### 9.4 与外部 APM 系统集成

只需在启动时注入 OTLP Exporter：

```go
// 对接 Jaeger
tracerProvider := tracesdk.NewTracerProvider(
    tracesdk.WithBatcher(jaegerExporter),
    tracesdk.WithResource(resource.NewWithAttributes(
        semconv.SchemaURL,
        semconv.ServiceNameKey.String("agent-runtime"),
    )),
)
otel.SetTracerProvider(tracerProvider)
```

---

## 10. Token 消耗追踪

### 10.1 UsageMeter 接口

```go
type UsageMeter interface {
    // 记录一次 LLM 调用的 Token 消耗
    Record(ctx context.Context, usage TokenUsage) error
    // 查询当前 Run 的累计消耗
    GetRunUsage(ctx context.Context, runID string) (*AggregatedUsage, error)
    // 查询租户的消耗汇总（支持时间范围）
    GetTenantUsage(ctx context.Context, tenantID string, period TimePeriod) (*TenantUsage, error)
    // 检查预算是否超限
    CheckBudget(ctx context.Context, runID string) (bool, error)
}

type TokenUsage struct {
    RunID            string
    StepID           string
    TenantID         string
    AgentID          string
    Model            string
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
    CostUSD          float64
    OccurredAt       time.Time
}

type AggregatedUsage struct {
    TotalPromptTokens     int64
    TotalCompletionTokens int64
    TotalTokens           int64
    TotalCostUSD          float64
    BudgetRemaining       float64
    StepBreakdown         []StepUsage
}
```

### 10.2 预算控制

```go
// StopController 在每轮 Loop 结束后检查预算
type StopController interface {
    ShouldStop(ctx context.Context, runCtx *RunContext) (bool, StopReason, error)
}

// 内置停止条件检查
// - 已超过 max_iterations
// - Token 已超过 max_tokens（由 UsageMeter 检查）
// - 费用已超过 max_cost
// - 执行时间已超过 max_duration
// - 连续失败次数超过 max_consecutive_fail
// - Skill 完成条件满足
```

---

## 11. 记忆系统（Memory System）

> **架构升级**：原设计把所有记忆压入一个 `MemoryStore` 接口，导致存储策略和检索方式混乱。  
> 实际上四种记忆的生命周期、存储后端、检索方式完全不同，必须分类设计。

### 11.1 四种记忆类型对比

```text
类型              生命周期          存储位置            检索方式            实现归属      典型内容
──────────────────────────────────────────────────────────────────────────────────────────────────────
工作记忆          Run 内部          内存（RunContext）   直接访问            框架负责      当前迭代的中间结果
Working Memory    Run 结束即释放                                                          当前 Plan、已观察结果

短期记忆          Session 生命周期  Phase 1 文件存储     按时间顺序 / 摘要   框架负责      本次会话的对话历史
Short-term Memory 默认 7 天        后续 DB + Redis                                                              最近 N 条消息
                                                                           （接口可替换实现）

长期记忆          用户/Agent 范围   Phase 1 文件存储     关键词/结构化过滤    接口框架定义   过去任务的关键结论
Long-term Memory  永久（可设过期）  后续 DB + 向量库                                          写入由业务决定  用户显式标记的偏好
                                                                           框架只负责读取注入

注入式上下文      每次 Run 新鲜注入  见下方拆分说明       静态获取（无检索）  见下方拆分说明  当前时间/用户画像/
Static Context    不持久化                                                               租户配置/业务规则
```

**注入式上下文的实现归属拆分：**

```text
子类型            谁来实现                    实现方式
──────────────────────────────────────────────────────────────────────────────
当前时间          框架自动注入                PromptBuilder 内置，调用 now()，无需任何配置
run_id/session_id 框架自动注入                来自 RunContext，内置
用户基本信息      业务实现 StaticContextProvider  调用自己的 User Service API
用户画像/习惯     框架提供 UserProfileStore 接口  Phase 1 读写文件；后续可接 DB / CRM / 用户画像服务
业务规则          Agent.system_prompt 模板变量   配置在 Agent 的 system_prompt 里，变量替换
租户配置          业务实现 StaticContextProvider  查询租户配置表
```

---

### 11.2 工作记忆（Working Memory）

工作记忆是 RunContext 的一部分，见第 3.8 节，无独立接口，直接作为字典操作。

```text
RunContext.working_memory 常见 key：
  "current_plan"          Plan-Execute 的当前计划列表
  "plan_progress"         已完成的 Plan Step 索引
  "retrieved_chunks"      本次 Run 已检索的 RAG chunks（缓存避免重复检索）
  "tool_results"          各工具调用结果的汇总
  "llm_conclusions"       LLM 本 Run 内的中间判断（避免重复推理）
```

---

### 11.3 短期记忆（ShortTermMemory）

管理对话历史，负责 Context Window 压缩（见 4.8 节）。

```text
ShortTermMemory 接口
  append(message)                         追加消息
  get_recent(limit, token_budget)         获取最近 N 条（可设 token 上限）
  summarize_and_compress(keep_recent)     将旧消息总结为摘要，保留最近 keep_recent 条
  get_summary()                           获取已有摘要
  clear()                                 清空（Session 结束时）

请求必须携带：
  tenant_id, user_id, session_id
  # tenant_id 用于租户隔离；user_id 用于用户可见性、清理和后续画像联动

Phase 1 存储：
  FileShortTermMemory → 按 tenant_id/session_id 分文件存储消息与摘要
  # 文件格式建议 JSONL + summary.json，便于追加、调试和迁移
  # 路径由配置控制，默认位于 runtime_data/memory/short_term/

生产存储（后续替换）：
  DBShortTermMemory    → 使用 PostgreSQL 表 agent_message
  RedisShortTermMemory → 纯 Redis 或 Redis 热缓存（适合高并发、低延迟场景）

替换原则：
  engine/app 只依赖 ShortTermMemory 接口；文件、DB、Redis 都是 adapters/memory 下的实现。
```

---

### 11.4 长期记忆（LongTermMemory）

跨 Session 持久存储，按语义相似度检索。

```text
LongTermMemory 接口
  save(entry: LongTermEntry)                              保存记忆
  search(query, scope, top_k, filters)  → list[Entry]    检索记忆
  list(scope, filters, page)           → list[Entry]     用户/管理端分页查看
  get(entry_id)                         → Entry          精确获取
  update(entry_id, patch)                                用户或系统更新记忆
  delete(entry_id)                                        删除 / 遗忘
  consolidate(scope)                                      合并去重（MemoryConsolidator）
  decay(scope, now)                                      记忆衰退评分刷新

LongTermEntry
  id, tenant_id
  scope_type           # user / agent_instance / workspace / project
  scope_id             # 对应的 user_id / agent_instance_id / workspace_id / project_id
  memory_type          # episodic / semantic / procedural / negative
  content              # 记忆文本
  custom_data          # 自定义字段/内容，JSON object，业务可扩展
  embedding            # 向量（Phase 1 可为空；向量实现写入时生成）
  source_event_id      # 来源 Event（可空）
  source_run_id        # 来源 Run（可空）
  source_message_id    # 来源 Message（可空）
  importance           # 重要度评分（0~1，影响检索排序）
  confidence           # 置信度（0~1，影响是否注入上下文）
  sensitivity_level    # public / internal / confidential / secret / pii
  decay_policy         # none / time_decay / access_decay / custom
  last_accessed_at     # 最后访问时间（LRU / 衰退策略参考）
  expired_at           # 可选，支持过期
  supersedes_id        # 可选，表示本记忆替代旧记忆
  created_at, updated_at

memory_type 分类：
  episodic     → "用户在 2026-06-20 完成了合同审查任务，最终输出了 3 个风险点"
  semantic     → "用户偏好简短回答，不喜欢列举超过 5 条"（个人习惯）
  procedural   → "该用户的报告审查流程：先看摘要→再核数据→最后看结论"（工作方式）
  negative     → "某类任务中不要直接调用外部写接口，必须先确认"（失败经验/禁止路径）

Phase 1 存储：
  FileLongTermMemory → 按 tenant_id/scope_type/scope_id 分文件存储长期记忆
  # 检索可先用关键词、标签、memory_type、importance/confidence 排序
  # 路径由配置控制，默认位于 runtime_data/memory/long_term/

生产存储（后续替换）：
  PgVectorLongTermMemory   → PostgreSQL + pgvector（一体化，推荐启元 AI 平台）
  QdrantLongTermMemory     → Qdrant 向量数据库（高性能，推荐独立部署场景）
  ChromaLongTermMemory     → Chroma（轻量，适合开发/测试）

替换原则：
  engine/app 只依赖 LongTermMemory 接口；文件、DB、向量库都只是 adapters/memory 下的实现。
```

---

### 11.5 注入式上下文（StaticContextProvider）

每次 Run 启动时注入 system prompt，不做持久化。  
分两部分：**框架固定注入**（自动，零配置）和**业务自定义注入**（实现接口）。

#### 框架固定注入（无需任何配置，PromptBuilder 内置）

```text
以下字段由框架在 PromptBuilder 内自动注入，业务代码无需关心：

  current_datetime    → datetime.now(tz)，调用时当场取，始终新鲜
  current_date        → 日期（部分场景只需日期）
  run_id              → 来自 RunContext
  session_id          → 来自 RunContext
  agent_name          → 来自 Agent 配置

system prompt 模板示例（框架内置变量）：
  当前时间：{current_datetime}
  你的名字：{agent_name}
```

#### 业务自定义注入（实现 StaticContextProvider 接口）

```text
StaticContextProvider 接口
  get_context(req: ContextRequest) → dict[str, Any]

ContextRequest
  tenant_id, user_id, agent_id, session_id, run_id

业务实现返回的 key 会被合并进 PromptBuilder 的模板变量，例：
  {
    "user_name": "张三",                         ← 查询 User 表
    "user_role": "项目经理",                     ← 查询 User 表
    "tenant_name": "启元建设集团",               ← 查询 Tenant 表
    "user_preferences": "回答用中文，不超过 3 句" ← 查询 UserPreference 表 / CRM API
  }

注意：business_rules（业务规则）不走这里
  → 直接写在 Agent.system_prompt 模板里，属于配置，不需要动态查询
  → 例：Agent.system_prompt = "...合同金额超过 100 万需要法务审查..."

实现选项（可替换）：
  NoopStaticContext        → 不注入任何业务上下文（默认，适合通用场景）
  DBStaticContext          → 查询 User / Tenant 表（标准业务实现）
  APIStaticContext         → 调用外部 CRM / 用户画像 API（企业集成场景）
  CompositeStaticContext   → 组合多个来源（先查 DB，再补充 API）
```

---

### 11.6 MemoryConsolidator（记忆整合器，可选业务扩展）

> **归属说明**：MemoryConsolidator 是**业务代码**，不是框架核心组件。  
> 框架不自动从 Run 结果里提取任何内容写入长期记忆——"值得记什么"是业务决策。  
> 业务方按需通过 `SkillHook.on_run_complete` 触发，或作为异步后台任务手动调用。

```text
MemoryConsolidator（业务自行实现）
  extract_memories(run_result, run_ctx) → list[LongTermEntry]
    # 业务方决定提取什么：
    # 例："用户确认了合同条款 3.2 有风险" → 写入用户范围的 episodic 记忆
    # 例："用户要求回答简洁" → 写入 semantic 记忆
    # 框架不做这个决策

  merge_similar(scope)
    # 防止记忆库膨胀：与现有记忆做相似度比较，合并重复条目

  expire_stale(scope)
    # 清理过期和低价值记忆

触发时机（业务代码自行选择）：
  方式 A  SkillHook.on_run_complete 里同步触发（简单，轻量任务适合）
  方式 B  SkillHook.on_run_complete 里发布异步事件，后台 Worker 处理（推荐，不影响响应速度）
  方式 C  用户显式触发（如"记住这个偏好"工具调用）

用户画像/个人习惯的正确归属：
  ✅ 框架定义 UserProfileStore 契约，文件、DB、CRM、用户画像服务都是可替换实现
  ✅ Agent 框架可通过 StaticContextProvider 读取用户画像并注入上下文
  ❌ 不应该由框架自动从 Run 结论中无审批地推断并写入画像
```

---

### 11.7 记忆在 ContextBuilder 中的注入顺序

```text
Run 启动时（一次性，缓存到 RunContext）：
  1. StaticContextProvider.get_context()     → RunContext.injected_static_context
  2. LongTermMemory.search(user_input)       → RunContext.injected_memories

每轮 Loop 的 ContextBuilder.build() 拼装顺序：
  [System Prompt]
    = role + skill_instructions
    + static_context（当前时间/用户画像/业务规则）
    + long_term_memories（Run 启动时检索，本次 Run 内缓存不重复查）
  [History]
    = ShortTermMemory.get_recent()（按 4.8 节策略裁剪）
  [Working Observations]
    = RunContext.steps_so_far 的 Observation（当前 Run 内积累的观察结果）
  [User Input]
    = 当前用户消息
```

---

### 11.8 用户画像（UserProfile）

用户画像属于记忆系统的一部分，但不等同于长期记忆。长期记忆记录事实、事件和经验；用户画像记录相对稳定的偏好、身份上下文和交互方式。画像应当可查看、可编辑、可删除，不能只存在于不可解释的 Prompt 总结里。

```text
UserProfileStore 接口
  get(scope)                         → UserProfile
  update(scope, patch)               → UserProfile
  list_fields(scope)                 → list[ProfileField]
  delete_field(scope, field_name)    → void

UserProfile
  tenant_id, user_id
  builtin_fields       # 系统通用字段
  custom_fields        # 租户/业务自定义字段/内容，JSON object
  evidence             # 字段来源证据：source_event_id/source_run_id/source_message_id
  confidence           # 字段级置信度
  visibility           # user_visible / internal / system_only
  sensitivity_level    # public / internal / confidential / secret / pii
  created_at, updated_at

builtin_fields 建议：
  locale / timezone / preferred_language
  communication_style      # 简洁 / 详细 / 列表优先 / 示例优先
  domain_roles             # 项目经理 / 法务 / 开发者等
  tool_preferences         # 常用工具或禁用工具偏好
  risk_preference          # 保守 / 平衡 / 激进
  memory_opt_in            # 是否允许长期记忆与画像
```

Phase 1 可提供 `FileUserProfileStore`，把画像存为按 `tenant_id/user_id` 切分的 JSON 文件。后续替换为 DB、CRM、用户画像服务或组合实现。`StaticContextProvider` 读取画像时只依赖 `UserProfileStore` 接口，不关心底层来源。

---

### 11.9 记忆更新、衰退与用户可编辑

记忆系统必须支持更新和遗忘，而不是只追加。Phase 1 即使使用文件存储，也要把数据结构设计成可迁移、可编辑、可审计。

```text
记忆更新策略：
  append      # 新事件、新事实直接追加
  merge       # 与相似记忆合并，保留 evidence 列表
  supersede   # 新记忆替代旧记忆，旧记忆保留 superseded 状态
  correct     # 用户显式纠正，优先级高于系统推断
  delete      # 用户显式删除或策略遗忘

记忆衰退评分：
  score = importance * confidence * recency_factor * access_factor

衰退原则：
  - 低分记忆优先降权，不立即物理删除
  - 用户显式保存、合规规则、安全边界类记忆默认不自动衰退
  - secret / pii 默认不进入 LLM 上下文，除非策略明确允许
  - 用户可见的画像和记忆必须能通过 UI/API 编辑和删除
```

Phase 1 需要实现的是接口和文件级基础能力：保存、读取、列表、更新、删除、简单检索。复杂的向量召回、批量衰退、冲突合并、审计报表可以放到后续 DB 化时增强。

---

### 11.10 从记忆沉淀为 Skill Candidate

“自主进化”不应理解为 Agent 自动修改自己的能力，而是从长期运行中生成可审核的 Skill 候选。Skill 是能力资产，必须版本化、可审批、可回滚。

```text
Successful Runs / Trace / Memory
  → Pattern Mining
  → Skill Candidate
  → Review / Approval
  → Versioned Skill
  → Runtime Skill Registry

SkillCandidate
  tenant_id
  source_run_ids / source_memory_ids
  name, description
  proposed_instructions
  required_tools / required_mcp / required_permissions
  examples
  risk_level
  status              # candidate / reviewing / approved / rejected / active / deprecated
  version
```

Phase 1 只预留数据结构和接口边界，不自动生成 Skill。Phase 2 可从高频成功 Run、人工标注记忆中生成候选；Phase 2.5/3 再引入审批流、权限策略、多 Agent 共享经验沉淀。

---

## 12. 外部系统接入与授权

### 12.1 接入方式总览

外部系统可以通过以下方式与 Agent Runtime 交互：

```text
1. 同步 HTTP API     → 触发 Run、查询状态、提交审批
2. WebSocket / SSE   → 实时接收 Run 的流式输出和事件
3. Webhook 回调      → Run 完成/失败/需审批时主动通知
4. 工具注册 API      → 注册外部工具，让 Agent 能调用
5. 事件推送          → 外部事件触发 Agent（审批、文件、告警）
```

### 12.2 API 鉴权方式

支持多种鉴权方式，在 Gateway 层统一处理：

```go
type AuthorizationProvider interface {
    // 验证请求身份，返回调用方信息
    Authenticate(ctx context.Context, req AuthRequest) (*CallerInfo, error)
    // 检查调用方是否有权限执行某操作
    Authorize(ctx context.Context, caller *CallerInfo, action string, resource string) (bool, error)
}

// 内置鉴权实现：
// - APIKeyAuth：Bearer {api_key}，适合服务器到服务器
// - SessionAuth：通过 Redis Session，适合 Web 前端
// - OAuthAuth：支持 OAuth2 Client Credentials，适合第三方系统
// - HMACAuth：HMAC 签名，适合 Webhook 回调验证

type CallerInfo struct {
    TenantID   string
    CallerType string        // user / service / agent / webhook
    CallerID   string
    Scopes     []string      // 授权范围
}
```

### 12.3 标准 HTTP API

```http
# 触发一次 Run
POST /api/v1/agents/{agent_id}/runs
Authorization: Bearer {api_key}
Content-Type: application/json

{
  "session_id": "optional",
  "input": "帮我分析这份合同的风险点",
  "attachments": [],
  "runtime_mode": "react_loop",
  "stream": true
}

# 响应（stream=true 时返回 SSE）
→ event: step
   data: {"step_index": 0, "action_type": "think", "content": "..."}

→ event: tool_approval_required
   data: {"approval_id": "...", "tool_name": "send_email", "params": {...}}

→ event: completed
   data: {"run_id": "...", "result": "...", "usage": {...}}

# 提交审批
POST /api/v1/approvals/{approval_id}/grant
Authorization: Bearer {api_key}
→ Run 自动恢复执行

# 拒绝审批
POST /api/v1/approvals/{approval_id}/reject
Authorization: Bearer {api_key}
{"reason": "不允许发送外部邮件"}

# 查询 Run 状态
GET /api/v1/runs/{run_id}
Authorization: Bearer {api_key}

# 注册外部 Webhook（Run 完成时回调）
POST /api/v1/agents/{agent_id}/webhooks
Authorization: Bearer {api_key}
{
  "url": "https://your-system.com/agent-callback",
  "events": ["run.completed", "approval.created"],
  "secret": "hmac-secret-for-verification"
}
```

### 12.4 外部事件触发 Agent

```http
# 外部系统推送事件，触发某个 Agent 处理
POST /api/v1/events
Authorization: Bearer {api_key}

{
  "event_type": "external.alert",
  "source_type": "monitoring",
  "source_id": "alert-123",
  "payload": {
    "alert_name": "CPU 超过 90%",
    "severity": "high",
    "host": "prod-server-01"
  },
  "target_agent_id": "ops-agent-xxx"
}
```

### 12.5 工具沙盒隔离（外部工具安全边界）

外部注册的工具默认在隔离环境执行：

```text
外部工具调用链路：
Agent Loop 决定调用工具
  ↓
Tool Gateway 查找工具（外部注册类型）
  ↓
创建沙盒请求（网络白名单、超时、无文件系统权限）
  ↓
向 callback_url 发起 HTTP 请求（带 HMAC 签名）
  ↓
等待响应（同步）或轮询（异步）
  ↓
验证响应签名
  ↓
返回 Observation 给 Loop
```

---

## 13. 数据模型

> 所有表必须包含 `tenant_id`，所有查询必须带 `tenant_id` 过滤。  
> Go 领域模型与下表字段的对应关系见 **§3.0**（`ResourceAudit` / `RuntimeAudit` 嵌入约定）。
>
> 按表类型分两套标准审计字段：
>
> **资源表**（配置类：`agent` / `agent_instance` / `agent_session` / `agent_task` / `agent_webhook`）
>
> | 字段 | 类型 | 说明 |
> |------|------|------|
> | `owner_id` | VARCHAR(36) | 当前拥有者 ID（可转让，用于权限控制） |
> | `owner_name` | VARCHAR(200) | 拥有者名称（冗余，避免 JOIN） |
> | `created_by` | VARCHAR(36) | 最初创建人 user_id（不可变，审计用）|
> | `updated_by` | VARCHAR(36) | 最后修改人 user_id |
> | `created_at` | TIMESTAMPTZ | 创建时间 |
> | `updated_at` | TIMESTAMPTZ | 最后修改时间 |
>
> > `owner_id` vs `created_by` 的区别：创建时两者相同；当所有权被转让时，`owner_id` 变更而 `created_by` 永远记录最初创建人。
>
> **运行时流水表**（系统自动生成：`agent_run` / `agent_step` / `agent_event` / `agent_intervention` / `agent_message` / `agent_memory` / `agent_usage`）
>
> | 字段 | 类型 | 说明 |
> |------|------|------|
> | `owner_id` | VARCHAR(36) | 继承自父实体的用户 ID（用于过滤和报表） |
> | `created_at` | TIMESTAMPTZ | 创建时间 |
> | `updated_at` | TIMESTAMPTZ | 最后更新时间（只写表可省略） |
>
> > 流水表不需要 `created_by`/`updated_by`（永远是 `'system'`，无信息量）；不需要 `owner_name`（只在 `agent_run`、`agent_usage` 等报表相关表按需保留）。

### 13.1 agent（Agent 配置模板）

```sql
CREATE TABLE agent (
    id                VARCHAR(36) PRIMARY KEY,
    tenant_id         VARCHAR(36)  NOT NULL,
    name              VARCHAR(200) NOT NULL,
    description       TEXT,
    type              VARCHAR(50)  NOT NULL,          -- react_loop / plan_execute / skill_based / coding
    default_model     VARCHAR(100),
    system_prompt     TEXT,
    tools             JSONB DEFAULT '[]',              -- ToolRef 列表
    skills            JSONB DEFAULT '[]',              -- SkillRef 列表
    rag_config        JSONB DEFAULT '{}',
    mcp_config        JSONB DEFAULT '{}',
    permission_policy JSONB DEFAULT '{}',
    runtime_policy    JSONB DEFAULT '{}',              -- max_iterations / max_tokens 等
    retry_policy      JSONB DEFAULT '{}',              -- 默认重试策略（RetryPolicy）
    memory_config     JSONB DEFAULT '{}',
    output_schema     JSONB,
    status            VARCHAR(20) DEFAULT 'active',
    owner_id          VARCHAR(36),                    -- 拥有者 ID（用户或团队）
    owner_name        VARCHAR(200),                   -- 拥有者名称（冗余）
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by        VARCHAR(36),                    -- 创建人 user_id
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by        VARCHAR(36)                     -- 最后修改人 user_id
);
CREATE INDEX idx_agent_tenant ON agent(tenant_id);
CREATE INDEX idx_agent_owner  ON agent(tenant_id, owner_id);
COMMENT ON TABLE agent IS 'Agent 配置模板';
COMMENT ON COLUMN agent.runtime_policy IS '运行策略：最大迭代次数、Token 预算、超时等';
COMMENT ON COLUMN agent.retry_policy   IS '默认重试策略，可被 Run 级别覆盖';
COMMENT ON COLUMN agent.owner_id       IS '拥有者 ID，通常为创建该 Agent 的用户或所属团队';
COMMENT ON COLUMN agent.created_by     IS '创建人 user_id';
COMMENT ON COLUMN agent.updated_by     IS '最后修改人 user_id';
```

### 13.2 agent_instance（运行实例）

```sql
CREATE TABLE agent_instance (
    id               VARCHAR(36)  PRIMARY KEY,
    tenant_id        VARCHAR(36)  NOT NULL,
    agent_id         VARCHAR(36)  NOT NULL,
    workspace_id     VARCHAR(36),
    name             VARCHAR(200),
    role             VARCHAR(100),
    status           VARCHAR(20)  DEFAULT 'active',
    memory_scope     VARCHAR(200),
    workspace_path   VARCHAR(500),
    permission_scope JSONB DEFAULT '[]',
    owner_id         VARCHAR(36),                    -- 拥有者 ID
    owner_name       VARCHAR(200),                   -- 拥有者名称（冗余）
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by       VARCHAR(36),                    -- 创建人 user_id
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_by       VARCHAR(36)                     -- 最后修改人 user_id
);
CREATE INDEX idx_agent_instance_tenant ON agent_instance(tenant_id);
CREATE INDEX idx_agent_instance_agent  ON agent_instance(agent_id);
CREATE INDEX idx_agent_instance_owner  ON agent_instance(tenant_id, owner_id);
COMMENT ON TABLE agent_instance IS 'Agent 在特定空间的运行实例';
COMMENT ON COLUMN agent_instance.owner_id IS '实例拥有者（通常为部署/授权该实例的用户）';
```

### 13.3 agent_session（对话上下文）

```sql
CREATE TABLE agent_session (
    id                VARCHAR(36)  PRIMARY KEY,
    tenant_id         VARCHAR(36)  NOT NULL,
    agent_instance_id VARCHAR(36)  NOT NULL,
    user_id           VARCHAR(36),
    workspace_id      VARCHAR(36),
    title             VARCHAR(500),
    status            VARCHAR(20)  DEFAULT 'active',
    context_window    JSONB DEFAULT '[]',            -- 当前上下文消息列表（已迁移到 agent_message 表）
    owner_id          VARCHAR(36),                  -- 拥有者 ID（通常等于 user_id）
    owner_name        VARCHAR(200),                 -- 拥有者名称（冗余）
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by        VARCHAR(36),                  -- 创建人 user_id
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_by        VARCHAR(36)                   -- 最后修改人 user_id
);
CREATE INDEX idx_session_tenant ON agent_session(tenant_id);
CREATE INDEX idx_session_user   ON agent_session(tenant_id, user_id);
CREATE INDEX idx_session_owner  ON agent_session(tenant_id, owner_id);
COMMENT ON TABLE agent_session IS '对话会话，包含上下文消息';
COMMENT ON COLUMN agent_session.owner_id IS '会话拥有者，通常为发起会话的用户';
```

### 13.4 agent_task（业务任务）

```sql
CREATE TABLE agent_task (
    id               VARCHAR(36)  PRIMARY KEY,
    tenant_id        VARCHAR(36)  NOT NULL,
    workspace_id     VARCHAR(36),
    session_id       VARCHAR(36),
    parent_task_id   VARCHAR(36),
    assignee_id      VARCHAR(36),                   -- AgentInstance ID
    created_by_type  VARCHAR(50),                   -- user / agent / scheduler / webhook
    created_by_id    VARCHAR(36),                   -- 创建者 ID（与 created_by 语义一致，保留兼容）
    trigger_event_id VARCHAR(36),
    title            VARCHAR(500),
    payload          JSONB,
    status           VARCHAR(30) DEFAULT 'pending', -- pending/running/waiting_approval/waiting_event/completed/failed/cancelled
    priority         INT DEFAULT 5,
    deadline         TIMESTAMPTZ,
    retry_count      INT DEFAULT 0,
    max_retry        INT DEFAULT 3,
    owner_id         VARCHAR(36),                   -- 任务拥有者（通常为提交任务的用户）
    owner_name       VARCHAR(200),                  -- 拥有者名称（冗余）
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by       VARCHAR(36),                   -- 创建人 user_id
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_by       VARCHAR(36)                    -- 最后修改人 user_id
);
CREATE INDEX idx_task_tenant ON agent_task(tenant_id);
CREATE INDEX idx_task_status ON agent_task(tenant_id, status);
CREATE INDEX idx_task_owner  ON agent_task(tenant_id, owner_id);
COMMENT ON TABLE agent_task IS '业务任务，支持暂停、重试、派发';
COMMENT ON COLUMN agent_task.owner_id      IS '任务拥有者，通常为提交任务的用户或所属团队';
COMMENT ON COLUMN agent_task.created_by_id IS '任务发起者 ID（用于 user/agent/scheduler 区分类型）';
```

### 13.5 agent_run（执行记录）

```sql
CREATE TABLE agent_run (
    id                VARCHAR(36)   PRIMARY KEY,
    tenant_id         VARCHAR(36)   NOT NULL,
    task_id           VARCHAR(36),
    session_id        VARCHAR(36)   NOT NULL,
    agent_instance_id VARCHAR(36)   NOT NULL,
    runtime_mode      VARCHAR(50),                   -- react_loop / plan_execute 等
    status            VARCHAR(30)   DEFAULT 'created',
    pause_reason      TEXT,
    total_tokens      BIGINT        DEFAULT 0,
    total_cost        NUMERIC(12,6) DEFAULT 0,
    started_at        TIMESTAMPTZ,
    finished_at       TIMESTAMPTZ,
    owner_id          VARCHAR(36),                   -- 触发此 Run 的用户 ID（继承自 session）
    owner_name        VARCHAR(200),                  -- 触发者名称（冗余，方便报表不 JOIN）
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_run_tenant  ON agent_run(tenant_id);
CREATE INDEX idx_run_session ON agent_run(session_id);
CREATE INDEX idx_run_status  ON agent_run(tenant_id, status);
CREATE INDEX idx_run_owner   ON agent_run(tenant_id, owner_id);
COMMENT ON TABLE agent_run IS 'Agent 一次执行，包含状态机和 Token 计量';
COMMENT ON COLUMN agent_run.owner_id   IS '触发此 Run 的用户 ID；系统/调度触发时为 NULL';
COMMENT ON COLUMN agent_run.owner_name IS '冗余字段，报表统计时避免 JOIN user 表';
```

### 13.6 agent_step（执行步骤）

```sql
CREATE TABLE agent_step (
    id                VARCHAR(36)   PRIMARY KEY,
    tenant_id         VARCHAR(36)   NOT NULL,
    run_id            VARCHAR(36)   NOT NULL,
    step_index        INT           NOT NULL,
    action_type       VARCHAR(50),                   -- think/rag_search/tool_call/mcp_call/agent_call/final_answer
    action_payload    JSONB,
    observation       JSONB,
    prompt_tokens     INT           DEFAULT 0,
    completion_tokens INT           DEFAULT 0,
    cost              NUMERIC(12,6) DEFAULT 0,
    status            VARCHAR(20)   DEFAULT 'pending',
    started_at        TIMESTAMPTZ,
    finished_at       TIMESTAMPTZ,
    owner_id          VARCHAR(36),                   -- 继承自所属 Run 的 owner_id（方便按用户过滤 Trace）
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_step_run   ON agent_step(run_id);
CREATE INDEX idx_step_owner ON agent_step(tenant_id, owner_id);
COMMENT ON TABLE agent_step IS 'Run 内部每一步，是 Trace 和恢复的最小单位';
COMMENT ON COLUMN agent_step.owner_id IS '冗余自 agent_run.owner_id，方便按用户过滤执行步骤';
```

### 13.7 agent_event（事件总线）

```sql
CREATE TABLE agent_event (
    id            VARCHAR(36)  PRIMARY KEY,
    tenant_id     VARCHAR(36)  NOT NULL,
    workspace_id  VARCHAR(36),
    event_type    VARCHAR(100) NOT NULL,            -- user_message/approval_granted/webhook 等
    source_type   VARCHAR(50),
    source_id     VARCHAR(200),
    target_run_id VARCHAR(36),                     -- 需要恢复的 Run（可空）
    payload       JSONB,
    status        VARCHAR(20)  DEFAULT 'pending',
    processed_at  TIMESTAMPTZ,
    owner_id      VARCHAR(36),                     -- 事件触发者 ID；系统调度触发时为 NULL
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
    -- 事件是只写记录，无 updated_at
);
CREATE INDEX idx_event_tenant ON agent_event(tenant_id);
CREATE INDEX idx_event_status ON agent_event(status, created_at);
CREATE INDEX idx_event_owner  ON agent_event(tenant_id, owner_id);
COMMENT ON TABLE agent_event IS '事件总线，所有触发统一路由（一期用 DB 表模拟）';
COMMENT ON COLUMN agent_event.owner_id IS '事件来源的用户 ID；系统/调度触发时为 NULL';
```

### 13.8 agent_intervention（人工干预记录）

> 原 `agent_approval` 表升级为 `agent_intervention`，覆盖五种干预类型。

```sql
CREATE TABLE agent_intervention (
    id                  VARCHAR(36)  PRIMARY KEY,
    tenant_id           VARCHAR(36)  NOT NULL,
    run_id              VARCHAR(36)  NOT NULL,
    step_id             VARCHAR(36),
    intervention_type   VARCHAR(50)  NOT NULL,          -- tool_approval / tool_confirmation /
                                                        -- user_input_required / manual_override /
                                                        -- step_by_step / authorization
    tool_name           VARCHAR(200),                   -- tool_approval / tool_confirmation 时有值
    action_payload      JSONB,                          -- 工具参数（展示给用户）
    risk_level          VARCHAR(20),                    -- low / medium / high
    display_info        JSONB DEFAULT '{}',             -- 前端展示信息（参数摘要、风险说明）
    llm_question        TEXT,                           -- user_input_required 时，LLM 的问题文本
    requested_by_agent  VARCHAR(36)  NOT NULL,
    status              VARCHAR(30)  DEFAULT 'pending', -- pending / resolved / expired / cancelled
    default_on_timeout  VARCHAR(30),                    -- approve / reject / cancel（超时自动处理）
    expires_at          TIMESTAMPTZ,
    resolved_by         VARCHAR(36),                    -- 处理人 user_id
    resolution          JSONB,                          -- { action, input, reason }
    resolved_at         TIMESTAMPTZ,
    owner_id            VARCHAR(36),                    -- 干预目标用户（继承自 agent_run.owner_id）
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW()
    -- resolved_by 已记录处理人，无需额外 updated_by
);
CREATE INDEX idx_intervention_tenant  ON agent_intervention(tenant_id);
CREATE INDEX idx_intervention_run     ON agent_intervention(run_id);
CREATE INDEX idx_intervention_status  ON agent_intervention(tenant_id, status);
CREATE INDEX idx_intervention_owner   ON agent_intervention(tenant_id, owner_id);
CREATE INDEX idx_intervention_expires ON agent_intervention(status, expires_at)
    WHERE status = 'pending';                           -- 超时扫描专用索引
COMMENT ON TABLE agent_intervention IS '人工干预记录（审批/确认/用户输入/手动接管/逐步确认/授权）';
COMMENT ON COLUMN agent_intervention.intervention_type IS 'tool_approval/tool_confirmation/user_input_required/manual_override/step_by_step/authorization';
COMMENT ON COLUMN agent_intervention.resolution        IS '处理结果：{ action: approve/reject/confirm/input/override, input: "用户输入文本", reason: "拒绝原因" }';
COMMENT ON COLUMN agent_intervention.owner_id          IS '干预目标用户，继承自 agent_run.owner_id';
COMMENT ON COLUMN agent_intervention.resolved_by       IS '处理干预的用户 ID（等同于流水表的 updated_by）';
```

### 13.9 agent_message（对话消息）

> 原 Session 表的 ContextWindow 字段拆出为独立表，避免单行 JSONB 过大。

```sql
CREATE TABLE agent_message (
    id           VARCHAR(36)  PRIMARY KEY,
    tenant_id    VARCHAR(36)  NOT NULL,
    session_id   VARCHAR(36)  NOT NULL,
    run_id       VARCHAR(36),                       -- 关联的 Run（可空，用户消息无 run_id）
    role         VARCHAR(20)  NOT NULL,             -- system / user / assistant / tool
    content      TEXT,
    tool_calls   JSONB,                             -- assistant role 包含工具调用时有值
    tool_call_id VARCHAR(200),                      -- tool role 的结果对应的 tool_call_id
    token_count  INT          DEFAULT 0,            -- 该消息的 token 数（缓存，避免每次重新计数）
    owner_id     VARCHAR(36),                       -- 消息所属用户（user role 为发送者；其余继承会话 owner）
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
    -- 消息是只写记录，无 updated_at
);
CREATE INDEX idx_message_session ON agent_message(session_id, created_at);
CREATE INDEX idx_message_run     ON agent_message(run_id);
CREATE INDEX idx_message_owner   ON agent_message(tenant_id, owner_id);
COMMENT ON TABLE agent_message IS '对话消息明细，ShortTermMemory 的存储表';
COMMENT ON COLUMN agent_message.owner_id IS 'user role 消息为发送用户 ID；assistant/tool 消息为会话 owner_id';

-- agent_session 表中去掉 context_window JSONB 字段，改为引用 agent_message 表
-- session 只存 context 的配置（窗口策略、摘要等）
ALTER TABLE agent_session ADD COLUMN IF NOT EXISTS
    context_summary TEXT;                              -- 历史消息的摘要（summarize_and_compress 结果）
ALTER TABLE agent_session ADD COLUMN IF NOT EXISTS
    context_strategy VARCHAR(30) DEFAULT 'sliding_window'; -- sliding_window / summary / selective
```

### 13.10 agent_memory（长期记忆）

```sql
CREATE TABLE agent_memory (
    id               VARCHAR(36)   PRIMARY KEY,
    tenant_id        VARCHAR(36)   NOT NULL,
    scope_type       VARCHAR(30)   NOT NULL,           -- user / agent_instance / workspace
    scope_id         VARCHAR(36)   NOT NULL,           -- 对应 user_id / agent_instance_id / workspace_id
    memory_type      VARCHAR(30)   NOT NULL,           -- episodic / semantic / procedural
    content          TEXT          NOT NULL,
    embedding_model  VARCHAR(100),                     -- 向量模型（pgvector 使用时记录）
    source_event_id  VARCHAR(36),                      -- 来源 Event（可空）
    source_run_id    VARCHAR(36),                      -- 来源 Run
    source_message_id VARCHAR(36),                    -- 来源 Message（可空）
    importance       NUMERIC(3,2)  DEFAULT 0.5,        -- 重要度（0~1），影响检索排序
    metadata         JSONB DEFAULT '{}',
    confidence       NUMERIC(3,2)  DEFAULT 0.7,        -- 置信度（0~1）
    sensitivity_level VARCHAR(30) DEFAULT 'internal', -- public/internal/confidential/secret/pii
    decay_policy     VARCHAR(30) DEFAULT 'time_decay',-- none/time_decay/access_decay/custom
    supersedes_id    VARCHAR(36),                      -- 替代的旧记忆 ID
    status           VARCHAR(30) DEFAULT 'active',    -- active/superseded/deleted
    last_accessed_at TIMESTAMPTZ,                      -- 最后访问时间（LRU 参考）
    expired_at       TIMESTAMPTZ,
    owner_id         VARCHAR(36),                      -- 记忆归属用户 ID（通常等于 scope_id）
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_memory_scope   ON agent_memory(tenant_id, scope_type, scope_id);
CREATE INDEX idx_memory_type    ON agent_memory(tenant_id, memory_type);
CREATE INDEX idx_memory_owner   ON agent_memory(tenant_id, owner_id);
CREATE INDEX idx_memory_expires ON agent_memory(expired_at) WHERE expired_at IS NOT NULL;
COMMENT ON TABLE agent_memory IS '长期记忆：episodic（发生了什么）/ semantic（习惯偏好）/ procedural（工作方式）';
COMMENT ON COLUMN agent_memory.memory_type IS 'episodic=事件记忆, semantic=习惯偏好, procedural=操作流程';
COMMENT ON COLUMN agent_memory.importance  IS '重要度评分 0~1，检索时加权排序，MemoryConsolidator 自动评估';
COMMENT ON COLUMN agent_memory.confidence  IS '置信度评分 0~1，用户纠正或多次验证可提高置信度';
COMMENT ON COLUMN agent_memory.metadata    IS '自定义字段/内容，Phase 1 文件存储和后续 DB 存储保持同一 JSON 结构';
COMMENT ON COLUMN agent_memory.owner_id    IS '记忆归属用户，通常等于 scope_id（scope_type=user 时）';
```

### 13.11 agent_user_profile（用户画像）

```sql
CREATE TABLE agent_user_profile (
    id                VARCHAR(36)  PRIMARY KEY,
    tenant_id         VARCHAR(36)  NOT NULL,
    user_id           VARCHAR(36)  NOT NULL,
    builtin_fields    JSONB DEFAULT '{}',              -- 系统通用画像字段
    custom_fields     JSONB DEFAULT '{}',              -- 租户/业务自定义字段/内容
    evidence          JSONB DEFAULT '[]',              -- 来源证据：event/run/message 等
    confidence        NUMERIC(3,2) DEFAULT 0.7,        -- 整体置信度，字段级置信度可放在 JSON 内
    visibility        VARCHAR(30) DEFAULT 'user_visible', -- user_visible/internal/system_only
    sensitivity_level VARCHAR(30) DEFAULT 'internal',  -- public/internal/confidential/secret/pii
    memory_opt_in     BOOLEAN DEFAULT TRUE,            -- 是否允许长期记忆与画像
    owner_id          VARCHAR(36),                     -- 通常等于 user_id
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, user_id)
);
CREATE INDEX idx_user_profile_tenant_user ON agent_user_profile(tenant_id, user_id);
CREATE INDEX idx_user_profile_owner       ON agent_user_profile(tenant_id, owner_id);
COMMENT ON TABLE agent_user_profile IS '用户画像：通用字段 + 租户/业务自定义字段，支持用户查看、编辑和删除';
COMMENT ON COLUMN agent_user_profile.custom_fields IS '自定义字段/内容，Phase 1 文件存储和后续 DB 存储保持同一 JSON 结构';
COMMENT ON COLUMN agent_user_profile.memory_opt_in IS '用户是否允许长期记忆和画像能力，关闭后不得自动写入画像/长期记忆';
```

### 13.12 agent_usage（Token 消耗记录）

```sql
CREATE TABLE agent_usage (
    id                VARCHAR(36)   PRIMARY KEY,
    tenant_id         VARCHAR(36)   NOT NULL,
    agent_id          VARCHAR(36),
    run_id            VARCHAR(36),
    step_id           VARCHAR(36),
    model             VARCHAR(100),
    prompt_tokens     INT           DEFAULT 0,
    completion_tokens INT           DEFAULT 0,
    total_tokens      INT           DEFAULT 0,
    cost_usd          NUMERIC(12,8) DEFAULT 0,
    occurred_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    owner_id          VARCHAR(36),                    -- 消耗归属用户（继承自 Run.owner_id，用于配额管理）
    owner_name        VARCHAR(200)                    -- 归属用户名称（冗余，报表直接展示）
    -- 只写记录，无 created_at / updated_at 单独列，occurred_at 即为记录时间
);
CREATE INDEX idx_usage_tenant ON agent_usage(tenant_id, occurred_at);
CREATE INDEX idx_usage_run    ON agent_usage(run_id);
CREATE INDEX idx_usage_owner  ON agent_usage(tenant_id, owner_id, occurred_at);
COMMENT ON TABLE agent_usage IS 'Token 消耗明细，支持按租户/Agent/Run/用户汇总';
COMMENT ON COLUMN agent_usage.owner_id   IS '消耗归属用户，用于用量统计和配额管理';
COMMENT ON COLUMN agent_usage.owner_name IS '冗余字段，避免报表 JOIN user 表';
```

### 13.13 agent_webhook（Webhook 配置）

```sql
CREATE TABLE agent_webhook (
    id         VARCHAR(36)   PRIMARY KEY,
    tenant_id  VARCHAR(36)   NOT NULL,
    agent_id   VARCHAR(36)   NOT NULL,
    url        VARCHAR(2000) NOT NULL,
    events     JSONB         NOT NULL DEFAULT '[]', -- 订阅的事件类型列表
    secret     VARCHAR(500),                        -- HMAC 验签密钥
    status     VARCHAR(20)   DEFAULT 'active',
    owner_id   VARCHAR(36),                        -- 配置该 Webhook 的用户 ID
    owner_name VARCHAR(200),                       -- 配置者名称（冗余）
    created_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    created_by VARCHAR(36),                        -- 创建人 user_id
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_by VARCHAR(36)                         -- 最后修改人 user_id
);
CREATE INDEX idx_webhook_tenant ON agent_webhook(tenant_id);
CREATE INDEX idx_webhook_owner  ON agent_webhook(tenant_id, owner_id);
COMMENT ON TABLE agent_webhook IS '外部系统订阅 Agent 事件的 Webhook 配置';
COMMENT ON COLUMN agent_webhook.owner_id IS '配置该 Webhook 的用户，具备管理权限';
```

---

## 14. 语言实现参考

> 本章提供 **Go（Eino）** 和 **Python（LangGraph / LangChain）** 两种实现参考。  
> 两者面向相同的接口设计，可独立使用，也可混合部署（通过 HTTP API 互调）。

---

### 14.1 工程模块结构（架构分层）

目录建议保持为**稳定骨架 + 域内演进**，不要把文档写成需要跟着每个文件增删一起维护的“实时文件清单”。顶层只约束稳定职责边界，能力域内部目录允许按实际实现逐步细化。

```text
agent-runtime/
  cmd/                  # 进程入口：api / agent-cli / worker / desktop
  internal/
    bootstrap/          # 依赖装配根：读取配置、选择实现、组装系统
    app/                # 少量跨能力域应用编排；不继续扩张为大杂烩
    domain/             # 核心领域对象与稳定模型
    platform/           # 通用基础设施与平台底座：config / db / cache / idgen / logger / httpclient
    runtime/            # Agent Runtime 内核：action / context / stop / strategy / session
    capabilities/       # 能力域：llm / tool / memory / sandbox-client / trace / usage / profile
    interfaces/         # 外部接入面：http / cli / sse / websocket / webhook / desktop
  api/                  # OpenAPI / protobuf / SDK 契约产物
  configs/              # 默认配置与环境样例
  docs/                 # 架构与设计文档
  deploy/               # 部署资源
  scripts/              # 开发与运维脚本
  web/                  # 前端控制台
```

补充原则：

- `bootstrap` 保持顶层，不下沉到某个能力域。
- `app` 只放跨域 orchestration；能力内部服务优先放入对应 `capabilities/*`。
- `platform` 放跨域复用底座，不承载具体业务能力。
- `runtime` 只负责 Agent 运行时，不直接耦合具体 LLM、Tool、MCP、Sandbox、HTTP 框架或数据库实现。
- `capabilities` 作为能力聚合层，内部可按 `contract / adapter / service / model` 继续细分，但不要求本文档逐项枚举。
- `interfaces` 表示系统对外接入面，比单纯 `transport` 更贴近当前职责。
- 沙箱服务已拆分为独立仓库时，主平台内仅保留 `sandbox-client` 相关契约与适配，不承载沙箱服务实现本体。

#### 14.1.1 核心依赖方向

```text
interfaces → app → runtime → domain
bootstrap  → app / runtime / capabilities / platform
capabilities → domain / platform
```

约束：

- `domain` 不依赖外部框架，不依赖 `platform / capabilities / interfaces` 的具体实现。
- `runtime` 只依赖稳定模型与抽象，不直接依赖 Eino、OpenAI、具体工具实现、MCP Client、Docker/WASM 或 HTTP/CLI。
- `capabilities` 负责承接具体能力的契约、适配与域内服务，避免再把能力实现拆散回多个全局顶层目录。
- `platform` 只放跨域复用基础设施，如配置、日志、ID、数据库、缓存、HTTP Client、可观测性等。
- `interfaces` 只做协议转换、参数校验、响应序列化与流式输出，不承载核心业务逻辑。
- `bootstrap` 是唯一集中装配实现的位置。

#### 14.1.2 能力域的内部组织建议

推荐把能力相关代码尽量聚拢到同一域内，例如：

```text
capabilities/
  llm/
    contract/
    adapter/
    service/
    model/
  tool/
    contract/
    adapter/
    service/
    model/
  memory/
    contract/
    adapter/
    service/
    model/
  sandbox/
    contract/
    adapter/
    service/
```

这样做的目标不是强制统一层数，而是降低一个能力横跨多个顶层目录带来的认知成本。后续新增字段、实现或子目录时，只要不突破顶层职责边界，就不要求同步更新本文档。

#### 14.1.3 Tool / MCP / Code 沙箱边界

Tool、MCP、代码执行都可能依赖沙箱，但三者仍应保持各自的调用边界：

```text
runtime/action
  → Tool Capability      → builtin / http callback / registry
  → MCP Capability       → mcp client / server adapter
  → Code Execution       → sandbox client
  → Agent Delegation     → sub-agent / agent-as-tool

capabilities/sandbox
  → lease / exec / logs / file / policy mapping

治理与权限
  → capability scope
  → permission / approval / intervention
  → sandbox policy
```

这样文件系统工具、MCP filesystem server、coding skill、shell 执行都可以复用统一的沙箱能力，但各自仍保留独立的能力入口、审计策略和授权策略。

---

### 14.2 Go 实现（推荐用于独立部署 / 高并发场景）

#### 工程结构

Go 侧推荐维护“稳定顶层骨架 + 域内逐步细化”的结构，不再在设计文档里维护到每个子目录或文件名；只要保持顶层职责边界与依赖方向一致，域内可以按实现自然生长。

```text
genesis-agent/
  cmd/
    agent/
    api/
    worker/
    desktop/

  internal/
    bootstrap/
    app/
    domain/
    platform/
    runtime/
    capabilities/
    interfaces/

  api/
  configs/
  docs/
  deploy/
  scripts/
  web/
```

推荐理解方式：

- `bootstrap`：系统装配与入口依赖组装。
- `app`：极少量跨域用例编排。
- `domain`：稳定核心模型。
- `platform`：日志、配置、数据库、缓存、HTTP Client、可观测性等通用底座。
- `runtime`：Agent Loop、状态机、停止条件、上下文装配等核心运行时。
- `capabilities`：LLM、Tool、Memory、Trace、Sandbox Client、Usage、Profile 等能力域。
- `interfaces`：HTTP、CLI、SSE、Webhook、Desktop 等外部入口。

说明：

- 新增文件或子目录时，只要仍属于上述顶层职责边界，就不要求同步修改本文档。
- 优先保证“目录语义稳定”，而不是追求文档与代码的逐文件一一镜像。
- 若某个能力后续独立为模块或独立服务，应优先通过 `capabilities/*/contract + adapter` 与主运行时解耦。

#### Eino 适配器（Go）

Eino（字节跳动 CloudWego）作为**底层适配器**使用，不影响上层核心抽象。

| Eino 组件 | 在 Runtime 中挂载的位置 | 说明 |
|---|---|---|
| `components/model.ChatModel` | `capabilities/llm/adapter` + `runtime/strategy` | 由 LLM 适配层承接，运行时策略负责编排调用 |
| `components/tool.InvokableTool` | `capabilities/tool/adapter` | 实现内置工具与工具适配层 |
| `components/retriever.Retriever` | `capabilities/memory` 或独立 `rag` 能力域 | 单次检索调用，可按项目规模独立成能力域 |
| `components/embedding` | `capabilities/memory/adapter` | 生成记忆或知识向量 |
| `flow/agent.ReActAgent` | 参考实现 | 可作为 `ReactLoopStrategy` 初版参考，但不直接对外暴露 |
| `compose.Graph` | `runtime/strategy` | Plan-Execute DAG 步骤调度（静态图适合此场景） |

**不使用 Eino 的地方：**

- **ReAct 主循环**：Eino Graph 是静态 DAG，动态 while 循环自己写
- **Session / Task / Event 状态**：存自己的 DB，不托管给 Eino
- **对外 API 入口**：通过 `RunEngine` 接口对外，不直接暴露 Eino 对象

**ChatModel 适配示例：**

```go
// 包装 Eino ChatModel，注入 Token 计量
type einoChatModelAdapter struct {
    model model.ChatModel
    meter UsageMeter
}

func (a *einoChatModelAdapter) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
    resp, err := a.model.Generate(ctx, convertMessages(req.Messages))
    if err != nil {
        return nil, err
    }
    usage := resp.ResponseMeta().Usage
    _ = a.meter.Record(ctx, TokenUsage{
        RunID:            req.RunID,
        StepID:           req.StepID,
        PromptTokens:     usage.PromptTokens,
        CompletionTokens: usage.CompletionTokens,
    })
    return &ChatResponse{
        Content:   resp.Content,
        ToolCalls: convertToolCalls(resp.ToolCalls()),
    }, nil
}
```

---

### 14.3 Python 实现（推荐用于与现有 Python 平台集成）

#### 工程结构

```text
agent_runtime/
  main.py              # FastAPI 应用入口
  worker.py            # Celery Worker 入口

  runtime/
    engine.py          # RunEngine 实现（asyncio）
    loop_strategy.py   # ReAct Loop 策略
    plan_strategy.py   # Plan-Execute 策略
    coding_strategy.py # Coding Loop 策略
    stop_controller.py
    state_persister.py

  agent/
    model.py           # Pydantic 数据模型
    registry.py
    loader.py

  session/
    model.py / service.py

  task/
    model.py / queue.py / worker.py / scheduler.py

  event/
    model.py / bus.py / dispatcher.py

  tool/
    provider.py        # ToolProvider Protocol
    registry.py
    gateway.py         # Tool Gateway（授权 + 执行 + 沙盒）
    external.py        # 外部 HTTP 工具适配器
    sandbox.py

  rag/
    executor.py        # AgenticRAGExecutor
    parallel.py        # asyncio.gather 并行检索
    evaluator.py

  mcp/
    client.py / gateway.py

  skill/
    model.py / registry.py / matcher.py / hook.py

  approval/
    engine.py / notifier.py

  memory/
    store.py           # MemoryStore Protocol
    pg_store.py        # SQLAlchemy + pgvector
    vector_store.py    # Qdrant / Weaviate

  protocol/
    action.py / agent_caller.py

  guard/
    policy.py / watcher.py

  prompt/
    builder.py

  usage/
    meter.py

  trace/
    tracer.py          # opentelemetry-sdk 包装

  auth/
    api_key.py / session_auth.py / oauth.py

  webhook/
    dispatcher.py

  api/v1/
    runs.py / approvals.py / events.py / tools.py / webhooks.py / stream.py
```

#### LangGraph / LangChain 适配器（Python）

Python 侧可选择性引入 LangGraph / LangChain 组件，同样作为**底层适配器**，不影响上层接口。

| 组件 | 在 Runtime 中挂载的位置 | 说明 |
|---|---|---|
| `langchain_core.language_models.ChatModel` | `runtime/loop_strategy.py` | LLM 调用，包装后记录 Token |
| `langchain_core.tools.BaseTool` | `tool/provider.py` | 实现内置工具（适配 ToolProvider Protocol） |
| `langchain_core.retrievers.BaseRetriever` | `rag/parallel.py` | 单次检索调用 |
| `langchain_core.embeddings.Embeddings` | `memory/vector_store.py` | 记忆向量生成 |
| `langgraph.graph.StateGraph` | `runtime/plan_strategy.py` | Plan-Execute DAG 步骤调度（静态图适合此场景） |
| `langchain_community.vectorstores` | `memory/vector_store.py` | pgvector / Qdrant 向量存储实现 |

**不使用 LangGraph 的地方：**

- **ReAct 主循环**：`StateGraph` 是静态图，ReAct 动态循环用 `asyncio` while 循环自己写
- **Session / Task / Event 状态**：存自己的 DB（SQLAlchemy），不托管给 LangGraph 的 checkpointer
- **对外 API 入口**：通过 `RunEngine` Protocol 对外，不直接暴露 LangGraph 的 compiled graph

**ChatModel 适配示例（Python）：**

```python
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import HumanMessage, SystemMessage

class LangChainChatModelAdapter:
    """包装 LangChain ChatModel，注入 Token 计量"""

    def __init__(self, model: BaseChatModel, meter: UsageMeter):
        self._model = model
        self._meter = meter

    async def chat(self, req: ChatRequest) -> ChatResponse:
        messages = convert_messages(req.messages)
        resp = await self._model.ainvoke(messages)

        # 记录 Token 消耗
        usage = resp.usage_metadata or {}
        await self._meter.record(TokenUsage(
            run_id=req.run_id,
            step_id=req.step_id,
            prompt_tokens=usage.get("input_tokens", 0),
            completion_tokens=usage.get("output_tokens", 0),
        ))

        return ChatResponse(
            content=resp.content,
            tool_calls=convert_tool_calls(resp.tool_calls),
        )
```

**Plan-Execute 用 LangGraph StateGraph 示例：**

```python
from langgraph.graph import StateGraph, END

# Plan-Execute 的步骤图是静态 DAG，适合 LangGraph
def build_plan_execute_graph(engine: RunEngineImpl) -> CompiledGraph:
    graph = StateGraph(PlanExecuteState)
    graph.add_node("planner", engine.planner.plan)
    graph.add_node("executor", engine.action_executor.execute)
    graph.add_node("replanner", engine.replanner.replan)
    graph.add_edge("planner", "executor")
    graph.add_conditional_edges(
        "replanner",
        should_continue,         # 判断是否继续还是结束
        {"continue": "executor", "end": END},
    )
    graph.add_edge("executor", "replanner")
    graph.set_entry_point("planner")
    return graph.compile()
    # 注意：LangGraph checkpointer 不使用，状态由 StatePersister 存自己的 DB
```

---

### 14.4 两种实现的选型对比

| 维度 | Go + Eino | Python + LangGraph |
|---|---|---|
| **适合场景** | 独立部署、高并发、低延迟要求 | 与 Python 平台集成、快速迭代 |
| **并发模型** | goroutine，天然高并发 | asyncio，I/O 密集型够用 |
| **与现有 Python 代码集成** | 跨语言 HTTP/gRPC 调用 | 直接 import，无额外开销 |
| **框架生态** | Eino（成熟但相对小众） | LangChain（生态最大） |
| **维护成本** | 单独维护一套 Go 服务 | 与 Python 项目共享工具链 |
| **扩展 AI 生态（Hugging Face 等）** | 需桥接 | 天然支持 |
| **部署复杂度** | 多语言多服务 | 单语言单服务 |
| **主循环实现** | while 循环 + goroutine | asyncio while + asyncio.gather |

**决策建议：**

```text
现有项目是 Python 平台，需要深度集成 → Python + LangGraph 部分组件
全新项目，需要高并发独立服务 → Go + Eino
两者都要 → 先 Python 实现 MVP，接口设计对齐，后续可迁移 Go 实现同一接口
```

---

## 15. 分期实施计划

> 本路线图以“当前代码真实阶段”为准，文档中的目标架构按阶段逐步落地。Phase 1A/1B 聚焦 Agent Runtime 核心闭环、基础记忆和生产骨架；RBAC、SaaS 多级限流、全链路异步、缓存/并发优化放到 Phase 2.5，避免在核心 Loop 尚未稳定前引入过多平台复杂度。

### Phase 1A：当前核心闭环

```text
□ CLI / HTTP API
□ ReAct Loop
□ LLM Provider 适配
□ Tool Registry / Builtin Tool
□ ShortTermMemory 接口 + 文件存储实现
□ LongTermMemory 接口 + 基础文件存储实现
□ UserProfileStore 接口 + 基础文件存储实现
□ 基础 Trace / Log
□ 基础 PromptBuilder
```

目标：先让单次 Run 可以稳定完成“用户输入 → LLM 推理 → 工具调用 → 最终回答”的同步闭环，并具备基础短期记忆、长期记忆和用户画像能力。此阶段允许使用文件存储和轻量 Trace，但必须通过接口注入，后续可替换为 DB、Redis、向量库或外部画像服务。

### Phase 1B：Agent Runtime 生产骨架

```text
□ Tool Gateway
□ 工具权限策略：auto / confirm / approval / deny
□ 人工干预：确认、审批、用户输入
□ Run 状态持久化
□ SSE 实时反馈
□ Session / Message 持久化
□ 记忆/画像管理 API：查看、编辑、删除、清空
□ 记忆写入策略：显式保存、用户纠正、基础 evidence
□ Token Usage 统计
□ Hook 扩展点
□ Skills 基础机制
```

目标：补齐生产级 Run Engine 的状态、权限、干预、可观测性和记忆治理骨架。Tool/MCP/Skill 的能力适用范围在此阶段开始落模型与执行前校验；记忆和画像需要支持用户可见、可编辑、可删除，但完整 RBAC、多级限流和审计报表仍放到 Phase 2.5。

### Phase 2：高级 Agent 能力

```text
□ Agentic RAG
□ Plan-Execute
□ Sub Agent / Agent-as-Tool
□ Sandbox
□ 文件系统权限
□ 高级 Memory System：向量检索、衰退、合并、冲突处理
□ Skill Candidate：从记忆 / Trace / 成功 Run 沉淀候选 Skill
□ 桌面应用接入
```

目标：让 Runtime 支持更复杂任务，包括检索增强、计划执行、子 Agent 调用、代码/文件系统类能力和桌面端场景。Sandbox 在此阶段作为 Tool / MCP / Code Executor 的统一隔离能力落地。

### Phase 2.5：平台化治理能力

```text
□ RBAC
□ 租户/用户/项目级权限
□ API 限流
□ Agent 限流
□ 工具限流
□ Token 限流
□ 套餐有效期
□ 并发数控制
□ 缓存/并发优化
□ 全链路异步
```

目标：从“单 Runtime 能力”升级为“SaaS 平台治理能力”。限流维度包括全局平台、租户、用户、项目，并覆盖模型级、工具级、Agent 级和 API 级；限流策略包括并发数、套餐有效期、Token 用量、API 调用总次数、单位时间窗口次数，以及工具数量、Token 数量、Skills 数量等数量限制。

全链路异步在此阶段引入：Run/Task/Event 持久化、Worker、重试、幂等、取消、恢复、死信和调度器统一设计。Phase 1B 可先提供 SSE 实时反馈，但不强制所有链路异步化。

### Phase 3：多 Agent 协作空间

```text
□ Workspace / MessageBus
□ @Agent 路由
□ Supervisor
□ Parallel Fan-out
□ Reducer
□ Handoff
□ Watcher / Safety Guard
```

目标：实现企业级多 Agent 协作空间，使人和 Agent 可以在 Workspace 中通过 @Agent、任务委派、并行协作、结果归约和安全观察者完成复杂项目协作。

---

## 附：核心接口一览（语言无关）

```text
========== 核心执行 ==========
RunEngine
  start(request)           → Run
  resume(run_id, event)    → void
  pause(run_id, reason)    → void
  cancel(run_id)           → void
  get_state(run_id)        → RunState

========== 工具与权限 ==========
ToolProvider              工具实现（definition / execute / requires_sandbox）
ToolRegistry              工具注册表（register / unregister / get / list）
PermissionChecker         动态权限检查（check → PermissionResult）
  └── 实现：StaticRuleChecker / APIPermissionChecker / DynamicRiskChecker / CachedPermissionChecker
Sandbox                   工具沙盒（execute）

========== 人工干预 ==========
InterventionRequest       五种类型：tool_approval / tool_confirmation / user_input_required /
                          manual_override / step_by_step
                          统一由 InterventionEngine 处理，通过 SSE 推送给前端

========== Skill ==========
SkillHook                 干预 Loop 四个钩子（on_run_start / on_step_start / on_step_complete / on_run_complete）
SkillMatcher              自动匹配 Skill（match）

========== 记忆系统（分层，各自独立可替换） ==========
WorkingMemory             RunContext 内存字典，不持久化
ShortTermMemory           对话历史（append / get_recent / summarize_and_compress）
  └── 实现：FileShortTermMemory（Phase 1）/ DBShortTermMemory / RedisShortTermMemory
LongTermMemory            跨 Session 语义记忆（save / search / list / update / delete / decay）
  └── 实现：FileLongTermMemory（Phase 1）/ PgVectorLongTermMemory / QdrantLongTermMemory / ChromaLongTermMemory
UserProfileStore          用户画像（get / update / list_fields / delete_field）
  └── 实现：FileUserProfileStore（Phase 1）/ DBUserProfileStore / APIUserProfileStore
StaticContextProvider     注入式实时上下文（get_context）
  └── 实现：ConfigStaticContext / DBStaticContext / APIStaticContext / CompositeStaticContext
MemoryConsolidator        记忆整合（extract_memories / merge_similar / expire_stale）

========== 提示词 ==========
PromptBuilder             系统提示词构建（build_system）

========== 可观测性 ==========
Logger                    日志（info / warn / error / debug）
Tracer                    OTel Trace（start_span）- 兼容 Jaeger / Datadog / SkyWalking
UsageMeter                Token 计量（record / get_run_usage / check_budget）

========== 鉴权 ==========
AuthorizationProvider     API 鉴权（authenticate / authorize）
  └── 实现：APIKeyAuth / SessionAuth / OAuthAuth / HMACAuth

========== 事件 ==========
EventHandler              事件订阅（event_types / handle）- 异步，不阻塞 Loop

========== RAG ==========
AgenticRAGExecutor
  execute(request)         → RAGSearchResult
  └── 内部：keyword_extract → parallel_retrieve → evaluate_coverage → loop（受 max_rag_rounds 控制）

========== Agent 协作协议 ==========
AgentProtocol
  call(request)            → AgentCallResult      # Agent-as-Tool
  delegate(request)        → Task                 # 委派任务
  handoff(request)         → void                 # 交接控制权
  fanout(requests)         → list[Task]            # 并行分发（二期）
  fanin(task_ids)          → AggregatedResult      # 汇总结果（二期）

========== Loop 内部动作类型（ActionType） ==========
think                     LLM 推理步骤
rag_search                Agentic RAG 检索（触发 AgenticRAGExecutor）
tool_call                 工具调用（经 PermissionChecker → InterventionEngine → 执行）
mcp_call                  MCP 调用
agent_call                子 Agent 调用
ask_user                  请求用户澄清（触发 user_input_required 干预）
final_answer              输出最终答案，结束 Loop

========== Run 状态机 ==========
created
  → running
      → waiting_intervention   # 等待人工干预（审批/确认/输入/逐步确认）
           → running            # 干预完成，Resume
      → completed              # 正常完成（含 partial_complete）
      → failed                 # 致命错误
      → cancelled              # 用户取消

Intervention 子状态（waiting_intervention 时）：
  pending_approval          需要管理员审批工具
  pending_confirmation      需要用户确认低风险工具
  pending_user_input        需要用户回答 LLM 的澄清问题
  pending_manual_override   用户已请求手动接管
  pending_step_confirm      逐步模式下等待用户确认下一步

========== 流式事件类型（SSE/WebSocket）==========
run.started / run.completed / run.failed / run.paused / run.cancelled
step.started / step.completed
llm.thinking_token / llm.output_token / llm.completed
tool.calling / tool.result
rag.searching / rag.completed
agent.calling
intervention.required      # 需要人工干预（含 type 和 display_info）
```

---

## 16. Redis 使用策略与最佳实践

> 本章回答：企业级通用 Agent Loop 中，Redis 在哪些环节有价值，哪些环节不应该引入，以及分期落地建议。

### 16.1 整体判断原则

Redis **不是必须的银弹**，引入前必须先问：

```text
这个问题能用 PostgreSQL 解决吗？     → 优先 PG（少一个组件）
这个问题能用进程内内存解决吗？       → 优先内存（更简单）
这个问题需要跨进程/跨实例共享吗？   → 考虑 Redis
这个问题需要亚毫秒响应且高频读写？  → Redis 合适
```

### 16.2 各层 Redis 必要性分析

#### 层 1：Session / 会话状态缓存（✅ Phase 2 引入，价值高）

**问题**：用户每次发消息，都需要加载 Session + AgentInstance + Agent Profile + ShortTermMemory，每次都查 PostgreSQL 有明显延迟。

**Redis 方案**：
```text
Session 缓存：Redis Hash，key = session:{tenant_id}:{session_id}
  - TTL = 30min（会话活跃期自动续期）
  - 存：session 基本信息 + agent_instance_id + 最近 N 条 Message 摘要
  - 写透（Write-Through）：DB 写入成功后同步更新 Redis
  - 读穿（Read-Through）：Redis miss 时从 DB 加载并回写
```

价值：**把每次 Run 启动的 DB 查询从 3~5 次降到 0~1 次**。

---

#### 层 2：SSE 连接与 Run 流式事件分发（✅ Phase 2 引入，多实例场景必须）

**问题**：单实例时 SSE 直接从内存 channel 推送没问题。**多实例部署**时，用户 SSE 连接在实例 A，但 Run 在实例 B 执行，B 无法直接推送给 A 的客户端。

**Redis 方案**：Redis Pub/Sub 或 Redis Streams 作为实例间事件总线：

```text
事件流：stream = run_events:{run_id}
  - Run 执行进程（B）向 stream 写入每个 SSE 事件（step.completed / tool.result 等）
  - SSE 连接进程（A）订阅该 stream，收到事件后推送给客户端
  - 客户端断连后 cursor 保留在 stream 中，重连后可 XREAD 续传历史事件
  - TTL：stream 保留 1h（对应 SSE 断连续流窗口）

替代方案（单实例无需 Redis）：
  - 内存 broadcast channel（sync.Map + channel per run_id）
  - 只要不做多实例水平扩展，进程内方案完全够用
```

**分级选择**：
```text
Phase 1（单实例）：内存 channel，不需要 Redis
Phase 2（多实例）：Redis Pub/Sub（简单）或 Redis Streams（可重放）
```

---

#### 层 3：Run 状态分布式锁（✅ Phase 2 多实例，防并发 Resume）

**问题**：同一个 Run 可能收到多个并发 Resume 事件（如用户快速点击"批准"两次），导致 Run 被重复执行。

**Redis 方案**：
```go
// 分布式锁，防止同一 Run 并发 Resume
lockKey := fmt.Sprintf("run_lock:%s:%s", tenantID, runID)
lock, err := redisClient.SetNX(ctx, lockKey, workerID, 30*time.Second)
if !lock {
    return ErrRunAlreadyResuming
}
defer redisClient.Del(ctx, lockKey)
```

**单实例替代**：Go `sync.Map` + per-run Mutex，完全够用。

---

#### 层 4：Task Queue / 异步 Job 调度（✅ Phase 2.5，长期任务必须）

**问题**：Agent Run 如果是长期任务（几分钟到几小时），不能占用 HTTP 请求线程等待。需要异步提交 + 持久化队列确保服务重启后任务不丢失。

**推荐方案：Asynq（基于 Redis）**

```go
// 提交长期任务到 Asynq 队列
client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})

// 任务类型
const TypeAgentRun = "agent:run"

// 提交
task := asynq.NewTask(TypeAgentRun, payload, asynq.Queue("critical"), asynq.MaxRetry(3))
info, err := client.Enqueue(task)

// Worker 侧消费
mux := asynq.NewServeMux()
mux.HandleFunc(TypeAgentRun, handleAgentRun)
```

**队列分层**（按任务优先级和类型）：

| 队列名 | 权重 | 任务类型 |
|:---|:---:|:---|
| `critical` | 6 | 用户实时对话触发的 Run |
| `default` | 3 | Agent 定时任务、Webhook 触发的 Run |
| `low` | 1 | 后台长期任务、批处理、记忆整合 |

**替代方案（Phase 1 不想引入 Redis）**：
```text
River（基于 PostgreSQL advisory lock）
  - 无需 Redis，利用已有 PG
  - 支持延迟任务、优先级、重试、幂等
  - 吞吐量略低于 Asynq，但 Agent 场景够用
  - 推荐：如果 Phase 1 已有 PG 且不想加 Redis，先用 River 过渡
```

---

#### 层 5：限流计数器（✅ Phase 2.5 平台治理）

**问题**：多租户场景下，需要对 API 调用、Token 消耗、工具调用次数做实时限流，且必须在多实例间共享计数。

**Redis 方案**（sliding window 限流）：

```go
// 租户级 API 限流：每分钟最多 100 次
key := fmt.Sprintf("rate_limit:api:%s:%d", tenantID, time.Now().Minute())
count, _ := redisClient.Incr(ctx, key)
redisClient.Expire(ctx, key, 2*time.Minute) // 2 倍窗口防边界
if count > tenantPlan.MaxAPIPerMinute {
    return ErrRateLimitExceeded
}

// Token 预算限流：每天最多消耗 N Token
tokenKey := fmt.Sprintf("token_budget:%s:%s", tenantID, today)
used, _ := redisClient.IncrBy(ctx, tokenKey, int64(tokensUsed))
redisClient.Expire(ctx, tokenKey, 25*time.Hour)
```

**单实例替代**：内存计数器（`sync/atomic`），但不支持多实例共享。

---

#### 层 6：Agent 心跳 / 在线状态（✅ Phase 3 多 Agent 协作）

**问题**：多 Agent 协作空间中，需要知道哪些 Agent 当前在线/空闲/忙碌，以便 Supervisor 做任务分配。

**Redis 方案**：
```text
key = agent_heartbeat:{tenant_id}:{agent_instance_id}
value = {"status": "idle", "current_run_id": null, "last_seen": "2026-06-30T..."}
TTL = 30s（Agent 每 10s 更新一次，30s 未更新视为离线）
```

---

#### 层 7：幂等去重缓存（✅ Phase 2，防止 Webhook/事件重复触发）

**问题**：Webhook 可能因网络重试发送多次相同事件，必须做幂等去重。

**Redis 方案**：
```go
// 幂等 key：event_dedup:{tenant_id}:{event_id}
key := fmt.Sprintf("event_dedup:%s:%s", tenantID, eventID)
set, _ := redisClient.SetNX(ctx, key, "1", 24*time.Hour)
if !set {
    return nil // 已处理，幂等跳过
}
// 正常处理事件...
```

**替代方案**：在 PostgreSQL `events` 表上加 `UNIQUE(tenant_id, event_id)` 唯一索引，插入失败即重复，无需 Redis。

---

#### ❌ 不需要 Redis 的环节

| 环节 | 原因 |
|:---|:---|
| **ShortTermMemory（对话历史）** | 直接存 PostgreSQL，按 session_id 查询，不高频 |
| **LongTermMemory（语义记忆）** | 向量库（pgvector/Qdrant），不需要 Redis |
| **Run 状态持久化** | PostgreSQL，有事务保证 |
| **Sandbox Pool 内部队列** | 进程内 Go channel + WFQ，单机足够 |
| **Trace / 日志** | 直接写 OTel Collector / 文件，不经 Redis |
| **Tool 配置** | 启动时加载到内存，低频变更 |

---

### 16.3 分期落地建议

```text
Phase 1（当前）：无 Redis
  - 所有状态存 PostgreSQL
  - SSE 走进程内 channel
  - 限流用内存计数器
  - 幂等用 DB 唯一索引
  - 任务调度用 River（基于 PG）或同步执行

Phase 2（多实例 / 高并发）：引入 Redis
  ├─ Session 缓存（减少 DB 查询延迟）
  ├─ SSE 多实例事件分发（Redis Pub/Sub 或 Streams）
  ├─ 分布式锁（防并发 Resume）
  └─ 幂等去重（可选，DB 索引也够）

Phase 2.5（平台治理）：Redis 扩展使用
  ├─ 任务队列切换 Asynq（从 River 迁移，获得更好的 UI 和监控）
  └─ 多维度限流计数器（API / Token / Tool 维度）

Phase 3（多 Agent 协作）：Redis Streams 全面使用
  ├─ Agent 心跳 / 在线状态
  └─ 多 Agent 协作事件总线（MessageBus）
```

### 16.4 Redis 引入后的整体架构

```text
┌─────────────────────────────────────────────────────────┐
│  接入层：HTTP API · WebSocket · Webhook · CLI            │
└──────────────────────────┬──────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────┐
│  调度层                                                   │
│  ┌─────────────────┐   ┌──────────────────────────────┐ │
│  │ Task Queue       │   │ Event Bus                    │ │
│  │ Asynq (Redis)   │   │ Redis Pub/Sub / Streams      │ │
│  │ 或 River (PG)   │   │ (Phase 2 多实例 SSE 分发)    │ │
│  └────────┬────────┘   └──────────────┬───────────────┘ │
└───────────┼──────────────────────────┼─────────────────┘
            ↓                          ↓
┌─────────────────────────────────────────────────────────┐
│  执行层：Run Engine                                       │
│  ┌───────────────────────────────────────────────────┐  │
│  │ Redis 使用点                                       │  │
│  │  ├─ Session Cache（减少 DB 查询）                  │  │
│  │  ├─ 分布式锁（防并发 Resume）                     │  │
│  │  ├─ 限流计数器（Token / API / 工具）               │  │
│  │  └─ 幂等去重（Webhook 防重）                      │  │
│  └───────────────────────────────────────────────────┘  │
└──────────────────────────┬──────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────┐
│  基础设施                                                 │
│  PostgreSQL（状态/记忆/审计）  Redis（缓存/队列/锁）      │
│  向量库（LongTermMemory）      对象存储（文件/Artifact）   │
└─────────────────────────────────────────────────────────┘
```

### 16.5 Redis 数据 Key 命名规范

统一前缀格式：`{模块}:{tenant_id}:{实体}` 或 `{模块}:{tenant_id}:{实体}:{id}`

```text
# Session 缓存
session:{tenant_id}:{session_id}                  Hash，TTL=30min

# Run 事件流（SSE 多实例分发）
run_events:{tenant_id}:{run_id}                   Stream，TTL=1h

# 分布式锁
run_lock:{tenant_id}:{run_id}                     String，TTL=30s
agent_lock:{tenant_id}:{agent_instance_id}        String，TTL=30s

# Agent 心跳
agent_heartbeat:{tenant_id}:{agent_instance_id}   Hash，TTL=30s

# 限流计数器
rate_limit:api:{tenant_id}:{minute_bucket}        String，TTL=2min
rate_limit:token:{tenant_id}:{day_bucket}         String，TTL=25h

# 幂等去重
event_dedup:{tenant_id}:{event_id}                String，TTL=24h
job_dedup:{tenant_id}:{idempotency_key}           String，TTL=1h

# 任务队列（Asynq 内部管理，无需手动命名）
# Asynq 自动管理：asynq:{queue_name}:* 系列 key
```

### 16.6 Redis 不引入时的降级方案

整个系统必须能在**无 Redis 的情况下完整运行**（Phase 1 目标），所有 Redis 能力都有降级实现：

| Redis 能力 | 降级方案 | 性能损耗 |
|:---|:---|:---|
| Session 缓存 | 每次从 PostgreSQL 查询 | +5~20ms/次，可接受 |
| SSE 多实例分发 | 限制单实例部署 | 无法水平扩展 |
| 分布式锁 | 进程内 `sync.Mutex` | 单实例可靠 |
| 任务队列 | River (PG) 或同步执行 | 长任务阻塞 HTTP |
| 限流计数器 | 内存原子计数 | 多实例计数不共享 |
| 幂等去重 | DB 唯一索引 | 额外 DB 写入 |

所有接口通过依赖注入组装，`bootstrap/` 根据配置决定是否加载 Redis 实现：

```go
// bootstrap/redis.go
func InitRedis(cfg *config.Config) (RedisClient, error) {
    if !cfg.Redis.Enabled {
        return &NoopRedisClient{}, nil // 无 Redis 时返回空实现，不影响代码逻辑
    }
    return redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr}), nil
}
```

```yaml
# config.yaml
redis:
  enabled: false   # Phase 1 默认关闭
  addr: "localhost:6379"
  db: 0
  password: ""
  # pool_size: 20
  # min_idle_conns: 5
```






