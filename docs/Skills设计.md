# Skills 系统设计

## 一、设计目标

Skills 是可复用的任务知识包，负责把某类任务的专业流程、约束、示例、脚本和资源按需注入 Agent Runtime。Genesis 需要支持 CLI、Desktop、Enterprise 三类产品，因此 Skills 不能设计成某个产品的本地目录扫描功能，也不能直接塞进 `runtime` 或 `prompt` 里。

本设计目标：

- 统一 Skill 元数据、加载、选择、读取、注入和审计语义。
- 支持多来源：内置、本地用户、本地项目、插件包、执行环境、企业数据库、远程对象存储。
- 支持多产品：CLI/Desktop 偏本地文件和插件，Enterprise 偏 DB/租户/RBAC/审计。
- 支持渐进式上下文：默认只把技能摘要放进提示词，真正使用时再加载 `SKILL.md` 主体和引用资源。
- 保持产品边界：`internal/capabilities/skill` 只定义通用能力和服务，产品 source/审批/持久化由 `products/<product>/bootstrap` 注入。

结论：**Skill 能力应统一设计，但来源实现不统一。** 工具语义、模型、选择、注入、审计统一；CLI/Desktop/Enterprise 通过不同 `SkillSource` 或 `SkillProvider` 装配不同数据来源。

## 二、参考源码结论

### 2.1 Kode-CLI 可借鉴点

参考代码：

- `D:\workspace\go\go-project\Kode-CLI\docs\skills.md`
- `D:\workspace\go\go-project\Kode-CLI\kode-agent-sdk\src\core\skills\manager.ts`
- `D:\workspace\go\go-project\Kode-CLI\kode-agent-sdk\src\core\skills\xml-generator.ts`
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\customCommands\discovery.ts`
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\customCommands\types.ts`
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\customCommands\naming.ts`
- `D:\workspace\go\go-project\Kode-CLI\packages\tools\src\tools\interaction\SkillTool\SkillTool.tsx`
- `D:\workspace\go\go-project\Kode-CLI\packages\core\src\permissions\policies\skill.ts`

值得吸收：

| 设计点 | Kode 做法 | Genesis 取舍 |
| --- | --- | --- |
| `SKILL.md` 格式 | YAML frontmatter + Markdown body | 采用，保持生态兼容 |
| 发现阶段只读 frontmatter | `MAX_SKILL_FRONTMATTER_BYTES`，不读取完整 body | 必须采用，控制启动和上下文成本 |
| 技能名规则 | 目录名为准，`a-z0-9-`，1-64 字符 | 采用；企业 DB 也应同规则 |
| 懒加载主体 | 调用 skill 时重新读取 `SKILL.md` body | 采用，支持热更新和减少 prompt |
| 可选字段 | `allowed-tools`、`model`、`context: fork`、`agent`、`max-thinking-tokens`、`disable-model-invocation` | 模型预留，第一轮可先实现 `allowed_tools` 和 `context_mode` |
| Skill Tool | LLM 通过 `Skill` 工具显式加载技能 | Genesis 应做 `load_skill` 或 `skill` 工具，但必须走 ToolGateway/Approval |
| 权限键 | `Skill(name)` 和 `Skill(prefix:*)` | Genesis 放入通用 approval/policy，而不是工具内硬编码 |
| 插件/市场 | plugin install 后把 skills 安装到本地 | CLI/Desktop 可参考；Enterprise 不应依赖本地安装目录 |

不直接照搬：

- Kode 把 skills 与 custom commands 结合较紧，Genesis 应把 slash command 视为产品交互层，Skill 能力本身保持独立。
- Kode 的扫描实现偏本地文件系统，Enterprise DB 技能无法复用该实现，但可以复用模型和懒加载策略。
- Kode 的 `SkillTool` 会扩展消息并修改上下文；Genesis 应把注入动作放在 `skill/service` 和 runtime prompt pipeline，工具只负责触发加载。

### 2.2 Codex 可借鉴点

参考代码：

- `D:\workspace\go\go-project\codex\codex-rs\core-skills\src\model.rs`
- `D:\workspace\go\go-project\codex\codex-rs\core-skills\src\loader.rs`
- `D:\workspace\go\go-project\codex\codex-rs\core-skills\src\root_loader.rs`
- `D:\workspace\go\go-project\codex\codex-rs\core-skills\src\service.rs`
- `D:\workspace\go\go-project\codex\codex-rs\core-skills\src\config_rules.rs`
- `D:\workspace\go\go-project\codex\codex-rs\core-skills\src\render.rs`
- `D:\workspace\go\go-project\codex\codex-rs\core-skills\src\injection.rs`
- `D:\workspace\go\go-project\codex\codex-rs\ext\skills\src\provider.rs`
- `D:\workspace\go\go-project\codex\codex-rs\ext\skills\src\catalog.rs`
- `D:\workspace\go\go-project\codex\codex-rs\ext\skills\src\selection.rs`
- `D:\workspace\go\go-project\codex\codex-rs\ext\skills\src\render.rs`
- `D:\workspace\go\go-project\codex\codex-rs\core\src\context\available_skills_instructions.rs`
- `D:\workspace\go\go-project\codex\codex-rs\app-server\src\skills_watcher.rs`

值得吸收：

| 设计点 | Codex 做法 | Genesis 取舍 |
| --- | --- | --- |
| SkillScope | `user/repo/system/admin` | 扩展为 `system/admin/user/project/plugin/tenant/org/agent/session` |
| 多来源 authority | Host/Executor/Orchestrator/Custom | 采用，Enterprise DB 是 `SkillAuthority{Kind: enterprise_db}` |
| Opaque Resource ID | list/read 必须走同一 provider，不从 resource id 解析本地路径 | 必须采用，防止企业/沙箱资源越权 |
| Product filtering | skill policy 可按产品过滤 | Genesis 必须支持 `cli/desktop/enterprise` |
| Cache key | 按 cwd/config/rules/root 生成缓存，避免跨会话污染 | 必须采用，Enterprise 还要带 tenant/project/user/version |
| Disabled rules | name/path selector，后加载规则覆盖前规则 | 采用并扩展为 scope rule |
| Prompt budget | bounded skills list、正文截断 | 必须采用 |
| Explicit mention | `$skill` / `skill://...` / 结构化选择 | CLI/Desktop 可用；Enterprise API 也可用结构化 SkillSelection |
| Watcher | 文件变化清缓存并发通知 | CLI/Desktop 本地 watcher；Enterprise 用 DB version/event |
| Analytics | skill injected / implicit invocation | 接入已有 audit/usage sink |

不直接照搬：

- Codex 有 host/executor/orchestrator 复杂扩展层；Genesis 第一轮不需要全量 extension runtime，但模型要保留 authority/resource 路由。
- Codex 使用 Rust executor filesystem；Genesis 已有 `FileSystemBackend`、sandbox contract，应通过 port 适配。
- Codex 的 Product 是 Codex 专有枚举；Genesis 要用自身 `ChannelType` 和 Product Profile。

## 三、Genesis 目标架构

### 3.1 目录结构

```text
internal/capabilities/skill/
  model/
    skill.go              # SkillMetadata / SkillContent / SkillScope / SkillPolicy
    source.go             # SkillAuthority / SkillPackageID / SkillResourceID
    injection.go          # SkillCatalog / SkillSelection / SkillInjection
  contract/
    source.go             # Source/List/Read/Search 与 Watcher 契约
    service.go            # Service 接口
    parser.go             # Parser / Renderer
  service/
    service.go            # 聚合多 source、缓存、过滤、选择、注入
    selector.go           # 显式 mention、隐式匹配、冲突处理
    renderer.go           # available skills prompt fragment
  parser/
    markdown.go           # SKILL.md frontmatter + body 解析
  adapter/
    memory/               # 测试和开发态 source
    embedded/             # 内置 skills source；不绑定宿主机路径
  tool/
    load_skill/           # 通过 ToolGateway 触发技能加载

shared/local/skill/
  source.go               # CLI/Desktop 共享的本地主机 skill roots source
  watcher.go              # 本地文件 watcher；不 import products/*

products/cli/internal/skill/
  config.go               # CLI skill roots/profile/extra roots 装配配置

products/desktop/internal/skill/
  config.go               # Desktop skill roots + GUI 管理状态

products/enterprise/internal/skill/
  source.go               # DB / tenant / project / org source
  repository.go           # 企业 DB repository
  admin_api.go            # 后续 Skill 管理接口
```

边界：

- `internal/capabilities/skill` 不 import `products/*`、`shared/local/*`、Wails、PostgreSQL、HTTP handler 或宿主机本地路径实现。
- 本地主机目录扫描属于 CLI/Desktop 本地适配，放 `shared/local/skill`；具体 roots 仍由 CLI/Desktop bootstrap 注入。
- 内置 skills 可以放 `internal/capabilities/skill/adapter/embedded`，因为它不依赖产品和宿主机工作区。
- 企业 PostgreSQL repository 放 `products/enterprise/internal/skill`，通过 `skillcontract.Source` 注入。
- Skill Tool 不直接读文件、DB、HTTP、Wails；只调用 `skillcontract.Service`。

### 3.2 核心模型

```go
type SkillScope string

const (
    SkillScopeSystem  SkillScope = "system"
    SkillScopeAdmin   SkillScope = "admin"
    SkillScopeUser    SkillScope = "user"
    SkillScopeProject SkillScope = "project"
    SkillScopePlugin  SkillScope = "plugin"
    SkillScopeTenant  SkillScope = "tenant"
    SkillScopeOrg     SkillScope = "org"
    SkillScopeAgent   SkillScope = "agent"
    SkillScopeSession SkillScope = "session"
)

type SkillMetadata struct {
    ID               string
    Name             string
    QualifiedName    string
    Description      string
    ShortDescription string
    Scope            SkillScope
    Authority        SkillAuthority
    PackageID        SkillPackageID
    MainResource     SkillResourceID
    Version          string
    Enabled          bool
    PromptVisible    bool
    Policy           SkillPolicy
    Interface        SkillInterface
    Dependencies     SkillDependencies
    SourceRef        map[string]string
    UpdatedAt        time.Time
}
```

`Name` 是本地短名，`QualifiedName` 用于解决冲突，例如：

- `pdf`
- `doc-tools:pdf`
- `tenant-acme:invoice-audit`

冲突策略：

- 同一 authority + package 下 ID 唯一。
- Prompt 展示时，若短名冲突，展示 qualified name。
- 用户显式 `$pdf` 只有在当前 catalog 中唯一时才可解析；否则必须使用 `skill://...` 或 qualified name。

### 3.3 Skill 格式

本地文件标准格式：

```md
---
name: review-fix-rereview
description: 审查、修复、再审查工程产物，直到没有可执行问题。
short-description: 审查闭环
version: 1.0.0
allowed-tools:
  - read_file
  - grep
  - apply_patch
context: inline
model: inherit
allow-implicit-invocation: false
products:
  - cli
  - desktop
---

# Skill instructions
...
```

第一轮建议支持字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 必须与目录名或 DB slug 一致，`a-z0-9-`，1-64 字符 |
| `description` | 是 | 用于模型选择，最大 1024 字符 |
| `short-description` | 否 | UI 和 prompt 简短展示 |
| `allowed-tools` | 否 | Skill 激活后允许或建议的工具集合 |
| `context` | 否 | `inline/fork`，第一轮只实现 inline，fork 预留给多 Agent |
| `agent` | 否 | fork 时指定 agent 类型 |
| `model` | 否 | `inherit/quick/task/main` 或具体 route |
| `disable-model-invocation` | 否 | 禁止模型自动调用，只能 UI/用户显式触发 |
| `allow-implicit-invocation` | 否 | 是否允许隐式匹配，默认 true |
| `products` | 否 | 限定 `cli/desktop/enterprise` |
| `dependencies` | 否 | MCP/tool/connection 依赖，后续用于自动提示安装 |

企业 DB 技能也应映射到同一模型。DB 中可以拆成 `metadata_json` + `body` + `assets` 表，但对 runtime 暴露统一 `SkillMetadata` / `ReadSkill`。

## 四、加载与注入链路

### 4.1 启动装配

```text
products/<product>/bootstrap
  -> 构造 SkillSource 列表
  -> 构造 SkillService
  -> 注册 load_skill 工具
  -> 把 SkillService / PromptInjector 注入 internal/bootstrap BuildOptions
  -> React loop 每轮构建 prompt 时注入 available skills fragment
```

`internal/bootstrap` 只接收接口：

```go
type BuildOptions struct {
    ...
    SkillService skillcontract.Service
    PromptInjectors []prompt.ContextInjector
}
```

不要让 `internal/bootstrap` 自己知道 CLI skills roots、Desktop GUI 状态、Enterprise DB。

### 4.2 Source 接口

```go
type Source interface {
    Authority() model.SkillAuthority
    List(ctx context.Context, query ListQuery) (ListResult, error)
    Read(ctx context.Context, req ReadRequest) (ReadResult, error)
    Search(ctx context.Context, req SearchRequest) (SearchResult, error)
}

type Watcher interface {
    Watch(ctx context.Context, roots []WatchRoot) (<-chan ChangeEvent, error)
}
```

关键约束：

- `List` 返回 metadata，不返回完整 body。
- `Read` 必须使用 `Authority + PackageID + ResourceID`，不能要求调用方拼路径。
- `ResourceID` 是 opaque 字符串，不允许上层从里面解析本地路径、DB 主键或 URL 后自行读取。
- `Search` 面向远程/企业知识库 source；本地文件第一轮可以不实现或简单 grep metadata。
- `Watch` 不放进必选 Source，避免 Enterprise DB source 被迫模拟文件 watcher；CLI/Desktop 用本地 watcher，Enterprise 用 DB 版本号、消息总线或 SSE。

### 4.3 Service 职责

```go
type Service interface {
    Catalog(ctx context.Context, req CatalogRequest) (SkillCatalog, error)
    Resolve(ctx context.Context, req ResolveRequest) (SkillMetadata, error)
    Load(ctx context.Context, req LoadRequest) (SkillInjection, error)
    SelectForTurn(ctx context.Context, req SelectionRequest) ([]SkillMetadata, error)
}
```

Service 做：

- 聚合多个 Source。
- 按 Product/Profile/Tenant/Project/Agent/User/Role/Environment 过滤。
- 处理 enable/disable 规则。
- 处理名称冲突和 qualified name。
- 缓存 catalog snapshot。
- 生成 bounded prompt fragment。
- 读取 Skill 主体并截断。
- 写 audit/usage：catalog listed、skill loaded、skill injection success/fail、source latency、cache hit。

Service 不做：

- 不直接读 CLI 本地目录。
- 不直接查企业 PostgreSQL。
- 不直接弹 UI。
- 不直接修改 tool registry。

### 4.4 Prompt 注入

Genesis 当前 `internal/runtime/prompt.Builder` 只有 `BuildSystem(agent)`。需要扩展为可注入上下文：

```go
type BuildRequest struct {
    Agent *domain.Agent
    RunContext *runtime.RunContext
    Product profilemodel.ChannelType
    SkillCatalog *skillmodel.SkillCatalog
}

type ContextInjector interface {
    Inject(ctx context.Context, req BuildRequest) (Fragment, error)
}
```

Skills prompt fragment 使用 developer/system 级片段，内容包括：

- 可用 skill 名称、简短描述、locator。
- 使用规则：显式 `$skill` 或调用 `load_skill`。
- Progressive disclosure：不要把所有 skill body 放入初始上下文。
- 资源读取规则：后续 references/assets 必须通过对应 Source 或文件工具权限读取。

预算：

- available skills fragment 默认 8KB。
- 单个 `SKILL.md` 主体默认 8KB，超出时返回 truncated 标记。
- description 最大 1024 字符。
- catalog 按 scope 优先级 + name 排序。

### 4.5 Skill Tool

工具名建议：`load_skill`，而不是直接叫 `Skill`。原因是 Genesis 工具命名已有 snake_case，且 `load_skill` 更准确表达“加载技能内容”。

输入：

```json
{
  "name": "review-fix-rereview",
  "args": "optional freeform arguments",
  "resource": "skill://..."
}
```

行为：

- 先走 ToolGateway。
- 调用 Approval：高风险 skill、企业受控 skill、需要外部依赖的 skill 可以 Ask/Deny。
- 通过 SkillService.Resolve 找到唯一 skill。
- 通过 SkillService.Load 读取主体。
- 输出给 runtime：新增用户/开发者消息片段，或返回 `SkillInjection` 供 React loop 注入下一轮上下文。

第一轮建议实现 inline 注入。`context: fork` 后续映射到多 Agent / Task tool，不要第一轮混进来。

## 五、多产品策略

### 5.1 CLI

默认来源：

```text
system:   内置 skills
user:     %USERPROFILE%\.genesis\skills 或配置目录 skills
project:  <workspace>\.genesis\skills
plugin:   后续 CLI plugin cache
```

特点：

- 本地 watcher 可清缓存。
- 可允许用户显式 extra roots。
- Skill body 读取仍要通过 Source，不让工具直接读任意路径。
- 可以支持 legacy `.agents/skills` 或 `.codex/skills`，但第一轮不建议引入太多兼容路径，避免边界复杂。

### 5.2 Desktop

默认来源：

```text
system:   内置 skills
user:     Desktop 用户技能目录
project:  用户打开的项目目录下 .genesis/skills
plugin:   Desktop plugin cache
```

特点：

- GUI 管理 enable/disable、安装和详情。
- watcher 触发 UI skills changed notification。
- native picker 选择目录后再加入 extra roots。
- 不把 Wails 放进 `internal/capabilities/skill`。

### 5.3 Enterprise

默认来源：

```text
tenant:   租户级 DB skills
org:      组织/部门级 DB skills
project:  项目级 DB skills
agent:    Agent 绑定 skills
system:   平台内置受控 skills
```

企业端不应把 skills 设计成宿主机目录扫描。最佳实践是：

- `products/enterprise/internal/skill/repository` 读取 PostgreSQL。
- 元数据列表走 DB 查询，带 `tenant_id`、`project_id`、`enabled`、`version`。
- 主体可以存 DB，也可以存对象存储，但通过 repository/source 统一读取。
- `SkillPolicy` 接 RBAC/ABAC：租户、项目、角色、用户、Agent、环境。
- 支持版本化和发布状态：draft/published/archived。
- 支持审计：谁创建、谁发布、哪个 run 加载了哪个版本。
- 支持缓存失效：DB `updated_at/version`、事件总线、SSE 通知。

统一模型足够支持 Enterprise；不需要单独做一套 EnterpriseSkill，但 Enterprise Source 会比本地 Source 多治理字段。

## 六、权限、依赖与安全

### 6.0 Profile 与 CapabilityScope

产品 Profile 需要增加 SkillSet，但不要做成“大一统配置巨石”。建议：

```go
type SkillSet struct {
    Enabled []string
    Disabled []string
    Sources []SkillSourceRef
    AllowImplicit bool
}
```

执行时仍必须经过 CapabilityScope 过滤：product/channel、tenant、project、agent、user、role、environment 都参与判断。Profile 只声明默认启用集合和 source 引用，不直接决定最终授权。


### 6.1 Approval

Skill 加载本身也要经过通用 approval 能力：

| 场景 | 默认策略 |
| --- | --- |
| 当前产品启用、当前 scope 可见、普通 skill | allow |
| disabled skill | deny |
| `disable-model-invocation=true` 且由模型调用 | deny |
| Enterprise 受控 skill，需要角色审批 | ask/deny，取决于产品 requester |
| Skill 请求扩大工具权限 | ask |
| Skill 来源未知或签名不可信 | ask/deny |

### 6.2 Tool 依赖

`allowed-tools` 不是直接绕过权限的白名单，它只是 Skill 期望的工具能力声明。执行时仍经过：

```text
Product Profile
  -> ToolGateway
  -> Approval / RBAC
  -> ToolTraits / ResourceLock / Audit
```

Skill 可以临时收窄工具集，不应默认扩大工具集。若要扩大，需要 approval。

### 6.3 资源访问

Skill 包中的 `references/`、`scripts/`、`assets/` 必须有来源边界：

- Host 文件来源：通过 filesystem Source 的 `Read` 或文件工具读取，受 PathResolver/Approval 约束。
- Enterprise DB 来源：通过 DB Source 读取，受 tenant/RBAC 约束。
- Executor/沙箱来源：通过对应 environment resource 读取。

上层不能把 `SkillResourceID` 当本地 path 使用。

### 6.4 Scripts 与 Assets

Skill 包可以携带 `scripts/`、`assets/`、`references/`，但加载 Skill 不等于允许运行脚本：

- `references/` 默认是可读知识资源，仍通过 Source/ResourceID 读取。
- `assets/` 默认是只读资源，是否暴露给模型取决于 Source 和产品 UI。
- `scripts/` 只能作为资源被引用；真正执行必须走 `run_command` 或后续专门 script runner，并经过 execution policy、sandbox、approval、audit。
- Enterprise 中脚本类 skill 默认应禁用或 require approval，除非管理员发布并绑定受控执行环境。

## 七、缓存、并发与一致性

缓存层级：

| 缓存 | Key | 失效 |
| --- | --- | --- |
| Catalog snapshot | product + tenant + project + user + agent + roots + config rules + source version | watcher / DB version / config change |
| Skill body | authority + package + resource + version | source updated / TTL |
| Rendered prompt | catalog hash + budget + product | catalog 变化 |

并发：

- `Catalog` 可以并发调用多个 Source，但要设置 source timeout。
- 同一个 Source 内部应自行限流，避免 DB 或文件系统被扫爆。
- Service 缓存使用 copy-on-write snapshot，避免运行中 catalog 被原地修改。
- `Load` 不持有全局写锁，只对单个 resource 做 read-through cache。

一致性：

- CLI/Desktop 本地文件允许热更新，发现阶段只读 frontmatter，加载阶段读最新 body。
- Enterprise 默认读 published version；运行中的 Run 应记录 skill version，避免审计不可追溯。
- Skill 被禁用后，新 turn 不可见；已注入到当前 turn 的内容不回滚，但后续工具权限仍可 deny。

## 八、审计与 Usage

接入已有 `audit/usage` sink，至少记录：

- `skill.catalog.list`
- `skill.resolve`
- `skill.load`
- `skill.inject`
- `skill.disabled`
- `skill.source.error`

字段：

| 字段 | 说明 |
| --- | --- |
| `skill.name` | 短名 |
| `skill.qualified_name` | 完整名 |
| `skill.scope` | scope |
| `skill.authority.kind/id` | 来源 |
| `skill.package_id` | package |
| `skill.resource_id` | 主资源 |
| `skill.version` | 企业版本或文件 hash |
| `product/channel` | CLI/Desktop/Enterprise |
| `tenant_id/project_id/user_id/agent_id` | 企业治理 |
| `invocation_type` | explicit/implicit/tool/slash |
| `cache_hit` | 是否命中缓存 |
| `truncated` | 是否截断 |

## 九、落地顺序

第一轮建议做“可用但不膨胀”的纵切：

1. `internal/capabilities/skill/model`：模型、scope、authority、resource、policy。
2. `parser/markdown`：解析 `SKILL.md` frontmatter，限制大小，校验 name/description。
3. `contract.Source`、`contract.Watcher` 和 `service.Service`：多 source 聚合、过滤、缓存。
4. `shared/local/skill`：基于注入 root 的本地主机 source，真实本地目录扫描只放这里，不放 `internal`。
5. `profilemodel.SkillSet`：产品默认启用集合和 source 引用，不承载最终授权。
6. `service.RenderAvailableSkills`：bounded prompt fragment。
7. runtime prompt builder 扩展 `ContextInjector`。
8. `load_skill` 工具：只调 SkillService，不直接读来源。
9. CLI bootstrap 注入 system/user/project source，CLI profile 开启 `load_skill`。
10. tests：parser、source、service、冲突解析、prompt budget、load_skill。

Enterprise 第二轮：

1. `products/enterprise/internal/skill/repository`。
2. DB schema：skill、skill_version、skill_asset、skill_binding、skill_audit。
3. Enterprise Source 实现 `Source`。
4. RBAC/tenant filter。
5. Admin HTTP/SSE 管理接口。

## 十、实现时必须参考的源码清单

实现前应直接对照这些文件，避免遗漏细节：

| 主题 | 参考文件 |
| --- | --- |
| frontmatter 懒解析、大小限制 | `Kode-CLI\apps\cli\src\services\customCommands\discovery.ts` |
| skill name / namespace | `Kode-CLI\apps\cli\src\services\customCommands\naming.ts` |
| `allowed-tools/context/model/agent` 字段 | `Kode-CLI\apps\cli\src\services\customCommands\types.ts` |
| Skill tool 行为 | `Kode-CLI\packages\tools\src\tools\interaction\SkillTool\SkillTool.tsx` |
| Skill 权限前缀 | `Kode-CLI\packages\core\src\permissions\policies\skill.ts` |
| 多 scope metadata | `codex\codex-rs\core-skills\src\model.rs` |
| roots 加载和产品过滤 | `codex\codex-rs\core-skills\src\loader.rs` |
| 多 root 合并排序 | `codex\codex-rs\core-skills\src\root_loader.rs` |
| cache / extra roots / bundled skills | `codex\codex-rs\core-skills\src\service.rs` |
| enable/disable rules | `codex\codex-rs\core-skills\src\config_rules.rs` |
| explicit mention 选择 | `codex\codex-rs\core-skills\src\injection.rs` 与 `ext\skills\src\selection.rs` |
| bounded available skills prompt | `codex\codex-rs\ext\skills\src\render.rs` |
| authority/resource provider 边界 | `codex\codex-rs\ext\skills\src\provider.rs` 与 `catalog.rs` |
| skills changed watcher | `codex\codex-rs\app-server\src\skills_watcher.rs` |

## 十一、关键决策

| 决策点 | 结论 |
| --- | --- |
| Skills 是否三端统一 | 统一模型、服务、选择、注入；来源实现按产品隔离 |
| Enterprise 是否用本地目录 | 不用，默认 DB/source adapter |
| Skill Tool 是否直接读文件 | 不能，只能调用 SkillService |
| Skill 是否默认加载完整内容 | 不能，必须 progressive disclosure |
| `allowed-tools` 是否扩大权限 | 不能自动扩大，只能声明需求，扩大需 approval |
| Skill resource 是否可被解析为 path | 不能，必须通过 authority provider 读取 |
| 是否第一轮实现 fork skill | 不建议，预留字段，后续接多 Agent |
| 是否兼容 Kode/Codex skill 格式 | 兼容核心 `SKILL.md` frontmatter，不复制全部插件市场 |
| 是否需要 watcher | CLI/Desktop 需要本地 watcher；Enterprise 用 DB/event 失效 |
| 是否要审计 | 必须，尤其 Enterprise 要记录版本、租户、调用来源 |

