# Skill 多 Invocation 与运行时侧车设计

> 状态：核心链路已实现并通过审查（含主 Agent 工具链解耦与第三方 Skill 自动 Fork 增强）
>
> 日期：2026-07-24

>
> 适用范围：Genesis Agent 的内置、安装式及第三方 Skill 运行时
>
> 已落地对象：`office-ppt`、`office-word`、`office-excel`、`office-pdf`

## 1. 背景与目标

旧实现中一个物理 Skill 只能暴露一套全局运行属性。以 `office-ppt` 为例：读取 PPT 只需要一次轻量文本提取，但创建、编辑、渲染和视觉检查需要子 Run、远程沙箱会话、多轮脚本执行和交付门禁。把整个 Skill 固定为 `context: fork` 会导致读取任务也承担完整工作链路的成本；拆成两个物理 Skill 又会复制 `SKILL.md`、脚本和依赖知识，形成长期漂移。

本方案引入 **一个物理 Skill、多个逻辑 Invocation、一个 Genesis 运行时侧车文件**：

- `SKILL.md` 保持可移植，承载模型需要理解的任务知识和通用工作流；
- `genesis.skill.yaml` 承载 Genesis 控制面的调用入口、认知模式、执行环境、能力门槛、工具权限、QA 和交付契约；
- 一个 Skill 可以向模型暴露多个稳定 handle，例如 `office-ppt-read` 与 `office-ppt`；
- 模型只选择 handle，不选择 `entrypoint`、沙箱或交付参数；
- 后端根据已解析的 Invocation Binding 决定 inline/fork、per-call/session、能力门禁和 DeliverableSpec。

目标是同时满足：

1. 读取类调用足够轻，不创建子 Run，不启动会话式工作区；
2. 制作类调用保持完整隔离、视觉 QA、交付和父 Run Adoption；
3. 第三方 `SKILL.md` 正文和脚本尽可能原样使用；
4. 运行策略由声明驱动，不依赖 Skill 名称启发式或自然语言猜测；

本设计采用一次性破坏式重构：不双读旧 frontmatter，不迁移旧运行数据，不保留 deprecated alias。无侧车的标准第三方 Skill 仍具有正式默认 Invocation；这是新架构的一等行为，不是兼容分支。

运行中已经解析的 Invocation 必须固定包摘要、输入版本和有效策略，以保证并发一致性、恢复和审计；这种版本固定不属于历史兼容。

5. 无视觉模型时诚实降级或 fail closed，绝不把渲染成功冒充视觉 QA 通过；
6. 设计可复用于 Word、Excel、PDF、图像、视频及其他重型 Skill。

## 2. 核心决策

### 2.1 独立 Skill，不使用 Plugin Manifest

`office-ppt` 保持为独立 Skill 包，不创建 `plugin.json`，也不使用以下 Plugin 外壳：

```json
{
  "name": "genesis-office",
  "capabilities": [
    { "type": "skill", "name": "office-ppt" }
  ]
}
```

Plugin 用于组合多个 Skill、Tool、MCP、资源和应用能力；单个 Skill 不应为获得多入口能力而被迫包装成 Plugin。

### 2.2 固定侧车文件名

每个需要 Genesis 扩展运行语义的 Skill，可以在自身目录增加：

```text
genesis.skill.yaml
```

这是 **Skill Runtime Manifest**，不是 Marketplace Package Manifest。没有该文件的 Skill 继续使用通用默认行为。

选择该文件名的原因：

- `genesis` 命名空间避免与第三方标准文件冲突；
- `*.skill.yaml` 与现有路径契约分析器的 Skill manifest 识别规则一致；
- 内置 Skill 使用 `//go:embed all:skills`，该文件能随 Skill 自动嵌入；
- 文件位于 Skill 根目录，安装、复制和版本化时不会与 `SKILL.md` 分离。

### 2.3 一个物理 Skill，多个模型可见 handle

`office-ppt` 首期提供两个 Invocation：

| Invocation | 模型可见 handle | Cognition | 执行生命周期 | 交付物 |
|---|---|---|---|---|
| `read` | `office-ppt-read` | `inline` | `per_call` | 无强制交付物 |
| `work` | `office-ppt` | `fork` | `sandboxed_session` | 必须交付 `.pptx` |

模型调用保持简单：

模型侧固定网关只保留三个业务参数：

```text
Skill(skill, task?, inputs?)
```

- `skill`：从可见 Catalog 中选择稳定 handle；
- `task`：任务目标，由 Invocation request contract 决定是否必填；
- `inputs`：显式资源引用，由 Invocation request contract 校验数量、类型和访问方式。

禁止从自然语言或工作区扫描猜测文件名；用户附件可以由产品层预绑定为可选输入，但进入 Invocation 前必须转成带版本/hash 的明确 ResourceRef。

```text
Skill(skill="office-ppt-read", inputs=["report.pptx"])
```

或：

```text
Skill(skill="office-ppt")
```

不设计 `Skill(skill="office-ppt", entrypoint="read")`。这样模型只做一次离散选择，参数 schema 不随 Skill 内容动态变化。

## 3. 文件布局与职责

```text
office-ppt/
├── SKILL.md
├── genesis.skill.yaml
├── references/
│   └── invocations/
│       ├── read.md
│       └── work.md
├── editing.md
├── pptxgenjs.md
├── LICENSE.txt
└── scripts/
    └── ...
```

### 3.1 `SKILL.md`

保留：

- `name`、`description`；
- `LICENSE.txt` 等随包法律文件；`SKILL.md` frontmatter 只解析标准 `name`、`description`；
- PPT 内容读取、创建、编辑、渲染的知识；
- 命令示例、设计原则、验证方法和资源导航；
- 对模型有用、且跨宿主环境成立的工作流说明。

不再承载 Genesis 控制面字段：

- `context`；
- `sandbox`；
- `allowed-tools`；
- 机器可执行的 `dependencies`；
- `requires`；
- `qa`；
- DeliverableSpec；
- agent/model 选择；预算只允许作为 `cognition` 的受限请求；
- 产品、租户和角色策略；
- 具体 runtime profile、镜像、endpoint 或 credential。

> 第三方 `SKILL.md` 正文中原本存在的人类可读依赖说明可以保留原文；真正参与安装白名单、RuntimeProfileResolver 和运行前检查的结构化依赖以侧车文件为准。

### 3.2 `genesis.skill.yaml`

承载：

- Invocation 定义与模型可见 handle；
- Invocation 级 description；
- `inline` / `fork` 认知模式与执行预算请求；
- Invocation 指令入口及是否注入完整 `SKILL.md`；
- `task` 与显式输入资源契约；
- 只能收紧权限的 `tool_policy`；
- 运行依赖与沙箱生命周期；
- 启动前能力门槛 `requires`；
- 复数 Deliverable 契约及各自 QA；
- 交付物角色、数量、类型、门禁和投递策略；
- 运行期不可变 Binding 所需的静态声明。

该文件只由控制面读取，不作为普通 Skill resource 注入模型上下文。模型看到的是后端从 Manifest 派生出的 catalog 条目和 Invocation 指令。

### 3.3 Invocation 指令文件

`references/invocations/*.md` 只描述该入口的边界和补充流程：

- `read.md`：只提取和总结文字、备注、表格；不渲染、不修改、不创建交付物；
- `work.md`：声明子 Run 对生成、修正、QA、候选选择和交付闭环负责。

Invocation 指令不复制完整 `SKILL.md`。`work` 通过 `skill_body: include` 复用完整知识；`read` 通过短指令完成轻量处理。Skill 指令永远处于平台系统契约和 Runtime Bridge 之下，第三方 Skill 不得成为子 Agent 的原始 SystemPrompt。

## 4. `office-ppt` 参考 Manifest

```yaml
schema: genesis.skill/v1
skill: office-ppt

runtime_profiles:
  read:
    sandbox:
      execution_mode: per_call
      backends:
        - remote_sandbox
        - local_platform_sandbox
    dependencies:
      runtime:
        python:
          - name: markitdown
            import: markitdown

  work:
    sandbox:
      execution_mode: sandboxed_session
      backends:
        - remote_sandbox
        - local_platform_sandbox
    dependencies:
      runtime:
        python:
          - name: markitdown
            import: markitdown
          - name: Pillow
            import: PIL
          - name: defusedxml
            import: defusedxml
        node:
          - name: pptxgenjs
            require: pptxgenjs
        system:
          - name: libreoffice
            command: soffice
          - name: poppler
            command: pdftoppm

invocations:
  - id: read
    handle: office-ppt-read
    description: 提取并总结已有 PPT 的文字、备注和表格，不渲染、不编辑
    agent_mode: main
    runtime_profile: read
    request:
      task:
        required: false
      inputs:
        min_items: 1
        max_items: 1
        access: read_only
        accepted_suffixes: [.pptx]
        accepted_mimes:
          - application/vnd.openxmlformats-officedocument.presentationml.presentation
    prompt:
      instructions: references/invocations/read.md
      skill_body: omit
    tool_policy:
      allow:
        - list_skill_resources
        - read_skill_resource
        - search_skill_resources
        - run_skill_command
        - install_skill_dependencies
      required:
        - run_skill_command
    result:
      kind: message

  - id: work
    handle: office-ppt
    description: 创建、编辑、渲染或视觉检查 PPT；不要用于只读提取或摘要
    agent_mode: fork
    runtime_profile: work
    request:
      task:
        required: true
      inputs:
        min_items: 0
        max_items: 16
        access: read_only
        accepted_suffixes: [.md, .txt, .pptx, .docx, .xlsx, .pdf]
    prompt:
      instructions: references/invocations/work.md
      skill_body: include
    tool_policy:
      allow:
        - list_skill_resources
        - read_skill_resource
        - search_skill_resources
        - run_skill_command
        - install_skill_dependencies
        - read_file
        - write_file
        - edit_file
        - view_image
        - select_deliverable_candidate
      required:
        - run_skill_command
        - write_file
        - select_deliverable_candidate
    result:
      kind: deliverables
      deliverables:
        - id: deck
          role: primary
          required: true
          cardinality: exactly_one
          accepted_suffixes: [.pptx]
          accepted_mimes:
            - application/vnd.openxmlformats-officedocument.presentationml.presentation
          delivery_policy: run-output
          qa:
            policy: visual-qa/v1
            enforcement: optional
```

`runtime_profiles` 是 Skill 包内的逻辑执行要求，不是产品部署镜像名。产品 RuntimeProfileResolver 根据依赖、环境、租户和后端能力选择实际环境。

`request.inputs` 只接受已解析为 ResourceRef 的显式输入。产品可以把当前 Turn 附件预绑定为候选，但 Runtime 不得从自然语言或目录扫描推断输入文件。

`tool_policy.allow` 是权限上限，不是授权来源。`work` 默认不包含 `Skill`，避免相同 Skill 在子 Run 中递归调用；跨 Skill 协作必须同时满足产品策略和调用深度策略。

`tool_policy.required` 声明该 Invocation 启动所必需的 Tool；它必须是 `allow` 的子集。`view_image` 对 optional 视觉 QA 不是启动必需项，因此只在 allow 中；`run_skill_command` 等核心执行原语缺失时必须 fail closed。

`cognition` 只声明 `inline|fork` 及 timeout、turn、token、tool-call 预算请求，并且只能被产品策略进一步收紧。目标模型、Router 和 Agent 由平台执行计划决定，不允许 Skill 包自行指定。它们都不进入模型的 `Skill` 参数；未声明预算时使用产品默认值。

`office-ppt/work` 不声明 `requires: vision`。没有视觉模型时，它仍然可以生成 PPT、做内容检查和渲染证明，只是不能声称已完成真正的视觉 QA。

## 5. `requires` 与 `qa` 的区别

`requires` 和 `qa` 解决的是两个不同阶段的问题，不能合并。

### 5.1 `requires`：启动前能力门禁

对于“理解图片本身就是完成任务的前提”的 Invocation：

```yaml
requires:
  - kind: vision
    enforcement: required
```

语义：

- 在创建子 Run、打开沙箱、安装依赖之前检查；
- Runtime 必须先解析目标执行计划，再检查实际执行者的 EffectiveVisionMode；
- inline 检查当前 Run 的有效视觉模式，fork 检查目标子执行计划的有效视觉模式；
- 至少一条真实视觉路径必须声明 `supports_image=true`；
- 无视觉能力时拒绝启动 Invocation；
- 返回稳定错误码 `SKILL_CAPABILITY_REQUIRED`；
- 不产生一个注定无法完成的子 Run，也不消耗重型执行资源。

典型场景：看图问答、图片审查、仅凭视觉内容提取信息、视觉设计评审。

### 5.2 `qa`：执行后交付质量策略

对于 PPT 这类“没有视觉也能生成，但视觉检查能提升质量”的 Invocation：

```yaml
qa:
  policy: visual-qa/v1
  enforcement: optional
```

语义：

- 有视觉能力时应执行视觉 QA 并记录结构化证据；
- 无视觉能力时允许继续生成和交付；
- QA 结果记录为 `skipped` 或 `degraded`，原因是 `vision_unavailable`；
- 必须在 Trace、审计和最终摘要中披露未执行视觉 QA；
- 文本内容检查、OOXML 校验和渲染成功可以继续执行，但不得记作 `visual-qa/v1=passed`。

省略 `enforcement` 与写 `optional` 完全等价。只有显式 `required` 才形成完成门禁：

```yaml
qa:
  policy: visual-qa/v1
  enforcement: required
```

此时 Invocation 可以启动，但 DeliverableSpec 没有获得有效的 `visual-qa/v1` passed 证据时，Run 不得进入 completed。

### 5.3 判定矩阵

| 声明 | 无视觉模型 | 有视觉模型但未通过 QA | 是否允许交付完成 |
|---|---|---|---|
| 无 `requires`、无 `qa` | 正常执行 | 不适用 | 允许 |
| `requires.vision=optional` | 正常执行并可记录降级 | 按工作流处理 | 允许 |
| `requires.vision=required` | 启动前拒绝 | 可以启动 | 取决于是否另有 QA 门禁 |
| `visual-qa/v1` + `enforcement=optional` | 执行，QA 记 skipped/degraded | 记录缺陷但不硬卡 Completion | 允许，但必须披露 |
| `visual-qa/v1` + `enforcement=required` | 可启动，但交付门禁阻塞 | 交付门禁阻塞 | 不允许 |
| `requires.vision=required` + required visual QA | 启动前拒绝 | QA 不通过则交付门禁阻塞 | 仅真实视觉 QA 通过后允许 |

### 5.4 视觉 QA 证据纪律

`visual-qa/v1=passed` 必须来自真实视觉能力和可校验的结构化断言：

- 主模型具备图像输入能力并输出符合协议的 checklist；或
- Runtime 的 VisionExpert 通过 `router.vision` 完成检查并返回结构化结论。

### 5.5 QA 与交付顺序

QA 必须绑定候选产物的不可变版本/hash，并在对用户可见的 Delivery 前完成决策。这里将 Publication 明确定义为内部不可变 Artifact 提交，不等于外部交付：

```text
Produced candidate
  → 类型、二进制和 OOXML Gate
  → Selection
  → 内部 Publication Commit，固定 subject version/hash
  → content/render/visual QA 形成精确版本证据
  → required QA passed，或 optional QA 明确 passed/failed/skipped/degraded
  → Delivery
  → Completion
```

禁止先把未通过 required QA 的产物交付给用户，再仅仅阻止 Run 进入 completed。内部 Publication Commit 只用于生成不可变 Artifact、`PublicationID`、subject version/hash 和 QA 锚点，不产生用户可见文件；`DeliveryService` 会独立重验 QA 证据，不能只依赖上游 Finalizer。

optional QA 也必须形成确定性记录：

- 有视觉能力且执行成功：`passed`；
- 有视觉能力但检查失败：`failed`，允许交付但必须披露缺陷；
- 无视觉能力：`skipped` 或 `degraded`，`failure_code=vision_unavailable`；
- Runtime 在外部 Delivery 前自动写入或由真实视觉检查桥接写入，不依赖模型用自然语言自觉记账；缺少确定性 outcome 时不得绕过 Delivery。

以下证据均不能冒充视觉 QA 通过：

- `markitdown` 文本提取成功；
- LibreOffice 转 PDF 成功；
- `pdftoppm` 或 `thumbnail.py` 生成图片成功；
- 文件存在、MIME 正确或 OOXML 校验通过；
- 模型未读取图像时仅用文字声称“视觉检查通过”。

这些证据应分别记录为 content QA、render proof 或 binary validation。

## 6. Sandbox 与 Runtime Profile 分层

### 6.1 侧车声明 Backend 优先级与生命周期

侧车通过 `sandbox` 节点声明 Invocation 的物理后端优先级与生命周期模式：

```yaml
sandbox:
  execution_mode: per_call              # per_call | sandboxed_session
  backends:
    - remote_sandbox
    - local_platform_sandbox
```

- `execution_mode`：
  - `per_call`：单次离散命令独立绑定执行环境，用完销毁，适用于无状态提取/转换任务（在远程容器、本地沙箱和宿主环境均天然支持）；
  - `sandboxed_session`：多轮命令复用同一受控工作区和 session。
- `backends`（有序优先级列表）：
  - 数组顺序即降级顺序（如 `[remote_sandbox, local_platform_sandbox]`）；
  - 数组包含 `local_host` 时表示允许降级至宿主直跑；不包含 `local_host` 时即表达强制沙箱（Fail-Closed）；
  - 兼容兜底：对于未声明 `backends` 的旧侧车文件或无侧车 Skill，若 `required: true` 自动展开为 `[remote_sandbox, local_platform_sandbox]`，若 `required: false` 或完全无侧车自动展开为 `[remote_sandbox, local_platform_sandbox, local_host]`。

### 6.2 动态选型与 Staging 严格对齐 (SelectedBackend & Staging Alignment)

在命令执行前，运行时根据 `backends` 优先级列表与当前环境的物理后端可用性（`RemoteSandboxAvailable` / `LocalSandboxAvailable`）求出实际调用的物理后端 `SelectedBackend`：

- 当 `SelectedBackend == "remote_sandbox"`：走向 `runRemote`，执行远程 Package Materialize，并通过 `stageInputManifestRemote` 将输入资源同步至远程容器工作区；
- 当 `SelectedBackend == "local_platform_sandbox"` 或 `"local_host"`：走向 `runLocal`，在宿主机/本地沙箱目录 Materialize，并通过 `stageInputManifestLocal` stage 文件，同时通过 Windows/Linux 本地沙箱 ACL 显式赋予该 Skill 工作目录的读写权限。

物理选中结果 `SelectedBackend` 完整记入 `EffectiveExecutionPolicy` 和审计 Trace，保证上层选型与下层物理 Staging/执行策略绝对对齐，杜绝环境与文件流转错位。

### 6.3 统一与单一事实源 (SSOT)

Invocation 后端选型完全基于 Skill 包内 `genesis.skill.yaml` 的 `backends` 数组声明，废除异地 `invocations_override` 配置以消除规则冲突。

有效策略求解顺序为：

```text
侧车最低约束 backends 列表
  + 全局/产品/租户安全上限
  + Invocation deployment policy
  + 当前 Run 动态物理可用性检测
  → SelectedBackend & EffectiveExecutionPolicy
```

若 `backends` 列表中所有支持的后端在当前环境中均不可用，系统返回 `SKILL_RUNTIME_PROFILE_UNAVAILABLE` 阻断，禁止静默旁路直跑。

### 6.3 RuntimeProfileResolver

`runtime_profile` 是 Skill 包内的逻辑 ID。它声明依赖和执行生命周期，不声明镜像名。RuntimeProfileResolver 根据逻辑 profile、结构化依赖、产品/租户/环境/架构和后端可用性选择实际运行环境。禁止根据 Skill 名称硬编码镜像。

### 6.4 Session 生命周期

`sandboxed_session` 必须定义清晰的资源所有权：

- 所有者是具体 InvocationBinding/child Run，不是物理 Skill 全局；
- 同一 session 中写命令默认串行，除非调度器证明并发安全；
- 取消、超时和终态必须关闭或释放 session；
- 暂时断线允许按固定 Binding 恢复或续租，禁止重新解析到新版本 Skill；
- lease 过期后，未持久化 ProducedResource 不得冒充可交付产物；
- 清理失败必须进入 Trace/Audit 和后台回收队列；
- 重试和恢复必须复用幂等键，避免重复发布或重复投递。

## 7. 后端解析与执行链路

### 7.1 发现、校验与索引

```text
扫描 Skill 目录
  → 解析标准 SKILL.md
  → 可选解析 genesis.skill.yaml
  → 验证包来源、签名/摘要、schema、资源引用和策略约束
  → 生成 PhysicalSkillDefinition
  → 展开 InvocationDefinition
  → 按稳定内部身份和 handle 建立 Invocation Catalog
```

稳定内部身份必须包含 `authority + package_id + invocation_id`；handle 只是模型可读别名。不同 authority 下同名 handle 必须报冲突或要求显式 qualified handle/resource locator，不能依赖来源遍历顺序任选。

对于 `office-ppt`，Catalog 对模型暴露：

```text
office-ppt-read → physical_skill=office-ppt, invocation=read
office-ppt      → physical_skill=office-ppt, invocation=work
```

存在 Manifest 时不再暴露无绑定的物理 Skill 条目。无 Manifest 的标准 Skill 生成一个正式默认 Invocation：`handle=SKILL.name`、`cognition=inline`、`skill_body=include`、无能力/交付声明，并使用平台安全默认 Runtime Profile。

发布者提供的侧车属于 Skill 包内容并参与包签名/摘要；管理员或产品覆盖属于包外策略 overlay。安装后不得为适配 Genesis 而直接修改已签名第三方包。

### 7.2 Resolve Invocation 与 Binding

```text
Skill(skill, task?, inputs?)
  → 按 handle 精确解析 InvocationDefinition
  → 检查产品/租户/用户/角色可见性
  → 校验 task 与显式 ResourceRef 输入契约
  → 固定 SkillPackageSnapshot 和输入版本/hash
  → 将完整原始 Skill 包写入 package-digest 内容寻址存储（CAS）
  → 解析 cognition、预算请求与 Runtime Profile
  → 求解 EffectiveToolPolicy 和 EffectiveExecutionPolicy
  → 按实际执行者计算 EffectiveVisionMode 等能力
  → 检查 requires.required
  → 检查依赖、安装许可和审批
  → 持久化 InvocationBinding 和声明式 DeliverableSpec
  → 进入 inline 或 fork
```

能力门禁必须先于依赖安装、子 Run 创建和沙箱启动。fork Invocation 检查目标子执行计划，而不是错误复用父模型能力。

InvocationBinding 是运行期唯一真相源，至少固定：

```text
binding_id / tenant / run / parent_run
source authority / package_id / package_version / package_digest
manifest schema / manifest_digest / invocation_instruction_digest
invocation_id / handle / cognition
cognition / bounded budgets / target execution capability snapshot
runtime_profile / effective sandbox policy
base tool set / effective tool policy
requires and effective capability snapshot
normalized task
input ResourceRef IDs / versions / hashes / aliases
DeliverableSpecs / QA policies / delivery policies
policy snapshot version / created_at
```

后续 `run_skill_command`、依赖安装、Tool Gateway、QA Recorder、Artifact Finalizer、子 Run Controller 和恢复流程都只读取 Binding，不重新解析当前磁盘上的 Skill，也不要求模型重复传递 profile、QA 或交付参数。Binding 创建前必须先把完整原始包写入按 `package_digest` 寻址的不可变快照存储；本地和远端 materialize 只读 CAS 并逐文件复验 size/hash，不能回读已升级的 Source。输入 staging 不得覆盖包内文件。

幂等键至少覆盖 `package_digest + invocation_id + normalized_task + input_versions/hashes + consumer_run`。重试不得重复创建子 Run、发布记录或投递记录。

### 7.3 指令权限层级

无论 inline 还是 fork，指令优先级固定为：

```text
平台安全与运行时契约
  > 产品/Agent system contract
  > Genesis runtime bridge
  > Skill body 与 Invocation instructions
  > task 与用户输入
```

Skill 内容作为带来源标记的低权限 instruction 注入。第三方 `SKILL.md` 或 Invocation markdown 不能成为临时子 Agent 的原始 SystemPrompt，不能覆盖沙箱、权限、路径、交付和审计规则。

Prompt 组合顺序必须确定且进入摘要：先 Skill body（若 include），再 Invocation instructions，最后 task/input 清单；总大小受独立预算限制，截断必须 fail closed 或按声明的可截断边界执行，不能随机截掉安全/交付指令。

### 7.4 Tool Policy 与 Invocation 生命周期

有效工具集合按交集求解：

```text
产品可见工具
  ∩ RBAC/PermissionEngine
  ∩ Agent 工具上限
  ∩ Run/环境策略
  ∩ Invocation tool_policy.allow
```

Manifest 只能收紧权限，不能自行授予 Tool。求交为空或缺少 Invocation 必需 Tool 时，启动失败；禁止告警后恢复为全量工具。

第一版生命周期采用简单确定的规则：

- inline Activation 与当前 Run 同生命周期，工具集合只能单调收紧，直到 Run 结束；
- fork 工具策略只作用于子 Run；
- 不声明无法判定结束时间的“临时 tool lease”；
- 同一 Run 需要多个权限差异很大的 Skill 时，使用 fork/上层编排，而不是恢复已移除的工具；
- 默认禁止同一 physical skill/invocation 在祖先链递归；跨 Skill 调用受最大深度、环检测和产品策略约束。

### 7.5 Inline read

```text
Resolve office-ppt-read
  → 校验恰好一个只读 PPT ResourceRef
  → cognition=inline
  → 注入 read.md，不注入完整 SKILL.md
  → Run-scoped 工具策略收紧
  → run_skill_command 使用 per_call 沙箱和固定输入快照
  → 返回 message result
  → 不创建 DeliverableSpec
```

### 7.6 Fork work

```text
Resolve office-ppt
  → 校验 task 和显式输入
  → cognition=fork
  → 先持久化 InvocationBinding 与 DeliverableSpecs
  → 创建隔离子 Run并绑定 sandboxed_session
  → 按固定权限层级注入 SKILL.md + work.md + task/input manifest
  → 多轮生成、渲染、QA、修正
  → Produced candidate / Gate / Selection / internal Publish / QA / Delivery
  → 子 Run 通过 Completion 门禁
  → 父 Run Adoption
  → 父 Run 总结结果、证据和降级状态
```

### 7.7 Adoption 边界

父 Run 不重新执行子 Run QA，也不扫描目录猜测产物。只有同时满足以下条件才能 Adoption：

- 子 Run 达到成功终态；
- 所有 required Deliverable 已通过类型、版本和 required QA 门禁；
- Publication committed 且 Delivery succeeded；
- 产物 hash、owner run、deliverable id、QA 状态和 lineage 可验证；
- 父子 tenant/scope/授权关系成立。

AdoptionRecord 必须版本锁定并携带足够 lineage。父 Run 若有自己的 DeliverableSpec，只能由类型、角色和策略匹配的已接纳子交付销账；不能仅凭“子目录存在同后缀文件”完成。AdoptionStore 必须按租户/工作空间实例化并经 Run Context 注入，禁止使用会被后续租户覆盖 state root 的进程级可变单例。

## 8. 建议领域模型

示意结构用于说明边界，不要求与最终 Go 字段逐字一致：

```go
type RuntimeManifest struct {
    Schema          string
    Skill           string
    RuntimeProfiles map[string]RuntimeProfile
    Invocations     []InvocationDefinition
}

type InvocationDefinition struct {
    ID             string
    Handle         string
    Description    string
    Cognition      CognitionRequest
    RuntimeProfile string
    Request        RequestContract
    Prompt         InvocationPrompt
    ToolPolicy     ToolPolicy
    Requires       []CapabilityRequirement
    Result         ResultContract
}

type RequestContract struct {
    Task   TaskContract
    Inputs InputContract
}

type InputContract struct {
    MinItems         int
    MaxItems         int
    Access           string
    AcceptedSuffixes []string
    AcceptedMIMEs    []string
}

type InvocationPrompt struct {
    Instructions string
    SkillBody    string // include | omit
}

type ToolPolicy struct {
    Allow    []string
    Required []string
}

type ResultContract struct {
    Kind         string // message | deliverables
    Deliverables []DeliverableDeclaration
}

type DeliverableDeclaration struct {
    ID               string
    Role             string
    Required         bool
    Cardinality      string
    DesiredName      string
    AcceptedSuffixes []string
    AcceptedMIMEs    []string
    DeliveryPolicy   string
    QA               QADeclaration
}

type RuntimeProfile struct {
    Sandbox      SandboxRequirement
    Dependencies Dependencies
}
```

领域对象必须区分声明、解析结果和运行事实：

- `PhysicalSkillDefinition`：一个不可变安装包及资源边界；
- `SkillPackageSnapshot`：authority、版本、包摘要和可读取资源快照；
- `InvocationDefinition`：发布者提供的静态调用声明；
- `InvocationBinding`：某次 Run 已解析、求交、授权并持久化的不可变有效配置；
- `EffectiveToolPolicy` / `EffectiveExecutionPolicy` / `EffectiveCapabilitySnapshot`：平台求解结果；
- `RuntimeProfile`：包内逻辑依赖和隔离生命周期；
- `DeliverableSpec`：运行期持久化交付契约；
- `QAEvidenceRecord` / `Publication` / `Delivery` / `AdoptionRecord`：不可混为一个状态的交付事实。

不要继续把发现元数据、模型提示、权限、依赖、执行模式、QA 和交付全部堆入现有单一 `skill.Metadata`。Manifest 声明对象也不能直接当作 Effective Policy 使用。

## 9. Manifest 与包校验规则

安装、Catalog 构建和 CI 必须复用同一个 strict validator：

1. `schema` 必须是受支持的精确版本，未知顶层字段和 YAML duplicate key 直接失败；
2. `skill` 必须等于 `SKILL.md.name` 和目录名；
3. Manifest、单个指令文件、Invocation 数量、描述长度、依赖数和 Tool 数必须有硬上限；
4. Invocation `id` 在物理 Skill 内唯一，内部身份由 authority/package/id 构成；
5. handle 在有效 Catalog 作用域内唯一；冲突不能按来源顺序静默覆盖；
6. handle、id、profile ID 只允许小写字母、数字和连字符；
7. `cognition.mode` 只允许 `inline`、`fork`，预算值必须非负且受产品上限约束；
8. `runtime_profile` 必须存在，依赖必须通过包名、命令名、版本/来源和安装策略校验；
9. `request.task.required`、输入 min/max、access、后缀和 MIME 必须自洽；
10. 输入只接受 ResourceRef，不接受绝对路径、`..`、目录扫描结果或未经快照的可变文件；
11. `prompt.instructions` 必须位于当前包内，禁止绝对路径、`..`、符号链接越界和跨包引用；
12. `prompt.skill_body` 只允许 `include`、`omit`；
13. `tool_policy.allow` 中每项必须是已注册 Tool 名，但注册存在不代表授权；启动时仍须与平台权限求交；
14. 求交后缺少必需 Tool 或工具集合为空时 fail closed；
15. `requires.kind` 必须来自 Capability Registry；enforcement 只允许 `optional`、`required`；
16. QA policy 必须来自 QA Policy Registry，enforcement 只允许 `optional`、`required`；
17. `result.kind=message` 不得声明 Deliverable；`result.kind=deliverables` 必须至少声明一个 Deliverable；
18. Deliverable ID 唯一，role、cardinality、delivery policy 必须合法；primary 必须 required；
19. required Deliverable 必须声明可验证的后缀/MIME/desired name 之一；后缀小写且以 `.` 开头，MIME 合法；
20. required QA 必须绑定具体 required Deliverable；无交付 message Invocation 不得声明交付 QA；
21. package digest 必须覆盖 SKILL.md、Manifest、Invocation instructions、scripts 和执行期可见 assets/references；
22. 发布签名、安装来源和管理员 overlay 必须分别验证，overlay 不得修改包摘要内文件；
23. 默认禁止 Invocation 祖先链递归，跨 Skill 调用受深度和环检测约束。

非法枚举、未知安全字段和拼写错误必须拒绝，不能把 `requred` 等错误静默解释为 optional。对安全无关且明确允许前向扩展的 metadata，必须放入单独命名空间，不能放宽核心 schema。

## 10. 错误语义与可观测性

建议稳定错误码：

| 错误码 | 场景 |
|---|---|
| `SKILL_PACKAGE_UNTRUSTED` | 包摘要、签名或来源不满足策略 |
| `SKILL_REQUEST_INVALID` | task 或输入 ResourceRef 不满足 Invocation request contract |
| `SKILL_TOOL_POLICY_UNSATISFIABLE` | 工具策略求交为空或缺少必需 Tool |
| `SKILL_RECURSION_DENIED` | Invocation 祖先链递归、环或深度超限 |
| `SKILL_BINDING_VERSION_CONFLICT` | 恢复/重试时包、输入或策略版本与固定 Binding 冲突 |
| `SKILL_MANIFEST_INVALID` | Manifest schema 或引用非法 |
| `SKILL_INVOCATION_NOT_FOUND` | handle 不存在 |
| `SKILL_INVOCATION_CONFLICT` | handle 冲突 |
| `SKILL_CAPABILITY_REQUIRED` | required 能力不可用 |
| `SKILL_RUNTIME_PROFILE_UNAVAILABLE` | 无法满足运行依赖或沙箱要求 |
| `SKILL_QA_REQUIRED` | required QA 未获得有效 passed 证据 |
| `SKILL_DELIVERABLE_REQUIRED` | 必需交付物未选择、发布或交付 |

Trace/Audit 至少记录：

- Tool Policy 求交明细及拒绝原因；
- session 创建、续租、恢复、关闭和后台回收结果；
- physical skill、invocation id、handle；
- source authority、package version/digest、manifest schema/digest、instruction digest；
- binding id、policy snapshot、task digest、输入 ResourceRef/version/hash；
- 幂等键、重试/恢复次数和版本冲突；
- cognition 和最终 execution mode；
- RuntimeProfileResolver 决策与沙箱后端；
- capability gate 输入和结果；
- QA policy、enforcement、状态和 skip/degraded 原因；
- DeliverableSpec、候选、发布、交付和 Adoption ID；
- 所有降级 warning。

## 11. 实现状态与代码落点

### 11.1 核心链路

| 能力 | 状态 | 主要实现 |
|---|---|---|
| 严格 `SKILL.md` 与 Runtime Manifest | `[x]` | `skill/parser` 仅接受标准 frontmatter；`manifest.go` 严格校验 schema、重复键、枚举、资源边界与限制 |
| 多 Invocation Catalog / Resolve | `[x]` | `skill/service` 展开 handle，使用 `authority + package_id + invocation_id` 稳定身份并拒绝冲突 |
| 固定 `Skill(skill, task?, inputs?)` | `[x]` | 网关 schema 已删除 `args/entrypoint/model` 等控制面参数，RequestContract 在执行前校验 |
| 不可变 Binding 与 package CAS | `[x]` | Binding 与 policy snapshot 持久化；完整原始包按 digest 存储，本地/远端执行只从 CAS materialize |
| inline / fork | `[x]` | inline 在父 Run 注入低权限指令并单调收紧工具；fork 原子 claim 后创建隔离子 Run |
| Tool / Capability / Sandbox 求解 | `[x]` | 权限取交集且缺 required tool fail closed；capability gate 早于安装、审批、Spawn；后端降级写入 Binding warning |
| per-call / sandboxed-session | `[x]` | `run_skill_command` 只读 Binding；远端 session 支持持久 workspace、恢复、续租、关闭与回收 |
| 显式输入快照 | `[x]` | ResourceRef、版本/hash、只读 staging、alias 冲突与 Skill 包文件覆盖检查均 fail closed |
| Deliverable / Artifact / QA / Delivery | `[x]` | 声明式复数 Spec、确定性选择、内部 Publication、精确版本 QA、Delivery 独立重验与 Completion 门禁 |
| optional / required visual QA | `[x]` | 无视觉自动 `degraded(vision_unavailable)`；真实 checklist 可写 passed/failed；required 只接受 exact passed |
| 子 Run Adoption | `[x]` | 子 Completion 通过后才接纳；Adoption 版本锁定、持久化、按租户实例化并通过 Context 注入 |
| 重启与并发幂等 | `[x]` | Binding、package CAS、Artifact ledger、SubAgent invocation claim 均持久化；CLI/Enterprise 使用 durable store |
| Office 技能迁移 | `[x]` | PPT、Word、Excel、PDF 均已拆为 `read`/`work` Invocation，并各自提供侧车和指令文件 |

### 11.2 已完成的破坏式清理

1. `[x]` 删除 `SKILL.md` 中 `context/allowed-tools/dependencies/requires/qa/sandbox` 等 Genesis 扩展解析，只保留 `name/description`。
2. `[x]` 删除旧 Skill sandbox spec、旧 execution policy pipeline 和保留原工具集的回退行为。
3. `[x]` 删除 `Skill.args`、动态 `entrypoint`、模型选择和控制面参数；模型只提交 handle、task、inputs。
4. `[x]` fork 不再把 Skill body 当作原始 SystemPrompt，改为带来源和优先级的 Skill instruction。
5. `[x]` Invocation Resolver 不从自然语言、目录扫描或后缀猜输入；产品入口只可预绑定明确 ResourceRef。
6. `[x]` required capability 检查已前置到依赖安装、审批、Spawn 和沙箱启动之前。
7. `[x]` required QA 已成为外部 Delivery 的独立门禁；optional outcome 必须明确记为 passed/failed/skipped/degraded。
8. `[x]` 有 Invocation 声明时在子 Run 启动前建立 DeliverableSpec，不由 Prompt 猜测。
9. `[x]` 下游执行只读 InvocationBinding 与 CAS，不在重试/恢复时重新解析当前 Source。
10. `[x]` 不存在双读、deprecated alias、旧数据迁移或静默 sandbox fallback。

### 11.3 验证范围与尚未扩张的产品面

- `[x]` `go vet ./...`、`go test ./...`、`go build ./...` 和 `git diff --check` 通过。
- `[x]` embedded/local/memory Source、CLI 与 Enterprise 装配、Office Manifest、脚本物化、远端 session、QA/Delivery、Adoption 和重启幂等均有自动化测试。
- `[x]` 旧 Office `SKILL.md` 控制面字段残留扫描无匹配。
- `[ ]` 独立的管理型 API/UI 页面和 Marketplace Invocation 展示不是本次 Runtime 切换的阻塞项；后续只消费现有 Catalog/Binding，不应新增第二套协议。
- `[ ]` `go test -race` 在当前 Windows 环境因 CGO 未启用无法执行；并发路径已用原子 claim、revision CAS、锁和并发单测覆盖，CI 应在启用 CGO 的构建器补跑 race detector。

## 12. 破坏式实施记录

开发可以分阶段提交代码，但运行时切换必须是单路径，禁止双读、双写和旧数据迁移。

### Phase 1 `[x]`：纯新领域模型与校验器

1. 定义 PhysicalSkill、RuntimeManifest、InvocationDefinition、PackageSnapshot、InvocationBinding；
2. 实现 strict YAML parser、包摘要器、来源身份校验和 schema validator；
3. 实现 Manifest fixtures、duplicate key、未知字段、越界和限制测试；
4. 完成领域模型单测后，以单路径接入生产 Resolver；未保留旧解析路径。

### Phase 2 `[x]`：新 Source、Catalog 与 Resolver

1. embedded/local/memory Source 统一返回标准 Skill + 可选 Manifest + PackageSnapshot；Marketplace 安装后的包进入同一 local Source 路径；
2. 构建 Invocation Catalog、qualified identity、handle 冲突检测；
3. 实现 `Skill(skill, task, inputs)`、RequestContract 和 ResourceRef binding；
4. 实现无侧车标准默认 Invocation；
5. 持久化完整 Binding/policy snapshot。

### Phase 3 `[x]`：执行安全边界

1. 实现 Prompt Composer 和指令权限分层；
2. 实现 Tool Policy 求交、fail-closed、inline Run-scoped Activation、递归/环检测；
3. 实现 RuntimeProfileResolver、Invocation deployment policy 和 session 生命周期；
4. capability gate 基于目标执行计划并前置于依赖审批、Spawn 和沙箱；
5. 命令、依赖、资源工具全部改为只读 Binding。

### Phase 4 `[x]`：交付状态机

1. Invocation 复数 Deliverable 在执行前建立 DeliverableSpecs；
2. 禁用 Invocation Run 的 Prompt/ProducedResource 启发式建约；
3. 调整为 Gate/Selection/内部 Publication/QA/外部 Delivery 顺序；
4. optional QA 自动记账，required QA 在 Delivery 前阻塞；
5. Adoption 校验成功终态、版本、QA、Delivery 与 lineage；
6. 重试、恢复和投递使用 Binding 幂等键。

### Phase 5 `[x]`：一次性切换 Office Skills

同一个变更中完成：

1. 增加最终版 `genesis.skill.yaml` 和 Invocation instructions；
2. `SKILL.md` frontmatter 收敛为标准字段；
3. 物理后端策略完全收敛至 `genesis.skill.yaml`；
4. Runtime Catalog、模型 prompt、CLI 与 Enterprise 装配切换为 Invocation；
5. 删除旧 parser 字段、`Metadata` 混合字段、输入猜测、Skill 级 sandbox override、旧启发式和旧测试；
6. PPT、Word、Excel、PDF 的 read/work Manifest、编译、单元测试和装配测试已通过。

不允许“先双读观察一段时间”。若新链路不满足验收，整个变更不合并，而不是保留旧分支作为兜底。

### Phase 6 `[x]`：推广到 Word、Excel、PDF

已按相同模型迁移 Word、Excel、PDF。只有确有差异化 Invocation 的 Skill 才创建侧车；简单第三方 Skill 使用正式默认 Invocation，不生成空 Manifest。

### Phase 7 `[x]`：主 Agent 工具链解耦与第三方技能自动 Fork 增强

1. **主 Agent 工具链彻底解耦**：`tool_policy.allow` 被明确界定为 **Fork 子 Agent 专属工具策略 (Subagent Tool Policy)**。主 Agent (`context=main` / `AudienceRoot`) 调用 `Skill(...)` 时仅负责派生或注入，主 Agent 自身的全局工具链（`write_file`, `Task`, `Skill` 等）保持完整可用，不再被 `InvocationAllowsTool` 锁定误伤。
2. **第三方 Skill 无缝感知与自动 Fork**：对于无 `genesis.skill.yaml` 的标准第三方 Skill（参照 Kode / Anthropic Agent Skills 规范），运行时自动扫描包内文件快照：
   - 若包含 `scripts/` 目录或 `.py`/`.js`/`.sh`/`.ps1` 脚本，自动推导 `agent_mode: {mode: fork}` 并赋予默认安全的子 Agent 工具白名单 (`[run_skill_command, install_skill_dependencies, list_skill_resources, read_skill_resource]`)；
   - 若仅包含 Markdown 文档与静态资源，自动推导 `agent_mode: {mode: main}` (Inline Prompt 展开)。


## 13. 测试与验收标准

### 13.1 Parser、包与 Catalog

- 有效 Manifest 展开两个 handle；无 Manifest 生成一个正式默认 Invocation；
- duplicate key、未知核心字段、非法枚举、超限、越界引用和摘要错误均 fail closed；
- skill 名、profile、资源路径、handle 冲突稳定失败；
- embedded、本地和 memory Source 产生相同领域对象；Marketplace 安装结果通过 local Source 进入同一模型；
- Manifest 不被注入模型正文，Skill 指令不进入原始 SystemPrompt；
- 安装后修改包内任一执行资源都会改变 package digest 并使旧 Binding 拒绝重解析。

### 13.2 模型选择评测

建立独立 eval prompts，至少覆盖：

- “读取/总结/提取 PPT 备注或表格”选择 `office-ppt-read`；
- “创建/编辑/渲染/检查 PPT”选择 `office-ppt`；
- “读 PPT 后写邮件”仍选择 read；
- “根据现有 PPT 改版”选择 work；
- 不含 PPT 的任务不触发二者；
- 同时包含读取和修改时选择 work 或由上层明确拆分；
- descriptions 的负向边界能稳定降低误选，不依赖 entrypoint 参数。

### 13.3 Request、Binding 与幂等

- read 缺输入、多输入或非 `.pptx` 输入时，在执行前返回 `SKILL_REQUEST_INVALID`；
- work 缺 task 时失败，零输入创建和显式多输入均可；
- Runtime 不扫描目录、不从 task 文本猜文件；
- ResourceRef 的版本、大小、hash 和 alias 在 staging 前后校验；
- Binding 固定包、指令、策略和输入摘要；Skill 升级后恢复旧 Run 仍使用旧快照；
- 相同幂等键不会重复 Spawn、Publish 或 Delivery，不同输入 hash 不会错误去重。

### 13.4 Read Invocation

- `Skill(skill="office-ppt-read", inputs=[...])` 不创建子 Run；
- 不打开 `sandboxed_session`，不加载 LibreOffice、Poppler、Pillow、pptxgenjs；
- Run-scoped Tool Policy 不包含写文件和候选选择；
- 不创建 DeliverableSpec，返回 message result；
- 使用真实 fixture 验证普通文本、speaker notes、表格、隐藏页和图表可提取文字；不支持的内容必须明确披露，不能假设 markitdown 全覆盖。

### 13.5 Work Invocation

- `Skill(skill="office-ppt", task=..., inputs=[...])` 创建隔离子 Run；
- DeliverableSpecs 在模型执行前持久化，不使用 Prompt/ProducedResource 启发式；
- 多次命令复用同一 session，命令默认串行；
- 取消、超时、lease 过期和恢复都遵守固定 Binding 并完成资源清理；
- 未通过 Gate/required QA/Selection/内部 Publication/外部 Delivery 任一环节时不能成功交付；
- 子 Run 成功后父 Run 只 Adoption 和总结，不重复 QA、不扫目录。

### 13.6 Vision 与 QA

- fork 的 required vision 按目标子模型/router 判断，不能用父模型能力冒充；
- required capability 在依赖审批、Spawn 和沙箱前失败；
- optional visual QA 无视觉时自动记录 skipped/degraded + `vision_unavailable`，仍可交付并披露；
- 渲染、文本或 OOXML 成功不能写入 `visual-qa/v1=passed`；
- required visual QA 未通过时外部 Delivery 不发生；内部 Publication 只作为不可变 QA subject 存在；
- 只有结构化 checklist 或 VisionExpert 结论能产生匹配 exact subject version/hash 的 passed 证据。

### 13.7 权限、递归与降级

- Manifest 请求未授权 Tool 不能扩大权限；
- Tool Policy 求交为空或缺必需 Tool 时失败，绝不恢复原工具集；
- inline Activation 到 Run 结束前只能单调收紧工具；
- 相同 Invocation 祖先链递归、跨 Skill 环和超深调用被拒绝；
- `sandbox.required=true` 不能被普通请求削弱；remote 不可用且禁止降级时失败；
- 任何允许的降级都产生 warning/trace/audit；
- 所有业务路径使用 workspace-relative/sandbox path，不暴露宿主绝对路径。

### 13.8 破坏式清理验收

- 仓库中不再存在旧 Genesis Skill frontmatter 解析字段；
- 不再存在 `args`、Skill 级 `context/sandbox/allowed-tools`、输入文件猜测和旧配置键；
- 不存在双读、deprecated alias、旧数据迁移或运行时 fallback；
- 新老相关测试不可并存，删除测试必须由覆盖新契约的测试替代；
- Go 全量测试、静态检查、三端装配测试和 office-ppt 真实端到端测试通过。

## 14. 最终结论

最终结构是：

```text
一个物理 office-ppt Skill
  + 一个 genesis.skill.yaml
  + 两个逻辑 Invocation
  + 显式 Request/Input Contract
  + 两个差异化 Runtime Profile
  + 只能收紧权限的 Tool Policy
  + 复数 Deliverable/QA/Delivery Contract
  + 不可变 InvocationBinding 与 PackageSnapshot
```

其中：

- `request` 决定 task 和哪些显式输入可以进入 Invocation；
- `requires` 决定实际执行者“能不能启动”；
- `cognition` 决定在父 Run 内执行还是 fork 子 Run；
- `runtime_profile` 决定依赖、沙箱和工作区生命周期要求；
- `tool_policy` 只收紧平台已授权工具；
- `result.deliverables` 决定交付角色、数量、类型、QA 和投递策略；
- `InvocationBinding` 固定包、输入和有效策略，支撑恢复、幂等和审计；
- 产品配置决定满足约束的具体后端、镜像和部署凭据。

模型始终只调用 `Skill(skill, task?, inputs?)`，不选择 entrypoint、沙箱、模型、QA 或交付参数。Runtime 负责把声明、平台策略和实际能力求解为不可变 Binding。

该方案不拆分物理 Skill、不引入 Plugin、不保留旧 Genesis Skill 扩展路径，同时解决轻读取、重制作、权限收敛、视觉诚实降级和可靠交付，形成可推广到 Word、Excel、PDF、图像与视频 Skill 的统一执行模型。
