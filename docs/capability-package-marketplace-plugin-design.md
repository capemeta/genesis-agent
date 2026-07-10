# Capability、Package、Marketplace 与 Plugin 设计

> 状态：设计基线  
> 适用范围：CLI、Desktop、Enterprise 的能力安装、发现、启用、治理与运行时投影

## 一、核心结论

Genesis 应区分四个概念：

- **Capability**：运行时真正可见和可调用的能力，例如 Skill、Tool、MCP、SubAgent、Model Profile、Sandbox Profile、Template/Resource。
- **Package**：可安装、可发布、可版本化的分发单元，例如 skill-package、tool-package、mcp-package、subagent-package、plugin。
- **Marketplace**：发现 Package 的目录，负责来源、manifest、版本和可安装条目，不直接参与运行时调用。
- **Plugin**：一种组合型 Package，用来把多种 Capability、资源、依赖、UI 元数据和治理声明打包给别人安装或从别人处导入。

一句话：**运行时消费 Capability，Marketplace 发现 Package，Plugin 是组合 Package，Capability Registry 负责把已安装 Package 投影成可搜索、可过滤、可审计的能力清单。**

## 二、第一性原理

能力分发系统要解决的根问题不是“Plugin 和 Skill 谁更大”，而是：

1. 一个能力从哪里来：内置、本地安装、团队分发、企业发布、远程 Marketplace。
2. 一个能力是什么：Skill、Tool、MCP、SubAgent、Template、Script、Model Profile 等。
3. 一个能力怎么被运行时发现和使用：可搜索、可启用、可过滤、可审计。
4. 一个能力怎么被治理：来源、版本、签名、权限、产品可见性、租户/项目/角色范围。
5. CLI、Desktop、Enterprise 怎么保持统一概念，只在入口和策略上分化。

必须保持的不变量：

- 运行时不直接调用 Plugin，只调用 Skill / Tool / MCP / SubAgent 等 Capability。
- Marketplace 不直接管理运行时能力实例，只管理可安装发布单元及其 manifest。
- Plugin 内部能力物理上归属于 Plugin，逻辑上投影到 Capability Registry。
- 卸载、升级、禁用 Plugin 时，必须能追踪并影响它提供的全部 Capability。
- 产品只决定可见性、启用策略、安装入口和权限治理，不改变核心模型。

## 三、概念分层

```text
Marketplace
  -> PackageManifest
     -> Package
        -> CapabilityProjection
           -> Runtime Capability Registry
              -> Skill / Tool / MCP / SubAgent / Resource 可见与调用
```

### 3.1 Capability

Capability 是运行时原子能力。典型类型包括：

| 类型 | 说明 | 是否可被 Agent 直接使用 |
| --- | --- | --- |
| Skill | 任务流程、约束、资源和执行说明 | 是，通过 Skill Runtime / `Skill` 网关（）；skill 名永不进 function schema |
| Tool | 稳定原子动作 | 是，通过 Tool Gateway |
| MCP | 外部系统连接和远程能力 | 是，通过 MCP Runtime / Connector |
| SubAgent | 可委派的智能体角色或执行单元 | 是，通过多 Agent 编排 |
| Model Profile | 模型路由和参数配置 | 间接使用 |
| Sandbox Profile | 执行隔离策略 | 间接使用 |
| Template / Resource | 模板、脚本、样例、资产 | 间接使用 |

### 3.2 Package

Package 是可安装发布单元。它可以只包含一个 Capability，也可以包含多个 Capability。

推荐 Package 类型：

```text
skill-package
mcp-package
tool-package
subagent-package
plugin
```

Package 负责：

- manifest 声明
- 版本、来源、签名、license
- 依赖声明
- 安装范围
- 产品兼容性
- 包内资源组织
- 卸载、升级、回滚边界

### 3.3 Plugin

Plugin 是一种组合 Package，适合以下场景：

- 一组 Skills、脚本、模板需要一起发布。
- Skill 依赖某个 MCP、Tool 或 SubAgent。
- 需要统一版本、签名、权限提示、企业审批和回滚。
- 需要从别人那里导入一整套能力。
- 团队或企业要统一发布一个受控能力包。

Plugin 不应该成为运行时大对象。运行时看到的是 Plugin 投影出来的 Capability。

示例：

```text
genesis-office-plugin/
  plugin.json
  skills/
    office-word/
      SKILL.md
      references/
      scripts/
      assets/
    office-excel/
  mcp/
    microsoft-graph.example.toml
  tools/
    office-preview-adapter/
  subagents/
    document-reviewer/
  assets/
    templates/
```

### 3.4 Marketplace

Marketplace 是可发现 Package 的目录，不是运行时 Registry。

Marketplace 可以展示：

- 单个 Skill Package
- MCP Package
- Tool Package
- SubAgent Package
- Plugin Package

安装前展示的是 Package 条目；安装后才投影为 Capability。

## 四、物理归属与逻辑投影

Plugin 安装后，不应把内部 Skill、Tool、MCP 复制成失去来源关系的全局散件。

推荐方式：

```text
物理存储：
~/.genesis/capabilities/plugins/genesis-office/
  plugin.json
  skills/office-word/SKILL.md
  skills/office-excel/SKILL.md
  mcp/microsoft-graph.toml

逻辑索引：
CapabilityIndex
  type: skill
  name: office-word
  source_type: plugin
  source_package_id: genesis-office
  resource_root: plugins/genesis-office/skills/office-word
```

这样可以同时满足：

- `skill search office` 能搜到 Plugin 中的 Skill。
- `plugin show genesis-office` 能展示 Plugin 包含哪些能力。
- 卸载 Plugin 时能准确清理它提供的投影能力。
- 企业审批时能展示能力来源、版本和签名。
- 可以整体禁用 Plugin，也可以按 Capability 单独禁用。

## 五、CLI、Desktop、Enterprise 的统一与差异

三类产品应共用 Capability Core，只在入口和策略上分化。

### 5.1 CLI

- 主入口是命令行。
- 支持 `install/search/list/enable/disable`。
- 可管理 user / project scope。
- 可用于 Desktop 的预装和开发调试。
- 默认偏本地能力，但仍走统一权限和来源记录。

### 5.2 Desktop

Desktop 应支持命令行安装，但不应另起一套安装体系。

推荐方式：

```text
genesis capability install <package>
genesis desktop capability install <package>
```

两者本质都调用同一个 Capability Service。区别是后者默认带 Desktop profile / product visibility。

Desktop GUI 与 CLI 命令都写同一个 Capability Store。GUI 是普通用户主入口，CLI 是开发者、自动化、企业预装和调试入口。

### 5.3 Enterprise

- 主入口是管理后台 / API。
- 支持 tenant / org / project / user / role scope。
- 用户不一定能自行安装，可能只能申请启用。
- 强制签名、审批、审计、版本锁定和回滚。
- 默认不信任宿主机本地能力，不应依赖 `shared/local`。

## 六、推荐数据模型

```text
PackageManifest
  id
  type: plugin | skill-package | mcp-package | tool-package | subagent-package
  name
  version
  description
  source
  signature
  capabilities[]
  dependencies[]
  products[]
  permissions[]

CapabilityManifest
  type: skill | tool | mcp | subagent | model-profile | sandbox-profile | resource
  name
  path
  entrypoint
  description
  products[]
  dependencies[]

InstallRecord
  package_id
  package_type
  source
  version
  install_root
  enabled
  scope: user | project | tenant | org
  product_visibility[]
  installed_at
  updated_at

CapabilityIndex
  capability_type
  name
  source_package_id
  source_package_type
  resource_path
  enabled
  effective_scope
  products[]
  checksum
```

## 七、命令与管理视角

按能力查看：

```text
genesis skill list
genesis skill search office
genesis tool list
genesis mcp list
genesis subagent list
```

按包查看：

```text
genesis marketplace search office
genesis package install genesis-office
genesis package list
genesis plugin show genesis-office
genesis plugin disable genesis-office
```

规则：

- `skill search` 必须展示 Plugin 提供的 Skill。
- `plugin show` 必须展示 Plugin 内含的 Skills / Tools / MCP / SubAgents / Resources。
- `marketplace search` 展示可安装 Package，不展示运行时散件实例。
- `capability list` 展示当前上下文有效能力，包括内置、本地、Plugin 投影和企业发布能力。

## 八、落地顺序

1. 先把当前 Skill Marketplace 术语收敛为 Package Marketplace 的子集。
2. 增加 CapabilityIndex，把已安装 Skill Package 和 Plugin 内 Skill 都投影为 Skill Capability。
3. 保留现有 `genesis skill install` 作为兼容入口，但内部走 Package Install Service。
4. 增加 `plugin.json` manifest 和 Plugin 安装/展示/禁用语义。
5. Desktop 复用同一 Capability Store，并增加 GUI 管理入口。
6. Enterprise 增加租户级 Package 发布、Capability 启用、审批、签名和审计。

## 九、与 Office Skills 的关系

Office 能力优先以内置 Skills 落地；稳定后可以形成 `genesis-office-plugin`。

运行时使用：

```text
Skill(skill="office-word")
read_skill_resource office-word/references/validation-checklist.md
run_command python scripts/create_docx.py
```

分发与治理使用：

```text
genesis-office-plugin
  -> 提供 office-word / office-excel / office-ppt / pdf-review
  -> 声明 python / libreoffice / microsoft-graph 等依赖
  -> 提供模板、脚本、验证清单和 UI 元数据
```

最终原则：**Office Plugin 管打包和治理，Office Skill 管流程，通用 Tool 管动作，Execution Runtime 管执行。**

## 十、本轮实现结果（2026-07-04）

本轮继续把 Package Marketplace、Capability Registry 与 Tool Runtime 的边界收敛到更通用的能力域。当前关系如下：

```text
Package Marketplace
  -> 位于 internal/capabilities/package/marketplace
  -> 管理 Package / Plugin 的发现、安装、启停、卸载
  -> 安装后生成 InstallRecord
  -> 把 Package 内声明的能力投影为 CapabilityIndexRecord

Capability Registry
  -> 位于 internal/capabilities/capability
  -> 管理运行时能力索引、查询、单 Capability 启停
  -> 通过 RuntimeAdapterRegistry 把启停事件同步到具体 runtime adapter

Tool Runtime Adapter
  -> 位于 internal/capabilities/tool/adapter/capability
  -> 把 enabled tool capability 投影为 Tool Gateway 工具
  -> disabled / unregistered tool capability 在 Gateway 中 hidden，不可见、不可执行

Skill Runtime
  -> 仍通过 Skill / list_skill_resources / read_skill_resource 暴露
  -> 可见性受 Capability Registry 中 skill / skill-resource enabled 状态约束
```

### 10.1 已完成

| 项目 | 状态 | 实现证据 | 说明 |
| --- | ---: | --- | --- |
| Capability 模型迁到独立能力域 | [x] 已完成 | `internal/capabilities/capability/model/model.go` | `CapabilityType`、`CapabilityManifest`、`CapabilityQuery`、`CapabilityIndexRecord`、`Permission`、`Signature` 已从 Marketplace model 中拆出。 |
| Capability Registry 独立服务与端口 | [x] 已完成 | `internal/capabilities/capability/contract`、`internal/capabilities/capability/service` | RegistryStore、Registry、RuntimeAdapter、AdapterRegistry 已位于通用 capability 能力域。 |
| Package Marketplace 物理目录迁移 | [x] 已完成 | `internal/capabilities/package/marketplace/{model,contract,service}` | 原 `internal/capabilities/skill/marketplace` 已迁到通用 package/marketplace 能力域；Skill 能力域不再承载 Package Marketplace 物理目录。 |
| Marketplace model 收敛为 Package 语义 | [x] 已完成 | `internal/capabilities/package/marketplace/model/model.go` | Marketplace model 保留 Package、Manifest、MarketplaceRecord、InstallRecord、SourceProvenance、Product protocol；Package 内引用 capability model。 |
| Package / Plugin 安装投影 CapabilityIndex | [x] 已完成 | `shared/local/skillmarket/installer.go`、`shared/local/skillmarket/store.go` | 安装后生成 `skill`、`skill-resource`，以及 Tool/MCP/SubAgent/Resource 的安装态索引记录。 |
| 单 Capability 启停 | [x] 已完成 | `Registry.SetCapabilityEnabled`、`Service.SetCapabilityEnabled`、`capability enable/disable` | 支持按 capability id 单独启停，并同步更新 InstallRecord 中的 capability 状态；Registry 可通过 RuntimeAdapterRegistry 同步 runtime adapter。 |
| Tool Runtime Adapter | [x] 已完成（Gateway 可见性层） | `internal/capabilities/tool/adapter/capability` | enabled tool capability 可投影为 Tool Gateway 工具；disabled / unregister 后在 Gateway 中 hidden。实际外部执行器仍未接入。 |
| CLI / Tool Gateway 装配 | [x] 已完成 | `products/cli/bootstrap/container.go`、`products/cli/bootstrap/capability_tool_test.go` | CLI 启动时从 Capability Registry 加载 tool capability，并作为 AdditionalTools 进入共享 Tool Gateway。 |
| Skill 可见性接入 Capability Registry | [x] 已完成 | `skill/service.Options.Visibility`、`filterVisibleSkills`、CLI bootstrap 注入 Registry | disabled Skill capability 会从 catalog、`Skill`、prompt 注入中消失；enable 后恢复。协议边界见 `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`。 |
| Skill Resource 工具接入启用态校验 | [/] 部分完成 | `list_skill_resources.Deps.Registry`、`read_skill_resource.Deps.Registry` | 工具使用 Capability Registry 过滤/校验 enabled 的 `skill-resource`；实际内容读取仍由 Skill Source 完成。 |
| Product 装配协议 | [x] 已完成（协议层） | `CLIProtocol()`、`DesktopProtocol()`、`EnterpriseProtocol()` | CLI/desktop/enterprise 的持久化协议与 scope 结构已定义；Desktop SQLite 与 Enterprise DB 实现尚未落地。 |
| Plugin 组合 Package | [/] 部分完成 | `plugin install/list/show/enable/disable/uninstall`、`plugin.json` 解析 | Plugin 可组合 Skill/Tool/MCP/SubAgent/Resource 并进入索引；Tool 已进入 Gateway 可见性层，MCP/SubAgent 仍未接具体 runtime。 |

### 10.2 审查修复记录

本轮执行了一次 review-fix-rereview 闭环。

从第一性原理看，本轮有两个核心不变量：

1. **Package Marketplace 是分发安装域，不是 Skill 子域**：Skill 可以作为 Package 内的一类 Capability，但 Marketplace 不能继续物理挂在 Skill 能力域下。
2. **Tool Capability 的 enabled 状态必须影响 Tool Gateway 可见性**：安装态索引只是来源，运行时最终要通过 Gateway 的 list/get/execute 体现可用性。

审查发现并修复的问题：

1. **Tool Adapter Unregister 残留风险**：初版 `Unregister` 只从 adapter map 删除记录，如果工具已注册到 Tool Registry，旧 wrapper 仍可能留在 Gateway 中。已修复为 unregister 前先把 wrapper 置为 disabled/hidden，再移除 adapter map，并补充测试。
2. **路径迁移后的文档漂移**：旧文档仍写 `internal/capabilities/skill/marketplace`。已统一修正为 `internal/capabilities/package/marketplace`，并更新本文件状态表。

验证命令：

```powershell
$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'; $env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'; go test ./internal/capabilities/capability/... ./internal/capabilities/package/marketplace/... ./internal/capabilities/tool/... ./internal/capabilities/skill/... ./shared/local/skillmarket ./products/cli/internal/skill ./products/cli/internal/command ./products/cli/bootstrap
```

验证结果：通过。

## 十一、代码与文档对齐审计

### 11.1 审计范围

- 参考来源：本文第一至第九章关于 Capability、Package、Marketplace、Plugin、Desktop、Enterprise 的设计约束，以及本轮用户确认的 Marketplace 物理迁移与 Tool Runtime Adapter 需求。
- 实现范围：`internal/capabilities/capability/**`、`internal/capabilities/package/marketplace/**`、`internal/capabilities/tool/**`、`internal/capabilities/skill/service/**`、Skill Resource tools、`shared/local/skillmarket/**`、`products/cli/internal/command/**`、`products/cli/bootstrap/**`。
- 范围假设：Desktop / Enterprise 涉及 DB 的装配本轮仍不做，只保留协议结构；Tool Adapter 本轮完成 Gateway 可见性和启停闭环，不实现外部 tool 进程/脚本/MCP 执行器。

### 11.2 对照表

| 设计点 | 实现证据 | 状态 | 差距 / 说明 |
| --- | --- | ---: | --- |
| Capability 是运行时原子能力 | `capability/model`、`capability/contract`、`capability/service` | [x] 已实现 | 核心类型和 Registry 已从 Marketplace 物理域拆出。 |
| Marketplace 管 Package，不直接管运行时能力 | `package/marketplace/model.Package` 引用 `capmodel.CapabilityManifest`；安装后投影索引 | [x] 已实现 | 物理目录已迁到 `internal/capabilities/package/marketplace`。 |
| Skill 能力域不再承载 Package Marketplace | 包依赖扫描无 `internal/capabilities/skill/marketplace` import | [x] 已实现 | `skill` 能力域只保留 Skill runtime/source/tool 相关实现。 |
| CLI / Desktop / Enterprise 可依赖通用 marketplace service | CLI 已依赖 `package/marketplace/service`；协议提供 `CLIProtocol`、`DesktopProtocol`、`EnterpriseProtocol` | [/] 部分实现 | CLI 已装配；Desktop/Enterprise 还未实现 DB store/bootstrap，但不再需要依赖 Skill Marketplace 路径。 |
| Plugin 是组合 Package | `PackageTypePlugin`、`plugin.json`、CLI plugin 命令 | [/] 部分实现 | 已能安装、展示、启停、卸载；Tool 进入 Gateway 可见性层，MCP/SubAgent 尚未进入真实 runtime。 |
| Skill Package 与 Plugin 内 Skill 进入 CapabilityIndex | `Installer.projectCapabilities`、`CapabilityIndexStore` | [x] 已实现 | Skill 与 Skill Resource 都会生成索引记录。 |
| Tool / MCP / SubAgent 进入 CapabilityIndex | `CapabilityTypeTool/MCP/SubAgent`、manifest 校验、安装态投影 | [x] 已实现（索引层） | Tool 已有 Gateway adapter；MCP/SubAgent 仍未注册到对应 runtime。 |
| Tool Runtime Adapter 接入 Tool Gateway | `tool/adapter/capability.Adapter`、`buildCapabilityTools`、adapter/bootstrap tests | [x] 已实现（可见性层） | Tool capability 可进入 Gateway list/get；执行时返回“执行器未接入”的明确错误，外部执行器是后续工作。 |
| `capability disable <id>` 后 Tool 不可用 | `Adapter.SetEnabled` hidden 行为、`TestAdapterDisablesToolCapabilityInGateway` | [x] 已实现 | CLI 命令更新安装态索引；新的 CLI runtime 启动会按索引隐藏 disabled tool。进程内同步能力由 RuntimeAdapterRegistry port 支持。 |
| Skill 可见性受 Capability Registry 控制 | `filterVisibleSkills`、相关 service tests | [x] 已实现 | 无索引的本地/内置 Skill 会保留可见，避免开发态被误隐藏。 |
| Skill Resource 工具与 CapabilityIndex 闭环 | `list_skill_resources` / `read_skill_resource` 注入 Registry | [/] 部分实现 | enabled 状态和存在性通过索引校验；内容读取仍由 Skill Source 执行。 |
| Desktop CLI/GUI 复用同一协议 | `DesktopProtocol()` | [/] 部分实现 | 协议已定义，SQLite store、migration、Desktop bootstrap 未实现。 |
| Enterprise 多租户治理 | `EnterpriseProtocol()`、scope 常量 | [/] 部分实现 | 只有结构协议；DB store、tenant_id 查询约束、审批、审计、签名验证未实现。 |
| 供应链治理 | `Permission`、`Signature`、`SourceProvenance`、`ContentHash` | [/] 部分实现 | 字段、来源记录和内容 hash 已具备；签名验证、权限审批、依赖解析、升级/回滚未实现。 |

### 11.3 当前未完成项

1. **Tool Capability 外部执行器未实现**：Tool 已能进入 Gateway 可见性层，但 `runtime` / `entrypoint` 尚未绑定脚本、进程、HTTP、MCP 或内置 tool delegate。
2. **MCP / SubAgent Runtime Adapter 未实现**：MCP/SubAgent 已有 CapabilityIndex 和 adapter port，但未接 MCP Runtime、多 Agent 编排。
3. **Desktop SQLite Store 未实现**：需要 schema、migration、repository，并由 Desktop bootstrap 注入同一 Package/Capability service。
4. **Enterprise DB 与治理策略未实现**：需要 tenant/org/project/user/role scope 查询约束、审批、审计、签名验证和企业发布入口。
5. **供应链治理未闭环**：signature、permissions、products、dependencies、license 仍主要是 manifest 字段和展示基础，缺执行策略。
6. **Skill Resource 内容读取不是纯 Registry 驱动**：当前 Registry 控制可见性和启用态，内容读取仍交由 Skill Source；这是当前可接受边界。

## 十二、下一步建议

1. **实现 Tool Capability 执行器**
   - 变更范围：`internal/capabilities/tool/adapter/capability`、execution runtime、manifest runtime/entrypoint 约定、权限审批。
   - 验收标准：tool capability 可基于 manifest `runtime` / `entrypoint` 执行脚本、命令或委托内置 tool；执行走 Tool Gateway 审计、权限和调度。
   - 验证方式：脚本 tool fixture、执行成功/失败/权限拒绝测试、Gateway 审计测试。

2. **实现 MCP / SubAgent Runtime Adapter**
   - 变更范围：`internal/capabilities/capability`、MCP runtime、多 Agent 编排注册表。
   - 验收标准：安装 plugin 内 MCP/SubAgent capability 后，对应 runtime 能 list/show；`capability disable <id>` 后不可用。
   - 验证方式：adapter 单元测试、runtime 集成测试。

3. **实现 Desktop SQLite Store**
   - 变更范围：Desktop bootstrap、SQLite schema/migration、Package/Install/Capability repository。
   - 验收标准：Desktop 使用 SQLite 持久化 marketplace、install_record、capability_index；migration 幂等；GUI 与命令行读取同一安装态。
   - 验证方式：SQLite repository 单元测试、Desktop bootstrap 装配测试。

4. **补供应链治理闭环**
   - 变更范围：manifest 校验、安装服务、产品策略端口、CLI 安装确认展示。
   - 验收标准：签名失败阻断安装；权限审批失败阻断启用；products visibility 能过滤 package/capability。
   - 验证方式：manifest 签名/权限策略测试、安装失败回滚测试。
