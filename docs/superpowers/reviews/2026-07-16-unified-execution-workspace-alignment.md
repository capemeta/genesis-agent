# 统一执行工作空间实现对齐审查

> 基准：`docs/统一执行工作空间、文件权限与产物规范.md`  
> 日期：2026-07-18
> 结论：核心安全纵切、L1/L2/L3 执行隔离与显式 Handoff 已形成通用控制面；跨产品持久治理、资源投影和产品编排接入仍未完成，不能把整份规范标记为“全部完成”。

## 对齐矩阵

| 规范能力 | 状态 | 代码证据 | 说明 |
| --- | --- | --- | --- |
| 三种 WorkspaceMode 与 backend 正交 | 已实现 | `internal/capabilities/execution/model`、`execution/service`、`sandbox/session` | 旧 mode 常量、字符串和 adapter 改写逻辑已删除 |
| ExecutionBinding/owner/path policy | 已实现 | `execution/model/binding.go`、`execution/pathcontract` | runner 缺少或冲突 binding 时稳定失败 |
| StateRoot、Run manifest 与 execution namespace | 已实现（Enterprise adapter 待接） | `workspace/service/run_preparer.go`、`shared/local/workspace/manifest_store.go`、`workspace/adapter/sandbox`、`products/enterprise/bootstrap/container.go` | Run ID/binding/state root/path map 在 Engine 前固化；Store 查询强制 tenant_id 并用 expected revision CAS；Enterprise 未注入外部 Store 时拒绝启动，不再使用内存 adapter |
| InputRef/InputStager/WorkspaceView | 本地闭环已实现；Enterprise adapter 待接 | `workspace/service/input_stager.go`、`workspace/service/workspace_view.go`、`shared/local/workspace/resource_registry.go`、`view_projector.go`、Skill harness | CLI 请求精确文件在 LLM 前完成 scope/version/hash/快照/相对别名工作副本；Skill 自动投影；Enterprise 租户 InputStore/Projector 未实现 |
| 模型相对路径与工具一致性 | 已实现 | `workspace/model/logical.go`、`shared/local/pathresolver/resolver.go`、`runtime/prompt/environment.go` | 文件工具、command cwd 与 Skill 输入均消费 PreparedRun；静态项目根回退和物理 cwd 提示已删除 |
| 项目变更完成门禁 | CLI 已实现 | `shared/local/workspace/git_change_guard.go`、`runtime/strategy/react/react_loop.go` | 比较 RunStartBaseline 与当前 Git 状态，兼容用户原有 dirty worktree；Sandbox optional/required 不静默回退 Host attempt |
| Run 资源释放 | CLI 已实现 | `workspace/contract.RunResourceReleaser`、`skill/script/service.ReleaseRun`、`app.RunOnce` | 远程 Skill session 在 Run 内复用，任意终态立即关闭，idle TTL 仅兜底 |
| produced 与 Artifact 分离 | 已实现 | `skill/script/contract`、`skill/script/service`、`artifact/tool/publish_artifact` | Skill 旧 Artifact 字段和自动回收链已删除 |
| Artifact 两阶段发布与结构门禁 | 已实现 | `artifact/service/publisher.go`、`artifact/service/gate.go`、`shared/local/artifact/store.go` | ZIP/Office 条目、展开量、压缩比和必要结构已校验 |
| DeliveryTarget 默认策略 | 部分实现 | `artifact/service/delivery.go`、`shared/local/artifact/default_delivery.go`、Desktop/Enterprise bootstrap | CLI 支持源平级→项目根；Desktop 无项目时投影到可见 Inbox；Enterprise 已强制注入 publisher/source/delivery，具体租户对象存储与下载入口未实现 |
| Agent App 工作空间裁剪 | 部分实现 | `agentapp/contract`、`agentapp/adapter/memory`、`workspace/service/resolver.go`、三产品 bootstrap | App ID 选择、有效快照 registry、会话作用域校验和三端注入已完成；持久 Source 与多层策略合并未完成 |
| MCP Placement/FileBinding | 部分实现 | `mcp/model/config.go`、`mcp/transport`、`mcp/tooladapter/file_binding.go` | placement/环境白名单/schema 发现已完成；sandbox launcher 和文件上传重写未完成 |
| 多智能体资源隔离 | 部分实现 | `runtime/multiagent/controller`、`subagent/tool/task`、`runtime/multiagent/execution`、`runtime/multiagent/handoff` | L1 子 Run、L2 step 独立 namespace、L3 member 独立子 Run和显式 Handoff 核心服务已实现；产品 Workflow/CollaborationSpace 编排接入、只读投影和受锁共享写仍待实现 |
| Enterprise 不回退实例本地执行 | 已实现 | `products/enterprise/bootstrap/container.go` | 只允许远程受治理执行；未配置时明确拒绝；产品隔离脚本通过 |
| 生命周期、配额、retention、GC、全链路审计 | 未实现 | — | 属于 Phase F，不能用本地文件默认值代替租户治理 |

## 当前里程碑判定

| 范围 | 判定 | 收尾说明 |
| --- | --- | --- |
| 通用 Workspace/Execution 控制面 | 已完成 | 契约、控制面、三端装配边界和失败策略稳定，可以冻结 |
| 通用 Artifact/Delivery 控制面 | 已完成 | 本地交付闭环完成；Enterprise 具体对象存储属于后续 adapter |
| L1/L2/L3 默认执行隔离与 Handoff | 已完成核心 | 通用 Coordinator/Handoff 已完成；实际业务编排调用属于产品集成 |
| Enterprise 生产持久治理 | 后续里程碑 | 依赖 DB、对象存储、租户策略、配额、retention、GC 和下载入口 |
| UI、MCP bridge、资源投影与共享写 | 后续里程碑 | 不改变本轮核心契约，应在各自产品/能力阶段独立验收 |

因此本轮可以按“统一执行工作空间核心控制面与安全纵切完成”正式收尾，但不能把整份规范中的生产治理和产品体验阶段标记为全部完成。

## review-fix-rereview 历史记录

### Cycle 1

从“中间文件不能因写入目录而自动升级为正式交付”出发，发现 ReAct 和 Skill 合同仍保留 `artifacts[].path` 旧语义。修复包括：删除 Skill `Artifact` 字段及自动产物链；`produced` 只返回 `run:/` 候选；交付完成状态只由 `publish_artifact` 建立；删除子智能体默认共享 `WorkspaceRoot: "."`。

### Cycle 2

从“所有边界转换必须重新鉴权并绑定版本”出发，发现 Skill 仍在内核解析裸路径并直接 `os.ReadFile`。修复包括：产品控制面 `ResourceRegistry`、版本化 `ResourceRef`、通用 `InputStager`、不可变快照和 harness hash 复核；删除旧 `stageInputs` 与路径猜测；远程 Skill 包直接 materialize 到 WorkspaceFS，不再借宿主 `/workspace` 建临时目录；增强 Artifact 结构门禁。

### Cycle 3

从“三产品共享内核，但 Enterprise 不能获得宿主旁路”出发，产品隔离检查证实 Enterprise 仍依赖本地 runner；发布源也存在解析后被替换的 TOCTOU 窗口。修复包括：移除服务器本地执行和 local platform sandbox 回退；未配置远程 executor 时明确失败；Capability 文件 store 移入能力 adapter；Artifact 源打开后复核文件身份与内容 hash；`scripts/check_product_isolation.ps1` 通过。

### Cycle 4

从“Run 身份和资源授权必须先于执行副作用”出发，发现 Run ID 仍由 ReAct 内部生成，`run_command` 仍自行构造 project binding，子 Run 也没有独立工作空间 manifest。修复包括：增加统一 `RunPreparer`、有效 Agent App 校验、RunManifest 排他持久化和受信 context 快照；三端在 bootstrap 注入同一 resolver；ReAct 强制消费控制面 Run ID；`run_command` 强制消费控制面 binding/workspace；L1 子 Run 重新解析独立 `task_job` binding，并固化 parent 关联。复审又发现 sandbox override 拒绝发生在 manifest 创建之后，已调整为先校验、后创建，避免无效请求留下误导性 Run 状态。

### Cycle 5

对修复后的控制面重新执行 manifest 不变量、父子隔离、三产品依赖和历史名称扫描。补充 RunPreparer 身份/binding/parent/App/manifest 固化测试，并把 manifest store 从浅字段检查提升为完整模型校验。未发现本轮实现范围内新的阻断缺陷；Enterprise 持久化、L2/L3、统一派生 execution preparer 等仍按下节明确保留，未伪装为已完成。

## 未闭环风险与后续顺序

1. **高：Enterprise Artifact/Input 租户存储 adapter 未实现。** Bootstrap 已要求外部 publisher/source/delivery 且缺失即拒绝启动；仍需对象存储、WorkspaceFS staging、租户下载入口、配额和 retention 的具体实现。
2. **高：Enterprise Run manifest 持久 adapter 未实现。** 进程内默认已删除，端口已强制 `tenant_id` 查询和 expected revision CAS；在 DB/远端 KV adapter 注入前 Enterprise 入口会明确拒绝启动。
3. **高：L2/L3 产品编排和持久 Handoff Store 尚未接入。** 通用 Coordinator 已生成独立 binding/子 Run，Handoff 已按权威 manifest 解析双方并逐项重新鉴权；仍需实际 Workflow/CollaborationSpace 服务调用这些端口，并注入多租户持久 receipt store。
4. **中：MCP sandbox placement 目前 fail-closed。** 需要 ExecutionRunner launcher、ResourceRef staging/upload 和返回资源重写后才能启用。
5. **中：产品 UI/资源入口投影未完全闭环。** Desktop 已物化到可见 Inbox，但 Wails 卡片/预览尚未实现；Enterprise 仍需返回租户 Artifact/业务资源链接；内部 Run 路径不得作为替代。
6. **中：Delivery Freshness 条件替换未实现。** 当前策略拒绝覆盖，后续若允许替换必须带 expected version/hash 和同目录原子提交。
7. **低：Skill package cache 尚未抽象为独立 ResourceRef。** 依赖安装 binding 已统一，并验证实际缓存路径位于控制面 project workspace；后续若支持跨项目共享 cache，应先增加专用 ResourceRef、锁和配额，不能直接扩大当前路径授权。

下一实施顺序固定为：Enterprise tenant store adapters 与 Handoff 持久 Store → Workflow/CollaborationSpace 产品编排接入及资源投影/受锁共享写 → MCP sandbox/file bridge → Desktop Wails/Enterprise 下载入口 → retention/GC/audit 验收。

## 2026-07-17 继续实施与审查

### 本次 Cycle 1

从“manifest 是唯一执行授权事实”出发，删除 Hook、Skill command、Artifact 源解析和本地逻辑目录中的临时 binding 构造；新增统一 ExecutionPreparer、manifest revision 追加和稳定派生主体复用。审查发现既有可写 binding 可能被只读调用直接复用，已改为复用前重新校验 mode/access 收窄关系并补拒绝测试。

### 本次 Cycle 2

从“App 身份不是客户端可提交的权限配置”出发，增加产品注入的 EffectiveProfile Resolver 和内置 registry；RunRequest 只携带 App ID，CLI/Enterprise 会话固化规范化 App ID，app 层再次校验 tenant/user/App 会话作用域。复审发现子智能体请求仍能携带与父 manifest 不一致的 tenant/session/parent 标识，已改为强制从父快照派生。

### 本次 Cycle 3

重新检查恢复和并发路径，发现 Resume/跨 Controller 恢复时调用上下文可能没有父快照。已改为按持久 `parent_run_id` 从控制面恢复 manifest，并选择根 execution 重建只读上下文；当时的 Enterprise 内存实现只会在进程内恢复，后续本轮已彻底删除该生产默认并改为外部 Store fail-closed。

### 本次 Cycle 4

从“并发安全 store 不能把可变内部状态泄露给调用方”重新审查内存 adapter，发现 Go slice/map 的浅拷贝允许读取方修改已保存 manifest。已对 App allowed modes、限制列表、ProjectRoot、execution 列表和 workspace metadata 做深拷贝，并补充调用方篡改返回值不影响 store 的测试。

### 本次 Cycle 5

最终静态扫描发现 Skill 依赖安装是 resolver 外最后一个生产 binding 构造点。确认本地 workspace scope 的缓存位于已授权项目 `.genesis` 后，将其改为稳定的 project 派生 execution，删除 session binding 拼装和 `WorkspaceRoot="."` 回退；执行前对 project root 与 install root 做真实路径解析和越界校验。至此生产代码中 `ExecutionBinding` 仅由 WorkspaceResolver 创建。

## 2026-07-17 Enterprise 持久化边界与 Desktop 交付审查

### 第 1 轮

从“多实例不能丢失 manifest revision”出发，发现 Store 虽已增加 expected revision CAS，`RunPreparer` 对不同派生主体的合法并发仍会直接失败。修复为仅对 `RESOURCE_VERSION_CONFLICT` 有界重读并重试三次；同一 owner 仍走幂等复用，其他存储错误不重试。增加模拟竞争主体先提交的回归测试，确认两个 execution 都保留。

### 第 2 轮

从“tenant_id 是存储键且不能产生别名”出发，发现本地 manifest 目录名使用字符替换，理论上不同原始 ID 可映射到同一目录。改为 tenant/run 分别使用固定长度 SHA-256 存储键，测试不再依赖物理目录名字；Enterprise 仍禁止使用该宿主 adapter。

### 第 3 轮

从“Delivery 只能写入已授权目标”出发，发现为 Desktop 创建 Inbox 的共用辅助函数也会创建不存在的项目根。拆分语义：ProjectRoot 必须已存在且是目录，ProductInbox 才允许由产品显式创建；补充“缺失项目根不得产生目录”测试。

### 第 4 轮

从“manifest 是完整授权事实”出发，发现校验只约束 execution owner，没有约束 StateRoot/ProjectRoot scope。现要求 StateRoot、可选 ProjectRoot、所有 execution owner 的 tenant/project/user 与 manifest scope 完全一致，并增加三类错配拒绝测试。

### 第 5 轮

从头复审 Enterprise 生产依赖、Desktop 交付副作用、tenant 查询、CAS、Artifact 失败保留和文档状态。未发现本轮范围内新的可执行问题；Enterprise 具体持久 adapter、L2/L3/Handoff、MCP sandbox bridge、配额/retention/GC 仍是明确残余风险，不标记为完成。

## 2026-07-17 L2/L3 与 Handoff 审查闭环

### 第 1 轮

从“子执行只能继承或收窄既有授权”出发，发现 L3 Coordinator 的运行策略可携带独立 ProjectRoot，从而给成员子 Run 注入父 Run 未持有的项目资源；同时 Handoff 接受非规范空白、反斜杠路径和大小写哈希，会让同一引用产生多个幂等指纹。已删除运行策略中的项目根入口，成员只继承权威父 manifest 的 ProjectRoot/ProjectDir；稳定引用改为拒绝非规范形式，并补相应拒绝测试。

### 第 2 轮

从“adapter 只能映射物理环境，不能改写授权事实”出发，发现 Provisioner 的 binding 不变约束只有注释，没有运行时校验；manifest 也未校验 execution 的 App、parent、session 和主体唯一性。已在 RunPreparer 校验 StateRoot/scope、ProjectRoot/scope、返回 binding 全等和 workspace 结构，并将 App/parent/session/主体唯一性固化为 RunManifest 不变量；错误 fixture 同步改为构造完整可信 owner。

### 第 3 轮

从“编排请求不是授权来源”出发，发现成员请求可用 `HasProject=true` 自报项目存在，空 App ID 还会隐式落到默认 App；Coordinator 的 mode slice 也可能在构造后被调用方修改。已强制显式 Member App ID、始终从父 ProjectRoot 派生 HasProject，并深拷贝运行策略切片；补充伪造项目授权和隐式 App 拒绝测试。

### 第 4 轮

从“持久回执必须能独立证明原子写入结果”出发，发现 Handoff Service 默认信任 Store 返回的 receipt，且测试 Store 未约束同租户 receipt ID 唯一。已对返回值复核 tenant/idempotency/source/target，重算完整 payload 指纹并防御性复制；Store 增加 tenant+receipt ID 唯一索引和冲突测试。

### 第 5 轮

从“跨 execution 的一次授权必须覆盖导出和接收两端”出发，复核 Authorizer 契约、资源 scope、Artifact producer/run/hash、未知 binding、幂等冲突和物理路径泄漏。将端口明确为同时验证 source 对精确版本的导出权与 target 接收权；最终全仓测试、关键包 vet、产品隔离、历史 mode/owner 旁路扫描和 diff 检查均通过。本轮未发现新的可执行阻断项；产品级 Workflow/CollaborationSpace 接入、持久 Handoff Store 和具体双向 Authorizer 仍保留为明确后续，不伪装为完成。

## 验证结果

- `go test ./...`：通过。
- `go vet ./...`：通过。
- `scripts/check_product_isolation.ps1`：通过。
- 旧 WorkspaceMode、旧 `artifacts[].path`、Skill 旧 `stageInputs`、子智能体 `WorkspaceRoot: "."` 静态扫描：零命中。
- `git diff --check`：通过。
- `go test -race`：已尝试；当前 Windows 工具链先因 `CGO_ENABLED=0` 拒绝，显式启用后因 PATH 中无 `gcc` 无法构建，因此本次没有有效 race 结论。

最终收尾复验日期为 2026-07-17；本轮未再扩展功能，仅校正文档状态并重复执行上述验证。

### 收尾后回归修复：ProjectRoot scope 绑定

实际运行发现 CLI/Desktop 在 bootstrap 阶段创建的本地 ProjectRoot 尚不知道 session tenant/user，引用 scope 为空；RunPreparer 的 manifest 全等不变量因此正确拒绝了未绑定引用。修复放在产品控制面：`RunOnce` 在解析出本次 Run scope 后复制项目引用，先拒绝已有 tenant/project/user 冲突，再把副本绑定到精确 Run scope；RunPreparer 和 RunManifest 的全等校验保持不变。L1 子 Run 同步改为继承父 manifest 中已经绑定的 ProjectRoot/ProjectDir，不再读取装配期引用。新增无 scope 绑定、原引用不可变、跨 tenant/project 拒绝测试；全仓测试、相关包 vet、产品隔离和 diff 检查再次通过。
