# 用户 Turn 输入与附件契约设计

> 状态：**已实现（内核 + CLI）** — 2026-07-19；推迟项见文末「实现状态」  
> 实现计划：[`superpowers/plans/2026-07-19-multimodal-and-turn-input.md`](./superpowers/plans/2026-07-19-multimodal-and-turn-input.md)  
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

1. 上传/选文件 → 换稳定 ID，再进对话协议（§4.2）。  
2. **图片 ≠ 任意文件**：image → 视觉；docx/pdf/xlsx → 文本抽取或工具读 staged 文件。  
3. 附件清单与「进模型的多模态 parts」可以双轨（清单更宽，parts 更窄）。  
4. 禁止把用户电脑绝对路径作为跨产品/跨 Run 协议字段。  
5. 跨端与持久化只传标识；字节仅在 Provider outbound 短暂出现。

### 2.4 本地 Coding Agent 对照（Codex / Kode-CLI）

网页产品（§2.1–2.2）偏「显式附件即消费」；本地 Coding Agent 偏「路径进上下文 + 工具按需读」。Genesis CLI/Desktop 必须同时能解释这两种锚点。

| 维度 | Codex | Kode-CLI | Genesis 采纳 |
|------|-------|----------|--------------|
| 显式贴图 / `--image` / 剪贴板图 | 首轮前读成 data URL → `input_image` | 剪贴板图 → user message `image` blocks | **同**：E1 图片首轮进视觉 Parts（或形态 B Expert） |
| `@路径` 指向图片 | TUI 识别扩展名则 attach 为图；否则当路径文本 | **不**预读；`<system-reminder>` 要求模型用 `Read` | **E1**（`--attach`/`@` 显式绑定）走视觉；**E2** 文本点名默认 `view_image` |
| 文档 / 普通文件 | **无** Document 附件类型；路径写入 Text，模型用 shell/exec 等读取 | `@file` → reminder → 模型 `Read`；docx/xlsx 等二进制 **Read 拒绝** | **契约保留** `role=document`；默认策略见 §4.5 / §7 |
| `extracted_text` 预抽 | **无** | **无**（旧「@file 内联全文」路径已从 turn 移除） | **可选**（对齐 Claude，非对齐 Codex/Kode）；见 §7 |
| 客户端请求次数 | 一次 turn | 一次 turn | 一次 `StartRun`（抽取若发生，在同 Run 内、首次 LLM 前） |

要点：

1. Codex / Kode **都不**在「用户提交一轮」之外再开第二次「解析 API」；图的字节装配是同一次 turn 的同步预处理。  
2. 二者对**文档默认不预抽正文**——首轮往往只有路径/提醒，正文靠工具。这与 Claude 网页的 `extracted_content` 不同。  
3. Genesis：**图片对齐 Codex**（显式附件首轮进视觉）；**文档预抽对齐 Claude、可关闭以对齐 Codex/Kode**（Profile，见 §7.2）。

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

### 4.2 标识优先：上传换 ID，协议不传字节（业界硬约定）

业界对用户附件的主流形态是：

```text
上传 / 选文件 → 文件存储（对象存储 / Files API / staging）
            → 返回不透明 file_id（或 InputRef）
对话 / StartRun / 会话持久化 → 只携带标识与元数据
真正访问大模型前的一瞬间 → 服务端用标识取字节（或换成厂商已托管的 file）→ 缩放 → 写入 Provider Body
```

| 产品 / 形态 | 协议里传什么 | 字节何时出现 |
|-------------|--------------|--------------|
| ChatGPT 网页 | `sediment://file_...` 等指针 + attachments 元数据 | 服务端持有；请求不带大 base64 |
| Claude 网页 | `files[]` UUID；文档另可带 `extracted_content` | 同上；正文预览是文本不是原文件字节 |
| OpenAI / Anthropic API | 先 `files.upload` 得 `file_id`，再在 messages 里引用 | Provider 侧托管；调用侧传 id |
| Codex 本地 | `LocalImage{path}` / 工具 `path` | 本机路径作「本地 staging」；进模型前才变 data URL |
| **Genesis** | `AttachmentDescriptor.id` + `InputRef` / `ImageRef` | 见下表分层 |

**Genesis 分层硬规则：**

| 层 | 允许携带 | 禁止 |
|----|----------|------|
| 浏览器 / App → `POST /files` | multipart 或 JSON；常用文档/图片/音视频/压缩包 | `.exe`/`.dll`/未知二进制 |
| 浏览器 / App → `StartRun`（发消息） | Descriptor id / `input_ref`；**仅图片**可带 `content_base64`（入站立即 staging 并清空） | 文档/音视频/压缩包的 base64；宿主绝对路径 |
| Session / Message 持久化 | `ImageRef` / attachment id | 原始像素、data URL、残留 `content_base64` |
| Provider Adapter outbound | 短暂打开 → 缩放 → data URL | 把 outbound 字节写回 DB / SSE |

**时序：**

```text
路径 A（推荐，大文件/文档）：
  POST /files (multipart) → id → StartRun{attachments:[{id}]}

路径 B（发消息时内联图片，仅 jpeg/png/webp/gif）：
  StartRun{attachments:[{name, mime, content_base64}]}
  → 服务端 staging → 清空 content_base64 → 后续同路径 A
```

验收：Message Store / 落盘 JSON **不得**残留 `content_base64`；文档类不得通过 StartRun 内联 base64。

**CLI / Desktop 本地特例（不等同于「协议传路径当永久真相」）：**

- `--attach` / 拖拽可先落本机 staging 或直接持有可读路径，生成 `AttachmentDescriptor`（含 `id` + `workspace_alias`，`LocalPath` 仅进程内 `json:"-"`）。
- **上云桥接 / 多实例 Resume** 前必须先 stage 成可跨端的 `InputRef`，不得把 `D:\...` 写进跨端 Body。
- 语义上仍是「先有标识，再在出站读字节」；本地路径只是 staging 实现细节。

**与「轻量抽取」的关系（避免误解）：**

- 可选的 `extracted_text` 是**文本预览**，不是把原文件塞进协议。
- 抽取若发生，仍在同一次 StartRun 内、首次 LLM 前完成（§7.1）；客户端无「先解析 API、再开聊」的第二次对话请求。
- 原文件始终通过标识在存储侧可再次打开（工具深读 / 再物化）。

验收要点：任意抓包会话落盘 JSON，**不得**残留附件 `content_base64`；StartRun 仅允许**图片**内联 base64（入站即清空）；文档等须先 `/files`。调模型的 HTTP Body 可短暂含 data URL，但该字节不得回写 Message Store。

### 4.3 分流规则（硬性）

| role / MIME | 进入视觉 Parts？ | 默认处理 |
|-------------|------------------|----------|
| `image/*`（jpeg/png/webp/gif…） | **是**（形态 A）；形态 B 走 VisionExpert | 首轮自动注入 image part |
| 办公文档（docx/xlsx/pptx/pdf…） | **否** | staged path 必有；`extracted_text` 按 §7.2 Profile 可选；深读靠工具/Skill |
| 其它（zip/bin…） | **否** | 仅 staging；模型用工具，不伪造成 image |

`view_image` **只**服务图片（及明确支持的视觉媒体）。对 docx 调用必须返回结构化错误（如 `not_an_image`），并提示走文档通道。

### 4.4 与 ChatGPT / Claude 字段对照

| 语义 | ChatGPT | Claude | Genesis |
|------|---------|--------|---------|
| 用户文本 | `parts` 中字符串 | `prompt` | `TurnInput.text` |
| 图片 | `image_asset_pointer` | `files[]` | `attachments[role=image]` → ContentPart |
| 文档清单 | `metadata.attachments` | `attachments[]` | `attachments[role=document]` |
| 文档正文 | （服务端另路） | `extracted_content` | 可选 `extracted_text` +/或工具读 path |
| 不透明指针 | `sediment://file_...` | file UUID / 沙箱 path | `InputRef` / `ImageRef`（§4.2） |

### 4.5 显式附件：系统分流 vs 模型选工具

**规则（一句话）**：附件类型决定入口通道（系统确定性分流）；任务深度决定是否再调工具（模型探索）。

| role | 系统在首次 LLM 前必须做的 | 留给模型 / Skill 的 |
|------|---------------------------|---------------------|
| `image`（E1） | staging + ImageRef；形态 A 注入 Parts / 形态 B 调 VisionExpert | 通常无需再 `view_image`；工作区另图仍可按需 view |
| `document`（E1） | staging + `workspace_alias`；按 Profile **可选**轻量 `extracted_text`（有预算） | 深读、分页、编辑、QA、结构化表 → `read_file` / markitdown / Skill |
| `other` | 仅 staging + 清单提示 | 解压、专用工具；禁止伪造成 image/text part |

禁止：

- 把 docx/pdf 当 image part。  
- 在首轮无预算地全量解析大文档（堵 TTFB、爆上下文）。  
- 要求用户先调「解析接口」再开对话（见 §7.1）。  
- 在跨端协议或 Message 持久化中携带附件原文件 bytes / 大 base64（见 §4.2）。

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

```text
multipart / 对象存储上传
  → 返回 file id（§4.2 步骤 1–2）
  → StartRun.attachments 只带 id + 元数据（步骤 3）
  → 控制面 staging / InputRef 绑定租户与审计
```

禁止把上传响应里的临时下载 URL 或宿主路径当作跨 Run 永久真相；租户隔离与审计挂在 `Descriptor.id` / `InputRef` 上。

### 5.4 与 SSE / HTTP 的关系

- **输入**：`POST StartRun`（或等价）Body 携带 `TurnInput`（仅标识与元数据）；因此不能用仅 GET 的原生 `EventSource`（SSE 文档已说明原因）。大文件必须先走上传换 id（§4.2），不要塞进 SSE。
- **输出**：仍只遵循 SSE 事件规范；本文不扩展 `block.*`；SSE **不**回传附件原文件。

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

## 7. 装配流水线与轻量抽取时序

### 7.1 客户端一次请求 ≠ 后端零预处理

用户侧始终是**一次**进入对话（CLI 一次 run / Enterprise 一次 `POST StartRun`）。  
若开启文档轻量抽取，抽取发生在**同一次 Run 内部、第一次 LLM Generate 之前**，是同步装配步骤，**不是**第二次用户对话请求。

```text
【客户端】文本 + 附件（或已上传 file id）
        │  一次 StartRun / run
        ▼
【后端同 Run】
  1. 校验 MIME / size / role
  2. staging → InputRef + workspace_alias
  3. role=image（E1）：准备 ImageRef（形态 A Parts / 形态 B 预跑 Expert）
  4. role=document（E1）且 Profile 允许预抽：
       Extractor.Extract(path) → extracted_text（截断到预算，失败则降级为仅 path 提示）
  5. BuildUserTurnMessage → 写入 Messages
  6. Per-Request ImageSanitizer(targetModel)
  7. 第一次 LLM Generate / Stream  → 之后才是工具 Loop
```

易混淆点：

| 现象 | 解释 |
|------|------|
| 内部「先抽再调模型」 | 同 Run 顺序两步，延迟叠在首包 TTFB |
| Enterprise 先 upload 再 StartRun | upload 只换 file id；抽取仍挂在 StartRun，不是第二轮聊天 |
| Codex/Kode 文档无预抽 | 步骤 4 跳过；首轮只有路径/提醒，正文靠工具（§2.4） |

Extractor 约束：快、有预算、可失败降级；OCR / 整本 PDF / OOXML 全量等重活**不得**默认同步堵死首轮，应留给工具或异步任务。

### 7.2 文档预抽 Profile（对齐哪条锚点）

| `document_extract`（建议名） | 行为 | 对齐 |
|------------------------------|------|------|
| `preview`（Enterprise 默认推荐） | E1 文档同步抽预算内文本进本轮 user 上下文 + 保留 path | Claude `extracted_content` |
| `path_only`（CLI Coding 默认推荐） | E1 文档只 staging + 路径/清单提示，不预抽正文 | Codex / Kode-CLI |
| `off` | 与 path_only 同（显式关闭） | — |

图片通道**不受**此开关影响：E1 图片始终按视觉形态首轮处理（对齐 Codex）。

当前实现：明文 txt/md/csv 可预填 `ExtractedText`；docx/pdf 完整 Extractor 仍为后续项。落地 Extractor 时必须尊重本开关与 `MaxExtractedTextBytes` 预算。

### 7.3 与多模态 / 产物图的边界

```text
TurnInput
  → §7.1 装配
  → Provider Adapter
```

工具侧产生的图（如 office-ppt QA thumbnail）**不**走本 Turn 的用户 attachments，仍走 `view_image` + leased ProducedResource（见多模态设计）。

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
4. **DocumentExtractor**（docx/pdf 等）与 `extracted_text` 预算；落地 `document_extract` Profile（§7.2）。  
5. **Desktop** 拖拽/选择对齐同一适配器接口。  
6. （可选）`mention_resolve=auto_attach` 产品开关。

---

## 10. 验收标准

1. 同轮「文本 + 图 + docx」：图进视觉 Parts（或形态 B 专家结论），docx **从不**进 image part；至少有 staged path，预抽与否服从 §7.2。  
2. 持久化与跨端 Body **无**原始 image bytes / 大 base64；符合 §4.2「标识优先」。  
3. CLI `@a.png` 与 Enterprise 上传同一 `role=image` 语义；Enterprise 路径为「先上传得 id，再 StartRun」。  
4. 用户仅说「描述下 111.png 的内容」且未附件：默认**不**首轮注入像素；通过 `view_image`（或 hint 后 view）完成。  
5. SSE 文档无需承载输入 schema；`StartRun` POST Body 能完整携带 `TurnInput`（仅标识）。  
6. 文档预抽若开启：发生在同一次 StartRun 内、首次 LLM 前；客户端无「先解析再对话」的第二次请求。  
7. `document_extract=path_only` 时行为对齐 Codex/Kode：E1 文档不预抽正文。  
8. Provider outbound 可读字节；该字节不得回写 Message Store / SSE 业务载荷。

---

## 11. 文档分工

| 文档 | 职责 |
|------|------|
| **本文** | 用户 Turn 输入、附件、三端适配、文本点名策略 |
| 多模态输入与视觉能力设计 | Parts、Sanitizer、`view_image`、视觉 QA |
| 统一执行工作空间… | InputRef、staging、禁止宿主绝对路径 |
| SSE 与重试策略 | **仅输出流**；可链接本文作为输入契约 |
| agent loop 设计 | `StartRunRequest` 总览字段 |

---

## 12. 实现状态（2026-07-19）

| 项 | 状态 |
|----|------|
| `AttachmentDescriptor` + `StartRunRequest.Attachments` + `app.RunRequest.Attachments` | 已落地 |
| MIME 分流 + `BuildUserTurnMessage`（含 expert_route 不注入图） | 已落地 |
| CLI `--attach` + TUI `@path` | 已落地 |
| 文本点名默认不注入（E2） | 已落地（靠 `view_image`；默认 `mention_resolve=off`） |
| §4.2 标识优先 | 已落地：`POST /files` 白名单（文档/图/音视频/压缩包）；**发消息/StartRun 仅图片**可带 `content_base64`（入站 staging 后清空） |
| `mention_resolve=hint/auto_attach` | 已落地（`Profile.TurnInput.MentionResolve`；默认 off；单测覆盖） |
| `document_extract` Profile（preview / path_only） | 已落地（`Profile.TurnInput.DocumentExtract`；Enterprise=preview，CLI/Desktop=path_only） |
| docx/pdf Extractor | 已落地（OOXML `document.xml` + PDF 文本层；失败降级 path） |
| Desktop 最小附件交互 | 已落地：`genesis-desktop run --attach`（Wails UI 仍待） |
| Enterprise Web 最小上传 | 已落地：agent-demo/qa Live API（选文件→`/v1/files`→Run attachments） |
| Codex / Kode 对照与同 Run 抽取时序 | **已写入** §2.4 / §4.5 / §7 |
