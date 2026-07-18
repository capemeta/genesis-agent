# CLI、Desktop、Enterprise 三端统一设计

> 状态：设计草案  
> 适用范围：Genesis Agent 三端产品定位、本地与云端边界、身份连接、资产模型、执行路由和记忆归属  
> 相关文档：`docs/产品分发架构设计.md`、`docs/项目目录与边界说明.md`、`docs/agent loop设计.md`、`docs/统一配置权限与审批治理设计.md`、`docs/人工干预机制设计与最佳实践修正.md`、`docs/文件系统设计方案.md`、`docs/capability-package-marketplace-plugin-design.md`

## 1. 设计结论

Genesis Agent 采用“一个共享内核、三条产品线、一个可选云端平台”的总体设计：

- CLI 是 local-first 的开发者工具，无需登录即可完整使用；登录云端后增加已授权的云端能力。
- Desktop 是 local-first 的个人 Agent OS，始终拥有 Local Profile；默认单 Profile 自动进入，不要求本地账号登录，用户可以按需启用 Profile Lock 或连接云端账号。
- Enterprise Web 是 Genesis Cloud 上的企业工作空间和治理产品，不是 CLI/Desktop 本地运行的必选后台。
- Genesis Cloud Platform 提供云端身份、资产注册、授权、运行和治理能力，与 Enterprise Web 的产品界面分离。
- 云端连接只增加能力，不改变本地资产、记忆和凭据的所有权，也不触发隐式上传或合并。
- 资产定义、部署、授权和运行相互分离；资产从哪里创建不能直接决定它永远在哪里运行。
- Agent、Workflow 和 Cloud Tool 默认通过 Cloud Deployment 调用；MCP 根据 Connection 选择本地或远程；Skill Package 可以显式安装到本地，但 Skill 本身不直接执行。
- 云端连接不自动下载可执行内容。本地安装、克隆、发布和同步是四种不同的显式操作。
- Desktop 无需云账号也必须支持加密备份、恢复和换机迁移；Cloud Account 只增加可选的端到端加密云备份。
- 记忆存储由创建时确定的 MemorySpace 和 MemoryBinding 决定。Agent 只能使用只读 MemoryView，不能修改 Backend、Scope 或同步策略。
- 本地与云端执行统一经过 Policy、Approval、Intervention、Audit 和资源锁；后台任务无法交互确认时必须暂停或失败关闭，不能自动批准。
- Desktop 必须提供本地活动时间线、待确认事项和紧急停止入口，让个人用户能够看见、控制和追溯 Agent 的真实操作。
- 发布者信任、包签名和来源证明只能降低安装决策的认知成本，不能替代运行时权限、沙箱或人工确认。
- 三端统一语义但不强求相同交互；系统使用稳定默认 Deployment 并保持本机/云端边界可见，只在数据、费用、权限或运行位置变化时打断用户。
- CLI/Desktop 本地资产默认隔离，但提供有来源记录的一次性受控复制，降低搬运摩擦而不建立隐式同步。
- Enterprise 治理同时提供 Web 与 Management API/GitOps；企业数据通过独立 Managed Data Domain 收紧策略，不能借此读取或擦除个人 Local Profile。

核心原则是：

> 三端统一的是内核、协议和领域语义；产品身份、部署位置、数据空间和权限边界保持独立。

## 2. 产品与平台边界

### 2.1 产品定位

| 产品 | 产品定位 | 本地空间 | 云端连接 | 默认运行位置 | 核心价值 |
|---|---|---|---|---|---|
| CLI | 面向开发者的 AI 编程工具 | 自动使用本地用户环境和当前 Workspace | 可选 | 本地 | 代码理解与修改、本地工具调用、项目自动化 |
| Desktop | 个人 Agent OS 与长期助手 | 默认 Local Profile 自动进入；加锁或多 Profile 时选择/解锁 | 可选 | 本地 | 长期助手、桌面能力、主动任务和多 Agent 协作 |
| Enterprise Web | 企业级 Agent 工作空间与治理产品 | 无 | 必须 | 云端 | 企业 Agent 使用与创建、团队协作、资产治理和审计 |

### 2.2 Genesis Cloud Platform

Genesis Cloud Platform 是可被多个产品使用的云端能力平台，主要提供：

- Cloud Identity、组织、租户和成员关系；
- Agent、Skill、Tool、MCP、Workflow 等云端资产 Registry；
- Deployment、Grant、Credential、Connection 和 Policy；
- Cloud Agent Runtime、Workflow Runtime 和 Sandbox Gateway；
- Cloud Memory、企业知识库、Trace、Audit 和 Usage。

Enterprise Web 是 Genesis Cloud Platform 的企业工作空间和管理入口，但二者不是同一个概念：

- Cloud Platform 是平台能力；
- Enterprise Web 是面向企业用户和管理员的产品；
- Desktop/CLI 可以连接 Cloud Platform，而不依赖 Enterprise Web 前端；
- 一个 Cloud Account 可以拥有零个或多个租户、团队成员关系。
- 个人、小团队和家庭协作可以复用 Cloud Workspace、成员关系与 Grant 做产品包装，不新增“本地共享账号”或另一套团队架构，也不要求使用 Enterprise Web 前端。

### 2.3 三端职责

#### CLI

- 未连接云端时，使用本地 Agent、Skill、Tool、MCP、Workflow、项目上下文和本地记忆。
- 连接云端后，可以发现和调用当前 Cloud Account 已获授权的云端 Deployment。
- 本地项目文件、命令输出、项目上下文和项目记忆默认不上传。
- 不承担租户、成员、RBAC、企业审计等治理职责。

#### Desktop

- 始终在 Local Profile 中运行，本地 Runtime 不依赖 Cloud Identity 和 Cloud API。
- 首次启动自动创建默认 Local Profile；单 Profile 且未加锁时直接进入，不显示本地登录页。
- Local Profile 拥有独立的 Agent、记忆、凭据、任务、运行记录和产品设置。
- Profile Lock 是可选的本地解锁机制，不是账号体系，也不用于获取云端权限。
- 可以连接 Cloud Account，并在本地能力之外使用已授权的云端 Deployment。
- 重点承载长期助手、定时或事件触发任务、多 Agent 编排、桌面原生集成和本地隐私数据处理。
- 断开云端连接不删除、不转移也不改变 Local Profile 中的数据。

#### Enterprise Web

面向普通用户提供：

- 云端 Agent 的使用、创建和编排；
- 团队工作空间和多 Agent 协作；
- 企业知识库、云端记忆、Workflow 和运行记录。

面向管理员提供：

- 组织、租户、成员、角色和权限管理；
- 云端资产的注册、发布、授权、版本和下线管理；
- Credential、Connection、Deployment、运行环境与沙箱策略管理；
- Audit、Trace、Usage、配额和安全策略管理。

Enterprise Web 不读取或直接操作 CLI/Desktop 的个人本地数据，也不直接访问用户宿主机文件系统。Enterprise Managed 只通过 Cloud Policy、MDM 或登记后的 Device Gateway 管理明确标记的 Managed Data Domain。

### 2.4 统一语义，不强求同形交互

三端统一的是资产身份、运行位置、权限状态、数据去向、错误语义和审计关联，不是界面形态：

- CLI 优先键盘、管道、脚本、稳定退出码和机器可读输出；Desktop 优先长期状态、后台任务、通知和可视化控制；Enterprise Web 优先团队协作、批量治理和审计。
- 不为“看起来一致”把 GUI 流程照搬到 CLI，也不把脚本参数原样暴露给 Desktop 普通用户。相同动作使用相同领域语义和可追踪 ID，各端采用符合自身场景的交互。
- 本地/云端差异不能隐藏，因为它影响数据、延迟、费用与可用性；但也不能要求用户每次手动路由。系统应使用可解释的稳定默认 Deployment，常驻展示“本机/云端”状态，只在运行位置、数据出境、费用或权限边界发生变化时打断确认。
- 默认路径只展示任务、结果和必要风险；Deployment ID、Grant、Digest、MemoryBinding 等进入高级详情。用户主动进入高级模式后才承担精细路由和治理心智。
- 多 Profile、来源不可信、恢复冲突、跨端复制等分叉点必须用人话解释“为什么需要选择、选择后会发生什么”，不能只暴露内部模型名或错误码。

### 2.5 企业治理自动化接口

Enterprise Web 不是唯一治理入口。Genesis Cloud Platform 必须提供与 Web 共用应用服务和权限模型的 Management API，并按需要提供企业管理 CLI、IaC Provider 或 GitOps Adapter：

- Grant、Policy、Deployment、Connection 元数据、配额和组织配置使用版本化声明 Schema；Secret 只允许使用 CredentialRef，不进入 Git 或变更计划。
- 支持 `validate/plan/apply`、幂等请求、预期版本、差异预览、策略检查、审批、审计和失败回滚，避免批量治理退化成不可追踪脚本。
- 声明式管理必须定义权威来源和漂移策略：由 GitOps 托管的字段在 Web 中只读或明确产生受审计的 override，不能让 ClickOps 与配置文件互相覆盖。
- 人员登录使用 Cloud Account，CI 治理使用 Workload Identity/Service Principal；二者调用同一授权闭环，不提供绕过 Web 审批和审计的“超级 API Key”。
- 管理 CLI/API 是 Cloud Platform 的控制面触点，不是第四条终端产品线，也不把租户治理职责塞入面向本地开发的普通 CLI Runtime。

## 3. 本地 Profile 与云端身份

### 3.1 正交身份模型

本地数据空间与云端身份是两个正交概念，不设计成相互替代的“两种账号”：

```text
LocalProfile
  ├── Local Identity / Unlock Policy
  ├── Local Assets
  ├── Local Memory
  ├── Local Credentials
  ├── Local Runs / Tasks
  └── Cloud Connections（可选）
        ├── Cloud Account
        ├── Tenant Memberships
        └── Authorized Deployments
```

| 场景 | 本地上下文 | 云端身份 | 可用能力 |
|---|---|---|---|
| CLI 未连接云端 | 本地用户 + 当前 Workspace | 无 | 本地能力 |
| CLI 已连接云端 | 本地用户 + 当前 Workspace | Cloud Account | 本地能力 + 云端授权能力 |
| CLI CI/Headless | 当前 Workspace 或无交互本地上下文 | 可选 Workload Identity / Service Principal | 本地自动化 + 已授权云端能力 |
| Desktop 纯本地使用 | Local Profile | 无 | 当前 Profile 的全部本地能力 |
| Desktop 已连接云端 | Local Profile | Cloud Account | 当前 Profile 的本地能力 + 云端授权能力 |
| Enterprise Web | 无本地上下文 | Cloud Account | 当前租户、团队和用户范围内的云端能力 |

### 3.2 Desktop Profile

Desktop 必须拥有 Local Profile，但不要求用户注册或登录本地账号：

- 首次启动自动创建默认 Local Profile；只有一个 Profile 且未启用 Profile Lock 时，后续启动自动进入。
- 存在多个 Profile 时要求选择；启用 Profile Lock 时要求完成本机解锁。
- 创建、选择、解锁和使用 Local Profile 均不请求 Genesis Cloud。
- Local Profile ID 在本机生成，不上传云端用于静默匹配身份。
- 不同 Profile 的资产、记忆、凭据和运行记录相互隔离。
- Profile Lock 优先使用 Windows Hello、Touch ID、系统凭据验证和操作系统安全存储；如支持独立密码或 PIN，必须明确数据是否加密以及忘记凭据后的恢复边界。
- Profile Lock 只有在真正解锁 Credential Vault 或数据加密密钥时才构成强安全边界；仅隐藏 UI 不能宣称保护磁盘数据。
- 共享本机模型文件或非敏感缓存时，仍不得突破 Profile 数据边界。

“连接云端账号”是 Local Profile 中的可选设置，不是另一套本地数据空间：

- 云端不可用只影响云端资产发现和调用，本地功能继续工作。
- Cloud Token 和云端受控缓存保存在操作系统安全存储或等价机制中。
- 断开连接时清理 Token 和必须清理的受控缓存，不自动删除 Local Profile 数据。
- 一个 Profile 连接多个 Cloud Account 时，每次云端调用必须绑定明确的 Connection，不得静默选择身份。

### 3.3 数据所有权与治理归属

- 个人模式下的 Local Profile 数据始终归属于该 Profile，不因连接 Cloud Account 而改变归属。
- Cloud Account 数据始终归属于对应云端用户、租户或团队空间。
- 物理存储位置与治理归属不是同一概念。Enterprise Managed 中允许落地本机的租户数据仍属于 Tenant 治理域，必须标记为 Managed Data Domain，不能因为存入设备就混入个人数据或获得个人数据的导出、备份规则。
- 不存在“本地账号升级成云端账号”的隐式模型。
- 本地数据进入云端必须通过显式发布、导入或复制流程。
- 云端数据进入本地必须遵守下载、缓存、离线和企业数据防泄漏策略。
- 个人数据和 Managed Data Domain 在存储、密钥、Retention、导出、备份、审计与擦除策略上保持隔离；企业策略不能借管理租户数据之名读取或擦除不属于企业的个人 Local Profile。

### 3.4 多用户边界

Desktop 的默认安全模型是“一个操作系统账号对应一个可信操作者”，不提供应用内多租户或多人并发使用：

- 多个 Local Profile 用于同一操作者的工作、生活、客户或实验环境隔离，不代表多个真人用户或企业租户。
- 不同真人使用同一设备时，应使用不同的 Windows/macOS/Linux 操作系统账号，由操作系统隔离应用数据目录、进程和安全存储。
- 不支持多个真人在同一操作系统账号下共享 Local Runtime、Gateway、Credential Vault 或长期记忆。
- Profile Lock 可以防止误打开并保护静态密钥，但不能把同一操作系统账号可靠地升级为强隔离的多人平台。
- Desktop 不承担团队成员、角色、RBAC、共享工作空间和多人审计；这些能力属于 Genesis Cloud 与 Enterprise Web。
- Cloud Account 连接只表示当前可信操作者获得云端身份，不把 Desktop 本地数据空间变成多人共享空间。

如果未来确有共享设备或多人并发需求，应作为独立的 Managed Desktop 方案重新设计操作系统级身份映射、会话隔离、数据库行级权限、独立 Gateway 和审计，不能复用多 Profile 冒充多用户。

### 3.5 Desktop 产品策略

Desktop 共享同一套 Local Profile 和 Local Runtime 模型，通过产品级策略选择发行形态，不实现多套账号系统：

| 产品策略 | Local Profile | Profile Lock | Cloud Connection | 适用场景 |
|---|---|---|---|---|
| Personal Default | 自动创建并进入 | 用户可选 | 用户可选 | 默认个人产品 |
| Privacy Enhanced | 自动创建，进入前解锁 | 强制 | 用户可选 | 高隐私个人环境 |
| Enterprise Managed | 保留 Local Profile Core；个人空间可禁用，受管数据域独立隔离 | 企业策略决定 | 强制 | 企业受管设备，不代表应用内多用户；设备清点与策略下发优先由 OS/MDM 负责 |

发行装配进一步分为：

- Desktop Standalone 不注册 Genesis Cloud Adapter，不显示云端入口，也不在启动时探测、重试或请求 Genesis Cloud。
- Desktop Connected 注册可选 Cloud Adapter；未连接时的本地行为与 Standalone 完全一致。
- Enterprise Managed 可以在产品 bootstrap 中要求 Cloud Connection，但不能让该策略反向成为 Desktop Core、Local Profile 或 Local Runtime 的依赖。

#### Enterprise Managed 数据控制

Enterprise Managed 可以收紧企业数据的默认策略，但控制范围必须绑定受管租户、Managed Data Domain 和已登记设备，不能模糊扩大到整台设备上的个人数据：

- 按数据分类和 Deployment 规定“允许本地持久化、仅允许加密短期缓存、禁止本地持久化/仅云端处理”，并控制离线时长、Cache TTL、备份、导出、剪贴板、外部目录和网络出口。
- “仅云端处理”表示 Desktop 不创建持久化业务副本，只保留运行所需的最小元数据；不可避免的内存或临时缓冲必须加密、限时并在 Run 结束后清理。若合规要求连受控临时数据也不得进入终端，应使用 Enterprise Web/Cloud Runtime，而不是作出“桌面端零落地”的虚假承诺。
- 所有允许落地的受管数据强制静态加密，密钥与 Device/Managed Workspace 绑定；设备不合规、成员离职或授权撤销时，先撤销 Cloud Token、Credential 和 Deployment Grant，再通过 MDM 或已登记 Device Gateway 执行受审计的密钥销毁或受管数据擦除。
- Genesis Cloud 不得直接静默擦除个人 Local Profile。整机擦除由 OS/MDM 承担；Genesis 只可擦除明确登记的 Managed Data Domain，并必须具备设备身份、命令签名、目标预览、幂等执行、结果回执和审计。
- 受管策略采用签名快照并声明有效期、离线宽限期和失效行为。策略过期后，涉及企业数据的能力按风险失败关闭；个人本地能力仍按个人产品边界工作，除非该发行版明确禁止个人空间。
- 用户始终能看见设备是否受管、哪些数据受企业控制、哪些动作被禁止、最后策略更新时间以及擦除/保留规则。

Profile Lock 和 Cloud Connection 都是产品策略或用户设置，不允许 Agent、Skill、Workflow 在运行时开启、关闭或绕过。

### 3.6 云端主体与非交互认证

云端身份不能只建模为交互式登录用户。不同主体必须使用不同凭据和审计语义：

| 主体 | 适用场景 | 推荐认证方式 |
|---|---|---|
| Cloud Account | CLI/Desktop 交互式个人使用、Enterprise Web | OAuth/OIDC 用户授权与短期 Token |
| Workload Identity / Service Principal | CI/CD、定时集成、无界面自动化 | 优先使用 OIDC Workload Identity Federation；其次使用 OAuth2 Client Credentials |
| Device Identity | Enterprise Managed 设备服务、未来 Device Gateway | 显式设备注册、设备密钥与证明，不复用 Local Profile ID |

- 纯本地 CLI 自动化不要求任何云端身份；只有调用 Cloud Deployment 时才需要非交互云端主体。
- 长期 API Key 或 Personal Access Token 仅作为受限兼容方案；必须限制租户、项目、Deployment、动作、有效期和来源环境，并支持轮换、撤销与审计。
- CI Secret 必须由 CI 密钥库、Credential Resolver 或环境变量引用注入，不得写入仓库、项目配置、日志或资产包。
- Headless 环境不能弹出交互式审批。策略结果为 `ask` 时，只有已配置持久化审批代理且能安全等待时才进入暂停；否则返回结构化错误并失败关闭，绝不自动批准。
- Enterprise Managed 的设备清点和策略下发优先由 Intune、Jamf 等 OS/MDM 能力承担，不需要上传 Local Profile ID。只有 Genesis 设备服务确有需求时，才创建独立的 Device Registration；设备身份与操作者、Local Profile 和 Cloud Account 保持正交。

## 4. 统一资产模型

### 4.1 五个核心概念

| 概念 | 回答的问题 | 说明 |
|---|---|---|
| AssetDefinition | 它是什么 | 资产内容、类型、版本和来源证明 |
| Installation | 哪些不可变内容已物化到当前产品 | 安装记录、内容摘要、来源、启用状态和本地物化位置 |
| Deployment | 它在哪里、以什么条件运行 | Runtime、Endpoint、Credential、Policy、Sandbox 等绑定 |
| Grant | 谁可以做什么 | 发现、调用、管理、发布等权限 |
| AssetRef | 如何稳定引用 | Registry、Asset ID、Version 或版本约束 |

Installation 与 Deployment 不能混为一谈：Skill Package 可以安装到本地，但 Skill 本身仍不是可执行原语；Tool/MCP Package 安装到本地后，还需要创建可用的 Local Deployment 或 Connection。

资产的创建端、注册位置、安装位置、部署位置和访问端不能互相推导。例如，在 Enterprise Web 创建的 Agent Definition 默认可以创建 Cloud Deployment，但 Definition 是否允许导出或建立 Local Deployment，由发布策略决定。

### 4.2 通用外壳与类型专属定义

不同资产共享最小公共元数据，但业务字段保留在各自的 Spec 中：

```yaml
asset:
  id: asset-id
  kind: agent
  version: 1.0.0
  registry: cloud:tenant-id
  provenance:
    created_by: principal-id
    created_at: 2026-07-17T00:00:00Z
  spec:
    # Agent / Skill / Tool / MCP / Workflow 各自定义
```

公共模型不直接放置统一的 `execution_target`、`backend` 或单一 `scope` 字段：

- 执行目标属于 Deployment；
- 本地物化和启用状态属于 Installation；
- 数据存储属于对应 Store/Space/Binding；
- 所有权属于 AssetDefinition；
- 可见性和可操作范围属于 Grant 与 Policy；
- 来源证明属于 Provenance。

这些字段被刻意排除在公共可写 Schema 之外，目的是避免 Agent 配置、资产作者或 UI 把声明性字段误当成执行授权，从而绕过 Deployment、Grant、Policy、MemoryBinding 等权威解析。运行位置、权限和数据域只能由对应控制面在运行时确定。

### 4.3 不同资产的执行语义

- Agent Definition 描述角色、策略和能力需求；Agent Deployment 绑定实际 Runtime 和治理策略。
- Skill 是任务知识和流程包，不是可执行原语；Skill Package 可以安装到本地，但只能通过固定 Skill 网关加载，不能把 Skill 名当作 Tool 调用。
- Tool Definition 描述工具契约；Tool Deployment/Binding 决定本地、云端或沙箱实现。
- MCP Definition 描述服务能力；Connection 决定本地进程或远程 Endpoint、Credential 和授权。
- Workflow Definition 使用 AssetRef 引用稳定版本；运行时由 Execution Planner 解析各节点的 Deployment。

### 4.4 云端连接后的分发与调用策略

CLI/Desktop 连接 Cloud Account 后，不把所有云端资产统一下载到本地，也不把所有资产统一留在云端。默认行为按资产的可移植性、执行依赖和安全边界决定：

| 资产 | 默认消费方式 | 本地物化策略 | 默认执行位置 |
|---|---|---|---|
| Agent | 调用 Cloud Agent Deployment | 仅在允许导出且依赖可本地解析时，显式克隆为新的 Local Agent | Cloud Runtime |
| Workflow | 调用 Cloud Workflow Deployment | 仅在全部节点通过本地兼容性检查时，显式克隆为新的 Local Workflow 草稿 | Cloud Workflow Runtime |
| Tool | 获取 Tool Schema，通过远程代理 Tool 调用 Cloud Tool Deployment | 只有提供受信任本地实现包时，才能显式安装并创建 Local Deployment | Cloud Tool Deployment |
| MCP | 连接 Remote MCP Server 或 Genesis Cloud MCP Gateway | 只有声明为 Local MCP Package 时，才能显式安装并启动本地 Server | Remote MCP 或 Local MCP Server |
| Skill | Cloud Agent 使用云端安装；Local Agent 使用前显式安装不可变 Skill Package | 推荐支持按 Version 和 Digest 安装、缓存与离线使用 | Skill 不直接执行；脚本由选定 Backend 执行 |

#### Agent

- CLI/Desktop 直接使用云端 Agent 时，只缓存 Catalog 和 Deployment 摘要，通过 Cloud Runtime 发起 Run，不下载 Agent 实现和云端依赖。
- Local Agent 如果需要把云端 Agent 作为子能力调用，应通过固定 `invoke_agent` 网关或规范化 Subagent 接口引用 Deployment ID，不把每个 Agent 名直接注册成 Tool。
- “克隆为本地 Agent”会创建新的 Local AssetDefinition 和 Local Deployment，并保留来源关系；它不是云端 Agent 的缓存，也不与云端版本自动同步。
- 克隆前必须检查模型、Skill、Tool、MCP、Memory、Credential 和 Policy 是否存在合法的本地替代。

#### Workflow

- 云端 Workflow 默认由 Cloud Workflow Runtime 持有编排状态，CLI/Desktop 只负责发起、订阅事件和展示结果。
- 一个 Workflow Run 只能有一个主编排器。本地与云端不能共同持有同一 Run 的推进状态、补偿状态和重试状态。
- Local Workflow 可以把 Cloud Agent、Cloud Tool 或 Remote MCP 调用建模为有明确输入输出的远程节点，但主编排器仍在本地。
- Cloud Workflow 不得直接调用本机能力；未来如需支持，必须经过独立的 Device Gateway 和安全协议。

#### Tool

- Local Agent 可以把获授权的 Cloud Tool Deployment 投影为远程代理 Tool；模型看到 Tool Schema，真正调用仍经过本地 Tool Gateway 和 Cloud Tool Gateway 双重治理。
- Cloud Tool 的实现代码和云端 Credential 不下载到本地。Credential 原则上留在实际执行位置。
- 只有 Tool Definition 同时提供受信任的本地实现包时，用户才能显式安装；安装、启用和创建 Local Deployment 是独立步骤。
- Cloud Deployment 不可用时不得静默切换到同名 Local Deployment，反之亦然。

#### MCP

- Remote MCP 使用 Streamable HTTP 等远程 Transport；企业托管 MCP 默认经 Genesis Cloud MCP Gateway，以集中处理 Credential、Grant、Audit 和数据策略。
- 用户级 Remote MCP 在协议和策略允许时可以由 CLI/Desktop 直接连接，但必须使用独立 Connection、OAuth/Token 和授权范围。
- Local MCP 使用 stdio 等本地 Transport。Cloud Registry 可以提供安装描述，但 Server Package 必须经用户确认、来源校验和安装记录后才能在本地启动。
- MCP Tool 经命名空间、Capability Negotiation、Profile、Grant 和 Tool Gateway 过滤后进入模型 Schema；MCP Resource 作为受控上下文读取，不能与 Tool 执行权限混为一谈。

#### Skill

- Cloud Agent 加载 Cloud Registry 中已安装的 Skill，不需要把 Skill Package 下载到 CLI/Desktop。
- Local Agent 使用云端 Skill 前，必须先通过 Package Marketplace 显式安装，生成 `InstallRecord`、`SourceProvenance` 和 Capability Index；连接云端账号本身不触发安装。
- Skill Package 使用不可变 Version 和内容 Digest 标识，可进入受配额管理的本地缓存；来源版本漂移时必须重新解析和审批。
- 安装只物化 `SKILL.md`、scripts、references、assets 和 manifest，不自动安装 Python、Node 或系统依赖，不扩大 Product Profile 权限。
- 加载仍通过 `Skill(skill=...)` 网关；脚本仍通过 `run_skill_command`，由 Runtime Profile 选择本地直跑、本地平台沙箱或远程 genesis-sandbox。

### 4.5 本地与云端资产

| 资产形态 | 权威存储 | 默认 Deployment | 可访问端 |
|---|---|---|---|
| CLI 本地资产 | CLI User/Workspace | CLI Local | CLI |
| Desktop 本地资产 | Local Profile | Desktop Local | 当前 Desktop Profile |
| 云端资产 | Cloud Registry | Cloud | Enterprise Web，以及已授权的 CLI/Desktop Cloud Connection |

边界规则：

- 本地资产默认不出现在 Enterprise Web，也不在 CLI 与 Desktop 之间自动共享。
- 云端资产的本地索引只用于发现和加速，不成为新的权威副本。
- Skill 等不可变 Package 的本地 Installation 可以引用同一云端 AssetRef 和内容 Digest，但安装记录、启用状态与运行依赖属于本地产品。
- 本地资产发布到云端时生成明确的云端 Asset ID 和 Version，不把本地文件直接变成云端权威存储。
- 跨端引用使用 AssetRef、workspace-relative path、sandbox path 或 resource ID，不使用宿主机绝对路径。

### 4.6 供应链威胁模型与发布者信任

来源、签名和 Digest 不是零散元数据，而是一条从发现、获取、安装到执行前复验的供应链控制链：

| 威胁 | 必须实施的控制 |
|---|---|
| 包替换、下载篡改、镜像漂移 | 不可变 AssetRef/Version、内容 Digest、签名与 Provenance；安装和执行前分别校验 |
| 恶意或少报权限的 Skill/Tool/MCP | Manifest 显式声明能力、依赖与权限；安装审批、隔离检查、沙箱和默认限制网络出口 |
| Prompt、Skill 内容或 Tool 描述诱导模型扩权 | 一律视为不可信内容；Skill 只能经固定网关加载，Tool Schema 只经过 Tool Gateway 白名单投影，描述文本不能授予权限 |
| 依赖混淆、隐式安装脚本和运行时污染 | Registry/Source Allowlist、依赖锁定；安装包与运行依赖分离，禁止隐藏安装器自动修改系统环境 |
| 更新、降级或审批后内容变化 | 审批绑定 Version、Digest、权限和依赖快照；任一变化都重新校验并按策略重新确认，拒绝未授权降级 |
| 包或脚本窃取凭据和数据 | 包内禁止携带 Secret；执行时通过 CredentialRef 注入最小权限凭据，并应用数据访问、网络出口、日志脱敏和审计策略 |

发布者信任分为“官方、已验证组织、未验证第三方”三个产品展示等级：

- 签名只能证明内容来自某个密钥控制者且未被修改，不能证明代码安全、行为善意或适合当前数据。
- 信任等级可以影响 Catalog 排序、风险提示和安装确认摩擦；用户或企业可以预先显式信任发布者，但信任范围必须可查看、可撤销。
- 发布者信任不得绕过 Runtime Policy、`allow/ask/deny`、Tool Gateway、Sandbox、Credential 或数据出口策略。
- 资产版本、Digest、权限、依赖、执行入口或发布者身份发生变化时，已有安装批准和持久授权必须按策略失效或重新确认。
- 完整包格式、验证状态机和市场治理由 Capability Package/Marketplace 设计维护；本节只定义三端共同遵守的安全不变量。

## 5. 总体架构

```text
                        Genesis Cloud Platform
┌─────────────────────────────────────────────────────────────────┐
│ Cloud Identity / Tenant / Membership / RBAC                    │
│ Asset Registry / Deployment / Grant / Credential / Connection  │
│ Cloud Runtime / Workflow Runtime / Sandbox Gateway              │
│ Cloud Memory / Knowledge Base / Trace / Audit / Usage           │
└───────────────┬─────────────────────┬───────────────────────────┘
                │                     │
         必选 Cloud Session      可选 Cloud Connection
                │                     │
      ┌─────────▼────────┐      ┌─────┴─────────────────────┐
      │ Enterprise Web  │      │                           │
      │ 企业工作空间     │  ┌───▼────────────┐    ┌────────▼───────┐
      │ 企业治理入口     │  │ Desktop       │    │ CLI            │
      └─────────────────┘  │ Local Profile  │    │ Local Context  │
                           │ Local Runtime  │    │ Local Runtime  │
                           │ Local Assets   │    │ Local Assets   │
                           │ Local Memory   │    │ Project Memory │
                           └────────────────┘    └────────────────┘
```

Desktop/CLI 到 Genesis Cloud 的连接必须是可移除适配，不得成为本地 Runtime、Local Profile、文件存储或本地认证的传递依赖。

## 6. 能力发现、授权与执行

### 6.1 能力发现

- 本地 Registry 返回当前 Local Profile、CLI User 或 Workspace 中的资产和 Deployment。
- Cloud Registry 只向有效 Cloud Connection 返回已授权的资产与 Deployment 摘要。
- UI 可以展示友好名称，但内部必须保留 Registry、Asset ID、Version 和 Deployment ID。
- 本地与云端同名资产不互相覆盖；发生歧义时要求显式选择或使用已保存的项目绑定。

推荐命名空间：

```text
cloud:<registry>/<asset>@<version>
desktop:<profile>/<asset>@<version>
cli:user/<asset>@<version>
cli:project/<asset>@<version>
```

### 6.2 云端授权闭环

云端能力能否执行，至少由以下条件共同决定：

1. Cloud Connection 和 Token 是否有效；
2. 用户、租户、团队和角色是否拥有对应 Grant；
3. 当前客户端和运行环境是否满足 Policy；
4. Deployment 是否有效且依赖、Credential、Connection 和 Sandbox 可用；
5. 企业安全策略是否允许本次调用。

客户端发现时可以过滤和缓存授权结果，但 Cloud Runtime 执行前必须重新鉴权。Tool/MCP 真正调用时继续执行细粒度权限、凭据和审计检查。

### 6.3 执行路由

Runtime 根据 Deployment 路由，不根据资产名称或创建端猜测执行位置：

```text
用户选择 AssetRef 或 Deployment
          │
          ▼
解析 Definition、Version 与候选 Deployment
          │
          ▼
检查 Identity、Grant、Policy、Credential 和 Runtime 状态
          │
          ├── Local Deployment ──> 当前产品 Local Runtime
          │
          └── Cloud Deployment ──> Cloud Runtime / Sandbox
```

默认策略：

- CLI/Desktop 本地资产创建 Local Deployment。
- Enterprise Web 创建的 Agent/Workflow 默认创建 Cloud Deployment。
- Agent、Workflow 和 Cloud Tool 在 CLI/Desktop 中默认调用 Cloud Deployment，不自动下载实现或创建 Local Deployment。
- 同一 AssetDefinition 可以存在多个 Deployment，但一次调用必须绑定明确的 Deployment ID；网络失败或后端不可用时不得静默切换。
- Workflow Run 由创建它的 Workflow Runtime 单独持有编排状态；跨端能力只能作为显式远程节点调用。
- 云端编排不能假设可访问用户本机文件、命令或凭据。
- 如未来支持 Cloud Runtime 调度本地设备，必须单独设计设备注册、在线状态、端到端鉴权、用户确认、任务租约、审计和断线处理协议。
- `SandboxAuto` 不得静默降级为无沙箱；`SandboxRequire` 无法满足时必须失败。

### 6.4 跨端输入与产物

Cloud Agent、Workflow、Tool 或 Remote MCP 需要使用本地文件时，必须经过显式 staging，不能传递宿主机绝对路径：

```text
本地文件或资源
  -> 用户授权与数据策略检查
  -> 大小、MIME、Hash 和敏感信息检查
  -> 生成 ResourceRef / InputRef
  -> Stage 到目标 Run 的 Input Space
  -> Cloud Runtime 使用 sandbox path
```

云端结果通过 `ArtifactRef` 或 ResourceRef 返回，用户按需显式下载或交付到本地目标。远程 MCP 优先使用签名 URL、Resource ID 或其上传 API；不支持文件上传时必须明确失败，不能发送本地路径碰碰运气。

跨端传输遵循最小披露原则：只传本次 Run 明确选择的输入，不默认上传整个 Workspace、Local Profile、MemorySpace 或 Credential。

CLI 与 Desktop 的资产、记忆和凭据 Store 继续相互隔离；但二者若同时操作同一磁盘 Workspace，必须复用规范化 Resource ID 和 FreshnessTracker。ResourceLock/ToolScheduler 负责进程内与同一 Runtime 的并发；两个产品可协作时再使用 OS 文件锁或共享 LockProvider 协调跨进程写入，但不能把内存锁误当成跨进程保护。无论是否能协作加锁，最终写入都必须携带 expected Hash/版本；冲突操作应排队或结构化失败，并提示重新加载或比较差异，不允许后写静默覆盖先写。

### 6.5 云端协议版本与能力协商

Desktop/CLI 的发布节奏慢于 Cloud Platform，云端 API 不能假设客户端永远与服务端同版：

- 建立独立的 Cloud Client Protocol Version，不复用模型供应商配置中的 `api_version`，也不只依赖备份 SchemaVersion。
- 建连时客户端上报产品、客户端版本、协议版本和支持能力；服务端返回协商后的协议、最低受支持版本、建议版本、弃用信息与必需能力。
- 同一协议版本内优先采用可选字段和向后兼容扩展；客户端忽略未知可选字段。改变既有语义、删除字段或改变安全默认值时必须升级协议版本。
- 服务端维护明确的兼容窗口和弃用周期。旧客户端不满足安全或协议最低要求时返回 `upgrade_required` 及可操作信息，不能表现为普通网络错误。
- Deployment、事件流、Artifact 和审批协议使用稳定 ID、幂等键及可演进 Schema；恢复连接后按游标续传，不能依赖进程内状态猜测。
- 云端协议不兼容只影响云端能力；不得阻塞 Desktop/CLI 的本地 Runtime、Local Profile 或本地资产使用。

### 6.6 Usage、预算与配额体验

Usage 不只是后台计量。每次云端 Run 必须让用户和自动化系统能够理解资源限制、费用影响和下一步动作：

- 统一返回 `rate_limited`、`quota_exceeded`、`budget_exceeded`、`billing_required`、`concurrency_limited` 等结构化错误，并携带适用的 `retry_after`、限制值、当前用量、重置时间和解决动作。
- 在可以可靠估算时，运行前展示模型、Deployment、数据位置和预计资源区间；运行中应用 Token、Tool、时间、并发和费用硬预算，并持续展示消耗。
- 排队必须由 Deployment/租户策略显式允许并展示队列状态；不能把配额不足伪装成无限等待或无提示重试。
- 不得因配额或费用问题静默切换到成本、数据驻留、模型能力或执行位置不同的后端。任何此类降级都需要已声明策略或用户确认。
- Cloud Deployment 失败只结束或暂停对应远程调用，本地能力继续可用。CLI/CI 额外提供稳定退出码和机器可读 JSON，避免自动化依赖自然语言错误。

## 7. 本地执行安全、人机协作与可观测性

### 7.1 统一权限模型

本地文件、命令和桌面能力与云端 Tool 使用同一套 Policy/Approval 语义：策略结果为 `allow`、`ask` 或 `deny`，授权范围为 once、turn、session、project 等既有 Scope。`deny` 是硬拒绝，任何 Agent、Skill、Workflow、发布者信任或产品默认值都不能覆盖。

不新增“只读/建议/自动”作为第二套权限状态机。只读/写入、是否有外部副作用、可逆性和风险等级是能力与动作的属性，用于 Policy 求值；是否执行仍由 `allow/ask/deny` 与授权 Scope 决定。

| 本地能力 | 最少区分的资源或动作维度 | 安全默认值 |
|---|---|---|
| 文件系统 | Workspace 内外、读/写/删除、受保护路径 | 明确 Workspace 内按策略授权；外部目录和破坏性操作至少 `ask`，敏感路径可 `deny` |
| 命令执行 | 只读检查、修改状态、危险命令、运行环境 | 修改状态或高风险命令至少 `ask`；需要沙箱时不得降级直跑 |
| 网络 | 目标域、上传/下载、数据分类 | 未声明出口默认拒绝或询问；上传本地数据必须绑定明确目标 |
| 桌面与设备 | 剪贴板、屏幕、键鼠控制、摄像头、麦克风 | 按能力独立授权，采集和主动控制必须可见 |
| 外部沟通 | 邮件、消息、工单、购买等真实副作用 | 发送或提交前确认收件人、内容和影响，除非已有精确的持久授权 |
| 凭据 | CredentialRef、用途、目标服务 | Agent 不可读取明文，只能在获授权动作中由执行层代用 |

### 7.2 运行时确认、中断与紧急停止

- 有副作用的 Tool 在真实执行前完成策略求值和审批，审批请求展示动作、资源、参数摘要、数据去向、风险与授权范围。
- 前台 `ask` 可以即时弹出确认；后台、定时任务或用户离线时生成 `intervention.required` 并暂停 Run，同时通过本地通知和活动中心提醒。
- 未配置可持久化 Intervention Broker、等待超时或审批通道不可用时，默认拒绝或按已声明策略失败；禁止 Headless/Desktop 后台自动批准。
- 用户可以批准、拒绝、修改可修改参数或中止 Run。紧急停止必须取消当前 Run 及其受控子 Run、阻止新的 Tool 调用，并留下完整审计记录。
- 已授予权限必须在权限中心按 Agent、能力、资源和 Scope 查看与撤销；撤销影响后续调用，不伪造已经发生的外部操作已被回滚。
- 详细状态机和端口分别以统一权限审批设计、人工干预机制设计为准，本设计只规定三端产品行为。

### 7.3 Desktop 本地活动时间线

Desktop 为个人用户提供本地优先的活动时间线，它不是 Enterprise Audit 的缩小副本，而是建立自主 Agent 信任的核心界面：

- 由本地 Run Event、Trace 和 Audit 投影生成，默认存入当前 Local Profile；纯本地使用不上传云端。
- 展示谁在何时因何任务调用了什么能力、目标资源、运行位置、审批结果、状态、产物以及可用的文件差异。
- 聚合待确认事项、后台任务状态、失败原因、重试入口和紧急停止入口，并允许跳转到对应权限或运行详情。
- “撤销”只在执行前建立了可验证快照、事务或补偿动作，并通过 Freshness 校验确认目标未被其他进程修改时提供。邮件发送、外部提交等不可逆动作明确标记不可撤销，不承诺通用 Undo。
- 个人活动记录支持本地保留期限、敏感字段脱敏、导出和删除；企业受管策略可以对 Managed Data Domain 要求保留审计副本或限制删除，但必须明确提示治理主体、数据去向和保留期。

### 7.4 降低确认疲劳

- 用户可以对精确的 Agent、动作、资源范围和期限授予持久权限；同一计划内的同类操作可以合并展示影响摘要，避免逐调用重复弹窗。
- 一旦目标资源、动作类别、数据去向、资产 Version/Digest、权限声明或依赖发生实质变化，必须重新求值，不能沿用模糊的“始终允许”。
- 官方或已验证发布者可以降低安装阶段的提示摩擦，但不能降低文件删除、命令执行、数据上传等运行时审批标准。
- 权限中心提供清晰的来源、最近使用时间和一键撤销；系统不得通过默认勾选、模糊措辞或把拒绝按钮藏起来诱导授权。
- 冷启动不应假设所有来源都未验证：内置资产和可验证的官方包可以预先展示来源状态，但首次运行仍只获得完成示例所需的最小权限。
- 新用户默认从无外部副作用的引导任务开始，在用户选择的示例目录或受控 Workspace 中运行；先展示一份任务计划和权限包，再随真实需要渐进请求，不在首次启动集中索要文件、Shell、屏幕、网络等全部权限。
- 重复点击“允许”不会自动提高信任等级。只有用户明确保存了具体 Scope，或管理员配置了可审计 Policy，系统才减少后续确认。

### 7.5 内部概念与用户语言

领域模型保留精确术语，普通 UI 使用用户能够直接做决定的语言，并把底层标识留在高级详情：

| 内部概念 | 默认用户语言 |
|---|---|
| LocalProfile | 本地空间 |
| ProfileLock | 本地空间锁 |
| CloudConnection | 云端账号连接 |
| AssetDefinition | Agent、技能、工具或工作流及其版本 |
| Installation | 已安装到本机 |
| Deployment | 运行位置：本机或云端 |
| Grant / Policy | 权限与允许范围 |
| MemorySpace / MemoryBinding | 记忆来源与使用范围 |
| ArtifactRef | 结果或文件 |
| Intervention | 需要你确认 |
| BackupSnapshot | 备份版本 |

首次连接云端后，产品应明确高亮“新增的云端能力”，同时展示运行位置、需要发送的数据、权限、信任来源和可能费用；不得把本地与云端能力无差别混排，让用户靠试错发现执行位置。

## 8. 记忆与用户画像

### 8.1 概念边界

| 概念 | 生命周期与用途 | 归属 |
|---|---|---|
| Working Memory | 当前 Run 中间状态，Run 结束释放 | RunContext |
| ShortTermMemory | Session 消息、摘要和上下文窗口 | MemorySpace |
| LongTermMemory | 跨 Session 的长期事实和经验 | MemorySpace |
| UserProfile | 用户偏好与画像，作为静态上下文注入 | UserProfileStore |

UserProfile 不是 Agent 可声明的 Memory Type。Runtime 通过 `UserProfileStore` 和 Context Builder 读取允许注入的画像数据。

### 8.2 MemorySpace

MemorySpace 表示一个有明确所有者、安全边界和存储归属的记忆空间：

```text
MemorySpace
  ID
  DataDomain        local | cloud
  OwnerType         profile | user | agent | project | team | tenant
  OwnerID
  RetentionPolicy
  PrivacyPolicy
  StoreRef
```

MemorySpace 由产品服务创建，`StoreRef` 由产品 bootstrap 和 Memory 能力装配，不暴露给 Agent 配置。

典型空间：

| 场景 | DataDomain | Owner | 权威存储 |
|---|---|---|---|
| Desktop 个人助手 | local | profile/agent | Desktop Local Store |
| CLI 项目记忆 | local | project/agent | CLI Workspace Store |
| CLI 用户偏好 | local | user | CLI User Store |
| 云端用户 Agent | cloud | user/agent | Cloud Memory Store |
| 企业团队记忆 | cloud | team/agent | Cloud Memory Store |
| 企业租户知识与经验 | cloud | tenant | Cloud Memory/Knowledge Store |

### 8.3 MemoryBinding

MemoryBinding 在 AgentInstance、Workspace 或 Session 创建时，将运行实例绑定到允许使用的 MemorySpace：

```text
MemoryBinding
  SubjectID
  ShortTermSpaceID
  LongTermSpaceIDs
  UserProfileStoreIDs
  ReadPolicy
  WritePolicy
```

约束如下：

- MemoryBinding 由产品应用服务根据身份、Deployment、Workspace 和 Policy 创建。
- Agent 配置不暴露任意 `backend`、`sync` 或 `scope` 组合。
- Agent 只能读取 Runtime 解析出的只读 MemoryView，不能增加 Space、修改 StoreRef 或扩大权限。
- 一个 Run 内的 Binding 保持不变；管理面修改只影响后续 Run，并且需要权限与审计。
- Local Profile 不连接云端时，只能绑定本地 MemorySpace 和本地 UserProfileStore。
- Cloud Deployment 的可写记忆必须落入 Cloud DataDomain。
- 本地 Runtime 可以按授权只读访问云端 MemorySpace 或 UserProfileStore，但本地写入与云端写入使用不同目标，不做隐式跨域复制。

只读解析结果示例：

```text
MemoryView
  short_term -> local:profile/session
  long_term  -> local:profile/agent

UserProfileView
  local      -> local:profile/user
  cloud      -> cloud:user/profile   # 仅在已连接且获授权时存在
```

### 8.4 长期记忆写入

Runtime 提供记忆契约与读取注入能力，但“哪些内容值得成为长期记忆”属于业务决策，不能由框架无条件推断和写入：

```text
Run 消息、工具结果与上下文
          │
          ▼
业务 Memory Extractor（可选）
          │
          ▼
分类、去重、合并、敏感信息检查
          │
          ├── 无长期价值 ──> 丢弃
          │
          └── 候选记忆 ──> 用户确认或策略审批 ──> Binding 允许的 Space
```

长期记忆和用户画像必须可查看、可编辑、可删除、可审计。云端查询必须包含租户与授权边界，不得仅凭 Agent ID 跨租户读取。

## 9. 缓存、安装、克隆、发布与同步边界

以下概念必须严格区分：

| 动作 | 是否创建新资产 | 是否持续保持一致 | 典型用途 |
|---|---|---|---|
| Cache | 否 | 从权威源单向失效与刷新 | 云端资产索引、授权摘要、运行结果预览 |
| Install/Materialize | 否，安装同一不可变 Package | 不跟随可变来源；按 Version/Digest 更新 | Skill、Local MCP 或本地 Tool Package 安装 |
| Clone/Fork | 是，保留来源关系 | 否 | Cloud Agent/Workflow 显式克隆为本地 Definition |
| Publish/Copy | 是 | 否 | 本地资产发布为新的云端资产、CLI/Desktop 之间的一次性受控复制、显式导入导出 |
| Sync | 可能形成多个可写副本 | 是 | 少数经过专门设计的数据类型 |

设计约束：

- 云端连接默认只带来 Catalog Cache 和远程调用能力，不触发 Install、Clone、Publish 或 Sync。
- Skill/Package 安装必须生成 InstallRecord，固定 Version、Digest、SourceProvenance、安装范围和启用状态。
- Agent/Workflow 克隆必须生成新的 Local Asset ID；不能把缓存元数据伪装为可执行的 Local Deployment。
- Tool/MCP 的可执行 Package 不得自动下载、自动启用或绕过来源、权限和供应链检查。
- 不提供通用的本地与云端双向 Sync。
- 本地资产、记忆、凭据和运行记录不会因连接 Cloud Account 自动上传。
- 如果未来为特定数据类型提供 Sync，必须单独定义版本、冲突解决、删除传播、加密、离线队列、失败恢复和审计规则。
- 企业策略禁止落盘的数据不得进入本地 Cache。
- Cache 删除后应能从权威存储重建，不能承载唯一数据。

跨端不自动互通不等于故意制造搬运障碍。CLI/Desktop 应提供“复制到 Desktop”或“用于当前 CLI 项目”的一次性引导流程：

- 通过共享的 Portable Asset Bundle/Import Service 导出与导入，不能让一个产品直接读取另一个产品的私有 Store。
- 复制前展示 Definition、版本、依赖、运行位置、将排除的 Credential/Memory、路径兼容性和目标空间；缺少本地实现或企业策略禁止导出时明确说明原因。
- 复制后生成新的 Asset ID、Provenance 和目标端 Deployment/Installation 草稿，默认不启用高风险能力；源和目标此后独立演进，不建立隐藏的同步关系。
- 当用户在另一端搜索到“同一个”资产时，界面明确说明“这里还没有副本”，并提供远程调用、一次性复制或发布到云端等合法选项，而不是只显示找不到。

## 10. 备份、恢复与设备迁移

### 10.1 设计原则

备份是 local-first Desktop 的基础能力，不能以注册 Genesis Cloud Account 为前提：

- 无 Cloud Account 时，用户可以创建加密备份、恢复备份、写入用户选择的目录或执行设备迁移。
- 连接 Cloud Account 后，可以额外启用端到端加密的 Genesis Cloud Backup。
- Backup 是某个时间点的只读快照，不是多设备双向 Sync，也不承担并发合并。
- 个人 Local Profile 仍是本地权威数据空间；备份只用于灾难恢复和显式迁移。Managed Data Domain 是否允许进入备份由企业数据策略决定，不能混入个人备份绕过治理。
- 如果用户未创建任何备份且原设备不可用，本地数据无法恢复。产品必须明确展示最后备份时间和风险状态。

### 10.2 备份方式

| 方式 | 是否需要 Genesis Cloud Account | 目标 | 适用场景 |
|---|---|---|---|
| 手动加密备份 | 否 | 用户指定的 `.gbackup` 文件 | 手动归档、移动硬盘迁移 |
| 自动目录备份 | 否 | 本地目录、NAS 或用户已有同步盘目录 | 定时备份和用户自管异地存储 |
| 设备到设备迁移 | 否 | 经过确认的目标设备 | 旧设备仍可用时换机 |
| Genesis Cloud Backup | 是 | Genesis Cloud Backup Store | 自动异地备份、版本保留和跨设备发现 |

Desktop Standalone 必须支持前三种能力，不得因为没有注册 Cloud Adapter 而失去备份与恢复能力。

自动目录备份只向目标目录写入已经完成的一致性快照，不能把正在运行的 SQLite、Credential Vault 或整个应用数据目录直接放进同步盘。

### 10.3 BackupSnapshot

备份使用版本化、可验证的快照契约：

```text
BackupSnapshot
  ID
  ProfileID
  CreatedAt
  ProductVersion
  SchemaVersion
  EncryptionVersion
  IncludedCategories
  ExcludedCategories
  ManifestHash
  PayloadHash
```

创建流程：

```text
冻结本次快照的逻辑版本
  -> 使用 SQLite Online Backup 或等价一致性快照机制
  -> 收集选定的 Profile 数据
  -> 生成 BackupManifest
  -> 加密 Payload
  -> 计算 Hash 并验证可读取性
  -> 原子发布 .gbackup 或上传密文
```

不允许一边复制可变数据库文件、一边继续写入后把所得目录声明为可恢复备份。备份成功必须以 Manifest、Hash 和解密校验全部通过为准。

### 10.4 备份内容

默认包含：

- Local Profile 元数据和 Desktop 设置；
- 本地 Agent、Workflow 和用户自建 Skill；
- Package InstallRecord、SourceProvenance 和 Capability Index；
- Session、对话、长期记忆、UserProfile、Task 和必要运行状态；
- Tool、MCP、模型和 Cloud Connection 的非敏感配置；
- Cloud AssetRef、DeploymentRef 和其他可重新解析的引用。

按用户选择包含：

- Artifact 与用户明确纳入的 Workspace 文件；
- 完整历史 Run、Trace 和本地 Audit；
- 无法从可信来源重新获取的第三方 Package 原包。

默认排除：

- 临时目录、Sandbox、日志和可重建 Cache；
- Python/Node 依赖目录和可重新安装的 Package 内容；
- 模型权重和其他大型可重新下载资源；
- 云端资产实体、云端 Run 副本和云端受控数据 Cache；
- 企业策略禁止导出或备份的 Managed Data Domain 内容；
- 操作系统明确标记为不可导出的 Credential。

每次备份必须在 UI、Manifest 和审计记录中显示实际包含与排除的数据类别，不能用“全部数据”掩盖排除项。

### 10.5 Credential 与加密

Profile Lock 和 Backup Encryption 是两个不同边界：操作系统绑定的 Profile 解锁密钥通常不能在新设备使用，因此跨设备备份必须拥有独立的恢复密码或 Recovery Key。

默认安全模式：

- 不导出 API Key、OAuth Refresh Token、MCP Token 和 Channel Credential；
- 恢复后保留 Connection 配置，但要求用户重新授权；
- Backup Manifest 不记录 Secret 明文或可用于恢复 Secret 的日志。

完整迁移模式：

- 用户必须显式选择“包含可导出的 Credential”；
- 使用独立恢复密码或 Recovery Key 对整个敏感 Payload 加密；
- UI 明确提示备份文件等同于高敏感 Credential Vault；
- 操作系统标记为不可导出的密钥仍只能重新认证。

密码派生、认证加密、随机数和密钥封装必须使用经过审计的标准密码库，并在 Manifest 中记录算法版本以支持升级；不得自行设计加密算法。

### 10.6 Genesis Cloud Backup

Genesis Cloud Backup 是 Cloud Connection 的可选能力，默认关闭：

```text
Local Profile
  -> 本地创建一致性 BackupSnapshot
  -> 本地使用 Backup Key 加密
  -> 上传密文与最小必要元数据
  -> Cloud Account 管理定位、配额、设备和保留策略
```

安全边界：

- Genesis Cloud 默认只保存密文，不能读取对话、记忆、本地 Agent、Credential 和 Workspace 内容。
- Cloud Account 用于定位备份、管理配额和授权设备；Recovery Key 或已授权设备密钥用于解密。
- 开启云备份前必须展示包含范围、预计大小、保留期、地域与删除策略。
- 断开 Cloud Connection 后停止新的上传；已有备份的保留或删除按用户选择和服务策略处理，不能静默删除。
- 不提供 Genesis 托管的匿名云备份。无账号用户使用加密文件、用户自选目录或设备迁移。
- 云备份失败不得影响 Local Runtime，只记录状态并进行有上限、可取消的后台重试。

### 10.7 恢复与冲突处理

恢复默认创建新的 Local Profile，不直接覆盖当前 Profile：

```text
选择本地备份或云端 BackupSnapshot
  -> 验证 Manifest、Hash、版本与解密能力
  -> 预览包含项、排除项和不兼容项
  -> 选择创建新 Profile 或明确覆盖
  -> 覆盖前自动备份当前 Profile
  -> 在隔离目录恢复并执行 Schema Migration
  -> 完整性检查通过后原子切换
  -> 重新授权未迁移的 Credential
```

恢复不得把两个正在演进的 Profile 自动合并。存在冲突时默认新建 Profile；覆盖和选择性导入必须展示影响范围并要求确认。

恢复完成页必须明确说明“已恢复为新的本地空间，原空间仍然存在”，并同时展示当前与恢复后 Profile 的名称、时间、大小和入口。用户可以立即切换、检查差异或启动受控的选择性导入；确认新 Profile 可用前不主动建议删除旧 Profile，也不使用含糊的“合并”承诺掩盖逐项导入规则。

设备到设备迁移复用同一 BackupSnapshot 契约，通过一次性配对码、双方确认和端到端加密通道传输，不另造一套数据格式。

### 10.8 生命周期与产品体验

- 用户创建重要数据后应提示配置备份，但不得阻塞本地使用或强迫连接云端。
- 设置页持续显示最近成功备份时间、目标、校验状态和下一次计划。
- 备份提醒按风险渐进升级：初次使用采用非阻塞引导；产生长期记忆、自建 Agent/Workflow 或持续使用一段时间后仍无已验证备份时，升级为明显但可暂缓的 Banner；超过保留目标或连续失败时展示具体风险和修复动作。
- 每次提醒都同时提供“不使用云端”的自动目录备份和手动加密备份入口，避免把数据丢失焦虑变成注册 Cloud Account 的暗黑模式。用户可以暂缓或关闭提醒，但设置页和 Profile 状态始终显示“未受备份保护”。
- 同盘自动快照可以用于误操作恢复，但不能冒充设备损坏时仍可用的灾难备份；产品必须区分“本机恢复点”和“位于其他介质/设备的已验证备份”。
- 自动备份采用保留策略与磁盘配额，清理旧版本前保留至少一个已验证快照。
- 产品升级、批量迁移和高风险恢复前自动创建并验证本地 BackupSnapshot。
- Backup Store 和运行数据 Store 使用独立生命周期；删除 Local Profile 时必须单独询问是否删除本地与云端备份。

[OpenClaw Backup](https://docs.openclaw.ai/cli/backup) 与[换机迁移](https://docs.openclaw.ai/install/migrating)可作为产品参考：它将状态、配置、会话、认证资料和可选 Workspace 纳入可验证备份，同时把云端上传、调度和保留策略与本地备份命令分离。Genesis Desktop 在此基础上增加统一快照契约和可选端到端加密云备份。

## 11. 核心用户旅程

### 11.1 Desktop 纯本地使用

```text
首次启动自动创建默认 Local Profile
  -> 后续自动进入，或按策略选择/解锁 Profile
  -> 创建本地 Agent
  -> 授权本地目录或桌面能力
  -> Local Deployment 执行
  -> 结果与记忆写入 Local Profile
```

整个流程在断网、Cloud DNS 失败或 Genesis Cloud 完全不可用时仍可完成。

### 11.2 Desktop 连接云端

```text
进入 Local Profile
  -> 连接 Cloud Account
  -> 获取授权 Deployment 摘要
  -> 选择 Cloud Deployment
  -> Cloud Runtime 执行
  -> 云端结果按策略展示或缓存
```

断开 Cloud Connection 后，本地 Agent、记忆、任务和运行记录保持不变。

### 11.3 CLI 本地开发

```text
进入代码 Workspace
  -> 加载项目 Agent/Skill/Tool/MCP
  -> CLI Local Runtime 执行
  -> 项目记忆写入 Workspace Store
```

### 11.4 CLI 调用云端能力

```text
连接 Cloud Account
  -> 选择明确的 AssetRef/Deployment
  -> Cloud Runtime 执行
  -> CLI 展示流式事件与结果引用
```

### 11.5 本地资产发布云端

```text
选择本地 AssetDefinition
  -> 展示将发布的内容与依赖
  -> 敏感信息和本地路径检查
  -> 用户确认目标 Registry
  -> 创建新的云端 Asset ID/Version
  -> 按权限创建 Cloud Deployment
```

发布不是同步，发布后本地与云端版本分别演进。

### 11.6 本地安装云端 Skill

```text
从 Cloud Catalog 选择 Skill Package
  -> 展示来源、版本、Digest、权限与依赖
  -> 用户确认安装范围
  -> Package Marketplace 获取并校验不可变包
  -> 生成 InstallRecord 和 Capability Index
  -> Local Skill Catalog 刷新
  -> Agent 通过 Skill 网关显式加载
```

安装 Skill Package 不等于执行 Skill，也不自动安装脚本运行时依赖。后续脚本执行仍由 `run_skill_command` 和 Runtime Profile 决定 Backend。

### 11.7 Desktop 无账号换机

```text
旧设备创建并验证加密 BackupSnapshot
  -> 写入移动介质、用户自选目录或设备迁移通道
  -> 新设备安装 Desktop Standalone
  -> 选择“从备份恢复”
  -> 输入恢复密码或 Recovery Key
  -> 恢复为新的 Local Profile
  -> 重新授权不可迁移的 Credential
```

该旅程不请求 Genesis Cloud，也不要求用户注册 Cloud Account。

### 11.8 CLI Agent 一次性复制到 Desktop

```text
在 CLI 选择“复制到 Desktop”
  -> 导出 Portable Asset Bundle
  -> 预览 Definition、依赖、排除项与目标 Profile
  -> Desktop 校验来源、兼容性和产品策略
  -> 创建新的 Local Asset ID 与 Deployment 草稿
  -> 用户检查权限并显式启用
```

复制不需要 Cloud Account，也不建立同步关系。若 Desktop 不在同一设备，可将加密 Bundle 交付到目标设备；Credential、项目记忆和绝对路径默认不随资产复制。

### 11.9 企业策略以代码交付

```text
管理员在 Git 修改声明式策略
  -> CI 使用 Workload Identity 调用 validate/plan
  -> 展示影响范围、漂移与策略检查结果
  -> 进入企业审批
  -> 以 expected version 幂等 apply
  -> Enterprise Web 展示结果、审计和托管来源
```

Web 与 GitOps 使用同一 Management API 和授权闭环；被 GitOps 托管的字段不能在 Web 中被无审计覆盖。

## 12. 明确不做的事情

- 不要求 CLI 登录云端后才能使用本地能力。
- 不要求 Desktop 注册本地账号或连接云端后才能创建、进入、解锁或使用 Local Profile。
- 不默认强制 Profile Lock；是否启用由用户设置或受管产品策略决定。
- 不把 Local Profile 设计成可隐式升级的 Cloud Account。
- 不把多个 Local Profile 当作应用内多用户、租户或 RBAC 系统。
- 不支持多个真人在同一操作系统账号下并发共享 Local Runtime、Gateway、Credential 或 Memory。
- 不自动合并 Desktop Local Profile 与 Cloud Account 数据。
- 不自动互通 CLI 与 Desktop 本地 Agent、记忆和凭据。
- 不为表面一致而强迫 CLI、Desktop、Web 使用相同交互，也不隐藏运行位置或要求用户每次手动选择 Deployment。
- 不因连接 Cloud Account 自动下载、安装或启用任何可执行 Package。
- 不把云端 Agent/Workflow 的 Catalog Cache 当成可本地执行的资产。
- 不让本地与云端 Runtime 共同持有同一个 Workflow Run 的编排状态。
- 不在 Cloud Deployment 失败时静默降级到同名 Local Deployment。
- 不根据资产创建端永久锁死所有可部署位置。
- 不允许 Agent 动态指定 Memory Backend、StoreRef 或扩大 Scope。
- 不让 Cloud Workflow 无确认地控制用户本机。
- 不让 Desktop 后台任务或 Headless CLI 在缺少审批通道时自动批准 `ask` 操作。
- 不把官方、已验证发布者或包签名当作运行时安全许可。
- 不承诺对邮件发送、外部提交、不可恢复删除等任意副作用提供通用撤销。
- 不为小团队另造本地共享账号层；团队共享复用 Cloud Workspace、成员关系与 Grant。
- 不允许 Enterprise Managed 策略读取或擦除未登记为企业受管的数据域，也不把 Genesis Cloud 远程命令冒充 OS/MDM 整机擦除。
- 不让 ClickOps 和 GitOps 无规则地同时修改同一受管字段。
- 不提供泛化的本地与云端双向同步。
- 不把备份等同于同步，也不在两台设备之间自动合并 Local Profile。
- 不要求用户注册 Cloud Account 才能备份、恢复或换机。
- 不把运行中的数据库目录直接复制到同步盘并宣称为可恢复备份。
- 不默认导出 Secret，也不提供 Genesis 托管的匿名云备份。
- 不把三端打包成同一个二进制，也不让产品之间直接依赖私有实现。

## 13. 工程落地原则

- 通用 Runtime、领域模型和能力契约放在 `internal`。
- CLI/Desktop 共享的宿主机适配放在 `shared/local`。
- 身份、Store、Registry、Deployment、Policy、Sandbox 和接入面由 `products/<product>/bootstrap` 装配。
- Cloud Connection 使用端口与适配器接入，不得反向污染 Local Runtime。
- Desktop Standalone 的 bootstrap 不注册 Genesis Cloud Adapter；不能用空 Endpoint、失败重试或隐藏 UI 冒充独立运行。
- 远程 Agent/Workflow 通过固定调用网关接入；远程 Tool/MCP 通过 Tool Gateway 或 MCP Client/Gateway 接入，不把云端产品实现 import 到本地 Runtime。
- Package Marketplace 负责获取、安装、InstallRecord 和 Capability Index；Skill Service 只负责 Catalog、加载与运行期协议，二者不得合并。
- Policy Engine、Approval Service、Intervention Broker、Audit Store 和 Activity Projection 通过端口装配；Desktop/CLI 不得用 AutoApprove 实现替代不可用的审批通道。
- CLI 与 Desktop 对同一 Workspace 的文件操作复用 PathResolver、FreshnessTracker 和 expected Hash；ResourceLock/ToolScheduler 管理 Runtime 内并发，跨进程按产品能力注入 OS 文件锁或共享 LockProvider，不能只依赖内存锁。
- Cloud Protocol Negotiator、结构化 Usage/Quota Error 和 Workload Identity Adapter 属于平台契约；本地 Runtime 只依赖可选端口，不直接依赖 Cloud SDK。
- Management API、Enterprise Web、管理 CLI 和 IaC/GitOps Adapter 复用同一应用服务、Policy 与 Audit，不各自实现一套治理逻辑。
- Managed Data Domain 使用独立密钥、Storage Policy、Retention 和 Erasure Port；个人 Local Profile 与受管数据不能共用一个无法区分归属的数据库或备份集合。
- Publisher Trust Policy 只影响发现与安装体验，运行时仍统一进入 Tool Gateway、Policy、Approval、Sandbox 和 Audit。
- Backup Service 负责一致性快照、Manifest、加密、校验和恢复编排；具体本地目录、设备传输和 Cloud Backup Store 由产品 bootstrap 注入。
- Cloud Backup Adapter 不得成为 Backup Service 或 Desktop Standalone 的必选依赖。
- 三个产品不得互相 import 对方的 `internal` 实现。
- 企业版不得直接访问宿主机文件系统，只能通过受治理的 sandbox/filesystem backend。
- Docker/genesis-sandbox 模式不展示宿主机绝对路径。
- 新增跨产品能力时必须通过 import graph 验证产品隔离。

## 14. 推荐落地顺序

### Phase 1：建立 local-first 产品闭环

- 固化 CLI、Desktop、Enterprise 三个独立入口和 Product Profile。
- Desktop 建立默认 Local Profile、Profile 级数据隔离、可选 Profile Lock 和本地数据目录。
- 固化单一可信操作者边界；验证多个操作系统账号使用各自独立的 Desktop 数据根和安全存储。
- 交付 Desktop Standalone 装配，验证启动和本地核心旅程不产生任何 Genesis Cloud 网络请求。
- 验证断网、Cloud DNS 失败和 Genesis Cloud 不可用时，Desktop 本地核心旅程仍然完整可用。
- CLI 建立用户级与 Workspace 级本地上下文。
- 固化 AssetRef、AssetDefinition 和最小 Agent Deployment 模型。
- 固化 Package Installation、InstallRecord、SourceProvenance 与 Capability Index 的边界。
- 建立包 Digest/Provenance 校验、Manifest 权限与依赖检查、未验证来源高摩擦确认等供应链安全基线。
- 建立 Local Registry、Local Deployment 和本地能力发现。
- 建立本地文件、命令、网络和桌面能力的 `allow/ask/deny` Policy、作用域授权、前台审批和失败关闭的 Intervention Broker。
- Desktop 提供权限中心、本地活动时间线、待确认通知和紧急停止；只对具备快照或补偿契约的动作展示撤销。
- CLI/Desktop 对同一 Workspace 的写入启用 expected Hash/Freshness 校验和冲突提示；协作式跨进程锁作为增强，但正确性不能依赖内存锁。
- 普通 UI 落实内部概念到用户语言的映射，使用稳定默认 Deployment 并常驻展示本机/云端状态，只在边界变化时打断确认。
- 提供无外部副作用的新手引导、渐进式权限请求，以及 CLI/Desktop 之间不建立同步关系的一次性 Portable Asset 复制。
- 建立 MemorySpace、MemoryBinding、MemoryView 和 UserProfileStore 边界。
- 建立 BackupSnapshot、BackupManifest、加密备份、验证、恢复和覆盖前保护机制。
- 支持手动 `.gbackup`、自动写入用户指定目录和无账号换机；加入风险递进的备份提醒与“恢复为新 Profile”的完成引导，不实现通用同步。

### Phase 2：接入可选云端能力

- 建立 Genesis Cloud Identity、Cloud Connection 和安全 Token 存储。
- 为 CI/Headless 建立 Workload Identity Federation、Service Principal、最小权限 Scope 和机器可读失败语义。
- 交付 Desktop Connected 装配；未连接 Cloud Account 时与 Standalone 保持相同本地行为。
- 区分 Genesis Cloud Platform 与 Enterprise Web 产品边界。
- 建立与 Enterprise Web 共用应用服务的 Management API，以及管理 CLI/IaC/GitOps 的 validate、plan、apply、漂移和审计闭环。
- 建立 Cloud Registry、Cloud Deployment、Grant 和统一授权查询。
- CLI/Desktop 通过固定网关调用已授权的 Cloud Agent 和 Cloud Workflow Deployment。
- 支持 Cloud Tool 远程代理与 Remote MCP Connection；Tool Schema/MCP Capability 经过本地与云端双重治理后才对模型可见。
- 支持 Cloud Skill Package 显式安装到 CLI/Desktop，复用 Package Marketplace、InstallRecord、SourceProvenance 和 Capability Index。
- 完成包签名验证、发布者身份验证、信任等级和授权失效规则；信任等级不绕过运行时治理。
- 建立 ResourceRef/InputRef staging 与 ArtifactRef 返回协议，禁止跨端传递宿主机绝对路径。
- 建立云端调用的流式事件、Trace、Audit、Usage 和错误恢复协议。
- 建立客户端—云端协议版本协商、兼容窗口、弃用与 `upgrade_required` 契约。
- 建立预算、配额、限流、排队和费用的结构化错误与用户可感知体验，禁止静默改变成本或数据位置。
- 完成 Token 过期、权限撤销、离线、缓存失效和多 Cloud Connection 显式选择。
- 支持本地 AssetDefinition 显式发布为新的云端资产版本。
- 支持默认关闭的端到端加密 Genesis Cloud Backup，包括配额、设备、保留、删除和恢复密钥流程。

### Phase 3：扩展本地化部署与协作能力

- 完善 Tool、MCP 和 Workflow 的多 Deployment 管理、兼容性检查和策略选择。
- 支持受策略控制的 Cloud Agent/Workflow 显式克隆，并创建独立的 Local AssetDefinition 与 Local Deployment。
- 支持受信任 Tool/MCP Package 显式安装、本地启用和 Local Deployment/Connection 创建。
- 完善 Workflow 跨 Deployment 的 Execution Planner。
- 支持企业知识库、团队 MemorySpace 和受治理的多 Agent 协作。
- 建立独立 Managed Data Domain、加密密钥、数据驻留/Cache/离线/DLP 策略，并严格隔离个人 Profile 的存储、备份和擦除范围。
- 按明确需求接入 OS/MDM 管理；只有 Genesis 设备服务确有必要时才引入独立 Device Identity，不上传 Local Profile ID 充当设备身份。
- 对受管数据支持经 MDM 或 Device Gateway 审计执行的撤权、密钥销毁和定向擦除；Genesis Cloud 不承担个人 Profile 或整机远程擦除。
- 完善持久化 Intervention、多设备通知以及针对具有补偿契约动作的受控恢复体验。
- 如有明确产品需求，再为特定数据类型设计单独的同步协议。
- 如有明确产品需求，再设计 Cloud Runtime 调度本地设备的安全协议。

## 15. 最终产品边界

- CLI 是无需云端即可完整使用、连接云端后获得增量能力的开发者工具。
- Desktop 是以 Local Profile 为数据根、以 Local Runtime 为执行根、可选 Profile Lock、可选连接云端的个人 Agent OS；默认不要求本地账号登录。
- Desktop 默认服务单一可信操作者；多 Profile 是个人上下文隔离，不是多用户系统，团队多用户能力属于 Genesis Cloud 与 Enterprise Web。
- Enterprise Web 是 Genesis Cloud 上的企业工作空间和治理入口。
- Genesis Cloud Platform 是可选云端能力平台，不是 CLI/Desktop 本地运行的基础依赖。
- 三端保持领域语义和安全状态一致，但交互分别服务脚本化、个人长期使用和企业治理；本机/云端边界保持可见，稳定默认值避免让用户每次手动路由。
- 本地和云端能力遵守同一 Policy、Approval、Intervention 与 Audit 语义；无交互审批通道时失败关闭，Desktop 以本地活动时间线提供可见、可控、可追溯的自主执行体验。
- AssetDefinition、Installation、Deployment、Grant 和 AssetRef 分别解决“是什么、物化了什么、在哪里运行、谁能使用、如何引用”。
- MemorySpace 和 MemoryBinding 决定记忆的数据域与权限，Agent 只能消费只读 MemoryView。
- Desktop 无账号时仍支持加密备份、恢复和换机；Cloud Account 只增加可选的端到端加密云备份。
- Backup 是只读恢复快照，不是多设备状态同步；恢复默认创建新的 Local Profile。
- Agent、Workflow 和 Cloud Tool 默认远程调用；MCP 按 Connection 选择本地或远程；Skill Package 可以显式安装到本地，但 Skill 仍不直接执行。
- Cloud Account、Workload Identity、Device Identity 和 Local Profile 相互正交；CI 不需要伪装成人类账号，企业设备也不复用 Local Profile ID。
- 企业可以通过 Management API 和 GitOps 管理云端治理及明确登记的 Managed Data Domain，但不能借此读取或擦除个人 Local Profile。
- 签名与发布者信任证明来源而非安全，只能优化安装体验，不能取代运行时权限和沙箱。
- Cache、Install、Clone、Publish/Copy 和 Sync 是不同产品能力；默认不做自动下载、隐式上传和通用双向同步。
