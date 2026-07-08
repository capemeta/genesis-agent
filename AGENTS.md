# Genesis Agent — AI 编程助手上下文

本文件是项目级全局上下文，只保留稳定原则和硬约束。完整架构、数据模型、路线图以 `docs/agent loop设计.md` 为准；多 Agent 模式参考 `docs/Agent设计模式与多Agent协作空间设计.md`；当前目录职责、依赖方向和产品边界以 `docs/项目目录与边界说明.md` 为准。

## 项目定位

- 项目名称：`genesis-agent`
- 主语言：Go
- 企业 Web 前端目录：`products/enterprise/web`
- 沙箱服务已拆分为独立仓库 `D:\workspace\go\genesis-sandbox`；本仓库仅保留对接契约与客户端适配层
- 目标：构建通用、可扩展、生产级 Agent Runtime，支持 RAG、代码 Agent、业务办理、监控告警、多 Agent 协作和长期执行。
- 当前代码可能落后于文档；开发时以当前代码为事实，以文档为目标方向，必要时同步修正文档。

## 核心原则

- Run Engine 是内核，ReAct / Plan-Execute / Coding / RAG 都是可替换策略。
- 主流程保持通用，组件通过契约接口扩展，避免在 Engine 中硬编码具体实现。
- Tool / MCP / Skill / Sandbox / Memory / LLM / Trace / Auth / Usage 等能力应可独立演化。
- 系统装配集中在 `bootstrap`；平台级通用基础设施放 `platform`；能力实现优先按能力域聚合在 `capabilities`。
- Tool / MCP / Skill / Sandbox 使用统一能力适用范围配置，按接入端、租户、项目、Agent、用户、角色、运行环境过滤。
- 记忆系统从 Phase 1 起支持 ShortTermMemory / LongTermMemory / UserProfileStore 接口；当前可用文件存储实现，后续替换为 DB、Redis、向量库或外部画像服务。
- RBAC、多级限流、全链路异步、缓存/并发优化属于 Phase 2.5；Phase 1B 优先做 Tool Gateway、权限策略、人工干预、状态持久化、SSE、Session/Message 持久化、记忆/画像管理 API、Usage、Hook、Skills 基础。



## 目录与产品边界

当前目录职责、目标分层、依赖方向、产品分发边界和目录调整流程集中维护在 `docs/项目目录与边界说明.md`。后续编码默认必须遵守该文档；如果发现目录不优雅、边界不清晰或影响扩展，必须先按该文档的“文档与目录变更规则”说明方案，并经过用户确认后再实施。

## 工具、文件系统与沙箱边界

以下只记录稳定硬边界；完整设计以 `docs/文件系统设计方案.md` 为准：

- 文件系统工具不得直接依赖 `os`/`filepath` 执行真实文件读写，不得直接 import Docker SDK、HTTP/gRPC client、Wails、RBAC、DB；只能依赖 `FileSystemBackend`、`PathResolver`、`FreshnessTracker`、权限/审计等 port。
- 命令执行工具不属于文件系统工具；应放在 execution 能力域，通过 `CommandRunner` / `SandboxRunner` port 执行，并复用 `PermissionEngine`、`ToolScheduler`、`PathResolver`、`SandboxProfile`，不在 tool 内直接调用 `exec.Command` 或产品 sandbox 实现。
- `internal/capabilities/sandbox/adapter` 只能放产品无关的 genesis-sandbox API client，不放产品默认 endpoint、credential、租户/RBAC 策略；这些由 `products/<product>/bootstrap` 注入。
- `shared/local` 只放宿主机本地能力和本机平台沙箱能力；Docker/genesis-sandbox API backend 不属于 local host backend。
- `products/enterprise` 不拥有通用 genesis-sandbox client，只负责企业上下文、治理策略、endpoint/credential 注入和 bootstrap。
- Docker/genesis-sandbox 模式下，工具和 UI 不得使用或展示宿主机绝对路径作为业务路径，只能使用 workspace-relative path、sandbox path 或 resource id。
- `SandboxAuto` 不能静默降级为无沙箱；降级必须有 warning/trace/audit，`SandboxRequire` 不满足时必须失败。



## 编码规则

- 新增大量代码前，先判断目录层级是否符合架构分层，是否模块化、可扩展、可维护；涉及目录调整或跨产品边界变化时，必须先说明详细原因并经用户确认。
- 控制层不要写业务逻辑，业务逻辑放 `app` 服务层或对应能力域服务。
- 接口优先，通过依赖注入组装。
- 所有函数优先传 `context.Context`。
- 错误使用 `fmt.Errorf("...: %w", err)` 包装，不 panic。
- 多租户相关 DB 查询必须带 `tenant_id`。
- 代码注释用中文，文件编码统一 UTF-8。
- 单文件不要超过 1000 行，超过前先考虑拆分；大拆分前先确认。
- 现阶段是开发阶段，不需要兼容旧数据。
- 后端开发要考虑并发问题和性能问题。
- **所有的设计按最佳实践、优雅设计、可扩展原则。**
- **当前是开发阶段，不要兼容旧代码、旧的数据结构，旧代码和数据结构要清理干净。**



## 文档规则

- 修改文档必须结构清晰，不要简单追加到末尾，内容应放到最合适的位置。
- 架构细节、完整目录、阶段规划写入 `docs/agent loop设计.md`，AGENTS.md 只保留稳定全局约定。



## shell命令执行

- 当前是window环境，你要使用PowerShell/cmd的语法，而不是使用linux的语法
- 执行 Go 构建、测试、工具命令时，必须显式使用仓库内缓存路径：`GOCACHE=D:\workspace\go\genesis-agent\.gocache`，`GOMODCACHE=D:\workspace\go\genesis-agent\.gomodcache`。PowerShell 示例：`$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'; $env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'; go test ./...`
- 编辑 Windows 路径、PowerShell 示例或包含 `$env:` 的文档时，必须注意字符串转义：优先使用单引号、here-string 或 `apply_patch`；不要在 PowerShell 双引号字符串里直接写 `$env:` 示例，避免变量插值把文本改坏。修改后必须读回相关片段确认。




