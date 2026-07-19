# 用户 Turn 输入与附件契约设计

> 状态：设计规范（对照 ChatGPT / Claude 网页抓包与业界多模态实践收束）  
> 范围：CLI / Desktop / Enterprise 的用户文本+附件如何进入 Runtime；与视觉流水线、工作空间 staging 的边界  
> 相关：[`多模态输入与视觉能力设计.md`](./多模态输入与视觉能力设计.md)、[`统一执行工作空间、文件权限与产物规范.md`](./统一执行工作空间、文件权限与产物规范.md)、[`agent loop设计-SSE与重试策略设计.md`](./agent%20loop设计-SSE与重试策略设计.md)、[`todo/cli desktop enterprise 三端统一设计.md`](./todo/cli%20desktop%20enterprise%20三端统一设计.md)

---

## 1. 问题与目标

### 1.1 缺口

1. SSE 规范只覆盖**输出流**；`StartRunRequest.attachments` 在架构文档中有名，**无输入契约**；当前代码 `domain.StartRunRequest` 仅有 `UserInput string`。
2. 多模态设计规定了 Parts / Sanitizer / `view_image`，但未规定**产品面如何采集文件**，以及**文档 vs 图片**如何分流。
3. 用户只说「描述下 111.png 的内容」、未点附件时，缺统一策略（自动注入 vs 工具按需看图）。

### 1.2 目标

- **统一内核契约**：三端进入 Engine 的结构相同（`TurnInput`）。
- **产品 UX 独立**：CLI / Desktop / Enterprise 只负责「采集 → staging → 填满 TurnInput」。
- **分流正确**：图片走视觉通道；办公文档走抽取/工具通道；禁止把 docx 当成 image part。
- **协议不传大字节**：会话与跨端只传不透明 ID / `InputRef`，字节仅在调 Provider 时读取。

### 1.3 非目标

- 不规定 SSE 事件形状（仍以 SSE 文档为准）。
- 不在本文展开 Provider 各家 `image_url` / Anthropic `image` 编码细节（见多模态设计）。
- 不强制 CLI 第一期实现剪贴板贴图。

---

## 2. 业界锚点（ChatGPT / Claude）

### 2.1 ChatGPT 网页（同轮：图 + docx + 文本）

| 槽位 | 形态 | 含义 |
|------|------|------|
| `content.content_type` | `multimodal_text` | 多模态 user 消息 |
| `content.parts` | `image_asset_pointer`（`sediment://file_...`）+ 文本字符串 | **模型直接消费**：图指针 + 文 |
| `metadata.attachments` | 图 + docx 元数据（id / mime / name / size） | **本轮绑定清单**；docx **不进** parts |

要点：请求中**无图片/文件字节**；文档不进入视觉 parts。

### 2.2 Claude 网页（同轮：图 + docx + 文本）

| 槽位 | 形态 | 含义 |
|------|------|------|
| `prompt` | 纯文本字符串 | 用户话术 |
| `files` | UUID 列表 | 媒体/文件不透明 ID（图走此槽） |
| `attachments` | docx：`extracted_content` + 沙箱 `path`（如 `/mnt/user-data/uploads/...`） | 文档：**预抽文本** + 可工具访问路径 |

要点：文档默认**抽文本进上下文**；路径是受控上传区，不是宿主机绝对路径。

### 2.3 共同原则（Genesis 必须遵守）

1. 上传/选文件 → 换稳定 ID，再进对话协议。  
2. **图片 ≠ 任意文件**：image → 视觉；docx/pdf/xlsx → 文本抽取或工具读 staged 文件。  
3. 附件清单与「进模型的多模态 parts」可以双轨（清单更宽，parts 更窄）。  
4. 禁止把用户电脑绝对路径作为跨产品/跨 Run 协议字段。

---

## 3. 分层：独立性与统一性

```text
┌────────────────────────────────────────────────────────────┐
│ 产品 UX（各自独立，不得泄漏进 Engine）                          │
│  CLI: 输入框文本 + @path / --attach                            │
│  Desktop: 拖拽 / 文件选择 / 剪贴板（可选）                       │
│  Enterprise: 上传 / 对象存储 / 项目资源选择                      │
└────────────────────────────┬───────────────────────────────┘
                             ▼ products/*/bootstrap 适配器
┌────────────────────────────────────────────────────────────┐
│ 统一 TurnInput（三端同一契约）                                  │
│  text + images[] + documents[] +（可选）other_files[]         │
└────────────────────────────┬───────────────────────────────┘
                             ▼ 控制面 staging
┌────────────────────────────────────────────────────────────┐
│ InputRef / ResourceRef + WorkspaceViewManifest 相对别名        │
└────────────────────────────┬───────────────────────────────┘
                             ▼ Runtime
┌────────────────────────────────────────────────────────────┐
│ image → ContentPart(ImageRef) → Sanitizer → Provider         │
│ document → Extractor 文本块 和/或 工作区 path 供工具读取         │
│ 未显式附件、仅文本点名 → 见 §6（默认不静默注入像素）              │
└────────────────────────────────────────────────────────────┘
```

| 层 | 是否统一 | 说明 |
|----|----------|------|
| 选文件 UX | 否 | 三端交互不同 |
| `TurnInput` | **是** | Engine 唯一入口 |
| `InputRef` staging | **是** | 见工作空间规范 |
| Parts / Sanitizer / `view_image` | **是** | 见多模态设计 |

---

## 4. 统一契约：`TurnInput`

### 4.1 逻辑结构（目标）

```text
TurnInput
  text: string                         # 用户自然语言（可空，若仅附图）
  attachments: []AttachmentDescriptor  # 本轮显式绑定的全部资源
```

```text
AttachmentDescriptor
  id: string                 # 稳定 ID（staging 后生成；跨端唯一真相）
  name: string               # 展示名，如 111.png / 文档2.docx
  mime: string               # 服务端校验后的 MIME（不盲信客户端）
  sha256: string
  size: int64
  role: image | document | other
  source: upload | workspace | clipboard | project_picker | cli_attach
  # 以下按 role 可选：
  width, height              # role=image
  input_ref: InputRef        # staging 结果
  extracted_text: string     # role=document 可选预抽取（对齐 Claude；宜有长度上限）
  workspace_alias: string    # 投影给模型的相对路径，如 uploads/文档2.docx
```

进入 `domain.StartRunRequest` 的演进目标：

```text
StartRunRequest
  ...现有字段...
  UserInput: string          # = TurnInput.text（过渡期可并存）
  Attachments: []AttachmentDescriptor
```

持久化 Message 时：只存 `ImageRef` / attachment id，**不存**原始 bytes / 大 base64（与多模态设计一致）。

### 4.2 分流规则（硬性）

| role / MIME | 进入视觉 Parts？ | 默认处理 |
|-------------|------------------|----------|
| `image/*`（jpeg/png/webp/gif…） | **是**（形态 A）；形态 B 走 VisionExpert | 首轮自动注入 image part |
| 办公文档（docx/xlsx/pptx/pdf…） | **否** | 预抽取文本（可选）+ staged path；模型用文本或 Skill/工具 |
| 其它（zip/bin…） | **否** | 仅 staging；模型用工具，不伪造成 image |

`view_image` **只**服务图片（及明确支持的视觉媒体）。对 docx 调用必须返回结构化错误（如 `not_an_image`），并提示走文档通道。

### 4.3 与 ChatGPT / Claude 字段对照

| 语义 | ChatGPT | Claude | Genesis |
|------|---------|--------|---------|
| 用户文本 | `parts` 中字符串 | `prompt` | `TurnInput.text` |
| 图片 | `image_asset_pointer` | `files[]` | `attachments[role=image]` → ContentPart |
| 文档清单 | `metadata.attachments` | `attachments[]` | `attachments[role=document]` |
| 文档正文 | （服务端另路） | `extracted_content` | 可选 `extracted_text` +/或工具读 path |
| 不透明指针 | `sediment://file_...` | file UUID / 沙箱 path | `InputRef` / `ImageRef` |

---

## 5. 产品适配器（UX 独立）

### 5.1 CLI（第一期推荐）

| 方式 | 行为 |
|------|------|
| 输入框文本 | → `TurnInput.text` |
| `--attach path`（可重复） | 解析 path → staging → `attachments[]` |
| TUI `@path` / `@111.png` | 与 `--attach` 同语义（显式绑定） |
| 剪贴板贴图 | 第二期可选 |

规则：

- 相对路径相对当前 workspace / cwd，经 PathResolver；禁止把未 staging 的宿主绝对路径写入跨端请求。
- 本地 Run：可生成 `authority=host` 的 ResourceRef 再投影别名。
- 上云 / Enterprise 桥接：必须先 stage，再传 ID（见三端统一设计 §6.4）。

### 5.2 Desktop

拖拽、文件选择器、（可选）剪贴板 → 同一 `AttachmentDescriptor`；不向 Engine 传递 Wails/OS 私有句柄。

### 5.3 Enterprise

multipart / 对象存储上传 → file id → staging → 同一契约；租户隔离与审计挂在 Descriptor.id 上。

### 5.4 与 SSE / HTTP 的关系

- **输入**：`POST StartRun`（或等价）Body 携带 `TurnInput`；因此不能用仅 GET 的原生 `EventSource`（SSE 文档已说明原因）。
- **输出**：仍只遵循 SSE 事件规范；本文不扩展 `block.*`。

---

## 6. 文本点名未附件：`描述下111.png的内容`

这是与「显式上传/ @attach」不同的路径，必须单独规定。

### 6.1 场景分类

| 场景 | 用户行为 | 文件是否在工作区 | 处理 |
|------|----------|------------------|------|
| **E1 显式附件** | `@111.png` / `--attach` / UI 上传 | staging 后必有 | **首轮注入** image part（或形态 B 专家路由） |
| **E2 纯文本点名且文件可解析** | 「描述下 111.png 的内容」 | 工作区内唯一匹配 | **默认不静默注入像素**；由模型调用 `view_image(path=...)`（或 glob 后 view） |
| **E3 纯文本点名但歧义/不存在** | 同上 | 0 个或多个同名 | 不注入；模型 `glob`/`view_image` 失败或追问用户 |
| **E4 点名的是文档** | 「总结下 报告.docx」 | 可解析 | 不走视觉；`read_file` / 文档抽取 / Skill |

### 6.2 为什么 E2 默认不自动注入

1. **成本与隐私**：文件名误匹配会把无关大图塞进上下文。  
2. **歧义**：多目录下多个 `111.png`。  
3. **与业界一致**：ChatGPT/Claude 的视觉 parts 来自**显式附件**；工作区文件通常靠工具按需打开（对齐 Codex `view_image`）。  
4. **与 `read_file` 二进制门禁一致**：文本点名不能绕过「看图必须经视觉原语」。

### 6.3 E2 推荐运行时行为

```text
User: 描述下111.png的内容
  → TurnInput.text = 该句；attachments = []
  → 主模型（可带短 hint，见下）决定调用：
        view_image(path="111.png")   # 或先 glob
  → 形态 A：tool result 带 image part
  → 形态 B：VisionExpert 返回文本描述
  → 形态 C：vision_unavailable，引导用户改口或配置 vision
```

可选增强（Profile 开关，默认关）：

| 开关 | 行为 |
|------|------|
| `mention_resolve=off`（默认） | 无自动解析；全靠模型 + `view_image` |
| `mention_resolve=hint` | 若 basename 在工作区**唯一**命中，向本轮注入**文本 hint**（路径别名），仍不注入像素 |
| `mention_resolve=auto_attach` | 唯一命中则升格为 E1（自动进 attachments）。仅建议在可信本地 CLI/Desktop 开启 |

`hint` 示例（给模型，非给用户 UI 也可）：

```text
[workspace_hint] filename "111.png" uniquely resolves to "assets/111.png". Use view_image to inspect.
```

### 6.4 与显式附件的差异（验收用）

| | E1 显式附件 | E2 文本点名 |
|--|-------------|-------------|
| `attachments` | 含该图 | 空（除非 auto_attach） |
| 首轮是否已有 image part | 是（形态 A） | 否 |
| 期望工具 | 通常无需再 view（已在上下文） | 应 `view_image` |
| Token | 用户已选择承担 | 按需加载 |

---

## 7. 装配流水线（与多模态文档衔接）

```text
TurnInput
  → 校验 MIME / size / role
  → staging → InputRef + workspace_alias
  → 文档：可选 Extractor → extracted_text（截断）写入 user 文本块或独立 context 段
  → 图片（仅 E1 或 auto_attach）：ContentPart(ImageRef) 追加到本轮 user message
  → Per-Request ImageSanitizer(targetModel)
  → Provider Adapter
```

工具侧产生的图（如 office-ppt QA）**不**走本 Turn 的用户 attachments，仍走 `view_image` + leased ProducedResource（见多模态设计）。

---

## 8. 错误码（输入侧）

| 码 | 含义 |
|----|------|
| `attachment_too_large` | 超过产品/租户大小限制 |
| `attachment_mime_denied` | MIME 不在允许列表 |
| `attachment_stage_failed` | staging / 上传失败 |
| `not_an_image` | 对非图片调用了 `view_image` |
| `mention_ambiguous` | 文本点名命中多个路径（hint/auto 模式） |
| `mention_not_found` | 文本点名无法解析（可选，多在工具错误中体现） |

---

## 9. 落地阶段

1. **契约类型**：`AttachmentDescriptor` + `StartRunRequest.Attachments`；文档与代码字段对齐。  
2. **Enterprise 上传 → staging → TurnInput**（Web 路径最完整，对齐 ChatGPT/Claude）。  
3. **CLI `--attach` + TUI `@path`**；默认 `mention_resolve=off` 或 `hint`。  
4. **DocumentExtractor**（docx/pdf 等）与 `extracted_text` 预算。  
5. **Desktop** 拖拽/选择对齐同一适配器接口。  
6. （可选）`mention_resolve=auto_attach` 产品开关。

---

## 10. 验收标准

1. 同轮「文本 + 图 + docx」：图进视觉 Parts（或形态 B 专家结论），docx **从不**进 image part；可有抽取文本或 staged path。  
2. 持久化与跨端 Body **无**原始 image bytes / 大 base64。  
3. CLI `@a.png` 与 Enterprise 上传同一 `role=image` 语义。  
4. 用户仅说「描述下 111.png 的内容」且未附件：默认**不**首轮注入像素；通过 `view_image`（或 hint 后 view）完成。  
5. SSE 文档无需承载输入 schema；`StartRun` POST Body 能完整携带 `TurnInput`。

---

## 11. 文档分工

| 文档 | 职责 |
|------|------|
| **本文** | 用户 Turn 输入、附件、三端适配、文本点名策略 |
| 多模态输入与视觉能力设计 | Parts、Sanitizer、`view_image`、视觉 QA |
| 统一执行工作空间… | InputRef、staging、禁止宿主绝对路径 |
| SSE 与重试策略 | **仅输出流**；可链接本文作为输入契约 |
| agent loop 设计 | `StartRunRequest` 总览字段 |
