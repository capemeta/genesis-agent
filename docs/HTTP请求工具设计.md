# HTTP 请求工具设计

## 1. 目标与定位

本文档定义 `genesis-agent` 项目级 HTTP Client 的第一版落地方案。

定位：

- 它是 **平台级通用基础设施**，目录落点为 `internal/platform/httpclient/`。
- 它负责解决“如何稳定、可观测、可治理地发起 HTTP 请求”。
- 它不承载 Sandbox、LLM Provider、Webhook、MCP Gateway 等具体上游协议语义。
- 具体上游协议映射由各能力域自己的 adapter 承担，例如：
  - `internal/capabilities/sandbox/adapter/http`
  - `internal/capabilities/llm/adapter/...`

核心结论：

- **基于 `net/http` 自建一层项目级 HTTP Client。**
- 不直接把 `resty` 作为长期核心抽象。
- 若未来确实借用第三方能力，也必须隐藏在 `platform/httpclient` 内部，不能向上层暴露第三方调用风格。

---

## 2. 为什么不用 resty 作为核心抽象

`resty` 适合快速对接普通 REST API，但当前项目的诉求更偏“Agent Runtime 平台基础设施”：

- 既有普通 JSON 请求，也有 SSE、长连接流、Webhook、Sandbox、未来 gRPC 并存场景。
- 需要统一 trace、日志、错误分类、认证、重试、超时、审计信息透传。
- 需要让项目形成自己的请求/响应/错误约定，而不是把调用风格散落在每个业务点。

因此最佳实践是：

- 基于 `net/http` 自建项目抽象。
- 通过 middleware / transport / policy 组合功能。
- 让上层能力代码只依赖项目自己的 `Request` / `Response` / `Error` / `EventStream` 模型。

---

## 3. 职责边界

```text
platform/httpclient         # 通用 HTTP 基础设施
capabilities/*/adapter/http # 具体上游协议适配
interfaces/http            # 主平台服务端对外 HTTP 入口
```

边界说明：

- `platform/httpclient`
  - 负责：请求发送、URL 拼装、header、认证、超时、重试、错误归一、日志、trace、SSE 解码。
  - 不负责：Sandbox 领域字段、LLM Provider 特定字段、Webhook 签名语义、业务错误码解释。
- `capabilities/*/adapter/http`
  - 负责：把某个领域请求映射到 HTTP 协议，并把响应映射回领域对象。
- `interfaces/http`
  - 负责：主平台自身对外 API 的 handler / router / DTO / SSE 输出。

这三层必须分开，避免 `httpclient` 逐渐膨胀成“什么都管”的大目录。

---

## 4. 目录建议

```text
internal/platform/httpclient/
  client.go        # Client 接口与默认实现入口
  request.go       # Request / Response / StreamRequest 等基础模型
  errors.go        # 统一错误模型与错误分类
  transport.go     # http.Transport / RoundTripper 装配
  middleware.go    # middleware / decorator 链
  retry.go         # RetryPolicy、退避与 Retry-After 处理
  auth.go          # bearer / api-key / basic / custom header
  json.go          # JSON encode / decode
  sse.go           # SSE 事件流读取与解码
  logging.go       # 请求日志、脱敏、截断
  trace.go         # trace/span 注入
  options.go       # Client / Transport / Middleware 选项
```

说明：

- 第一版不要求所有文件一次性全部落地，但接口命名和职责边界建议按这个结构设计。
- 如果某一能力暂时很薄，可以先合并到少量文件中，后续再自然拆分。

---

## 5. 第一版必须支持的能力

第一版建议必须具备：

- `Do(ctx, req)`
- `StreamSSE(ctx, req)`
- `BaseURL + Path + Query` 拼装
- Header / Auth
- JSON 编解码
- 请求级超时覆盖
- Retry
- 统一错误模型
- Logging
- Trace
- 可配置 `http.Transport`
- `Context` 全链路贯穿
- 请求体 / 响应体大小限制

第一版可以暂缓：

- Multipart 上传
- Metrics
- Circuit Breaker
- Rate Limiter
- mTLS
- Proxy 特殊策略
- Cookie Jar
- 自动分页
- HTTP/3 特殊优化

---

## 6. 核心接口设计

### 6.1 Client 接口

```go
package httpclient

import "context"

type Client interface {
    Do(ctx context.Context, req *Request) (*Response, error)
    StreamSSE(ctx context.Context, req *Request) (EventStream, error)
}
```

说明：

- `Do` 用于普通 HTTP 请求，返回完整响应。
- `StreamSSE` 用于服务端事件流，避免把 SSE 硬塞进普通请求路径。
- 所有调用都必须显式传 `context.Context`。

### 6.2 Request 模型

```go
package httpclient

import (
    "net/http"
    "net/url"
    "time"
)

type Request struct {
    Method  string
    BaseURL string
    Path    string
    Query   url.Values

    Headers http.Header

    Body        any
    ContentType string

    Auth *AuthConfig

    Timeout time.Duration
    Retry   *RetryPolicy

    ExpectedStatus []int

    IdempotencyKey string

    Metadata map[string]string
}
```

字段建议：

- `BaseURL + Path` 统一由 client 负责拼装，避免调用方自己手写 URL。
- `Headers` 建议使用 `http.Header`，以支持多值 header，并与标准库保持一致。
- `Body` 用于 JSON 或原始字节输入；具体编码策略由实现决定。第一版优先支持可重放 body（如 JSON 对象、`[]byte`、`string`），不要默认支持不可重放流式请求体自动重试。
- `Timeout` 用于单请求覆盖默认超时。
- `Retry` 用于单请求覆盖默认重试策略。
- `ExpectedStatus` 用于精确声明成功状态集合，避免把所有 2xx 都当成成功。
- `IdempotencyKey` 用于幂等创建类请求。
- `Metadata` 不参与传输协议本身，主要供日志、trace、审计附加上下文使用。

### 6.3 Response 模型

```go
package httpclient

import "net/http"

type Response struct {
    StatusCode int
    Headers    http.Header
    Body       []byte
}
```

说明：

- `Response` 保持尽量薄。
- JSON 反序列化可由上层 helper 完成，不建议把业务对象解码强耦合进统一 Client 主路径。

### 6.4 SSE 接口

```go
package httpclient

type SSEEvent struct {
    ID    string
    Event string
    Data  []byte
    Retry int
}

type EventStream interface {
    Recv() (*SSEEvent, error)
    Close() error
}
```

SSE 特殊要求：

- 支持 `event:` / `data:` / `id:` / `retry:`。
- 支持注释行与心跳帧。
- 支持 `Last-Event-ID`。
- 不透明自动重连；重连策略应交由调用方或上层能力决定。

---

## 7. 统一错误模型

不要直接把 `net/http` 原始错误向上抛出。建议统一为项目错误类型：

```go
package httpclient

type ErrorKind string

const (
    ErrorKindTimeout         ErrorKind = "timeout"
    ErrorKindCanceled        ErrorKind = "canceled"
    ErrorKindNetwork         ErrorKind = "network"
    ErrorKindUnauthorized    ErrorKind = "unauthorized"
    ErrorKindForbidden       ErrorKind = "forbidden"
    ErrorKindNotFound        ErrorKind = "not_found"
    ErrorKindRateLimited     ErrorKind = "rate_limited"
    ErrorKindUpstream        ErrorKind = "upstream"
    ErrorKindInvalidResponse ErrorKind = "invalid_response"
    ErrorKindDecode          ErrorKind = "decode"
    ErrorKindSSE             ErrorKind = "sse"
    ErrorKindTooLarge        ErrorKind = "too_large"
)

type Error struct {
    Kind         ErrorKind
    Message      string
    StatusCode   int
    Retryable    bool
    Operation    string
    URL          string
    RequestID    string
    UpstreamCode string
    RawBody      []byte
    Err          error
}
```

错误映射建议：

- `context.DeadlineExceeded` -> `timeout`
- `context.Canceled` -> `canceled`
- DNS / dial / reset / TLS 等网络错误 -> `network`
- `401` -> `unauthorized`
- `403` -> `forbidden`
- `404` -> `not_found`
- `429` -> `rate_limited`
- `5xx` -> `upstream`
- JSON / SSE 解码失败 -> `decode` / `sse`
- 响应体超限 -> `too_large`

实现要求：

- `Error` 必须实现 `error` 接口。
- 需要支持 `Unwrap()`，便于 `errors.Is/As`。
- `RawBody` 不能无上限保存，必须截断。

---

## 8. 超时设计

超时不能只靠一个“大总超时”。建议至少分三层：

1. Client 默认超时
2. Request 级超时覆盖
3. Transport 级超时
   - dial timeout
   - TLS handshake timeout
   - response header timeout
   - idle conn timeout

最佳实践：

- 普通 JSON 请求可以配置默认总超时，例如 30s 或 60s。
- SSE 不应复用同一套短总超时逻辑。
- SSE 更适合主要依赖 `context.Context` 控制取消，底层 transport 只保留合理连接级超时。

建议：

- `http.Client.Timeout` 不要粗暴用于 SSE。
- `Do` 和 `StreamSSE` 可以内部走不同的 `http.Client` 或不同的请求策略。

---

## 9. 重试策略设计

### 9.1 原则

不要默认“一切都重试”。

默认只建议重试：

- 网络瞬时错误
- `429`
- `502`
- `503`
- `504`

默认只对幂等请求重试：

- `GET`
- `HEAD`
- `PUT`
- `DELETE`
- 或显式带 `Idempotency-Key` 的 `POST`

### 9.2 RetryPolicy 建议

```go
package httpclient

import "time"

type RetryPolicy struct {
    MaxAttempts      int
    InitialBackoff   time.Duration
    MaxBackoff       time.Duration
    Multiplier       float64
    Jitter           bool
    RetryStatusCodes []int
    RetryMethods     []string
}
```

实现要求：

- 支持 exponential backoff + jitter。
- 支持尊重 `Retry-After`。
- 支持基于错误类型判断是否可重试。
- SSE 流处理中不做透明自动拼接式重试，避免事件语义错乱。
- 对不可重放的请求体不自动重试；第一版应默认只对可重放 body 启用重试。
- 若后续需要支持文件流或大对象上传，应显式引入 body provider / rewind 机制，而不是在第一版里隐式兜底。

---

## 10. 认证设计

不要只支持 Bearer Token。建议统一抽象：

```go
package httpclient

type AuthType string

const (
    AuthTypeNone         AuthType = "none"
    AuthTypeAPIKeyHeader AuthType = "api_key_header"
    AuthTypeAPIKeyQuery  AuthType = "api_key_query"
    AuthTypeBearerToken  AuthType = "bearer_token"
    AuthTypeBasicAuth    AuthType = "basic_auth"
    AuthTypeCustomHeader AuthType = "custom_header"
)

type AuthConfig struct {
    Type       AuthType
    HeaderName string
    QueryName  string
    Username   string
    Password   string
    Token      string
    Value      string
}
```

要求：

- 认证头默认脱敏日志输出。
- 不在错误对象和日志中明文记录凭证。
- 与 LLM Provider 的 auth 风格保持一致。

---

## 11. Logging / Trace / 可观测性

### 11.1 Logging

日志建议默认只记录：

- method
- host
- path
- status_code
- duration
- retry_count
- request_id
- upstream_service（如有）

注意：

- 默认不要完整打印 request / response body。
- body 日志要支持开关、截断、脱敏。
- Authorization、API Key、Cookie 等字段必须默认脱敏。

### 11.2 Trace

HTTP Client 应接项目自己的 trace contract，而不是直接耦合某个 tracing SDK。

span 标签至少包含：

- method
- host
- path
- status_code
- retry_count
- duration
- error_kind
- request_id

### 11.3 Metrics Hook

第一版可以先不实现完整 metrics，但建议预留 hook 点，未来接：

- 请求次数
- 耗时分布
- 错误率
- 重试次数
- SSE 流持续时长

---

## 12. SSE 设计要点

SSE 不是“特殊一点的 GET”，而是持续事件流。

`StreamSSE` 设计要求：

- 独立于普通 JSON 请求路径。
- 支持逐事件读取，不一次性读完整 body。
- 支持服务端心跳帧。
- 支持客户端主动取消。
- 支持 `Last-Event-ID`。
- 支持流结束与异常中断的清晰区分。

不建议：

- 自动无限重连
- 自动拼接多次断流后的事件流
- 将 SSE 当成普通响应 body 一次性读取

说明：

- Agent 流式输出、Sandbox 日志流、未来部分长任务事件流都可能复用这条能力。
- 但具体“断流后如何重连、如何续传”属于上层能力策略，而不是通用 client 自动替你决定。

---

## 13. 连接复用与 Transport 配置

底层建议使用可复用的 `http.Transport`，并由 `Client` 统一管理，避免每次请求新建 transport。

建议考虑的配置：

- `MaxIdleConns`
- `MaxIdleConnsPerHost`
- `IdleConnTimeout`
- `TLSHandshakeTimeout`
- `ResponseHeaderTimeout`
- `ExpectContinueTimeout`
- `DisableCompression`

原则：

- 默认值偏稳健，不追求过早性能极限。
- 所有参数支持通过 option 或配置覆盖。
- 普通请求和 SSE 请求可复用大部分 transport，但超时控制策略应分开。

---

## 14. Body 大小限制

必须支持 request / response body 大小限制，避免被异常上游拖垮内存。

建议：

- 默认限制普通响应体最大大小，例如 4MB 或 8MB。
- 对需要大响应的能力域允许显式覆盖。
- 超限时返回 `ErrorKindTooLarge`。
- 错误对象中的 `RawBody` 只保留截断后的前缀。

---

## 15. 配置建议

建议在 `platform/config` 中支持 HTTP Client 通用配置，例如：

```yaml
http_client:
  default_timeout: 30s
  sse_idle_timeout: 0s
  response_header_timeout: 15s
  tls_handshake_timeout: 10s
  idle_conn_timeout: 90s
  max_idle_conns: 100
  max_idle_conns_per_host: 10
  max_response_body_bytes: 4194304
  retry:
    max_attempts: 3
    initial_backoff: 200ms
    max_backoff: 2s
    multiplier: 2.0
    jitter: true
```

原则：

- 提供平台默认值。
- 允许请求级覆盖部分策略，例如 timeout、retry、expected status。
- 不建议让调用方任意覆盖所有 transport 细节；应保持平台默认治理能力。
- `User-Agent`、`X-Request-ID`、链路透传 header 等平台级约定，建议由默认 middleware 统一注入，而不是散落在调用方。

---

## 16. 与上层能力的关系

推荐调用路径：

```text
capabilities/sandbox/service
  -> capabilities/sandbox/adapter/http
    -> platform/httpclient

capabilities/llm/service
  -> capabilities/llm/adapter/xxx
    -> platform/httpclient
```

约束：

- 能力域 adapter 不直接到处手写 `net/http`。
- 能力域自己的错误，应该在 adapter 层基于 `httpclient.Error` 做二次映射。
- `httpclient` 不反向依赖任何能力域。

---

## 17. 测试建议

第一版至少应覆盖：

- URL 拼装
- Header / Auth 注入
- JSON 编解码
- 超时与 context cancel
- Retry 行为
- `Retry-After` 处理
- 错误分类映射
- 响应体大小限制
- SSE 事件解析
- 日志脱敏

推荐分层：

- 单测：请求模型、错误映射、重试策略、SSE 解码。
- 集成测试：基于 `httptest.Server` 覆盖真实 HTTP 行为。
- 不依赖公网，不引入外部不稳定因素。

---

## 18. 第一版实现顺序

建议实现顺序：

1. `Request` / `Response` / `Error` 基础模型
2. `Client.Do`
3. URL / Header / Auth / JSON
4. 超时与 `context` 处理
5. Retry
6. Logging / Trace
7. Response body limit
8. `StreamSSE`
9. 配置集成

这样可以先把普通请求路径打稳，再补流式能力。

---

## 19. 最终结论

对当前项目来说，最佳实践不是“找一个功能最多的 HTTP 库直接包住”，而是：

- 用 `net/http` 建立自己的平台级抽象。
- 把超时、重试、认证、日志、trace、错误模型、SSE 都收口到 `platform/httpclient`。
- 让具体上游协议适配放在各自能力域的 `adapter/http`。
- 让主平台服务端对外接口继续留在 `interfaces/http`。

一句话总结：

> `platform/httpclient` 负责“稳定发请求”，`capabilities/*/adapter/http` 负责“理解上游协议”，`interfaces/http` 负责“对外提供接口”。

