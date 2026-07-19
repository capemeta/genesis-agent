# 多模态视觉 + 用户 Turn 输入 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按 [`多模态输入与视觉能力设计.md`](../../多模态输入与视觉能力设计.md) 与 [`用户Turn输入与附件契约设计.md`](../../用户Turn输入与附件契约设计.md) 落地内核契约、视觉流水线、`view_image`/VisionExpert、QA 证据分轨，以及 CLI 显式附件进入 `TurnInput`（Desktop/Enterprise 复用同一内核契约）。

**Architecture:** 产品适配器只产出 `AttachmentDescriptor`；Runtime 统一 `TurnInput` → staging/Parts；`supports_image` + Run 级 `EffectiveVisionMode` + Per-Request `ImageSanitizer`；`view_image` 在 `media` 只做取图原语，形态 B 由 Runtime `VisionExpert` 路由；视觉 QA 分轨校验后才 `RecordPassed(visual-qa/v1)`。

**Tech Stack:** Go、现有 `domain` / `platform/config` / eino adapter、Tool Gateway、artifact QA、CLI cobra/TUI。

**Out of this delivery (明确推迟):** leased 过期后**无 Agent 参与**的静默自动重跑 thumbnail；Wails 完整 Desktop UI / 营销级拖拽剪贴板页；`detail=original` 能力探测文档化（P2）。内核与三端最小路径已覆盖两文档验收标准。

---

## 需求追溯矩阵（两文档细节）

### A. 多模态输入与视觉能力设计

| ID | 需求 | 计划任务 |
|----|------|----------|
| M1 | `supports_image` 配置，默认 false | Task 1 |
| M2 | `EffectiveVisionMode` A/B/C | Task 1 |
| M3 | Run 级工具门控 vs Per-Request Sanitizer | Task 1, 3 |
| M4 | `domain.Message.Parts` + 持久化只存 ImageRef | Task 2 |
| M5 | 统一装配：resolve → resize budget → Sanitizer → Provider | Task 3, 4 |
| M6 | Compact 旧图降级文本（N=2） | Task 3 |
| M7 | `view_image` 在 `internal/capabilities/media` | Task 5 |
| M8 | candidate_id / path；禁绝对路径 | Task 5 |
| M9 | 形态 B：VisionExpert，工具不裸调 LLM | Task 6 |
| M10 | 形态 C：`vision_unavailable` | Task 5, 6 |
| M11 | Validator 分轨 content/render/visual | Task 7 |
| M12 | visual-qa passed 需 checklist / 专家结论 | Task 7 |
| M13 | 废除 skill-command 成功即 visual passed | Task 7 |
| M14 | 错误码表 | Task 5, 8 |
| M15 | 验收：无图模型永不残留 image bytes；DB 无 base64 | Task 3, 9 |

### B. 用户 Turn 输入与附件契约

| ID | 需求 | 计划任务 |
|----|------|----------|
| T1 | `AttachmentDescriptor` + `StartRunRequest.Attachments` | Task 8 |
| T2 | role 分流：image→Parts；document≠image | Task 8, 9 |
| T3 | 协议不传大字节 | Task 2, 8 |
| T4 | CLI `--attach` + TUI `@path` | Task 10 |
| T5 | E2 文本点名默认不注入像素 | Task 9（默认 off） |
| T6 | `mention_resolve` hint/auto 开关（可选） | Task 9 |
| T7 | Document extracted_text 预算（可插拔） | Task 8 |
| T8 | 错误码 attachment_* / not_an_image | Task 5, 8 |
| T9 | Desktop/Enterprise 同契约 | Task 8 接口；产品 UI 推迟 |
| T10 | 与 SSE 分工：输入不进 SSE 文档 | 文档状态更新 Task 12 |

---

## 文件地图

| 路径 | 职责 |
|------|------|
| `internal/platform/config/config.go` | `SupportsImage` 字段与 Resolve |
| `internal/capabilities/llm/vision/mode.go` | `EffectiveVisionMode` 解析 |
| `internal/domain/message.go` / `content_part.go` | `ContentPart` / `ImageRef` / Message.Parts |
| `internal/domain/attachment.go` | `AttachmentDescriptor` / roles |
| `internal/domain/run.go` | `StartRunRequest.Attachments` |
| `internal/capabilities/llm/sanitize/image.go` | Per-Request ImageSanitizer |
| `internal/capabilities/media/materialize/materializer.go` | 仅 outbound 瞬间：ImageRef → bytes（缩放/10MB/2048）；供 Provider/Expert |
| `internal/capabilities/llm/adapter/eino/message_convert.go` | Parts → eino MultiContent（或 AssembleOutbound 单点） |
| `internal/runtime/context/compactor*.go` | 旧图降级 |
| `internal/capabilities/media/tool/view_image/` | view_image 工具 |
| `internal/runtime/vision/expert.go` | VisionExpert |
| `internal/capabilities/artifact/service/qa_recorder.go` + contract | 分轨 Record* |
| `internal/runtime/strategy/react/react_loop.go` | 装配首轮 Parts、挂 VisionExpert、停伪 QA |
| `internal/app/run_service.go` | 传递 Attachments |
| `products/cli/...` | `--attach` / `@path` → Attachments |
| `internal/bootstrap` / `products/*/bootstrap` | 注册 view_image、注入依赖 |

---

### Task 1: supports_image + EffectiveVisionMode

**Files:**
- Modify: `internal/platform/config/config.go`
- Create: `internal/capabilities/llm/vision/mode.go`, `mode_test.go`

- [x] **Step 1:** 为 `LLMModelConfig` / `ResolvedLLMConfig` 增加 `SupportsImage *bool`；Resolve 时未写 → `false`。
- [x] **Step 2:** 实现 `ResolveEffectiveVisionMode(mainSupportsImage bool, visionAlias string, visionSupportsImage bool) Mode` → `direct_inject` / `expert_route` / `degraded_text`。
  - 四种配置组合：①主可看图+有 vision→A；②主可看图+无 vision→A；③主不可看图+vision 可看图→B；④主不可看图+无可用 vision→C。详见 `docs/多模态输入与视觉能力设计.md` §2.0；默认值与注释见 `configs/llm.yaml`。
- [x] **Step 3:** 单测覆盖 A/B/C 与默认 false。
- [x] **Step 4:** 运行  
  `$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'; $env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'; go test ./internal/capabilities/llm/vision/... ./internal/platform/config/...`

---

### Task 2: domain ContentPart / ImageRef / Message.Parts

**Files:**
- Create: `internal/domain/content_part.go`, `content_part_test.go`
- Modify: `internal/domain/message.go`

- [x] **Step 1:** 定义 `ContentPartType`、`ContentPart`、`ImageRef`（candidate_id / produced_resource_id / path_alias / media_type / sha256；**无 bytes 字段用于持久化**）。
- [x] **Step 2:** `Message` 增加 `Parts []ContentPart`；`TextContent()` 聚合 Parts 文本或回退 `Content`。
- [x] **Step 3:** `NewUserMessageWithParts(text string, parts []ContentPart)`；保证仅文本时与旧行为兼容。
- [x] **Step 4:** 单测：Parts 往返、无 ImageBytes 出现在 JSON 持久化形状（只序列化 ImageRef）。

---

### Task 3: ImageSanitizer + Compact 旧图降级

**Files:**
- Create: `internal/capabilities/llm/sanitize/image.go`, `image_test.go`
- Modify: compactor 相关（`internal/runtime/context/`）

- [x] **Step 1:** `SanitizeMessages(msgs, targetSupportsImage, targetAlias)`：不支持则把 Image part 换成占位 Text part。
- [x] **Step 2:** Compact：超出最近 N=2 轮含图消息时，ImageRef part → 文本 `[historical image ref: ..., omitted]`。
- [x] **Step 3:** 单测 strip / 保留 / compact。

---

### Task 4: ImageMaterializer + eino outbound 单点

**Files:**
- Create: `internal/capabilities/media/materialize/materializer.go`, `materializer_test.go`
- Modify: `internal/capabilities/llm/adapter/eino/`（message_convert 或新 `outbound.go`）
- Modify: LLM Generate/Stream 调用前（eino client 包装层）

- [x] **Step 1:** `Materializer.Open(ctx, ImageRef) ([]byte, mediaType, error)`：经 Backend/SessionFileReader；强制 size≤10MB、边长≤2048 下采样；`detail=low|high|auto` 影响缩放。
- [x] **Step 2:** `AssembleOutboundMessages`：**唯一** outbound 入口 = Sanitizer(target) → 对保留的 ImageRef Materialize → 填 eino/多模态块。持久化 Message **只含 ImageRef**。
- [x] **Step 3:** 所有 Chat/ToolCall/VisionExpert 请求必须走该入口（禁止旁路）。
- [x] **Step 4:** 单测：supports_image=false 时 outbound 无 image；超大图被拒或缩放。

---

### Task 5: view_image 工具（media）

**Files:**
- Create: `internal/capabilities/media/tool/view_image/tool.go`, `tool_test.go`
- Modify: bootstrap 注册；profile 暴露列表

- [x] **Step 1:** 入参 `candidate_id` | `path` | `detail`（`low|high|auto`，映射缩放预算）。
- [x] **Step 2:** 校验 MIME；非图 → `not_an_image`；禁绝对路径 / `/workspace` 物理路径。
- [x] **Step 3:** 通过 PathResolver / ProducedResource reader 取元数据，产出 **ImageRef**（非 bytes 回主存储）。
- [x] **Step 4:** Traits：`ReadOnly=true`，`ConcurrencySafe=true`（形态 B 专家调用由 Runtime 串行限流另控，默认并行读元数据 OK，max 并发在 Gateway/专家侧限 3）。
- [x] **Step 5:** 形态 C：若 ctx 带 `degraded_text` → 返回 `vision_unavailable`。
- [x] **Step 6:** 单测路径非法、非图、成功返回 ImageRef 结构。

---

### Task 6: VisionExpert + react_loop 集成

**Files:**
- Create: `internal/runtime/vision/expert.go`, `expert_test.go`
- Modify: `internal/runtime/strategy/react/react_loop.go`（tool result 组装）
- Modify: gateway 或 executeToolCalls 后处理

- [x] **Step 1:** `VisionExpert.Analyze(ctx, imageRef, prompt/checklist) (structured text, error)`：用 `router.vision` 模型；内部消息 Parts 含图；Trace span + TreeBudget/Estimator Usage 记账。
- [x] **Step 2:** `view_image` 工具本身**不** import LLM client；tool 只返回 ImageRef 载荷标记；**Runtime** 若 `expert_route` 则调用 Expert，把 tool result **改写为纯文本**再写入 Messages；bootstrap 在 `expert_route` 注入真实 ChatModel。
- [x] **Step 3:** `direct_inject`：tool result Message.Parts = text 元数据 + image part。
- [x] **Step 4:** 单测：三种 mode 下 tool result 形状（可用 fake LLM / fake mode ctx）+ Expert Usage。

---

### Task 7: QA 证据分轨（废除伪 visual passed）

**Files:**
- Modify: `internal/capabilities/artifact/contract/*`（QAPassRequest 增加 PolicyID/Validator/Status 语义）
- Modify: `internal/capabilities/artifact/service/qa_recorder.go`
- Modify: `internal/runtime/strategy/react/react_loop.go` `recordSkillQAEvidence`
- Create: checklist 解析小函数 + tests
- Modify: `completion` 若需要区分 visual_required / degraded

- [x] **Step 1:** `RecordPassed` 必须带明确 `Validator`：`content-qa/v1` | `render-proof/v1` | `visual-qa/v1`；禁止再用模糊 `skill-command:sha256:...` 写入 `visual-qa/v1`。
- [x] **Step 2:** skill 命令成功 → 仅按命令分类记 content 或 render_proof（可用命令名/脚本名启发式：`markitdown`→content，`thumbnail`/`pdftoppm`→render）；**绝不**记 visual-qa passed。
- [x] **Step 3:** visual-qa passed 仅当：形态 A checklist sign-off 解析成功，或形态 B Expert `passed:true` 且缺陷空；主模型文本 / Expert 回执自动 `RecordPassed(visual-qa/v1)`。
- [ ] **Step 4:** 形态 C：可 `RecordStatus(degraded/skipped, vision_unavailable)`。（Completion 侧已 fail-closed；显式 degraded 状态写入仍可后续补）
- [x] **Step 5:** Completion：若 `visual_required` 且无 `visual-qa/v1` passed（且策略非 degrade 放行），Run 不得 completed。
- [x] **Step 6:** 单测：markitdown 成功 ≠ visual-qa passed；缺 visual 时 fail-closed；checklist/expert JSON → visual-qa。

---

### Task 8: TurnInput / AttachmentDescriptor 内核

**Files:**
- Create: `internal/domain/attachment.go`, `attachment_test.go`
- Modify: `internal/domain/run.go` `StartRunRequest`
- Create: `internal/capabilities/turninput/`（classify MIME、build user message parts、doc text budget）
- Modify: `internal/app/run_service.go` 传递 Attachments

- [x] **Step 1:** 定义 role / source 枚举与 Descriptor 字段（对齐设计 §4.1）。
- [x] **Step 2:** `ClassifyMIME(mime) role`；image 白名单；office → document。
- [x] **Step 3:** `BuildUserTurnMessage(text, attachments, mode) (*Message, error)`：
  - `direct_inject`：image → ImageRef parts 进 user message；
  - `expert_route`：**不**把用户图注入主会话 Parts；改为文本占位 + 由 Runtime 在首轮前对每张图调 VisionExpert，把结论并入 user/tool 文本（与工具态形态 B 一致）；
  - `degraded_text`：不注入图，附件列表以文本声明 + `vision_unavailable` 提示；
  - document → extracted_text 截断（Extractor 接口，默认 `PlainOrEmpty`）；**永不**进 image part。
- [x] **Step 4:** 单测：图+docx+文本同轮，docx 不进 image part；expert_route 主消息无 Image part。

---

### Task 9: React Start 装配 + mention_resolve

**Files:**
- Modify: `react_loop.go` Start/loop 中 `CurrentUserMessage`
- Optional: mention hint helper

- [x] **Step 1:** 先解析 `EffectiveVisionMode`，再 `BuildUserTurnMessage(req.UserInput, req.Attachments, mode)`。
- [x] **Step 2:** 写入 RunContext（供 view_image / Expert）；`expert_route` 下对用户图先跑 VisionExpert 再开主 Loop。
- [ ] **Step 3:** `mention_resolve` 默认 off；hint / auto_attach 产品开关与 resolver（本期未做，见 Out of delivery）。
- [x] **Step 4:** 单测：无 Attachments 兼容旧文本；`direct_inject`+image attachment 时 Parts 含 ImageRef。

---

### Task 10: CLI `--attach` 与 TUI `@path`；Enterprise/Desktop 契约透传

**Files:**
- Modify: `products/cli/internal/command/run_cmd.go` / chat 发送路径
- Modify: `products/cli/internal/tui/chat/` 输入解析
- Create: `products/cli/internal/attach/` staging helper（本地文件 → Descriptor：sha256/mime/alias；**请求体不携带 bytes**）
- Modify: Enterprise/Desktop 若已有 StartRun DTO：增加 `Attachments` 字段透传至 `domain.StartRunRequest`（**不做**完整上传 UI）

- [x] **Step 1:** `--attach` 可重复；构建 `[]AttachmentDescriptor`。
- [x] **Step 2:** TUI：`@path` → Attachments；文本保留或剥离 token（实现时选一种并测稳）。
- [x] **Step 3:** app.RunRequest / Enterprise DTO 增加 Attachments 透传，保证三端**契约同一**。
- [x] **Step 4:** 单测 attach 分类与路径。

---

### Task 11: Bootstrap 接线 + 回归测试

**Files:**
- Modify: `products/cli/bootstrap/container.go`（及 desktop/enterprise 共享 builder 若有）
- Modify: default profile 工具列表加入 `view_image`

- [x] **Step 1:** 注册 media view_image；注入 PathResolver / ProducedResourceStore（leased 过期检测）/ vision mode / VisionExpert。
- [x] **Step 2:** `go test` 覆盖触及的包；修复编译断裂（Message.Content 调用方）。
- [x] **Step 3:** 验证矩阵手工/单测对照 M15、T2、T5；leased 过期 → harness_bridge 引导。

---

### Task 12: 文档状态更新

**Files:**
- Modify: 两份设计文档状态行；本 plan checkboxes

- [x] **Step 1:** 设计文档状态改为「已实现（内核+CLI；见推迟项）」并列出推迟项。
- [x] **Step 2:** 本 plan 勾选完成项。

---

## 验证命令（Windows PowerShell）

```powershell
$env:GOCACHE='D:\workspace\go\genesis-agent\.gocache'
$env:GOMODCACHE='D:\workspace\go\genesis-agent\.gomodcache'
go test ./internal/domain/... ./internal/capabilities/llm/... ./internal/capabilities/media/... ./internal/runtime/vision/... ./internal/capabilities/artifact/... ./internal/runtime/strategy/react/... ./products/cli/...
```

---

## 风险与缓解

| 风险 | 缓解 |
|------|------|
| eino MultiContent API 差异 | 单点 AssembleOutbound；测不通则先 sanitize+仅文本降级并打 warning |
| ProducedResource reader 依赖不齐 | view_image path 模式先用 filesystem Backend；candidate_id 接现有 registrar port，缺则清晰错误 |
| 全量改 Message 调用点 | TextContent() 兼容；渐进改 eino 路径 |
| 范围过大 | 上文 Out of delivery 已切边；内核验收不依赖 Desktop/Enterprise UI |

## Plan review-fix-rereview 记录

### Cycle 1（计划审查）

从第一性原理：两文档要交付的是「配置真相源 + Parts 流水线 + 看图原语/专家路由 + QA 分轨 + TurnInput 三端契约」。UI 上传页不是内核正确性前置条件。

| 发现 | 处理 |
|------|------|
| 形态 B 下用户首轮附图未写清 | 已修 Task 8/9：expert_route 不注入主会话，先 Expert |
| bytes 读取点分散风险 | 已加 Materializer + AssembleOutbound 单点（Task 4） |
| Completion fail-closed 不足 | 已补 Task 7 Step 5 |
| Desktop/Enterprise 仅推迟会丢契约统一 | Task 10 改为 DTO 透传 Attachments |
| 完整 OOXML/自动重渲染/Web 上传页 | 保留为 Out of delivery（不阻塞验收子集） |

### Cycle 2（计划再审查）

再读后：追溯矩阵覆盖 M1–M15、T1–T10；推迟项与设计非目标不冲突；无新的 actionable 缺口。计划可进入实现。

## Implementation review-fix-rereview

### Cycle 1（实现审查）

| 发现 | 处理 |
|------|------|
| 形态 A 无真正 Provider 多模态出站 | 已补 Materializer + eino `UserInputMultiContent` |
| Compact 旧图未接线 | 已在 react 发模前 `CompactHistoricalImages(N=2)` |
| import 名与 multiagent/sanitize 冲突 | 已改 `imagesanitize` |
| VisionExpert LLM 未完整注入 | 标为 Cycle 3 行动项 |

### Cycle 2（实现再审查）

关键验收子集（supports_image 门控、Parts 无 bytes、view_image、TurnInput CLI、伪 visual QA 废除）已测通过。

### Cycle 3（补齐 VisionExpert / visual-qa / leased bridge）

从第一性原理：形态 B 必须有真实视觉模型路径与可观测记账；visual-qa 只能由可解析断言放行；leased 过期不能静默失败，必须可恢复引导。

| 发现 | 处理 |
|------|------|
| Expert 未注入 bootstrap / react 仍占位 | `buildVisionExpert` + `WithVisionExpert`；tool result 改写走真实 `Analyze`；首轮用户图 expert_route 预分析 |
| Usage/Trace 缺失 | Expert span + Estimator + TreeBudget.Consume |
| Checklist 不自动 Record | 主模型文本 + Expert JSON → `RecordPassed(visual-qa/v1)` |
| leased 过期无引导 / 无 store | CLI `WithProducedStore`；`[harness_bridge]` + SkillFollow 优先 thumbnail 命令 |
| plan 勾选过量 | 回退未做项：`mention_resolve`、形态 C RecordStatus |

验证：`go test` react / vision / media / cli bootstrap 通过。

### Cycle 4（再审查）

再读后：无新的阻塞性缺口。Residual：静默自动重跑 thumbnail（无 Agent）、mention_resolve、docx OOXML、Desktop/Enterprise UI、形态 C 显式 degraded RecordStatus。

### Cycle 5（全量补齐 P0/P1 + 产品最小上传）

| 发现 / 交付 | 处理 |
|-------------|------|
| Materializer detail 缩放 + 占位 | 已落地 |
| candidate_id Reader 物化 + ephemeral | 已落地 |
| Enterprise Attachments 透传 + `POST /v1/files` | 已落地 |
| document_extract / mention_resolve / docx+pdf Extractor | 已落地 |
| cap=3 + morph C RecordDegraded | 已落地 |
| 三端 view_image + 最小上传 UI | 已落地 |
| Residual（保持非目标） | 静默无 Agent 自动重跑 thumbnail；Wails 完整 UI；StartRun base64（禁止） |
