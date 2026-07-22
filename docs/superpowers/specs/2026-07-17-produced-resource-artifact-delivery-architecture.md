# ProducedResource 到 Artifact Delivery 的统一架构设计

> 状态：主链路已实施；跨仓 durable 提升与流式 OpenObject 延期（见 §22）  
> 日期：2026-07-17  
> 适用范围：Host、本地平台沙箱、远程 sandbox API；CLI、Desktop、Enterprise  
> 评审重点：资源身份、路径语义、持久化、发布与交付、完成门禁、三类执行后端的统一与独立

## 1. 背景

### 1.1 改造前的问题

改造前，执行结果到正式交付之间同时存在两套链路，且资源身份不统一：

1. 半手工链路：`run_skill_command.produced[]`（含可复制路径提示）→ 模型调用 `publish_artifact` → Artifact Gate → ArtifactStore → Delivery。
2. 旁路链路：远程 sandbox `OutputArtifacts -> DownloadArtifact -> LocalArtifactRoot`，由 sandbox client 自动写入宿主目录，绕过 Gate/Delivery。

远程 session 文件读取一度依赖进程内 `ProducedSourceOpener` / `RunSourceRegistry.external`：单次进程可用，但不能支持重启、多实例、lease 过期后的结构化失败、持久化完成门禁和统一资源治理。模型还承担了路径选择与发布参数拼装，容易产生字段错误和无效迭代。

### 1.2 当前主链路（已实施）

本设计统一资源身份与控制面后，主链路为：

```text
DeliverableSpec
  -> OutputReservation / 受限差异检测
  -> ProducedResourceRegistrar（稳定 candidate_id）
  -> 唯一匹配：Harness 经 Artifact Gateway 自动 Stage→Gate→Commit→Delivery
  -> 多匹配：模型仅调用 select_deliverable_candidate(deliverable_id, candidate_id)
  -> CompletionPolicy（仅看持久化 Publication/Delivery/QA 事实）
```

跨仓 durable 提升与流式 OpenObject 仍见 §10.1 / §22；其余旧兼容路径已删除，不得再按 §1.1 的旧链路开发。

## 2. 修改原因

### 2.1 路径不是资源身份

同一个业务资源在不同环境中可能表现为：

- Windows 宿主路径；
- Linux/macOS 宿主路径；
- 本地平台沙箱中的受控宿主映射；
- 远程 session 的 `/workspace/...`；
- 远程 executor object ID。

如果上层协议传播物理路径，业务逻辑就必须理解每种执行环境的路径规则，模型也容易把一个环境中的路径复制到另一个环境。长期正确的抽象应是稳定资源身份，而不是路径字符串。

### 2.2 持久化行为对象无法恢复

`ProducedSourceOpener` 封装的是带运行时状态的读取行为，远程实现还捕获了当前进程中的 session 对象。行为对象不能可靠持久化，也不能在另一个实例上恢复。

应持久化纯数据 `ProducedResourceDescriptor`，由运行时根据其中的不透明 `ResourceRef` 重新选择 Reader、注入 credential 并打开资源。

### 2.3 Artifact 与 executor output 语义混用

executor 输出对象只是执行后端资源，尚未经过格式、安全、业务和 QA 校验；Genesis Artifact 是已经通过 Gate 并提交到 ArtifactStore 的正式对象。两者不能共享 `Artifact` 名称和完成语义。

### 2.4 完成状态不能依赖内存提示

当前完成门禁会读取 `SkillFollowState` 中的 produced 和 delivered basename。进程重启后这些事实会丢失，不同目录下的同名文件也可能被误关联。

Run 完成必须由持久化的 Deliverable、Publication、Delivery 和 QA 事实决定。

### 2.5 能由 Harness 确定的动作不应交给模型

当任务只要求一个 PPT，且 Harness 只发现一个满足声明约束的 PPT 候选时，再要求模型复制路径并调用发布工具没有业务价值，只增加失败机会。

模型只应参与真正需要语义判断的候选选择；确定性路径解析、版本校验、唯一候选选择、幂等发布和重试均由 Harness 控制。

## 3. 要解决的问题

本设计解决以下问题：

1. Host、本地平台沙箱、远程 sandbox API 的路径语义不一致。
2. 模型猜测、复制或混用宿主路径与 `/workspace` 路径。
3. `run:/` 到远程 session 文件依赖进程内 closure，重启后失效。
4. sandbox client 自动下载产物并写宿主目录，绕过 Artifact Gate 和 Delivery。
5. execution output 与正式 Artifact 使用相同概念，职责不清。
6. Artifact Publisher 的简化 `SourceReader` 丢失版本、大小和 MIME 信息。
7. basename 交付关联可能把不同 ProducedResource 误判为同一个文件。
8. 任意 QA 截图、中间文件或日志都可能触发错误的完成门禁。
9. 发布成功、交付失败后缺少持久化状态，不能只重试 Delivery。
10. 并发或重复调用可能重复创建 Artifact 或重复交付。
11. 远程 session locator 被持久化后仍可能因 lease 过期而不可读。
12. 旧字段、旧路径和旧自动回收文档继续误导模型或后续开发者。

## 4. 第一性原理与不变量

系统真正需要完成的是：

> 将某次受控执行产生的、身份和版本确定的资源，在重新鉴权后读取、验证、提交并交付，同时允许进程重启、后端切换、重复请求和失败恢复。

必须始终成立以下不变量：

1. 模型不能决定物理路径、backend locator、version token、binding 或权限范围。
2. 每个 ProducedResource 必须有稳定 ID，禁止使用 basename 作为身份。
3. `ExecutionBinding` 在 Run 内不可变；资源必须归属于明确 binding。
4. Host、LocalSandbox、Remote 只在 locator 创建、Reader 和生命周期方面不同。
5. 正式 Artifact 只能来自版本锁定且重新鉴权成功的 ProducedResource。
6. Gate 必须检查不可变隔离对象，不能直接信任可变工作区文件。
7. Publication 和 Delivery 必须持久化、幂等并支持并发竞争。
8. 凭证、session client、Reader、closure 不得进入持久化 descriptor。
9. Run 完成只依据持久化控制面事实，不依赖模型回答和内存提示。
10. 远程模式不得向 Tool、模型、UI 或业务协议暴露宿主绝对路径。

## 5. 最终总体架构

```text
TaskIntent / App Template / API
  -> 持久化 DeliverableSpec
  -> ExecutionBinding + ExecutionBackendRef

Execution Harness
  -> 执行命令
  -> OutputReservation / 可信文件差异检测
  -> ProducedResourceRegistrar
  -> 持久化 ProducedResourceDescriptor
  -> 返回 candidate_id
     run:/ 仅作为逻辑别名

ArtifactPublicationPolicy（实现：DeterministicFinalizer）
  |- 唯一匹配：Harness 自动选择
  |- 多个匹配：模型只能选择 candidate_id
  `- 无匹配：结构化失败

ArtifactPublicationService
  -> 根据 candidate_id 加载 Descriptor
  -> 重新加载 Binding / Backend / Scope / Policy
  -> ResourceReaderRouter.Open(SourceRef, VersionToken)
  -> ArtifactStore.Stage（quarantine）
  -> ArtifactGatePipeline 校验 staged object
  -> ArtifactStore.Commit
  -> 持久化 ArtifactPublicationRecord
  -> DeliveryService
  -> 持久化 DeliveryRecord

CompletionPolicy
  -> required DeliverableSpec 全部满足
  -> PublicationRecord 成功
  -> DeliveryRecord 成功
  -> 必要 QA Evidence 成功
  -> Run Completed
```

### 5.1 为什么必须先 Stage 再 Gate

原始概念链路容易被理解为：

```text
ResourceReader -> Artifact Gate -> ArtifactStore
```

最终链路必须明确为：

```text
ResourceReader
  -> ArtifactStore.Stage（不可见 quarantine）
  -> ArtifactGatePipeline 读取不可变 staged object
  -> ArtifactStore.Commit
```

原因：

- 避免工作区文件在读取与 Gate 之间发生变化；
- 避免大文件为 hash、格式检查和提交重复跨网络读取；
- Gate 失败时可以统一 Abort；
- 只有 Commit 后的对象才是正式 Artifact；
- QA 和安全扫描可以复用同一份不可变输入。

## 6. 核心模型

### 6.1 ExecutionBackendRef

Backend 信息属于不可变 execution snapshot。不能同时在 descriptor 中保存另一个可独立修改的 `BackendKind`，否则会形成双重真相。

```go
type ExecutionBackendRef struct {
    Kind       BackendKind `json:"kind"`
    Provider   string      `json:"provider,omitempty"`
    InstanceID string      `json:"instance_id,omitempty"`
    Authority  string      `json:"authority"`
}

type PreparedExecutionSnapshot struct {
    Binding   ExecutionBinding   `json:"binding"`
    Backend   ExecutionBackendRef `json:"backend"`
    Workspace ExecutionWorkspace `json:"workspace"`
}
```

`ProducedResourceDescriptor` 只保存 `BindingID`。读取时从 RunManifest 恢复 BackendRef，并校验 `Source.Authority` 与 BackendRef 一致。

### 6.2 ProducedResourceDescriptor

ProducedResource 属于 `workspace` 能力域，因为它描述的是执行产生的稳定工作空间资源，不要求一定发布成 Artifact。

```go
type ResourceAvailability string

const (
    ResourceAvailabilityLeased  ResourceAvailability = "leased"
    ResourceAvailabilityDurable ResourceAvailability = "durable"
)

type ProducedResourceDescriptor struct {
    ID        string `json:"id"`
    TenantID  string `json:"tenant_id"`
    RunID     string `json:"run_id"`
    BindingID string `json:"binding_id"`

    // 规范化逻辑别名，用于审计、UI 和高级资源工具。
    // 普通模型调用不依赖它定位物理资源。
    LogicalRef string `json:"logical_ref"`

    // 由可信 backend adapter 创建；ID 和 Version 对业务层不透明。
    Source ResourceRef `json:"source"`

    ObservedName string `json:"observed_name"`
    MediaType    string `json:"media_type,omitempty"`
    Size         int64  `json:"size"`

    Availability ResourceAvailability `json:"availability"`
    ExpiresAt    *time.Time           `json:"expires_at,omitempty"`
    CreatedAt    time.Time            `json:"created_at"`
}
```

约束：

- Descriptor 按 ID 排他创建，创建后不可变；
- `(tenant_id, run_id, logical_ref)` 指向**当前 head**：同槽位内容指纹相同则登记幂等返回原 descriptor；内容变更（`Source.Version`/Size 等）则 `UpsertCurrent` 推进 head，旧 descriptor 仍可按 ID 读取但不参与候选枚举；
- `ListByRun` 只返回各 logical_ref 的当前 head；
- `Source.Authority/Scheme/ID/Version` 必须完整；
- `Source.ID` 是不透明 locator ID（不参与内容幂等比较）；
- 同槽位重登记时 Registrar 传入 `PreferSource`：内容指纹仍匹配则 **复用** 既有 locator（可刷新 leased `ExpiresAt`），禁止 Create 孤儿 locator；内容变更才新建 locator，旧 locator 由旧 descriptor 引用保留；
- 不持久化 credential、URL token、Reader、session client 或 closure；
- 不向普通模型输出 `Source`；
- 不把 publication/delivery 状态写入 Descriptor。

### 6.3 DeliverableSpec

任务要交付什么必须由 TaskIntent、App Template、Workflow 或显式 API 参数声明。不能从任意 `produced[]` 反推出业务要求。

**职责切分（Harness 优先 + 命名对齐 Kode/Codex）：**

- 沙箱 / remote skill cwd 是**执行隔离**，不是「用户可见交付」。用户可见落盘的唯一稳定路径是 **Publish → Delivery（FinalizeRequired）**。
- **禁止**从用户自然语言正则抠 `DesiredName`（会把「复制一份/重命名为」粘进文件名；也不适合写代码场景）。
- **命名声明方式**（与 Kode `Write.file_path` / Codex `apply_patch` path 同构）：
  1. API / App Template 显式 `DeclaredDeliverable.DesiredName`（产品硬契约）；
  2. 否则 Spec.`DesiredName` 留空，Publish 时使用所选 produced 的 **`ObservedName`**——即模型在 skill 命令/脚本中写出的文件名。
- Agent 负责产出内容正确、后缀/MIME 匹配契约的候选，并用产物文件名声明用户可见名；**不**承担「从沙箱拷到宿主」或自行宣布交付成功。
- Prompt 出现交付类后缀/类型词时，**不再**仅凭 NLP 置 `artifact_required=true` 预建门禁（双模态 Skill / 只读任务会被误伤）。
- **证据驱动（默认）**：Run 启动可不建 Spec；当 Harness 登记到可交付 office 产物（`.pptx/.docx/.xlsx/.pdf`，排除 qa_asset）且尚无 required primary 时，`FinalizeRequired` 再建 Spec 并交付。只读无产物 → 无门禁，可正常完成。
- **显式声明仍优先**：API / App Template `DeclaredDeliverable` 在启动时持久化，证据建约不会覆盖已有 primary required。

Run 初始化优先级：

1. **显式声明**（推荐）：API / CLI / App Template 经 `RunInitializationRequest.Deliverables`（`DeclaredDeliverable`）提交，控制面持久化为 `DeliverableSpec`；
2. **产物证据建约**：无 primary required 时，由 `FinalizeRequired` → `ensurePrimaryFromProduced` 按已登记产物后缀建约；
3. **遗留 ArtifactRequired 启发式**：仅当调用方显式置 `artifact_required=true`（API/Supplied Intent）且无 Deliverables 时，仍可由 `TaskDeliverableInitializer` 从 Prompt 推断类型；NLP IntentResolver **不再**自动置该标志。

命名策略（与交付覆盖策略配套）：

- **显式 `DesiredName`**（DeclaredDeliverable）：原样使用（basename 规范化）；跨 Run 同名时统一原子覆盖当前文件；
- **未指定 `DesiredName`**：Publish/Delivery 使用 produced.`ObservedName`；模型改名 = 产出新文件名；
- 历史版本由 Artifact publication 保留，不靠磁盘自动改名堆副本。

`artifact_required=true` 只是快速标记，不能替代具体 Spec；有显式声明时以声明为准。

```go
type DeclaredDeliverable struct {
    ID             string   `json:"id,omitempty"`
    Required       bool     `json:"required"`
    Role           string   `json:"role,omitempty"` // primary|supporting，默认 primary
    DesiredName    string   `json:"desired_name,omitempty"`
    AcceptedMIMEs  []string `json:"accepted_mimes,omitempty"`
    AcceptedSuffix []string `json:"accepted_suffixes,omitempty"`
    QAPolicy       string   `json:"qa_policy,omitempty"`
    DeliveryPolicy string   `json:"delivery_policy,omitempty"` // 默认 run-output
}

type DeliverableSpec struct {
    ID             string          `json:"id"`
    RunID          string          `json:"run_id"`
    Required       bool            `json:"required"`
    Role           DeliverableRole `json:"role"`
    DesiredName    string          `json:"desired_name,omitempty"`
    AcceptedMIMEs  []string        `json:"accepted_mimes,omitempty"`
    AcceptedSuffix []string        `json:"accepted_suffixes,omitempty"`
    QAPolicy       string          `json:"qa_policy,omitempty"`
    DeliveryPolicy string          `json:"delivery_policy"`
}
```

ProducedResource 与 Deliverable 通过独立选择记录关联：

```go
type DeliverableSelection struct {
    DeliverableID      string    `json:"deliverable_id"`
    ProducedResourceID string    `json:"produced_resource_id"`
    SelectedBy         string    `json:"selected_by"`
    CreatedAt          time.Time `json:"created_at"`
}
```

这样可以避免把 `primary/qa/required` 等业务语义写入纯资源 descriptor。

### 6.4 Publication 与 Delivery

```go
type ArtifactPublicationRecord struct {
    ID                 string            `json:"id"`
    ProducedResourceID string            `json:"produced_resource_id"`
    DeliverableID      string            `json:"deliverable_id"`
    ArtifactID         string            `json:"artifact_id,omitempty"`
    GateVersion        string            `json:"gate_version"`
    IdempotencyKey     string            `json:"idempotency_key"`
    Status             PublicationStatus `json:"status"`
    FailureCode        string            `json:"failure_code,omitempty"`
    // Gate 拒绝时可选持久化，供 Run 历史 / 失败诊断；非 Gate 失败保持为空。
    FailureValidator   string            `json:"failure_validator,omitempty"`
    FailureReason      string            `json:"failure_reason,omitempty"`
    Revision           uint64            `json:"revision"`
}

type DeliveryRecord struct {
    ID             string         `json:"id"`
    ArtifactID     string         `json:"artifact_id"`
    Target         ResourceRef    `json:"target"`
    IdempotencyKey string         `json:"idempotency_key"`
    Status         DeliveryStatus `json:"status"`
    FailureCode    string         `json:"failure_code,omitempty"`
    Revision       uint64         `json:"revision"`
}
```

推荐幂等键：

```text
publication:
tenant + run + deliverable_id + produced_resource_id + desired_name + gate_version

delivery:
tenant + artifact_id + target_resource_id + expected_target_version
```

Publication 与 Delivery 分离后，Artifact 已成功提交但交付失败时，只重试 Delivery，不能重新生成或重复提交 Artifact。

### 6.5 OutputReservation

`OutputReservation` 是 Harness 为某个 Deliverable 和 execution attempt 分配的逻辑写入槽位。它解决的是“受控命令应该把正式候选写到哪里”，不是正式 Artifact，也不是物理路径。

该模型属于 `artifact` 能力域，因为 reservation 由 Deliverable 驱动；其中的逻辑目标使用 workspace 的 `WorkspacePath`：

```go
type OutputReservation struct {
    ID            string        `json:"id"`
    RunID         string        `json:"run_id"`
    BindingID     string        `json:"binding_id"`
    DeliverableID string        `json:"deliverable_id"`
    AttemptID     string        `json:"attempt_id"`

    LogicalTarget WorkspacePath `json:"logical_target"`
    DesiredName   string        `json:"desired_name,omitempty"`
    MediaType     string        `json:"media_type,omitempty"`

    CreatedAt time.Time  `json:"created_at"`
    ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
```

约束：

- reservation 排他创建，每个 execution attempt 使用新 reservation，不覆盖旧 attempt 的候选；
- reservation 由 `OutputReservationStore` 持久化，进程重启后仍能验证命令产生的资源是否来自受信槽位；
- `LogicalTarget` 是规范化 workspace 相对路径，不携带 Host 或 sandbox 物理根；
- backend WorkspaceResolver 将 LogicalTarget 映射为进程环境变量或受控输出句柄；
- 环境变量名和值由 Harness 注入，模型不能覆盖；
- `task_job` 默认映射到严格隔离的 output namespace；
- `session_workspace` 映射到当前 binding 的受控 work namespace，可保留其他草稿，但正式候选仍写入本次 reservation；
- `project_workspace` 的 Artifact 生成默认写入 Run 状态空间的 reservation，不能因为最终 Delivery 回写项目就直接在项目目标路径生成；
- reservation 完成后仍必须注册为 ProducedResource，不能直接成为 Artifact；
- 不支持 reservation 的第三方 verbatim skill 使用受限差异检测，但不能把猜测路径写回 reservation。

### 6.6 QAEvidenceRecord

QA Evidence 必须绑定到被检查内容的稳定身份，不能只记录“执行过某条 QA 命令”。最小模型如下：

```go
type QAEvidenceRecord struct {
    ID                 string           `json:"id"`
    TenantID           string           `json:"tenant_id"`
    RunID              string           `json:"run_id"`
    DeliverableID      string           `json:"deliverable_id"`
    ProducedResourceID string           `json:"produced_resource_id"`
    PublicationID      string           `json:"publication_id,omitempty"`

    SubjectVersion string           `json:"subject_version"`
    SubjectSHA256  string           `json:"subject_sha256"`
    PolicyID       string           `json:"policy_id"`
    Validator      string           `json:"validator"`
    ValidatorVer   string           `json:"validator_version"`
    Status         QAEvidenceStatus `json:"status"`
    FailureCode    string           `json:"failure_code,omitempty"`

    EvidenceResourceIDs []string  `json:"evidence_resource_ids,omitempty"`
    CreatedAt           time.Time `json:"created_at"`
}
```

约束：

- QA 必须绑定 Source version 和 staged content SHA256；
- Commit 后 Artifact SHA256 必须与 evidence 的 SubjectSHA256 一致，否则 evidence 无效；
- Validator/Policy 版本变化后是否需要重跑由 QAPolicy 决定；
- QA 截图、渲染页等仍是 ProducedResource，通过 ID 引用，不直接变成正式 Artifact；
- 质量 QA 失败可以阻止 completed，但不能伪造为底层读取或格式 Gate 失败；已 Commit Artifact 与 incomplete QA 必须分别记录；
- CompletionPolicy 只接受与当前 DeliverableSelection 和 PublicationRecord 精确匹配的成功 evidence。

## 7. run:/ 的最终语义

`run:/` 保留为 ProducedResource 的规范化逻辑别名，但不是 backend locator，也不是模型发布时必须复制的参数。

```text
run:/work/binding-123/skills/office-ppt/output.pptx
  -> ProducedResourceDescriptor.ID
  -> BindingID
  -> PreparedExecutionSnapshot.Backend
  -> Source ResourceRef
  -> ResourceReaderRouter
```

普通模型只看到最小候选投影：

```json
{
  "candidate_id": "produced-01J...",
  "name": "output.pptx",
  "media_type": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  "deliverable_id": "deliverable-primary"
}
```

以下信息不得进入普通 Tool 输出：

- Host 绝对路径；
- `/workspace/...` 物理执行路径；
- backend locator；
- session ID；
- `path_map`；
- 可被模型复制的物理 `skill_dir/work_dir`。

`run:/` 仍可用于日志、UI、审计、运维诊断和高级资源工具。

## 8. Harness 决策策略

### 8.1 输出登记

优先顺序：

1. Harness 为声明交付物分配 `OutputReservation`，命令写入受控输出句柄或环境变量；
2. 第三方 verbatim skill 无法使用 reservation 时，使用受限目录的可信差异检测；
3. 按 DeliverableSpec 的 MIME、后缀、声明名称和输出范围过滤（reservation 命中始终保留；diff 候选须匹配交付契约后才登记）；
4. Harness 调用 ProducedResourceRegistrar 创建 descriptor；
5. 模型不得提交 locator、version 或物理路径参与登记。

不能为了某一个项目或 office-ppt 把 `run_skill_command.inputs`、generated module 等项目专用协议写入通用 `SKILL.md`。通用能力通过 Harness contract 和 skill metadata 表达。

### 8.2 唯一候选

如果只有一个 ProducedResource 满足 required DeliverableSpec：

```text
Harness 自动创建 DeliverableSelection
  -> 通过统一权限、审计和 Artifact Gateway 发起发布
```

不要求模型再调用 `publish_artifact`。

这不是自动发布任意 produced，而是确定性执行已经存在的交付契约。

### 8.3 多个候选

仅在多个候选确实需要语义选择时允许模型参与：

```json
{
  "tool": "select_deliverable_candidate",
  "arguments": {
    "deliverable_id": "deliverable-primary",
    "candidate_id": "produced-01J..."
  }
}
```

`candidate_id` 必须来自 Harness 返回的候选集合；`deliverable_id` 必须来自同一轮候选投影或交付契约。文件名默认由 DeliverableSpec 决定，只有任务确实没有声明名称时才允许受限重命名。

### 8.4 无候选

返回结构化错误：

```json
{
  "failure_kind": "required_deliverable_not_produced",
  "deliverable_id": "deliverable-primary",
  "retryable": true
}
```

不得要求模型猜测路径、扫描不受信目录或构造 `run:/`。

## 9. 三类执行环境

统一的是 Descriptor、ResourceHandle、Publication、Gate、Store、Delivery、权限、审计、幂等和完成门禁。

独立的是 locator 创建、Reader、版本 token、文件身份和生命周期。

| 环境 | ResourceRef 示例 | Reader 行为 |
|---|---|---|
| Host | `host/run-file/<opaque-id>@version` | no-follow、realpath、文件身份、size/mtime/hash 复验 |
| LocalSandbox | `local-sandbox/sandbox-file/<opaque-id>@version` | 按平台能力读取，不能假定一定存在宿主路径映射 |
| Remote | `remote-executor/session-file` 或 `executor-object` | WorkspaceFS 流式读取或 durable object 下载 |

ReaderRouter 必须根据：

```text
ResourceRef.Authority + ResourceRef.Scheme
```

选择 Reader，不能根据模型传入的 provider/backend 字符串分支。

统一 Reader 返回版本化 `ResourceHandle`：

```go
type ResourceHandle struct {
    Reader    io.ReadCloser
    Size      int64
    Version   string
    MediaType string
}
```

Artifact 能力中仅返回 `io.ReadCloser` 的重复 `SourceReader` 应删除。

### 9.1 Windows、Linux、macOS

三种宿主 OS 可以使用不同实现，但不能改变上层契约：

- Windows Reader 处理 volume、junction、reparse point、文件 ID 和大小写规则；
- Linux/macOS Reader 处理 inode、symlink、mount boundary 和大小写策略；
- 所有业务 `WorkspacePath` 使用正斜杠且必须是规范化相对路径；
- OS `filepath` 只允许出现在具体 Host/LocalSandbox adapter 内；
- 通用 logical model 不得使用当前进程 OS 规则展开远程路径。

## 10. 远程资源生命周期

持久化 Descriptor 不等于资源内容永久存在。

支持两种 availability：

```text
leased:
  依赖 remote session/lease，具有 ExpiresAt

durable:
  已提升为 executor object，可在 Run 恢复后读取
```

`durable executor object` 的正式定义是：

> 由远程执行后端管理的不可变、版本化二进制对象；其可读性不依赖创建它的 live session，在约定的 Run/租户保留期内可以仅凭受 scope 约束的 opaque object ID 和 version token 流式读取。

它至少必须具备：

- 排他或幂等创建；
- immutable/versioned 内容；
- tenant/run scope；
- hash、size、media type 元数据；
- 流式读取，不能要求客户端整文件载入内存；
- 明确保留期、过期时间和删除语义；
- 审计与授权；
- locator 不包含公开 URL 和长期 credential。

把 session 文件复制到 genesis-agent 宿主临时目录不属于 durable executor object。

规则：

1. required 候选未发布前，不得释放其 session lease；短链路可通过续租保持可读，但续租不等于 durable；
2. Run 可能暂停、重启、跨实例恢复或等待人工选择时，目标态应将相关候选提升为 durable object；
3. session 过期后不得重新扫描并猜测相似文件；
4. locator 过期返回稳定 `PRODUCED_RESOURCE_EXPIRED`；
5. credential 由 bootstrap/runtime 注入，不写入 descriptor；
6. 如果 genesis-sandbox 暂不支持 durable object，必须把该能力作为跨仓依赖明确实现，不能用宿主临时目录伪装持久化。

### 10.1 当前实施边界（延期项）

以下能力属于目标架构，**当前阶段明确延期**，不以宿主临时目录或假持久化替代：

1. **leased → durable 提升 API（跨仓）**  
   genesis-agent 已装配 durable `executor-object` Reader/Resolver，但 **不在本阶段实现** session-file 到 durable object 的提升调用。远程产物在 lease 有效期内可读；过期返回 `PRODUCED_RESOURCE_EXPIRED`。跨进程/跨实例长时间恢复依赖后续 genesis-sandbox 提供正式 durable object 创建与提升 API 后再接入。

2. **流式 OpenObject（跨仓）**  
   生产目标要求 durable object **流式**读取。当前若临时使用 sandbox 既有 `DownloadArtifact`（`[]byte`）缓冲适配，仅作开发期兼容，**不视为生产完成态**；真正流式 `OpenObject` 需 genesis-sandbox 提供后再替换缓冲适配。本阶段不为此单独升级伪装实现。

在延期期间，产品策略可以是：短链路发布前保 lease/续租；无法续租或已过期则结构化失败并允许用户重跑，而不是静默降级到宿主落盘。

## 11. Artifact Gate 与 QA

`ArtifactGatePipeline` 读取 staged immutable object，只承载决定“该内容能否成为正式 Artifact”的 publication-blocking validator，可组合：

- 大小限制；
- hash 与版本一致性；
- MIME 和扩展名一致性；
- 文件格式完整性；
- 恶意内容或宏策略；
- 业务基础校验。

质量 QA 与 Artifact Gate 是两个相关但独立的概念：

- 文件损坏、MIME 欺骗、恶意内容等属于 Artifact Gate；失败时不得 Commit；
- 排版质量、视觉一致性、业务内容完整度等属于 QAPolicy/QAEvidence；可以读取同一 staged object 或已 Commit Artifact；
- 某项质量 QA 是否 publication-blocking 由受信 QAPolicy 明确声明，不能由模型临时决定；
- 非 publication-blocking QA 失败或环境不可用时，Artifact 可以 Commit/Delivery，但 Run 必须以 incomplete 或结构化失败结束，不能记为 completed；
- CompletionPolicy 根据 QAEvidenceRecord 判断，不解析 Gate 日志或模型回答。

视觉 QA 缺少图片检查原语不阻塞本架构。未来可以作为 Gate validator 或独立 QA Evidence producer 接入，不需要修改 ProducedResource、Reader、Store 和 Delivery 主链路。

## 12. 完成门禁

Run 可进入 completed，当且仅当：

```text
对每个 required DeliverableSpec：
  存在唯一有效 DeliverableSelection
  且存在成功 ArtifactPublicationRecord
  且存在成功 DeliveryRecord
  且 QAPolicy 要求的 QA Evidence 已通过
```

`artifact_required=true` 可以作为 TaskIntent 快速标记，但不能替代具体 DeliverableSpec。

`DeliveryArtifactOnly` 也必须产生成功的 DeliveryRecord。它表示 ArtifactStore 中的 ArtifactRef 已经成为用户可访问的正式交付对象，只是不执行额外的物理 materialize；这样 CompletionPolicy 不需要为 Artifact-only 建立特殊旁路。

任意 `produced[]` 不再自动阻止完成，因为它可能是：

- QA 截图；
- 缩略图；
- 日志；
- 临时渲染；
- 中间 JSON；
- 缓存文件。

`SkillFollowState` 可以继续承担模型提示、当前 Turn 软提醒和重复调用保护，但不得作为完成事实。

## 13. 错误语义

至少提供以下稳定错误码：

| 错误码 | 含义 |
|---|---|
| `PRODUCED_RESOURCE_NOT_FOUND` | candidate ID 不存在或不属于当前 scope |
| `PRODUCED_RESOURCE_EXPIRED` | leased locator 已过期 |
| `PRODUCED_RESOURCE_VERSION_CONFLICT` | 注册后资源发生变化 |
| `PRODUCED_RESOURCE_BACKEND_MISMATCH` | Binding backend 与 Source authority 不一致 |
| `DELIVERABLE_NOT_PRODUCED` | required deliverable 无合法候选 |
| `DELIVERABLE_SELECTION_AMBIGUOUS` | 存在多个候选，需要选择 |
| `ARTIFACT_INVALID` | staged object 未通过格式、安全、大小或业务 Gate；通过结构化 `reason/validator` 区分具体拒绝原因 |
| `ARTIFACT_PUBLICATION_CONFLICT` | 幂等键相同但请求内容不同 |
| `DELIVERY_TARGET_DENIED` | 交付目标未授权 |
| `DELIVERY_TARGET_CONFLICT` | 目标无法覆盖（非普通文件、目录、symlink 或权限拒绝等）；普通同名文件应走原子覆盖而非本错误 |
| `ARTIFACT_DELIVERY_REQUIRED` | required deliverable 尚未完成交付 |

错误必须保留原始分类，Artifact Publisher 不得把 Reader 的 version/expiry/backend 错误统一包装成 `ARTIFACT_PATH_INVALID`。

`ARTIFACT_INVALID` 复用主规范和现有 Artifact contract 的稳定语义，不再新增并行的 `ARTIFACT_GATE_REJECTED`。Gate 细分原因放入结构化字段（返回错误与 `ArtifactPublicationRecord.failure_validator` / `failure_reason`），避免公共错误码随 validator 数量膨胀。

## 14. 能力域与目录归属

推荐目录：

```text
internal/capabilities/workspace/
  model/
    produced_resource.go
  contract/
    produced_store.go
    resource_reader.go
  service/
    produced_registrar.go
    resource_reader_router.go

internal/capabilities/artifact/
  model/
    deliverable.go
    output_reservation.go
    qa_evidence.go
    publication.go
    delivery.go
  contract/
    publisher.go
    output_reservation_store.go
    qa_evidence_store.go
    publication_store.go
    delivery_store.go
    completion_policy.go
  service/
    output_reservation.go
    publication.go
    gate_pipeline.go
    delivery.go
    completion.go

shared/local/workspace/
  host_resource_reader.go
  host_locator_store.go
  host_resource_reader_windows.go
  host_resource_reader_unix.go

internal/capabilities/workspace/adapter/
  sandbox_resource_reader.go
```

边界：

- workspace 拥有资源身份、ProducedResource、Reader contract 和路由；
- execution 拥有 Binding、BackendRef 和命令执行；
- artifact 拥有 Deliverable、Publication、Gate、ArtifactStore 和 Delivery；
- sandbox adapter 只提供产品无关 API client/contract；
- Host 真实文件读写只存在于 `shared/local`；
- Enterprise 注入租户级 ProducedResourceStore、OutputReservationStore、QAEvidenceStore、ArtifactStore、PublicationStore 和 DeliveryStore，不回退本地文件或内存；
- products 只负责配置、credential、endpoint、策略和 bootstrap。

备选方案是新增独立 `resource` 能力域。当前 workspace 已经拥有 `ResourceRef`、`ResourceReader` 和 RunManifest，新增能力域会增加概念及迁移成本，因此本阶段不采用。

### 14.1 依赖方向

```text
skill/script Harness
  -> workspace/contract.ProducedResourceRegistrar

workspace/service.ProducedResourceRegistrar
  -> workspace/contract.ProducedResourceStore
  -> workspace/contract.BackendResourceResolver

artifact/service.ArtifactPublicationService
  -> workspace/contract.ProducedResourceStore
  -> workspace/contract.ResourceReaderRouter
  -> artifact/contract.ArtifactStore
  -> artifact/contract.PublicationStore

artifact/service.OutputReservationService
  -> artifact/contract.OutputReservationStore
  -> workspace/contract.WorkspaceResolver

artifact/service.DeliveryService
  -> artifact/contract.ArtifactStore
  -> artifact/contract.DeliveryStore

runtime CompletionPolicy
  -> artifact/contract.DeliverableStore
  -> artifact/contract.PublicationStore
  -> artifact/contract.DeliveryStore
  -> artifact/contract.QAEvidenceStore
```

关键限制：

- artifact service 只依赖 workspace contract/model，不依赖 workspace service 或具体 Reader；
- workspace 不依赖 artifact；`DeliverableID` 只出现在 artifact 拥有的 reservation/selection 中；
- skill Harness 通过 registrar contract 登记资源，不直接写 ProducedResourceStore；
- runtime 通过完成策略端口查询控制面事实，不直接理解具体数据库或文件 store。

### 14.2 跨域一致性与事务

ProducedResource、ArtifactStore、Publication、Delivery 可能位于不同存储，禁止假设存在跨域分布式事务。统一采用持久化状态机、幂等调用和补偿恢复：

1. ProducedResource 已经排他创建后才允许创建 PublicationRecord；
2. PublicationRecord 先以 `pending` 创建并取得执行 lease；
3. Stage 成功后记录 quarantine transaction ID；
4. Gate 失败则 Abort staged object，并把 PublicationRecord 标记为 failed；
5. Commit 成功后先查询/记录 ArtifactRef，再把 PublicationRecord CAS 为 published；
6. 进程在 Commit 与 record 更新之间崩溃时，通过 idempotency key 查询 ArtifactStore 并恢复 record；
7. Delivery 作为下一独立状态机运行，失败不回滚已 Commit Artifact；
8. 异步实现使用 transactional outbox 或等价可靠事件机制，不能依赖进程内消息；
9. 单一数据库部署可以在实现内部使用本地事务优化，但不能改变端口语义。

## 15. 必须删除的旧代码与兼容路径

当前处于开发阶段，最终主分支不保留双链路、alias 或旧数据兼容：

- `ProducedSourceOpener`；
- `ProducedSourceRegistration.Opener`；
- `RunSourceRegistry.external`；
- 捕获内存 session 的 `remoteProducedSource`；
- Artifact 域重复的简化 `SourceReader`；
- `ArtifactCollectionPolicy`；
- `execution.Result.Artifacts`；
- 旧 `execution.Artifact`；
- `LocalArtifactRoot`；
- `materializeArtifacts`；
- sandbox client 自动下载并写宿主目录；
- `InputArtifactRef.LocalPath/RemotePath`；
- basename 交付完成判定；
- `publish_artifact` 对 `artifact/artifact_path/artifact_name` 旧字段的恢复提示；
- 普通模型可见的物理 `work_dir/skill_dir/path_map`。

`ArtifactCollectionPolicy` 如果仍有执行输出发现价值，应替换为语义明确的 `OutputDiscoveryPolicy` 或 `DeclaredOutputSpec`，不能继续使用 Artifact 命名。

外部 sandbox API 如果暂时保留 JSON 字段 `output_artifacts`，只能停留在 transport DTO，并立即映射为 executor object/resource locator；不能进入 Genesis 业务 `Artifact` 模型。若允许同步修改 `genesis-sandbox`，应改名为 `output_objects` 或 `produced_resources`。

## 16. 文档修改

### 16.1 保留但修正主规范

`docs/统一执行工作空间、文件权限与产物规范.md` 的总体方向正确，不推翻，但需要修改两项表述。

原表述“produced 不能自动进入 ArtifactStore”应改为：

> 任意诊断 produced 候选不能自动发布；与受信 DeliverableSpec 唯一匹配的 required 候选，可由 Harness 通过统一权限、审计和 Artifact Gateway 确定性发布。

原表述“必须由模型显式调用 publish_artifact”应改为：

> 发布必须是显式控制面操作，但不要求由模型发起。Harness 可以在确定性匹配后发起；只有真实语义歧义才交给模型选择 candidate ID。

### 16.2 标记或重写旧文档

以下旧设计仍描述 `OutputArtifacts -> DownloadArtifact`、`/workspace/output` 自动回收或 `artifacts[].path`，需要标记 superseded 或按本设计重写：

- `docs/执行工作空间与Sandbox文件路径契约.md`；
- `docs/通用第三方Skill执行模型与office-ppt迁移设计.md`；
- `docs/项目目录与边界说明.md` 中三端产物路径表述。

目标是确保规范、代码、Tool schema 和 prompt 中只存在一套资源与 Artifact 语义。

## 17. 迁移方案

该变更涉及 workspace、artifact、execution、sandbox 和产品 bootstrap，实施前需要按目录变更规则确认。

### 阶段一：模型与规范

1. 更新主规范和关联文档；
2. 增加 ExecutionBackendRef；
3. 增加 ProducedResourceDescriptor、DeliverableSpec、OutputReservation、QAEvidenceRecord、PublicationRecord、DeliveryRecord；
4. 定义 store、registrar、reader router、completion policy 端口；
5. 固化错误码和幂等语义。

### 阶段二：Host 与当前本地沙箱 Reader 准备

1. 实现本地持久化 ProducedResourceStore；
2. 实现 Host locator store 和 Host Reader；
3. 实现使用 workspace ResourceReaderRouter 的新版 ArtifactPublicationService；
4. 先通过 contract/integration test 验证 Host 链路，不在生产装配中双写或按 feature flag 暴露两套 Publisher；
5. 准备远程和当前启用的 LocalSandbox Reader 后，在同一合并单元中原子切换 Publisher 构造、bootstrap 装配和测试；
6. 同一合并单元删除 Artifact `SourceReader`、旧 resolver/reader 和旧 constructor，不提供兼容 adapter。

### 阶段三：远程接入与 Publisher 原子切换

1. 实现 Remote session-file Reader；
2. 核对并实现 durable executor object；
3. 实现 lease/expiry/promotion；
4. 将 sandbox `OutputArtifacts` 映射改为 ProducedResource locator；
5. 与阶段二共同完成 Publisher/ReaderRouter 的原子运行时切换；
6. 删除 LocalArtifactRoot 和自动 materialize。

### 阶段四：扩展 LocalSandbox 与跨平台实现

1. 根据平台沙箱是否共享 Host workspace 选择 Reader；
2. 不根据 provider 名猜测可访问性，使用明确 capability；
3. 实现 Windows/Linux/macOS 文件身份与 no-follow 校验；
4. 完成跨平台契约测试。

### 阶段五：Harness 与完成门禁

1. 持久化 DeliverableSpec；
2. 实现 OutputReservation 和候选匹配策略；
3. 唯一候选由 Harness 自动发布；
4. 多候选只暴露 candidate ID；
5. 完成门禁切换到持久化 ledger；
6. 删除 SkillFollow basename 完成判定、旧 Tool 提示和旧文档。

每个阶段可以分提交实施，但最终合并结果不得保留可被运行时选择的旧链路。

## 18. 并发、恢复与安全

### 18.1 并发

- Descriptor ID 使用排他 Create；同 logical_ref 的内容变更通过 UpsertCurrent 推进 head（非 conflict）；
- DeliverableSelection：首次 Create 排他；仅当旧选择已不是当前 produced head 时允许 ReplaceSelection 重绑并重新发布；
- Delivery materialize：目标为普通文件且同名已存在时，**统一** `ReplaceMaterialize` 原子覆盖（跨 Run 与同 Run supersede 同策略）；symlink/目录/权限拒绝等仍返回 `DELIVERY_TARGET_CONFLICT`，不得静默改名；
- 未指定交付名时靠 Run 级 stamp 降低跨 Run 撞车；指定名则覆盖用户可见槽位，历史进 Artifact；
- `FinalizeRequired` 在仍无法覆盖的 `DELIVERY_TARGET_CONFLICT` 时返回结构化 `delivery_conflict`，不得把后续 `run_skill_command`（含 QA）整体打成失败；完成门禁仍要求最终成功 Delivery；
- Publication/Delivery 状态更新使用 revision/CAS；
- 相同幂等键、相同请求返回原结果；
- 相同幂等键、不同请求返回 conflict；
- 只有一个 worker 获得 `publishing` 或 `delivering` lease；
- lease 超时后允许安全接管，但必须先查询 Store/Delivery 是否已完成。

### 18.2 恢复

- 进程重启后从 store 恢复 Descriptor、Publication 和 Delivery；
- staged object 存在但 record 未提交时，根据 transaction ID 继续或 Abort；
- Artifact 已 Commit、Delivery 未完成时只重试 Delivery；
- session-file 已过期时返回明确失败，禁止猜测或静默重新生成；
- required deliverable 未交付时 Run 不得写 completed。

### 18.3 权限与租户

- 所有查询和唯一键必须带 tenant scope；
- 每次 registrar、reader open、publish、delivery 都重新鉴权；
- Binding、Source Scope、Artifact Scope 和 Delivery Target Scope 必须一致或存在明确授权转换；
- locator store 不得允许模型按 ID 枚举其他 Run 或租户资源；
- backend credential 只由 bootstrap 注入。

## 19. 验收标准

### 19.1 功能

- 唯一声明产物无需模型调用发布工具即可交付；
- 多候选时模型只能选择合法 candidate ID；
- 发布失败不会产生正式 Artifact；
- 交付失败不会重复创建 Artifact；
- QA 截图和中间文件不会误触发 required delivery；
- 重启后能够继续 publication/delivery；
- session 过期返回稳定错误；
- 同名不同资源不会被误关联。

### 19.2 三环境

- Windows/Linux/macOS Host；
- Windows/Linux/macOS 本地平台沙箱；
- Remote session-file；
- Remote durable object；
- Remote 模式不展示宿主绝对路径；
- 同一任务在三环境中的 candidate、ArtifactRef 和 Delivery 语义一致。

### 19.3 安全与并发

- symlink/junction/reparse point 越界；
- 注册后替换文件；
- 读取期间版本变化；
- 并发重复发布；
- 并发重复交付；
- 跨租户 locator 访问；
- Delivery 目标在审批后发生版本变化；
- remote lease 过期和 worker 接管。

### 19.4 工程验证

```powershell
$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'
$env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'
go test ./...
```

同时执行产品隔离检查、race 测试、Remote API contract 测试和必要的人工端到端交付测试。

## 20. 被否决方案

### 20.1 继续为路径错误增加兼容分支

否决原因：不能解决重启、版本、权限、远程生命周期和完成事实问题，只会扩大路径语义数量。

### 20.2 让模型继续复制 run:/ 并重试 publish_artifact

否决原因：确定性动作没有必要交给模型；会继续出现字段错误、路径抄错和无效迭代。

### 20.3 持久化 ProducedSourceOpener 或 session 对象

否决原因：行为对象和进程内 client 不能跨实例、跨版本可靠恢复。

### 20.4 自动发布所有 produced 文件

否决原因：会把 QA 截图、日志、中间文件和缓存当成用户交付物。自动发布只允许用于唯一匹配的 required DeliverableSpec。

### 20.5 将 Remote 文件统一下载到宿主临时目录

否决原因：破坏 Enterprise 和远程边界，造成不必要传输，并绕过 backend 生命周期和统一 ResourceReader。

### 20.6 在 Descriptor 中重复保存 BackendKind

否决原因：Binding snapshot 与 Descriptor 可能产生不一致。Backend 是 execution 控制面事实，Descriptor 只引用 BindingID。

### 20.7 新增独立 resource 能力域

暂不采用原因：workspace 已拥有 ResourceRef、ResourceReader 和 RunManifest；现阶段新增能力域会增加概念和迁移成本。未来若资源能力明显超出 workspace 边界，可以再通过正式目录变更评审拆分。

## 21. 影响范围

预计涉及：

- `internal/capabilities/workspace`；
- `internal/capabilities/artifact`；
- `internal/capabilities/execution`；
- `internal/capabilities/sandbox`；
- `internal/capabilities/skill/script/service`；
- `internal/runtime` 完成门禁；
- `shared/local/workspace` 和 `shared/local/artifact`；
- CLI、Desktop、Enterprise bootstrap；
- genesis-sandbox API 对接契约；
- Tool schema、prompt 和相关架构文档；
- 本地文件 store、Enterprise tenant store 和测试矩阵。

## 22. 剩余风险、延期项与产品确认

### 22.1 已确认延期（本阶段不做）

| 项 | 原因 | 当前行为 |
|---|---|---|
| genesis-sandbox **durable object 提升 API** | 跨仓依赖；禁止用宿主临时目录伪装 | 远程候选保持 leased；lease 内可读，过期 `PRODUCED_RESOURCE_EXPIRED` |
| genesis-sandbox **流式 OpenObject** | 跨仓依赖；`DownloadArtifact` 全量缓冲不符合生产契约 | 若装配缓冲适配，仅开发期；生产切换前不得宣称 durable 读取完成 |
| Desktop **完整 Skill 工具栈** | Desktop 当前 Profile 未启用 Skill 工具；Artifact 控制面已装配 | 启用 `run_skill_command` 时必须像 CLI 注入 ProducedResources / Reservations / Finalizer / Selector |

### 22.2 仍需产品/部署确认

1. remote session lease 的最大时长、续租和 worker 接管语义（在无 durable 提升前，这是远程恢复的主要手段）；
2. Enterprise 租户级 Store 由何种持久化实现提供（显式注入，Init fail-closed，禁止静默回退内存/本机文件）；
3. ArtifactStore quarantine transaction 的恢复/Abort 运维策略；
4. 哪些 App Template 在产品边界把交付契约映射为 `DeclaredDeliverable` 注入 Run；其余继续依赖 TaskIntent 启发式（猜不出须 fail-closed）。

这些是实施依赖和产品策略选择，不改变本文核心架构。

## 23. 外部评审检查清单

评审者应重点检查：

- 确认 ProducedResource 归属 workspace 域的现有理由是否充分，是否出现必须提前拆分独立 resource 域的新证据；
- Descriptor 与 ExecutionBinding 是否仍存在重复真相；
- opaque locator 是否足以支持三类 backend；
- leased/durable 是否覆盖远程恢复场景；
- Stage -> Gate -> Commit 是否满足 ArtifactStore 实现能力；
- DeliverableSpec 是否足以避免自动发布中间文件；
- Publication/Delivery 幂等键是否满足并发与恢复；
- CompletionPolicy 是否遗漏无需物理 Delivery 的 Artifact-only 场景；
- Harness 自动发布是否仍经过统一权限、审计和审批；
- Windows junction/reparse point 与 Unix symlink 是否均被 adapter 隔离；
- 旧链路是否可以彻底删除而不影响当前宿主环境；
- 文档、Tool schema、prompt 和代码是否最终只保留一套语义。

## 24. 外部评审建议吸收记录

2026-07-17 外部架构评审认为总体方向正确，并提出若干实施清晰度问题。本版按以下方式吸收：

| 建议 | 决策 | 修改原因 |
|---|---|---|
| 补充 OutputReservation 模型 | 吸收 | 它是 Harness 确定性输出链路的核心，缺失会导致 task/session/project 各自发明路径语义 |
| 补充 QAEvidenceRecord | 吸收 | CompletionPolicy 必须查询可持久化、与具体内容版本绑定的 QA 事实 |
| 定义 durable executor object | 吸收 | 持久化 locator 与资源真正可恢复是两个概念，必须给出最低契约 |
| 明确 SourceReader 迁移 | 调整后吸收 | 明确原子切换并删除旧接口，不新增兼容层或双运行路径 |
| 处理 ARTIFACT_GATE_REJECTED/ARTIFACT_INVALID 重叠 | 吸收 | 保留主规范已有 `ARTIFACT_INVALID`，用结构化 reason/validator 表达 Gate 原因 |
| 明确跨域事务策略 | 吸收 | workspace/artifact/delivery 不应依赖分布式事务，改用持久化状态机、幂等与恢复 |
| 明确 Registrar 依赖方向 | 吸收 | 防止 artifact 依赖 workspace service、Harness 绕过 registrar 直接写 store |
| 调整评审清单的开放性措辞 | 吸收 | 正文已经作出 workspace 归属决策，检查项只应验证理由是否仍成立 |
| 增加 Mermaid 依赖图 | 部分吸收 | 使用仓库文档更稳定的文本依赖图表达相同信息，不额外依赖 Mermaid 渲染 |

## 25. 最终决策

采用持久化 ProducedResourceDescriptor、版本化 ResourceReaderRouter、Deliverable 驱动的 Harness 确定性选择、ArtifactStore 两阶段提交、持久化 Publication/Delivery ledger 和控制面 CompletionPolicy。

不再继续为宿主路径、sandbox 路径和远程路径增加补丁；不保留旧 Artifact 自动下载链路；不让模型承担可由 Harness 确定完成的路径和发布决策。
