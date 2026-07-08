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
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\skillMarketplace\schema.ts`
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\skillMarketplace\sources.ts`
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\skillMarketplace\marketplaces.ts`
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\skillMarketplace\plugins\install.ts`
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\skillMarketplace\plugins\resolve.ts`
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\services\pluginValidation\marketplace.ts`
- `D:\workspace\go\go-project\Kode-CLI\apps\cli\src\ui\screens\overlays\PluginsScreen.tsx`

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
| Marketplace Source | 支持 local path、GitHub owner/repo、URL/zip/json，下载到 cache 后读取 manifest | 采用 source 抽象，但 Genesis 要增加 allowed source、签名/校验、approval/audit |
| Manifest 校验 | `.kode-plugin/marketplace.json` 中 plugin source/skills/commands 必须是安全相对路径 | 采用 safe relative path、禁止 `..`、禁止绝对路径、安装前验证 SKILL.md |
| 安装状态 | `installed-skill-plugins.json` 记录 plugin、marketplace、scope、enabled、installedAt | 采用 InstallRecord 模型；CLI/Desktop 用文件，Enterprise 用 DB 发布/绑定记录 |

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
- `D:\workspace\go\go-project\codex\codex-rs\config\src\config_requirements.rs`
- `D:\workspace\go\go-project\codex\codex-rs\config\src\marketplace_edit.rs`
- `D:\workspace\go\go-project\codex\codex-rs\config\src\requirements_layers\stack_tests.rs`
- `D:\workspace\go\go-project\codex\codex-rs\app-server\README.md`
- `D:\workspace\go\go-project\codex\codex-rs\app-server-protocol\src\protocol\common.rs`

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
| Marketplace allowed sources | requirements 层可限制 marketplace 来源、Git URL、host pattern、本地路径 | Genesis 必须把第三方安装纳入 Policy/Profile，而不是任意 URL 下载 |
| Marketplace config edit | 用户 marketplace 写入配置，记录 source、ref、last_revision、sparse_paths | Genesis 采用 MarketplaceRegistry + InstallStore，记录版本、commit、hash、来源和审计字段 |
| Plugin/App catalog RPC | `plugin/list`、`plugin/installed`、`plugin/read`、`app/list` 分离发现、已安装、详情和应用目录 | Genesis 技能广场也要分离 list、installed、detail，保留 featured、availability、load errors |

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
  marketplace/
    model/                # Marketplace / SkillPackage / InstallRecord 通用模型
    contract/             # MarketplaceRegistry / PackageInstaller / InstallStore 契约
    service/              # 解析、校验、安装编排；不直接碰网络、本地 FS、DB

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
- 第三方 Skill 安装属于产品侧管理能力，不属于 Agent Runtime 主循环；`internal/capabilities/package/marketplace` 只放模型、契约和产品无关编排，不直接 `git clone`、HTTP 下载、写本地目录或查企业 DB。
- CLI/Desktop 的 GitHub/URL/local 安装适配放 `products/<product>/internal/skill` 或 `shared/local/skillmarket`；Enterprise 的 marketplace、审核、发布、绑定记录放 `products/enterprise/internal/skill`。
- 安装后的 Skill 仍通过 Source 暴露给运行时；不要让 runtime 根据安装路径自行读取文件。

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
| `dependencies` | 否 | MCP/tool/connection 依赖，用于加载前依赖校验、安装提示和审批策略 |

企业 DB 技能也应映射到同一模型。DB 中可以拆成 `metadata_json` + `body` + `assets` 表，但对 runtime 暴露统一 `SkillMetadata` / `ReadSkill`。

### 3.4 第三方 Skill 分发与安装模型

Genesis 必须支持从 GitHub、第三方技能仓库、企业内部仓库安装 Skill，但安装能力不能污染运行时边界。推荐采用四层模型：

```text
MarketplaceSource
  -> MarketplaceManifest
  -> SkillPackage / PluginPackage
  -> InstallRecord / SkillSource
```

核心对象：

| 对象 | 职责 | 存储位置 |
| --- | --- | --- |
| `MarketplaceSource` | 描述来源：`github`、`git`、`url`、`file`、`directory`、`enterprise` | Profile / 产品配置 / 企业 DB |
| `MarketplaceManifest` | 描述 marketplace 名称、所有 package、版本、owner、签名、hash | 第三方仓库或企业 DB |
| `SkillPackage` | 一个可安装包，包含 skills、commands、assets、dependencies、version | marketplace manifest |
| `InstallRecord` | 记录安装结果、scope、enabled、source ref、commit/hash、installed_at | CLI/Desktop 文件；Enterprise DB |
| `InstallStore` | 管理安装状态，不参与 runtime source 读取 | 产品侧实现 |
| `InstalledSkillSource` | 把已启用安装包转换成 `skillcontract.Source` | 产品 bootstrap 注入 |

Manifest 建议格式：

```json
{
  "schema": "genesis.skill.marketplace.v1",
  "name": "office-skills",
  "description": "Office document skills",
  "version": "1.0.0",
  "owner": { "name": "Genesis", "url": "https://example.com" },
  "packages": [
    {
      "name": "document-skills",
      "version": "1.2.0",
      "source": "./",
      "skills": ["./skills/pdf", "./skills/docx"],
      "commands": [],
      "dependencies": {
        "tools": ["read_file", "write_file"],
        "mcp": [],
        "connections": []
      }
    }
  ]
}
```

安装输入支持：

| 输入 | 示例 | 说明 |
| --- | --- | --- |
| GitHub shorthand | `owner/repo[@ref][#path]` | 默认下载 zip，不要求本机 git |
| GitHub explicit | `github:owner/repo@v1.0.0#marketplace` | 推荐用于可审计安装 |
| Git URL | `git:https://host/org/repo.git@ref#path` | 企业可通过 allowed source 限制 host |
| URL | `url:https://host/marketplace.zip` 或 `.json` | 默认需要 approval；Enterprise 默认禁用公网 URL |
| Local directory | `dir:D:\skills\marketplace` | CLI/Desktop 开发态使用 |
| Local file | `file:D:\skills\marketplace.json` | 只作为 manifest，不复制任意路径 |
| Enterprise catalog | `enterprise:<catalog>/<package>` | 只由 Enterprise Source/DB 实现 |

安全规则：

- 所有 manifest 中的 `source`、`skills`、`commands`、`assets` 必须是 `./...` 相对路径，禁止绝对路径、反斜杠、`..`、软链接逃逸。
- 安装前必须读取并校验每个 `SKILL.md` frontmatter，`name` 必须与目录一致，description 必填且不超过 1024 字符。
- GitHub/URL 下载必须进入临时目录，验证通过后原子替换 cache/install 目录；失败要清理临时目录。
- 安装记录必须保存 `source_type/source/ref/path/commit_or_revision/content_hash/installed_at/enabled/scope/product`。
- 第三方来源默认不应被视为 trusted；加载含外部依赖、脚本、command dependency 的 Skill 时仍必须走 `Approval` 和 `ToolGateway`。
- `allowed-tools` 和 `dependencies` 只能声明需求或收窄工具，不自动扩大产品 Profile 授权。
- Enterprise 中第三方 Skill 必须先进入 pending/reviewed/published 状态，运行时只读取 published version。

### 3.5 技能广场与 Marketplace 边界

技能广场不是新的运行时来源，也不等于 marketplace。`Marketplace` 表示一个可刷新、可校验、可安装的分发来源；技能广场是聚合多个 marketplace、企业 catalog 和本地安装状态后的产品视图。它负责展示、搜索、筛选、详情、安装状态和治理信息；真正安装仍走 marketplace installer，真正运行仍走 SkillService。

参考 Kode 与 Codex 后，Genesis 采用三层分工：

```text
SkillCatalogView
  -> SkillCatalogCard / SkillDetailView
  -> InstallAction / EnableAction / GovernanceAction
```

核心原则：

- `Marketplace` 是来源管理概念，负责 add/update/remove、manifest、hash、signature、allowed source。\n- `技能广场` 是产品展示概念，负责 list/search/show、分类、featured、安装状态、治理状态。\n- 技能广场中的条目不直接等于可信来源；每个条目仍要标记 source、scope、trust、signature、install_policy。
- Desktop 和 Enterprise 需要完整技能广场 UI；CLI 也需要命令行入口，用于无 GUI 环境、脚本化安装、服务器开发机和 CI 预装。
- CLI 不暴露 `store` 作为一级命令；CLI 使用 `genesis skill list/search/show` 表达技能广场的只读发现能力，用 `genesis skill marketplace ...` 管理分发来源。
- Desktop 复用同一 catalog service，提供 Discover、Installed、Marketplaces、Errors 这类视图；UI 不直接下载或写本地目录。
- Enterprise 技能广场分为用户可见目录与管理员治理台：普通用户只能看到 published 且被绑定/授权的技能，管理员可以导入、审核、发布、下架和绑定。
- Catalog 查询结果必须能表达安装状态：`not_installed / installed / enabled / disabled / update_available / blocked / pending_review`。
- Catalog 查询结果必须能表达治理状态：`trusted / untrusted / signature_missing / signature_invalid / disabled_by_admin / requires_approval / unavailable`。
- Catalog 查询失败不应破坏已安装技能加载；远程目录失败要进入 `marketplaceLoadErrors` / audit，而不是让本地 installed catalog 全部不可用。

建议模型：

```go
type SkillCatalogCard struct {
    ID             string
    Name           string
    DisplayName    string
    Description    string
    Marketplace    string
    Package        string
    Version        string
    Categories     []string
    Tags           []string
    Featured       bool
    InstallState   InstallState
    Governance     GovernanceState
    Trust          TrustState
    Dependencies   SkillDependencies
    UpdatedAt      time.Time
}

type SkillCatalogQuery struct {
    Product       string
    Scope         string
    Query         string
    Categories    []string
    InstalledOnly bool
    FeaturedOnly  bool
    IncludeErrors bool
    Limit         int
    Cursor        string
}
```

### 3.6 CLI 技能发现与安装命令

CLI 需要技能发现和安装命令，但不需要暴露 `store` 这个额外概念。原因不是为了替代 Desktop/Enterprise 的技能广场 UI，而是保证核心能力在 headless 环境可用，也方便自动化配置开发机和项目级技能。

推荐命令分三组：`list/search/show` 面向发现，`marketplace` 面向来源管理，`install/enable` 面向本地安装状态。

```powershell
genesis skill list [--query <text>] [--category <name>] [--featured] [--installed] [--json]
genesis skill search <query> [--json]
genesis skill show <skill-or-package> [--marketplace <name>] [--json]

genesis skill marketplace list [--json]
genesis skill marketplace add <source> [--name <name>] [--trust-session] [--json]
genesis skill marketplace update [<name>] [--json]
genesis skill marketplace remove <name>

genesis skill install <package>@<marketplace> [--scope user|project] [--version <version>] [--json]
genesis skill installed [--scope user|project|all] [--json]
genesis skill enable <package>@<marketplace> [--scope user|project]
genesis skill disable <package>@<marketplace> [--scope user|project]
genesis skill uninstall <package>@<marketplace> [--scope user|project]
```

命令行为：

- `list/search/show` 只读目录，不安装；默认可显示 marketplace load errors 的摘要，`--json` 输出完整错误数组。
- `marketplace add/update/remove` 会触发 Approval；URL/GitHub/Git 来源必须经过 AllowedSourcePolicy。
- `install` 安装前展示 package、marketplace、version/hash、skills、commands、dependencies 和 requested tools；非交互环境必须支持 `--json` 输出明确错误，不隐式同意。
- `installed` 只读取 InstallStore，不强制刷新远程目录，避免网络失败影响本地状态查询。
- `enable/disable/uninstall` 修改安装状态后，CLI bootstrap 下一次构建 SkillService 时只注入 enabled packages。
- 项目级安装必须记录 `project_path`，并只写当前 workspace 的 `.genesis` 目录。

参考取舍：

- Kode 的 `plugin marketplace add/list`、`plugin install`、`enable/disable` 命令可借鉴；Genesis 命名使用 `skill` 作为顶层，避免把 Skill 与未来完整 Plugin Runtime 混淆。
- Kode TUI 的 Discover/Installed/Marketplaces/Errors 适合 Desktop/CLI TUI 借鉴，但 Go CLI 第一轮先做非交互命令。
- Codex 的 `plugin/list`、`plugin/installed`、`plugin/read` 分离很重要：Genesis 也应分离“远程/广场发现”和“本地已安装状态”。
- Codex 的 `featuredPluginIds`、`marketplaceLoadErrors`、`availability/install_policy` 适合吸收，避免技能广场 UI 只能看到成功路径。


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
- 支持 `genesis skill marketplace add <source>`、`genesis skill install <package>@<marketplace> --scope user|project`、`enable/disable/uninstall/list/update`。
- 第三方 marketplace cache 放用户配置目录，例如 `%USERPROFILE%\.genesis\plugins\marketplaces/<marketplace>`；安装记录放 `%USERPROFILE%\.genesis\installed-skill-packages.json` 或等价文件。
- project scope 安装只写 `<workspace>\.genesis\skills` / `<workspace>\.genesis\plugins`，并记录 `project_path`，避免跨项目误用。

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
- Desktop 与 CLI 复用 marketplace/installer 契约，但 UI 只调用产品内部 service；安装下载、路径校验和 cache 写入仍在产品/本地适配层，不进入 Wails 组件。
- Desktop 可增加“信任的 marketplace”管理界面，但最终策略仍写入产品配置并进入 approval/audit。

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
- Enterprise 不直接从公网 GitHub 安装到运行时；第三方 marketplace 应先进入企业管理流程：import -> validate -> security review -> publish -> bind tenant/project/agent。
- 企业安装记录必须带 `tenant_id`、`publisher_id`、`reviewer_id`、`source_revision`、`content_hash`、`published_version`、`status`。
- 运行时只读取 published SkillSource；draft/pending/rejected 不能进入 Agent catalog。

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
| 添加 GitHub/URL marketplace | ask；Enterprise 默认 deny，除非来源在 allowed sources |
| 安装 third-party package | ask，并展示 package、marketplace、版本、hash、skills、commands、dependencies |
| 更新 marketplace 或 package | ask；如果 source/ref/hash 变化必须重新校验并记录审计 |

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


### 6.5 Marketplace 来源治理

第三方安装必须先过来源策略，再进入下载/校验/安装流程：

```text
MarketplaceSource
  -> AllowedSourcePolicy
  -> Download/Import Adapter
  -> Manifest Validator
  -> Package Installer
  -> InstallStore
  -> SkillSource
```

AllowedSourcePolicy 建议支持：

| 策略 | 示例 | 说明 |
| --- | --- | --- |
| allow github repo | `github:owner/repo` | 只允许指定仓库 |
| allow git host | `host_pattern: ^github\.example\.com$` | 企业内网 Git 服务 |
| allow local path | `path: D:\trusted\skills` | CLI/Desktop 开发态 |
| deny url | `url:*` | 默认拒绝任意公网 URL |
| require ref | `require_ref: true` | 禁止安装 floating main/master |
| require hash/signature | `require_signature: true` | 企业发布必选 |

`MarketplaceSource` 不等于可信来源。即使来源被允许，具体 Skill 在加载和执行时仍必须经过 `SkillPolicy`、`Approval`、`ToolGateway`、`RBAC` 和 `Audit`。

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
- `skill.marketplace.add`
- `skill.marketplace.update`
- `skill.package.install`
- `skill.package.enable/disable/uninstall`
- `skill.package.validate.fail`

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
| `marketplace.name/source/ref/revision` | 第三方来源追踪 |
| `skill.package/version/hash/signature_status` | 安装包审计 |
| `install.scope/status` | user/project/tenant/org/agent 与 enable/disable 状态 |

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

第三方安装第二轮（不涉及 Enterprise DB 运行时）：

1. `internal/capabilities/package/marketplace/model`：MarketplaceSource、MarketplaceManifest、SkillPackage、InstallRecord。
2. `internal/capabilities/package/marketplace/contract`：SourceFetcher、ManifestValidator、Installer、InstallStore、AllowedSourcePolicy。
3. `shared/local/skillmarket`：local directory/file、GitHub zip、URL zip/json adapter；所有下载进入 temp dir，验证后原子落盘。
4. `products/cli/internal/skill`：`genesis skill list/search/show`、`marketplace add/list/update/remove` 与 `install/installed/enable/disable/uninstall`。
5. CLI bootstrap 把已启用 installed skill roots 作为 plugin scope Source 注入 SkillService。
6. tests：source parser、safe relative path、zip slip、防软链接逃逸、重复 package、ambiguous package、安装状态、enable/disable、审计事件。

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
| marketplace manifest/schema | `Kode-CLI\apps\cli\src\services\skillMarketplace\schema.ts` |
| marketplace source parser/download/cache | `Kode-CLI\apps\cli\src\services\skillMarketplace\sources.ts` 与 `marketplaces.ts` |
| skill package install 状态 | `Kode-CLI\apps\cli\src\services\skillMarketplace\plugins\install.ts`、`pluginState.ts`、`paths.ts` |
| plugin spec 冲突解析 | `Kode-CLI\apps\cli\src\services\skillMarketplace\plugins\resolve.ts` |
| marketplace 安全校验 | `Kode-CLI\apps\cli\src\services\pluginValidation\marketplace.ts` |
| marketplace allowed sources | `codex\codex-rs\config\src\config_requirements.rs` 与 `requirements_layers\stack_tests.rs` |
| marketplace config edit | `codex\codex-rs\config\src\marketplace_edit.rs` |
| 技能商店 UI / Discover 列表 | `Kode-CLI\apps\cli\src\ui\screens\overlays\PluginsScreen.tsx` |
| marketplace/plugin RPC | `codex\codex-rs\app-server\README.md` 与 `app-server-protocol\src\protocol\common.rs` |

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
| 是否兼容 Kode/Codex skill 格式 | 兼容核心 `SKILL.md` frontmatter；Marketplace 采用精简且受治理的安装模型，不照搬全部插件 runtime |
| 是否需要 watcher | CLI/Desktop 需要本地 watcher；Enterprise 用 DB/event 失效 |
| 是否要审计 | 必须，尤其 Enterprise 要记录版本、租户、调用来源 |
| 是否支持第三方 Skill 安装 | 支持 GitHub/URL/local/enterprise catalog，但必须经过 allowed source、manifest 校验、approval、install record 和审计 |
| CLI 是否需要技能广场命令 | 需要，但不暴露 `store` 概念；CLI 提供 headless 的 list/search/show/install/installed/enable/disable/uninstall，Desktop/Enterprise 提供图形化或管理台体验 |

## 十二、实现情况审计与进度对照表

### 审计元数据 (Audit Meta)
- **参考文档 (Reference Source)**: [Skills设计.md](file:///d:/workspace/go/genesis-agent/docs/Skills设计.md)
- **被审计代码 (Implementation Inspected)**:
  - 核心能力模型: [model.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/model/model.go)
  - 契约接口: [service.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/contract/service.go), [source.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/contract/source.go)
  - 解析器: [markdown.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/parser/markdown.go)
  - 服务实现: [service.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/service/service.go)
  - 工具集: [load_skill/tool.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/tool/load_skill/tool.go), [read_skill_resource/tool.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/tool/read_skill_resource/tool.go), [search_skill_resources/tool.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/tool/search_skill_resources/tool.go)
  - 本地适配实现: [source.go](file:///d:/workspace/go/genesis-agent/shared/local/skill/source.go), [watcher.go](file:///d:/workspace/go/genesis-agent/shared/local/skill/watcher.go)
  - CLI产品装配: [container.go (CLI)](file:///d:/workspace/go/genesis-agent/products/cli/bootstrap/container.go), [default_profile.go (CLI)](file:///d:/workspace/go/genesis-agent/products/cli/internal/profile/default_profile.go)
- **审计假设 (Assumptions)**:
  - 假设 Desktop 与 Enterprise 产品的技能对接属于后续 Phase 2 的独立迭代，本期未集成代码符合预期。

### 进度对照表 (Progress Comparison Table)

| 需求 / 设计点 | 参考设计章节 | 实现代码证据 | 状态 | 差异与备注 / Gaps |
| :--- | :--- | :--- | :---: | :--- |
| **3.1 目录结构** | 三、Genesis 目标架构 | `internal/capabilities/skill/`, `shared/local/skill/` | `Implemented` | 完全符合设计分层，无越界依赖。 |
| **3.2 核心模型** | 三、Genesis 目标架构 | [model.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/model/model.go) | `Implemented` | Scope、Metadata、Catalog、Injection 等结构体均已就绪。 |
| **3.3 Skill 格式 (YAML frontmatter)** | 三、Genesis 目标架构 | [markdown.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/parser/markdown.go) | `Implemented` | 支持 `allowed-tools`、`context`、`dependencies` 等字段的结构化解析。 |
| **4.2 Source 接口** | 四、加载与注入链路 | [source.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/contract/source.go) | `Implemented` | 抽象了 `Source` 及可选的 `Watcher` 契约。 |
| **4.3 Service 职责** | 四、加载与注入链路 | [service.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/service/service.go) | `Implemented` | 实现了多 Source 聚合、按 Scope/Product 过滤、 qualified_name 冲突解决、缓存及 Prompt 渲染等功能。 |
| **4.4 Prompt 注入** | 四、加载与注入链路 | [container.go:L168-174](file:///d:/workspace/go/genesis-agent/products/cli/bootstrap/container.go#L168-174) | `Implemented` | CLI 启动时通过 `promptbuilder.ContextInjector` 注入 `skills_instructions` 到系统提示词。 |
| **4.5 Skill Tool (`load_skill`)** | 四、加载与注入链路 | [load_skill/tool.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/tool/load_skill/tool.go) | `Implemented` | 实现对缺失依赖工具校验、对外部依赖发起 Approval、并在审批通过后进行 inline 注入。 |
| **辅助工具 (`read_skill_resource`, `search_skill_resources`)** | 四、加载与注入链路 | [read_skill_resource/tool.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/read_skill_resource/tool.go), [search_skill_resources/tool.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/search_skill_resources/tool.go) | `Implemented` | 支持只读/搜索 Skill 包下 references/assets/scripts，带有强安全边界限制。 |
| **5.1 CLI 产品适配 (本地 Roots, Watcher, Extra)** | 五、多产品策略 | [container.go:L233-298](file:///d:/workspace/go/genesis-agent/products/cli/bootstrap/container.go#L233-298), [source.go](file:///d:/workspace/go/genesis-agent/shared/local/skill/source.go) | `Implemented` | 扫描 `.genesis/skills`、`~/.genesis/skills` 及自定义 extra 目录；通过本地轮询 watcher 热重载缓存。 |
| **5.2 Desktop 产品适配** | 五、多产品策略 | 无对应代码文件 | `Unimplemented` | 待 Desktop Phase 2 开启时开发。 |
| **3.4/3.5/3.6 第三方 Skill 分发、技能广场与 CLI 命令** | 三、Genesis 目标架构；九、落地顺序 | 设计已写入本文档；无 marketplace/catalog 代码目录 | `Planned` | 下一轮实现 marketplace/catalog model/contract/service、shared/local/skillmarket 与 CLI 管理命令；外部 registry adapter 后续按需评估。 |
| **5.3 Enterprise 产品适配** | 五、多产品策略 | 无对应代码文件 | `Unimplemented` | 待 Enterprise Phase 2 开启时开发，涉及 DB 存储、RBAC 校验与 Admin API。 |
| **6.1-6.4 权限、依赖与安全** | 六、权限、依赖与安全 | [load_skill/tool.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/tool/load_skill/tool.go), [source.go](file:///d:/workspace/go/genesis-agent/shared/local/skill/source.go) | `Implemented` | 采用 `isWithin` 防止文件路径越界越权；使用通用 `Approval` 机制对高风险依赖进行授权拦截。 |
| **七、八、缓存、一致性与审计/Usage** | 七、缓存、并发与一致性; 八、审计与目标 | [service.go](file:///d:/workspace/go/genesis-agent/internal/capabilities/skill/service/service.go) | `Implemented` | 实现 Catalog 级并发安全读写缓存；上报 `audit` 与 `usage` 进行运行监控审计。 |

### 审计反思 (Gap Reflections)
- **实现偏差 / 缺漏 (Implementation Gaps)**:
  - 第一阶段（CLI 产品及其宿主机环境、内置 Skills 服务与核心 Parser）已完整落地，测试覆盖齐全。
  - 第三方 Skill marketplace / install 尚未实现，本次只完成设计补充；后续按“第三方安装第二轮”落地。
  - Desktop 和 Enterprise 特色功能仍处于未实现状态，与现阶段项目定位符合。
- **文档与设计偏差 (Design/Document Gaps)**:
  - 代码中为避免类型定义冗余，将 `SkillScope` 简写为了 `Scope`，`SkillMetadata` 简写为了 `Metadata`，属于 Go 语言包内命名惯例优化。
  - `allowed-tools` 等在 Frontmatter 的定义已被完全支持解析并落入依赖校验。
- **架构或流程漂移 (Architectural or Process Drift)**:
  - 无明显架构漂移。宿主机相关逻辑均独立收纳于 `shared/local/skill`，没有反向侵入 `internal/capabilities`，符合目录原则。

### 后续规划与行动项 (Actionable Next Steps)
1. **Desktop 技能对接 (Priority Medium)**:
   - **具体工作**: 在 `products/desktop/internal/skill` 下装配本地 Source；结合 Wails GUI 暴露 Skills 管理。
   - **验收标准**: GUI 可正确扫描并列出 Skills，具备启用/禁用开关；本地文件变更时触发 GUI 列表热重载。
   - **验证方式**: Desktop App 本地编译及运行功能测试。
2. **Enterprise 技能对接 (Priority High)**:
   - **具体工作**:
     - 在 `products/enterprise/internal/skill` 实现 PostgreSQL repository (表结构包括 `skill`, `skill_version`, `skill_asset` 等) 以及实现 `Source` 契约接口。
     - 对接 RBAC 授权引擎以对 Scope 为 tenant, org, project, agent 的技能使用进行租户与角色过滤。
     - 提供 Admin HTTP/SSE 控制管理接口。
   - **验收标准**: 多租户技能环境隔离，不可跨租户越权加载；支持技能的版本化审计与发布。
   - **验证方式**: API 集成测试及 RBAC 访问权限校验。


