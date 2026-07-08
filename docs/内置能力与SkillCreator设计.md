# 内置能力与 Skill Creator 设计

## 一、设计目标

Genesis 需要同时支持 CLI、Desktop、Enterprise 三类产品。Skills、Tools、MCP、Plugins 都是扩展 Agent 能力的入口，但它们的生命周期、权限边界、运行环境和治理方式不同。如果不先定义清楚“内置”和“外部”，后续很容易出现两类问题：

- 内核被业务能力污染：把 Office、CRM、搜索、企业连接等产品或场景能力硬编码进 Runtime。
- 扩展能力缺少治理：外部 Skill、MCP 或 Tool 直接获得文件、命令、网络、密钥能力，绕过审批和审计。

本文定义 Genesis 的内置能力边界，并设计一个可内置的 `skill-creator` 系统技能。目标是：

- 统一 Skills、Tools、MCP、Plugins 的职责和内外部边界。
- 平衡 CLI、Desktop、Enterprise 的统一能力模型与产品独立装配。
- 给 `skill-creator` 一个足够强但不过度绑定 Anthropic/Claude Code 的 Genesis 实现路线。
- 保持架构优雅、可治理、可扩展，并符合当前项目目录边界。

## 二、核心概念

### 2.1 Tool

Tool 是 Agent 可调用的原子能力入口。它应该稳定、少量、可审计，并由 ToolGateway 统一执行。

Tool 适合承载：

- 文件读写、搜索、补丁。
- 命令执行和交互会话。
- Skill 加载和 Skill 资源读取。
- 网络搜索、HTTP 请求等通用 I/O 能力。

Tool 不适合承载：

- 大量 Office 细粒度操作。
- 某个业务系统的完整流程。
- 产品特有的认证、UI、租户策略。
- 绕过 Runtime 的直接文件、命令、网络实现。



### 2.2 Skill

Skill 是可复用任务工作流包，遵循 Agent Skills 标准核心：`SKILL.md`、frontmatter、Markdown instructions、`scripts/`、`references/`、`assets/` 和 progressive disclosure。

Skill 适合承载：

- 专业任务流程。
- 领域约束、操作顺序、验证清单。
- 可复用脚本、模板和参考资料。
- 对通用 Tool 的使用策略。

Skill 不应该直接承载：

- 真实密钥。
- 产品级 endpoint 或租户策略。
- 绕过 ToolGateway 的执行入口。
- 需要长期运行和授权会话的外部连接。



### 2.3 MCP

MCP 是外部上下文和外部动作接入协议。它适合连接外部系统，而不是替代本地 Runtime。

MCP 适合承载：

- SaaS 和企业系统连接：GitHub、Slack、Google Drive、Microsoft Graph、Figma、Notion、CRM、ERP。
- 外部文档、数据、工具和授权上下文。
- 可独立部署、独立认证、独立升级的远程能力。

MCP 不适合承载：

- 本地文件系统默认读写。
- 本地命令执行默认入口。
- Skill 工作流本身。
- Genesis 产品私有治理策略。



### 2.4 Plugin

Plugin 是分发和组合单元。它可以包含 Skills、MCP 配置、资源、UI 元数据、依赖声明和安装信息。

Plugin 的价值是：

- 安装、卸载、启用、禁用、升级和版本锁定。
- 记录来源、作者、许可证、签名和审计信息。
- 将多个相关 Skills 和 MCP 依赖作为一个能力包发布。
- 支持团队和企业按租户、组织、项目发布统一能力。

Plugin 不是新的执行层，也不是 Skill 或 MCP 的替代品。

## 三、内置与外部的定义



### 3.1 内置能力

内置能力是随 Genesis 产品或平台发行、由 Genesis 维护和治理的能力。内置不等于默认无限授权，也不等于绕过审批。

内置能力应满足：

- 来源可信：由 Genesis 仓库、官方插件仓库或企业管理员发布。
- 版本可追踪：随产品版本或企业发布版本固定。
- 权限可声明：明确需要哪些 Tool、命令、MCP、连接和路径能力。
- 行为可审计：加载、执行、失败、外部依赖都能进入审计和 usage。
- 可被禁用：用户、项目、租户、管理员可以按策略禁用。

内置能力分三类：


| 类型        | 示例                                     | 放置建议                                                                |
| --------- | -------------------------------------- | ------------------------------------------------------------------- |
| 内置 Tool   | `read_file`、`run_command`、`load_skill` | `internal/capabilities/*/tool`                                      |
| 内置 Skill  | `skill-creator`、`plan`、基础 review skill | `internal/capabilities/skill/adapter/embedded` 或产品注入的 system source |
| 内置 MCP 配置 | 官方文档 MCP、企业内控 MCP 模板                   | 产品 bootstrap 或企业配置，不写死到 runtime                                     |




### 3.2 外部能力

外部能力是用户、项目、插件市场、第三方仓库、企业数据库、远程服务提供的能力。

外部能力应满足：

- 默认不信任或半信任。
- 必须经过来源、签名、许可证、依赖和权限检查。
- 不能因为被安装就自动获得更高 Tool 权限。
- 运行时仍走 ToolGateway、Approval、Policy、Sandbox。
- 企业版必须带租户、项目、用户、角色和审计上下文。

外部能力包括：

- 用户本地 skills：`~/.genesis-agent/cli/skills`。
- 项目 skills：`.genesis/skills`。
- 插件安装的 skills：`.genesis/installed/skills` 或用户 installed 目录。
- 外部 MCP server。
- 企业 DB 中发布的 tenant/org/project skills。
- 第三方 marketplace。



### 3.3 判定标准


| 问题                   | 内置       | 外部          |
| -------------------- | -------- | ----------- |
| 是否由 Genesis 或企业管理员发布 | 是        | 否或未知        |
| 是否随产品版本一起测试          | 是        | 不一定         |
| 是否可脱离安装流程直接可见        | 可以，但仍可禁用 | 不应          |
| 是否默认可信               | 相对可信     | 默认不可信或需分级   |
| 是否能绕过审批              | 不能       | 不能          |
| 是否能持有密钥              | 不能       | 不能          |
| 是否能声明依赖              | 可以       | 可以，但需要更严格审批 |




### 3.4 信任分级与 Scope

“内置/外部”不是二元开关，运行时应使用信任分级和 scope 共同决策。

建议分级：


| 信任级别                 | 来源                 | 默认行为                |
| -------------------- | ------------------ | ------------------- |
| `system`             | Genesis 随产品发布的内置能力 | 默认可见，可被策略禁用         |
| `admin`              | 企业管理员或机器管理员发布      | 默认按管理员策略可见          |
| `tenant/org/project` | 企业租户、组织、项目发布       | 必须带租户和项目策略          |
| `user`               | 用户本地安装或创建          | 只对当前用户可见            |
| `plugin`             | marketplace 或插件包安装 | 取决于安装 scope、签名和启用状态 |
| `session`            | 当前会话临时注入           | 会话结束后失效             |
| `unknown`            | 未登记来源或解析失败         | 默认不可用或必须人工确认        |


Scope 不是目录名，而是治理语义。无论能力来自本地文件、DB、对象存储还是远程 provider，都应该映射为统一的 `Authority + PackageID + ResourceID + Scope`，读取资源时必须回到同一个 Source，不允许上层从 resource id 反推宿主机路径。

## 四、三类产品的统一与独立



### 4.1 统一层

CLI、Desktop、Enterprise 应共享同一套核心模型：

- `SkillMetadata`、`SkillSource`、`SkillService`、`SkillInjection`。
- `Tool`、`ToolGateway`、`Approval`、`Policy`、`Usage`、`Audit`。
- `MCPServerRef`、`ConnectionRef`、`CredentialRef`。
- Capability scope：product、tenant、project、agent、user、role、environment。

统一层只表达能力契约，不表达产品默认策略。

### 4.2 产品独立层

产品独立性体现在 bootstrap 和产品 internal 中：


| 产品         | Skill 来源           | 执行环境                      | MCP/连接              | 管理体验           |
| ---------- | ------------------ | ------------------------- | ------------------- | -------------- |
| CLI        | 用户目录、项目目录、插件 cache | 本地 runner、本地沙箱、远程沙箱可选     | 本地 config、环境变量、连接配置 | 命令行安装、启用、禁用    |
| Desktop    | 用户目录、项目目录、桌面插件     | 本地 runner、Wails/native、沙箱 | 桌面密钥库、连接面板          | 图形化管理、通知、权限弹窗  |
| Enterprise | DB、对象存储、租户/组织/项目发布 | 企业沙箱、远程 executor          | 企业连接、OIDC/RBAC、集中凭据 | 管理后台、发布审批、审计报表 |




### 4.3 共同原则

- `internal/runtime` 不直接依赖产品实现、MCP client、Docker、DB、Wails。
- `internal/capabilities/skill` 不扫描具体本地目录，不访问企业 DB。
- `products/<product>/bootstrap` 决定默认启用哪些 sources、tools、MCP、profiles。
- `shared/local` 只放 CLI/Desktop 可复用的本地主机适配。
- Enterprise 不复用 `shared/local` 的本地执行作为默认生产能力。



### 4.4 Enterprise 的多级能力覆盖

Enterprise 不能只按“平台内置”或“租户内置”两层处理。一个企业用户实际看到的能力，应由多级 scope 合成：

```text
system defaults
  -> enterprise admin policy
  -> tenant policy
  -> org / department policy
  -> project / workspace policy
  -> agent app profile
  -> role policy
  -> user preference / user-installed skills
  -> session grants
```

合成原则：

- 上层可以设置默认启用、默认禁用和强制禁用。
- 下层可以在允许范围内增加用户偏好或会话授权。
- 强制禁用优先级最高，用户级配置不能绕过租户或管理员 deny。
- 用户级 Skill 可以存在于 Enterprise，但必须进入企业治理：来源、许可证、依赖、数据出境、工具权限和审计都要可见。
- 租户级 Skill 可以对租户内多个项目共享，但读取和执行仍必须带 `tenant_id`、project、agent、user、role。
- Agent App Profile 决定默认能力组合，但不直接授予超出 policy 的权限。

推荐决策模型：

```text
CapabilityCatalog = MergeSources(system, admin, tenant, org, project, agent, user, session)
VisibleCapabilities = Policy.Filter(CapabilityCatalog, subject, resource, environment)
ExecutableCapability = Approval.Authorize(VisibleCapability, action, risk)
```

这样既能支持企业统一治理，也允许用户在合规边界内安装个人或项目级 Skills。

## 五、内置 Tool 设计



### 5.1 定义

内置 Tool 是平台级原语，由 Genesis 维护。它们应该少而稳，优先围绕 I/O、安全边界和能力发现。

建议分层：

```text
internal/capabilities/<domain>/contract
internal/capabilities/<domain>/service
internal/capabilities/<domain>/adapter
internal/capabilities/<domain>/tool
```

Tool 只依赖 contract/service，不直接绑定产品实现。

### 5.2 内置 Tool 清单策略

内置 Tool 分为基础、可选和受控三类：


| 类别      | 示例                                                     | 默认策略                       |
| ------- | ------------------------------------------------------ | -------------------------- |
| 基础只读    | `read_file`、`list_dir`、`grep`、`search_skill_resources` | 可按 workspace scope 允许      |
| 基础写入    | `write_file`、`edit_file`、`apply_patch`                 | 需要路径策略和写锁                  |
| 执行类     | `run_command`、`write_stdin`                            | 默认更严格，按 sandbox profile 审批 |
| 网络类     | `web_search`、`web_fetch`、HTTP request                  | 需要网络策略、域名策略、密钥策略           |
| Skill 类 | `load_skill`、`read_skill_resource`                     | 低风险读取，但外部依赖需审批             |




### 5.3 不应内置为 Tool 的能力

- `word.insert_table`、`excel.set_cell`、`ppt.add_slide` 等细粒度 Office 操作。
- `github.create_issue` 等外部 SaaS 操作，优先走 MCP/Connector。
- 产品 UI 操作，放 Desktop/Enterprise 产品层。
- 企业业务办理流程，优先 Skill + MCP + Workflow。



## 六、内置 Skill 设计



### 6.1 定义

内置 Skill 是 Genesis 随产品提供的系统级工作流说明。它不直接扩大工具权限，只通过已启用 Tool、MCP 和 Execution Runtime 工作。

内置 Skill 适合：

- 元能力：`skill-creator`、`plan`、`review-fix-rereview`。
- 通用工程能力：代码审查、迁移计划、问题诊断。
- 通用文档能力：文档生成、PDF 审阅、Office 操作参考，但需注意许可证。
- 产品帮助能力：如何配置 Genesis、如何安装插件、如何创建 app。



### 6.2 来源形态

当前 CLI 已有 `skillmemory.NewSource(..., nil)` 作为 system source 占位。后续建议补成 embedded source：

```text
internal/capabilities/skill/adapter/embedded/skills/
  skill-creator/
    SKILL.md
    references/
    scripts/
  plan/
    SKILL.md
```

也可以由产品 bootstrap 注入系统技能：

```text
products/cli/bootstrap -> system embedded skills
products/desktop/bootstrap -> system embedded skills + desktop-only skills
products/enterprise/bootstrap -> system embedded skills + tenant controlled skills
```

关键是：内置 Skill 的读取仍走 `SkillSource`，不让 runtime 直接拼路径。

### 6.3 内置 Skill 权限

内置 Skill 的信任级别高于未知外部 Skill，但仍要遵守：

- `allowed-tools` 只收缩或建议工具，不自动扩权。
- `dependencies` 中的 command、mcp、connection 仍需审批或管理员预授权。
- Skill 加载进入 audit/usage。
- 用户、项目、租户可禁用内置 Skill。



## 七、内置 MCP 设计



### 7.1 定义

“内置 MCP”不应该理解为 Runtime 内部硬编码的远程服务，而应该理解为 Genesis 官方维护的 MCP 连接模板或默认可选能力。

内置 MCP 可以包括：

- 官方文档 MCP 配置模板。
- 企业内部 MCP 注册模板。
- Desktop 内置 app connector 映射。
- CLI 的推荐 MCP server 配置片段。



### 7.2 为什么不直接硬编码

MCP 通常涉及：

- 网络 endpoint。
- OAuth 或 bearer token。
- 用户/租户授权。
- 外部数据发送。
- 工具级权限和审批。

这些都不应该放进 `internal/runtime` 或通用 `internal/bootstrap`。正确方式是：

```text
MCP template -> 产品配置/连接管理 -> MCP client adapter -> ToolGateway/Policy -> Agent 使用
```



### 7.3 内外部 MCP 区分


| 类型             | 示例                           | 治理             |
| -------------- | ---------------------------- | -------------- |
| 官方模板           | OpenAI Docs、Context7 类文档 MCP | 可展示为推荐，用户确认后启用 |
| 产品内置 connector | Desktop 文件选择器、浏览器控制          | 产品内授权，仍要可禁用    |
| 企业托管 MCP       | 企业 GitHub、知识库、CRM            | 管理员发布，RBAC/审计  |
| 用户外部 MCP       | 用户自配 stdio/http server       | 默认不信任，工具和网络审批  |




### 7.4 MCP 模板与 MCP 实例

内置 MCP 应拆成“模板”和“实例”两层：


| 层级         | 内容                                            | 是否含密钥   | 谁管理                               |
| ---------- | --------------------------------------------- | ------- | --------------------------------- |
| MCP 模板     | server 类型、推荐 transport、工具说明、默认风险级别            | 否       | Genesis 或企业管理员                    |
| MCP 实例     | endpoint、enabled tools、租户/用户授权、credential_ref | 否，只引用凭据 | 产品配置或企业连接管理                       |
| Credential | token、OAuth refresh token、client secret       | 是       | Credential Store / Secret Manager |


模板可以内置，实例必须由产品或企业配置创建，真实密钥只能进入 Credential Store。Agent、Skill 和 Tool 只引用 `connection_ref` 或 `credential_ref`，不能直接持有密钥值。

CLI 可以把用户 MCP 实例写入用户配置；Desktop 可以通过 GUI 管理连接；Enterprise 应由管理员发布租户级实例，再允许用户完成 OAuth 授权或申请访问。

## 八、Plugin 与内置能力的关系

Plugin 是外部能力变成“可治理安装单元”的方式。内置能力不一定需要 Plugin，但当能力需要独立升级、组合、市场分发或企业发布时，应打包为 Plugin。

建议策略：

- `skill-creator`：内置 system skill，不依赖安装。
- `document-skills`：不直接内置 Anthropic 版本；可以支持用户安装 marketplace。
- `genesis-office-plugin`：未来 Genesis 自研后作为官方插件发布。
- 企业业务技能包：以企业 plugin 或 DB 发布对象管理。



## 九、Genesis 版 skill-creator 设计



### 9.1 是否应该内置

应该内置。`skill-creator` 是 Genesis Skills 生态的脚手架，属于元能力，用户只要开始创建、迁移、审查、优化 Skills，就会需要它。

它的触发场景：

- “帮我创建一个 skill”。
- “把刚才这个流程沉淀成 skill”。
- “把 Claude/Codex/Kode 的 skill 迁移到 Genesis”。
- “检查这个 SKILL.md 合不合规范”。
- “给这个 skill 写 evals”。
- “生成 marketplace manifest”。



### 9.2 Anthropic skill-creator 可借鉴点

Anthropic `skill-creator` 是 Apache 2.0，可作为实现参考。它的核心结构值得吸收：


| 模块                                    | 作用                                       | Genesis 取舍                                |
| ------------------------------------- | ---------------------------------------- | ----------------------------------------- |
| `SKILL.md`                            | 创建、评测、迭代、优化描述的主流程                        | 可高度借鉴，但改成 Genesis 工具和产品语义                 |
| `references/schemas.md`               | eval、grading、metrics、benchmark 等 JSON 结构 | 可借鉴字段，但增加 Genesis run/tool/audit 语义       |
| `scripts/quick_validate.py`           | 校验 `SKILL.md` frontmatter                | 应改为 Go validator 或 Genesis CLI 命令         |
| `scripts/package_skill.py`            | 打包 `.skill` 文件                           | 可参考，但 Genesis 优先生成 marketplace/plugin 包   |
| `scripts/run_eval.py` / `run_loop.py` | 调 Claude CLI 做触发评测和描述优化                  | 不照搬；Genesis 应通过自身 Agent Runtime/Runner 执行 |
| `scripts/improve_description.py`      | 根据 trigger eval 优化 description           | 可借鉴算法思想，不依赖 `claude -p`                   |
| `agents/grader.md`                    | 评测输出和断言质量                                | 可内置为 reference 或后续 subagent prompt        |
| `agents/analyzer.md`、`comparator.md`  | 分析 benchmark 和盲测比较                       | Phase 2 引入                                |
| `eval-viewer`                         | 人工查看结果和反馈                                | Desktop/Enterprise 可做 UI；CLI 可先生成静态报告     |




### 9.3 必须改造的点

Anthropic 版本假设 Claude Code 环境，Genesis 不能原样照搬运行语义：

- `claude -p` 要替换为 Genesis Run Engine 或模型路由。
- subagents 要映射到 Genesis 多 Agent / Task Worker。
- Bash 示例要改成跨平台 PowerShell/Windows 友好，或通过 `run_command` shell profile。
- `/tmp`、`open`、`nohup` 等环境假设要改成产品适配。
- `present_files`、browser viewer 要改成 Desktop UI、CLI 静态文件或 Enterprise Web。
- `allowed-tools` 需要映射到 Genesis 工具名：`read_file`、`write_file`、`edit_file`、`run_command`、`load_skill`。
- 生成的 manifest 应优先支持 `.genesis/marketplace.json`，兼容 `.claude-plugin/marketplace.json`。



### 9.4 Genesis skill-creator 的能力范围

第一版应聚焦“创建和校验”，不要一开始就做完整自动评测平台。

Phase 1 能力：

- 访谈用户意图：任务、触发条件、输入输出、依赖、成功标准。
- 生成标准 `SKILL.md`。
- 生成 `references/`、`scripts/`、`assets/` 目录建议。
- 校验名称、description 长度、frontmatter、路径安全。
- 生成 `evals/evals.json` 初稿。
- 生成可选 `skill-card.md` 发布治理卡片。
- 生成 `.genesis/marketplace.json` 初稿。
- 检查 Claude/Codex/Kode Skill 兼容性。
- 给出许可证、依赖、权限、部署地域、风险缓解、伦理/合规和沙箱提醒。

Phase 2 能力：

- 运行 eval prompts，对比 with-skill / baseline。
- 生成 grading、metrics、benchmark。
- 使用 reviewer HTML 或 Desktop/Enterprise UI 收集反馈。
- 根据反馈迭代 Skill。
- 优化 description 触发准确率。

Phase 3 能力：

- 企业 Skill 发布流程。
- 多版本 A/B、灰度、回滚。
- 组织级模板和合规模板。
- 自动生成插件包并推送企业 marketplace。



### 9.5 建议目录结构

```text
internal/capabilities/skill/adapter/embedded/skills/skill-creator/
  SKILL.md
  references/
    genesis-skill-schema.md
    genesis-marketplace-schema.md
    compatibility-guide.md
    eval-schema.md
  scripts/
    validate_skill.go 或 validate_skill.py
    package_skill.go 或 package_skill.py
```

如果第一阶段不想引入脚本，也可以只内置 `SKILL.md` 和 references，把校验逻辑放到 Go CLI 命令：

```text
genesis-cli skill validate <path>
genesis-cli skill package <path>
genesis-cli skill create
```

更推荐把确定性校验放 Go 代码，Skill 只负责编排和解释。

### 9.6 生成的 Skill 标准模板

Genesis 版 `skill-creator` 应生成如下基础模板：

```md
---
name: example-skill
description: Use this skill when ...
short-description: Short UI label
version: 0.1.0
allowed-tools:
  - read_file
  - write_file
  - run_command
context: inline
model: inherit
products:
  - cli
  - desktop
dependencies:
  tools:
    - type: tool
      value: read_file
      description: Read input files
---

# Example Skill

## When To Use

...

## Workflow

1. ...

## Validation

- ...
```

注意：Agent Skills 标准核心只要求 `name` 和 `description`。Genesis 扩展字段应保持可选，并由兼容层明确处理。

### 9.6.1 skill-card.md 发布治理卡片

NVIDIA Skills 文档中的 Skill Card 不应被理解为替代 `SKILL.md` 的运行标准。Genesis 的核心运行标准仍是 Agent Skills 结构：`SKILL.md`、frontmatter、instructions、`references/`、`scripts/`、`assets/` 和 progressive disclosure。Skill Card 更适合作为发布治理和信任元数据，用来回答“谁维护、能不能分发、依赖什么、风险是什么、在哪里部署、输出如何验收”。

Genesis 将其落为可选文件 `skill-card.md`，分层要求如下：


| 场景             | 要求                                     | 原因                              |
| -------------- | -------------------------------------- | ------------------------------- |
| 个人草稿           | 可缺失，只给 warning                         | 降低创建门槛，避免把治理表格变成早期负担            |
| 团队/项目复用        | 建议生成并校验                                | 需要 owner、license、依赖和风险说明，方便协作维护 |
| marketplace 发布 | 默认 warning，后续可在 marketplace policy 中强制 | 发布给他人安装时需要来源、条款、输出和风险透明         |
| Enterprise 发布  | 应强制，并进入审批、签名、审计流程                      | 企业需要租户、地域、合规、数据出境和人工复核依据        |


`skill-card.md` 第一版建议包含这些章节：

- `Description`
- `Owner`
- `License And Terms`
- `Use Case`
- `Deployment Geography`
- `Requirements And Dependencies`
- `Known Risks And Mitigations`
- `References`
- `Skill Output`
- `Skill Version`
- `Ethical Considerations`

对应 CLI 能力：

```text
genesis-cli skill card generate <path>
genesis-cli skill card validate <path>
```

`skill package` 在缺少或不完整 `skill-card.md` 时只输出 warning，不阻断个人和本地 marketplace 包。企业发布、官方 marketplace 发布和团队强治理可以在后续 policy 层把这些 warning 升级为 error。

### 9.7 复制与改造策略

Anthropic `skill-creator` 与 document skills 的许可证不同：`skill-creator` 是 Apache 2.0，因此 Genesis 可以在满足许可证要求的前提下复用。推荐策略不是“从零重写一切”，而是分层复用：


| 内容                                                   | 处理方式                               | 原因                                            |
| ---------------------------------------------------- | ---------------------------------- | --------------------------------------------- |
| 创建流程、访谈问题、progressive disclosure 写作指导                | 可直接借鉴或改写                           | 属于通用 Skill 创建方法论                              |
| `quick_validate.py` 校验规则                             | 可迁移为 Go validator                  | Genesis 已有 Go parser，应避免 Python 规则和 Go 规则分叉   |
| `package_skill.py`                                   | 可参考排除规则和 zip 逻辑                    | Genesis 更需要 marketplace/plugin 包，不只是 `.skill` |
| eval / grading / benchmark schema                    | 可改造复用                              | 需要加入 Genesis run_id、tool usage、audit metadata |
| `run_eval.py`、`run_loop.py`、`improve_description.py` | 不直接运行，重写执行入口                       | 依赖 `claude -p` 和 Claude Code auth             |
| `eval-viewer`                                        | 可作为 CLI 静态报告参考                     | Desktop/Enterprise 后续应有原生 UI                  |
| `agents/grader.md`、`analyzer.md`、`comparator.md`     | 可作为内置 reference/subagent prompt 起点 | 需改成 Genesis 工具名和输出路径约定                        |


落地时应在仓库保留 Apache 2.0 许可证声明，明确哪些文件来自 Anthropic、哪些是 Genesis 修改版本。若只是吸收思想并重写文本和代码，也应在设计说明中保留参考来源，方便后续合规审查。

### 9.8 Anthropic 与 Yao 的组合最佳实践

Anthropic `skill-creator` 和 `yao-meta-skill` 解决的问题不同，Genesis 不应二选一，也不应把两者完整相加。更优雅的组合方式是：

```text
Yao 负责前置判断与治理边界
  -> 是否应该创建 Skill
  -> Skill 属于 personal / team / enterprise-governed 哪一档
  -> 输入、输出、排除边界、资源边界、风险等级

Anthropic 负责创建与迭代闭环
  -> 访谈和提炼意图
  -> 生成 SKILL.md
  -> 生成少量 eval prompts
  -> with-skill / baseline 对比
  -> grading / benchmark / description 优化
  -> 打包与发布

Genesis 负责运行边界和产品语义
  -> SkillSource / ToolGateway / Approval / Policy / Sandbox / Audit
  -> CLI / Desktop / Enterprise 的不同装配
  -> 租户、用户、项目、角色、会话级 scope
```

因此，Genesis 版 `skill-creator` 的第一版不复制 Yao 的 Skill OS、Review Studio、Skill IR compiler，也不原样运行 Anthropic 的 `claude -p` 脚本。它应该吸收两者的稳定方法，落到 Genesis 自己的运行时。

#### 9.8.1 决策门：先判断是否应该创建 Skill

创建前必须先分类：


| 用户需求                        | 推荐产物            | 原因            |
| --------------------------- | --------------- | ------------- |
| 一次性解释、摘要、翻译、头脑风暴            | 直接回答            | 没有复用价值        |
| 可复用但没有 Agent 执行流程的规范、备忘、教程  | 文档              | 不需要路由和执行边界    |
| 确定性、单一、可命令化的重复动作            | 脚本或 Tool helper | 路由不是主要问题      |
| 重复任务、容易误触发、需要流程/资源/验证/治理    | Skill           | 符合 Skill 的价值  |
| 多个 Skills + MCP + 资源 + 发布治理 | Plugin          | 需要安装、升级、禁用、审计 |


这是 Yao 最值得吸收的部分。它能防止 Genesis 把所有东西都包装成 Skill，保持生态干净。

#### 9.8.2 分级：用三档替代四档

Yao 的 `Scaffold / Production / Library / Governed` 很完整，但 Genesis 第一版可以简化：


| Genesis 档位            | 来源借鉴                           | 适用场景                | 默认要求                          |
| --------------------- | ------------------------------ | ------------------- | ----------------------------- |
| `personal`            | Yao Scaffold                   | 个人草稿、短期复用           | `SKILL.md` + 基础校验             |
| `team`                | Yao Production / Library 的轻量部分 | 团队复用、路由风险较高         | owner、evals 初稿、依赖声明、资源边界检查    |
| `enterprise-governed` | Yao Governed                   | 租户/组织/项目发布、高权限或合规场景 | 审批、版本、签名、审计、回滚、review cadence |


这样既能承接企业版用户和租户级能力，又不会在个人创建 Skill 时强制生成复杂治理材料。

#### 9.8.3 创建闭环：以 Anthropic 为主干

Anthropic `skill-creator` 的核心闭环适合作为 Genesis 的主流程：

1. 从对话中抽取已有流程、工具、输入、输出和用户纠正。
2. 询问少量会改变设计的问题：触发条件、输出格式、边界、依赖、成功标准。
3. 先写 `description`，因为它是路由入口。
4. 生成 `SKILL.md`，只把核心流程放入正文。
5. 只有在确实有复用价值时，生成 `references/`、`scripts/`、`assets/`。
6. 生成 2-3 个真实 eval prompts，不强迫所有 Skill 都做复杂断言。
7. 对高风险或团队复用 Skill，运行 with-skill / baseline 对比。
8. 根据用户反馈和 grading 结果迭代。
9. 最后再做 description 触发优化和打包。

Genesis 应保留这个顺序，但把执行器从 Claude Code 替换为 Genesis Run Engine，把 subagent 替换为 Genesis 多 Agent / Task Worker，把 viewer 替换为 CLI 静态报告、Desktop UI 或 Enterprise Web。

#### 9.8.4 资源边界：以 Yao 规则约束 Anthropic 闭环

Anthropic 版本偏重“做出并评测”，Yao 版本更强调“不要膨胀”。Genesis 应采用以下硬规则：

- `SKILL.md` 只放触发、核心工作流、输出契约、资源导航和安全默认值。
- `references/` 只放被 `SKILL.md` 明确引用、且按需读取有价值的资料。
- `scripts/` 只放确定性、重复性、易出错的逻辑。
- `assets/` 只放模板、图片、样例文件等输出资源，不放长说明。
- `evals/` 只在 team 或 enterprise-governed 档位默认生成；personal 档位可选。
- 不创建空目录，不生成装饰性报告，不用治理材料伪装成熟度。



#### 9.8.5 权限和信任：声明归 Skill，执行归 Genesis

Yao 的 security policy 值得参考，但不能在 Skill 包里形成第二套权限系统。Genesis 的处理方式应是：

```text
Skill dependency declaration
  -> Skill validator / package scanner
  -> Capability catalog
  -> Policy filter
  -> Approval decision
  -> Sandbox / ToolGateway execution
  -> Audit / Usage record
```

Skill 只声明需要什么，例如 `run_command`、`python`、`network`、`mcp:microsoft-graph`。是否允许执行，由 Genesis 的产品 profile、租户策略、用户授权、审批和沙箱共同决定。

#### 9.8.6 不采纳项

以下内容不进入 Genesis 第一版：

- Yao 的完整 Skill OS 2.0。
- Yao 的 Skill IR compiler、target adapters、Review Studio、Skill Atlas、world-class evidence ledger。
- Anthropic 的 `claude -p` 触发评测实现。
- Anthropic 的浏览器 viewer 运行方式和 `nohup`、`open`、`/tmp` 等环境假设。
- 默认为每个 Skill 生成 benchmark、reports、security、registry 等重型目录。

这些可以作为 Phase 2/3 的参考，但不能阻塞内置 `skill-creator` 的最小可用闭环。

### 9.9 开发落地方案



#### 9.9.1 第一阶段目标

第一阶段交付一个内置 Genesis `skill-creator`，目标是“能创建、能校验、能安装、能迁移”，不追求完整自动评测平台。

必须交付：

- system embedded skill source 能加载 `skill-creator`。
- `skill-creator/SKILL.md` 能指导创建 Genesis Skill。
- `genesis-cli skill validate <path>` 能校验标准核心和 Genesis 扩展。
- `genesis-cli skill create <name>` 能生成 Genesis Skill 脚手架。
- `genesis-cli skill package <path>` 或 `skill marketplace` 命令能生成/更新 `.genesis/marketplace.json`。
- 兼容扫描能识别 Anthropic / Claude、Codex、Kode 常见扩展字段。
- 文档明确外部 Skill 不因安装而获得额外 Tool 权限。

暂不交付：

- 自动 trigger optimization。
- with-skill / baseline 多轮 benchmark。
- Desktop/Enterprise 可视化 reviewer。
- 企业发布审批 UI。



#### 9.9.2 推荐代码落点


| 能力                           | 推荐落点                                                                | 说明                                             |
| ---------------------------- | ------------------------------------------------------------------- | ---------------------------------------------- |
| embedded system skill source | `internal/capabilities/skill/adapter/embedded`                      | 只实现产品无关的 embedded source                       |
| CLI 注入 system skills         | `products/cli/bootstrap`                                            | 决定 CLI 默认启用哪些 system skills                    |
| Genesis `skill-creator` 内容   | `internal/capabilities/skill/adapter/embedded/skills/skill-creator` | 放 `SKILL.md`、references、必要脚本                   |
| Skill 解析和校验                  | `internal/capabilities/skill/parser`                                | 复用统一 parser，不让 CLI 自己解析一套                      |
| CLI validate/package 命令      | `products/cli/internal/command/skill_cmd.go`                        | 对用户暴露命令入口                                      |
| 本地 marketplace 兼容            | `shared/local/skillmarket`                                          | 兼容 `.genesis`、`.claude-plugin`、`.kode-plugin`  |
| 安装后的能力合成                     | `internal/capabilities/package/marketplace` 或 service                 | 合并来源、scope、启用状态                                |
| Enterprise Skill source      | `products/enterprise/internal/skill`                                | DB/object store source，必须带 tenant/project/user |
| Phase 2 eval runner          | `internal/capabilities/skill/eval` 或 app service                    | 调用 Run Engine，不依赖 Claude CLI                   |


目录边界要求：

- `internal/capabilities/skill` 不直接扫描用户目录，不访问企业 DB。
- `shared/local/skillmarket` 可以处理本地目录和 manifest 格式，但不持有企业策略。
- `products/<product>/bootstrap` 负责装配 source、profile、policy。
- Enterprise 不复用本地目录扫描作为默认生产 source。



#### 9.9.3 Genesis Skill 校验规则

`genesis-cli skill validate` 第一版至少检查：


| 类别          | 规则                                                          |
| ----------- | ----------------------------------------------------------- |
| 结构          | 必须存在 `SKILL.md`                                             |
| frontmatter | 必须有 `name`、`description`                                    |
| name        | `^[a-z0-9-]+$`，不以 `-` 开头/结尾，不含连续 `--`，不超过 64 字符             |
| description | 字符串，不含 `<`、`>`，建议不超过 1024 字符                                |
| 标准兼容        | 除 `name`、`description` 外的字段都按扩展处理，不能破坏加载                    |
| Genesis 扩展  | `allowed-tools`、`dependencies`、`products`、`metadata` 必须类型正确 |
| 资源引用        | `SKILL.md` 引用的本地资源必须存在                                      |
| 空目录         | 非必要空目录给 warning                                             |
| 脚本          | `scripts/` 中可执行文件给出权限、命令、网络风险提示                             |
| 密钥          | 扫描疑似 token、secret、cookie，至少 warning；高置信命中 block package     |
| 路径          | 可分发 Skill 中不应固化用户绝对路径                                       |
| 许可证         | 第三方来源或 plugin package 应有 license/source 元数据                 |


校验输出建议分三类：

```text
error: 阻止加载或打包
warning: 可加载但需要用户/管理员确认
info: 建议优化，不阻塞
```



#### 9.9.4 Genesis `skill-creator` 生成模板

第一版生成的 `SKILL.md` 应尽量 lean：

```md
---
name: example-skill
description: Use this skill when the user needs ...
metadata:
  author: Genesis
---

# Example Skill

## Workflow

1. Understand the input and confirm missing constraints only when they affect the result.
2. Use the referenced resources or scripts only when the task requires them.
3. Produce the output contract below.
4. Validate the result before responding.

## Output Contract

- ...

## Resource Map

- Read `references/example.md` when ...
- Use `scripts/example.ps1` when ...
```

Genesis 扩展字段可以由 CLI 生成到 `manifest` 或 `agents/interface.yaml`，不要强迫每个 `SKILL.md` frontmatter 都塞满平台字段。这样更接近 Agent Skills 标准，也更容易兼容 Claude/Codex/Kode。

#### 9.9.5 参考文件清单

开发时优先参考以下文件，不要把整个外部仓库当成实现模板。

Anthropic `skill-creator`：


| 文件                                                                                                 | 参考用途                              | Genesis 处理                          |
| -------------------------------------------------------------------------------------------------- | --------------------------------- | ----------------------------------- |
| `D:\workspace\go\go-project\anthropics-skills\skills\skill-creator\SKILL.md`                       | 创建、评测、迭代、描述优化主流程                  | 改写为 Genesis 语义                      |
| `D:\workspace\go\go-project\anthropics-skills\skills\skill-creator\references\schemas.md`          | eval、grading、metrics、benchmark 结构 | Phase 2 改造成 Genesis schema          |
| `D:\workspace\go\go-project\anthropics-skills\skills\skill-creator\scripts\quick_validate.py`      | 基础 frontmatter 校验规则               | 迁移为 Go validator                    |
| `D:\workspace\go\go-project\anthropics-skills\skills\skill-creator\scripts\package_skill.py`       | `.skill` 打包和排除规则                  | 参考 zip/package 逻辑                   |
| `D:\workspace\go\go-project\anthropics-skills\skills\skill-creator\scripts\run_eval.py`            | trigger eval 思路                   | 不运行，Phase 2 用 Genesis Run Engine 重写 |
| `D:\workspace\go\go-project\anthropics-skills\skills\skill-creator\scripts\improve_description.py` | 根据失败样本优化 description              | 参考算法，不依赖 `claude -p`                |
| `D:\workspace\go\go-project\anthropics-skills\skills\skill-creator\agents\grader.md`               | grading 输出和断言质量                   | 可改写为 Genesis reviewer prompt        |
| `D:\workspace\go\go-project\anthropics-skills\skills\skill-creator\agents\analyzer.md`             | benchmark 分析                      | Phase 2 参考                          |
| `D:\workspace\go\go-project\anthropics-skills\skills\skill-creator\agents\comparator.md`           | 盲测比较                              | Phase 2/3 参考                        |


Yao `meta-skill`：


| 文件                                                                                 | 参考用途                                   | Genesis 处理                            |
| ---------------------------------------------------------------------------------- | -------------------------------------- | ------------------------------------- |
| `D:\workspace\go\go-project\yao-meta-skill\SKILL.md`                               | 模式选择和输出契约                              | 抽取资格判断和风险分级                           |
| `D:\workspace\go\go-project\yao-meta-skill\references\non-skill-decision-tree.md`  | 不创建 Skill 的决策树                         | 放入 Genesis `skill-creator` 前置流程       |
| `D:\workspace\go\go-project\yao-meta-skill\references\skill-engineering-method.md` | Skill 工程方法                             | 抽取轻量闭环                                |
| `D:\workspace\go\go-project\yao-meta-skill\references\resource-boundaries.md`      | `SKILL.md`、references、scripts、evals 边界 | 纳入 Genesis validator warning          |
| `D:\workspace\go\go-project\yao-meta-skill\references\operating-modes.md`          | Scaffold/Production/Library/Governed   | 简化为 personal/team/enterprise-governed |
| `D:\workspace\go\go-project\yao-meta-skill\references\governance.md`               | owner、review cadence、生命周期              | 仅 team/enterprise 档启用                 |
| `D:\workspace\go\go-project\yao-meta-skill\security\permission_policy.md`          | 权限声明                                   | 映射到 Genesis Policy/Approval           |
| `D:\workspace\go\go-project\yao-meta-skill\security\script_policy.md`              | 脚本安全                                   | 映射到 validator 与 sandbox profile       |
| `D:\workspace\go\go-project\yao-meta-skill\security\dependency_policy.md`          | 依赖固定                                   | 映射到 package scanner                   |
| `D:\workspace\go\go-project\yao-meta-skill\security\trust_policy.md`               | 信任报告                                   | Phase 3 企业发布参考                        |
| `D:\workspace\go\go-project\yao-meta-skill\skill-ir\schema.json`                   | 平台中立 IR                                | 暂不采纳，仅作为未来迁移参考                        |


Genesis 当前相关文件：


| 文件                                             | 用途                             |
| ---------------------------------------------- | ------------------------------ |
| `docs/项目目录与边界说明.md`                            | 判断目录与产品边界                      |
| `docs/Office能力与Skills设计.md`                    | Office/document skills 的领域能力参考 |
| `shared/local/skillmarket/manifest.go`         | 本地 marketplace manifest 兼容解析   |
| `shared/local/skillmarket/skillmarket_test.go` | marketplace 兼容测试               |
| `products/cli/internal/command/skill_cmd.go`   | CLI skill 命令入口                 |
| `products/cli/bootstrap/container.go`          | CLI source/profile 注入点         |




#### 9.9.6 开发顺序

推荐按以下小步推进：

1. 完善 `shared/local/skillmarket`：稳定支持 `.genesis/marketplace.json`、`.claude-plugin/marketplace.json`、`.kode-plugin/marketplace.json`。
2. 实现 `SkillValidator`：先覆盖结构、frontmatter、name、description、资源引用和风险 warning。
3. 增加 `genesis-cli skill validate <path>`。
4. 增加 embedded system skill source，让 CLI 能看到内置 `skill-creator`。
5. 写 Genesis 版 `skill-creator/SKILL.md`，先不放复杂 eval runner。
6. 增加 `genesis-cli skill create` 或让 `skill-creator` 指导用户生成目录。
7. 增加 package/manifest 生成命令。
8. Phase 2 再做 eval runner、grading、benchmark、description optimization。

每一步都应该可独立验收，避免把内置 Skill、marketplace、eval、企业发布绑成一个大 PR。

## 十、兼容外部 Skills 的策略

Genesis 应以 Agent Skills 标准为基线，兼容主流宿主扩展。

### 10.1 标准核心

必须支持：

- `SKILL.md`。
- `name`、`description`。
- Markdown instructions。
- `scripts/`、`references/`、`assets/`。
- progressive disclosure。



### 10.2 Claude Code 扩展

应尽量映射：


| Claude 字段/行为                      | Genesis 映射                             |
| --------------------------------- | -------------------------------------- |
| `allowed-tools`                   | Genesis enabled tools / policy matcher |
| `context: fork`                   | 多 Agent / Task Worker，未实现前提示不支持        |
| `disable-model-invocation`        | `Policy.DisableModelInvocation`        |
| `$ARGUMENTS`                      | `load_skill.args`                      |
| `${CLAUDE_SKILL_DIR}`             | Skill resource root，不暴露宿主绝对路径          |
| `.claude-plugin/marketplace.json` | marketplace 兼容解析                       |
| `!command` 动态注入                   | 默认不支持；后续可通过受控预处理实现                     |




### 10.3 Codex/Kode 扩展

应兼容：

- `.kode-plugin/marketplace.json`。
- plugin/marketplace 中的 `plugins` 字段。
- `$skill`、`skill://...` 或 qualified name 选择。
- 插件安装记录和 enable/disable。



## 十一、安全与治理



### 11.1 Skill 创建安全

`skill-creator` 在生成 Skill 时必须提醒并检查：

- 不写入真实密钥、token、cookie。
- 不把个人绝对路径固化进可分发 Skill。
- 不生成绕过审批的命令执行指令。
- 不创建诱导越权、数据外传、恶意持久化的 Skill。
- 外部依赖要进入 `dependencies`。
- 许可证必须明确，导入第三方内容不能默认内置分发。



### 11.2 安装和执行安全

- 安装来源记录 marketplace、version、hash、scope。
- 外部 Skill 默认不扩大工具权限。
- command、mcp、connection 依赖需要审批或管理员预授权。
- Enterprise 下所有 Skill 加载和执行都带 `tenant_id`、project、user、role。
- SandboxAuto 不能静默降级为无沙箱。



### 11.3 安装来源与域名审计

通过 URL、GitHub、Git、文件或目录安装 Skill / Plugin 时，安装记录必须保存安装时的来源快照，不能只保存 marketplace 名称。最低应记录：


| 字段                      | 说明                                                     |
| ----------------------- | ------------------------------------------------------ |
| `type`                  | `github`、`git`、`url`、`file`、`directory`、`enterprise`   |
| `address`               | 标准化后的来源地址，例如 `https://github.com/org/repo@tag#subpath` |
| `domain`                | 远程来源域名，例如 `github.com`、`example.com`；本地文件/目录可为空        |
| `repo` / `url` / `path` | 按来源类型保存结构化字段                                           |
| `ref` / `sub_path`      | Git ref、tag、branch、commit、仓库子路径                        |
| `package_source`        | marketplace manifest 中 package 的 `source` 子路径          |
| `resolved_revision`     | fetcher 能识别到的 commit/tag/hash；无法识别时为空                  |
| `content_hash`          | 安装时 marketplace cache 或 package 内容 hash                |
| `marketplace`           | 来源 marketplace 名称                                      |


治理规则：

- Registry 记录 marketplace source，InstallRecord 记录安装时 source provenance，两者都需要保留。
- GitHub shorthand 也必须规范化为可审计地址，并记录 `domain=github.com`。
- URL 安装必须解析并记录 host/domain，后续可用于企业 allowlist/denylist。
- 本地 file/dir 安装记录本地路径，但分发包内不应固化宿主机绝对路径。
- Enterprise 后续应在安装记录上增加 signer、publisher、tenant、reviewer、approval_id、policy_snapshot。
- 安装来源记录不授予权限；执行仍必须通过 ToolGateway、Approval、Policy、Sandbox 和 Audit。



## 十二、实现路线



### Phase 1：内置 skill-creator 最小闭环

- 增加 embedded system skill source。
- 内置 Genesis 版 `skill-creator/SKILL.md`。
- 增加 `genesis-cli skill validate <path>`。
- 增加 `genesis-cli skill create <name>`。
- 增加 `genesis-cli skill package <path>` 或生成 `.genesis/marketplace.json` 的命令。
- 增加 `genesis-cli skill card generate/validate <path>`，先作为发布治理 warning，不阻断个人草稿。
- 支持 `.claude-plugin/marketplace.json` 作为 marketplace 输入。
- 在文档中声明 Anthropic `skill-creator` 可参考，但 Genesis 内置版本为自有实现。



### Phase 2：评测闭环

- 增加 eval schema。
- 增加 `genesis-cli skill eval validate <path>`，先做本地 schema、路径和断言结构校验。
- 增加 `genesis-cli skill eval validate-run <run-dir>`，校验 `grading.json`、`outputs/metrics.json`、`timing.json` 的本地输出契约。
- 支持 with-skill / baseline run。
- 支持 grading 和 benchmark 汇总。
- CLI 生成静态 HTML 报告，Desktop/Enterprise 提供 UI。



### Phase 3：企业发布

- Skill draft/published/archived。
- 租户/组织/项目发布策略。
- 审批、签名、审计、回滚。
- 企业 marketplace 和插件管理。



### 12.4 开发落点与验收

详细代码落点、校验规则、参考文件和开发顺序以 9.9 节为准。本节只保留阶段验收口径，避免多个清单并行导致实现分歧。

第一阶段验收标准：

- 启动 CLI 后，模型能在 available skills 中看到 `skill-creator`。
- 调用 `load_skill` 后能读取 Genesis 版 `skill-creator` 的完整说明和引用资源。
- `genesis-cli skill create <name>` 生成的 Skill 能通过 `genesis-cli skill validate <path>`。
- `genesis-cli skill package <path>` 能输出包含 `.genesis/marketplace.json` 的可安装目录包，并在缺少或不完整 `skill-card.md` 时提示发布治理 warning。
- `.genesis/marketplace.json`、`.claude-plugin/marketplace.json`、`.kode-plugin/marketplace.json` 的本地兼容扫描有测试覆盖。
- 外部 Skill 安装、加载、执行不会绕过 ToolGateway、Approval、Policy、Sandbox 和 Audit。
- Enterprise 相关实现只通过企业 source、租户策略和产品 bootstrap 注入，不复用本地目录扫描作为默认生产能力。



## 十三、当前建议

1. 立即内置 Genesis 版 `skill-creator`，作为 system skill。
2. Anthropic `skill-creator` 是 Apache 2.0，可以复制结构、脚本思路和部分实现，但必须保留许可证/NOTICE，并把 Claude Code 专属运行语义改造成 Genesis 语义。
3. 第一版只做创建、校验、兼容扫描、manifest 生成，不做复杂 benchmark viewer。
4. 保留对 Anthropic skills marketplace 的兼容安装能力，但 document skills 因许可证限制不应作为 Genesis 内置分发内容。
5. 将确定性校验能力尽量做成 Go 命令或 service，Skill 负责引导和编排。

最终原则：内置能力负责提供可靠的基础脚手架，外部能力负责生态扩展；所有能力都必须经过同一套 ToolGateway、Approval、Policy、Sandbox 和 Audit 边界。

## 十四、审查记录

本节记录按 `review-fix-rereview` 进行的文档审查结果。

### 14.1 第一性原理分析

核心问题：Genesis 需要一个长期稳定的能力扩展模型，让内置能力提供可靠基础，让外部能力形成生态，同时不破坏 Runtime 内核、产品边界、企业治理和用户安全。对 `skill-creator` 来说，核心不是“生成更多文件”，而是让 Agent 能稳定创建可路由、可维护、可治理、可验证的 Skill。

最低可接受结果：

- 清楚定义 Tool、Skill、MCP、Plugin 的职责。
- 清楚定义内置和外部能力的差异，以及它们都不能绕过审批。
- 覆盖 CLI、Desktop、Enterprise 的统一模型与产品独立性。
- 覆盖 Enterprise 的租户、用户、项目、角色、会话等多级能力合成。
- 给出 `skill-creator` 的内置理由、参考 Anthropic 的取舍、实现阶段和代码落点。
- 结合 Anthropic `skill-creator` 和 Yao `meta-skill` 时，明确谁负责创建闭环、谁负责前置判断和治理边界。
- 给开发者列出应参考的具体文件，并说明哪些只参考思想、哪些可迁移实现。

失败条件：

- 把内置能力误解为默认无限授权。
- 把 MCP 当成本地执行层。
- 把 Skill 当成密钥或产品策略载体。
- 忽略 Enterprise 用户级和租户级能力并存。
- 因许可证误判导致错误地内置不可分发内容，或过度保守地放弃可合法复用的 Apache 2.0 内容。
- 照搬 Yao 的重型 SkillOps 体系，导致 Genesis 第一版过度设计。
- 照搬 Anthropic 的 Claude Code 运行假设，导致 Genesis 自身 Run Engine、ToolGateway、Approval、Sandbox 被绕开。



### 14.2 审查发现与修复


| 发现                                   | 风险                                                                         | 修复                                              |
| ------------------------------------ | -------------------------------------------------------------------------- | ----------------------------------------------- |
| 初稿只粗略区分内置/外部，缺少 scope 和信任分级          | Enterprise、Plugin、用户级能力会混在一起                                               | 增加“信任分级与 Scope”章节                               |
| 初稿没有显式覆盖 Enterprise 用户级、租户级、项目级覆盖关系  | 用户补充指出企业版还会有用户和租户级别                                                        | 增加“Enterprise 的多级能力覆盖”章节                        |
| 内置 MCP 容易被理解为硬编码 server              | 可能把 endpoint/密钥写进 runtime 或 Skill                                          | 增加“MCP 模板与 MCP 实例”章节                            |
| 对 Anthropic `skill-creator` 复用策略过于保守 | Apache 2.0 内容本可复用，容易增加无谓重写成本                                               | 增加“复制与改造策略”，区分可复用和需重写部分                         |
| Yao `meta-skill` 的价值和风险没有写入落地方案      | 开发者可能要么忽略它，要么照搬它的重型体系                                                      | 增加 9.8 节，明确 Yao 负责前置判断和治理边界，Anthropic 负责创建与迭代闭环 |
| Anthropic 与 Yao 的组合没有明确第一版范围         | 容易把 eval viewer、Skill IR、Review Studio、description optimizer 一次性塞进 Phase 1 | 增加不采纳项和 9.9.1 第一阶段目标，明确 Phase 1 只做创建、校验、安装、迁移   |
| 参考来源过散                               | 后续开发可能直接复制整个外部仓库或遗漏关键文件                                                    | 增加 9.9.5 参考文件清单，逐项说明参考用途和 Genesis 处理方式          |
| 新增 9.9 后与原 12.4 代码落点清单重复             | 两处清单并行会导致实现口径分裂                                                            | 将 12.4 改为阶段验收摘要，详细落点统一以 9.9 为准                  |
| 代码落点需要符合项目目录边界                       | 可能让 `internal` 依赖 `shared/local` 或让 Enterprise 复用本地扫描                      | 对照 `docs/项目目录与边界说明.md`，在 9.9 和 12.4 中明确目录边界要求   |




### 14.3 再审查结论

修订后，文档已经覆盖用户提出的核心任务：

- 对内置 Skills、Tools、MCP 的定义、边界、内外部区分、产品统一与独立平衡给出结构化设计。
- 对 Genesis 版 `skill-creator` 给出参考 Anthropic 实现的取舍、可复用部分、必须改造部分、阶段计划和代码落点。
- 对 Anthropic `skill-creator` 与 Yao `meta-skill` 的组合给出明确判断：Yao 的资格判断、资源边界、治理分级值得吸收；Anthropic 的创建、评测、迭代、打包闭环作为主干；Genesis 运行边界必须自有实现。
- 给出开发参考文件清单、第一阶段目标、校验规则、推荐代码落点和开发顺序，可直接指导后续拆任务。



### 14.4 实现后审查记录

本次按 `review-fix-rereview` 对 Phase 1 最小闭环进行实现审查。

第一性原理判断：`skill-creator` 的最低价值不是复杂评测平台，而是让用户和 Agent 可以创建、校验、打包、安装可治理的 Skill，同时不把外部 Skill 变成新的权限通道。

已闭合项：

- embedded system skill source 已能加载内置 `skill-creator`。
- Genesis 版 `skill-creator/SKILL.md` 已落到 embedded skills，内容为 Genesis 运行边界，不照搬 Claude Code 工具假设。
- `genesis-cli skill validate <path>` 已接入 Go validator。
- `genesis-cli skill create <name>` 已能生成 lean `SKILL.md`、可选资源目录和 `evals/evals.json` 初稿。
- `genesis-cli skill package <path>` 已能生成 `.genesis/marketplace.json` 目录包，并参考 Anthropic package 逻辑排除 `evals`、`__pycache__`、`node_modules`、`.pyc`、`.DS_Store`。
- 已新增 `internal/capabilities/skill/card` 和 `genesis-cli skill card generate/validate <path>`；`skill package` 会在缺少或不完整 `skill-card.md` 时输出发布治理 warning，但不阻断个人草稿和本地包。
- marketplace 安装记录已增加 source provenance，能记录 URL/GitHub/Git/file/dir 的来源地址、域名、ref、subpath、package_source、resolved_revision 和 content_hash。
- 本地 marketplace 兼容 `.genesis/marketplace.json`、`.claude-plugin/marketplace.json`、`.kode-plugin/marketplace.json` 的解析策略。
- 再审查发现 `skill package --force --out <dir>` 存在误删高风险目录的边界风险，已增加根目录、当前目录、home 目录覆盖保护并补测试。
- 在不涉及 DB 或界面的范围内，已新增 `internal/capabilities/skill/eval`、`genesis-cli skill eval validate <path>` 和 `genesis-cli skill eval validate-run <run-dir>`；`skill validate` 会自动检查 `evals/evals.json`，`validate-run` 会检查 grading、metrics、timing 产物契约。
- 本次 Skill Card 增量复审从第一性原理确认：`skill-card.md` 应提供发布治理证据，而不是运行授权或 Skill 路由标准；已修复 `skill card validate` 可在无效 Skill 目录上通过的问题，现在会先要求基础 Skill 校验无 error。

再审查结论：Phase 1 的“能创建、能校验、能打包、能安装审计来源”的最小闭环已经落地。Phase 2 中不涉及 DB 或界面的 eval schema、本地校验和 skill-card 发布治理提示已落地。仍未实现的是 eval runner、grading 执行、benchmark 多轮对比、description optimization、Desktop/Enterprise reviewer UI、企业发布审批、签名、灰度和回滚。

## 十五、代码与文档对齐审计

本节按 `code-doc-alignment` 对本文设计与当前实现进行对齐审计。审计日期：2026-07-04。

### 15.1 审计范围

参考来源：

- 本文第 2-12 节：内置能力边界、`skill-creator` 设计、Phase 1/2/3 路线。
- 本文第 14.4 节：实现后审查记录。
- 外部参考仅用于判断设计来源：Anthropic `skill-creator` 的 schema、validate/package/eval 思路；Yao `meta-skill` 的资源边界与非 Skill 决策思想。

实现检查范围：

- `internal/capabilities/skill/adapter/embedded`
- `internal/capabilities/skill/parser`
- `internal/capabilities/skill/eval`
- `internal/capabilities/package/marketplace`
- `shared/local/skillmarket`
- `products/cli/internal/command`
- `products/cli/bootstrap`

范围假设：

- 本次只审计不涉及企业 DB、不涉及 Desktop/Enterprise UI 的实现进度。
- Phase 2 中的模型执行、Run Engine 集成、grading agent、benchmark 多轮运行属于后续工作；当前只检查本地 schema、校验和 CLI 契约是否已落地。
- 文档中的 Enterprise 发布、租户/用户级 source、审批、签名、灰度、回滚仍视为目标设计，不要求当前代码已经实现。



### 15.2 进度对照表


| 设计点 / 要求                                                         | 文档位置                 | 实现证据                                                                                                                                                                 | 状态       | 差距 / 说明                                                                                      |
| ---------------------------------------------------------------- | -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- | -------------------------------------------------------------------------------------------- |
| Tool / Skill / MCP / Plugin 的职责边界                                | 第 2、5、6、7、8 节        | 主要为架构文档约束；Tool/Skill 的已有接口分布在 `internal/capabilities/*`，Plugin/MCP 仍主要是产品和生态设计                                                                                       | `/` 部分实现 | Skill/Tool 代码路径基本符合；MCP template、Plugin 统一安装单元尚未形成完整通用实现。                                    |
| 内置与外部能力的 scope / trust 分级                                        | 第 3、4 节              | `internal/capabilities/skill/model/model.go` 定义 `ScopeSystem/User/Project/Plugin/Tenant/Org/Agent/Session`、`Authority`、`SourceKind`                                  | `/` 部分实现 | 模型已具备；完整 Policy 合成、Enterprise 多级覆盖、强制 deny 优先级尚未在 Skill source 合成中完整实现。                      |
| embedded system skill source                                     | 第 6.2、9.9、12.1       | `internal/capabilities/skill/adapter/embedded/system.go` 的 `SystemFS()`；`source_test.go` 覆盖 `skill-creator` 可发现                                                      | `x` 已实现  | 与文档一致，产品无关 source 放在 capability adapter。                                                     |
| CLI 注入 system skills                                             | 第 9.9.2、12.4         | `products/cli/bootstrap/container.go` 使用 `skillembedded.SystemFS()` 并注册 `cli-system` embedded source                                                                 | `x` 已实现  | CLI 默认可见内置 `skill-creator`。                                                                  |
| Genesis 版 `skill-creator/SKILL.md`                               | 第 9.1-9.6、12.1       | `internal/capabilities/skill/adapter/embedded/skills/skill-creator/SKILL.md`                                                                                         | `x` 已实现  | 内容已改成 Genesis 语义，没有直接照搬 Claude Code 工具名和运行假设。                                                |
| Skill 基础解析与 validator                                            | 第 9.9.3              | `internal/capabilities/skill/parser/validator.go`；`validator_test.go` 覆盖 minimal、资源引用、脚本 warning、description、私钥、eval 错误                                              | `x` 已实现  | 已覆盖核心结构、安全与资源边界；license/source 元数据只作为设计要求，尚未做强校验。                                            |
| `genesis-cli skill validate <path>`                              | 第 9.5、9.9、12.1       | `products/cli/internal/command/skill_cmd.go` 调用 `parser.NewValidator().ValidateSkillFS`                                                                              | `x` 已实现  | 已接入 CLI，并会检查 `evals/evals.json`。                                                             |
| `genesis-cli skill create <name>`                                | 第 9.9.1、9.9.6、12.1   | `products/cli/internal/command/skill_scaffold_cmd.go` 的 `newSkillCreateCmd()`；`skill_scaffold_cmd_test.go` 覆盖创建、eval 初稿                                              | `x` 已实现  | 生成 lean `SKILL.md`、可选资源目录和 eval 初稿。                                                          |
| `genesis-cli skill package <path>` / `.genesis/marketplace.json` | 第 9.3、9.9、12.1       | `newSkillPackageCmd()`、`writeMarketplaceManifest()`；测试断言 `$schema`、skills 路径和排除 `evals`                                                                              | `x` 已实现  | 采用目录 marketplace 包，不是 Anthropic `.skill` zip；与 Genesis 当前设计一致。                               |
| Skill Card 发布治理元数据                                               | 第 9.6.1、12.1、12.4    | `internal/capabilities/skill/card`；`products/cli/internal/command/skill_card_cmd.go`；`skill package` 调用 `skillcard.NewValidator()` 输出 warning；测试覆盖 generate/validate | `x` 已实现  | 作为 `skill-card.md` 治理层落地：个人草稿缺失只 warning，后续企业/marketplace policy 可升级为强制。                     |
| package 覆盖安全                                                     | 第 11.1、11.2 安全边界     | `ensureCreatableDir()`、`isDangerousOverwriteTarget()` 拒绝覆盖根目录、当前目录、home；测试覆盖                                                                                         | `x` 已实现  | 来自审查修复，降低 `--force --out` 误删风险。                                                              |
| 本地 marketplace 兼容 `.genesis` / `.claude-plugin` / `.kode-plugin` | 第 9.3、10.2、10.3、12.1 | `shared/local/skillmarket/manifest.go` 查找四类 manifest；`skillmarket_test.go` 覆盖 Claude/Kode plugins 字段                                                                 | `x` 已实现  | 已兼容 `packages` 与 `plugins`。                                                                  |
| 安装来源、域名和 hash 审计                                                 | 第 11.3               | `marketplace/model.go` 的 `SourceProvenance`、`SourceDomain`、`SourceAddress`；`service.go` 安装时写入 provenance；`fetcher.go` 计算 `ContentHash`；相关测试                          | `x` 已实现  | 远程域名、GitHub shorthand、本地路径、resolved revision/hash 已具备基础记录。签名和 approval snapshot 尚未实现。        |
| 外部 Skill 安装不自动扩权                                                 | 第 3.2、6.3、11.2       | 安装记录只保存 package/source/scope/provenance；执行仍通过现有 `load_skill`、Tool/Approval/Policy/Sandbox 流程                                                                         | `/` 部分实现 | 没有发现安装即授予 Tool 权限的实现；但更细的依赖审批、管理员预授权、企业 allow/deny 策略仍未完整。                                   |
| eval schema：`evals/evals.json`                                   | 第 9.2、9.4、12.2       | `internal/capabilities/skill/eval/eval.go` 的 `Suite/Case/ValidateFS`；`eval_test.go` 覆盖 Anthropic 风格 eval、路径安全、重复 id                                                  | `x` 已实现  | 已支持本地 schema、文件路径安全、skill_name 一致性、断言结构校验。                                                   |
| `genesis-cli skill eval validate <path>`                         | 第 12.2               | `products/cli/internal/command/skill_eval_cmd.go` 的 `newSkillEvalValidateCmd()`；CLI 测试覆盖                                                                             | `x` 已实现  | 只做本地校验，不运行模型，符合当前边界。                                                                         |
| eval run 输出契约校验                                                  | 第 9.2、12.2           | `ValidateRunFS()` 校验 `grading.json`、`outputs/metrics.json`、`timing.json`；`skill eval validate-run <run-dir>` 暴露 CLI                                                  | `x` 已实现  | 已校验 summary 一致性、负数计数、证据缺失 warning。尚未生成这些产物。                                                  |
| with-skill / baseline run                                        | 第 12.2               | 无 Run Engine 集成代码                                                                                                                                                    | `` 未实现   | 需要设计 `internal/capabilities/skill/eval` runner/service，调用 Genesis Run Engine，而不是 Claude CLI。 |
| grading 执行                                                       | 第 9.2、12.2           | 目前只有 grading 输出 schema 校验，无 grader agent 或模型执行                                                                                                                       | `` 未实现   | 下一步需要 grader prompt/service，输出现有 `grading.json` 契约。                                          |
| benchmark 多轮对比与汇总                                                | 第 9.2、12.2           | 无 benchmark runner / summary 生成                                                                                                                                      | `` 未实现   | 可以先做 CLI JSON 汇总，再考虑 HTML 报告。                                                                |
| description optimization                                         | 第 9.2、12.2           | 无实现                                                                                                                                                                  | `` 未实现   | 需要基于 trigger eval/失败样本生成建议，不依赖 `claude -p`。                                                  |
| CLI 静态 HTML 报告                                                   | 第 9.2、12.2           | 无实现                                                                                                                                                                  | `` 未实现   | 可做本地报告生成，不涉及 Enterprise UI。                                                                  |
| Desktop / Enterprise reviewer UI                                 | 第 9.2、12.2           | 无实现；`products/desktop/bootstrap/execute.go` 仍提示暂未实现                                                                                                                  | `` 未实现   | 用户已明确企业 DB/UI 暂不做。                                                                           |
| Enterprise Skill source / 租户用户级发布                                | 第 4.4、9.9、12.3       | 模型有 `ScopeTenant/Org/User`，无企业 DB/object store source                                                                                                                | `/` 部分实现 | 仅有模型与文档设计；实际 source、RBAC、tenant_id 查询未做。                                                     |
| 企业审批、签名、灰度、回滚                                                    | 第 11.3、12.3          | provenance 模型未包含 signer/publisher/approval_id/policy_snapshot 的完整持久化流程                                                                                               | `` 未实现   | 属于企业发布阶段，按用户要求暂不做。                                                                           |
| Anthropic / Yao 参考边界                                             | 第 9.7、9.8、9.9.5      | 实现吸收 quick validate、package 排除、schema 校验、资源边界；没有照搬 Claude CLI 脚本                                                                                                     | `x` 已实现  | 当前取舍与文档一致：复用思想和结构，不复用运行边界。                                                                   |




### 15.3 差距反思

实现差距：

- Phase 1 最小闭环基本完成：内置 `skill-creator`、创建、校验、打包、skill-card 发布治理提示、marketplace 兼容、来源审计都已有代码和测试。
- Phase 2 已完成可本地落地的基础契约：`evals/evals.json` 校验、eval run 输出契约校验和发布治理卡片校验。
- Phase 2 的真实运行闭环仍未开始：还不能自动执行 with-skill / baseline、不能调用 grader、不能生成 benchmark 汇总，也不能根据失败样本优化 description。
- Phase 3 企业治理仍停留在模型和文档设计层：租户/用户级 source、企业 DB、审批、签名、灰度、回滚均未实现。`skill-card.md` 目前也只是 CLI warning，尚未接入企业/官方 marketplace policy 的强制发布门禁。

文档差距：

- 文档中 12.4 标题仍写“第一阶段验收标准”，但当前已经包含部分 Phase 2 本地校验能力；后续可拆出 “Phase 2 本地验收标准”。
- 第 9.5 建议目录中列出 embedded skill 的 `references/` 和 `scripts/`，当前 Genesis 内置 `skill-creator` 只有 `SKILL.md`。这是刻意保持轻量，不是实现缺陷；若后续 schema 说明变多，再放入 references。
- 文档提到许可证/NOTICE，但当前实现主要为自有 Go 代码和自写 SKILL.md，未复制 Anthropic 源文件；如果未来复制 Apache 2.0 内容，应补 LICENSE/NOTICE 归档。

架构边界评估：

- `internal/capabilities/skill` 没有直接扫描用户目录，也没有访问企业 DB；本地目录处理仍在 `shared/local/skillmarket` 和 CLI bootstrap，符合目录边界。
- eval 校验只读本地 `fs.FS`，不执行命令、不联网、不调用模型，不扩大权限面。
- marketplace fetcher 能下载 URL/GitHub zip，这是 marketplace 管理路径，不是 Skill 执行路径；安装后执行仍需经过 ToolGateway/Approval/Policy/Sandbox。



### 15.4 下一步建议

1. 将 Skill Card 接入发布 policy。
  - 涉及文件：`internal/capabilities/skill/card`、marketplace 发布服务、未来 Enterprise 发布流程。
  - 验收标准：个人草稿仍为 warning；官方 marketplace 或 Enterprise 发布可以按策略把缺失/不完整 `skill-card.md` 升级为 error。
  - 验证：policy 单元测试 + CLI/发布服务 fixture。
2. 实现本地 eval runner 骨架。
  - 涉及文件：`internal/capabilities/skill/eval`，可能新增 `runner.go`、`contract.go`；CLI 新增 `skill eval run`。
  - 验收标准：能读取 `evals/evals.json`，为每个 eval 创建 run directory，生成 `timing.json` 和占位 `grading.json` 或 executor transcript；不接企业 DB，不做 UI。
  - 验证：Go 单元测试 + CLI 临时目录烟测。
3. 接入 Genesis Run Engine 做 with-skill 单次执行。
  - 涉及文件：`internal/runtime` 或 app service 适配层、`internal/capabilities/skill/eval`。
  - 验收标准：单个 eval 能通过 Genesis 自身 Agent Runtime 执行，产物目录符合 `validate-run` 契约。
  - 验证：使用 fake runner 单元测试；真实 CLI 只跑可控简单 eval。
4. 增加 grader 输出生成。
  - 涉及文件：`internal/capabilities/skill/eval`，可能新增 grader prompt/service。
  - 验收标准：根据 transcript、outputs 和 expectations 生成 `grading.json`，并能通过 `skill eval validate-run`。
  - 验证：固定 transcript fixture 的 deterministic 测试。
5. 增加 benchmark JSON 汇总，不先做 HTML/UI。
  - 涉及文件：`internal/capabilities/skill/eval`、`products/cli/internal/command/skill_eval_cmd.go`。
  - 验收标准：多次 run 的 `grading.json` 能汇总为 `benchmark.json`，包含 with_skill / without_skill 分组和 pass_rate/time/tool_calls 统计。
  - 验证：fixture run dirs + 汇总单测。
6. 延后企业 DB/UI/审批签名。
  - 涉及文件：未来 `products/enterprise/internal/skill`、企业 Web UI、policy/audit store。
  - 验收标准：待企业发布模型确认后再拆任务；当前不进入实现。



### 15.5 当前结论

当前代码与本文的 Phase 1 目标高度对齐，并已向 Phase 2 推进了本地 schema、产物契约校验和发布治理卡片校验。剩余主要缺口不是“创建/安装/校验”能力，而是“真实运行和评测闭环”：runner、grader、benchmark、description optimization。企业 DB、租户发布、审批签名和 UI 按当前约束继续后置。
