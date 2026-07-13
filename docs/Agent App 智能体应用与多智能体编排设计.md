# Agent App 智能体应用与多智能体编排设计

> 状态：设计稿（评估 + 重写版）
> 范围：顶层能力配置单元命名、Agent App 模型、多智能体编排分层、CLI/Desktop/Enterprise 统一与差异、默认应用与切换、配置合成链
> 强相关文档：`docs/子智能体设计.md`、`docs/Agent设计模式与多Agent协作空间设计.md`、`docs/项目目录与边界说明.md`、`docs/agent loop设计.md`、`docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`

---

## 一、设计目标与要回答的问题

Genesis 不是一个 AI 编程工具，编程只是它的**默认应用场景**。它的目标是一个通用智能工作平台：用同一套 Runtime 内核，靠不同的提示词、Skills、Tools、MCP、子智能体、编排策略、权限和运行环境组合，去满足代码、文档审查、客服、运维、数据分析、业务办理等各类真实业务需求。

本设计要回答的问题：

1. **要不要做「Agent App」这层抽象**——Claude Code / Codex 是编码工具却「貌似」也能满足不同业务，Genesis 是否有必要引入一个显式的顶层可配置单元？（§二深度评估）
2. **顶层单元叫什么**，既顺应用户对「智能体」的心智，又不与运行时内部执行者混淆。（§三、§八）
3. **两类「智能体」怎么区分**：主 Agent 复杂任务时**自动调用干活的子智能体**，与用户**显式定义的业务智能体**，本质不同、归属不同。（§四）
4. **多智能体怎么编排**：配置文件是要支持多个 Agent 的编排，还是用配置组合多个 Agent App，还是在单个 Agent App 内部编排，还是都可以？（§五给出分层结论）
5. **CLI / Desktop / Enterprise 如何统一又保留差异**，统一与独立如何平衡。（§十~§十三）
6. **Registry / Marketplace / Catalog** 是不是一回事，怎么命名。（§三.7）

一句话结论（先给出，后文论证）：

> **需要 Agent App 层，但要克制**：概念从第一天定对，第一轮实现最小可运行骨架。
> Agent App = 面向一类工作目标的**能力配置包**；Run = 一次执行；**子智能体（SubAgent）是 App 内部的执行细节，不是顶层单元，也不再单独叫 Worker**。
> 多个 Agent App 之间的编排属于更高层的**协作空间 / 团队**对象，不塞进单个 App 的配置文件。

---

## 二、第一性原理：为什么需要 Agent App 层

### 2.1 Claude Code / Codex 为什么「貌似」能满足不同业务？

它们本质是**单一可配置 Agent + 配置即代码**，没有显式的「顶层 App 切换」概念，却能被塑造成不同用途，靠的是这几层可配置面：

| 可配置面 | Claude Code / Codex 做法 | 效果 |
| --- | --- | --- |
| 系统提示 / 项目上下文 | `AGENTS.md` / `CLAUDE.md` | 把「通用编码 Agent」塑造成某项目/某任务的专用行为 |
| Skills（技能包） | Markdown Skill，按需加载 | 注入领域方法论（PPT、PDF、审查流程） |
| SubAgent（子智能体） | `.claude/agents/*.md` / role.toml | 领域专家委派（探索、规划、API 设计） |
| MCP | 声明外部 server | 接入 GitHub、数据库、业务系统 |
| 工具白名单 / 权限模式 | 配置收窄 | 只读、需审批、沙箱 |

也就是说：它们**没有把「工作模式」做成用户可见的一等对象**，而是让用户（开发者）在一个工作区里改配置文件，把这个唯一的 Agent「捏」成需要的样子。

### 2.2 为什么这套对它们够用，对 Genesis 不够

Claude Code / Codex 的隐含前提是：**单一产品面（CLI/IDE）、单一技术型用户、以工作区为边界、用户愿意手写配置文件**。在这些前提下，「当前工作区配置」就等价于一个隐式的「App」，不需要显式对象。

Genesis 的前提完全不同：

| 维度 | Claude Code / Codex | Genesis |
| --- | --- | --- |
| 产品面 | CLI / IDE 单面 | CLI + Desktop + Enterprise 三面 |
| 用户 | 技术型开发者 | 开发者 + 业务人员（客服、运维、审查、办理） |
| 配置方式 | 手写 `AGENTS.md` / md | 业务用户**不会也不该**手写配置 |
| 切换工作模式 | 换工作区 / 改配置 | 需要**一键切换**多个预置工作模式 |
| 治理 | 基本无 | 谁能用哪个能力包、版本、审计、发布、授权 |
| 分发 | 技能/插件 | 需要分发「工作应用」而不仅是「技能」 |

结论：Genesis 的差异化恰恰是它是**平台**而非单一工具。平台必须提供一个**命名、可打包、可切换、可授权、可治理**的顶层能力单元——这正是 Agent App。Desktop 需要它做第一屏导航（我的智能体 / 智能体广场），Enterprise 需要它做治理对象，业务用户需要它来「选择工作方式」而不是「配置一个 Agent 对象」。

> 换个角度：Claude Code 用「配置即代码」替代了显式 App；Genesis 面向非技术用户与企业治理，必须把「配置」升级成**可管理的产品对象**。这不是重复造轮子，而是平台与工具的本质区别。

### 2.3 但要克制：不要一开始做成企业级大模型

引入 Agent App 层的**风险**是过度设计，把每个小任务都做成一个 App，让 Registry 碎片化成命令列表。因此本设计坚持两条纪律：

1. **概念 day1 定对，实现分期最小**：模型、命名、边界一次定清楚（避免后期返工）；第一轮只做「可配置、可切换、可运行」的 `code` 默认应用 + 最小 CLI。
2. **不新增冗余原语**：复用已锁定的 SubAgent / Skill / Tool / Profile，不为 Agent App 另造一套 Worker / 新 Task 语义（见 §三、§八）。

### 2.4 不做会怎样（失败条件）

- CLI 只有 `run/chat`，用户无法表达「用合同审查模式运行」「用运维巡检模式运行」。
- Desktop 无法自然呈现「我的智能体 / 智能体广场 / 最近使用」。
- Enterprise 无法管理「谁能用哪个业务应用、哪个版本、哪些工具/MCP 被授权」。
- 默认 AI 编程能力会固化成产品本身，而不是一个可切换的默认应用。
- 多智能体编排会与顶层配置命名冲突（Agent / Worker / SubAgent 混用）。

---

## 三、概念分层总览（一次讲清所有名词）

为避免命名碰撞，先把整套对象一次分层定义。**核心简化：取消草案里的「Worker」原语，统一用已锁定的「SubAgent（子智能体）」**——同一个东西不要三个名字（Agent / Worker / SubAgent）。

```text
能力原语（可复用、可独立演进、被 App 引用）
  Prompt · Skill · Tool · MCP · SubAgent（子智能体）· Memory · Policy · RuntimeStrategy
        │  组合
        ▼
Agent App / 智能体应用      顶层工作单元：一类工作目标所需能力集合 + 运行策略
  └─ Agent App Profile      在某产品/环境/用户/项目/租户下的配置覆盖层
        │  合成
        ▼
EffectiveAgentAppProfile    运行时真正消费的有效配置（只读快照）
        │  执行
        ▼
Run                         Runtime 对一次 Task/请求的执行记录
  └─ SubAgent（子）Run       主 Run 通过 Task 网关按需委派的子执行（App 内部细节）

更高层（多 App 组合，Phase 3+）
CollaborationSpace / Team   协作空间 / 团队：多个 Agent App（作为成员）+ 人 协同
  └─ 依赖：@mention · Handoff · Blackboard · Message Router · 权限审计
```

### 3.1 Agent App / 智能体应用

顶层智能工作单元，对用户可简称「智能体」。描述一类工作目标所需的能力组合与运行策略。示例：`code`（AI 编程）、`doc-review`（文档审查）、`ops-diagnosis`（运维诊断）、`customer-support`（客服）。

Agent App 是可安装、可配置、可发布、可授权、可启动的**能力包**；它不是一次任务，也不是一个运行时执行者。

### 3.2 Agent App Profile 与 EffectiveAgentAppProfile

- **Agent App** 定义「这个应用是什么」（默认能力与策略）。
- **Agent App Profile** 定义「在当前上下文（产品/环境/用户/项目/租户）里怎么运行」（覆盖层）。
- **EffectiveAgentAppProfile** 是多层合成后的只读结果，运行时只消费它，不直接读 App 存储。（合成链见 §七）

### 3.3 Task / Run（并处理与 LLM 工具 `Task` 的命名碰撞）

- `Task`（业务/产品层）：一次工作请求，「我要完成什么」。CLI 可弱化；Enterprise 可持久化、排队、分派、审批、恢复。
- `Run`（Runtime 层）：一次执行的过程与状态记录。

> **命名消歧（强制，避免与子智能体设计冲突）**：`docs/子智能体设计.md` 已**锁定** LLM 可见工具名 `Task` / `TaskOutput` / `TaskStop` **专指子智能体委派**。本文的业务 `Task` 与 `TaskTemplate` 只存在于**产品 / API / 配置层**，**绝不注册为 LLM function**。三处「Task」语义并存，必须按域区分：
>
> | 名称 | 域 | 含义 |
> | --- | --- | --- |
> | LLM 工具 `Task`/`TaskOutput`/`TaskStop` | 运行时 / LLM schema | 子智能体委派与生命周期（唯一委派入口） |
> | 业务 `Task` / `Run` | 产品 / API / 配置 | 一次工作请求与其执行记录 |
> | `TaskTemplate` | App 配置 | App 内固定任务流程/输入模板（见 §3.4） |
> | `TodoWrite` | 工具 | 计划待办，与上述无关 |

### 3.4 Task Template / Run Preset（避免 App 碎片化）

不是所有「不同任务搭配不同配置」都要升级成独立 App。判断标准：

| 配置对象 | 适用 | 示例 |
| --- | --- | --- |
| Agent App | 长期可管理、能力组合明显不同 | `code`、`doc-review`、`ops-diagnosis` |
| Task Template | 同一 App 下的固定任务流程/输入模板 | `code` 下的 `review-pr`、`fix-bug`、`write-tests` |
| Run Preset | 一次运行的参数组合，不一定持久化 | `--model quick --sandbox required --skill xxx` |

第一轮可不实现 Task Template，但模型与命令空间要预留。

### 3.5 SubAgent（子智能体）——取代草案的 Worker

App 内部的执行单元统一用**子智能体（SubAgent）**，语义、协议、限制**完全遵循 `docs/子智能体设计.md`**（`SubAgentDefinition` / Controller / `Task` 网关 / Catalog / 深度并发硬限 / 三端同源端口）。Agent App **不新造执行者原语**，只做两件事：

1. 声明本 App 的子智能体 Catalog 可见范围（引用内置 explore/plan/general-purpose，或用户/企业定义的领域专家）。
2. 通过 RuntimeStrategy / Workflow 决定何时、如何委派（见 §五）。

> 因此草案里的 `WorkerSet` / `Worker{Kind:agent}` 全部删除，改为「App 引用的 SubAgent Catalog 范围」。对用户展示仍可叫「子智能体」。

### 3.6 RuntimeStrategy / Workflow（App 的运行策略）

App 选择内部运行策略：第一轮只支持单 Agent `react`；模型预留 `plan_execute` / `sequential` / `supervisor` 等（属 App **内部**编排，见 §五 L2）。

### 3.7 Registry / Marketplace / Catalog（不是一回事）

| 概念 | 中文 | 职责 |
| --- | --- | --- |
| Registry | 资源库 | 当前上下文已拥有、已安装、可引用的资源集合 |
| Marketplace | 市场 / 广场 | 官方/社区/企业发布源，用于发现、安装、更新 |
| Catalog | 目录 / 资产目录 | 企业治理视角的可见资产索引（含权限、发布状态、版本、审计） |

关系：`Marketplace --install/subscribe--> Registry --组合--> Agent App --执行--> Run --委派--> SubAgent`。
因此 `marketplace list` 看外部可安装什么，`list` 看当前上下文已拥有/可引用什么。

---

## 四、两类「智能体」的根本区分（用户核心问题）

用户明确要区分：**主 Agent 复杂任务时自动调用干活的子智能体** vs **显式定义的业务智能体**。它们是**两个不同的东西，属于两个不同的层，不要混为一谈**。

| 维度 | 干活的子智能体（SubAgent） | 业务智能体（Business Agent） |
| --- | --- | --- |
| 本质 | Agent App **内部**的执行细节 | 就是**一个 Agent App**（或其在协作空间中的成员投影） |
| 用户可见性 | 不作为顶层入口；用户看到的是「主 Agent 委派了 explore」 | 顶层可见、可选择、可 @、可治理 |
| 谁创建 | 主模型经 `Task` 网关按需委派（或 Skill fork / mention） | 用户/管理员显式定义、安装、发布 |
| 「自动」含义 | **提示词驱动**：内置 Catalog 常驻 + System/Task description 引导主模型主动委派；**禁止**复杂度引擎静默 fork（对齐子智能体设计 §3.4） | 不自动产生；由人显式配置或安装 |
| 典型例子 | explore、plan、general-purpose、api-designer、security-reviewer | `code`、`doc-review`、政策Agent、材料Agent、审核Agent |
| 归属文档 | `docs/子智能体设计.md`（运行时与协议） | 本文 + 协作空间文档 |
| 生命周期 | 一次委派的子 Run；深度/并发硬限 | 长期能力包；版本/发布/授权 |

### 4.1 两个正交的轴

关键洞察：这两者是**正交的两个轴**，一个 App 可以同时用到：

- **纵向（App 内部委派）**：一个 Agent App 运行时，主循环向下委派 SubAgent 干专项活。这就是「复杂任务自动调用干活的子智能体」。
- **横向（App 之间协作）**：多个 Agent App 作为成员，在协作空间里平级协同（@、handoff、群聊）。这就是「显式定义的业务智能体」相互协作。

```text
业务智能体 A（Agent App）──@──业务智能体 B（Agent App）      ← 横向：协作空间（L3）
      │                              │
   ├─ 子智能体 explore              ├─ 子智能体 material-checker   ← 纵向：App 内委派（L1）
   └─ 子智能体 reviewer             └─ 子智能体 rule-validator
```

### 4.2 桥接关系（重要）

- 一个 **Agent App 可以被「暴露为」协作空间里的一个 Agent 成员**（Agent-as-member）。即：横向的「业务智能体成员」= 某个 Agent App 的对外投影。
- 一个 **Agent App 内部可以使用子智能体**（纵向）。
- 因此「业务智能体」和「子智能体」不是竞争关系：业务智能体是**用户/治理视角的顶层单元**，子智能体是**运行时视角的内部执行单元**。不要用一个去实现另一个的全部语义。

---

## 五、多智能体编排：三层模型（用户核心问题）

用户问：配置文件是要支持多个 Agent 编排？还是配置组合多个 Agent App？还是 App 内部编排？还是都可以？

**结论：都支持，但分在三个不同的层、用三个不同的对象，绝不把它们塞进同一个配置文件。**

| 层 | 名称 | 谁编排谁 | 用什么对象/机制 | 落地阶段 |
| --- | --- | --- | --- | --- |
| **L1** | App 内委派 | 单 App 的主循环 → 子智能体 | `Task` 网关 + Controller（子智能体设计） | Phase 1 |
| **L2** | App 内编排 | 单 App 的 Workflow → 多个内部步骤/角色/子智能体 | App 的 `RuntimeStrategy`/`Workflow`（plan_execute / sequential / supervisor） | Phase 2 |
| **L3** | App 间协作 | 多个 Agent App（成员）+ 人 | 独立的 **CollaborationSpace / Team** 对象 + Message Router | Phase 3+ |

### 5.1 明确回答用户的三个选项

1. **「配置文件要支持多个 Agent 的编排吗？」**
   - **单个 Agent App 的配置文件（`app.yaml`）只表达 L1 + L2**：本 App 的提示词、能力、内部运行策略、可用子智能体。它**不是**一张多 App 编排图。
2. **「用配置组合多个 Agent App 吗？」**
   - **是的，但在 L3 的独立对象里**（CollaborationSpace / Team），不是在单个 App 的 `app.yaml` 里。多 App 编排是一个更高层对象，有自己的成员管理、消息路由、权限审计。
3. **「在 Agent App 内部编排吗？」**
   - **是的，那就是 L2**：单 App 内部用 Workflow 策略编排多个步骤/角色/子智能体。owner 与治理边界仍是这一个 App。

> **L2 与 L3 的边界（防止 supervisor 语义串层）**：L2 的 `supervisor`/`sequential` 只在**单个 App 的治理边界内**编排它自己的步骤与子智能体（一个 owner、一套审计口径、走 L1 的 `Task` 网关）。一旦编排对象是**多个独立 Agent App 成员**（各有自己的 owner/授权/审计），就属于 L3，必须走 CollaborationSpace，不得用 App 内 Workflow 去「跨 App 调度」。判据：被编排者是否是本 App 内部的子智能体——是则 L2，否则 L3。

### 5.2 为什么这样分（设计理由）

- **单一职责与可组合性**：如果把「多 App 编排图」塞进单个 `app.yaml`，App 就不再是一个干净的、可发布/可授权的能力包，而变成一个耦合了他人的编排脚本，破坏 §二 的可治理性。
- **治理边界清晰**：L1/L2 的治理边界是「这一个 App」；L3 的治理边界是「这个协作空间/团队」（谁能加入哪些 Agent 成员、谁能 @ 谁）。两者审计口径不同。
- **对齐已有设计**：L1 完全复用 `docs/子智能体设计.md`；L3 完全对齐 `docs/Agent设计模式与多Agent协作空间设计.md` 的 @Agent 协作空间。本文只需把 L2 定义清楚，并声明 L3 的挂接点，不重复展开。

### 5.3 一个例子串起来

政务「边聊边办」场景：

- **L3 协作空间**：办事大厅空间里有 `政策Agent`、`材料Agent`、`审核Agent` 三个业务智能体（各是一个 Agent App）+ 人类坐席。用户 `@政策Agent` 咨询，`政策Agent` 处理后 `@材料Agent` 交接（Handoff）。
- **L2 App 内编排**：`审核Agent` 这个 App 内部用 `sequential` 策略：先规则校验 → 再证据检索 → 再出结论。
- **L1 App 内委派**：`材料Agent` 处理时，主循环向下 `Task(subagent_type=ocr-extract)` 委派一个只读子智能体抽取营业执照字段。

三层各司其职，互不越界。

---

## 六、Agent App 模型

第一轮聚焦「可配置、可切换、可运行」，不要一开始做成企业级大模型。模型的核心是一个共享内核 `AgentSpec` + App 特有的治理/编排字段（`AgentSpec` 与 `SubAgentDefinition` 共用，见 §6.1.1）。**注意：已删除草案的 `WorkerSet`，改为 `SubAgents`（子智能体 Catalog 引用）+ `Workflow`（内部编排策略）。**

```go
// 技术域包名：agentapp（不要叫 agent，避免与领域模型/运行时执行者/子智能体混淆）

// 共享内核：一个「可执行智能单元」的最小能力规格（agentapp 与 subagent 共用）
type AgentSpec struct {
    Prompt     PromptSpec
    Skills     SkillSet       // 引用 Skill 能力域
    Tools      ToolSet        // 声明需要的工具（受 Product Profile/Policy 约束）
    MCP        MCPSet         // 声明依赖的 MCP server
    Model      string         // inherit 或具名
    Permission PermissionMode // 只收窄语义（与子智能体设计的全序一致）
    Sandbox    SandboxRef
}

type AgentApp struct {
    // 顶层 / 治理 / 分发元数据
    ID          string
    Slug        string
    Name        string
    Description string
    Version     string
    Type        AgentAppType
    Status      AgentAppStatus
    Visibility  AgentAppVisibility
    Source      AgentAppSource
    Scope       profilemodel.CapabilityScope // 复用现有 CapabilityScope

    Spec AgentSpec // 共享内核：Prompt/Skills/Tools/MCP/Model/Permission/Sandbox

    // App 特有：内部编排 + 子智能体范围 + 任务模板 + 记忆/策略/运行
    SubAgents     SubAgentScope // App 可见的子智能体 Catalog 范围（引用 subagent 能力域，取代 Worker）
    Workflow      WorkflowSpec  // L2 内部编排策略；第一轮仅 react
    TaskTemplates []TaskTemplateSpec
    Memory        MemorySpec
    Policy        PolicySpec
    Runtime       RuntimeSpec
    Metadata      map[string]string
}
```

### 6.1 关键不变量：只收窄不放宽（对齐子智能体权限纪律）

> **Agent App 只能在 Product Profile 允许的能力宇宙内选择/收窄，绝不能放宽。** Product Profile（产品默认能力配置，`profilemodel.Profile`）与企业 Policy 决定「哪些工具/MCP/沙箱模式**存在且被允许**」；Agent App 只能在其中挑选与进一步收窄。App 声明需要某工具**不等于**授予权限——最终仍经 ToolGateway / Approval / Policy 拦截。

这与 `docs/子智能体设计.md` 的「子 Agent 权限只收紧不放宽」是同一条纪律，贯穿：Product Profile ⊇ Agent App ⊇ SubAgent。

### 6.1.1 与 SubAgentDefinition 的共享内核（避免两套 schema 漂移）

`AgentApp` 与 `docs/子智能体设计.md` 的 `SubAgentDefinition` 有约 70% 字段重叠（`prompt` / `tools` / `skills` / `mcp` / `model` / `permission` / `sandbox`）。若各写各的，两套 schema 会随时间漂移，且「把一个 App 暴露为子智能体 / 协作成员」会很别扭。

**决策：二者共用 §六 模型中的共享内核 `AgentSpec`，各自只加特有字段**：

- `AgentApp` = `AgentSpec` + 治理/分发元数据 + App 特有编排字段（见 §六 结构体）。
- `SubAgentDefinition` = `AgentSpec` + 委派特有字段（`max_depth` / `fork_context` / `execution_mode` / `budget` / `timeout`）。
- 两者共用校验、只收窄裁剪、序列化逻辑；**「App 暴露为协作成员 / 子智能体」= 直接复用其 `AgentSpec`**，无需转换，也不会 schema 漂移。

### 6.2 AgentAppType（仅分类，不决定运行行为）

```text
code | chat | rag | workflow | automation | business | custom
```

> **Type 只是展示/市场分类**（用于广场筛选、图标、默认模板），**不决定运行行为**。运行行为由 `Workflow.strategy`（react/sequential/plan_execute…）与 `AgentSpec` 决定。因此 `type=workflow` 与 `workflow.strategy=sequential` 不冲突：前者是「这是一个以流程编排为卖点的应用」的标签，后者是实际执行策略。避免把 Type 当成行为开关。

`code` 是内置默认应用，不是产品本身；用户可把默认启动应用切换到其他 App。

### 6.3 AgentAppSource

```text
builtin        # 内置应用，如 code
local_user     # CLI/Desktop 用户本地应用
local_project  # 项目内应用
marketplace    # 应用市场安装
enterprise     # 企业发布应用
remote         # 后续远程应用目录
```

### 6.4 AgentAppStatus

```text
draft | active | disabled | deprecated | published | archived
```

CLI/Desktop 本地可只用 `active/disabled`；Enterprise 需要 `draft/published/archived`。

### 6.5 易混字段消歧（保持模型正交）

- **Visibility vs Scope**：`Visibility`（private/team/public）表达「所有者意图的可见级别」；`Scope`（channels/tenants/projects/users/roles/envs）表达「命中规则的过滤条件」。二者正交：Visibility 是意图声明，Scope 是硬过滤；最终可见 = Visibility 允许 **且** Scope 命中 **且** Policy/RBAC 放行。
- **App.Policy vs Enterprise Policy**：`App.Policy` 只能**追加约束**（如本 App 强制某工具需审批、强制沙箱），不能放宽；Enterprise Policy 是租户安全基线，优先级更高（§七阶段二）。两者都只收窄。
- **Product Profile vs 内置 `code` App**：Product Profile 是**产品维度的能力宇宙 + 硬策略（上界）**，与「选哪个 App」无关；内置 `code` App 是**在该上界内的一份具体能力选择**。二者不重复：删除 `code` App 后产品仍有能力宇宙，但没有默认工作模式可选。落地时 `code` App 的默认能力应**引用/收窄** Product Profile，而非复制一份平行清单。

---

## 七、配置合成链与 EffectiveAgentAppProfile

合成分**两个阶段**，不能混成一条「后者全覆盖前者」的链——否则 CLI flag 会「覆盖」企业策略，破坏安全基线。

**阶段一：配置选择/覆盖（低→高，后者覆盖前者同名项）**——决定「选哪个 App、prompt/skills/workflow 等偏好」：

```text
Builtin App
  → Installed App（marketplace/plugin）
  → User Default App Config
  → Project App Override（.genesis/app.yaml）
  → CLI Flags / Desktop 临时选择
  → Run Request Override
```

**阶段二：硬约束收窄（取交集，任何层都不可放宽）**——对阶段一结果做能力裁剪：

```text
EffectiveTools/Skills/MCP/SubAgents =
    阶段一结果
  ∩ Product Profile（能力宇宙 + 硬策略：沙箱、危险工具）      ← 上界
  ∩ Enterprise Policy / RBAC（租户安全基线）                 ← 最终否决权
```

- **Product Profile 是上界**：规定能力宇宙与硬策略，任何 App/flag/override 只能在其内收窄，不能新增其不允许的工具/MCP/沙箱降级。
- **Enterprise Policy / RBAC 拥有最终否决权**：它只做**收窄**，不参与阶段一的偏好覆盖竞争，因此不会被后面的 CLI flag / Run override 放宽。
- 一句话：**偏好可以层层覆盖，能力只能层层收窄**——有效能力 ⊆ `Product Profile ∩ Enterprise Policy`，且 `SubAgent 能力 ⊆ App 能力 ⊆ 上述交集`（与 §6.1 一致）。

合成结果 `EffectiveAgentAppProfile`（只读）：

```go
type EffectiveAgentAppProfile struct {
    AgentAppID   string
    AgentAppSlug string
    Product      profilemodel.ChannelType
    Environment  profilemodel.RuntimeEnvironment

    Prompt      PromptSpec
    Skills      profilemodel.SkillSet
    Tools       profilemodel.ToolSet
    MCP         MCPSet
    SubAgents   SubAgentScope
    Workflow    WorkflowSpec
    TaskTemplates []TaskTemplateSpec
    Memory      MemorySpec
    Policy      PolicySpec
    Runtime     RuntimeSpec

    SourceChain []ProfileSource // 可解释：每层来源，供 `agent current` 诊断
}
```

运行时只消费 `EffectiveAgentAppProfile`，并注入现有 `internal/bootstrap` 的 `BuildOptions`（当前已有 `Profile` / `DefaultAgentID`；App 层在其上合成）。

---

## 八、命名决策

| 候选 | 是否推荐 | 原因 |
| --- | --- | --- |
| Agent App / 智能体应用 | ✅ 推荐 | 对外简称智能体；技术域保留 App 后缀，避免与内部执行者混淆 |
| Task / 任务 | 顶层不用 | 表达一次执行，不承载长期配置 |
| Agent / 智能体 | 仅用户侧简称 | 对用户自然，代码中会与执行者混淆；技术域用 `agentapp` / `subagent` |
| Assistant / 助手 | 不推荐 | 偏聊天人格，不适合工具/MCP/工作流/治理 |
| Mode / 模式 | 可作 CLI 别名 | 适合临时切换，不足以承载市场/版本/治理 |
| **Worker** | **删除** | 与已锁定的 SubAgent 重复，避免 Agent/Worker/SubAgent 三名并存 |

最终命名基线：

- 产品概念：`Agent App / 智能体应用`，对外简称「智能体」
- 技术域：`agentapp`
- CLI 命令：`genesis agent ...`（`agent` 指 Agent App，不指子智能体）
- 默认应用：`code`
- 执行实例：`Run`；业务任务：`Task`（产品层，非 LLM 工具）
- 内部执行单元：`SubAgent`（子智能体），协议见子智能体设计
- 多 App 组合：`CollaborationSpace / Team`（L3）

---

## 九、默认 Agent App 与切换策略

### 9.1 默认应用

第一轮内置：`slug: code`、`name: AI 编程`、`type: code`、`default: true`。默认能力：文件工具（read/write/edit/apply_patch/list/glob/grep）、命令工具（run_command/write_stdin）、代码类 Skills、内置子智能体 explore/plan/general-purpose、Workflow=react；沙箱 CLI 默认本地工作区、Desktop 可选、Enterprise 默认企业沙箱。

### 9.2 解析顺序

未显式指定时：

```text
--agent flag
  → GENESIS_AGENT 环境变量
  → 项目 .genesis/app.yaml 的 default_app
  → 用户 ~/.genesis-agent/<product>/config.yaml 的 default_app
  → 产品内置默认 code
```

解析出候选后，必须做**可见性与可运行性校验**：

```text
AgentAppResolver
  → Catalog 可见性过滤（Scope）
  → Product Profile 过滤
  → Policy / RBAC / Approval 校验
  → EffectiveAgentAppProfile 合成
```

> **静默回退纪律**：若 `--agent` 指定了当前不可见/不可运行的 App，**必须明确失败**，不得静默回退到 `code`。静默回退只允许发生在「未显式指定且默认来源缺失/损坏」，且必须 warning + audit。（与子智能体、沙箱的「不可静默降级」一致。）

Enterprise 增加租户/角色层：`请求指定 → 用户最近使用 → 项目/空间默认 → 租户默认 → 系统默认 code`。

### 9.3 切换

| 方式 | 范围 | 示例 |
| --- | --- | --- |
| 临时切换 | 当前命令/会话 | `genesis run --agent doc-review "审查合同"` |
| 项目默认 | 当前工作区 | `genesis agent use code --scope project` |
| 用户默认 | 当前用户 | `genesis agent use code --scope user` |

Desktop 切换 = UI 当前选中 App；Enterprise 切换 = 当前 Workspace/Project/Task 的 App 绑定。

---

## 十、CLI 命令设计

原则：既保留高频快速入口，又提供可脚本化配置与 App 管理。`skill` 仍只管理 Skills，不升级成顶层入口；`tools/mcp` 作为 App 配置子资源。（下例用二进制 `genesis-cli`；文中 `genesis` 为简写。）

### 10.1 高频执行

```powershell
genesis run "修复这个 bug"
genesis run --agent doc-review "审查 docs\合同.md"
genesis chat --agent customer-support
```

`run/chat` 默认读取解析出的默认 App。

### 10.2 App 管理

```powershell
genesis agent list                       # 当前上下文可见 App（builtin/本地/已安装/企业授权）
genesis agent show code                  # 展示 prompt/skills/tools/mcp/子智能体/workflow 摘要
genesis agent current                    # 解释当前默认 App 从哪一层解析出来（SourceChain）
genesis agent use code --scope user
genesis agent use doc-review --scope project
genesis agent create my-review --from code
genesis agent edit my-review
genesis agent delete my-review
```

### 10.3 App 运行

```powershell
genesis agent run code "实现 glob 工具"     # 与 genesis run --agent code 等价（显式管理语义）
genesis agent chat code
```

### 10.4 App 配置（命令空间预留，第一轮不必全实现）

```powershell
genesis agent config code skills add review-fix-rereview
genesis agent config code tools enable read_file grep apply_patch
genesis agent config code mcp add github --command "github-mcp"
genesis agent config code subagent add reviewer --from builtin:reviewer   # 子智能体，非 Worker
genesis agent config code workflow set react
```

### 10.5 Task Template（预留）

```powershell
genesis agent task list code
genesis agent task run code review-pr
genesis agent task create code fix-bug
```

### 10.6 应用市场（智能体广场）

```powershell
genesis agent marketplace list
genesis agent marketplace add github:org/genesis-apps@v1
genesis agent search review
genesis agent install code-review@official
genesis agent installed
genesis agent enable code-review
genesis agent uninstall code-review
```

区别：`skill marketplace` 管可安装 Skill；`agent marketplace` 管可安装 Agent App（App 包可声明依赖 Skills/MCP/工具/子智能体）。**安装 App 不自动扩权**，只声明需求，运行时仍经 Profile/Policy/Approval/ToolGateway。

### 10.7 命令完备性自检

| 用户动作 | 命令 |
| --- | --- |
| 看有哪些 App | `agent list/search/show` |
| 切换默认 App | `agent use/current` |
| 运行某 App | `run --agent` / `agent run` |
| 自定义 App | `agent create/config/edit/delete` |
| App 内任务模板 | `agent task list/run/create` |
| 安装共享 App | `agent marketplace/install/installed/enable/disable/uninstall` |

---

## 十一、Desktop 设计

Desktop 以 Agent App 为第一屏主导航，而非仅聊天框：

```text
左侧：Agent Apps / 智能体      （AI 编程 / 文档审查 / 运维诊断 / 自定义 / 广场）
中间：当前 App 的会话 / 任务列表
右侧（设置页）：App 配置        （Prompt / Skills / Tools / MCP / 子智能体 / Workflow / Permissions）
```

Desktop 允许：选择当前 App 开始会话；克隆内置 App；从广场安装；为 App 绑定本地 MCP/目录权限/沙箱模式；可视化配置 Workflow（L2）与子智能体范围。Desktop **不直接绕过能力域改运行时状态**，只调用产品 App Service。

---

## 十二、Enterprise 设计

Enterprise 的 Agent App 是治理对象：租户级 Catalog；版本管理；Draft/Published/Archived 生命周期；RBAC（谁能查看/运行/编辑/发布）；App 绑定项目/部门/角色/用户；运行审计（用了哪个版本、哪些 Skills/Tools/MCP/子智能体/Workflow）；企业 App 广场（普通用户看 published，管理员管理导入/审核/发布）。

运行入口：`HTTP API / SSE / Web UI / Webhook / Scheduler → AgentAppResolver → EffectiveAgentAppProfile → Run Engine`。

Enterprise 不用 CLI/Desktop 的本地文件 App Store，而用 DB repository + 租户策略。**多租户不变量**：App 与其 Run/子 Run 必须带 `tenant_id`，跨租户视为安全事件（对齐 AGENTS.md 多租户规则与子智能体设计 §10）。

---

## 十三、三端同源、后端异构（端口注入，对齐子智能体设计）

CLI/Desktop/Enterprise **共用同一套 `agentapp` 内核代码**（模型、合成、Resolver、Service），通过 bootstrap 注入不同**端口后端**实现差异，内核**零 `if product == ...` 分支**。这与 `docs/子智能体设计.md` 的「三端同源、后端异构」是同一范式。

| 端口 | 职责 | CLI/Desktop 后端 | Enterprise 后端 |
| --- | --- | --- | --- |
| `AppStore` | App 定义与实例的读写 | 文件 / SQLite | PostgreSQL + 租户隔离 |
| `AppSource` | App 定义来源加载与合并 | 内置 + 本地文件（用户/项目） | DB / 对象存储 + 策略下发 |
| `AppMarketplace` | 发现/安装/更新 App 包 | 本地/远程 JSON 仓 | 企业发布源 + 审核 |
| `AppPolicy` | 可见性/可运行性/RBAC 校验 | 轻量本地策略 | 租户 RBAC + 审批 |

- **统一什么**：`AgentApp` / `EffectiveAgentAppProfile` 模型、合成链、`AgentAppResolver`、命令/协议语义、`只收窄不放宽`不变量、静默回退纪律。
- **各自独立什么**：Store/Source/Marketplace/Policy 的后端实现，以及 Desktop 画布投影、Enterprise 分布式与审计。

---

## 十四、目录与边界建议

```text
internal/capabilities/agentapp/
  model/        # AgentApp / EffectiveAgentAppProfile / Type / Source / Status
  contract/     # AppStore / AppSource / AppMarketplace / AppPolicy / Resolver 端口
  service/      # 合成链、Resolver、Catalog、校验
  adapter/
    memory/     # 内存实现（测试/CLI 兜底）
    embedded/   # 内置 code 应用定义

shared/local/agentapp/
  store.go        # CLI/Desktop 本地文件/SQLite App 存储（实现 AppStore/Source）
  marketplace.go  # CLI/Desktop 本地/远程 App 安装适配

products/cli/internal/app/         # agent 命令、本地 config 解析
products/desktop/internal/app/     # App Service、native 投影
products/enterprise/internal/app/  # repository、admin_api、rbac
```

边界（遵循 `docs/项目目录与边界说明.md`）：

- `internal/capabilities/agentapp` **不** import `products/*`、`shared/local/*`、Wails、PostgreSQL、HTTP handler；只依赖端口 + `internal/domain`/`internal/platform`。
- 本地文件读写在 `shared/local/agentapp`；Enterprise DB/RBAC/Admin API 在 `products/enterprise/internal/app`。
- Runtime 不解析 App 存储，只接收 `EffectiveAgentAppProfile`。
- CollaborationSpace（L3）落在 `internal/runtime/multiagent`（协作空间 patterns）+ 产品层，**不**放进 `agentapp`（App 是能力包，协作空间是编排对象）。

---

## 十五、与现有 Profile / Skill / Tool / SubAgent 的关系

```text
Product Profile（profilemodel.Profile，产品默认能力配置，上界）
  → Agent App 定义（在上界内选择/收窄）
  → Agent App Profile / Override
  → EffectiveAgentAppProfile
  → internal/bootstrap.BuildOptions（现有 Profile / DefaultAgentID 之上合成）
```

- **Profile 不废弃**，成为 Agent App 合成的上界与运行配置的一部分。
- **Skill**：App 声明所需 Skills；Skill Marketplace 装 Skill 包；App Marketplace 装 App 包（App 可声明依赖 Skills）。Skill fork → 子智能体，走子智能体 Controller。
- **Tool**：App 声明需要哪些工具；Product Profile + Policy 决定是否可用；ToolGateway 最终拦截。
- **MCP**：App 声明依赖；CLI/Desktop 本地配置；Enterprise 管理员发布/授权。
- **SubAgent**：App 声明可见的子智能体 Catalog 范围（取代 Worker）；运行时委派完全走 `docs/子智能体设计.md` 的 `Task` 网关与 Controller。App 不重新实现子智能体运行时。

---

## 十六、配置文件建议

### 16.1 用户默认 `~/.genesis-agent/cli/config.yaml`

```yaml
app:
  default: code
```

### 16.2 项目默认 `.genesis/app.yaml`

```yaml
default_app: code
apps:
  code:
    extends: builtin:code
    tools:
      enabled: [read_file, grep, apply_patch, run_command]
    skills:
      enabled: [review-fix-rereview]
```

### 16.3 Agent App 定义 `.genesis/apps/doc-review/app.yaml`

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
  enabled: [doc, review-fix-rereview]

tools:
  enabled: [read_file, grep]
  disabled: [run_command]

# 本 App 可见的子智能体范围（引用 subagent 能力域，非 Worker）
subagents:
  enabled: [explore]          # 只读探索；不可再委派（max_depth 由子智能体设计约束）

workflow:
  strategy: react             # L2 内部编排；第一轮仅 react，预留 sequential/plan_execute

task_templates:
  review-contract:
    prompt: "审查输入文档的风险、遗漏和修改建议。"
    skills:
      enabled: [doc]
    tools:
      enabled: [read_file]
```

> 注意：单个 `app.yaml` **不表达多 App 编排**（那是 L3 CollaborationSpace 的独立配置，Phase 3+ 另行定义）。

---

## 十七、分期落地

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| **Phase 1** | `agentapp` model/contract/service/embedded；内置 `code`；`AgentAppResolver`（flag→env→project→user→builtin，含可见性/Policy 校验）；CLI `--agent` + `agent list/show/current/use/run`；合成 `EffectiveAgentAppProfile` 注入 `BuildOptions`。L1 子智能体委派复用子智能体设计 Phase 1。 | 零配置下 `code` 可运行；`--agent 未知` 明确失败不静默回退；`agent current` 能给出 SourceChain |
| **Phase 2** | App `create/edit/config`；Task Template 只读+运行；App Marketplace（本地/远程）；L2 Workflow（sequential/plan_execute）；`delegation_posture` 等对齐子智能体 Phase 2 | 自定义 App 与内置并存；App 内可选非 react 策略 |
| **Phase 3** | Desktop App 管理 UI + 画布；Enterprise App DB/RBAC/Admin API/审计/版本发布；多租户不变量强校验 | 三产品演示通路；企业治理闭环 |
| **Phase 4** | L3 CollaborationSpace / Team：Agent App 暴露为成员、@mention/Handoff/Blackboard；对齐协作空间文档 | 多 App + 人协同，不破坏 L1/L2 协议 |

每阶段结束：删除被替换的临时代码与开关，不累积兼容层（对齐 AGENTS.md「开发阶段不兼容旧代码」）。

---

## 十八、关键决策摘要

| 决策点 | 结论 |
| --- | --- |
| 是否需要 Agent App 层 | **需要**：平台面向多产品 + 业务用户 + 治理，Claude Code 的「配置即代码」不足以支撑；但概念 day1 定对、实现分期最小 |
| 顶层叫什么 | `Agent App / 智能体应用`（技术域 `agentapp`），对外简称智能体 |
| 是否新增 Worker 原语 | **否，删除**；App 内部执行单元统一用 SubAgent（子智能体），协议见子智能体设计 |
| AgentApp 与 SubAgentDefinition 关系 | 抽共享内核 `AgentSpec`，两者各自组合，避免两套 schema 漂移；App 暴露为子智能体/协作成员直接复用 `AgentSpec` |
| AgentAppType 的作用 | 仅展示/市场分类，不决定运行行为；行为由 `Workflow.strategy` + `AgentSpec` 决定 |
| Visibility vs Scope | 正交：Visibility=可见级别意图，Scope=硬过滤条件；最终可见需两者 + Policy 同时放行 |
| `code` 是什么 | 内置默认 Agent App，不是产品本身 |
| 业务 `Task` 与 LLM 工具 `Task` | 严格分域：业务 Task/TaskTemplate 只在产品/API/配置层，绝不注册为 LLM function |
| 干活子智能体 vs 业务智能体 | 两个正交轴：子智能体=App 内部纵向委派（运行时细节）；业务智能体=Agent App 本身（顶层/横向协作成员） |
| 多智能体编排如何分层 | L1 App 内委派 / L2 App 内 Workflow / L3 App 间协作空间；三者都支持，分对象分阶段 |
| 单个 app.yaml 是否表达多 App 编排 | **否**；多 App 编排在 L3 独立对象 CollaborationSpace/Team |
| App 权限边界 | 只收窄不放宽：Product Profile ⊇ Agent App ⊇ SubAgent；App 声明≠授权，仍经 Gateway/Policy |
| 未知 `--agent` | 明确失败，禁止静默回退到 code；回退须 warning+audit |
| 三端统一/独立 | 内核同源（agentapp model/service/Resolver）；差异靠 AppStore/AppSource/AppMarketplace/AppPolicy 端口后端注入，内核零产品分支 |
| Profile 是否废弃 | 不废弃，成为 App 合成的上界与 EffectiveAgentAppProfile 的一部分 |
| Skill/App Marketplace 是否重复 | 不重复：Skill 分发能力包，App 分发工作应用（可依赖 Skills） |

---

## 十九、与现有文档的关系 / 残留风险

| 文档 | 关系 |
| --- | --- |
| `docs/子智能体设计.md` | **L1 委派运行时与协议以其为准**；本文的「子智能体」= 其 SubAgent；删除本文早期草案的 Worker |
| `docs/Agent设计模式与多Agent协作空间设计.md` | **L3 协作空间以其为准**；本文只声明 App 暴露为成员的挂接点 |
| `docs/项目目录与边界说明.md` | 目录与依赖边界；新增 `agentapp` 能力域须遵守 |
| `docs/agent loop设计.md` | Run/Session/Profile/BuildOptions 对齐；`EffectiveAgentAppProfile` 注入现有 `BuildOptions` |
| `internal/capabilities/profile/model/profile.go` | 复用 `ChannelType`/`RuntimeEnvironment`/`CapabilityScope`/`ToolSet`/`SkillSet`，不另造平行模型 |

冲突时：**运行时/协议以子智能体设计与协作空间设计为准；顶层配置单元与命名以本文为准**。

**残留风险（不阻塞 Phase 1）**：

1. L3 CollaborationSpace 的成员模型与 Agent App「暴露为成员」的投影字段需在 Phase 4 与协作空间文档合并定稿。
2. Task Template 与 Run Preset 的持久化边界（哪些进 App 定义、哪些仅运行参数）待 Phase 2 结合实际用例细化。
3. App Marketplace 的依赖解析（App→Skills/MCP/子智能体）与版本冲突策略，需与 `capability-package-marketplace-plugin-design.md` 对齐后定稿。
