# Genesis Agent MCP 能力接入设计

> 状态：设计定稿 v1.1（Phase 1B/2 落地目标；经 review-fix-rereview 审查）
> 关联文档：`docs/agent loop设计.md`（§6.8 能力适用范围、§14 目录边界）、`docs/项目目录与边界说明.md`、`docs/统一配置权限与审批治理设计.md`、`docs/密钥与连接管理设计.md`、`AGENTS.md`
> 参考源码：`D:\workspace\go\go-project\Kode-CLI`（TypeScript）、`D:\workspace\go\go-project\codex`（Rust `codex-rs`）

本文给出 genesis-agent 接入 MCP（Model Context Protocol）的最佳架构设计，目标是：在 **cli / desktop / enterprise 三产品间平衡「统一内核」与「产品独立注入」**，复用现有 Tool Gateway、能力适用范围、审批/审计/Usage/Trace 治理链路，并保证扩展性、可维护性与优雅分层。

---

## 1. 第一性原理分析

在照搬任何参考实现之前，先把 MCP 接入还原为本质问题。

### 1.1 核心问题

MCP 让 Agent 能连接**外部工具服务器**（filesystem、browser、database、企业内部系统等），把这些 server 暴露的一组 tool（以及 resource / prompt）动态接入到当前 Run 的可用能力集合中，供 LLM 调用。

### 1.2 不变量（必须始终成立）

1. **LLM function schema 里只能出现 Tool 名**（`AGENTS.md` 硬约束）。MCP tool 最终必须以 `tool.Tool` 形态进入 LLM 可见集合，不能新造一类 LLM 可见原语。
2. **能力治理不可绕过**：任何 MCP tool 的执行都必须经过与本地 Tool 相同的适用范围过滤、权限/审批、审计、Usage、Trace。
3. **运行时内核（`internal/runtime`）不依赖任何具体 MCP 实现**；MCP 属于能力域 `internal/capabilities/mcp`。
4. **产品无关的通用逻辑放内核，产品特定的 endpoint/credential/策略/审批 UI 由 `products/<product>/bootstrap` 注入**。
5. **多租户、并发、安全**：远程连接凭证不落明文，连接生命周期可控，工具调用有超时与错误隔离。

### 1.3 约束

- Go 1.26；Windows/Linux 均需支持（stdio 子进程启动跨平台）。
- 开发阶段不兼容旧数据、旧结构；一次做对。
- 单文件 < 1000 行，接口优先，依赖注入组装。
- 现有 `CapabilityTypeMCP`、`RuntimeAdapter`、`ActionMCPCall`、`PackageTypeMCPPackage` 已是预留扩展点，必须复用而非另起炉灶。

### 1.4 失败条件（什么情况下设计是失败的）

- MCP tool 绕过 Tool Gateway 直接执行 → 治理失效。
- 三产品各自实现一份 MCP client → 重复、漂移、不可维护。
- 一次性把上百个 MCP tool 塞进 LLM schema → context 爆炸、成本失控。
- enterprise（HTTP API 端）默认放开高危 MCP server（如 filesystem/database）→ 越权。
- 连接管理无健康检查/超时 → 启动风暴、僵死连接、goroutine 泄漏。

---

## 2. 参考源码借鉴分析

### 2.1 两者定位对比

| 维度 | Kode-CLI（TS） | codex（Rust） | genesis-agent 取舍 |
|---|---|---|---|
| 协议栈 | 官方 `@modelcontextprotocol/sdk` | 官方 `rmcp` crate | **官方 `github.com/modelcontextprotocol/go-sdk`**，不自研 JSON-RPC |
| 角色 | Client + Server(`mcp serve`) | Client + 独立 Server crate | Phase 1 只做 **Client**；Server 暴露 Phase 2 可选 |
| 传输 | stdio/SSE/HTTP/WS + fallback | stdio/streamable-http | Phase 1：**stdio + streamable-http**；SSE 作为 HTTP 降级 |
| 连接管理 | 两套并存（memoize + Manager，欠统一） | 单一 `McpConnectionManager`（清晰） | **学 codex 单一 Manager**，避免 Kode 的双路径 |
| 命名 | `mcp__server__tool` + sanitize | 双命名（路由名 vs 模型可见名）+ sanitize + SHA1 去重 | **采用双命名**：路由用 (server, tool)，模型可见用 `mcp__server__tool` |
| 配置分层 | 全局/项目/`.mcp.json`/plugin 多来源合并 | `config.toml` + plugin + extension + requirements | **借鉴 catalog 多来源合并 + 优先级**，Phase 1 先做 config + marketplace 两来源 |
| 工具暴露 | 全量注入 + `MCPSearchTool` 按需 | direct / deferred exposure | **复用现有 `ToolExposure` direct/deferred**，多工具场景走 deferred + 检索 |
| 审批 | 项目文件 server 预连接审批 + 运行时工具审批 | per-tool approval_mode + elicitation | **复用现有 approval 能力**；project 来源 server 需预连接批准 |
| 安全 | workspace trust + headersHelper | 禁止 inline token，只允许 env 引用 + OAuth keyring | **强制 env 引用 token**，凭证走 credential 能力域 |

### 2.2 值得借鉴的点（→ 映射到本设计）

1. **官方 SDK 承载 transport + 协议**（Kode/codex 一致）→ 依赖 `modelcontextprotocol/go-sdk`，本域只做编排与治理适配。
2. **单一 ConnectionManager**（codex `connection_manager.rs`）→ `mcp/manager`，统一生命周期，杜绝 Kode 双路径问题。
3. **双命名体系**（codex `tools.rs` 的 `ToolInfo`）→ 路由名与模型可见名分离，规避 LLM function name 限制污染原始 MCP tool 名。
4. **Transport 抽象为 launcher/trait**（codex `stdio_server_launcher.rs`）→ Go `Transport` 接口 + `CommandTransport`/`StreamableHTTP` 实现，让「进程放哪」（本机 vs sandbox）与协议解耦。
5. **配置与 wire 类型分离**（codex `config/mcp_types.rs` vs `protocol/mcp.rs`）→ 本域 `config` 只描述运行时配置，不掺入 JSON-RPC 类型。
6. **多来源 catalog + 冲突消解 + 优先级**（codex `catalog.rs`）→ config server / marketplace MCP package 两来源合并。
7. **健康检查 + 失败退避 + 批量连接**（Kode `manager.ts`）→ Manager 内置 ping、退避、并发上限。
8. **listChanged → 工具缓存失效**（Kode `listChanged.ts`）→ 订阅 server 通知刷新 tool 快照。
9. **Resource/Prompt 不直接进 LLM schema**（Kode `ListMcpResourcesTool`）→ 用专用网关工具按需读取，控制 context。
10. **project 来源 server 预连接审批**（Kode `mcpServerApproval.tsx`）→ 不可信来源（随仓库进入的配置）连接前必须用户批准。
11. **禁止明文 token**（codex 安全默认）→ 配置只允许 `bearer_token_env` / credential 引用。
12. **启动可观测性**（codex `McpStartupUpdateEvent`）→ 每 server 启动状态进 Trace/审计，`required` server 失败可让无人值守任务 fail。
13. **WrappedClient 三态**（Kode `client/types.ts`）→ `ServerStatus` 区分 failed vs needs_auth，改善 UX 与 Enterprise API。
14. **Extension Contributor**（codex `ext/extension-api/contributors/mcp.rs`）→ `DefinitionSource` 接口，产品层注入 server 而不改内核。
15. **会话级 MCP 合并**（Kode ACP `apps/server/src/acp/agent/mcp.ts`）→ Desktop/Enterprise 支持 per-session 附加 server。
16. **企业 Requirements 过滤**（codex `mcp_requirements.rs`）→ Enterprise bootstrap 在 Catalog 后强制 disable 不合规 server。
17. **HTTP 管理 API 与 Run 内调用分离**（codex `app-server/mcp_processor.rs`）→ 管理面走 `/v1/mcp/*`，执行面仍走 Tool Gateway。

### 2.3 不照搬的点

- Kode 的**双连接路径**（memoize + Manager + ACP 第三份）→ 统一为一个 Manager。
- Kode 的 **Legacy Claude JSON 兼容** → 开发阶段不做。
- codex 的 **Codex Apps 内置 server / executor 远程双放置 / 复杂 plugin overlay** → 产品特定，Phase 1 不做（保留 Transport 抽象以便未来接 sandbox stdio）。
- codex 的 **SHA1 命名哈希** → 先用 `mcp__server__tool` + 数字后缀去重，够用再引入哈希。
- 自研 JSON-RPC server 手写 loop → 用官方 SDK server 能力（若做 Server 角色）。

### 2.4 参考实现的产品分层对照（统一 vs 独立）

| 层次 | Kode-CLI | codex | genesis-agent 映射 |
|---|---|---|---|
| **协议 + 传输** | `packages/core/src/mcp/` | `rmcp-client/` | `internal/capabilities/mcp/transport/` + go-sdk |
| **连接编排** | `client/manager.ts`（但与 memoize 双路径并存） | `codex-mcp/connection_manager.rs` | `mcp/manager/`（**只保留一条主线**） |
| **配置合并** | `client/config.ts` + `packages/config` | `catalog.rs` + `mcp_types.rs` | `mcp/catalog/` + `platform/config` DTO |
| **Tool 投影** | `client/tools.ts` + `packages/tools/registry` | `core/tools/handlers/mcp.rs` | `mcp/tooladapter/` → `tool.Registry` |
| **CLI 管理** | `apps/cli/commands/mcp/*` | `cli/mcp_cmd.rs` | `products/cli/internal/command/mcp_*` |
| **GUI/审批** | `MCPServerApprovalDialog` | `tui/mcp_startup.rs` | CLI TUI / Desktop Wails（待实现） |
| **HTTP API** | `apps/server/acp/agent/mcp.ts` | `app-server/mcp_processor.rs` | `products/enterprise/.../handler/mcp.go` |
| **企业管控** | workspace trust + headersHelper | `mcp_requirements.rs` | `RequirementsFilter` + `CapabilityScope` |
| **Server 角色** | `mcp/server.ts` | `mcp-server/` | M6 可选，`go-sdk` server |

**平衡原则**：上表前三行（协议/编排/配置/投影）**必须统一**在三产品共享的 `internal/capabilities/mcp`；后四行（CLI/GUI/API/管控）**允许产品独立**，但只通过 bootstrap 注入，不复制 client 逻辑。

---

## 3. 与现有架构的契合点（复用而非新建）

| 现有资产 | 位置 | MCP 如何复用 |
|---|---|---|
| `CapabilityType = "mcp"` | `internal/capabilities/capability/model/model.go:13` | MCP server 作为一类 capability 索引记录 |
| `RuntimeAdapter` / `RuntimeAdapterRegistry` | `internal/capabilities/capability/contract/contract.go:24-36` | 新增 MCP RuntimeAdapter，`Register/Unregister/SetEnabled` 驱动连接与 tool 投影 |
| `tool.Tool` / `tool.Registry` / `ToolTraits` | `internal/capabilities/tool/contract/interface.go` | MCP tool 投影成 `tool.Tool`，带 `NeedsPermission=true` traits |
| Tool Gateway | `internal/capabilities/tool/gateway/gateway.go` | MCP tool 执行统一走 Gateway：可见性、锁、Authorizer、Audit、Usage、Trace |
| `ToolExposure` direct/deferred/hidden | `interface.go:21-28` | 多工具场景标记 deferred，配合检索按需暴露 |
| `ActionMCPCall = "mcp.call"` | `internal/capabilities/approval/model/model.go` | MCP 调用审批动作类型 |
| approval / policy | `approval/service`、`policy/service/evaluator.go` | 通过 Gateway `Authorizer` 适配为执行前审批 |
| audit / usage / trace sink | `audit/contract`、`usage/contract`、`trace/contract` | Gateway 已自动打点，无需 MCP 重复实现 |
| credential / connection | `credential/service`、`connection/service` | 远程 server 的 token/endpoint 存取 |
| `Profile` / `CapabilityScope` / `ToolSet` | `internal/capabilities/profile/model/profile.go` | server 级与 tool 级适用范围过滤 |
| marketplace `PackageTypeMCPPackage` | `package/marketplace`、`shared/local/skillmarket/manifest.go` | MCP server 可作为可安装 Package 投影到能力索引 |
| bootstrap builder | `internal/bootstrap/builder.go` | 通用装配点，注册 MCP adapter 与 tool |

> 关键结论：MCP 不需要新的 LLM 可见原语，也不需要改 ReAct loop。**MCP tool = 一种“远程后端”的 `tool.Tool`**，server 级治理由 MCP 域自身承担，tool 级治理复用 Tool Gateway。这正是 codex `McpHandler` 注册进 tool registry 的思路。

### 3.1 谁驱动连接：Manager 主导，RuntimeAdapter 是来源之一

需要澄清一个关键关系，避免误解 `RuntimeAdapter` 的角色：

- `capability.RuntimeAdapter` 的方法签名基于 `CapabilityIndexRecord`（marketplace 安装态能力）。**它只覆盖“通过 marketplace 安装的 MCP package”这一种来源**，不覆盖 `mcp.yaml` / 项目文件里直接声明的 server（后者没有 `CapabilityIndexRecord`）。
- 因此**连接生命周期的唯一驱动者是 `mcp.Manager`**，输入是 `Catalog` 合并后的 `[]McpServerDefinition`。
- MCP 的 `RuntimeAdapter` 实现只是 **Catalog 的一个 `DefinitionSource`**：当 marketplace 安装/启停一个 MCP package 时，它把该 package 投影成 `McpServerDefinition` 并触发 `Manager.Sync`（增量）。它**不自己持有连接逻辑**。

这样 config 来源与 marketplace 来源统一收敛到 `Catalog → Manager.Sync`，只有一条连接主线（对齐第一性原理“单一 Manager”）。

### 3.2 需要对现有 Tool Registry 做的最小扩展

现有 `tool.Registry`（`interface.go:141-154`）只有 `Register`，**没有 `Unregister`**。而 MCP server 断开、被禁用或 `listChanged` 移除工具时，必须能**动态撤下**对应的 `tool.Tool`，否则 LLM 仍会看到已失效的工具。落地时需二选一（推荐前者）：

1. **给 `tool.Registry` 增加 `Unregister(name string)`**（内存实现加锁删除），Gateway 透传。改动小、语义清晰，其他动态能力（未来 subagent-as-tool）也受益。
2. 或引入 **MCP 专用动态子注册表**，由 Gateway 在 `ListInfos/Get/Execute` 时叠加查询。改动更隔离但 Gateway 需感知多注册表。

本设计采用方案 1，作为 M2 的前置小改动，并保持并发安全（注册表读写加锁）。

---

## 4. 整体架构

### 4.1 分层与依赖方向

```text
LLM 可见层        tool.Info（含 mcp__server__tool）—— 与本地工具无差别
      ▲
Tool Gateway     可见性过滤 / 授权 / 锁 / Audit / Usage / Trace（现有，不改）
      ▲  注册 tool.Tool
┌─────────────── internal/capabilities/mcp（新增能力域）───────────────┐
│  tooladapter   MCP tool → tool.Tool 投影（双命名、schema 转换）        │
│  gateway       MCP 域协调/治理：server 级适用范围过滤、tools/list      │
│                聚合、暴露策略（direct/deferred）、订阅 Manager 状态并   │
│                Register/Unregister 到 Tool Registry（不做连接）        │
│  manager       McpConnectionManager：多 server 连接与会话生命周期、    │
│                健康检查、超时、重连、批量并发、listChanged 通知源       │
│  transport     Transport 抽象 + stdio(CommandTransport) / streamHTTP  │
│  catalog       多来源 server 定义合并（config + marketplace）+ 冲突   │
│  contract      端口：Manager / Session / Transport / Store / 定义     │
│  model         McpServerConfig / McpServerDefinition / ToolRef / 状态 │
│  adapter/      RuntimeAdapter(capability) + credential/approval 适配  │
└──────────────────────────────────────────────────────────────────────┘
      │ 依赖
      ▼
platform（config/logger/httpclient） + domain
      ▲ 注入 endpoint/credential/scope/审批
products/<product>/bootstrap（cli / desktop / enterprise）
```

依赖方向严格向下：`mcp` 域依赖 `tool/contract`、`approval/contract`、`credential/contract`、`profile/model`、`capability/contract`、`platform`；**不反向依赖 products / shared/local / runtime**。底层 JSON-RPC 只依赖官方 SDK，封装在 `transport` + `manager`，其余子包不直接 import SDK。

### 4.2 建议目录结构

```text
internal/capabilities/mcp/
  contract/
    manager.go        // Manager、Session、ServerState 端口
    transport.go      // Transport、TransportFactory 端口
    catalog.go        // DefinitionSource、Catalog 端口
    store.go          // 审批状态 / 连接状态持久化端口
  model/
    config.go         // McpServerConfig（transport/env/timeout/scope/approval）
    definition.go     // McpServerDefinition、McpToolRef、Origin/Scope
    state.go          // ServerStatus、ToolSnapshot、StartupEvent
  transport/
    stdio.go          // CommandTransport 封装（官方 SDK）
    streamhttp.go     // StreamableHTTP 封装（bearer/env headers）
    factory.go        // 按 config.Type 构造 Transport
  manager/
    manager.go        // McpConnectionManager（连接编排、健康检查、退避）
    session.go        // 单 server 会话：initialize/tools_list/call/close
    health.go         // ping / 重连 / 失败退避
  catalog/
    catalog.go        // 多来源合并 + 优先级 + 冲突消解
    config_source.go  // 从 platform/config 读取 mcp_servers
    marketplace_source.go // 从 capability 索引读取 MCP package
  gateway/
    gateway.go        // MCP 域治理：server 级 scope 过滤、暴露策略
    naming.go         // 双命名：qualified name / sanitize / 去重
  tooladapter/
    adapter.go        // MCP tool → tool.Tool（含 schema 转换、call 路由）
    schema.go         // MCP inputSchema → tool.ParameterSchema
  adapter/
    capability/adapter.go   // 实现 capability.RuntimeAdapter（type=mcp）
    approval/authorizer.go  // 实现 tool.gateway.Authorizer（server/tool 审批）
    credential/resolver.go  // 从 credential 域解析 token/headers
  resourcetool/
    list_mcp_resources/tool.go  // 专用网关工具（按需读 resource，不进 schema）
    read_mcp_resource/tool.go
```

> 每个子包单一职责、可独立演化，符合 `docs/agent loop设计.md` §14 的顶层骨架 + 域内生长原则。

---

## 5. 核心抽象与接口

以下为端口草图（最终以实现为准），体现「接口优先 + 依赖注入」。

### 5.1 配置模型（model/config.go）

> **分层说明**：本节是 **mcp 域的领域模型**（可引用 `profile/model`、`tool/contract` 等能力契约）。它与 `platform/config` 的 YAML DTO（§7.1，只用 primitive 类型）是两层：DTO → 领域模型的映射发生在 `mcp/catalog` 或产品 bootstrap，**`platform` 不得反向依赖 capabilities**。

```go
// McpTransportType MCP 传输类型。
type McpTransportType string

const (
    McpTransportStdio          McpTransportType = "stdio"
    McpTransportStreamableHTTP McpTransportType = "streamable_http"
)

// McpServerConfig 描述单个 MCP server 的运行时配置（产品无关）。
// 安全约束：禁止 inline bearer token，只允许 BearerTokenEnv / CredentialRef 引用。
type McpServerConfig struct {
    Name    string           // 唯一标识（catalog 内）
    Type    McpTransportType // 默认 stdio
    Enabled bool             // 默认 true
    Required bool            // 无人值守任务下连接失败是否 fail

    // stdio
    Command string
    Args    []string
    Env     map[string]string
    Cwd     string

    // streamable_http
    URL            string
    BearerTokenEnv string            // 从环境变量读取 token
    CredentialRef  string            // 或从 credential 能力域引用
    Headers        map[string]string // 静态 header（禁止放密钥明文）
    EnvHeaders     map[string]string // header 值从 env 读取

    // 生命周期
    StartupTimeout time.Duration // 默认 30s
    ToolTimeout    time.Duration // 默认 300s

    // 治理
    Scope          profilemodel.CapabilityScope // server 级适用范围
    EnabledTools   []string                     // tool 白名单（空=全部）
    DisabledTools  []string                     // tool 黑名单
    ApprovalMode   string                       // auto / prompt / approve（默认继承 policy）
    Exposure       tool.ToolExposure            // direct / deferred（多工具建议 deferred）
}
```

### 5.2 传输抽象（contract/transport.go）

```go
// Transport 是与单个 MCP server 通信的底层连接抽象。
// 实现封装官方 modelcontextprotocol/go-sdk，屏蔽 stdio / streamable-http 差异。
type Transport interface {
    Kind() model.McpTransportType
}

// TransportFactory 按配置构造 Transport（进程放置策略可在此扩展：本机 / sandbox）。
type TransportFactory interface {
    Build(ctx context.Context, cfg model.McpServerConfig) (Transport, error)
}
```

> 未来接 sandbox 内 stdio server 时，只需新增一个 `TransportFactory` 实现，Manager 不变（对应 codex 的 launcher trait 思路，也满足 AGENTS.md “Tool/MCP/Code 复用统一沙箱但各自保留入口”）。

### 5.3 会话与连接管理（contract/manager.go）

```go
// Session 是与单个已连接 MCP server 的会话。
type Session interface {
    Name() string
    ListTools(ctx context.Context) ([]model.ToolSnapshot, error)
    CallTool(ctx context.Context, tool string, args json.RawMessage) (model.ToolResult, error)
    // Resource/Prompt 按需，Phase 1 可只读 resource
    Close(ctx context.Context) error
}

// Manager 统一编排多个 MCP server 连接的生命周期。
type Manager interface {
    // Sync 按最新 catalog 定义批量连接/断开/重连（configKey 变更时重建）。
    Sync(ctx context.Context, defs []model.McpServerDefinition) ([]model.ServerState, error)
    SessionFor(server string) (Session, bool)
    States() []model.ServerState
    Close(ctx context.Context) error
}
```

Manager 关键行为（融合 codex + Kode 最佳实践）：
- 批量并发连接，带并发上限（默认 3，可配）与 `StartupTimeout`。
- 每 server 独立会话，失败不影响其他 server；`Required=true` 且失败时上报致命。
- 健康检查（周期 ping）→ 失败则 close + 退避重连（退避默认 30s）。
- 订阅 `listChanged` 通知 → 使对应 server 的 tool 快照失效并刷新。
- 优雅关闭：Run/进程结束时 `Close` 释放所有子进程与 HTTP 连接。

### 5.4 双命名（gateway/naming.go）

- **路由名**：`(serverName, originalToolName)` — 调用时透传回 MCP server。
- **模型可见名**：`mcp__{sanitize(server)}__{sanitize(tool)}` — 进入 `tool.Info.Name` 与 LLM schema。
- sanitize：非法字符替换为 `_`；超长（如 >64）追加数字后缀去重（先不引入 SHA1）。
- 与 Skill 边界无冲突：Skill 走 `Skill()` 元工具，MCP tool 用 `mcp__` 前缀命名空间，二者天然隔离。

### 5.5 Tool 投影（tooladapter/adapter.go）

```go
// mcpTool 把一个 MCP server tool 适配为 genesis-agent 的 tool.Tool。
// 注意：不缓存 Session（会因健康检查/重连而失效），只持有 Manager 与 (server, tool)，
// 执行时通过 Manager 解析当前存活 Session。
type mcpTool struct {
    manager    contract.Manager
    serverName string
    toolName   string     // 原始 MCP 名（路由用）
    info       *tool.Info // Name=mcp__server__tool，Traits.NeedsPermission=true
    timeout    time.Duration
}

func (t *mcpTool) GetInfo() *tool.Info { return t.info }

func (t *mcpTool) Execute(ctx context.Context, params string) (string, error) {
    // 1. session, ok := t.manager.SessionFor(t.serverName)；不存在则返回明确错误
    // 2. 解析 params → json.RawMessage
    // 3. 派生带 ToolTimeout 的 ctx，session.CallTool(ctx, t.toolName, args)
    // 4. 归一化 content（text/json/image ref）→ string
    // 5. isError → 返回 error（供 ReAct 记为 observation 错误）
}
```

**责任划分（保持各组件高内聚）**：
- `mcp.Manager`：只负责连接生命周期与会话（连接/断开/重连/ping/CallTool），**不碰 Tool Registry**。
- `mcp.gateway`（协调器角色）：订阅 Manager 的 server 状态变化与 `listChanged`，用 `tooladapter` 把存活且通过 server 级适用范围过滤的 tool 投影为 `mcpTool`，`Register` 进 Tool Registry；server 下线/禁用/工具移除时 `Unregister`。

执行时自然经过 Tool Gateway 的全部治理。这样 **MCP 无需自定义 LLM action，ReAct loop 零改动**，且连接与工具投影职责分离，便于测试与演化。

---

## 6. 统一与独立的平衡（三产品）

设计核心：**一套内核（`internal/capabilities/mcp`）+ 产品 bootstrap 注入差异**。三产品共享 Manager/Transport/Catalog/Gateway/ToolAdapter 全部代码，仅在装配层注入不同的「配置来源、凭证、默认 Scope、审批 Requester、默认策略」。

| 维度 | CLI | Desktop | Enterprise（HTTP API） |
|---|---|---|---|
| 配置来源 | 用户级 → `mcp.yaml` → `config.local.yaml` → `.genesis/mcp.yaml` → marketplace | 同 CLI（+ 桌面 UI 管理） | 平台配置 + 租户级 DB（Phase 2.5）+ marketplace |
| 默认 Scope（server 级） | 宽松：允许 stdio 本地 server（filesystem/browser 等） | 宽松（含桌面自动化类） | **收紧**：默认禁止本地文件/进程类 stdio server；数据库类需 approval |
| stdio server | 允许（本机进程） | 允许 | 默认禁止（无本机工作区）；如需则走 sandbox transport |
| streamable-http server | 允许 | 允许 | 允许（企业内部 MCP 服务为主） |
| 凭证来源 | credential 本地加密存储 + env | 同 CLI | 平台密钥服务 / 注入的 secret |
| 审批 | 终端/TUI Requester | 桌面弹窗 | headless：策略决定 auto/deny，人工审批走工单（过渡：auto-approve 白名单） |
| project 来源 server 预连接审批 | 需要（随仓库进入） | 需要 | 默认不加载 project 来源，或强制 approval |
| 工具暴露 | 少量 direct，多则 deferred | 同 CLI | 倾向 deferred + 检索，控制 API 响应体量 |

> 落地机制：`Catalog` 从产品注入的 `DefinitionSource` 列表读取；`gateway` 用注入的 `RuntimeCapabilityContext`（channel=cli/desktop/api）对 server 做适用范围过滤；`Authorizer` 由产品注入具体审批实现。**内核代码完全一致，行为差异全部数据/策略驱动。**

---

## 7. 配置设计

### 7.1 platform/config 扩展

在 `internal/platform/config/config.go` 的 `Config` 增加：

```go
type Config struct {
    // ... 现有字段
    MCP MCPConfig `mapstructure:"mcp"`
}

type MCPConfig struct {
    Enabled              bool                       `mapstructure:"enabled"`
    ConnectBatchSize     int                        `mapstructure:"connect_batch_size"`   // 默认 3
    DefaultStartupTimeout time.Duration             `mapstructure:"default_startup_timeout"` // 默认 30s
    DefaultToolTimeout   time.Duration              `mapstructure:"default_tool_timeout"`    // 默认 300s
    Servers              map[string]MCPServerConfig `mapstructure:"servers"`
}
```

`MCPServerConfig` 是 **YAML DTO**（`platform/config` 层，字段全部为 primitive/`map[string]string`/`[]string`，**不引用任何 capabilities 类型**，如 scope 用 `channels []string`、`tenant_ids []string` 等原始字段）。加载后由 `mcp/catalog`（或产品 bootstrap）映射为 §5.1 的领域 `McpServerConfig`（含 `profilemodel.CapabilityScope`、`tool.ToolExposure`）。校验规则：
- `type` ∈ {stdio, streamable_http}；stdio 必须有 `command`，http 必须有 `url`。
- **禁止 inline token**：只接受 `bearer_token_env` / `credential_ref`；校验时若发现疑似明文 token 报错。
- 超时缺省用 `DefaultStartupTimeout` / `DefaultToolTimeout` 填充。
- server 名以 map key 为准（DTO 内不再重复 name 字段）。

### 7.2 多来源合并（catalog）

优先级（低→高，后者覆盖同名）：

```text
1. 内置/平台默认 server
2. 用户级 ~/.genesis-agent/<product>/config.yaml
3. configs/mcp.yaml 中的项目共享 mcp.servers
4. configs/config.local.yaml 中的项目本地覆盖
5. 项目级 .genesis/mcp.yaml（project 来源，需预连接审批）
6. marketplace 已安装 MCP package（capability 索引）
7. 运行时/会话级覆盖（CLI flag / API 请求）
```

冲突消解：记录被覆盖来源到审计（借鉴 codex catalog）；project 来源标记 `Origin=project` 触发审批。

这里的 `.genesis/mcp.yaml` 与 marketplace 属于 MCP Catalog 的独立 server 来源，不属于通用配置文件层；通用 MCP 字段内部仍严格遵循“用户级 < `mcp.yaml` < `config.local.yaml`”。

### 7.3 配置示例

```yaml
mcp:
  enabled: true
  connect_batch_size: 3
  servers:
    filesystem:
      type: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "${WORKSPACE}"]
      exposure: deferred
      scope:
        channels: [cli, desktop]   # enterprise 默认不可见
    company-db:
      type: streamable_http
      url: https://mcp.internal.example.com/db
      bearer_token_env: COMPANY_DB_MCP_TOKEN
      approval_mode: approve
      disabled_tools: ["drop_table"]
```

stdio server 的 `env` 是“传给 MCP 子进程的环境变量表”。其中的 `${ENV_NAME}` 在 Genesis 配置加载时从当前进程环境展开；例如 `MYSQL_PASS: ${MYSQL_PASS}` 会将宿主进程的 `MYSQL_PASS` 传给 MySQL MCP Server。它不自动加载 `.env`，可由 PowerShell/CMD 启动命令、`start.local.bat`、服务管理器、容器或 CI 的 Secret 注入提供。密钥不得写成 `env` 中的明文；详细注入方式见 `docs/密钥与连接管理设计.md` 的 §4.6.2。

---

## 8. 工具暴露与命名策略

- **默认 direct**：server tool 数量少（如 ≤ 阈值）时直接进 LLM schema。
- **deferred + 检索**：server tool 多时标记 `ToolExposureDeferred`，配合一个检索型元工具（后续可复用/扩展现有 skill 检索思路）按关键词把候选 MCP tool 提升为可调用，避免 context 爆炸（借鉴 Kode `MCPSearchTool` / codex deferred exposure）。
- **resource / prompt 不进 schema**：通过 `list_mcp_resources` / `read_mcp_resource` 网关工具按需访问。
- 命名统一 `mcp__server__tool`，`ToolTraits`：`NeedsPermission=true`，`ReadOnly` 依 server/tool 声明（保守默认 false），`Exposure` 由 server 配置决定。

---

## 9. 治理集成

### 9.1 适用范围过滤（两段式，对齐 §6.8.3）

1. **Run 启动 / Catalog Sync**：按 `RuntimeCapabilityContext`（channel/tenant/project/agent/env）过滤可见 server；只连接并投影可见 server 的 tool。
2. **执行前兜底**：Tool Gateway `isAllowed` + MCP `Authorizer` 再次校验 server/tool 级 scope 与权限，不依赖“LLM 没看到就不会调”。

### 9.2 审批（复用 approval / policy）

- MCP 域提供 `Authorizer`（实现 `tool.gateway.Authorizer`），在 Gateway 执行前调用 approval：动作 `ActionMCPCall`，资源 `mcp__server__tool`。
- 支持 server 级与 tool 级 `approval_mode`：auto / prompt / approve；policy 规则支持通配 `mcp__server__*`（借鉴 Kode）。
- **project 来源 server 预连接审批**：连接前需批准，状态持久化到 `store`（approved/rejected），未批准不连接。

### 9.3 审计 / Usage / Trace

- Tool Gateway 已在 start/finish 打 Audit、Usage、Trace（`gateway.go` 现有逻辑），MCP tool 自动获得。
- MCP 域额外上报 **server 生命周期事件**（connect/disconnect/health/startup-fail）到 Trace + Audit（借鉴 codex startup 可观测性）。

### 9.4 凭证（credential / connection）

- streamable-http token 通过 `bearer_token_env` 或 `credential_ref`（→ `credential/service`）解析，运行时注入 header，绝不落配置明文。
- endpoint 属于连接信息，可走 `connection/service` 管理。

---

## 10. 生命周期与并发

```text
Run/进程启动
  → Catalog 合并定义 → 适用范围过滤（server 级）→ Manager.Sync(defs)
      → 批量并发连接（≤ batch，各带 StartupTimeout）
      → 每 server：initialize → tools/list → 缓存 ToolSnapshot
      → 失败：非 Required 记 warning 继续；Required 上报致命
  → gateway（协调器）订阅 Manager 状态：把存活 server 的 tool 投影为
     mcpTool 并 Register 进 Tool Registry；server 下线时 Unregister
  → 健康检查协程（Manager 内）：周期 ping；失败 close+退避重连并通知 gateway
  → listChanged（Manager 转发）：gateway 刷新对应 server tool 快照并增量
     Register/Unregister

工具调用（ReAct）
  → registry.Execute(mcp__server__tool) → Tool Gateway
      → isAllowed（scope）→ Authorizer（approval）→ 锁 → mcpTool.Execute
      → session.CallTool(原始名, args)（带 ToolTimeout, ctx 取消）
      → 归一化结果 / isError → observation
  → Audit / Usage / Trace 自动记录

关闭
  → Manager.Close：关闭所有 session（stdio 子进程 kill，HTTP 连接释放）
```

并发与安全：Manager 内部 `map[string]session` 加锁保护；每次 CallTool 用派生 ctx 控制超时与取消；子进程用 process group 便于整体清理（借鉴 codex `process_group_cleanup`）。

---

## 11. 分阶段落地路线

| 阶段 | 范围 | 交付 |
|---|---|---|
| **M1 内核 Client** | contract/model/transport(stdio+http)/manager/session；官方 SDK 接入 | 能连接单/多 stdio server 并 `tools/list` |
| **M2 Tool 投影 + Gateway 集成** | tooladapter + 双命名 + capability RuntimeAdapter；注册进 Tool Registry | MCP tool 经 Tool Gateway 被 ReAct 调用，Audit/Usage/Trace 生效 |
| **M3 配置 + Catalog + 适用范围** | platform/config 扩展、catalog 多来源、scope 两段过滤 | 三产品差异化装配 |
| **M4 审批 + 凭证 + 安全** | Authorizer 适配 approval、credential 解析、project 预连接审批、禁明文 token | 生产级治理 |
| **M5 健康检查 + 暴露策略 + resource** | ping/退避/listChanged、deferred+检索、resource 网关工具 | 稳定性与 context 控制 |
| **M6（可选）Server 角色** | 把 genesis-agent 暴露为 MCP server（`go-sdk` server） | 供外部宿主调用 |

---

## 12. 相关源码文件索引（开发时对照用）

### 12.1 genesis-agent 现有可复用/需扩展文件

**能力索引与运行时适配（核心扩展点）**
- `internal/capabilities/capability/contract/contract.go`（RuntimeAdapter / RuntimeAdapterRegistry）
- `internal/capabilities/capability/model/model.go`（CapabilityTypeMCP、CapabilityIndexRecord）
- `internal/capabilities/capability/service/registry.go`（Registry / Matches 过滤）

**Tool 域（投影目标 + 治理网关）**
- `internal/capabilities/tool/contract/interface.go`（Tool / Registry / Info / ToolTraits / ToolExposure）
- `internal/capabilities/tool/adapter/registry/registry.go`（内存注册表）
- `internal/capabilities/tool/adapter/capability/adapter.go`（Capability→Tool 投影范例）
- `internal/capabilities/tool/gateway/gateway.go`（可见性/授权/锁/审计/Usage/Trace，含 Authorizer 接口）
- `internal/capabilities/tool/scheduler/locks.go`、`queue.go`

**治理复用**
- `internal/capabilities/approval/contract/interface.go`、`approval/model/model.go`（ActionMCPCall）、`approval/service/service.go`
- `internal/capabilities/policy/contract/evaluator.go`、`policy/service/evaluator.go`、`policy/adapter/approval/engine.go`、`policy/adapter/config/builder.go`
- `internal/capabilities/audit/contract/sink.go`、`audit/model/event.go`、`audit/adapter/file/sink.go`
- `internal/capabilities/usage/contract/sink.go`、`usage/model/event.go`
- `internal/capabilities/trace/contract/tracer.go`、`trace/adapter/tracer.go`
- `internal/capabilities/credential/contract/types.go`、`credential/service/service.go`
- `internal/capabilities/connection/contract/types.go`、`connection/service/service.go`

**适用范围 / Profile**
- `internal/capabilities/profile/model/profile.go`（CapabilityScope / ToolSet）
- `products/cli/internal/profile/default_profile.go`、`products/enterprise/internal/profile/default_profile.go`

**LLM schema 转换（了解 tool.Info 如何到 LLM）**
- `internal/capabilities/llm/contract/interface.go`
- `internal/capabilities/llm/adapter/eino/tool_convert.go`、`eino/adapter.go`

**运行时（确认无需改动）**
- `internal/runtime/strategy/react/react_loop.go`（getToolInfos / registry.Execute）
- `internal/runtime/action/action.go`

**配置**
- `internal/platform/config/config.go`（新增 MCPConfig）
- `internal/platform/httpclient/client.go`（streamable-http 可复用的 client 基础设施）

**装配点**
- `internal/bootstrap/builder.go`
- `products/cli/bootstrap/container.go`
- `products/enterprise/bootstrap/container.go`、`products/enterprise/bootstrap/execute.go`
- `products/desktop/bootstrap/execute.go`
- `cmd/genesis-cli/main.go`、`cmd/genesis-enterprise/main.go`、`cmd/genesis-desktop/main.go`

**marketplace（MCP package 来源）**
- `internal/capabilities/package/marketplace/model/model.go`（PackageTypeMCPPackage）
- `internal/capabilities/package/marketplace/service/service.go`、`contract/contract.go`
- `shared/local/skillmarket/manifest.go`、`shared/local/skillmarket/store.go`
- `products/cli/internal/command/capability_cmd.go`（`--type mcp` 过滤）

**现有设计文档**
- `docs/agent loop设计.md`（§6.8 适用范围、§14.1.3 Tool/MCP/Code 沙箱边界、MCPServerDefinition/MCPGateway 占位）
- `docs/统一配置权限与审批治理设计.md`、`docs/密钥与连接管理设计.md`、`docs/项目目录与边界说明.md`

### 12.2 Kode-CLI 参考文件（TypeScript，`D:\workspace\go\go-project\Kode-CLI`）

> **阅读顺序**：先读 Core 18 个文件（§12.2.1），再按需读配置/工具/产品层。文档 `docs/develop/modules/mcp-integration.md` 含过时伪代码，**以 `packages/core/src/mcp/` 源码为准**。

#### 12.2.1 MCP Client 核心（必读，18 文件）

```
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\index.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\README.md
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\server.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\index.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\manager.ts          ← 连接生命周期、ping、退避
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\connection.ts       ← transport 候选与 fallback
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\clients.ts          ← server 过滤、批量连接
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\config.ts         ← 多来源配置合并优先级
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\tools.ts            ← tool 投影、mcp__server__tool 命名
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\commands.ts       ← prompt→slash command
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\listChanged.ts    ← 缓存失效版本号
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\oauth.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\request.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\reset.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\settings.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\timeouts.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\types.ts        ← WrappedClient 三态
D:\workspace\go\go-project\Kode-CLI\packages\core\src\mcp\client\utils.ts
```

#### 12.2.2 配置与 Schema

```
D:\workspace\go\go-project\Kode-CLI\packages\config\src\schema.ts                    ← McpStdio/SSE/Http/Ws union
D:\workspace\go\go-project\Kode-CLI\packages\config\src\mcp.ts                       ← 项目 .mcp.json 解析
D:\workspace\go\go-project\Kode-CLI\packages\config\src\loader.ts
D:\workspace\go\go-project\Kode-CLI\packages\config\src\index.ts
D:\workspace\go\go-project\Kode-CLI\packages\config\src\compat\legacyClaudeJson.ts
D:\workspace\go\go-project\Kode-CLI\packages\config\src\compat\legacyEnv.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\utils\config.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\services\mcpCliUtils.ts
```

#### 12.2.3 Tool 注册 / Schema / 内置 MCP 元工具

```
D:\workspace\go\go-project\Kode-CLI\packages\core\src\tooling\mcpToolSchema.ts
D:\workspace\go\go-project\Kode-CLI\packages\tools\src\registry.ts
D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\mcp\MCPTool\MCPTool.tsx
D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\mcp\MCPTool\prompt.ts
D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\mcp\MCPSearchTool\MCPSearchTool.tsx   ← deferred 检索
D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\mcp\MCPSearchTool\prompt.ts
D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\mcp\ListMcpResourcesTool\ListMcpResourcesTool.tsx
D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\mcp\ListMcpResourcesTool\prompt.ts
D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\mcp\ReadMcpResourceTool\ReadMcpResourceTool.tsx
D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\mcp\ReadMcpResourceTool\prompt.ts
```

#### 12.2.4 权限与策略

```
D:\workspace\go\go-project\Kode-CLI\packages\core\src\permissions\policies\defaultTool.ts   ← mcp__server__* 通配
D:\workspace\go\go-project\Kode-CLI\packages\core\src\permissions\ruleString.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\permissions\fileToolPermissionEngine\paths.ts
```

#### 12.2.5 CLI 命令 / 审批 UI / 管理界面

```
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\mcpCli.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\mcpServerApproval.tsx        ← project 预连接审批
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\setupScreens.tsx
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\commands\mcp\index.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\commands\mcp\serve.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\commands\mcp\reset.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\commands\mcp\importClaudeDesktop.tsx
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\commands\mcp\servers\add.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\commands\mcp\servers\addJson.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\commands\mcp\servers\get.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\commands\mcp\servers\list.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\entrypoints\cli\commands\mcp\servers\remove.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\commands\mcp\mcp.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\ui\components\MCPServerApprovalDialog.tsx
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\ui\components\MCPServerMultiselectDialog.tsx
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\ui\screens\overlays\McpServersScreen.tsx
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\ui\screens\REPL\useReplInit.ts
D:\workspace\go\go-project\Kode-CLI\apps\cli\src\ui\screens\REPL\useReplController.tsx
```

#### 12.2.6 ACP Server（Desktop/Enterprise API 参考：会话级 MCP 注入）

```
D:\workspace\go\go-project\Kode-CLI\apps\server\src\acp\agent\mcp.ts               ← connectAcpMcpServers + merge
D:\workspace\go\go-project\Kode-CLI\apps\server\src\acp\agent\handlers\sessions.ts
D:\workspace\go\go-project\Kode-CLI\apps\server\src\acp\agent\handlers\initialize.ts
D:\workspace\go\go-project\Kode-CLI\apps\server\src\acp\agent\sessionStore.ts
D:\workspace\go\go-project\Kode-CLI\apps\server\src\acp\sessionManager.ts
D:\workspace\go\go-project\Kode-CLI\apps\server\src\handlers\chat.handler.ts
```

#### 12.2.7 kode-agent-sdk（简化版 Client，快速验证用）

```
D:\workspace\go\go-project\Kode-CLI\kode-agent-sdk\src\tools\mcp.ts
D:\workspace\go\go-project\Kode-CLI\kode-agent-sdk\src\tools\registry.ts
D:\workspace\go\go-project\Kode-CLI\kode-agent-sdk\src\tools\index.ts
```

#### 12.2.8 测试（实现时强烈建议对照）

```
D:\workspace\go\go-project\Kode-CLI\packages\core\src\test\unit\mcp-manager-lifecycle.test.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\test\unit\mcp-connection-internals.test.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\test\unit\mcp-list-changed.test.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\test\unit\mcp-content-normalization.test.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\test\unit\mcp-resources-tools-parity.test.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\test\unit\plugin-mcp-integration.test.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\test\unit\permission-rules-mcp.test.ts
D:\workspace\go\go-project\Kode-CLI\packages\core\src\test\integration\mcp-list-tools.test.ts
```

#### 12.2.9 文档与 Skill

```
D:\workspace\go\go-project\Kode-CLI\docs\mcp.md
D:\workspace\go\go-project\Kode-CLI\docs\develop\modules\mcp-integration.md
D:\workspace\go\go-project\Kode-CLI\docs\develop-zh\modules\mcp-integration.md
D:\workspace\go\go-project\Kode-CLI\packages\builtin-skills\skills\mcp-builder\SKILL.md
D:\workspace\go\go-project\Kode-CLI\packages\builtin-skills\skills\mcp-builder\reference\mcp_best_practices.md
D:\workspace\go\go-project\Kode-CLI\scripts\mcp-cli-wrapper.cjs
```

### 12.3 codex 参考文件（Rust，`D:\workspace\go\go-project\codex\codex-rs`）

> **阅读顺序**：`config/mcp_types.rs` → `codex-mcp/connection_manager.rs` + `catalog.rs` → `rmcp-client/rmcp_client.rs` → `core/mcp.rs` + `mcp_tool_call.rs` → `app-server/mcp_processor.rs`。全仓库 MCP 相关约 350+ 文件；以下为开发必读分层索引。

#### 12.3.1 Tier A — 核心实现（必读）

**Workspace**
```
D:\workspace\go\go-project\codex\codex-rs\Cargo.toml
```

**`codex-mcp` — 连接管理 / 目录 / 认证**
```
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\lib.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\connection_manager.rs      ← 单一 Manager 范本
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\connection_manager_tests.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\catalog.rs                 ← 多来源合并 + 冲突消解
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\catalog_tests.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\tools.rs                   ← 双命名 ToolInfo
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\mcp\mod.rs               ← sanitize、命名去重
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\mcp\auth.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\runtime.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\rmcp_client.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\resource_client.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\elicitation.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\auth_elicitation.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\plugin_config.rs
D:\workspace\go\go-project\codex\codex-rs\codex-mcp\src\server.rs
```

**`rmcp-client` — 传输 / OAuth / 底层客户端**
```
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\lib.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\rmcp_client.rs           ← 单 client、超时 30s/300s
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\stdio_server_launcher.rs ← Transport 抽象
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\http_client_adapter.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\http_client_adapter\www_authenticate.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\streamable_http_retry.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\oauth.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\perform_oauth_login.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\auth_status.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\elicitation_client_service.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\in_process_transport.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\executor_process_transport.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\startup_error.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\src\utils.rs
```

**`config` — MCP 配置类型 / 编辑 / 企业约束**
```
D:\workspace\go\go-project\codex\codex-rs\config\src\mcp_types.rs                ← 重点借鉴
D:\workspace\go\go-project\codex\codex-rs\config\src\mcp_types_tests.rs
D:\workspace\go\go-project\codex\codex-rs\config\src\mcp_edit.rs
D:\workspace\go\go-project\codex\codex-rs\config\src\mcp_requirements.rs         ← 企业管控
D:\workspace\go\go-project\codex\codex-rs\config\src\mcp_requirements_tests.rs
D:\workspace\go\go-project\codex\codex-rs\core\config.schema.json
```

**`protocol` — 配置与 wire 类型分离**
```
D:\workspace\go\go-project\codex\codex-rs\protocol\src\mcp.rs
D:\workspace\go\go-project\codex\codex-rs\protocol\src\mcp_approval_meta.rs
```

**`core` — 运行时集成**
```
D:\workspace\go\go-project\codex\codex-rs\core\src\mcp.rs
D:\workspace\go\go-project\codex\codex-rs\core\src\mcp_tool_call.rs               ← 审批 + 遥测
D:\workspace\go\go-project\codex\codex-rs\core\src\mcp_tool_call\telemetry.rs
D:\workspace\go\go-project\codex\codex-rs\core\src\mcp_tool_exposure.rs          ← direct/deferred
D:\workspace\go\go-project\codex\codex-rs\core\src\mcp_tool_approval_templates.rs
D:\workspace\go\go-project\codex\codex-rs\core\src\mcp_skill_dependencies.rs
D:\workspace\go\go-project\codex\codex-rs\core\src\session\mcp.rs
D:\workspace\go\go-project\codex\codex-rs\core\src\session\mcp_runtime.rs        ← step 级快照
D:\workspace\go\go-project\codex\codex-rs\core\src\tools\handlers\mcp.rs          ← McpHandler 投影范本
D:\workspace\go\go-project\codex\codex-rs\core\src\tools\handlers\mcp_resource.rs
D:\workspace\go\go-project\codex\codex-rs\core\src\tools\handlers\mcp_resource\list_mcp_resources.rs
D:\workspace\go\go-project\codex\codex-rs\core\src\tools\handlers\mcp_resource\read_mcp_resource.rs
D:\workspace\go\go-project\codex\codex-rs\core\src\tools\handlers\tool_search.rs
```

**`tools` — MCP tool → 模型 function schema**
```
D:\workspace\go\go-project\codex\codex-rs\tools\src\mcp_tool.rs
D:\workspace\go\go-project\codex\codex-rs\tools\src\mcp_tool_tests.rs
```

**`app-server` — HTTP 管理 API（Enterprise 参考）**
```
D:\workspace\go\go-project\codex\codex-rs\app-server\src\mcp_refresh.rs
D:\workspace\go\go-project\codex\codex-rs\app-server\src\request_processors\mcp_processor.rs
D:\workspace\go\go-project\codex\codex-rs\app-server\README.md
D:\workspace\go\go-project\codex\codex-rs\app-server-protocol\src\protocol\v2\mcp.rs
```

**Extension / Plugin 贡献点（产品分发关键）**
```
D:\workspace\go\go-project\codex\codex-rs\ext\extension-api\src\contributors\mcp.rs
D:\workspace\go\go-project\codex\codex-rs\ext\extension-api\src\contributors.rs
D:\workspace\go\go-project\codex\codex-rs\utils\plugins\src\mcp_connector.rs
D:\workspace\go\go-project\codex\codex-rs\plugin\src\manifest.rs
```

**CLI**
```
D:\workspace\go\go-project\codex\codex-rs\cli\src\mcp_cmd.rs
```

**MCP Server 角色（M6 可选）**
```
D:\workspace\go\go-project\codex\codex-rs\mcp-server\src\message_processor.rs
D:\workspace\go\go-project\codex\codex-rs\mcp-server\src\codex_tool_config.rs
D:\workspace\go\go-project\codex\codex-rs\mcp-server\src\codex_tool_runner.rs
D:\workspace\go\go-project\codex\codex-rs\docs\codex_mcp_interface.md
```

#### 12.3.2 Tier B — 测试（实现时对照）

```
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\tests\streamable_http_recovery.rs
D:\workspace\go\go-project\codex\codex-rs\rmcp-client\tests\process_group_cleanup.rs
D:\workspace\go\go-project\codex\codex-rs\core\tests\suite\mcp_tool_exposure.rs
D:\workspace\go\go-project\codex\codex-rs\core\tests\suite\mcp_refresh_cleanup.rs
D:\workspace\go\go-project\codex\codex-rs\app-server\tests\suite\v2\mcp_tool.rs
D:\workspace\go\go-project\codex\codex-rs\app-server\tests\suite\v2\mcp_server_status.rs
D:\workspace\go\go-project\codex\codex-rs\app-server\tests\suite\v2\mcp_server_elicitation.rs
D:\workspace\go\go-project\codex\codex-rs\cli\tests\mcp_list.rs
D:\workspace\go\go-project\codex\codex-rs\cli\tests\mcp_add_remove.rs
```

#### 12.3.3 Tier C — TUI / 可观测性 / Feature flags

```
D:\workspace\go\go-project\codex\codex-rs\tui\src\chatwidget\mcp_startup.rs
D:\workspace\go\go-project\codex\codex-rs\tui\src\bottom_pane\mcp_server_elicitation.rs
D:\workspace\go\go-project\codex\codex-rs\tui\src\history_cell\mcp.rs
D:\workspace\go\go-project\codex\codex-rs\rollout-trace\src\mcp.rs
D:\workspace\go\go-project\codex\codex-rs\features\src\feature_configs.rs
D:\workspace\go\go-project\codex\codex-rs\hooks\src\events\pre_tool_use.rs
D:\workspace\go\go-project\codex\codex-rs\core\src\guardian\approval_request.rs
```

### 12.4 官方依赖

- `github.com/modelcontextprotocol/go-sdk/mcp`（当前固定 `v1.6.1`，即当前可用最新稳定版；`v1.7.0` 仅有预发布标签，不作为生产依赖；`mcp.NewClient` / `mcp.CommandTransport`(stdio) / `mcp.StreamableClientTransport`(HTTP) / `mcp.Client.Connect` / `session.CallTool`）。后续规范升级时应确认兼容性后再升级至新的稳定版。
  - 文档：https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
  - 设计：https://github.com/modelcontextprotocol/go-sdk/blob/main/design/design.md

---

## 13. 关键设计决策与理由

1. **MCP tool 走 Tool Gateway（flatten），不新增 LLM action**：满足 `AGENTS.md`“schema 只出现 Tool 名”，零改动 ReAct loop，自动获得全部治理。server 级治理由 MCP `gateway` 承担，二者分工清晰（对应 codex `McpHandler`）。
   - **与现有 `docs/agent loop设计.md` 的关系（刻意细化，非冲突）**：原文档在 `ActionType` 中列了 `mcp_call` 并画了 `mcp_call → MCP Gateway` 的 ActionExecutor 路由。本设计对其**细化澄清**：LLM 层面不暴露 `mcp_call` 这类原语（否则违反 schema 只出现 Tool 名），MCP tool 一律以 `tool_call` 形态被调用；`MCPGateway` 不是 LLM action 的执行器，而是**本域 server 级治理组件**（连接生命周期、tools/list 聚合、server 级可见性、暴露策略）。`ActionMCPCall = "mcp.call"` 保留，仅作为**审批/审计的动作类型标识**，不是 LLM 可见 action。落地时应同步把 `agent loop设计.md` 的相关描述更新为该口径。
2. **单一 ConnectionManager**：规避 Kode 的双/三连接路径维护负担。
3. **官方 go-sdk**：协议随规范演进由 SDK 维护，本域聚焦编排与治理。
4. **双命名**：隔离 LLM function name 限制与 MCP 原始 tool 名。
5. **Transport 抽象**：为未来 sandbox 内 stdio server 预留扩展点，符合 §14.1.3 沙箱边界。
6. **一套内核 + 产品注入差异**：三产品统一代码，行为由注入的 Source/Scope/Authorizer/Credential 数据驱动。
7. **安全默认**：禁明文 token、project 来源预连接审批、enterprise 默认收紧本地类 server。

---

## 14. 残留风险与待决策

- **deferred 检索工具的具体形态**（复用 skill 检索还是新增 `mcp_search`）待 M5 结合届时工具规模决定。
- **enterprise 多租户 server 配置持久化**（DB）依赖 Phase 2.5 的租户/RBAC 落地，M3 先用配置 + 内存态，模型预留 `TenantIDs`。
- **elicitation（server→client 反向请求）**：Phase 1 可先 auto-deny 或忽略，M5/M6 再接（codex 已有完整范本）。
- **go-sdk 版本与 spec 演进**：需在引入时确认当前最新稳定版与所需 spec 版本，并在 CI 固定。
- **OAuth 型远程 server**：Phase 1 仅支持 bearer/env token；OAuth 流程（codex `oauth.rs`）延后。

---

## 15. 三产品装配契约（统一内核 + 独立注入）

设计核心：**`internal/capabilities/mcp` 是唯一实现**，三产品只在 `products/<product>/bootstrap` 注入差异。禁止在 `products/` 下复制 MCP client/manager 逻辑。

### 15.1 装配依赖图

```text
products/<product>/bootstrap
  ├─ DefinitionSource[]     ← 配置来源（config / project / marketplace / session）
  ├─ TransportFactory       ← 进程放置策略（本机 stdio vs sandbox stdio）
  ├─ Authorizer             ← 审批 Requester（TUI / 弹窗 / headless）
  ├─ CredentialResolver     ← 凭证解析（本地加密 / 平台密钥服务）
  ├─ ApprovalStore          ← project server 预连接审批持久化
  ├─ DefaultScopePolicy     ← server 级默认 channel/env 过滤
  └─ McpManagementAPI?      ← 仅 enterprise/desktop 暴露管理 RPC

internal/bootstrap/builder.go
  └─ 组装 Manager + Catalog + Gateway + ToolAdapter → 注入 AgentService
```

### 15.2 各产品注入差异（数据驱动，非代码分叉）

| 注入项 | CLI | Desktop | Enterprise |
|---|---|---|---|
| `DefinitionSource` | config + user + project `.genesis/mcp` + marketplace + CLI flag | 同 CLI + 会话级覆盖（Wails 传入） | 平台 config + 租户 DB（Phase 2.5）+ marketplace |
| `TransportFactory` | `LocalStdioFactory`（宿主机子进程） | 同 CLI | `SandboxStdioFactory`（genesis-sandbox 内启动）或仅 `StreamableHTTP` |
| `DefaultScopePolicy` | `channels: [cli]`，允许本地 stdio | `channels: [desktop]`，允许本地 stdio + 桌面集成类 | `channels: [enterprise, api]`，**默认禁止**本地 stdio |
| `Authorizer` | `products/cli/internal/approval` TUI | Wails 弹窗（待实现） | headless policy + 工单（过渡：白名单 auto） |
| `CredentialResolver` | `credential/service` 本地加密 | 同 CLI | 平台密钥服务 + `connection/service` |
| `ApprovalStore` | `~/.genesis-agent/cli/mcp-approvals.json` | 同路径或桌面偏好存储 | DB 表（Phase 2.5） |
| `McpManagementAPI` | CLI `genesis mcp *` 子命令 | Wails MCP 管理面板 | HTTP `/v1/mcp/*`（见 §16） |
| `RequiredServerBehavior` | warning + 继续 | 同 CLI | fail-fast + audit（对齐 `SandboxRequire` 语义） |

### 15.3 借鉴 Codex Extension Contributor 模式

Codex 用 `McpServerContributor` trait 让产品层在不改 core 的情况下注入/覆盖 MCP server（`ext/extension-api/contributors/mcp.rs`）。genesis-agent 对应：

```go
// internal/capabilities/mcp/contract/catalog.go
type DefinitionSource interface {
    // List 返回本来源的 server 定义；Origin 标记来源（builtin/config/project/marketplace/session）
    List(ctx context.Context, env RuntimeCatalogEnv) ([]model.McpServerDefinition, error)
    Precedence() int // 数字越大优先级越高
}
```

产品 bootstrap 按序注入 Source，Catalog 合并后交给 Manager。**Desktop/Enterprise 的会话级 server**（对标 Kode ACP `connectAcpMcpServers`）作为最后一个高优先级 Source，仅在当前 Run/Session 生效。

### 15.4 借鉴 Kode WrappedClient 三态模型

Kode 将连接结果分为 `connected | failed | needs-auth`（`client/types.ts`），UI/Runtime 可区分「连不上」与「需 OAuth」。genesis-agent 的 `model.ServerState` 应包含：

```go
type ServerStatus string
const (
    ServerStatusStarting   ServerStatus = "starting"
    ServerStatusReady      ServerStatus = "ready"
    ServerStatusFailed     ServerStatus = "failed"
    ServerStatusNeedsAuth  ServerStatus = "needs_auth"  // Phase 2 OAuth
    ServerStatusDisabled   ServerStatus = "disabled"
)
```

CLI/Desktop 在 TUI/GUI 展示启动状态；Enterprise 通过 SSE/轮询 API 推送（对标 codex `McpServerStatusUpdatedNotification`）。

### 15.5 Enterprise MCP Requirements（企业管控层）

借鉴 codex `config/mcp_requirements.rs`：在 Catalog 合并**之后**、Manager.Sync **之前**，由 enterprise bootstrap 注入 `RequirementsFilter`：

- 按 `command`/`url` 的 exact/prefix/regex 匹配，强制 disable 不合规 server
- 输出 `disabled_reason` 供管理员审计
- 与 `profile.CapabilityScope` 的 tenant/project/agent 过滤串联，形成两段式企业管控

---

## 16. Enterprise MCP 管理 API 设计（参考 codex app-server）

Enterprise 不直接暴露 MCP JSON-RPC，而是通过 **HTTP 管理 API + Run 内 tool 投影** 两层接入（对标 codex `app-server` 的 `mcp_processor.rs`）。

### 16.1 管理 API（Phase 2.5 完整，M3 可先只读）

| 端点 | 方法 | 职责 | codex 参考 |
|---|---|---|---|
| `/v1/mcp/servers` | GET | 列出 catalog 合并后的 server 状态 | `ListMcpServerStatus` |
| `/v1/mcp/servers/{name}` | GET | 单 server 详情（tools/resources/auth） | `mcp_processor` |
| `/v1/mcp/servers/{name}/refresh` | POST | 触发重连/刷新 tools | `McpServerRefreshResponse` |
| `/v1/mcp/servers/{name}/tools/{tool}/call` | POST | 调试/直连调用（管理员） | `McpServerToolCall` |
| `/v1/mcp/resources/read` | POST | 按需读 resource | `McpResourceRead` |

实现位置建议：`products/enterprise/internal/interfaces/http/handler/mcp.go`，依赖注入 `mcp.Manager`（只读/管理操作），**不绕过 Tool Gateway 的 Run 内调用路径**。

### 16.2 Run 内调用路径（与 CLI/Desktop 一致）

```
POST /v1/runs → ReAct loop → registry.Execute(mcp__server__tool)
  → Tool Gateway（scope + approval + audit）
  → mcpTool.Execute → session.CallTool
```

`GET /v1/tools` 应包含已通过 scope 过滤且 exposure=direct 的 MCP tool（deferred 的不进列表，需检索工具提升）。

### 16.3 SSE 事件扩展

在现有 Run SSE 流中增加 MCP 生命周期事件（借鉴 codex startup notification）：

- `mcp.server.starting` / `mcp.server.ready` / `mcp.server.failed`
- `mcp.tools.changed`（listChanged 触发）
- `mcp.tool.call.progress`（长耗时 MCP 调用，Phase 2）

---

## 17. 当前实现状态（对照表）

| 层级 | 状态 | 说明 |
|---|---|---|
| `internal/capabilities/mcp/` | **已落地** | contract/model/transport/manager/catalog/gateway/tooladapter/stack |
| go-sdk 依赖 | **已引入** | `github.com/modelcontextprotocol/go-sdk` |
| `tool.Registry.Unregister` | **已落地** | Tool Gateway 支持动态撤下 |
| `platform/config` MCPConfig | **已落地** | YAML DTO + 明文 token 禁止 |
| MCP RuntimeAdapter | **已注册** | 与 marketplace `AdapterRegistry` 共享，Install/SetEnabled 可热同步 |
| Project DefinitionSource | **已落地** | `.genesis/mcp(.yaml/.yml/.json)`，Precedence=40 |
| ApprovalStore（文件） | **已落地** | `~/.genesis-agent/{cli,desktop,enterprise}/mcp-approvals.json`（DB 仍属 Phase 2.5） |
| Project 审批热撤销 | **已落地** | 每次 Sync 重新读取审批决定；reject 会关闭既有 session 并撤下 tool 投影，后台 Dial 在提交前再次校验最新定义 |
| CapabilityScope 适用范围 | **已落地（fail-closed）** | 统一检查 channel、tenant、project、agent、user、role、environment；受限维度缺失运行时值时拒绝 |
| OnEvent → Audit/Trace | **已落地** | `adapter/observability.Listener` |
| policy `mcp://` / `mcp__*` matcher | **已落地** | `policy/matcher/mcp` |
| CLI `mcp` 子命令 | **已落地** | `list/get/approve/reject/refresh` |
| Enterprise `/v1/mcp/*` | **已落地（管理面）** | GET servers、GET detail、POST refresh；tool call 调试 API 未做 |
| Desktop | **内核可装配** | `products/desktop/bootstrap` 复用 MCP stack；Wails UI 仍待实现 |
| `ActionMCPCall` 治理接线 | **已落地** | Authorizer + policy matcher + ChainAuthorizer |
| `mcp_search` 动态提升 | **已落地** | 通过 MCP tool 的受锁 `ExposureUpdater` 更新，不再修改共享 `Info` 指针 |
| go-sdk 稳定版对齐 | **已完成** | `go.mod` 固定 `v1.6.1`；文档已修正此前不存在的 `v1.7.0` 稳定版要求 |

**本次明确延期**：Enterprise 管理 API 补全（`tools/call`、`resources/read`、detail 的 resources/auth）以及 Run SSE MCP 生命周期事件；两项均尚未实现。

**持续不在本轮范围**：DB 持久化、OAuth、elicitation、M6 MCP Server 角色。

**运行时身份接线边界**：MCP Stack 已支持注入 project/agent/user/role 上下文。当前 CLI/Desktop/Enterprise bootstrap 仅提供固定 channel/environment/tenant；因此配置了 project/agent/user/role 限制但产品未传入相应身份时会安全禁用，而不是越权放行。Enterprise 请求级身份接线应与多租户/RBAC Phase 2.5 一并完成。

---

## 18. 审查记录（review-fix-rereview）

### 第 1 轮审查（从第一性原理）

**核心目标**：MCP tool 必须以 `tool.Tool` 进入 LLM schema，执行走 Tool Gateway，三产品共享内核。

**发现问题（actionable）**：
1. 文档末尾有多余 ` ``` ` 闭合符 → 已删除
2. §12 参考文件清单不完整（缺 Kode 测试/ACP、Codex ext/app-server-protocol）→ 已补全
3. 缺三产品装配契约与 Enterprise API 专节 → 已增 §15/§16
4. 缺当前实现差距表 → 已增 §17
5. Desktop 产品未实现，§6 表格需明确「复用 CLI 内核 + 未来 Wails UI」→ §15 已细化

**判定**：无架构级冲突；与 `AGENTS.md`、`docs/产品分发架构设计.md`、现有 capability 契约一致。

### 第 2 轮再审查

- [x] LLM schema 不变量：MCP tool flatten 为 `tool_call`，`ActionMCPCall` 仅审批标识
- [x] 依赖方向：`mcp` 域不依赖 `products/`，bootstrap 向下注入
- [x] 单一 Manager：Catalog → Manager.Sync 唯一连接主线
- [x] Enterprise 安全默认：stdio 禁止 + RequirementsFilter
- [x] 参考文件：Kode Core 18 文件 + Codex Tier A 核心均已索引

**残留风险（非阻塞）**：deferred 检索工具形态、OAuth、elicitation、enterprise DB 持久化 — 已在 §14 列出。

**审查结论**：设计可进入 M1 实现。

### 第 3 轮审查与修复（2026-07-15）

**从第一性原理**：MCP 的连接、投影和调用必须始终服从同一份当前治理决策；范围或审批被收紧后，既有连接不能继续越权工作；动态暴露状态不能造成并发数据竞争；文档依赖版本必须对应实际可发布版本。

- [x] project server 在 `reject → refresh` 后会关闭现有 session 并撤下投影；后台连接提交结果前会校验最新定义，避免撤销与 Dial 并发时重新变为 ready。
- [x] scope 判断收敛为 MCP 域唯一匹配器，覆盖 channel、tenant、project、agent、user、role、environment，任何受限维度缺失或不匹配均拒绝。
- [x] `mcp_search` 改为调用 MCP tool 的受锁 `ExposureUpdater`，`GetInfo` 返回快照，消除共享 traits 的直接写入。
- [x] go-sdk 版本要求已与可用稳定版本对齐：当前稳定版为 `v1.6.1`，`v1.7.0` 尚无稳定标签。
- [x] 已新增 project 审批热撤销、全维度 scope、deferred tool 提升的回归测试；MCP/Tool 相关包测试通过。

**延期项（经确认）**：Enterprise 管理 API 补全与 Run SSE MCP 生命周期事件。本轮不修改。

**验证限制**：Windows 环境未安装 `gcc`，`go test -race` 无法启动；常规 Go 测试作为本轮自动化验证，后续 CI 应启用 race detector。

