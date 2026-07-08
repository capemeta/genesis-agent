# 启元AI集成架构设计

本文档说明“启元 Agent Go Runtime”和既有“启用 AI 平台 Python 后端”的集成策略。目标是在不立刻重写低代码平台的前提下，让 Agent Loop 成为低代码编排画布中的标准节点，同时让 Agent Runtime 能复用低代码平台已有的 RAG、工作流、工具、Skills、MCP、多租户和权限能力。

## 一、背景与核心问题

当前存在两套相关系统：

- **启用 AI 平台**：既有 Python 后端，类似 Dify，但加强了多租户、权限、低代码编排、RAG 知识库、工具和工作流能力。
- **启元 Agent**：当前 Go 仓库，重点建设通用 Agent Runtime，分为个人版 CLI、个人版 Desktop、企业版 Web，核心是可替换策略的 Run Engine / Agent Loop。

两者能力天然交叉：

- 启用 AI 平台的低代码画布需要一个强 Agent Loop 节点。
- 启元 Agent 的 Tool / Skill / MCP / Memory / Sandbox / Trace / Usage 能力也需要企业级治理。
- Agent Loop 需要能调用启用 AI 平台已有工作流。
- 启用 AI 平台已有工具、Skills、MCP、RAG 知识库也应该能被 Agent Loop 使用。

真正的问题不是“全部用 Go 还是全部用 Python”，而是：**如何让 Agent Runtime 成为统一运行面，同时不破坏既有低代码平台资产和多租户治理体系。**

## 二、第一性原理分析

### 2.1 用户真正需要什么

最终用户和平台使用者需要的是：

- 在低代码画布中拖拽一个 Agent Loop 节点，像调用普通工作流节点一样调用它。
- Agent Loop 可以安全调用知识库、工具、MCP、Skills、外部系统和低代码工作流。
- 同一租户、用户、项目、角色下的权限、密钥、审计和用量策略保持一致。
- CLI、Desktop、Enterprise、低代码平台调用同一套核心 Agent 行为，避免同一任务在不同入口表现不一致。
- 未来可以逐步迁移或替换模块，而不是一次性重写全部系统。

### 2.2 必须成立的不变量

- Agent Loop / Run Engine 是核心运行能力，不能在 Go 和 Python 中长期维护两套等价实现。
- 低代码画布、工作流定义、RAG 知识库、多租户权限是既有平台资产，不能因为 Agent Runtime 重建而被迫一次性重写。
- 企业版权限不能只靠前端隐藏按钮或共享数据库表，必须在运行时请求链路上携带并校验 `tenant_id`、`user_id`、`project_id`、`roles/scopes`。
- Tool / Skill / MCP / Workflow 的调用必须经过统一治理入口，不能让 Agent 自行绕过低代码平台或沙箱策略直接访问 DB、宿主机或外部系统。
- CLI/Desktop/Enterprise 是不同接入面，不应改变 `genesis-agent` 的产品边界：产品装配放在 `products/<product>/bootstrap`，通用能力放在 `internal/capabilities` 或 `internal/runtime`。

### 2.3 失败条件

以下情况会导致架构失败：

- 为了复用权限，Go 和 Python 直接大量共享业务表，导致两个服务都能写同一业务状态，后续 schema 演进互相锁死。
- 企业版 Agent Loop 改用 Python 实现，Go 只保留 CLI/Desktop，导致 Runtime 行为、Tool 语义、Trace、Memory、审批、Sandbox 双轨分裂。
- 立刻把启用 AI 平台后端全部重写成 Go，迁移范围失控，低代码、RAG、多租户、权限、工作流、工具生态一起进入长周期重写。
- Agent Loop 节点只是一个“HTTP 请求节点包装”，没有事件流、取消、恢复、审批、Trace、Usage、权限透传，无法成为生产级编排节点。

## 三、核心结论

推荐采用 **Go 启元 Agent 作为统一 Agent Runtime 服务，Python 启用 AI 平台保留低代码控制面与 RAG 能力，双方通过标准 API、事件流和能力网关集成**。

```text
启用 AI 平台（Python）
  - 多租户 / 权限 / 用户 / 组织 / 项目
  - 低代码画布 / 工作流编排 / App 管理
  - RAG 知识库 / 文档解析 / 向量检索
  - 既有工具、Skills、MCP、连接器
  - 新增 Agent Loop 节点，调用 Go Runtime

启元 Agent（Go）
  - Run Engine / Agent Loop / ReAct / Plan-Execute / Coding / RAG Strategy
  - Tool Gateway / Skill Runtime / MCP Adapter / Memory / Trace / Usage
  - CLI / Desktop / Enterprise 三个产品接入面
  - 暴露 Enterprise Runtime API、SSE 事件流、取消、恢复、审批接口
  - 将低代码工作流、RAG、Python 工具网关视为外部能力
```

一句话：**统一运行时，不急于统一全部后端语言。**

## 四、为什么不建议另外两条路

### 4.1 不建议立刻把低代码平台后端全部切换到 Go

这条路的优点是长期技术栈更统一，但当前阶段风险过大：

- 低代码画布、工作流执行器、RAG、权限、多租户、连接器、工具生态都会被卷入迁移。
- 迁移过程中 Go Runtime 的核心建设会被平台搬迁稀释。
- Python RAG 和文档解析生态仍然有现实优势，即使迁移也不可能完全消除跨语言调用。
- 短期很难带来用户可见收益，反而增加不可用窗口。

更合理的方式是先做运行面集成，后续按模块选择性迁移。

### 4.2 不建议企业版 Agent Loop 用 Python 重新实现

这条路短期看起来能贴近低代码平台，但会损害核心资产：

- CLI/Desktop/Enterprise 将出现两套 Agent Loop 行为。
- Tool / Skill / MCP / Memory / Trace / Usage / Approval / Sandbox 的语义需要双实现或双适配。
- Go 仓库的产品分发架构会被削弱，企业版不再复用同一个 Run Engine。
- 长期测试和排障复杂度显著上升。

企业版应该是 Go Runtime 的企业接入面，而不是另一套 Python Agent Runtime。

## 五、目标分层

建议把两套系统按控制面、运行面、知识面和执行隔离面拆分。

```text
Control Plane 控制面
  启用 AI 平台 Python
  - 租户、用户、组织、角色、权限
  - App / 应用、低代码画布、工作流定义、发布、版本
  - 企业管理端、审计查询、业务配置

Runtime Plane 运行面
  启元 Agent Go
  - Run Engine、Agent Loop、策略执行
  - Tool Gateway、Skill Runtime、MCP 调用、Human Approval
  - Trace、Usage、Memory、事件流、取消、恢复

Knowledge Plane 知识面
  初期由启用 AI 平台 Python 承载
  - 知识库、文档解析、切分、向量检索、召回重排
  - Go Runtime 通过 RAG Tool / Retriever API 调用

Isolation Plane 执行隔离面
  genesis-sandbox 独立服务
  - Code 节点、Tool/Skill 脚本、命令执行、浏览器或文件处理
  - 按租户、项目、运行环境和风险策略隔离
```

这个拆分允许不同语言在不同平面上发挥优势，同时避免核心 Agent Loop 分裂。

### 5.1 长期运行时统一策略

长期目标不是让 Go 只执行 Agent Loop 节点，而是让 **Go 统一承载运行时执行面**。Python 可以继续作为控制面、配置面和知识库管理面存在。

```text
Python 启用 AI 平台
  - 画布编辑、节点配置、工作流定义管理
  - Skill / MCP / Tool 配置管理和发布
  - App、租户、用户、角色、权限、管理后台
  - RAG 知识库管理、文档解析、向量索引管理

Go 启元 Runtime
  - Workflow Executor / DAG 状态机
  - Agent Run Engine / Agent Loop
  - Tool Gateway / Skill Runtime / MCP 调用
  - Sandbox 调度、审批、Trace、Usage、事件流
  - 长任务恢复、取消传播、并发调度、幂等和重试
```

分阶段边界：

- **短期**：Python Workflow Executor 继续执行普通节点；只有 `AgentLoop` 节点远程调用 Go Runtime。
- **中期**：Go 增加 Workflow Runtime。Python 仍保存画布和工作流定义，发布后把版本化 DAG 提交给 Go 执行。
- **长期**：Go 统一执行 Workflow、Agent、Tool、Skill、MCP、Sandbox 等运行时动作；Python 是否继续承载控制面，取决于维护成本和产品演进，不与 Runtime 迁移绑定。

这样可以先打通业务闭环，再把最需要一致性的执行语义逐步收敛到 Go，避免一次性重写低代码平台。

## 六、核心集成链路

### 6.1 低代码平台调用 Agent Loop

低代码画布新增 `AgentLoop` 节点。节点不直接嵌入 Go 代码，而是调用 Go Runtime API。

```text
Workflow Executor（Python）
  -> AgentLoop Node
  -> POST /enterprise/v1alpha/agent-runs
  -> Go Run Engine
  -> SSE / Webhook / Poll 返回事件和最终结果
  -> Workflow Executor 继续执行后续节点
```

节点输入建议包括：

```text
tenant_id
user_id
project_id
app_id
workflow_id
workflow_run_id
node_id
prompt / instruction
input_variables
files / resources
allowed_tools
allowed_skills
allowed_mcp_servers
memory_scope
sandbox_profile
approval_policy
stream_profile
idempotency_key
```

节点输出建议包括：

```text
run_id
status
final_message
structured_output
artifacts
tool_results_summary
trace_id
usage
error_code
```

最低可用版本可以先只支持同步等待或轮询；生产版本必须支持事件流、取消、超时、审批和可恢复执行。

### 6.2 Agent Loop 调用低代码工作流

Go Runtime 中注册一个受治理的工具，例如：

```text
workflow.call
```

它通过启用 AI 平台 API 调用指定工作流。

```text
Agent Run（Go）
  -> Tool Gateway
  -> workflow.call
  -> Policy / Permission / Credential Resolver
  -> Python Workflow API
  -> Workflow Run
  -> 返回结构化结果给 Agent
```

调用必须满足：

- 工作流可见性由 `tenant_id + user_id + project_id + roles/scopes` 裁决。
- 工作流输入 schema 必须在工具声明中暴露给模型，但敏感参数和凭据不得暴露给模型。
- 工作流运行结果需要截断、脱敏并记录 Trace。
- 调用必须支持超时、幂等键和取消传播。

### 6.3 Agent Loop 使用启用 AI 平台 RAG

RAG 不应通过共享向量库表直接耦合。第一阶段建议由 Python 平台暴露 Retriever API，Go Runtime 把它作为工具或 Retriever Provider。

```text
Agent Run
  -> rag.search / rag.retrieve
  -> Python Knowledge API
  -> 权限过滤后的 chunks / citations / metadata
  -> Agentic RAG Strategy 继续推理
```

要求：

- 检索请求必须携带租户、用户、项目、知识库、角色上下文。
- Python 侧负责知识库 ACL、文档级权限、向量召回和重排。
- Go 侧负责把检索结果纳入 Context Window、Trace、Usage 和回答引用。
- 不允许 Go Runtime 绕过 Python 权限直接查共享向量表。

### 6.4 Tool / Skill / MCP 互通

短期不要强行迁移启用 AI 平台已有工具生态。建议增加一个 Python Tool Gateway Adapter：

```text
Go Tool Gateway
  -> Native Go Tools
  -> MCP Tools
  -> Skill Runtime
  -> Python Tool Gateway Adapter
       -> 启用 AI 平台已有工具 / Skills / MCP / 连接器
```

原则：

- 工具声明、输入 schema、LLM-visible 参数、runtime-only 参数、credential ref 必须清晰分离。
- 凭据由所属平台或统一 Credential Resolver 管理，模型只看到必要的安全参数。
- 所有外部调用都进入审计、用量、租户限流和策略裁决。
- 后续高频、通用、与 Agent Runtime 强相关的工具可以逐步 Go 原生化。

## 七、数据与权限边界

### 7.1 不把共享数据库当成主要集成方式

可以共享稳定身份和治理基础表，但不能用“两个系统直接读写同一业务表”替代 API 契约。

适合共享或映射的基础数据：

```text
tenant
user
organization
role
permission
project
app
credential_ref
```

不建议直接双写共享的运行时业务数据：

```text
agent_runs
workflow_runs
tool_calls
trace_spans
memory_items
knowledge_chunks
approval_requests
```

这些数据必须明确 owner：

| 数据 | 推荐 owner | 说明 |
| --- | --- | --- |
| 租户、用户、角色、项目 | 启用 AI 平台 / 企业控制面 | Go 通过 JWT、内部 token、同步表或只读 API 使用 |
| 工作流定义和工作流运行 | 启用 AI 平台 | Go 通过 `workflow.call` 调用 |
| Agent Run 状态 | 启元 Agent Go Runtime | Python 通过 API 查询或订阅事件 |
| RAG 知识库与向量索引 | 启用 AI 平台 Python | Go 通过 Retriever API 调用 |
| Tool / Skill / MCP Registry | 分阶段双源聚合 | 先通过 Gateway 聚合，后续明确主 registry |
| Trace / Usage | 初期各自记录，统一 trace_id 关联 | 后续可建设统一观测查询层 |

### 7.2 权限上下文传递

每次跨服务调用都必须携带标准 Actor Context：

```json
{
  "tenant_id": "tenant-a",
  "user_id": "user-1",
  "project_id": "project-1",
  "app_id": "app-1",
  "roles": ["developer"],
  "scopes": ["workflow:run", "rag:query"],
  "source": "workflow",
  "trace_id": "trace-xxx"
}
```

Go Runtime 不应完全信任调用方传入的普通 JSON 字段。生产环境应使用以下方式之一：

- 内部服务 JWT，包含租户、用户、角色、scope、过期时间和签名。
- API Gateway 注入的已认证 claim。
- mTLS + 服务间 token + 请求体中的 actor context 双重校验。

身份源建议：

- **租户、用户、组织、角色、项目的主数据源** 初期由启用 AI 平台控制面持有。
- Go Runtime 不直接修改身份与权限主数据；只通过 token claim、只读同步表或控制面查询接口获得有效身份上下文。
- 如果为了性能引入本地缓存，缓存必须有 TTL、版本号或失效事件，不能成为新的权限事实来源。
- 跨服务 token 必须区分“服务身份”和“最终用户身份”：服务身份证明调用方可信，最终用户身份决定本次能力可见范围。
- 所有运行时权限裁决必须以服务端解析出的 claim 为准，请求体中的 `actor` 只作为审计和幂等辅助字段，不能单独作为授权依据。

### 7.3 策略裁决分工

推荐分工：

- Python 控制面决定“用户是否能使用某个 App、工作流、知识库、连接器”。
- Go Runtime 决定“本次 Agent Run 中哪些工具、Skills、MCP、Sandbox 能力可见并可执行”。
- Sandbox Service 决定“具体执行环境能否按策略启动、联网、挂载、运行命令”。

跨层策略必须向下收敛，不允许下层扩大上层权限。

### 7.4 契约版本与兼容边界

跨语言集成必须显式版本化。建议第一版使用 `v1alpha` 契约，稳定后再提升为 `v1`。

要求：

- Agent Run API、事件类型、工具 schema、RAG 检索响应、工作流调用响应都必须带 `schema_version` 或通过 URL/API 版本表达。
- 新增字段默认向后兼容；删除字段、改语义、改错误码必须升级主版本或经过迁移窗口。
- Go 和 Python 各自可以内部快速演进，但跨服务契约必须有契约测试或固定样例。
- 低代码画布节点保存的是契约版本和能力声明，不直接保存 Go 内部结构。

## 八、API 契约草案

### 8.1 创建 Agent Run

```http
POST /enterprise/v1alpha/agent-runs
```

请求要点：

```json
{
  "schema_version": "v1alpha",
  "actor": {
    "tenant_id": "tenant-a",
    "user_id": "user-1",
    "project_id": "project-1",
    "roles": ["developer"],
    "scopes": ["agent:run"]
  },
  "source": {
    "type": "workflow",
    "workflow_id": "wf-1",
    "workflow_run_id": "wfr-1",
    "node_id": "node-agent-loop"
  },
  "input": {
    "instruction": "分析客户投诉并给出处理建议",
    "variables": {}
  },
  "capability_scope": {
    "tools": ["rag.search", "workflow.call"],
    "skills": ["customer-support"],
    "mcp_servers": []
  },
  "runtime": {
    "strategy": "react",
    "memory_scope": "workflow_run",
    "sandbox_profile": "enterprise-default",
    "approval_policy": "tenant-policy"
  },
  "idempotency_key": "tenant-a:wfr-1:node-agent-loop"
}
```

响应要点：

```json
{
  "schema_version": "v1alpha",
  "run_id": "run-1",
  "status": "queued",
  "trace_id": "trace-1",
  "events_url": "/enterprise/v1alpha/agent-runs/run-1/events"
}
```

### 8.2 订阅事件

```http
GET /enterprise/v1alpha/agent-runs/{run_id}/events
```

事件类型至少包括：

```text
run.queued
run.started
message.delta
tool.call.started
tool.call.completed
approval.required
approval.resolved
artifact.created
usage.updated
run.completed
run.failed
run.cancelled
```

事件必须包含：

```text
event_id
run_id
tenant_id
trace_id
sequence
timestamp
payload
```

事件一致性要求：

- 事件必须按 `run_id + sequence` 单调递增，消费者可以用 `Last-Event-ID` 或 `sequence` 断点续订。
- 创建 Run、更新 Run 状态、写入事件应尽量使用事务性 outbox 或等价机制，避免状态已完成但事件丢失。
- 低代码平台消费事件时必须按 `event_id` 幂等处理，避免重连后重复推进工作流节点。
- 大内容不直接塞入事件，应写入 artifact/resource，再在事件中引用 resource id。

### 8.3 取消与恢复

```http
POST /enterprise/v1alpha/agent-runs/{run_id}/cancel
POST /enterprise/v1alpha/agent-runs/{run_id}/resume
```

低代码平台取消工作流节点时，必须向 Go Runtime 传播取消。Agent Run 因审批、用户输入或外部工作流等待暂停时，必须能恢复。

取消与恢复边界：

- 取消必须向下传播到正在执行的 Tool、Workflow、Sandbox Job；无法立即中断的外部动作必须标记为 `cancelling` 并最终收敛到终态。
- 恢复必须校验当前用户仍有权限继续该 Run，不能因为历史审批存在就绕过最新租户策略。
- 等待审批、等待用户输入、等待外部工作流的 Run 应释放昂贵资源，避免长期占用沙箱或模型流式连接。

### 8.4 调用低代码工作流工具

```text
tool name: workflow.call
```

工具输入（`wait_mode` 可取 `async` 或 `sync`，默认 `async`）：

```json
{
  "workflow_id": "wf-1",
  "input": {},
  "wait_mode": "async",
  "timeout_ms": 60000
}
```

工具输出：

```json
{
  "workflow_run_id": "wfr-2",
  "status": "completed",
  "output": {},
  "trace_id": "trace-wfr-2"
}
```

递归与死锁控制：

- `workflow.call` 允许 Agent 调用低代码工作流，但必须限制最大嵌套深度，例如 `max_call_depth=3`。
- Python 工作流中的 Agent Loop 节点再次调用 Go Runtime 时，必须继承同一个 root trace，并增加 `call_depth`。
- 同一个 `workflow_run_id` 或 `agent_run_id` 不得同步等待自己直接或间接创建的下游任务。
- 默认优先使用异步等待和事件回调；同步等待只适合短任务，并必须有明确超时。
- 策略层可以按租户、App、工作流禁用“Agent -> Workflow -> Agent”递归链路。

## 九、迁移与落地路线

### Phase 0：契约和边界冻结

- 明确 Go Runtime 是唯一长期 Agent Loop 实现。
- 明确 Python 平台继续拥有低代码画布、工作流、RAG、既有多租户权限。
- 定义 Actor Context、Run API、事件流、`workflow.call`、`rag.search`、Python Tool Gateway Adapter 的最小契约。
- 不做数据库大迁移，不做全量 Go 重写。

### Phase 1：最小双向调用

- Python 低代码平台增加 Agent Loop 节点。
- Go Enterprise Runtime 暴露创建 Run、查询 Run、基础事件或轮询接口。
- Go 注册 `workflow.call` 工具，能调用 Python 工作流。
- Go 注册 `rag.search` 或 Retriever Provider，能调用 Python 知识库。
- Trace 使用统一 `trace_id` 串联，但观测后台可以暂时分离。

### Phase 2：治理和生产化

- 接入服务间认证、租户权限校验、工具能力过滤。
- 支持 SSE 事件流、取消、恢复、审批、人类干预。
- Python Tool Gateway Adapter 接入已有工具、Skills、MCP。
- Usage、Audit、Approval 统一字段，至少能按 `tenant_id + trace_id` 查全链路。
- 对长任务增加幂等、重试、超时和补偿策略。

### Phase 3：Go Workflow Runtime

- Go 增加 Workflow Executor / DAG 状态机，承载节点调度、变量传递、取消、恢复、重试和事件流。
- Python 控制面继续负责画布编辑、工作流定义、节点配置和版本发布。
- Python 将版本化 workflow definition 提交给 Go 执行；Go 遇到 Agent 节点直接调用内部 Run Engine，遇到 RAG/Tool/MCP/Skill/Code 节点走统一 Tool Gateway 和 Sandbox。
- 保留 Python Workflow Executor 作为过渡运行时或兼容运行时，但新增生产能力优先落到 Go Runtime。

### Phase 4：能力收敛与选择性迁移

- 高频通用工具逐步 Go 原生化。
- 明确 Tool / Skill / MCP Registry 的主数据来源和同步方向。
- 评估是否将 App Catalog、权限策略、配置管理迁入 Go，或继续保留 Python 控制面。
- 如果启用 AI 平台 Python 后端出现维护瓶颈，再按模块迁移，而不是整体重写。

## 十、仓库落点建议

在当前 `genesis-agent` 仓库中，新增能力应遵守既有目录边界：

```text
internal/runtime
  - 保持 Run Engine 产品无关，不直接依赖 Python 平台 API

internal/capabilities/tool
  - 定义 Tool Gateway、外部工具 Provider、工具调用契约

internal/capabilities/memory / trace / usage / approval / policy
  - 保持通用治理能力

internal/capabilities/rag 或 retrieval（如后续新增）
  - 定义 Retriever Provider 契约，不直接绑定 Python 实现

products/enterprise/bootstrap
  - 注入企业认证、租户策略、Python 平台 API endpoint、credential、Tool Provider

products/enterprise/internal
  - 放 Enterprise HTTP API、SSE、Python 平台 client 的产品私有适配
```

注意：

- `internal/runtime` 不 import `products/enterprise`。
- `internal/capabilities` 不 import `shared/local` 或产品私有包。
- Python 平台 client 如果是企业集成语义，优先放 `products/enterprise/internal`；如果抽象为产品无关外部 workflow client，再放能力域 adapter。
- 不因低代码平台集成恢复旧的 `internal/interfaces` 扩张。

## 十一、关键风险与处理

| 风险 | 影响 | 处理方式 |
| --- | --- | --- |
| 双系统权限不一致 | 越权调用工具、知识库或工作流 | 标准 Actor Context + 服务间认证 + 双侧策略裁决 |
| 共享表耦合过深 | schema 演进困难、双写冲突 | 明确 owner，通过 API/事件同步运行态 |
| Agent Run 和 Workflow Run 互相等待 | 死锁或资源占用 | 超时、取消传播、异步 wait mode、最大嵌套深度 |
| Trace 分裂 | 排障困难 | 全链路 `trace_id`、`run_id`、`workflow_run_id` 互相关联 |
| Python Tool Gateway 返回过大或含敏感信息 | Prompt 泄漏、上下文爆炸 | 输出脱敏、截断、artifact 化、schema 化 |
| Go/Python 错误码不一致 | 低代码节点难以处理失败 | 建立跨服务错误码映射和 retryable 标记 |
| 一开始契约过重 | 集成迟迟不能落地 | 先实现最小闭环，再补事件流、审批和恢复 |

## 十二、决策记录

当前建议决策：

1. **Agent Loop 长期只保留 Go Runtime 一套核心实现。**
2. **短期 Python Workflow Executor 继续执行普通工作流节点，只有 Agent Loop 节点调用 Go。**
3. **中长期 Go Runtime 承载 Workflow Executor，统一 Workflow、Agent、Tool、Skill、MCP、Sandbox 的运行时执行语义。**
4. **启用 AI 平台 Python 后端可以继续作为控制面、配置面和 RAG 知识面。**
5. **两边不以直接共享业务运行表作为主集成方式，而以 API、事件流、工具网关和统一 Actor Context 集成。**
6. **低代码平台调用 Agent Loop，Agent Loop 也通过受治理工具调用低代码工作流。**
7. **Tool / Skill / MCP 先通过 Gateway 互通，再逐步选择性收敛到 Go 原生能力。**

这个方案保留已有平台资产，同时把最应该统一的 Agent Runtime 收敛到一套实现，是当前阶段风险、收益和长期演进性更均衡的选择。



