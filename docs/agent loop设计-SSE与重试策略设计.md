## 适用范围与多端架构说明

Genesis Agent 同时支持三个产品端：

| 产品 | 入口 | UI 层 | 与 Agent Runtime 的通信方式 |
|------|------|-------|-----------------------------|
| **CLI** | `cmd/genesis-cli` | BubbleTea TUI（终端） | **进程内直连**：`progress.Sink` 注入 context，BubbleTea `tea.Cmd` 分发 |
| **Desktop** | `cmd/genesis-desktop` | Wails 嵌入 Chromium 前端 | **Wails IPC**：`runtime.EventsEmit` Go→JS 单向推送，`window.runtime.EventsOn` 前端订阅 |
| **Web（Enterprise）** | `cmd/genesis-enterprise` | 独立浏览器 / Next.js | **HTTP SSE**：POST + `text/event-stream`，`@microsoft/fetch-event-source` |

> **核心原则**：
> - **事件语义（Block 模型）是通用的**，适用于三端。所有产品都使用相同的 `block.start → block.delta × N → block.stop` 生命周期概念、相同的事件分类（run / step / block / intervention / error）。
> - **传输层是各端私有的**。HTTP SSE 的 wire 格式（`id:` / `event:` / `data:` 文本行）、断连重连、`Last-Event-ID`、HTTP 响应头等，**仅适用于 Web（Enterprise）端**。CLI 和 Desktop 不走 HTTP。
> - 本文档 §1–§5 描述的 SSE 协议是 **Web 端的完整规范**；§6 描述三端如何统一到同一语义层。

---

### 1 流式输出协议（SSE 事件规范）

#### 1.1 设计原则

综合 Claude 和 ChatGPT 两家工业级实现、本项目工作流事件流设计（`工作流输出与事件流设计方案`系列）以及 A2UI/CopilotKit 实践，Agent Loop 的流式协议采用以下核心决策：

| 设计决策 | 选择 | 依据 |
|----------|------|------|
| 传输格式 | 标准 SSE（`event:` + `id:` + `data:`） | 浏览器原生支持，无需 polyfill，易调试 |
| 增量模型 | **Content Block 索引**（不用 JSON Patch） | 清晰可理解，客户端无需 Patch 库，Claude 验证可行 |
| 续流锚点 | **全局序列号** `seq` + `Last-Event-ID` | 精确续流，断连后不丢事件，ChatGPT `c` 字段已验证 |
| 工具入参 | **流式输出工具入参**（不只推送工具结果） | 用户实时看到 Agent 在做什么，Claude 已验证体验价值 |
| 思考过程 | 独立 `thinking` block，前端可折叠/隐藏 | 与最终输出分离，正式对话默认不暴露，Studio 可见 |
| 富文本输出 | `final_answer` 支持多种 `content_type` | 兼容纯文本、Markdown、结构化 JSON、A2UI Widget |
| 人工干预 | **显式暂停/恢复**，`intervention.*` 独立事件族 | 语义清晰，不和普通输出混淆，Dify 最佳实践 |
| 错误分类 | 四类错误事件（retriable / tool / fatal / auth） | 前端按类型做差异化处理 |
| 消费端裁剪 | Stream Profile（正式对话 / Studio / 外部 API） | 同一 Run 按消费端投影不同事件集合 |

---

#### 1.2 SSE 基础格式与公共字段

每条 SSE 消息由三行加空行组成，遵循 [W3C Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html) 标准：

```text
id: {seq}
event: {event_type}
data: {json_payload}
[空行]
```

- `id`：全局单调递增序列号（整数），同时作为 SSE `id:` 字段，支持断连续流
- `event`：事件类型名，格式为 `{域}.{动作}`，客户端可按事件类型注册监听
- `data`：JSON 序列化的 payload

**所有事件的公共字段（每条消息必含）：**

```json
{
  "seq":        42,
  "run_id":     "run_abc123",
  "session_id": "sess_xyz",
  "tenant_id":  "t_001",
  "ts":         "2026-06-28T12:00:00.123Z"
}
```

下文各事件 payload 描述仅列出**专有字段**，公共字段不再重复。

**服务端必须设置的 HTTP 响应头：**

```http
Content-Type: text/event-stream; charset=utf-8
Cache-Control: no-cache
X-Accel-Buffering: no
Connection: keep-alive
Access-Control-Allow-Origin: *
```

| Header | 说明 |
|--------|------|
| `Content-Type` | 必须为 `text/event-stream`，触发浏览器原生 SSE 解析模式 |
| `Cache-Control: no-cache` | 禁止所有中间层（CDN、代理）缓存响应体 |
| `X-Accel-Buffering: no` | **最容易遗漏的一行**。Nginx 默认缓冲上游响应，会导致 SSE 事件积压在缓冲区直到写满才批量发出，彻底破坏流式体验；该 Header 通知 Nginx 关闭此行为 |
| `Connection: keep-alive` | 显式保持 TCP 长连接（HTTP/1.1 默认已有，HTTP/2 不需要） |
| `Access-Control-Allow-Origin` | 跨域场景必须设置；生产环境应精确指定允许的域名，不应使用 `*` |

---

#### 1.3 Content Block 模型

Agent Loop 执行过程中的每一类输出（思考、工具调用入参、工具结果、文本回答等）对应一个 **Content Block**，block 在当前 Run 内用 `block_index`（单调递增整数）唯一标识。

每个 block 的生命周期由三类事件描述：

```text
block.start  →  block.delta × N（可选，流式内容）  →  block.stop
```

**`block.start` payload：**

```json
{
  "block_index":   3,
  "block_type":    "tool_input",
  "step_index":    1,
  "name":          "search_knowledge",
  "display_label": "正在搜索知识库",
  "display":       true,
  "content_type":  "application/json"
}
```

字段说明：

| 字段 | 类型 | 说明 |
|------|------|------|
| `block_index` | int | Block 在当前 Run 内的唯一编号 |
| `block_type` | string | 见 1.4 Block 类型全表 |
| `step_index` | int | 所属推理步骤编号 |
| `name` | string | 工具名 / block 标识，非所有 block 必有 |
| `display_label` | string | 前端展示的人类可读文案 |
| `display` | bool | 是否默认在用户界面展示 |
| `content_type` | string | 内容类型，用于 final_answer block |
| `extra` | object | 可选，扩展信息；各 block_type 可自定义扩展字段，RAG block 使用 `rag_round`、`reason` 等 |

**`block.delta` payload：**

```json
{
  "block_index": 3,
  "delta_type":  "json_delta",
  "value":       "{\"keywords\":[\"合同风险\""
}
```

| `delta_type` | 含义 | 适用 block_type |
|--------------|------|----------------|
| `text_delta` | 纯文本追加 | thinking / text / final_answer |
| `json_delta` | JSON 字符串分片追加 | tool_input / tool_result / widget/a2ui |

客户端按 `block_index` 将 delta 追加到对应 buffer，`block.stop` 后整体解析。

> **高频 delta 的精简格式**：LLM token 流每秒可产生数十条 delta，为减少传输开销，`block.delta` 事件允许省略公共字段（`run_id`、`session_id`、`tenant_id`、`ts`），只保留 `seq`、`block_index`、`delta_type`、`value` 四个字段。连接上下文由 `run.started` 事件已建立，客户端不会歧义。其他低频事件仍保留完整公共字段。

**`block.stop` payload：**

```json
{
  "block_index":      3,
  "stop_reason":      "complete",
  "duration_ms":      1240,
  "result_summary":   "找到 5 条相关内容，覆盖度 82%",
  "thinking_summary": "分析了用户关于合同违约的问题，决定搜索知识库获取违约条款相关内容",
  "resolution":       "approved",
  "retry_count":      0
}
```

字段说明：

| 字段 | 类型 | 适用场景 | 说明 |
|------|------|----------|------|
| `block_index` | int | 必有 | Block 唯一编号 |
| `stop_reason` | string | 必有 | 见下表枚举 |
| `duration_ms` | int | 可选 | Block 执行耗时 |
| `result_summary` | string | 可选 | 给用户看的结果摘要（tool_result / rag_search） |
| `thinking_summary` | string | 可选 | 仅 `block_type=thinking` 时存在，是思考过程的人类可读摘要，conversation profile 可展示此字段替代完整思考内容 |
| `resolution` | string | 可选 | 仅 `stop_reason=intervention_resolved` 时存在，值为 `approved`/`rejected`/`modified` |
| `retry_count` | int | 可选 | 该 block 实际重试次数（用于 Studio 调试） |
| `fallback_text` | string | 可选 | 仅 `stop_reason=fallback` 时存在，是 RetryPolicy 配置的降级回复文本，LLM 将以此继续推理 |
| `large_result` | object | 可选 | 工具结果超过降级阈值时存在，含 `resource_id`、`preview` 等字段；见 §5 大结果懒加载降级 |

`stop_reason` 枚举值：

| 值 | 含义 |
|----|------|
| `complete` | 正常完成 |
| `pending_approval` | 工具入参已构建，等待用户审批后继续 |
| `intervention_resolved` | 干预已处理（带 `resolution` 字段） |
| `fallback` | 重试耗尽，以 fallback_text 作为结果继续执行 |
| `error` | 执行出错，块内容可能不完整 |
| `cancelled` | 被取消 |

---

#### 1.4 Block 类型全表

| `block_type` | 含义 | delta 类型 | 默认对用户可见 |
|--------------|------|:----------:|:----------:|
| `thinking` | LLM 内部推理过程（CoT / 思考链） | text_delta | ❌（conversation 不推送，debug 可见；`block.stop` 携带 `thinking_summary`） |
| `text` | LLM 中间文本输出（步骤说明、分析过程） | text_delta | ✅ |
| `tool_input` | 工具调用入参构建（实时流出） | json_delta | ✅（展示"正在调用 XXX"） |
| `tool_result` | 工具执行结果 | json_delta（可选，结果较大时分片） | ✅（摘要形式）；完整数据在 delta 中 |
| `rag_search` | RAG 检索过程（关键词 + 轮次 + 结果数） | ❌（一次性，结果写入 `block.stop`） | ✅ |
| `agent_call` | 子 Agent 调用（含目标 run_id） | ❌（一次性） | ✅ |
| `final_answer` | 最终答案（支持多种富文本类型） | text_delta / json_delta | ✅ |
| `system_notify` | 系统通知（警告、降级说明、预算提醒） | ❌（一次性） | ✅ |
| `intervention` | 人工干预请求（与 `intervention.required` 事件对应） | ❌（一次性） | ✅ |

> **`text` vs `final_answer` 使用规则**：`text` 用于 Agent 在推理过程中输出给用户的中间说明（如"我将分三步分析..."），不代表最终结论；`final_answer` 是 Run 的最终输出结论，由 `run.completed.output_block_indices` 引用。每个 Run 可有多个 `final_answer` block（Plan-Execute 模式可产生多个阶段性结论）。

`final_answer` 的 `content_type` 枚举：

| `content_type` | 含义 |
|----------------|------|
| `text/markdown` | Markdown 文本（默认） |
| `text/plain` | 纯文本 |
| `application/json` | 结构化 JSON 数据 |
| `text/html` | HTML 片段（受限，沙箱渲染） |
| `widget/a2ui` | A2UI 富 UI 组件（见 1.8） |

---

#### 1.5 完整事件类型规范

**── Run 级事件 ──**

```text
run.started
  { mode: "react"|"plan_execute"|"loop",
    agent_config_summary: { name, model, max_iterations } }
  说明：Run 启动，客户端初始化 UI 状态

run.paused
  { reason: "intervention"|"user_pause"|"resource_limit",
    intervention_id?: "int_001" }
  说明：Run 暂停，等待外部输入

run.resumed
  { resumed_by: "user"|"timeout_default"|"system",
    intervention_id?: "int_001" }
  说明：Run 从暂停状态恢复执行

run.completed
  { output_block_indices: [12],   -- 数组！支持 Plan-Execute 多个 final_answer block
    usage: { input_tokens: 1200, output_tokens: 800, thinking_tokens: 200 },
    duration_ms: 3400 }
  说明：Run 正常完成，output_block_indices 指向所有 final_answer block 的索引列表

run.failed
  { error_code: "ITERATION_LIMIT"|"BUDGET_EXCEEDED"|"SYSTEM_ERROR",
    error_type: "fatal"|"partial_complete",
    message: "已超出最大迭代次数（20 次）",
    retriable: false }
  说明：Run 终止失败，与 error.fatal 事件配合使用

run.cancelled
  { cancelled_by: "user"|"system"|"admin" }
  说明：Run 被主动取消
```

**── Block 级事件（核心，见 1.3）──**

```text
block.start   { block_index, block_type, step_index, name?, display_label?, display, content_type? }
block.delta   { block_index, delta_type, value }
block.stop    { block_index, stop_reason, duration_ms?, result_summary? }
```

**── Step 级事件 ──**

```text
step.started
  { step_index: 2,
    action_type: "llm_think"|"generate_answer"|"tool_call"|"rag_search"|"agent_call",
    description: "第 2 步：检索知识库" }

step.completed
  { step_index: 2,
    action_type: "rag_search",
    block_indices: [3, 4, 5],
    summary: "检索完成，找到 5 条相关内容" }
```

**── 干预事件 ──**

```text
intervention.required
  { intervention_id: "int_001",
    intervention_type:
      "tool_approval"     -- 执行高危工具前需要用户明确授权（人工点击确认）
      | "tool_confirm"    -- 执行工具前需要用户确认参数是否正确
      | "user_input"      -- Agent 需要用户补充输入信息才能继续
      | "manual_override" -- Agent 不确定决策，需人工接管或选择路径
      | "step_by_step"    -- 调试模式，每步执行前都暂停等待
      | "authorization",  -- 需要用户完成 OAuth/API 授权才能访问外部资源
                          -- 区别于 error.auth（后者是已发生的权限拒绝）；
                          -- authorization 是前置授权流程，成功后 Run 继续执行
    block_index: 6,
    title: "需要您的授权",
    description: "Agent 将执行以下操作，请确认",
    subject: {
      tool_name: "delete_records",
      params_preview: { table: "orders", condition: "status='draft'" }
    },
    response_schema: {
      type: "object",
      properties: { approved: { type: "boolean" }, comment: { type: "string" } }
    },
    -- authorization 类型专属字段：
    auth_url?: "https://oauth.example.com/authorize?...",
    auth_scope?: ["read:calendar", "write:email"],

    allowed_actions: [
      { id: "approve", label: "授权执行", role: "approve" },
      { id: "reject",  label: "拒绝",     role: "reject" },
      { id: "modify",  label: "修改参数", role: "modify" }
    ],
    resume_token: "tok_xxxxxxxx",
    expires_at: "2026-06-28T12:05:00Z",
    default_on_timeout: "reject" }
  说明：需要人工干预，Run 将在此事件后立即发送 run.paused

intervention.expired
  { intervention_id: "int_001",
    default_action_applied: "reject",
    message: "干预已超时，已按默认策略处理" }

intervention.cancelled
  { intervention_id: "int_001", reason: "run_cancelled"|"admin_override" }
```

**── 错误事件 ──**

```text
error.retriable
  { error_code: "LLM_RATE_LIMIT"|"NETWORK_TIMEOUT"|"TOOL_EXEC_FAILED",
    message: "LLM API 限流，正在重试（第 2 次，共 3 次）",
    retry_attempt: 2,      -- 当前是第几次重试（1-based）
    max_retries: 3,        -- 来自 RetryPolicy.max_retries
    retry_after_ms: 4000,  -- 本次等待后重试（来自 RetryPolicy 计算）
    block_index?: 3,       -- 关联的 block（tool_input/agent_call 块）
    policy_id?: "tool:send_email" }  -- 哪个 RetryPolicy 被触发（调试用）
  说明：可重试错误，Run 未终止，前端显示"重试中"提示；
        重试耗尽时，依据 RetryPolicy.on_final_failure 决定后续行为：
        - "error"        → emit error.fatal，run.failed
        - "fallback_text"→ 以配置文本作为结果继续，block.stop.stop_reason="fallback"
        - "intervention" → emit intervention.required（type=manual_override），等待人工决策

error.tool
  { error_code: "TOOL_EXEC_FAILED"|"TOOL_TIMEOUT"|"SANDBOX_ERROR",
    tool_name: "search_web",
    message: "搜索服务连接超时",
    block_index: 5,
    will_retry: false,
    will_replan: true }
  说明：工具本身业务失败。will_retry=true 时配合后续 error.retriable 展示重试进度；
        will_replan=true 时表示 LLM 将重新规划，Run 继续；前端可标注该工具步骤失败。

  【error.retriable vs error.tool 分发规则】
  - error.retriable：用于 LLM API 层和基础网络层的重试感知（限流/超时），与具体工具无关
  - error.tool：用于工具本身业务失败，will_retry=true 时可视为工具级别的 retriable error
  - 工具失败后进入重试时：先发 error.tool(will_retry=true)，再在每次等待前发 error.retriable
  - 两者可在同一 block 上下文中同时出现，不互斥

error.fatal
  { error_code: "BUDGET_EXCEEDED"|"AUTH_FAILED"|"ITERATION_LIMIT"|"SYSTEM_ERROR",
    message: "已超出 Token 预算上限",
    run_terminated: true }
  说明：致命错误，Run 立即终止，后续必有 run.failed

error.auth
  { error_code: "PERMISSION_DENIED"|"TOKEN_EXPIRED"|"SCOPE_INSUFFICIENT",
    resource: "tool:send_email",
    message: "工具调用权限不足，令牌已过期",
    resolution_hint: "contact_admin"|"re_authorize"|"reduce_scope",
    auth_url?: "https://..." }
  说明：权限/授权错误，表示调用已发生并被拒绝（事后错误），Run 终止或需要重试。
        与 intervention_type=authorization 的区别：
        - authorization 干预：调用前发现缺少授权，暂停 Run 引导用户完成授权流程，授权成功后继续
        - error.auth：调用已发出，服务端拒绝（权限不足/Token 过期），Run 无法继续；
          若有 auth_url，前端可弹出重新授权窗口，用户完成后客户端需发起新的 Run
```

**── 系统事件 ──**

```text
ping
  id: {global_seq}            -- 复用全局序列号，断连续流时作为 Last-Event-ID 锚点
  { ts: "2026-06-28T12:00:30.123Z",   -- RFC3339Nano，前端可据此计算 RTT 延迟
    status: "running"|"paused" }       -- 当前 Run 状态，前端可刷新健康状态指示灯
  说明：保活心跳，后端空闲超 15s 时发送（time.Timer 实现，非 time.Ticker，见 §3）；
        客户端收到后须重置 Watchdog 定时器；前端 switch 中过滤 ping 不影响业务 UI

debug.step_snapshot  【仅 Studio 调试模式】
  { step_index: 2,
    variables_snapshot: { customer_name: "张三", ... },
    context_size_tokens: 4200,
    memory_state: { working_memory_size: 3, short_term_messages: 12 } }
  说明：Studio 试运行专用，正式对话不发送此事件
```

---

#### 1.6 典型执行序列示例

以"知识库 RAG 问答"为例，完整 SSE 流（省略公共字段）：

```text
id:1   event:run.started
       data: { mode:"react", agent_config_summary:{name:"合同助手", model:"claude-3-7-sonnet"} }

id:2   event:step.started
       data: { step_index:0, action_type:"llm_think", description:"第 1 步：分析问题" }

id:3   event:block.start
       data: { block_index:0, block_type:"thinking", step_index:0, display:false }

id:4   event:block.delta
       data: { block_index:0, delta_type:"text_delta", value:"用户想了解合同风险..." }

id:5   event:block.delta
       data: { block_index:0, delta_type:"text_delta", value:"需要搜索知识库获取相关条款" }

id:6   event:block.stop
       data: { block_index:0, stop_reason:"complete", duration_ms:820 }

id:7   event:block.start
       data: { block_index:1, block_type:"tool_input", step_index:0,
               name:"search_knowledge", display_label:"正在搜索知识库", display:true }

id:8   event:block.delta
       data: { block_index:1, delta_type:"json_delta", value:"{\"keywords\":[\"" }

id:9   event:block.delta
       data: { block_index:1, delta_type:"json_delta", value:"合同违约" }

id:10  event:block.delta
       data: { block_index:1, delta_type:"json_delta", value:"\",\"违约责任条款\"]}" }

id:11  event:block.stop
       data: { block_index:1, stop_reason:"complete" }

id:12  event:block.start
       data: { block_index:2, block_type:"tool_result", name:"search_knowledge", display:true }

id:13  event:block.stop
       data: { block_index:2, stop_reason:"complete", result_summary:"找到 5 条相关内容，覆盖度 82%" }

id:14  event:step.completed
       data: { step_index:0, block_indices:[0,1,2], summary:"RAG 检索完成" }

id:15  event:step.started
       data: { step_index:1, action_type:"generate_answer", description:"第 2 步：生成回答" }

id:16  event:block.start
       data: { block_index:3, block_type:"final_answer", step_index:1,
               content_type:"text/markdown", display:true }

id:17  event:block.delta
       data: { block_index:3, delta_type:"text_delta", value:"根据检索到的合同条款，" }

id:18  event:block.delta
       data: { block_index:3, delta_type:"text_delta", value:"该合同存在以下主要风险：\n\n" }

id:19  event:block.delta
       data: { block_index:3, delta_type:"text_delta", value:"**1. 违约金条款不对等**..." }

      ... 更多 text_delta ...

id:35  event:block.stop
       data: { block_index:3, stop_reason:"complete", duration_ms:2100 }

id:36  event:run.completed
       data: { output_block_indices:[3], usage:{input_tokens:1800, output_tokens:620}, duration_ms:5200 }
       -- output_block_indices 为数组，支持多个 final_answer（如 Plan-Execute 多阶段输出）
```

> **多轮 RAG 序列说明**：当第一轮检索覆盖度不足（如覆盖度 < 60%），Agent 自主发起第二轮搜索，换用更精确的关键词。每轮都是一个独立的 `tool_input` + `tool_result` block 对：
>
> ```text
> -- 第 1 轮 RAG（覆盖度不足，Agent 决定继续）
> id:11  event:block.stop
>        data: { block_index:2, stop_reason:"complete",
>                result_summary:"找到 3 条相关内容，覆盖度 48%，需要补充检索" }
>
> id:12  event:block.start  -- 第 2 轮：更换关键词
>        data: { block_index:3, block_type:"tool_input", name:"search_knowledge",
>                display_label:"补充检索（第 2 轮）", display:true,
>                extra:{ rag_round:2, reason:"覆盖度不足，扩展关键词" } }
>
> id:13  event:block.delta
>        data: { block_index:3, delta_type:"json_delta",
>                value:"{\"keywords\":[\"违约金上限\",\"损失赔偿条款\",\"免责情形\"]}" }
>
> id:14  event:block.stop
>        data: { block_index:3, stop_reason:"complete" }
>
> id:15  event:block.start
>        data: { block_index:4, block_type:"tool_result", name:"search_knowledge", display:true }
>
> id:16  event:block.stop
>        data: { block_index:4, stop_reason:"complete",
>                result_summary:"找到 7 条相关内容，覆盖度 91%，可以生成回答" }
>
> -- 第 2 轮 RAG 满足要求，进入生成阶段
> id:17  event:step.completed
>        data: { step_index:0, block_indices:[0,1,2,3,4], summary:"多轮 RAG 完成（2 轮）" }
> ```
>
> 客户端可根据 `extra.rag_round` 和 `result_summary` 中的覆盖度信息，在 UI 上展示多轮检索进度。

---

#### 1.7 工具授权 / 干预流程序列

当工具需要用户审批时，入参流结束后进入干预暂停状态：

```text
id:20  event:block.start
       data: { block_index:5, block_type:"tool_input", name:"delete_records",
               display_label:"准备删除数据", display:true }

id:21  event:block.delta
       data: { block_index:5, delta_type:"json_delta",
               value:"{\"table\":\"orders\",\"condition\":\"status='draft'\"}" }

id:22  event:block.stop
       data: { block_index:5, stop_reason:"pending_approval" }
                                 ↑ 告知前端入参已确定，等待审批

id:23  event:block.start
       data: { block_index:6, block_type:"intervention", step_index:1, display:true }

id:24  event:intervention.required
       data: {
         intervention_id: "int_001",
         intervention_type: "tool_approval",
         block_index: 6,
         title: "请确认删除操作",
         description: "Agent 将删除 orders 表中状态为 draft 的记录",
         subject: {
           tool_name: "delete_records",
           params_preview: { table: "orders", condition: "status='draft'" }
         },
         response_schema: {
           type: "object",
           properties: {
             approved: { type: "boolean", title: "是否授权" },
             comment:  { type: "string",  title: "备注（可选）" }
           },
           required: ["approved"]
         },
         allowed_actions: [
           { id: "approve", label: "授权执行", role: "approve" },
           { id: "reject",  label: "拒绝",     role: "reject" }
         ],
         resume_token: "tok_xxxxxxxx",
         expires_at: "2026-06-28T12:10:00Z",
         default_on_timeout: "reject"
       }

id:25  event:run.paused
       data: { reason:"intervention", intervention_id:"int_001" }

── 前端渲染审批卡片，用户点击"授权执行" ──
── 前端 POST /v1/runs/{run_id}/interventions/{intervention_id}/respond ──
── 参见 1.10 ──

id:26  event:block.stop
       data: { block_index:6, stop_reason:"intervention_resolved", resolution:"approved" }

id:27  event:run.resumed
       data: { resumed_by:"user", intervention_id:"int_001" }

id:28  event:block.start
       data: { block_index:7, block_type:"tool_result", name:"delete_records", display:true }

id:29  event:block.stop
       data: { block_index:7, stop_reason:"complete", result_summary:"成功删除 12 条记录" }
```

---

#### 1.8 富文本输出（A2UI Widget）

当最终答案需要渲染富 UI 组件时，`final_answer` block 的 `content_type` 设为 `widget/a2ui`：

```text
id:40  event:block.start
       data: {
         block_index: 8,
         block_type: "final_answer",
         content_type: "widget/a2ui",
         display: true,
         widget_meta: {
           surface_id: "risk_report_001",
           catalog_id:  "app.interaction.a2ui/v1",
           title: "合同风险分析报告"
         }
       }

id:41  event:block.delta
       data: { block_index:8, delta_type:"json_delta",
               value: "{\"operations\":[{\"version\":\"v0.9\",\"createSurface\":" }

id:42  event:block.delta
       data: { block_index:8, delta_type:"json_delta",
               value: "{\"surfaceId\":\"risk_report_001\",\"catalogId\":\"app.interaction.a2ui/v1\"}}" }

id:43  event:block.delta
       data: { block_index:8, delta_type:"json_delta",
               value: ",{\"version\":\"v0.9\",\"updateComponents\":{\"components\":[...]}}]}" }

id:44  event:block.stop
       data: { block_index:8, stop_reason:"complete" }
```

**前端渲染注意事项：**

- `block.stop` 之前**不要**渲染 A2UI 组件，等待完整 JSON 再解析（CopilotKit 实践验证：流式渲染会导致半成品组件报错）
- 收到 `block.stop` 后一次性解析 `operations` 数组并渲染
- 后端应确保 A2UI JSON 完整后才 flush 最后一个 delta，避免客户端收到截断的 JSON

**A2UI operations 结构（v0.9）：**

```json
{
  "operations": [
    { "version": "v0.9", "createSurface":    { "surfaceId": "...", "catalogId": "..." } },
    { "version": "v0.9", "updateComponents": { "surfaceId": "...", "components": [...] } },
    { "version": "v0.9", "updateDataModel":  { "surfaceId": "...", "path": "/", "value": {...} } }
  ]
}
```

---

#### 1.9 断连续流机制

SSE 的 `id:` 字段设为全局 `seq`，客户端断连后携带 `Last-Event-ID` 请求头重连。

> **重要**：重连必须使用与初始建连相同的 **`POST` 方法**（见 §4），不可改为 `GET`。原生 `EventSource` 自动重连使用 `GET`，因此必须使用 `@microsoft/fetch-event-source` 等库手动管理重连逻辑。

```text
断连时 seq = 25

重连请求（POST，携带原始 StartRunRequest Body）：
  POST /v1/runs/{run_id}/stream
  Last-Event-ID: 25
  Content-Type: application/json
  { ...原始 StartRunRequest Body... }

Server 续流策略（seq > 25 的事件）：
  ① 已完成的 block：只重发 block.start + block.stop（不重发中间 delta，避免重复渲染）
  ② 正在进行的 block：重发 block.start + 所有已发送 delta（全量重放，客户端清空重建 buffer）
  ③ pending 的 intervention：重发 intervention.required
  ④ 已完成的 run：直接发 run.completed / run.failed
  ⑤ ping：立即补发一条，告知客户端连接正常
```

**服务端持久化要求：**

| 数据 | 保留策略 |
|------|----------|
| `block.start` + `block.stop` 事件 | 永久保留（Run 生命周期内） |
| `block.delta` 按 block 归档 | TTL 24h（续流窗口） |
| `intervention.required` | 永久保留（写入 interaction 记录） |
| `run.*` 级别事件 | 永久保留 |

---

#### 1.10 前端响应授权 / 干预

前端对干预的响应通过**独立 REST endpoint** 提交，不通过 SSE 反向传递：

```text
POST /v1/runs/{run_id}/interventions/{intervention_id}/respond
Content-Type: application/json

{
  "action_id":       "approve",
  "response_data":   { "approved": true, "comment": "确认删除" },
  "resume_token":    "tok_xxxxxxxx",
  "idempotency_key": "cli_uuid_xxxx"
}
```

| 字段 | 说明 |
|------|------|
| `action_id` | 必须是 `intervention.required` 中 `allowed_actions` 的 id |
| `response_data` | 按 `response_schema` 填写 |
| `resume_token` | 来自 `intervention.required` 的 token，防伪造 |
| `idempotency_key` | 客户端生成的 UUID，防重复提交 |

**成功响应：**

```json
{
  "ok":                   true,
  "run_status":           "running",
  "intervention_status":  "resolved"
}
```

响应成功后，SSE 流会依次推送 `block.stop`（resolution=approved）→ `run.resumed` → 后续 block。

**错误响应（前端必须处理）：**

```json
{ "ok": false, "error_code": "INTERVENTION_EXPIRED",
  "message": "干预已超时，默认策略已生效", "run_status": "failed" }

{ "ok": false, "error_code": "INVALID_ACTION",
  "message": "action_id 不在允许范围内" }

{ "ok": false, "error_code": "ALREADY_SUBMITTED",
  "message": "该干预已被提交，不可重复操作",
  "submitted_by": "user_001", "submitted_at": "2026-06-28T12:03:00Z" }

{ "ok": false, "error_code": "INVALID_TOKEN",
  "message": "resume_token 无效或已过期" }

{ "ok": false, "error_code": "PERMISSION_DENIED",
  "message": "您没有处理该干预的权限" }
```

**`modify` 动作（修改工具入参后重试）：**

```json
{
  "action_id":    "modify",
  "response_data": {
    "approved":        true,
    "modified_params": { "table": "orders", "condition": "status='draft' AND created_at < '2025-01-01'" }
  },
  "resume_token":    "tok_xxxxxxxx",
  "idempotency_key": "cli_uuid_yyyy"
}
```

---

#### 1.11 Stream Profile（面向不同消费端）

同一个 Run 的内部事件按消费端投影为不同的事件集合，不同消费端连接不同的 stream endpoint 或通过 `profile` 参数区分：

```text
GET /v1/runs/{run_id}/stream?profile=conversation   ← 正式对话界面
GET /v1/runs/{run_id}/stream?profile=debug          ← Studio 试运行
GET /v1/runs/{run_id}/stream?profile=api            ← 外部 API
```

| 事件类型 | `conversation` | `debug` | `api` |
|----------|:--------------:|:-------:|:-----:|
| `run.*` | ✅ | ✅ | ✅（简化字段） |
| `block.*`（thinking） | ❌ 默认隐藏 | ✅ | ❌ |
| `block.*`（text / final_answer） | ✅ | ✅ | ✅ |
| `block.*`（tool_input） | ✅（摘要） | ✅（完整入参） | ✅（摘要） |
| `block.*`（tool_result） | ✅（摘要） | ✅（完整结果） | ✅（摘要） |
| `intervention.*` | ✅ | ✅ | ✅ |
| `error.*` | ✅ | ✅ | ✅ |
| `step.*` | ❌ | ✅ | ❌ |
| `debug.step_snapshot` | ❌ | ✅ | ❌ |
| `ping` | ✅ | ✅ | ✅ |

`conversation` profile 下，`thinking` block 虽然不主动推送，但 delta 已由服务端收集到 `block.stop` 的 `thinking_summary` 字段（可选），前端可按需展示"思考摘要"而无需在流中暴露完整推理过程。

---

#### 1.12 自定义重试配置（RetryPolicy）

Agent 执行、LLM 调用、工具调用均可独立配置重试策略，以支持不同业务场景下的容错需求。

**RetryPolicy 结构：**

```yaml
# 可在以下三个层级配置，优先级由高到低：
# 1. Tool 级别（tool_registry 中单个工具的配置）
# 2. Agent Run 级别（每次 Run 携带的覆盖参数）
# 3. 系统默认值（全局 fallback）

retry_policy:
  max_retries: 3               # 最大重试次数（不含第一次执行），0 表示不重试
  initial_interval_ms: 1000    # 初始等待时间（毫秒）
  backoff_strategy: exponential  # 退避策略：fixed | exponential | linear
  max_interval_ms: 30000       # 退避上限（指数退避时不超过此值）
  retryable_errors:            # 仅对指定错误码重试；空列表 = 全部可重试错误
    - "LLM_RATE_LIMIT"
    - "NETWORK_TIMEOUT"
    - "TOOL_EXEC_FAILED"
  on_final_failure:            # 重试耗尽后的行为
    action: error              # error | fallback_text | intervention
    # 仅 action=fallback_text 时有效：
    fallback_text: "抱歉，该步骤暂时无法完成，请稍后再试或联系管理员。"
    # 仅 action=intervention 时有效：
    intervention_title: "工具调用多次失败，需要人工决策"
    intervention_description: "send_email 工具连续失败 3 次，请选择处理方式"
```

**三种 `on_final_failure.action` 的行为对比：**

| action | 行为 | SSE 事件序列 | Run 最终状态 |
|--------|------|------------|------------|
| `error`（默认） | 终止 Run，上报错误 | `error.fatal` → `run.failed` | `failed` |
| `fallback_text` | 以配置文本替代工具结果，Run 继续执行 | `block.stop(stop_reason=fallback)` → `step.completed` → 继续下一步 | `completed`（结果可能不完整） |
| `intervention` | 暂停 Run，触发人工干预 | `intervention.required(type=manual_override)` → `run.paused` | 等待人工操作后 `completed` 或 `failed` |

**SSE 中的重试过程展示：**

```text
-- 工具调用失败，开始重试（max_retries=3）
id:10  event:error.retriable
       data: {
         error_code: "TOOL_EXEC_FAILED",
         message: "send_email 调用失败：SMTP 连接超时（第 1 次重试，共 3 次）",
         retry_attempt: 1,
         max_retries: 3,
         retry_after_ms: 2000,
         block_index: 4,
         policy_id: "tool:send_email"
       }

id:11  event:error.retriable
       data: { ..., message: "第 2 次重试，还剩 1 次", retry_attempt:2, retry_after_ms:4000 }

id:12  event:error.retriable
       data: { ..., message: "第 3 次重试", retry_attempt:3, retry_after_ms:8000 }

-- 情形 A：on_final_failure=fallback_text
id:13  event:block.stop
       data: {
         block_index: 4,
         stop_reason: "fallback",
         result_summary: "工具执行失败，已使用预设回复",
         fallback_text: "抱歉，该步骤暂时无法完成，请稍后再试或联系管理员。"
       }

-- 情形 B：on_final_failure=error
id:13  event:error.fatal
       data: { error_code:"TOOL_EXEC_FAILED", message:"send_email 重试 3 次均失败", run_terminated:true }
id:14  event:run.failed
       data: { ...}

-- 情形 C：on_final_failure=intervention
id:13  event:intervention.required
       data: {
         intervention_type: "manual_override",
         title: "工具调用多次失败，需要人工决策",
         description: "send_email 工具连续失败 3 次，请选择处理方式",
         allowed_actions: [
           { id: "skip",    label: "跳过此步骤，继续执行", role: "approve" },
           { id: "retry",   label: "立即重试一次",          role: "approve" },
           { id: "abort",   label: "终止 Run",              role: "reject" }
         ],
         resume_token: "tok_xxxxxxxx"
       }
id:14  event:run.paused
       data: { reason: "manual_intervention", paused_at_step: 2 }
```

**配置示例（Tool 级别）：**

```json
{
  "tool_name": "send_email",
  "description": "发送电子邮件",
  "retry_policy": {
    "max_retries": 3,
    "initial_interval_ms": 2000,
    "backoff_strategy": "exponential",
    "max_interval_ms": 16000,
    "retryable_errors": ["NETWORK_TIMEOUT", "SMTP_ERROR"],
    "on_final_failure": {
      "action": "fallback_text",
      "fallback_text": "邮件发送暂时失败，已记录任务，将在系统恢复后重新发送。"
    }
  }
}
```

**配置示例（Run 级别覆盖）：**

```json
{
  "session_id": "sess_xxx",
  "agent_id": "agent_001",
  "user_input": "...",
  "retry_policy_overrides": {
    "llm": {
      "max_retries": 5,
      "on_final_failure": { "action": "intervention" }
    },
    "tool:search_web": {
      "max_retries": 1,
      "on_final_failure": {
        "action": "fallback_text",
        "fallback_text": "无法访问搜索服务，请检查网络连接后重试。"
      }
    }
  }
}
```

> **RetryPolicy 生效优先级**：`Run 级别 tool:xxx 覆盖` > `Run 级别 llm 覆盖` > `Tool 注册时配置` > `系统默认（max_retries=3, exponential, on_final_failure=error）`

---

### 2、错误分类与重试策略

Loop 中的错误分四类，处理方式不同：

```text
错误类型              触发条件                         处理策略
────────────────────────────────────────────────────────────────────────
retriable_error       LLM API 限流 / 网络超时          遵循 RetryPolicy 配置（§1.12）
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

### 3. 保活心跳机制（Keep-Alive Ping）

#### 3.1 设计决策：单一 `event: ping` 机制

**结论：采用显式 `event: ping` 命名事件，不使用 SSE 注释（`: keep-alive`）双层方案。**

| 维度 | 选择 | 依据 |
|------|------|------|
| 机制 | 单一 `event: ping` | 一条 SSE 消息（不管是注释还是命名事件）都能同等重置代理空闲超时计数器，物理保活效果完全相同；维护两套定时器逻辑只增加复杂度 |
| 前端侵入 | 显式事件处理 | 前端已经要手动分发事件类型；把心跳当一等公民业务事件，Debug 时在 Chrome EventStream 面板直接可见，工程收益更大 |
| 业务信息 | 携带 `ts` + `status` | 前端可做 RTT 延迟计算、Studio 调试器连接健康度指示；比静默注释多出可观测价值 |
| 定时器 | Go `time.Timer`（非 `time.Ticker`） | `Ticker.Reset()` 存在 channel 积压竞态问题；`Timer` 每次手动重置，天然避免此坑 |

#### 3.2 Ping 事件格式

`ping` 事件**复用全局 `id:` 序列号**（与其他 SSE 事件共享同一递增器），这是断连续流能正确锚定 `Last-Event-ID` 的前提，不能为 ping 维护独立 seq。

```text
id: {global_seq}
event: ping
data: {"ts": "2026-07-09T15:00:00.123Z", "status": "running"}

```

| 字段 | 说明 |
|------|------|
| `ts` | 服务端当前时间（RFC3339Nano），前端可据此计算往返延迟 |
| `status` | 当前 Run 状态（`running` / `paused`），前端可据此刷新状态指示灯 |

#### 3.3 Go 后端实现参考

```go
// SSE 心跳保活。使用 time.Timer（非 time.Ticker）避免 Reset 后 channel 积压竞态。
// 每次有真实数据写出时，重置计时器；超过 15s 静默则发送 ping 保活。
func runSSEWithKeepalive(ctx context.Context, w http.ResponseWriter, dataCh <-chan SSEEvent) {
    flusher := w.(http.Flusher)
    seq := 0
    timer := time.NewTimer(15 * time.Second)
    defer timer.Stop()

    for {
        select {
        case event, ok := <-dataCh:
            if !ok {
                return // 数据源关闭，Run 结束
            }
            seq++
            writeSSEEvent(w, seq, event)
            flusher.Flush()
            // 重置心跳计时器（先 drain 再 Reset，防竞态）
            if !timer.Stop() {
                select {
                case <-timer.C:
                default:
                }
            }
            timer.Reset(15 * time.Second)

        case <-timer.C:
            seq++
            fmt.Fprintf(w, "id: %d\nevent: ping\ndata: {\"ts\":\"%s\",\"status\":\"running\"}\n\n",
                seq, time.Now().Format(time.RFC3339Nano))
            flusher.Flush()
            timer.Reset(15 * time.Second)

        case <-ctx.Done():
            return
        }
    }
}
```

#### 3.4 前端 Watchdog（客户端断连检测）

不能完全依赖 TCP 层断连检测——很多情况下连接是"假死"（网络中间件单向 RST，TCP 不立即报错），前端必须实现主动 Watchdog：

```typescript
// 前端 Watchdog 伪代码（适用于 @microsoft/fetch-event-source 或自定义 SSE 客户端）
const PING_INTERVAL_MS = 15_000;
const WATCHDOG_TIMEOUT_MS = PING_INTERVAL_MS * 2; // 2 倍心跳间隔（30s）判定假死

let lastReceivedAt = Date.now();
let watchdogTimer: ReturnType<typeof setTimeout>;

function resetWatchdog() {
    clearTimeout(watchdogTimer);
    lastReceivedAt = Date.now();
    watchdogTimer = setTimeout(() => {
        console.warn('SSE Watchdog: 连接疑似假死，主动重连');
        reconnectWithBackoff(); // ← 必须使用指数退避，防止重连风暴
    }, WATCHDOG_TIMEOUT_MS);
}

// 任何 SSE 事件（包括 ping）收到时都调用
onMessage((event) => {
    resetWatchdog();
    if (event.type === 'ping') return; // ping 不影响业务 UI
    dispatch(event);
});
```

> **重连风暴（Thundering Herd）防护**：`reconnectWithBackoff` 必须实现指数退避（如初始 1s → 2s → 4s → 8s → 上限 30s），并加入 ±20% 随机抖动（jitter）。如果所有在线用户同时 Watchdog 触发并立即重连，会对刚恢复的服务端造成瞬间冲击。

---

### 4. 前端 SSE 消费端选型（POST + fetch-event-source）

#### 4.1 为什么不能用原生 `EventSource`

浏览器原生 `EventSource` API 仅支持 `GET` 请求。但 Agent Run 的 `StartRunRequest` 携带了：
- 完整的对话历史（messages）
- 用户附件引用（attachments）
- 运行时覆盖配置（retry_policy_overrides）

这些数据量经常超出 URL 长度限制（通常为 8KB）。**必须使用 `POST` Body 传递请求体，建立 SSE 连接。**

> **输入契约不在本文展开。** `attachments` / 文本+图+文档分流 / CLI `@path` 与「仅文本点名文件」策略见 [`用户Turn输入与附件契约设计.md`](./用户Turn输入与附件契约设计.md)；视觉 Parts 与 `view_image` 见 [`多模态输入与视觉能力设计.md`](./多模态输入与视觉能力设计.md)。本文只规范 SSE **输出**事件与重试。

#### 4.2 推荐选型：`@microsoft/fetch-event-source`

```typescript
import { fetchEventSource } from '@microsoft/fetch-event-source';

fetchEventSource('/v1/runs/start', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(startRunRequest),
    onmessage(event) {
        resetWatchdog();
        switch (event.event) {
            case 'ping':             handlePing(event.data); break;
            case 'block.start':      handleBlockStart(event.data); break;
            case 'block.delta':      handleBlockDelta(event.data); break;
            case 'block.stop':       handleBlockStop(event.data); break;
            case 'run.completed':    handleRunCompleted(event.data); break;
            case 'intervention.required': handleIntervention(event.data); break;
            // ...
        }
    },
    onerror(err) {
        // fetch-event-source 在这里抛出异常会停止重连
        // 业务层应决定：是重连还是终止，是否需要通知用户
        throw err;
    },
    openWhenHidden: true, // 页面切到后台时保持连接（Agent 长跑任务需要）
});
```

该库由微软维护，底层使用标准 `fetch` + `ReadableStream`，支持：
- `POST` 方法和自定义 Headers
- 自动重连（支持 `Last-Event-ID` 续流）
- 页面可见性感知（`openWhenHidden`）
- 完整的 TypeScript 类型支持

---

### 5. 大结果懒加载降级（Large Result Lazy-Loading）

#### 5.1 问题根因分析

`tool_result` / `rag_search` 场景下，工具可能返回大体量数据（如 5MB 日志文件、grep 数万行匹配），若通过 `json_delta` 全量分片推入 SSE 管道，会造成**双重灾难**：

| 层面 | 问题 |
|------|------|
| **网络层** | SSE 管道被大包占满，后续推理 token 被阻塞延迟；连接总带宽告急 |
| **渲染层（更严重）** | `json_delta` 是逐字符/逐 Token 的增量协议，数万次 delta 触发数万次 DOM/state 更新，直接卡死浏览器主线程，产生肉眼可见的 UI 冻结 |

#### 5.2 降级策略：资源引用 + 摘要 + 可选预览

当工具返回的原始数据超过阈值时，后端采用以下降级策略：

**默认阈值：50KB**（合理起点，可按业务场景在 RetryPolicy 同层配置中调整）

**`block.stop` 大结果降级格式：**

```json
{
  "block_index": 4,
  "stop_reason": "complete",
  "result_summary": "日志共 12,847 行，发现 3 处 ERROR",
  "large_result": {
    "resource_id":  "res_abc123",
    "size_bytes":   5242880,
    "ttl_seconds":  86400,
    "fetch_url":    "/v1/resources/res_abc123",
    "preview": {
      "type":    "head_tail",
      "head":    "2026-07-09 08:00:01 [INFO] Server starting...\n...",
      "tail":    "...\n2026-07-09 08:47:33 [ERROR] Connection refused: dial tcp 127.0.0.1:5432",
      "head_lines": 5,
      "tail_lines": 5
    }
  }
}
```

| 字段 | 说明 |
|------|------|
| `resource_id` | 后端将大结果落盘/对象存储后生成的唯一 ID |
| `size_bytes` | 原始数据大小，前端可据此决定是否提示用户"较大文件" |
| `ttl_seconds` | 资源过期时间（建议 24h），过期后前端需提示用户"结果已过期" |
| `fetch_url` | 前端懒加载全量数据的 REST 接口地址 |
| `preview.type` | 预览策略：`head_tail`（日志类）/ `head`（代码/文档类）/ `none` |
| `preview.head` | 内容前 N 行/字节（UTF-8 截断，不截断多字节字符） |
| `preview.tail` | 内容最后 N 行，帮助用户快速定位最近错误（日志类特别有价值） |

#### 5.3 按数据类型细化预览策略

| 数据类型 | 预览策略 | 降级门槛建议 |
|----------|----------|-------------|
| 运行日志 | `head_tail`，前 5 行 + 后 10 行 | 50KB |
| 代码文件 | `head`，前 50 行 | 100KB（代码通常需要上下文，多给一点） |
| 搜索结果 | 返回前 N 条命中摘要（`result_summary` 字段已覆盖） | 按条数控制，不按字节 |
| 结构化 JSON | 顶层 Key 列表 + Value 类型提示，不展示具体值 | 50KB |
| 二进制数据 | 不预览，仅显示类型、大小、下载链接 | 任意大小直接降级 |

#### 5.4 前端懒加载 REST 接口

前端用户点击"查看完整内容"时，通过独立 REST 接口拉取，不走 SSE 管道：

```text
GET /v1/resources/{resource_id}
Authorization: Bearer {token}

→ 200 OK
Content-Type: application/octet-stream  （或 application/json，依内容类型）
Content-Disposition: attachment; filename="tool_result_abc123.log"
X-Resource-Size: 5242880
X-Resource-Expires: 2026-07-10T15:00:00Z

{...完整内容...}
```

> **设计约束**：`resource_id` 必须是**一次性 / 短生命周期**的凭证，绑定到当前 Session 和 User，不可猜测、不可跨 Session 访问，以防工具结果中的敏感数据泄露。

---

### 6. 多端适配架构（CLI / Desktop / Web）

#### 6.1 通用事件语义层（三端共享）

Agent Loop 内部使用 `progress.Event`（`internal/runtime/progress`）作为**跨产品的进程内事件总线**，所有产品都通过 `progress.WithSink(ctx, sink)` 注入接收器、`progress.Emit(ctx, event)` 发出事件。

`progress.Event` 与 SSE Block 模型的语义对应关系：

| `progress.Event` 字段 | SSE Block 模型对应 | 说明 |
|----------------------|--------------------|------|
| `Kind` = `run` | `run.*` 事件族 | Run 级别生命周期 |
| `Kind` = `llm` | `block_type=thinking` / `text` | LLM 推理过程 |
| `Kind` = `tool` | `block_type=tool_input` / `tool_result` | 工具调用两阶段 |
| `Kind` = `skill` | `block_type=agent_call` | 子 Agent / Skill 调用 |
| `Kind` = `sandbox` | `block_type=tool_result`（沙箱执行） | 代码/命令执行 |
| `Phase` = `start` | `block.start` 事件 | Block 开始 |
| `Phase` = `progress` | `block.delta` 事件 | Block 增量内容 |
| `Phase` = `complete` | `block.stop(stop_reason=complete)` | Block 完成 |
| `Phase` = `error` | `block.stop(stop_reason=error)` + `error.*` | Block 出错 |
| `Name` | `block.start.name` / `display_label` | 工具名或步骤标识 |
| `Summary` | `block.stop.result_summary` | 结果摘要 |
| `Detail` | `block.delta.value` | 增量内容或完整输出 |

> **目标**：三端共享的 `progress.Event` 结构应逐步补齐 `BlockIndex`、`StepIndex`、`ContentType`、`StopReason` 等 SSE Block 模型字段，使各端适配层只需做传输格式转换，不需要做语义重映射。

---

#### 6.2 CLI 端适配（进程内 BubbleTea 分发）

**架构**：Agent Runtime 与 TUI 在同一进程，无网络开销，直接通过 Go channel 传递。

```text
Agent Runtime
  └─ progress.Emit(ctx, event)
       └─ progress.Sink (= tea.Program.Send 的包装)
            └─ BubbleTea Update() 收到 progressMsg
                 └─ TUI 渲染（Viewport 追加内容）
```

**CLI 特有约束**：

| 方面 | 说明 |
|------|------|
| 无 HTTP | 不需要 `Content-Type: text/event-stream`，不需要 `Last-Event-ID`，无 Keep-Alive ping |
| 无断连重连 | 进程内 channel 不会断开，Run 失败即进程内错误处理 |
| 无 Stream Profile | CLI 直接接收完整事件集，由 TUI 渲染层决定是否展示（如折叠 thinking block） |
| 流式输出 | `Phase=progress` / `block.delta` 需驱动 TUI viewport 逐 token 追加；**当前实现待补齐** |
| 人工干预 | 通过 `approval.ApprovalRequiredMsg` 进入 BubbleTea 键盘交互流程，等效于 SSE 的 `intervention.required` |
| 大结果 | CLI 可直接在 viewport 中分页展示，或写入临时文件后提示用户路径；不需要 REST 懒加载 |

**CLI Sink 注入示意：**

```go
// products/cli/internal/tui/chat/update.go 中启动 Run
func startRun(ctx context.Context, prog *tea.Program, svc app.AgentService, req app.RunRequest) tea.Cmd {
    return func() tea.Msg {
        // 把 BubbleTea 的 Program.Send 包装为 progress.Sink
        sink := func(e progress.Event) {
            prog.Send(progressMsg{event: e})
        }
        ctx = progress.WithSink(ctx, sink)
        result, err := svc.Run(ctx, req)
        if err != nil {
            return runErrorMsg{err: err}
        }
        return runCompleteMsg{result: result}
    }
}
```

---

#### 6.3 Desktop 端适配（Wails IPC 推送）

**架构**：Wails 嵌入 Chromium，Go 后端与 JS 前端通过 Wails `runtime.EventsEmit` 双向通信，无需 HTTP 服务器，无端口冲突。

```text
Agent Runtime
  └─ progress.Emit(ctx, event)
       └─ progress.Sink (= wails runtime.EventsEmit 的包装)
            └─ Chromium JS 层
                 └─ window.runtime.EventsOn("block.delta", handler)
                      └─ Vue/React 组件更新
```

**事件命名约定**：Wails 事件名直接复用 SSE 事件名（`block.start`、`block.delta`、`block.stop`、`run.completed` 等），payload 结构与 SSE 的 `data:` JSON **完全一致**，便于 Web 和 Desktop 复用同一套前端逻辑（仅替换 transport 层）。

```go
// products/desktop/bootstrap 中注入 Wails sink
func buildDesktopSink(ctx context.Context) progress.Sink {
    return func(e progress.Event) {
        // 把 progress.Event 映射为 SSE 兼容 payload
        payload := mapToSSEPayload(e)
        eventName := mapToSSEEventName(e) // e.g. "block.delta", "run.completed"
        runtime.EventsEmit(ctx, eventName, payload)
    }
}
```

**Desktop 与 Web 的差异：**

| 方面 | Web（HTTP SSE） | Desktop（Wails IPC） |
|------|----------------|---------------------|
| 传输 | HTTP POST + `text/event-stream` | Wails `runtime.EventsEmit` |
| 断连重连 | 需要 `Last-Event-ID` 续流 | 进程内，不会断连 |
| 保活 ping | 需要（见 §3） | **不需要**，Wails IPC 由进程管理 |
| 大结果懒加载 | REST `/v1/resources/{id}` | 可直接通过 Wails binding 调用 Go 方法读取，无需额外 HTTP 接口 |
| 认证 | Bearer Token，每次请求携带 | 进程内，无需网络认证 |
| Stream Profile | 通过 `?profile=` 参数区分 | 由 Desktop Sink 过滤事件（代码控制） |

---

#### 6.4 Web（Enterprise）端适配（HTTP SSE）

这是 §1–§5 完整描述的方案，不再重复。关键要点回顾：

- 使用 `POST + @microsoft/fetch-event-source`（§4）
- 必须设置 `X-Accel-Buffering: no`（§1.2）
- 使用 `event: ping` 保活（§3）
- 大结果走 REST 懒加载（§5）
- 断连用 `Last-Event-ID` 续流，重连也用 POST（§1.9）

---

#### 6.5 三端能力矩阵

| 功能 | CLI（BubbleTea） | Desktop（Wails IPC） | Web（HTTP SSE） |
|------|:---------------:|:-------------------:|:---------------:|
| 流式文本输出（逐 token） | ✅ 进程内 channel | ✅ Wails EventsEmit | ✅ block.delta |
| 思考过程展示 | ✅（默认折叠） | ✅（默认折叠） | ✅（conversation profile 隐藏） |
| 工具调用可视化 | ✅ TUI 步骤行 | ✅ UI 卡片 | ✅ tool_input block |
| 人工干预 / 审批 | ✅ 键盘交互 | ✅ 对话框 | ✅ intervention.required |
| 断连自动重连 | ❌ 不需要 | ❌ 不需要 | ✅ Last-Event-ID |
| Keep-Alive ping | ❌ 不需要 | ❌ 不需要 | ✅ event: ping |
| 大结果懒加载 | ⚡ 本地文件分页 | ⚡ Wails binding | ✅ REST /v1/resources/ |
| Studio 调试事件 | ❌（CLI 无 Studio） | ✅ debug profile | ✅ ?profile=debug |
| 多租户隔离 | ❌（单用户） | ❌（单用户） | ✅ tenant_id 公共字段 |

> ⚡ = 各端有等效实现但实现方式不同

---

#### 6.6 后续实现规划 (Roadmap & Gaps)

在 Phase 1B 完成了 SSE 长连接、Timer 心跳以及大结果懒加载降级等核心地基后，以下高级协议特性已规划为后续阶段的演进任务：

##### 1. 断连重连与事件回放 (Last-Event-ID Replay)
- **阶段**: Phase 2.5 / Phase 3
- **描述**: 当客户端网络瞬断并携带 `Last-Event-ID` 请求头重连时，服务端需根据该 seq 回放历史事件。
- **依赖**: 需引入会话级别或运行级别的 SSE 事件持久化日志（如 Redis 或数据库 TTL 缓存，保存 24h 内的事件流）。

##### 2. Web 端干预审批回写 API
- **阶段**: Phase 2 (多人审批与治理)
- **描述**: 对应 `intervention.required` 事件，前端通过 `POST /v1/runs/{run_id}/interventions/{id}/respond` 端点提交授权、拒绝或修改后的参数，以异步唤醒处于 `run.paused` 状态的 ReAct 引擎。
- **依赖**: 结合 `PermissionEngine` 与异步协程唤醒（Go channel notify）机制。

##### 3. 消费端 Profile 裁剪过滤 (Stream Profiles)
- **阶段**: Phase 2
- **描述**: 根据客户端请求的 `profile` 参数（`conversation` / `debug` / `api`），在 `mapProgressEvent` 输出时动态剪裁和过滤事件集（例如正式对话隐藏 `thinking` 块与完整工具入参，调试模式全量保留）。

##### 4. 重试退避的 SSE 可观测事件 (Retry error events)
- **阶段**: Phase 2.5
- **描述**: 当 LLM/工具调用触发 `RetryPolicy` 指数退避等待时，需向 SSE 通道广播 `error.retriable` 事件（包含 `retry_attempt` 和 `retry_after_ms`），使前台界面展示“正在进行第 X 次自动重试，等待 Y 秒后继续”的可观测提示。
