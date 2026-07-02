/**
 * 智能问答演示 Mock 数据
 * 模拟 Agent 流式响应、思维链、工具调用、结构化卡片等场景
 */

/** 会话列表 Mock 数据 */
export const mockConversations = [
  {
    key: 'conv-1',
    label: 'ReAct 推理演示',
    timestamp: Date.now() - 1000 * 60 * 5,
  },
  {
    key: 'conv-2',
    label: '代码审查助手',
    timestamp: Date.now() - 1000 * 60 * 30,
  },
  {
    key: 'conv-3',
    label: '数据分析报告',
    timestamp: Date.now() - 1000 * 60 * 60 * 2,
  },
];

/** 预设快捷提问（对应四种演示场景） */
export const mockPrompts = [
  // thinking 场景
  { key: 'prompt-1', label: '解释 ReAct Agent 的工作原理', icon: '🤖', scenario: 'thinking' },
  // tool-call 场景
  { key: 'prompt-2', label: '帮我写一段 Go HTTP 中间件代码', icon: '💻', scenario: 'tool-call' },
  // card 场景
  { key: 'prompt-3', label: '制定一个微服务拆分方案', icon: '📋', scenario: 'card' },
  // markdown 场景
  { key: 'prompt-4', label: '介绍 Genesis Agent 的整体架构', icon: '🏗️', scenario: 'markdown' },
];

/**
 * 思维链步骤状态
 * 映射到 ThoughtChain 的有效值：
 * - 'loading'  → 处理中
 * - 'success'  → 已完成
 * - 'error'    → 出错
 * - undefined  → 等待中
 */
export type ThoughtStepStatus = 'loading' | 'success' | 'error' | undefined;

export interface ThoughtStep {
  title: string;
  description?: string;
  status: ThoughtStepStatus;
}

/** 演示场景类型 */
export type MockScenario = 'markdown' | 'thinking' | 'tool-call' | 'card';

/** 根据输入关键词识别演示场景 */
export function detectScenario(input: string): MockScenario {
  const lower = input.toLowerCase();
  // 工具调用场景
  if (lower.includes('代码') || lower.includes('code') || lower.includes('写') || lower.includes('中间件') || lower.includes('函数')) {
    return 'tool-call';
  }
  // 结构化卡片场景（计划/分析/拆分类任务）
  if (
    lower.includes('计划') ||
    lower.includes('计算') ||
    lower.includes('plan') ||
    lower.includes('分析') ||
    lower.includes('制定') ||
    lower.includes('拆分') ||
    lower.includes('方案') ||
    lower.includes('微服务')
  ) {
    return 'card';
  }
  // 思维链推理场景
  if (
    lower.includes('react') ||
    lower.includes('工作原理') ||
    lower.includes('解释') ||
    lower.includes('agent') ||
    lower.includes('原理') ||
    lower.includes('介绍')
  ) {
    return 'thinking';
  }
  return 'markdown';
}

/** Markdown 场景回复 */
export const markdownResponse = `
## Genesis Agent 简介

**Genesis Agent** 是一个**通用、可扩展、生产级**的 Agent Runtime，支持多业务场景：

### 核心能力

| 能力 | 描述 | 状态 |
|------|------|------|
| ReAct Loop | 推理-行动循环执行 | ✅ 已实现 |
| Plan-Execute | 任务规划与分步执行 | ✅ 已实现 |
| Agentic RAG | 自主多轮检索增强 | ✅ 已实现 |
| 多 Agent 协作 | 子 Agent 调用与协作 | 🔧 开发中 |

### 核心设计原则

1. **Run Engine 是内核**，Loop 只是可替换策略
2. **扩展点优先**，接口驱动
3. **所有触发走 Event**，所有等待可恢复
4. **租户隔离贯穿所有层**

\`\`\`go
type RunEngine interface {
    Start(ctx context.Context, req StartRunRequest) (*Run, error)
    Resume(ctx context.Context, runID string, event ResumeEvent) error
    Pause(ctx context.Context, runID string, reason string) error
    Cancel(ctx context.Context, runID string) error
}
\`\`\`
`.trim();

/** 思维链步骤数据 */
export const thinkingSteps: ThoughtStep[] = [
  { title: '理解用户意图', description: '分析输入：用户询问 ReAct Agent 的工作原理', status: undefined },
  { title: '检索知识库', description: '搜索相关文档：agent_loop_design.md', status: undefined },
  { title: '组织回答结构', description: '确定回答框架：定义 → 流程 → 示例', status: undefined },
  { title: '生成最终回答', description: '综合知识，生成结构化 Markdown', status: undefined },
];

export const thinkingResponse = `
## ReAct Agent 工作原理

**ReAct**（Reasoning + Acting）将**推理**与**行动**交替进行：

### 核心循环

\`\`\`
LOOP（每轮）：
  1. 思考 (Think)   — LLM 推理当前状态，输出下一步计划
  2. 行动 (Act)     — 执行工具调用 / RAG 检索 / 子 Agent 调用
  3. 观察 (Observe) — 获取执行结果，作为下轮输入
  4. 判断           — 是否已可生成最终答案？
\`\`\`

### 停止条件

- LLM 输出 \`final_answer\` 动作
- 达到最大迭代次数 \`max_iterations\`
- Token 预算耗尽
- 执行超时

### Step 记录

\`\`\`go
type Step struct {
    ActionType    ActionType      // think / tool_call / rag_search / final_answer
    ActionPayload json.RawJSON
    Observation   json.RawJSON
    TokenUsage    TokenUsage
}
\`\`\`
`.trim();

/** 工具调用场景回复 */
export const toolCallResponse = `
## Go HTTP 链路追踪中间件

\`\`\`go
package middleware

import (
    "context"
    "net/http"
    "time"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/trace"
)

// TraceMiddleware 为每个请求创建 OpenTelemetry Span
func TraceMiddleware(next http.Handler) http.Handler {
    tracer := otel.Tracer("genesis-agent")

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx, span := tracer.Start(r.Context(), r.URL.Path,
            trace.WithAttributes(
                attribute.String("http.method", r.Method),
                attribute.String("http.url", r.URL.String()),
            ),
        )
        defer span.End()

        ctx = context.WithValue(ctx, "trace_id", span.SpanContext().TraceID().String())

        start := time.Now()
        rw := &responseWriter{ResponseWriter: w, statusCode: 200}
        next.ServeHTTP(rw, r.WithContext(ctx))

        span.SetAttributes(
            attribute.Int("http.status_code", rw.statusCode),
            attribute.Float64("http.duration_ms", float64(time.Since(start).Milliseconds())),
        )
    })
}

type responseWriter struct {
    http.ResponseWriter
    statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
    rw.statusCode = code
    rw.ResponseWriter.WriteHeader(code)
}
\`\`\`

### 使用方式

\`\`\`go
mux := http.NewServeMux()
handler := TraceMiddleware(mux)
\`\`\`
`.trim();

/** 结构化卡片场景文本回复 */
export const cardResponse = `
## 微服务拆分方案

基于 **DDD 领域驱动设计**，分 4 个阶段、共 8 周完成渐进式拆分：

### 核心设计原则

- **单一职责**：每个服务专注一个限界上下文
- **数据自治**：服务间不共享数据库，通过事件通信
- **接口优先**：先用 OpenAPI 定义契约，再实现
- **绞杀者模式**：新旧并行，逐步切流，避免大爆炸重写

### 风险评估

| 风险 | 概率 | 应对策略 |
|------|------|----------|
| 数据一致性 | 中 | Outbox 模式 + 幂等消费 |
| 服务间延迟增加 | 高 | 服务网格 + 熔断降级 |
| 团队学习成本 | 中 | 分批培训 + 内部分享 |

> 📋 **结构化任务卡片已在下方生成**，包含 4 个阶段 10 个具体任务
`.trim();

/** 模拟流式输出 */
export function simulateStream(
  text: string,
  onChunk: (chunk: string, done: boolean) => void,
  delay = 30,
): () => void {
  let index = 0;
  let cancelled = false;

  function nextChunk() {
    if (cancelled) return;
    if (index >= text.length) {
      onChunk('', true);
      return;
    }
    const chunkSize = Math.floor(Math.random() * 4) + 2;
    const chunk = text.slice(index, index + chunkSize);
    index += chunkSize;
    onChunk(chunk, false);
    setTimeout(nextChunk, delay);
  }

  setTimeout(nextChunk, 100);
  return () => { cancelled = true; };
}
