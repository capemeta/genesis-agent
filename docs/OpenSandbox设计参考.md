# OpenSandbox 设计参考

> 参考项目：`D:\workspace\go\go-project\OpenSandbox`  
> 目的：提炼 OpenSandbox 在沙箱生命周期、协议、运行时、SDK、网络治理和长期 Agent 能力上的可借鉴设计，为 `genesis-agent` 的 Sandbox Service 设计提供参考。  
> 结论先行：OpenSandbox 很适合作为沙箱产品形态、API 资源划分、SDK 体验、egress sidecar、Credential Vault、桌面/浏览器沙箱能力的参考；但 Genesis 第一版仍应保持 Go 独立 Sandbox Service、gRPC + HTTP 双协议、接口优先、Phase 1 Docker SDK。OpenSandbox 风格的 egress sidecar / Credential Vault 属于更偏云原生的 Pod/sidecar 能力，应放到 Phase 2.5 与 K8s/RuntimeClass/多租户网络治理一起评估，不进入 Docker MVP 主流程。

---

## 1. OpenSandbox 总体架构

OpenSandbox 是一个通用沙箱平台，核心能力包括：

- 多语言 SDK：Python、JavaScript/TypeScript、Go、Kotlin、C#。
- CLI：`osb`，支持创建沙箱、执行命令、文件操作、诊断、egress 管理。
- MCP：提供给 Claude Code、Cursor 等 MCP 客户端使用。
- Lifecycle Server：管理沙箱生命周期、快照、endpoint、pause/resume、metadata。
- Execd：运行在沙箱内的执行服务，提供命令、代码、文件、metrics、isolated session API。
- Egress Sidecar：沙箱网络出站策略、DNS/nftables 控制、Credential Vault。
- Docker / Kubernetes Runtime：本地 Docker 与大规模 K8s 调度。
- 可视化环境示例：Playwright、Chrome、desktop、VS Code、Claude Code、OpenClaw 等。

它的整体边界可以抽象为：

```text
SDK / CLI / MCP
      |
      v
Lifecycle API Server
  - sandbox create/list/get/delete
  - snapshots
  - endpoints
  - pause/resume
  - pool
  - proxy
      |
      v
Runtime Provider
  - DockerSandboxService
  - KubernetesSandboxService
      |
      v
Sandbox Workload
  - app container
  - execd
  - optional egress sidecar
```

对 Genesis 的启发：

- 生命周期服务和沙箱内执行服务应分层。Genesis 的 Sandbox Service 不应把所有命令执行细节都塞进生命周期层。
- `execd` 这种“沙箱内 agent/daemon”值得借鉴，用于统一命令、文件、会话、metrics、截图、长期 workspace。
- egress 不一定做成主服务里的 HTTP proxy；在 K8s/Pod 模式下可以做成 sidecar，在单机 Docker/私有化模式下更适合先用外置 Tool-Proxy 或基础网络策略。
- SDK/CLI/MCP 是产品体验的一部分，第一版接口设计要为这些客户端留稳定协议。

---



## 2. 目录与分层可借鉴点

OpenSandbox 仓库大致分为：

```text
server/          # Python FastAPI 生命周期服务
specs/           # OpenAPI specs：lifecycle / execd / egress / diagnostics
sdks/            # 多语言 SDK
cli/             # osb CLI
components/
  egress/        # Go 实现的 egress sidecar
  internal/      # Go 公共组件，如 supervisor/logger/telemetry
kubernetes/      # K8s CRD / controller / manifests
oseps/           # 设计提案文档
examples/        # playwright / desktop / vscode / openclaw / qwen-code 等示例
tests/           # 多语言 E2E
```

值得借鉴：

- `specs/` 独立保存协议，便于 SDK 生成和版本治理。
- `oseps/` 作为设计提案目录，类似 ADR/RFC，适合记录重大沙箱能力演进。
- `components/egress` 独立 Go 组件，说明网络治理可以拆成专门组件。
- `examples/` 覆盖真实场景，不只是单元测试，对沙箱这种基础设施非常关键。

Genesis 推荐映射：

```text
cmd/
  sandbox-service/
internal/
  contracts/sandbox/       # 内部接口
  protocol/sandbox/        # HTTP/gRPC DTO 与 proto/openapi 映射
  app/sandbox/             # 用例编排：lease/job/file/pool/profile
  domain/sandbox/          # Sandbox/Job/Lease/Pool/Profile/Policy 模型
  adapters/sandbox/
    docker/
    localprocess/
    remote/
    egress/
  infra/config/
docs/
  沙箱环境设计.md
  OpenSandbox设计参考.md
  adr/ 或 oseps/          # 后续重大沙箱设计提案
api/
  proto/sandbox/
  openapi/sandbox/
```

注意：OpenSandbox 的 server 是 Python/FastAPI，Genesis 是 Go 项目，不应照搬代码结构；应借鉴“协议独立、服务抽象、运行时 provider、组件拆分”的思想。

---



## 3. 协议与 API 设计

OpenSandbox 主要使用 HTTP/OpenAPI，协议分成四类：

1. `sandbox-lifecycle.yml`
  - `GET /sandboxes`
  - `POST /sandboxes`
  - `GET /sandboxes/{sandboxId}`
  - `DELETE /sandboxes/{sandboxId}`
  - `PATCH /sandboxes/{sandboxId}/metadata`
  - `POST /sandboxes/{sandboxId}/pause`
  - `POST /sandboxes/{sandboxId}/resume`
  - `POST /sandboxes/{sandboxId}/renew-expiration`
  - `GET /sandboxes/{sandboxId}/endpoints/{port}`
  - snapshots 相关 API
2. `execd-api.yaml`
  - `/command`：执行命令、取消、查状态、查日志。
  - `/session`：bash session。
  - `/code`：代码执行 context。
  - `/files/*`：文件上传、下载、搜索、移动、权限、替换。
  - `/metrics`：系统指标。
  - `/v1/isolated/*`：隔离 session、diff、commit、隔离文件代理。
3. `egress-api.yaml`
  - `/policy`：获取、替换、patch、删除 egress rules。
  - `/credential-vault`：创建、更新、删除凭证仓库。
  - `/credential-vault/credentials`、`/bindings`：查看脱敏元数据。
4. `diagnostic-api.yml`
  - `/sandboxes/{sandboxId}/diagnostics/logs`
  - `/sandboxes/{sandboxId}/diagnostics/events`

Genesis 的取舍：

- 保留 OpenSandbox 的资源划分思想：Lifecycle、Execution、Files、Diagnostics 分开；Egress 只在协议上预留扩展点，具体实现放到 Phase 2.5。
- Genesis 已决定支持 `gRPC + HTTP`，所以不应只做 OpenAPI。
- 建议先定义 proto 作为内部强契约，再生成/手写 HTTP 映射：

```text
SandboxLifecycleService
  CreateSandbox
  LeaseSandbox
  ReleaseLease
  DrainSandbox
  DestroySandbox
  GetSandbox
  ListSandboxes
  RenewSandbox

SandboxJobService
  ExecJob
  CancelJob
  GetJob
  StreamJobLogs

SandboxFileService
  UploadFile
  DownloadFile
  ListArtifacts

SandboxPoolService
  GetPool
  UpdatePool
  ReconcilePool

SandboxEgressService
  ApplyPolicy
  GetPolicy

SandboxDiagnosticsService
  GetLogs
  GetEvents
  GetMetrics
```

需要补强 OpenSandbox 的地方：

- OpenSandbox 生命周期 API 里没有明确 `lease_id` 语义；Genesis 应把租约作为一等模型。
- OpenSandbox HTTP 资源很完整，但 Genesis 内部高频调用更适合 gRPC stream。
- OpenSandbox 的 pool 在 K8s runtime 才有 server-side API；Genesis Phase 1 单机 Docker 也需要本地池配置和队列。

---



## 4. Runtime Provider 与服务抽象

OpenSandbox 的 server 通过 `SandboxService` 抽象生命周期能力，并由 factory 选择实现：

```text
SandboxService
  create_sandbox
  list_sandboxes
  get_sandbox
  delete_sandbox
  pause_sandbox
  resume_sandbox
  renew_expiration
  patch_sandbox_metadata
  get_endpoint

implementations:
  docker -> DockerSandboxService
  kubernetes -> KubernetesSandboxService
```

这是值得直接借鉴的模式。

Genesis 推荐接口：

```go
type RuntimeDriver interface {
    Probe(ctx context.Context) (RuntimeCapabilities, error)
    CreateSandbox(ctx context.Context, spec SandboxSpec) (*SandboxHandle, error)
    StartSandbox(ctx context.Context, sandboxID string) error
    DestroySandbox(ctx context.Context, sandboxID string) error
    ExecJob(ctx context.Context, sandboxID string, job JobSpec) (*JobResult, error)
    CancelJob(ctx context.Context, jobID string) error
    StreamLogs(ctx context.Context, jobID string) (<-chan LogEvent, error)
    UploadFile(ctx context.Context, sandboxID string, file FileObject) error
    DownloadFile(ctx context.Context, sandboxID string, path string) (io.ReadCloser, error)
}
```

同时需要把执行后端抽象为：

```text
ExecutionBackend
  docker
  local_process
  trusted_only
  remote_sandbox
  disabled
```

OpenSandbox 的 `runtime.type = docker/kubernetes` 是 server 级配置；Genesis 需要更细：

- server 默认 backend
- profile 默认 backend
- 租户/项目/Agent 可覆盖
- Policy Engine 最终裁决

---



## 5. 配置体系

OpenSandbox 的配置非常细，主要章节：

- `[server]`：host、port、api_key、最大 sandbox TTL、并发限制、线程池、HTTP parser。
- `[runtime]`：docker / kubernetes、execd image。
- `[docker]`：network_mode、Docker API timeout、host_ip、capabilities、AppArmor、seccomp、pids_limit。
- `[kubernetes]`：namespace、service account、workload provider、informer、QPS、snapshot timeout。
- `[ingress]`：direct / gateway，gateway route mode。
- `[egress]`：sidecar image、dns / dns+nft、disable IPv6。该配置更适合 K8s/Pod sidecar 模型，Genesis Docker MVP 不应强依赖。
- `[storage]`：host bind allowlist、OSSFS root、volume default size。
- `[store]`：SQLite 持久化。
- `[secure_runtime]`：gVisor / Kata / Firecracker。
- `[renew_intent]`：访问时自动续期。

Genesis 可借鉴：

- 配置必须有 cross-field validation，启动时发现不兼容直接失败。
- `allowed_host_paths` 默认为空，禁止宿主机 bind mount，这是很好的安全默认值。
- `server.api_key` 缺失时要求显式风险确认，这个思路可用于开发模式。
- `secure_runtime` 不应静默启用失败，必须 probe 后明确失败或按策略降级。
- `egress` 要明确模式差异：DNS-only 不能保证 CIDR/IP 层拦截，严格模式要 dns+nft 或 CNI 级能力。单机 Docker 阶段只保留策略抽象，不实现 sidecar 复杂治理。

Genesis 需要新增/保留的配置：

```yaml
sandbox:
  enabled: true
  default_backend: docker
  profiles:
    code-python-basic:
      min_idle: 2
      initial_idle: 1
      max_idle: 10
      max_total: 50
      max_queue_depth: 200
      max_wait_seconds: 10
      exec_timeout_seconds: 30
      dependency_policy:
        allow_runtime_install: false
  local_process:
    enabled: false
    allow_user_code: false
  docker:
    network_mode: bridge
    no_new_privileges: true
    pids_limit: 256
  secure_runtime:
    preferred: runsc
    allow_downgrade: false
```

---



## 6. 池化设计

OpenSandbox 有两种池思路：

1. K8s server-side Pool API
  - `/pools`
  - `/pools/{poolName}`
  - capacitySpec 只允许更新容量，不允许更新 pod template。
  - 非 K8s runtime 返回 501。
2. OSEP-0005 Client-Side Sandbox Pool
  - SDK 本地维护 idle buffer。
  - 不依赖 runtime 特性。
  - runtime 仍是资源限制的权威。
  - empty behavior 可配置：直接创建或 fail fast。
  - state store 抽象，支持单机/分布式。
  - 用 primary lock、防重、backoff、metrics 处理多进程并发。

对 Genesis 的建议：

- Phase 1 不采用纯 client-side pool，因为 Agent Runtime 是服务端编排，池应在 Sandbox Service 统一治理。
- 但 OSEP-0005 的思想值得借鉴：池只是 idle buffer 目标，不承诺无限可用；runtime quota 才是硬约束。
- Pool 配置要有 `min_idle / max_idle / max_total / warmup_concurrency / empty_behavior / backoff / metrics`。
- 对 SDK 可以后续增加 client-side pool，用于桌面端或轻量客户端。

Genesis 推荐：

```text
Phase 1:
  Server-side Pool Manager
  profile 级池配置
  队列与租约统一在 Sandbox Service

Phase 2:
  分布式 Pool State Store
  Redis/PostgreSQL lease
  多节点调度

Phase 2.5:
  K8s/RuntimeClass 池
  client-side pool 可选
```

---



## 7. Execd 与沙箱内执行服务

OpenSandbox 的 `execd` 是非常值得借鉴的核心设计。它把“生命周期”和“沙箱内执行”拆开：

- Lifecycle Server 负责创建/销毁沙箱、查 endpoint。
- SDK 先通过 Lifecycle 获取 execd endpoint。
- SDK 再调用 execd 执行命令、读写文件、创建 session、获取 metrics。

execd API 覆盖：

- command：前台/后台命令、取消、状态、日志。
- session：长期 bash session。
- code context：代码执行上下文。
- files：上传、下载、搜索、移动、chmod、replace。
- directories：mkdir/list/delete。
- metrics：系统指标。
- isolated：隔离 session、diff、commit、isolated filesystem proxy。

Genesis 可借鉴：

- 不要把文件/命令操作都设计成 Docker exec 的薄封装。后续长期 Agent、浏览器、桌面、Claude Code 类能力都需要沙箱内 daemon。
- Phase 1 可以先用 Docker exec 实现最小能力，但接口要按 execd 思路设计。
- Phase 2 可实现 `genesis-execd`，统一命令、文件、会话、metrics。

推荐阶段：

```text
Phase 1:
  Docker exec + file staging
  接口预留 ExecdClient

Phase 1B:
  genesis-execd MVP
  command/file/session/log stream

Phase 2:
  isolated session / overlay workspace
  diff / commit / metrics
```

---



## 8. Isolated Execution 可借鉴点

OpenSandbox OSEP-0013 使用 bubblewrap 在一个 sandbox Pod 内提供 per-execution namespace isolation：

- `/v1/isolated/session`
- `/v1/isolated/session/{id}/run`
- `/diff`
- `/commit`
- isolated files proxy
- capabilities endpoint

核心思想：

- 外层容器仍是主安全边界。
- 内层 isolated session 用于减少多次执行互相污染。
- overlay workspace 支持回滚、diff、commit。
- capabilities endpoint 告诉客户端当前支持什么，不静默降级。

对 Genesis 的判断：

- 非第一版必需。
- 适合长期 Agent、代码解释器、RL/evaluation、高频多步骤工具。
- 不应把 bubblewrap 当作替代 Docker/gVisor 的安全边界。
- 如果引入，必须 capability probe + 明确 fallback。

Genesis 可放：

```text
Phase 2:
  isolated session 能力探索
  overlay workspace / diff / commit
  capabilities API
```

---



## 9. 网络治理与 Egress Sidecar

OpenSandbox 的 egress 是 Go 组件，设计成熟，值得重点借鉴：

- sidecar 与 app container 共享 network namespace。
- DNS proxy 拦截 53 端口。
- `dns` 模式：FQDN 过滤，但不保证 CIDR/IP 强拦截。
- `dns+nft` 模式：DNS + nftables，解析到的 IP 加入动态 allow set。
- `deny.always / allow.always / log_skip.always` 支持平台级不可覆盖规则。
- 支持动态 `/policy` API。
- 可选 transparent MITM，用于 Credential Vault。
- sidecar 需要 `CAP_NET_ADMIN`，app container 不需要特权。
- fail-closed：拦截规则设置失败则退出，避免流量泄漏。

对 Genesis 的启发：

- Tool-Proxy 不是唯一方式。对于 K8s/Pod 场景，egress sidecar 是很好的实现；对于单机 Docker/私有化轻量部署，外置 Tool-Proxy 或基础 Docker network 策略更简单。
- Phase 1 可以只做 `network=none`、基础 allowlist 字段、Docker bridge/internal network 隔离和策略 DTO。
- Phase 2.5 再做 Tool-Proxy；如果同时引入 K8s/RuntimeClass，再评估 egress sidecar。
- 如果要做严格 FQDN allowlist，必须考虑 DNS 解析后 IP 层 enforcement，否则容易绕过。
- IPv6 默认关闭/不承诺，是务实选择。
- service mesh sidecar 与透明 egress sidecar 会冲突，需作为 profile 限制。

Genesis 可借鉴配置，但应标记为 Phase 2.5 / K8s profile 配置，不作为 Docker MVP 配置：

```yaml
egress:
  enabled: false
  mode: dns+nft
  disable_ipv6: true
  always_deny:
    - 169.254.169.254
    - 10.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
  max_rules: 4096
```

---



## 10. Credential Vault

OpenSandbox Credential Vault 的设计非常适合 AI Agent：

- 真实凭证写入 host/sidecar 的 vault。
- 沙箱内只看到 fake/empty env。
- sidecar 通过透明 MITM 检查 outbound HTTPS 请求。
- 按 host/method/path/scheme/port 匹配 binding。
- 匹配后注入 bearer/basic/apiKey/customHeaders。
- vault API 返回脱敏元数据，不返回明文凭证。

这解决了一个关键问题：Agent 需要调用外部 API，但不能把真实 key 暴露给不可信代码或提示注入。

Genesis 建议：

- Phase 1 不必实现透明 MITM。
- 但协议上应预留 `CredentialBinding` / `SecretRef` / `credential_policy`。
- Tool Gateway 调外部 API 时，优先由宿主受控代理注入凭证。
- 第三方 Skill 不直接拿真实密钥。
- Phase 2.5 如果做 Tool-Proxy 或 K8s egress sidecar，再实现透明凭证注入；Docker MVP 不做透明 MITM。

---



## 11. Ingress / Endpoint / Proxy

OpenSandbox 支持通过 Lifecycle API 获取 sandbox endpoint：

```text
GET /sandboxes/{sandboxId}/endpoints/{port}
```

server 还有 proxy route，可以把 HTTP/WebSocket 请求代理到沙箱内服务，并过滤敏感 header：

- 不转发 `authorization`、`cookie`、API key header。
- 过滤 hop-by-hop headers。
- 注入 `X-Forwarded-*`。
- 支持访问时 renew expiration。

对 Genesis 的借鉴：

- 沙箱内服务暴露不应直接给 Pod IP/容器 IP。
- Endpoint 应是受控资源，带租户鉴权、短期 token、审计。
- 可视化浏览器、VNC、VS Code、长期 Agent 都需要 endpoint/proxy 能力。
- Phase 2.5 的桌面可视化要基于这个能力，而不是暴露宿主桌面。

---



## 12. Snapshot / Pause / Resume

OpenSandbox 生命周期 API 已包含：

- snapshots list/get/delete/create
- create sandbox from snapshot
- pause
- resume
- renew expiration

Genesis 当前文档把 Snapshot/Restore 放 Phase 2，这是合理的。

可借鉴点：

- Snapshot 是一等资源，不只是内部优化。
- Snapshot metadata 要持久化，OpenSandbox 用 SQLite 作为本地默认存储。
- pause/resume 和 snapshot 要区分：pause 保持状态，snapshot 用于恢复/复制。
- Kubernetes snapshot timeout 要比底层 controller timeout 更大，避免前端误判。

Genesis 建议：

```text
Phase 1:
  不承诺 snapshot SLA
  只保留接口扩展点

Phase 2:
  SnapshotRepository 接口
  Docker snapshot / image commit / volume archive 探索

Phase 2.5:
  K8s / RuntimeClass / CRD snapshot
```

---



## 13. 多租户设计

OpenSandbox OSEP-0014 的多租户设计重点：

- K8s 模式：tenant -> namespace。
- API key -> tenant。
- `TenantProvider` 抽象，初始文件 `tenants.toml`，未来可接 IAM/K8s Secret。
- Docker runtime 明确不支持强多租户；有 tenants.toml 时直接启动失败。
- per-tenant namespace 使用 ResourceQuota/LimitRange/NetworkPolicy。
- Auth middleware 注入 tenant context。

对 Genesis 的判断：

- OpenSandbox 对 Docker 多租户的结论偏严格，适合公网平台。
- Genesis Phase 1 若是单机私有化，可支持“弱隔离”：tenant_id + 独立 bridge + 独立 workspace + quota。
- 但文档必须明确：Docker 单机租户隔离不是强隔离，强多租户要 K8s namespace / Kata / Firecracker / 独立节点。

Genesis 可借鉴：

- `TenantProvider` 抽象。
- Auth middleware 注入 tenant context。
- 所有 sandbox/job/pool/endpoint 查询必须带 tenant scope。
- 多租户配置与普通 server config 分离。

---



## 14. Secure Runtime 设计

OpenSandbox `secure_runtime` 是 server-level 配置：

- `type = "" | gvisor | kata | firecracker`
- Docker 模式用 `docker_runtime`，如 `runsc`。
- K8s 模式用 `k8s_runtime_class`。
- Firecracker 仅 K8s。
- 启动时校验 runtime 是否可用，不可用则拒绝启动。

Genesis 与 OpenSandbox 差异：

- OpenSandbox 是 server 全局 runtime。
- Genesis 更适合 profile/policy 级 runtime：低风险 runc，高风险 runsc/kata。
- Genesis 需要允许按 risk_level 和 tenant_plan 裁决。

建议：

```text
RuntimeCapabilityProbe:
  docker runtimes: runc/runsc/kata
  k8s RuntimeClass: gvisor/kata/firecracker

Policy:
  high risk -> runsc/kata required
  medium -> runsc preferred, runc allowed if allow_downgrade
  local dev -> runc
```

---



## 15. 可视化桌面与长期 Agent

OpenSandbox examples 覆盖：

- Playwright
- desktop
- VS Code
- Claude Code
- OpenClaw
- Qwen Code

这说明 Manus 类能力并不是一个小功能，而是由以下基础设施组合出来的：

- 长期 sandbox workspace。
- endpoint/proxy。
- 浏览器或桌面镜像。
- VNC/noVNC/WebSocket/WebRTC。
- 文件上传下载。
- 命令执行和日志流。
- 网络和凭证治理。
- session TTL/renew。

Genesis 当前把可视化浏览器/桌面放 2.5 是合理的。

建议：

```text
Phase 1:
  browser-playwright headless
  screenshot / trace / artifact

Phase 2.5:
  browser-desktop profile
  noVNC/WebRTC endpoint
  操作事件审计
  下载文件隔离

Phase 3:
  长期 workspace + checkpoint + 多 Agent 协作
```

---



## 16. SDK/CLI/MCP 体验

OpenSandbox 的产品化能力很强：

- SDK 把 lifecycle、execd、egress 聚合成 `Sandbox` 对象。
- `SandboxManager` 管理列表、kill、pause、resume、snapshot。
- CLI 覆盖 create/run/file/diagnostics/egress。
- MCP 让 Claude Code/Cursor 直接调用沙箱。

Genesis 可借鉴 SDK 对象模型：

```go
type Sandbox struct {
    ID string
    Lifecycle SandboxLifecycleClient
    Jobs SandboxJobClient
    Files SandboxFileClient
    Egress SandboxEgressClient
}

type SandboxManager struct {
    Lifecycle SandboxLifecycleClient
    Pools SandboxPoolClient
    Snapshots SandboxSnapshotClient
}
```

不过 Genesis 第一阶段重点仍是服务端 Agent Runtime，不急着做完整多语言 SDK。应先保证 HTTP/gRPC 协议稳定，再做 CLI/SDK/MCP。

---



## 17. 测试与工程实践

OpenSandbox 值得借鉴的测试方式：

- server 单元测试覆盖 config、validators、routes、runtime resolver、docker/k8s service。
- 多语言 SDK E2E：Python、JavaScript、Go、Java、C#。
- egress Go 组件有 policy、dnsproxy、nftables、credential vault、metrics 测试；这些更适合放在 K8s/sidecar profile 的 E2E 中。
- examples 覆盖真实 AI Agent 场景。

Genesis 建议：

```text
Phase 1:
  RuntimeDriver fake 单测
  DockerDriver 集成测试可选
  Policy merge / config validation 单测
  Pool queue / lease / timeout 单测

Phase 1B:
  HTTP/gRPC contract test
  CLI smoke test
  Tool execution E2E

Phase 2.5:
  egress / browser / desktop E2E
```

---



## 18. 不建议照搬的点

1. 不建议 Genesis server 用 Python/FastAPI。
  - Genesis 主语言是 Go，Sandbox Service 更适合 Go 实现。
2. 不建议第一版只做 HTTP/OpenAPI。
  - Genesis 已需要内部服务高频调用，gRPC + stream 更合适。
3. 不建议第一版做完整 K8s CRD。
  - 先 Docker SDK + RuntimeDriver 抽象，Phase 2.5 再上 K8s/containerd/RuntimeClass。
4. 不建议第一版做透明 MITM Credential Vault。
  - 技术复杂、合规风险高。先做 SecretRef/Tool Gateway 受控注入。
5. 不建议把 isolated execution 当安全边界。
  - 它适合性能和污染隔离，不替代外层容器/gVisor/Kata。
6. 不建议单机 Docker 宣称强多租户。
  - 只能作为开发/私有化弱隔离；强隔离要 K8s namespace、RuntimeClass、Kata/Firecracker 或独立节点。

---



## 19. 对 Genesis 的落地建议



### 立即纳入 Phase 1 设计

- 协议资源拆分：Lifecycle / Job / File / Pool / Diagnostics / NetworkPolicy/Egress 扩展点。
- `RuntimeDriver` + `ExecutionBackend` 抽象。
- profile 级池配置和队列。
- `lease_id` 一等模型。
- metadata/labels/filter/list/pagination。
- endpoint/proxy 扩展点。
- config cross-field validation。
- host bind mount 默认禁止。
- runtime capability probe。



### Phase 1B 可做

- `genesis-execd` MVP：command、file、session、metrics。
- CLI smoke：create/exec/file/log。
- HTTP/gRPC contract test。
- Skill build/runtime profile。
- SecretRef/CredentialPolicy 协议字段。



### Phase 2

- SnapshotRepository。
- isolated session / overlay diff / commit。
- 分布式 pool state store。
- renew-on-access。



### Phase 2.5

- Tool-Proxy。
- K8s RuntimeClass / containerd adapter。
- K8s egress sidecar 与 Credential Vault（跟随 K8s/sidecar 能力，不单独阻塞 Docker 路线）。
- browser-desktop / desktop-agent。
- Falco/eBPF。
- 强多租户 namespace/Quota。

---



## 20. 推荐更新到 Genesis 沙箱文档的点

后续可以把以下点合并进 `docs/沙箱环境设计.md`：

- 新增 `genesis-execd` 作为 Phase 1B/2 目标。
- 明确 Lifecycle API 与 Execd API 分离。
- endpoint/proxy 作为可视化桌面和沙箱服务暴露的基础能力。
- Credential Vault 作为 Phase 2.5 Tool-Proxy 或 K8s egress sidecar 的子能力；Docker MVP 不做透明凭证注入。
- OSEP/ADR 文档机制，用于重大沙箱能力设计。
- `allowed_host_paths=[]` 作为默认安全配置。
- `deny.always / allow.always` 类平台级不可覆盖规则。
- `capabilities` endpoint，所有高级能力必须显式探测。

---



## 21. 最终判断

OpenSandbox 对 Genesis 的最大价值不是“照搬实现”，而是验证了一套成熟的沙箱产品边界：

```text
Lifecycle Server 管资源
Execd 管沙箱内执行
Egress/Proxy 管网络和凭证（K8s/sidecar 或 2.5 增强能力）
SDK/CLI/MCP 管开发者体验
OSEP/Specs 管协议演进
Runtime Provider 管 Docker/K8s/安全 runtime
```

Genesis 的最佳实践是吸收这套边界，同时保持自己的核心约束：

- Go 实现 Sandbox Service。
- gRPC + HTTP 双协议。
- Engine 不直接依赖 Docker/K8s/HTTP。
- Phase 1 聚焦 Docker SDK、配置化池、租约、队列、文件 staging、基础执行。
- Phase 2.5 再实现 Tool-Proxy/Falco/containerd/K8s/egress sidecar/桌面可视化等增强能力。




