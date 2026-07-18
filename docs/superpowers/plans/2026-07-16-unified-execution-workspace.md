# 统一执行工作空间实施计划

> 基线文档：`docs/统一执行工作空间、文件权限与产物规范.md`  
> 状态：核心控制面与安全纵切已完成并收尾；产品集成和生产治理进入后续里程碑  
> 原则：当前开发阶段直接清理旧模型，不保留兼容别名；不在未确认时迁移现有目录。

## 实施硬约束

- 不兼容任何旧 WorkspaceMode、旧序列化值、旧配置字段或旧目录语义。
- 不保留 deprecated alias、双读/双写、fallback mapper、feature flag 或“先兼容后删除”的过渡层。
- 新契约落地时同步删除旧常量、旧分支、旧测试和旧文档表述；调用方必须同批迁移。
- 持久化旧值若进入新版本，返回结构化错误并要求重新创建 Run，不做静默转换。
- 每个阶段都必须包含：目标、修改范围、明确删除项、实现任务、验收标准、验证命令。
- 最佳实践与边界清晰优先于减少改动量；但涉及目录迁移或跨产品边界变化时仍按项目规则先确认。

## 目标

把工作空间从“编码/本地/沙箱特例”收敛为三端同源、场景无关的执行契约：

- `project_workspace`、`task_job`、`session_workspace` 只表达资源生命周期；
- backend、sandbox、产品端与 WorkspaceMode 正交；
- 每个执行主体消费可信 `ExecutionBinding`，模型参数不能伪造 owner/root/mode；
- 输入、工作状态、Artifact 与最终 DeliveryTarget 分层；
- CLI、Desktop、Enterprise 共享内核契约，只注入后端、策略和存储。

## 本阶段收尾结论（2026-07-17）

本计划所要求的通用核心已经完成：三种场景无关 WorkspaceMode、可信 ExecutionBinding、RunManifest/StateRoot、Input staging、Artifact/DeliveryTarget、Agent App 裁剪、L1/L2/L3 默认隔离和显式 Handoff 均已有通用契约、控制面实现与测试。旧模式、旧路径推断和旧产物旁路已直接删除，不保留兼容逻辑。

本阶段正式收尾，不继续把产品化和外部基础设施工作混入本次改造。后续里程碑只包含两类工作：

1. 依赖外部基础设施的 Enterprise 多租户 Store、对象存储、下载入口、配额、retention、GC 和审计；
2. 基于现有端口的产品编排/UI 集成，包括 Workflow/CollaborationSpace 调用、只读资源投影、受锁共享写、MCP sandbox/file bridge 和 Desktop 预览。

这些后续项不改变本轮已经冻结的 WorkspaceMode、ExecutionBinding、ResourceRef、ArtifactRef 和 Handoff 核心契约。

## 实施前基线审计（历史快照，非当前状态）

| 设计点 | 当前证据 | 状态 | 主要缺口 |
| --- | --- | --- | --- |
| ExecutionWorkspace 与 path map | `internal/capabilities/execution/model/model.go`、`service/runner.go` | 部分实现 | 只有物理目录字段，没有 binding identity/owner；注释仍绑定“代码执行” |
| WorkspaceMode 场景无关 | `model.go` 的 `local_coding_workspace/local_task_workspace/sandbox_session_workspace` | 偏离 | mode 同时编码业务、backend 和 sandbox，无法三环境等价映射 |
| 路径策略解析 | `execution/pathcontract/validator.go` | 部分实现 | 直接根据旧 mode 和 provider 字符串推断，缺少 binding 校验 |
| Skill 工作空间 | `skill/script/workspace`、`skill/script/service/service.go` | 部分实现 | Skill 强设 local task / sandbox session，input/output/work 仍有重叠 |
| 远程 sandbox 映射 | `sandbox/session/options.go`、`sandbox/adapter/http/client.go` | 部分实现 | adapter 会改写业务 mode，违反 backend 正交原则 |
| InputRef / Artifact | execution 与 skill 中已有若干传输结构 | 部分实现 | 模型分散、缺少统一 manifest/store/publisher 与显式发布闭环 |
| DeliveryTarget | 无统一契约 | 未实现 | 现有产物主要落 Run output；缺少“目标同级 → 项目根”的用户可见交付策略 |
| Agent App / WorkspaceResolver | `internal/capabilities/agentapp` 尚不存在 | 未实现 | 无 EffectiveAgentAppProfile、候选选择和安全裁剪链 |
| L1/L2/L3 binding 隔离 | 子智能体已有 Run/结果安全基础 | 部分实现 | 尚无 per-execution binding 和受治理资源投影 |

## 分阶段任务

### Phase A：冻结并落地执行契约（已完成）

**目标：** 建立场景无关、backend 无关、可验证且不可伪造的执行绑定，作为后续所有阶段的唯一入口。

**修改范围：**

- `internal/capabilities/execution/model/model.go`
- `internal/capabilities/execution/contract/runner.go`
- `internal/capabilities/execution/pathcontract/*`
- `internal/capabilities/sandbox/session/*`
- `internal/capabilities/sandbox/adapter/http/*`
- `internal/capabilities/skill/script/workspace/*`
- `internal/capabilities/skill/script/service/*`

**任务：**

- [x] 将旧 mode 直接替换为 project/task/session；不保留 alias。
- [x] 增加 `ExecutionOwnerRef`、`ExecutionBinding`、访问姿态及结构校验。
- [x] `RunOptions` 明确携带 binding；ExecutionWorkspace 只承载实际路径映射。
- [x] path policy 按 mode 解析，不再按 local/sandbox mode 名推断。
- [x] sandbox adapter 只映射物理目录，不篡改业务 mode。
- [x] Skill 本地/远程选择 session 语义，不编码 backend。
- [x] 补 model、pathcontract、sandbox、skill 迁移测试。

**明确删除：**

- `WorkspaceModeLocalCoding`、`WorkspaceModeLocalTask`、`WorkspaceModeSandboxSess` 及三个旧字符串；
- sandbox adapter 修改业务 mode 的逻辑；
- Skill 用 local/sandbox 判断工作空间语义的分支；
- ExecutionRunner 在缺少 binding/workspace 时回退进程 cwd 的逻辑。

**验收：** 三个 mode 均可映射到 local/sandbox；旧字符串在 Go 代码中归零；缺少或冲突 binding 返回稳定错误；相关包测试通过。

**验证：** execution/sandbox/skill script 包测试；`rg` 确认旧常量与旧字符串为零。

**完成记录（2026-07-16）：** 目标包测试与 `go test ./...` 通过；旧常量、旧字符串和旧准备函数引用归零；`git diff --check` 通过。产品隔离脚本仍被实施前已存在的 Enterprise → `shared/local` 依赖阻断，该问题纳入 Phase F，不以兼容层规避。

### Phase B：StateRoot、Input 与工作目录隔离

**目标：** 让 Run 根、输入快照和 execution work namespace 稳定、可审计、并发隔离。

- [x] 实现 StateRootResolver port、本地 resolver 与远程 executor workspace adapter。
- [x] Run manifest 固化 state root、binding、owner、path map 和 backend；本地按 tenant/run 排他持久化；Enterprise 改为必须注入外部租户 Store，未配置即 fail-closed，不再使用进程内 adapter。
- [x] 统一 InputRef/InputStager：hash、重名、只读视图、版本冲突；CLI Skill 已接入。
- [x] `work/<execution_binding_id>` 隔离根 Run 与 Skill execution。
- [x] 清理 Skill 裸路径 staging、input/output/work 同目录和 basename 覆盖逻辑。
- [x] `RunManifest v3` 固化根 execution 的 `InputManifest + WorkspaceViewManifest`；请求中的精确已有文件在 LLM 启动前完成版本冻结、不可变快照和同路径工作副本投影。
- [x] 所有本地文件工具的裸相对路径只按当前 PreparedRun WorkDir 解析，删除 bootstrap 项目根回退；Skill 自动收集 Run bound inputs 与 command 入口脚本。

**当前剩余：** Enterprise 持久化租户 RunManifest/InputStore 的具体数据库/对象存储 adapter；Workflow/CollaborationSpace 产品编排接入。L2/L3 通用 binding 控制面已经完成；`RunManifestStore` 已要求 tenant_id 查询与 expected revision CAS，部署实现必须满足该契约。

**2026-07-17 阶段记录：** 删除 Enterprise 生产内存 Store 默认；容器必须注入租户 RunManifestStore。Get/AddExecution 强制 tenant_id，追加使用 expected revision CAS；RunPreparer 对版本冲突进行最多三次有界重读重试。Manifest 同时校验 StateRoot、ProjectRoot、execution owner 与顶层 scope 一致。

**2026-07-18 阶段记录：** 修复文件工具静态项目根与 Skill 当前 WorkDir 的 split-brain。模型只看 `root="."`、mode/access/persistence 和 WorkspaceView 相对别名；物理 cwd 不进入提示。`project_workspace` 增加 RunStart Git 基线增量门禁；remote optional/required 不再创建 Host 降级 attempt。显式 `SandboxDisabled` 仍可使用受审计 Host direct，此时隔离工作目录是路径命名空间而非 OS 安全边界。

远程 Skill session 改为 Run 生命周期资源：Run 内复用，Run 任意终态立即关闭；10 分钟 idle TTL 只保留为异常退出兜底，避免连续对话因旧 session 占用租户内存配额而产生重复失败。

**明确删除：** 默认当前进程 cwd 或 bootstrap 项目根的路径回退；不同 execution 共享可写 work；无条件 `filepath.Base` 扁平化输入；模型提交宿主绝对输入；remote optional 的 Host 执行降级；只靠约定实现只读输入。

**验收：** 改变 cwd/UI 目录不迁移既有 Run；并发 execution 不共享可写目录；本地/远程 Input manifest 一致。

**验证：** StateRoot 表驱动测试、输入重名/链接越界/版本漂移测试、并发 race 测试、local/remote contract test。

### Phase C：Artifact 与 DeliveryTarget

**目标：** 建立正式交付实体和用户可见落点，彻底分离内部输出、持久 Artifact 与最终导出位置。

- [x] 建立统一 ArtifactRef、manifest、结构 gate、ArtifactStore、Publisher。
- [x] 发布与 materialize 两阶段提交，Run 清理不删除 Artifact。
- [x] 实现 DeliveryTargetResolver：显式路径 → 目标文档平级 → 项目根 → 产品可见输出根。
- [x] 实现拒绝覆盖、原子导出与 Artifact 保留；禁止向用户暴露内部 runs 目录。
- [x] CLI `publish_artifact` 返回实际用户路径。
- [x] Desktop `publish_artifact` 投影到用户级 ArtifactStore 与可见 Inbox；无项目时不暴露内部 runs 目录。
- [x] Enterprise bootstrap 强制注入租户 Artifact publisher/source/delivery 并注册 `publish_artifact`；缺少任一端口即 fail-closed。

**当前剩余：** 目标 expected-version/Freshness 条件替换；Desktop Wails Artifact 卡片/预览；Enterprise 对象存储、WorkspaceFS source 与租户下载入口的具体 adapter。

**2026-07-17 阶段记录：** Desktop 注册正式 `publish_artifact`，Artifact 进入用户级 Store 后投影到 `~/Genesis Agent/Inbox`；项目根只允许已存在目录，不能被交付构造器隐式创建。Enterprise 容器要求外部 publisher/source/delivery 三项完整注入，禁止用实例本地文件兜底。

**明确删除：** 全目录 diff 自动交付、basename 扁平化、把 runs/output 当最终用户目录、成功后要求用户寻找内部路径、导出失败即丢失正式 Artifact。

**验收：** PDF→Markdown、Markdown→PPT 默认交付到目标平级；平级不可用回退项目根；导出失败仍可访问 ArtifactRef。

**验证：** publisher 两阶段提交、同名冲突/Freshness、源平级/项目根回退、Run 清理后 Artifact 可读、三产品投影测试。

### Phase D：Agent App 与 WorkspaceResolver

**目标：** 让 App 只声明需求与偏好，由统一 resolver 在可信控制面完成模式选择和安全裁剪。

- [x] 在获得目录变更确认后建立 `agentapp` 能力域。
- [x] 实现 EffectiveProfile Resolver 与内置并发安全 registry；请求只选择 App ID，不能提交权限字段。
- [x] 实现 workspace 候选选择与安全裁剪；App Type 不参与权限判断。
- [x] CLI/Desktop/Enterprise 统一注入产品侧 EffectiveProfile、允许模式、state root、provisioner 与 manifest store，不复制 resolver。

**当前剩余：** Enterprise DB/租户策略、Desktop/CLI 配置文件等持久 Source，以及多层配置合并；统一 resolver 端口、App ID 选择、会话作用域校验和三端内置 registry 已完成。

**明确删除：** `AgentAppType=code` 推导 project mode、产品层自行拼工作目录、模型参数直接选择更弱 mode、App 切换沿用旧 binding。

**验收：** code 与非 code App 均可运行三种 mode；App 切换不复用旧 binding；无明确项目时不落进程 cwd。

**验证：** resolver 决策表、显式选择冲突、App 切换、三端相同输入得到相同语义 binding 的契约测试。

### Phase E：Skill、MCP 与多智能体资源闭环（核心已完成，产品集成待后续）

**目标：** 所有扩展能力和多执行主体复用统一资源绑定，不再拥有文件旁路。

- [x] Skill Runtime Harness 接统一 binding 与 InputStager；`produced` 仅返回候选，Artifact 由独立 Publisher 显式发布。
- [x] MCP Placement 显式化、stdio 环境白名单、FileBinding 按 schema/annotation 发现；不再按字段名猜路径。
- [x] L1 子 Run强制创建独立 `task_job` Run/binding/manifest，并保留 parent Run 关联。
- [x] L2 Workflow step、L3 CollaborationSpace member 默认独立 binding（通用控制面已完成；产品编排接入另行实施）。
- [x] 同一 Run 的 Hook 与 Skill command 通过统一 ExecutionPreparer 创建/复用派生 binding，并追加不可变 manifest revision。
- [x] Skill 依赖安装使用受治理 project execution，并验证 package cache 的真实路径未越过控制面项目根。
- [x] 实现 Artifact/Handoff 稳定引用交接、双向重新鉴权契约和幂等 receipt。
- [ ] 实现只读资源投影和受锁项目写两种共享方式。

**当前剩余：** sandbox MCP launcher 与 ResourceRef 上传/重写；多智能体只读投影、受锁共享写、产品编排接入与持久 Handoff Store。

**明确删除：** Skill/MCP 自己 staging、stdio 裸启动、父子默认共享可写 cwd、Workflow 目录 diff 归属、跨 App 传宿主绝对路径。

**验收：** 并行执行无 cwd 竞态；跨成员访问重新鉴权；父子结果只返回安全摘要与稳定资源引用。

**验证：** Skill 原包不可变、MCP Placement/FileBinding、子 Run/并行 step 隔离、跨成员拒绝、Artifact/Handoff 重新鉴权测试。

### Phase F：三产品治理与清理

**目标：** 完成生产治理、三端适配与所有历史旁路清理，使规范成为唯一事实来源。

- [/] Enterprise 租户 WorkspaceFS/InputStore/ArtifactStore：bootstrap 持久化端口与 fail-closed 已完成，具体远端 adapter、配额、retention、GC 未完成。
- [ ] 审计字段覆盖 App/version、binding/owner、parent/child、delivery。
- [/] 旧路径模型和旧产物回收旁路已删除；跨仓库重复文档规则的长期归并留在文档治理阶段。
- [x] 删除 Enterprise 服务器本地执行回退并通过 `scripts/check_product_isolation.ps1`。
- [x] 运行全量测试；核心能力 race 命令已执行，但当前 Windows 环境缺少 CGO/gcc，未形成有效 race 结果。

**当前剩余：** Enterprise 租户存储和 Artifact 投影、全链路审计字段、retention/GC；在这些完成前 Phase F 不标记完成。

**明确删除：** Enterprise Local Backend 回退、实例本地租户文件、重复 workspace/artifact 实现、旧配置样例、旧文档双轨规则和迁移 feature flag。

**验收：** tenant 隔离、required sandbox、配额/retention/GC、审计关联键、三产品隔离全部闭环；仓库中不存在旧契约引用。

**验证：** `go test ./...`、race/集成测试、产品隔离脚本、Enterprise tenant 安全用例、文档与代码最终对齐审计。

## 本轮验证命令

```powershell
$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'
$env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'
go test ./internal/capabilities/execution/... ./internal/capabilities/sandbox/... ./internal/capabilities/skill/script/...
go test ./...
.\scripts\check_product_isolation.ps1
```

## 2026-07-17：L2/L3 与 Handoff 资源隔离阶段计划（已完成）

### 阶段判断

仓库当前没有 PostgreSQL、Redis、对象存储驱动或统一 Enterprise connection pool；genesis-sandbox `FileSystemClient` 是 session 生命周期接口，不能冒充租户持久化 Store。因此本阶段不实现伪 Enterprise adapter，优先完成不依赖外部基础设施、且属于规范高风险项的 L2/L3/Handoff 统一资源边界。

### Stage 1：执行主体契约（已完成）

**目标：** 让 Workflow step 与 CollaborationSpace member 都只能通过可信控制面生成 binding。

**修改范围：** `workspace/contract|service`、`execution/model`、`runtime/multiagent`。

**任务：**

- [x] `PrepareRunRequest` 增加只承载编排主体身份的受信 subject，不允许覆盖 tenant/run/App；
- [x] L2 step 在当前 Run 内使用独立 `WorkflowStepID` execution；
- [x] L3 member 使用自己的 EffectiveAgentAppProfile 创建独立 Run，并固化 `CollaborationSpaceID/MemberID`；
- [x] 串行与并行 step 都不复用其他 step 的可写 work namespace。

**删除项：** 通过共享 session/cwd 表达 Workflow step 或成员身份的旁路；根据 App 类型推导工作空间。

**验收：** 相同 step 幂等复用，不同主体 binding/workdir 不同；L3 的每次成员执行创建独立子 Run（同一成员允许多轮执行，不把 `MemberID` 错当请求幂等键），App 快照与发起者解耦；跨 tenant scope 直接拒绝。

### Stage 2：显式 Handoff（已完成）

**目标：** 只用 ResourceRef/ArtifactRef 在两个权威 binding 之间交接，并对接收方重新鉴权。

**修改范围：** `runtime/multiagent/handoff` 的 model/contract/service/adapter test implementation。

**任务：**

- [x] Handoff 请求只接受 tenant/run/binding ID 和稳定资源引用，不接受物理 cwd、宿主路径或 credential；
- [x] 从 RunManifestStore 解析 source/target binding，禁止信任调用方构造的 owner；
- [x] 每个 ResourceRef/ArtifactRef 校验 scope、版本/hash，并要求 source 导出与 target 接收双向授权；
- [x] 生成不可变、排他创建的 Handoff receipt；重复 idempotency key 返回既有等价记录，冲突请求失败。

**删除项：** 跨成员裸路径复制、仅凭 run_id 读取 binding、未经重新鉴权的 Artifact 透传。

**验收：** 跨租户、未知 binding、scope 不符、空版本 ResourceRef、空 hash Artifact、幂等键载荷冲突全部拒绝；成功 receipt 不含物理路径。

### Stage 3：验证与对齐（已完成）

**目标：** 证明新契约接入现有 RunPreparer，且不破坏三产品和 L1。

**任务：** 单元/并发/隔离测试、全仓测试、关键包 vet、产品隔离、旧逻辑静态扫描、更新对齐报告和 `review-fix-rereview` 均已完成。

**验收命令：**

```powershell
$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'
$env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'
go test ./internal/capabilities/workspace/... ./internal/runtime/multiagent/...
go test ./...
go vet ./internal/capabilities/workspace/... ./internal/runtime/multiagent/...
.\scripts\check_product_isolation.ps1
git diff --check
```
