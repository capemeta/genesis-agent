# Agent App 智能体应用与任务配置设计

## 一、设计目标

Genesis 不应只被设计成 Codex 类 AI 编程工具，而应成为一个通用智能工作平台：用户可以管理和配置不同的提示词、Skills、工具、MCP、子智能体、编排方式、权限和运行环境，用同一套 Runtime 完成代码、文档、客服、运维、数据分析、业务办理等不同任务。

本设计回答四个核心问题：

- 顶层能力配置单元叫什么，既符合用户对“智能体”的认知，又避免和内部执行角色混淆。
- Registry、Marketplace、Catalog 是否是同一概念，以及三者如何命名。
- CLI、Desktop、Enterprise 如何统一 Agent App 概念，又保留各自产品差异。
- 命令行如何支持默认智能体、切换智能体、临时覆盖智能体，以及后续自定义和市场安装。

结论：

> 顶层用户概念使用 **Agent App / 智能体应用**，对外可简称 **智能体**。  
> Agent App 是面向某类工作目标的智能能力配置包；Run 是一次执行；Worker 是 Agent App 内部的执行者。

技术域建议命名为 `agentapp`，不要命名为 `agent`，避免和领域模型、运行时执行者、子智能体混淆。`App` 可以作为产品文案或配置文件里的简写，但代码包名优先使用 `agentapp`。

## 二、从第一性原理分析

### 2.1 根问题

用户真正需要的不是“创建一个运行时 Agent 对象”，而是：

- 选择一种工作模式。
- 绑定这类工作需要的提示词、Skills、工具、MCP、文件/命令权限、记忆和模型路由。
- 在不同产品形态中启动、切换、分享、治理和审计这套工作模式。

如果代码和架构里把所有东西都叫 Agent，会把“用户选择的顶层智能体应用”和“运行时参与推理/执行的工作者角色”混在一起。对外可以顺应用户心智叫“智能体”，对内必须用更精确的 `Agent App` 与 `Worker` 拆开。

### 2.2 必须成立的不变量

- Agent App 是用户可见、可管理、可切换的顶层单元。
- Worker 是 Agent App 内部执行角色，不直接等于产品入口。
- Task/Run 是一次执行实例，不能承载长期配置。
- Profile 是配置快照或覆盖层，不应替代 Agent App。
- Skills、Tools、MCP、Worker、Workflow 都是 Agent App 的组成能力，不应独立决定运行入口。
- Registry 是当前上下文已拥有、可引用的资源库；Marketplace 是外部发现、安装、更新入口；Catalog 是企业治理视角下的可见资产目录。
- CLI/Desktop/Enterprise 使用同一 Agent App 模型，但来源、存储、权限、发布方式不同。

### 2.3 失败条件

如果不引入 Agent App 层，会出现这些问题：

- CLI 只有 `chat/run`，用户很难表达“用合同审查模式运行”或“用运维巡检模式运行”。
- Desktop 无法自然展示“我的智能体 / 智能体广场 / 最近使用智能体”。
- Enterprise 难以管理“谁能用哪个业务智能应用、哪个版本、哪些工具和 MCP 被授权”。
- 多 Worker 编排会和顶层智能体配置命名冲突。
- 默认 AI 编程能力会固化成产品本身，而不是一个可切换的默认 Agent App。

## 三、核心概念

### 3.1 Agent App / 智能体应用

Agent App 是顶层智能工作单元，面向用户可简称“智能体”，描述一类工作目标所需的能力集合和运行策略。

示例：

- `code`：AI 编程应用，默认启用文件、grep、apply_patch、命令执行、代码相关 Skills。
- `doc-review`：文档审查应用，启用文档读取、批注、摘要、合规审查 Skills。
- `ops-diagnosis`：运维诊断应用，启用命令执行、日志分析、监控 MCP、只读生产策略。
- `customer-support`：客服应用，启用知识库、CRM MCP、话术 Skills、人工升级流程。

Agent App 不是一次任务，也不是一个运行时 Worker。它是可安装、可配置、可发布、可授权、可启动的能力包。

### 3.2 Agent App Profile / 智能体应用配置档

Profile 是 Agent App 在某个产品、环境、用户、项目或租户下的有效配置快照。

Agent App 定义“这个智能体应用是什么”，Profile 定义“在当前上下文里怎么运行”。

Profile 可以来自：

- Agent App 默认配置。
- 用户本地覆盖。
- 项目覆盖。
- CLI flag 临时覆盖。
- Desktop UI 变更。
- Enterprise 租户/角色/项目策略。

### 3.3 Task / Run

Task 是用户或系统发起的工作请求。Run 是 Runtime 对该请求的一次执行记录。

推荐语义：

- `Task` 偏产品和业务层，表示“我要完成什么”。
- `Run` 偏 Runtime 层，表示“这次执行过程和状态”。

CLI 中可以弱化 Task 概念，直接 `agent run` 生成 Run；Enterprise 中 Task 可以持久化、排队、分派、审批和恢复。

### 3.4 Task Template / Run Preset

有些“不同任务搭配不同配置”不应该升级成独立 Agent App，而应该作为 Agent App 内部的任务模板或运行预设。

判断标准：

| 配置对象 | 适用场景 | 示例 |
| --- | --- | --- |
| Agent App | 长期可管理的智能体应用，能力组合明显不同 | `code`、`doc-review`、`ops-diagnosis` |
| Task Template | 同一个 Agent App 下的固定任务流程或输入模板 | `code` 智能体下的 `review-pr`、`fix-bug`、`write-tests` |
| Run Preset | 一次运行的参数组合，不一定持久化为任务模板 | `--model quick --sandbox required --skill xxx` |

例如 AI 编程不是只有一个任务：

```text
Agent App: code
  TaskTemplate: review-pr
  TaskTemplate: fix-bug
  TaskTemplate: write-tests
  TaskTemplate: explain-code
```

这样可以避免把每个小任务都做成一个 Agent App，导致 Registry 变成碎片化的命令列表。

第一轮可以先不实现 Task Template，但模型和命令要预留：

```powershell
genesis agent task list code
genesis agent task run code review-pr
genesis agent task create code fix-bug
```

### 3.5 Worker / 工作者

Worker 是 Agent App 内部的执行角色或执行模板。对外展示时，`kind=agent` 的 Worker 可以叫“子智能体”；代码、目录和运行时模型统一使用 Worker。

例如 `code` 智能体应用内部可以有：

- `planner`：规划和拆解。
- `coder`：编辑代码。
- `reviewer`：审查和测试。
- `researcher`：检索资料。

不要把内部执行者直接命名为 Agent。用户侧可以看到“子智能体”，但架构里应表达为 `Worker{Kind: agent}`，它只是 Agent App 的组成部分。

### 3.6 Workflow / 编排

Workflow 描述 Agent App 内部如何调度 Worker、Skills、Tools、MCP 和人工审批。

第一轮可以只支持默认 ReAct 或单 Worker 策略，但模型必须允许未来扩展：

- 单 Worker ReAct。
- Plan-Execute。
- 多 Worker 协作。
- 固定流程节点。
- 人工审批节点。
- 长期任务和恢复。

### 3.7 Registry / Marketplace / Catalog

Registry、Marketplace、Catalog 不是同一个概念，不能混用。

| 概念 | 中文名 | 职责 |
| --- | --- | --- |
| Registry | 资源库 | 当前用户、项目、工作空间或组织已经拥有、已安装、可引用的资源集合 |
| Marketplace | 市场 / 广场 | 官方、社区或企业发布源，用来发现、安装、更新资源 |
| Catalog | 目录 / 资产目录 | 企业治理视角下的可见资产索引，通常带权限、发布状态、版本、审计和审批 |

推荐关系：

```text
Marketplace / 广场
  -> install / import / subscribe
Registry / 资源库
  -> 当前上下文内可用资源
Agent App / 智能体应用
  -> 从 Registry 组合 Prompt / Skills / Tools / MCP / Workers / Workflow
Run
  -> 一次执行
Worker
  -> Run 中实际干活的执行者
```

因此，`marketplace list` 表示看外部有什么可安装，`list` 表示看当前上下文已经拥有、启用、可引用的资源。

## 四、命名决策

| 候选名 | 是否推荐 | 原因 |
| --- | --- | --- |
| Agent App / 智能体应用 | 推荐 | 对外可简称智能体，符合用户心智；技术侧保留 App 后缀，避免和内部 Worker 混淆 |
| Task / 任务 | 不适合作顶层 | 表达一次执行，不适合长期配置和能力包 |
| Agent / 智能体 | 可作用户侧简称，不推荐作代码包名 | 对用户自然，但代码中会和内部执行者混淆，因此技术域使用 Agent App / Worker |
| Assistant / 助手 | 不推荐 | 偏聊天人格，不适合工具、MCP、工作流和企业治理 |
| Mode / 模式 | 可作 CLI 别名 | 适合表达临时切换，但不够承载市场、版本和治理 |
| Scenario / 场景 | 可作为 App 分类 | 偏模板，不像可运行实体 |

最终建议：

- 产品概念：`Agent App / 智能体应用`，对外简称“智能体”
- 技术域：`agentapp`
- CLI 命令：`genesis agent ...`，其中 `agent` 指 Agent App，不指运行时 Worker
- 当前默认智能体应用：`code`
- 执行实例：`Run`
- 业务任务：`Task`
- 内部执行角色：`Worker`；智能体型 Worker 可展示为“子智能体”

## 五、Agent App 模型

第一轮建议模型聚焦“可配置、可切换、可运行”，不要一开始变成企业级大模型。

```go
type AgentApp struct {
    ID          string
    Slug        string
    Name        string
    Description string
    Type        AgentAppType
    Version     string
    Status      AgentAppStatus

    Visibility  AgentAppVisibility
    Source      AgentAppSource
    Scope       CapabilityScope

    Prompt      PromptSpec
    Skills      SkillSet
    Tools       ToolSet
    MCP         MCPSet
    Workers     WorkerSet
    Workflow    WorkflowSpec
    TaskTemplates []TaskTemplateSpec
    Memory      MemorySpec
    Policy      PolicySpec
    Runtime     RuntimeSpec

    Metadata    map[string]string
}
```

### 5.1 AgentAppType

```text
code
chat
rag
workflow
automation
business
custom
```

`code` 是内置默认 Agent App，不是产品本身。用户可以把默认启动 App 切换到其他 App。

### 5.2 AgentAppSource

```text
builtin        # 内置应用，例如 code
local_user     # CLI/Desktop 用户本地应用
local_project  # 项目内应用
marketplace    # 从应用市场安装
enterprise     # 企业发布应用
remote         # 后续远程应用目录
```

### 5.3 AgentAppStatus

```text
draft
active
disabled
deprecated
published
archived
```

CLI/Desktop 本地可以只使用 `active/disabled`；Enterprise 需要 `draft/published/archived`。

## 六、配置合成链

Agent App 的有效运行配置不应只来自单个文件，而应由多层合成。

```text
Builtin App
  -> Installed App
  -> User Default App Config
  -> Project App Override
  -> Enterprise Policy / RBAC
  -> CLI Flags / Desktop 临时选择
  -> Run Request Override
```

优先级从低到高。

合成后的结果叫 `EffectiveAgentAppProfile`。

```go
type EffectiveAgentAppProfile struct {
    AgentAppID       string
    AgentAppSlug     string
    Product     ChannelType
    Environment RuntimeEnvironment

    Prompt      PromptSpec
    Skills      SkillSet
    Tools       ToolSet
    MCP         MCPSet
    Workers     WorkerSet
    Workflow    WorkflowSpec
    TaskTemplates []TaskTemplateSpec
    Memory      MemorySpec
    Policy      PolicySpec
    Runtime     RuntimeSpec

    SourceChain []ProfileSource
}
```

运行时只消费 `EffectiveAgentAppProfile`，不直接读取 App 存储。

## 七、默认 Agent App 与切换策略

### 7.1 默认 Agent App

第一轮内置默认 Agent App：

```text
slug: code
name: AI 编程
type: code
default: true
```

默认能力建议：

- 文件工具：`read_file/write_file/edit_file/apply_patch/list_dir/walk_dir/glob/grep`
- 命令工具：`run_command/write_stdin`
- Skills：代码审查、修改、测试、文档生成类
- Sandbox：CLI 默认本地工作区权限，Desktop 可选本地/沙箱，Enterprise 默认企业沙箱
- Workflow：单 Worker ReAct，后续可切换 Coding Strategy

### 7.2 默认 Agent App 解析顺序

当用户没有显式指定 Agent App 时，按以下顺序解析：

```text
--agent flag
  -> GENESIS_AGENT 环境变量
  -> 项目配置 .genesis/app.yaml 中的 default_app
  -> 用户配置 ~/.genesis-agent/<product>/config.yaml 中的 default_app
  -> 产品内置默认 code
```

解析得到候选 Agent App 后，还必须执行可见性与可运行性校验：

```text
AgentAppResolver
  -> Agent App Catalog 可见性过滤
  -> Product Profile 过滤
  -> Policy / RBAC / Approval 校验
  -> EffectiveAgentAppProfile 合成
```

如果 `--agent` 指定了当前用户不可见或不可运行的 Agent App，必须明确失败，不能静默回退到 `code`。静默回退只允许发生在“未显式指定 Agent App 且默认来源缺失或损坏”的场景，并且需要 warning / audit。

CLI 和 Desktop 都适用。Enterprise 还需要加租户/角色策略：

```text
请求指定 agent app
  -> 用户最近使用 agent app
  -> 项目/空间默认 agent app
  -> 租户默认 agent app
  -> 系统默认 code 或企业默认助手
```

### 7.3 切换 Agent App

切换有三种语义：

| 方式 | 作用范围 | 示例 |
| --- | --- | --- |
| 临时切换 | 当前命令或当前会话 | `genesis run --agent doc-review "审查合同"` |
| 项目默认 | 当前工作区 | `genesis agent use code --scope project` |
| 用户默认 | 当前用户 | `genesis agent use code --scope user` |

Desktop 切换是 UI 当前选中 Agent App；Enterprise 切换是当前 Workspace/Project/Task 的 Agent App 绑定。

## 八、CLI 命令设计

CLI 需要同时支持快速使用、可脚本化配置、可管理 Agent App。

### 8.1 顶层命令原则

- `genesis` 或 `genesis-cli` 默认启动当前默认 Agent App。
- `run/chat` 保留为高频入口，但增加 `--agent`。
- `agent` 子命令负责智能体应用发现、切换、配置、安装和运行。
- `skill` 仍管理 Skills，不升级成顶层工作入口。
- `tools/mcp/worker/workflow` 可以作为 Agent App 配置子资源管理。

### 8.2 高频执行命令

```powershell
genesis chat
genesis chat --agent code
genesis chat --agent customer-support

genesis run "修复这个 bug"
genesis run --agent code "修复这个 bug"
genesis run --agent doc-review "审查 docs\合同.md"
```

推荐让 `chat/run` 都读取默认 Agent App。这样保持旧体验简单，但能力已经 Agent App 化。

### 8.3 Agent App 管理命令

```powershell
genesis agent list
genesis agent show code
genesis agent use code --scope user
genesis agent use doc-review --scope project
genesis agent current

genesis agent create my-review --from code
genesis agent edit my-review
genesis agent delete my-review
```

说明：

- `agent list` 列出当前上下文可见 Agent App，包括 builtin、本地、已安装、企业授权。
- `agent show` 展示提示词、Skills、Tools、MCP、Worker、Workflow 摘要。
- `agent use` 修改默认 Agent App，不直接启动。
- `agent current` 解释当前默认 Agent App 从哪一层解析出来。
- `agent create --from code` 复制内置 Agent App 形成用户自定义 Agent App。
- `agent edit` 第一轮可以打开配置路径或打印路径；Desktop 由 UI 编辑。

### 8.4 Agent App 运行命令

```powershell
genesis agent run code "实现 glob 工具"
genesis agent chat code
```

`agent run/chat` 和顶层 `run/chat --agent` 等价。

推荐保留两种方式：

- 高频短命令：`genesis run --agent code "..."`
- 显式管理语义：`genesis agent run code "..."`

### 8.5 Agent App 配置命令

```powershell
genesis agent config code
genesis agent config code set prompt.system "你是代码审查助手"
genesis agent config code skills add review-fix-rereview
genesis agent config code skills remove old-skill
genesis agent config code tools enable read_file grep apply_patch
genesis agent config code tools disable http_request
genesis agent config code mcp add github --command "github-mcp"
genesis agent config code worker add reviewer --kind agent --from builtin:reviewer
genesis agent config code workflow set react
```

第一轮不一定全部实现，但命令空间要预留清楚。

### 8.6 Agent App 安装与智能体广场

Skills 有技能广场，Agent App 也需要智能体广场。

```powershell
genesis agent marketplace list
genesis agent marketplace add github:org/genesis-apps@v1
genesis agent marketplace update official

genesis agent search code
genesis agent install code-review@official
genesis agent installed
genesis agent enable code-review
genesis agent disable code-review
genesis agent uninstall code-review
```

区别：

- `skill marketplace` 管理可安装 Skill 包。
- `agent marketplace` 管理可安装 Agent App 包。
- Agent App 包可以依赖 Skills、MCP、工具和子智能体 Worker。
- 安装 Agent App 不自动扩大工具权限，只声明需求；运行时仍经过 Profile、Policy、Approval、ToolGateway。

### 8.7 命令是否完善的判断

至少要覆盖五类用户动作：

| 用户动作 | 命令 |
| --- | --- |
| 看到有哪些 Agent App | `genesis agent list/search/show` |
| 切换默认 Agent App | `genesis agent use/current` |
| 运行某个 Agent App | `genesis run --agent` / `genesis agent run` |
| 自定义 Agent App | `genesis agent create/config/edit/delete` |
| 管理 Agent App 内任务模板 | `genesis agent task list/run/create` |
| 安装共享 Agent App | `genesis agent marketplace/install/installed/enable/disable/uninstall` |

这套命令比只加 `--mode` 更完整，因为它同时支持临时使用、默认切换、配置管理和分发治理。

## 九、Desktop 设计

Desktop 应以 Agent App 为第一屏主导航，而不是以“聊天框”作为唯一入口。

推荐结构：

```text
左侧：Agent Apps / 智能体
  - AI 编程 code
  - 文档审查 doc-review
  - 运维诊断 ops-diagnosis
  - 自定义应用

中间：当前 Agent App 的会话 / 任务列表
右侧或设置页：Agent App 配置
  - Prompt
  - Skills
  - Tools
  - MCP
  - Workers
  - Workflow
  - Permissions
```

Desktop 的本地 Agent App 可以使用文件存储，后续同步或导入企业应用。

Desktop 允许：

- 选择当前 Agent App 开始会话。
- 克隆内置 Agent App。
- 从 Agent App Marketplace 安装。
- 为 Agent App 绑定本地 MCP、目录权限、沙箱模式。
- 可视化配置 Workflow 和子智能体 Worker。

Desktop 不应直接绕过能力域修改运行时状态，而应调用 Agent App Catalog Service。

## 十、Enterprise 设计

Enterprise 的 Agent App 是治理对象。

核心能力：

- 租户级 Agent App Catalog。
- Agent App 版本管理。
- Draft / Published / Archived 生命周期。
- RBAC：谁能查看、运行、编辑、发布 Agent App。
- Agent App 绑定项目、部门、角色、用户。
- Agent App 运行审计：使用了哪个版本、哪些 Skills、Tools、MCP、Worker、Workflow。
- 企业 Agent App Marketplace：普通用户看 published apps，管理员管理导入、审核、发布。

Enterprise 中 Agent App 的运行入口：

```text
HTTP API / SSE / Web UI / Webhook / Scheduler
  -> AgentAppResolver
  -> EffectiveAgentAppProfile
  -> Run Engine
```

Enterprise 不应使用 CLI/Desktop 的本地文件 App Store；它应使用 DB repository 和租户策略。

## 十一、目录与边界建议

建议新增能力域：

```text
internal/capabilities/agentapp/
  model/
  contract/
  service/
  adapter/
    memory/
    embedded/

shared/local/agentapp/
  store.go          # CLI/Desktop 本地 Agent App 文件存储
  marketplace.go    # CLI/Desktop 本地/远程 App 安装适配

products/cli/internal/app/
  command.go
  config.go

products/desktop/internal/app/
  service.go
  native.go

products/enterprise/internal/app/
  repository.go
  admin_api.go
  rbac.go
```

边界：

- `internal/capabilities/agentapp` 不 import `products/*`、`shared/local/*`、Wails、PostgreSQL、HTTP handler。
- 本地文件读写在 `shared/local/agentapp`。
- CLI 命令在 `products/cli/internal/app` 或 `products/cli/internal/command`。
- Desktop UI 不直接读写 App 文件，调用产品 app service。
- Enterprise DB/RBAC/Admin API 放 `products/enterprise/internal/app`。
- Runtime 不直接解析 App 存储，只接收 `EffectiveAgentAppProfile`。

## 十二、与现有 Profile / Skill / Tool 的关系

现有 `profilemodel.Profile` 是产品默认能力配置。App 层引入后，Profile 不应消失，而应成为 App 合成后的运行配置的一部分。

关系：

```text
App Definition
  -> App Profile / Override
  -> EffectiveAgentAppProfile
  -> internal/bootstrap BuildOptions / Runtime Profile
```

Skills：

- Skill 是 App 可以引用的能力。
- Skill Marketplace 安装 Skill。
- Agent App Marketplace 安装 Agent App，Agent App 可以声明所需 Skills。

Tools：

- App 声明需要哪些工具。
- Product Profile 和 Policy 决定工具是否可用。
- ToolGateway 执行最终拦截。

MCP：

- App 可以声明 MCP server dependency。
- CLI/Desktop 可以本地配置 MCP。
- Enterprise 由管理员发布或授权 MCP。

Workers：

- App 内可以有一个或多个 Agent。
- Agent 不作为顶层用户入口。

Workflow：

- App 选择默认运行策略。
- 第一轮可以只有 `react`，但模型预留 `plan_execute/multi_agent/workflow`。

## 十三、配置文件建议

### 13.1 用户默认配置

`~/.genesis-agent/cli/config.yaml`

```yaml
app:
  default: code
```

### 13.2 项目默认配置

`.genesis/app.yaml`

```yaml
default_app: code
apps:
  code:
    extends: builtin:code
    tools:
      enabled:
        - read_file
        - grep
        - apply_patch
        - run_command
    skills:
      enabled:
        - review-fix-rereview
```

### 13.3 Agent App 定义文件

`.genesis/apps/doc-review/app.yaml`

```yaml
schema: genesis.app.v1
slug: doc-review
name: 文档审查
type: business
description: 审查合同、方案和产品文档。

prompt:
  system: |
    你是严谨的文档审查助手。

skills:
  enabled:
    - doc
    - review-fix-rereview

tools:
  enabled:
    - read_file
    - grep
  disabled:
    - run_command

workflow:
  strategy: react

task_templates:
  review-contract:
    prompt: "审查输入文档的风险、遗漏和修改建议。"
    skills:
      enabled:
        - doc
    tools:
      enabled:
        - read_file
```

## 十四、第一轮落地建议

第一轮只做可运行骨架：

1. 新增 `internal/capabilities/agentapp/model/contract/service`。
2. 内置 `code` App。
3. CLI 支持 `--agent`。
4. CLI 支持默认 Agent App 解析：flag -> env -> project -> user -> builtin code。
5. CLI 支持：
   - `genesis agent list`
   - `genesis agent show`
   - `genesis agent current`
   - `genesis agent use --scope user|project`
   - `genesis agent run`
   - `genesis agent task list` / `genesis agent task run` 先做只读和运行，`create/edit` 后续实现
6. Agent App 合成为 `EffectiveAgentAppProfile` 后注入现有 bootstrap。
7. 不急着做 Agent App Marketplace、Desktop UI、Enterprise DB。

第二轮再做：

1. Agent App create/edit/config。
2. Agent App Marketplace。
3. Desktop Agent App 管理 UI。
4. Enterprise Agent App DB/RBAC/Admin API。
5. 多 Worker / Workflow 可视化编排。

## 十五、关键决策

| 决策点 | 结论 |
| --- | --- |
| 顶层叫 Agent 吗 | 不叫，避免和内部 Agent 混淆 |
| 顶层叫什么 | App / 应用 |
| code 是什么 | 内置默认 Agent App，而不是产品本身 |
| run/chat 是否保留 | 保留，并支持 `--agent` |
| 是否需要 `agent run` | 需要，作为显式 Agent App 运行入口 |
| 是否需要默认 Agent App | 需要，支持 user/project/env/flag |
| CLI/Desktop/Enterprise 是否统一 | 统一 App 模型和 Service，来源与治理分产品实现 |
| Profile 是否废弃 | 不废弃，变成 EffectiveAgentAppProfile 的一部分 |
| Skill Marketplace 和 Agent App Marketplace 是否重复 | 不重复；Skill 分发能力包，Agent App 分发工作应用 |
| Enterprise 是否天然拥有 Agent App 能力 | 是，但 CLI/Desktop 也必须支持本地 Agent App，因为它们也需要切换不同工作模式 |

## 十六、命令最终建议

第一轮必须实现：

```powershell
genesis run --agent code "修复 bug"
genesis chat --agent code

genesis agent list
genesis agent show code
genesis agent current
genesis agent use code --scope user
genesis agent use code --scope project
genesis agent run code "修复 bug"
genesis agent task list code
genesis agent task run code review-pr
```

后续扩展：

```powershell
genesis agent create my-review --from code
genesis agent config my-review skills add review-fix-rereview
genesis agent config my-review tools enable read_file grep apply_patch

genesis agent marketplace add github:org/genesis-apps@v1
genesis agent search review
genesis agent install code-review@official
```

一句话原则：

> 用户通过 App 选择工作方式，通过 Task/Run 执行一次工作，通过 Agent/Skill/Tool/MCP/Workflow 完成内部协作。





