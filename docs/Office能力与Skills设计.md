# Office 能力与 Skills 设计

## 一、核心结论

Office 能力不应拆成大量细粒度 Tool。Genesis 应把 Office 定位为一组可复用的领域工作流：

- **Skill 负责流程**：描述 Word、Excel、PPT、PDF 等任务应该如何完成。
- **Tool 负责入口**：提供 `Skill` 网关（加载技能）、文件读写、命令执行等少量稳定原语。Skill 名（如 `office-ppt`）**不是** Tool 名。
- **Execution Runtime 负责执行**：运行 Python、LibreOffice、PDF 引擎，或调度远程沙箱。
- **MCP 负责外部系统接入**：连接 Microsoft Graph、Google Drive、SharePoint、企业文档系统等。
- **Plugin 负责分发和组合**：把 Skills、MCP 配置、资源、脚本、UI 元数据打包成可安装单元。

一句话：Office 是领域工作流，不是工具清单。Skill 是主要承载形式，Tool 只是通用执行入口，Plugin 是安装和分发边界。Skill / Tool 协议边界以 `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md` 为准。

## 二、Skills、MCP、Plugins 的关系

### 2.1 Skill 是工作流包

Skill 描述“做什么、什么时候用、怎么做”。它通常包含：

- `SKILL.md`：触发条件、步骤、约束、验证方式。
- `references/`：格式规范、样例、模板说明。
- `scripts/`：可复用脚本，例如生成 docx、检查 xlsx、渲染 PDF 预览。
- `assets/`：模板、图片、样式资源。

Skill 不应该直接绕过系统权限读写文件或调用外部服务。它应通过 Genesis 已有 Tool 和 Execution 能力完成实际动作。

### 2.2 MCP 是外部能力协议

MCP 适合接入系统外部的数据、工具和授权能力，例如：

- Microsoft Graph：读写 OneDrive、SharePoint、Word Online、Excel Online。
- Google Drive / Docs / Sheets / Slides。
- 企业文档库、审批系统、ERP、CRM。
- 第三方格式转换、OCR、签章、归档服务。

MCP 不适合作为本地 Office 文件处理的默认执行层。本地 `.docx`、`.xlsx`、`.pptx`、`.pdf` 文件的处理，优先走 Execution Runtime 和沙箱。

### 2.3 Plugin 是可安装分发单元

Plugin 的价值不是替代 Skill 或 MCP，而是把一组相关能力打成一个“产品化安装包”。

一个 Office Plugin 可以包含：

- 多个 Skills：`office-word`、`office-excel`、`office-ppt`、`pdf-review`。
- 脚本资源：Python 脚本、模板、样式、校验规则。
- MCP 配置声明：例如可选依赖 `microsoft-graph`、`google-drive`。
- 权限和依赖声明：需要哪些工具、命令、外部连接、用户审批。
- UI 元数据：名称、图标、说明、默认提示词、入口展示。
- 版本和发布信息：方便安装、升级、禁用、回滚和企业审计。

因此：

- **Skill 是能力内容**。
- **MCP 是外部连接协议**。
- **Plugin 是安装、分发、治理和组合边界**。

没有 Plugin 也可以使用内置 Skill；但当 Office 能力需要分发给不同用户、团队或租户，并携带脚本、模板、MCP 依赖和 UI 信息时，就需要 Plugin。

## 三、Office 能力分层

```text
Agent Runtime
  -> Skill System
     -> office-word / office-excel / office-ppt / pdf-review
  -> Tool Gateway
     -> Skill / read_file / write_file / edit_file / run_command / list|read|search_skill_resources
  -> Execution Runtime
     -> Local Runner / Sandbox Runner / Cloud Runner
  -> Office Adapters
     -> python-docx / openpyxl / python-pptx / LibreOffice / PDF Engine
  -> MCP Adapters
     -> Microsoft Graph / Google Drive / Enterprise Document Service
```

职责边界：

| 层级 | 负责 | 不负责 |
| --- | --- | --- |
| Skill | Office 任务流程、约束、格式规范、验证步骤；经 `Skill(skill=...)` 加载 | 直接绕过权限执行；把自己注册成可调用 Tool |
| Tool | 稳定原子入口、审批、路径解析、沙箱调度 | Office 领域细节和复杂业务流程 |
| Execution Runtime | 运行脚本、命令、转换器、渲染器 | 决定业务流程 |
| Office Adapter | 具体格式读写和转换 | 用户意图理解和工具选择 |
| MCP | 外部系统授权和远程数据动作 | 本地文件处理的默认实现 |
| Plugin | 分发、安装、依赖声明、组合治理 | 单个任务流程细节 |

## 四、为什么不做大量 Office Tools

不建议注册如下工具：

```text
word.create_document
word.insert_heading
word.insert_table
excel.set_cell
excel.create_chart
ppt.add_slide
ppt.set_theme
pdf.extract_page
```

原因：

- 工具数量会快速膨胀，模型选择成本变高。
- Office 操作高度组合化，单个动作很难表达完整任务意图。
- 细粒度工具会把领域逻辑固化到 Tool 层，违背 Tool 只做稳定原语的原则。
- 不同产品的执行环境不同，本地、桌面、企业沙箱、云环境难以统一。
- 文件格式库变化快，把它们绑定为 Tool API 会增加兼容和维护负担。

更合适的方式是：

```text
用户任务
  -> 选择 office-word Skill
  -> Skill 读取模板和规则
  -> 使用 read_file / write_file / run_command
  -> 在执行环境中运行脚本或适配器
  -> 生成和校验 Office 文件
```

## 五、建议的 Office Skills

第一阶段建议提供四个内置 Skill：

| Skill | 用途 | 典型输入 | 典型输出 |
| --- | --- | --- | --- |
| `office-word` | 创建、改写、审阅 Word 文档 | 需求说明、模板、Markdown、资料 | `.docx`、结构化审阅报告 |
| `office-excel` | 表格清洗、统计、公式、图表 | `.xlsx`、CSV、业务规则 | `.xlsx`、图表、数据摘要 |
| `office-ppt` | 生成或调整演示文稿 | 大纲、品牌规范、图片素材 | `.pptx`、讲稿备注 |
| `pdf-review` | PDF 解析、审阅、摘要、转换辅助 | `.pdf`、审阅要求 | 摘要、结构化提取、转换结果 |

后续可以按业务场景增加更高层 Skill：

- `financial-report`：财务报告生成。
- `contract-review`：合同审阅和批注。
- `meeting-deck`：会议汇报 PPT。
- `bid-document`：投标文件编制。
- `invoice-audit`：发票和报销材料核验。

高层业务 Skill 可以复用底层 Office Skill 的脚本和约束，但不应把所有场景塞进一个巨大的 `office` Skill。

## 六、Skill 包结构

内置 Skill 可以放在 Skill 能力的 embedded source 中，具体目录以后续实现为准。建议逻辑结构如下：

```text
office-word/
  SKILL.md
  scripts/
    create_docx.py
    inspect_docx.py
    render_preview.py
  references/
    docx-style-guide.md
    validation-checklist.md
  templates/
    report-template.docx

office-excel/
  SKILL.md
  scripts/
    inspect_workbook.py
    transform_workbook.py
    validate_formulas.py
  references/
    spreadsheet-rules.md

office-ppt/
  SKILL.md
  scripts/
    create_deck.py
    inspect_deck.py
  references/
    presentation-style-guide.md

pdf-review/
  SKILL.md
  scripts/
    extract_pdf.py
    render_pages.py
  references/
    pdf-review-checklist.md
```

`SKILL.md` 示例：

```md
---
name: office-word
description: 生成、修改、检查 Word 文档。适用于用户要求创建 docx 报告、套用模板、整理 Markdown 到 Word、检查文档结构和样式的任务。
short-description: Word 文档处理
version: 0.1.0
allowed-tools:
  - read_file
  - write_file
  - edit_file
  - run_command
  - read_skill_resource
dependencies:
  tools:
    - type: tool
      value: run_command
      description: 运行受控脚本处理 Office 文件
    - type: command
      value: python
      description: 执行 python-docx 脚本
context: inline
model: inherit
products:
  - cli
  - desktop
  - enterprise
---

# 使用原则

1. 先确认输入文件、输出路径和格式要求。
2. 优先复用本 Skill 的脚本和模板。
3. 文件读写必须通过 Genesis 工具和路径权限。
4. 生成后必须检查文档结构、关键段落、表格、页眉页脚和可打开性。
```

## 七、执行策略

### 7.1 本地 CLI / Desktop

CLI 和 Desktop 可以优先使用本地执行：

- Python 库：`python-docx`、`openpyxl`、`python-pptx`、`pypdf`、`pdfplumber`。
- LibreOffice headless：负责 Office 与 PDF 的转换、打开性校验。
- Poppler 或等价工具：负责 PDF 渲染和页面预览。

这些执行动作应通过 `run_command` 进入 Execution 能力，继续复用审批、路径解析、超时、输出截断、并发锁和沙箱策略。

Office Skill 脚本的 sandbox profile 选择以 `docs/沙箱API对接与Profile选择规则.md` 为准：脚本来源是 Skill 只影响权限和审计，实际 profile 应按工作负载判定。只要脚本处理 `.docx`、`.xlsx`、`.pptx`、`.pdf`，或依赖 LibreOffice、Poppler、PDF/OCR/Office 库，就应优先归类为 `task_type=office`。普通结构化 Office/PDF 处理使用 `office-basic`；当 Word/PPT/Excel/PDF 中存在扫描页、截图、拍照内容、无文本层页面，且任务要求识别图片文字时，使用 `office-ocr`；只有纯通用脚本才使用 `skill-polyglot-basic`。

### 7.1.1 与 Anthropic Office Skills 的取舍

参考 `D:\workspace\go\go-project\anthropics-skills\skills\pdf`、`docx`、`pptx`、`xlsx` 后，Genesis 的取舍如下：

| 能力点 | Anthropic Skills 做法 | Genesis 当前做法 | 原因 |
| --- | --- | --- | --- |
| 触发条件 | 按文件类型和交付物强触发，描述非常细 | 保留强触发思想，但改成中文、通用模型友好的简短规则 | 避免长提示淹没上下文，降低非 Claude 模型误读概率 |
| PDF | 覆盖 pypdf、pdfplumber、reportlab、qpdf、pdftk、Poppler、OCR、表单脚本 | 已提供 inspect、文本抽取、表单字段列举、页面渲染脚本 | 保留高价值入口；复杂填表、加密、合并拆分后续扩展，避免一次性搬入过重脚本 |
| DOCX | 深度使用 docx-js、pandoc、OpenXML 解包/打包、comments、tracked changes、schema validator | 已提供 inspect 和 LibreOffice 转 PDF 入口，文档要求保留模板和验证闭环 | tracked changes/comments 很复杂，直接照搬容易和 Genesis 权限、作者名、审计不一致；后续应独立实现 Genesis 版 |
| PPTX | 强调视觉设计、模板编辑、缩略图、LibreOffice+Poppler 渲染 QA、pptxgenjs | 已提供 inspect/preview/thumbnail/`create_pptx.js`；`dependencies.runtime` 声明 pptxgenjs/Pillow；缺包走 `install_skill_dependencies`；`office-basic` 镜像包清单见沙箱文档 §10 | 通用模型更需要短流程和强验证；设计长指南后续可拆 reference，不塞满主 Skill |
| XLSX | 强调公式而非硬编码、LibreOffice 重算、公式错误 JSON、金融模型格式规范 | 已提供 inspect 和 recalc 脚本，保留公式重算和零错误原则 | 金融模型规范保留在验证清单方向；复杂 openpyxl 样例后续按场景拆 reference |
| 共享 office 脚本 | pack/unpack/validate/soffice helpers 和大量 schema | 当前不直接复制 schema 和 helper | 原脚本带专有许可且体积大；Genesis 应沉淀自有、可测试、可审计版本 |
| OCR | PDF 中强调扫描件 OCR | 扩展为内容/操作触发，Word/PPT/Excel 内嵌图片也可升级 `office-ocr` | 更符合真实 Office 文档，避免把 OCR 错误绑定到扩展名 |

结论：Anthropic 的成熟设计证明了“脚本 + 验证闭环 + Office 专用 runtime”是正确方向；Genesis 保留这个方向，但第一版选择更小、更结构化、更适合多模型和企业审计的 Skill。后续扩展脚本时，应优先补齐 `pack/unpack/validate/soffice` 自有实现，而不是直接复制长提示或专有脚本。

### 7.2 Enterprise

Enterprise 不应默认依赖宿主机本地 Office 环境。推荐路线：

- 通过 genesis-sandbox 或企业云沙箱运行 Office 脚本。
- 通过产品 bootstrap 注入沙箱 endpoint、credential、租户策略。
- 文件路径对业务层只暴露 workspace-relative path、sandbox path 或 resource id。
- 如果接入 Microsoft 365 / Google Workspace，则通过 MCP 或企业 connector 处理远程文档。

### 7.3 Cloud / Remote Runtime

远程 Runtime 适合处理：

- 大文件。
- 高风险文档。
- 企业受控模板。
- 需要专用字体、LibreOffice、OCR、签章、转换服务的任务。

远程 Runtime 的引入不应改变 Skill 和 Tool 的语义，只替换 Execution Adapter。

## 八、依赖与权限

Office Skill 可能声明三类依赖：

```yaml
dependencies:
  tools:
    - type: tool
      value: run_command
    - type: command
      value: python
    - type: command
      value: libreoffice
    - type: mcp
      value: microsoft-graph
```

治理规则：

- `tool` 依赖必须在当前产品 Profile 中启用。
- `command` 依赖需要 Execution policy 判定，并可能触发审批。
- `mcp` 或 `connection` 依赖需要连接授权和外部数据审批。
- 企业 Skill 加载、远程文档读取、模板访问必须带租户、项目、用户、角色和审计信息。
- Skill 不应因为声明了依赖就自动扩大工具权限；扩大权限必须经过 ToolGateway 和 Approval。

## 九、Plugin 分发策略

Office 能力可以先以内置 Skills 方式落地。等到能力稳定后，再提供 Office Plugin。

建议 Plugin 形态：

```text
genesis-office-plugin/
  .genesis-plugin/
    plugin.json
  skills/
    office-word/
    office-excel/
    office-ppt/
    pdf-review/
  assets/
    icons/
    templates/
  mcp/
    microsoft-graph.example.toml
    google-drive.example.toml
```

Plugin manifest 应描述：

- 插件名称、版本、说明。
- 包含哪些 Skills。
- 需要哪些工具和命令。
- 可选 MCP / connector 依赖。
- 支持哪些产品：CLI、Desktop、Enterprise。
- 默认启用策略和权限提示。
- 企业发布状态、签名、来源和审计信息。

Plugin 的定位：

- 对个人用户：一键安装 Office 能力包。
- 对团队：统一模板、规范、脚本和最佳实践。
- 对企业：通过租户、组织、项目维度发布受控能力。
- 对平台：把 Skills、MCP、资源和 UI 元数据作为一个版本化单元管理。

## 十、产品边界

遵守项目现有产品分发架构：

- `internal/capabilities/skill` 只保留通用 Skill 模型、服务、解析、注入和工具。
- `internal/capabilities/execution` 继续承载命令执行和会话。
- `internal/capabilities/filesystem` 继续承载文件系统抽象和路径权限。
- `products/cli/bootstrap` 决定 CLI 的本地 Skill source、执行 runner 和依赖策略。
- `products/desktop/bootstrap` 决定 Desktop 的本地 Skill source、GUI 管理和 native 适配。
- `products/enterprise/bootstrap` 决定企业 DB Skill source、RBAC、审计、沙箱 endpoint 和外部 connector。
- `shared/local` 只能放 CLI/Desktop 共享的本地主机适配，不放企业策略。

Office 相关逻辑不应反向 import `products/*`，也不应在 Tool 内直接 import 具体 Office 库或 Docker / Wails / DB 实现。

## 十一、阶段计划

### Phase 1：内置 Office Skills

- 新增 `office-word`、`office-excel`、`office-ppt`、`pdf-review` 四个 Skill。
- 每个 Skill 只提供 `SKILL.md`、基础脚本和验证清单。
- 复用 `Skill` 网关（）、`list/read/search_skill_resources`、文件工具和 `run_command`。协议见 `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`。
- 明确依赖声明和审批策略。

### Phase 2：Office 脚本与模板库

- 沉淀稳定脚本：文档检查、表格检查、PPT 检查、PDF 渲染。
- 建立模板复用规则。
- 增加脚本级测试数据和输出校验。
- 统一错误格式，便于 Agent 理解失败原因。

### Phase 3：Office Execution Adapter

- 抽象 Office 处理服务接口。
- 支持 Local、Sandbox、Cloud 三种执行后端。
- 将高频脚本沉淀为可复用 adapter，但仍不暴露大量 LLM Tool。

### Phase 4：Office Plugin

- 将稳定 Skills、脚本、模板、MCP 配置声明打包为 Office Plugin。
- 支持安装、禁用、升级、版本锁定和企业发布。
- Desktop 增加可视化插件和 Skill 管理入口。
- Enterprise 增加租户级发布、审批、审计和回滚。

## 十二、判断准则

新增 Office 能力时按以下规则判断落点：

| 问题 | 推荐落点 |
| --- | --- |
| 是一个可复用任务流程吗 | Skill |
| 是跨任务稳定原子能力吗 | Tool 或已有 Tool 扩展 |
| 是具体文件格式读写实现吗 | Execution Adapter / 脚本 |
| 是外部 SaaS 或企业系统接入吗 | MCP / Connector |
| 是一组能力、资源和依赖的分发包吗 | Plugin |
| 是产品特有安装、认证、UI 或租户策略吗 | products/<product>/bootstrap 或 products/<product>/internal |

最终原则：先用 Skill 承载领域流程，用通用 Tool 执行动作；只有当能力稳定、跨产品复用且接口边界清楚时，再沉淀为 adapter 或 plugin。

## 十三、代码实现对齐审计与后续执行计划

> 审计时间：2026-07-04  
> 审计方法：按 `$code-doc-alignment` 技能对本文档设计点与当前仓库实现进行对照。  
> 审计结论：当前仓库已经具备 Office Skills 落地所需的 Skill、文件系统、命令执行、审批、沙箱和 CLI Skill Marketplace 基础骨架；四个内置 Office Skill 与最小 JSON inspect 脚本已落地。后续重点是扩展脚本库、补齐 Office Adapter、Enterprise/Cloud 执行装配、二进制资源策略和 Office Plugin 分发。

### 13.1 审计范围

参考来源：

- `docs/Office能力与Skills设计.md`
- `docs/agent loop设计.md` 中 Skill Runtime、能力适用范围、目录边界相关设计
- `docs/项目目录与边界说明.md` 中产品边界和目录职责约束

已检查实现：

- Skill 模型、解析、服务、embedded source、host source 与资源读取：`internal/capabilities/skill/**`
- Skill 工具：`internal/capabilities/skill/tool/skill`（对外名 `Skill`）、`list_skill_resources`、`read_skill_resource`、`search_skill_resources`
- 协议边界：`docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`
- 文件系统工具与路径权限：`internal/capabilities/filesystem/**`、`shared/local/filesystem`、`shared/local/pathresolver`
- 命令执行与沙箱执行：`internal/capabilities/execution/**`、`shared/local/execution`、`shared/local/sandbox`
- CLI 产品装配：`products/cli/bootstrap/container.go`
- Desktop / Enterprise 产品装配：`products/desktop/bootstrap/execute.go`、`products/enterprise/bootstrap/container.go`
- Skill Marketplace / 本地安装：`internal/capabilities/package/marketplace/**`、`shared/local/skillmarket/**`、`products/cli/internal/skill/**`、`products/cli/internal/command/skill_cmd.go`

### 13.2 进度对照表

| 设计点 | 文档位置 | 当前实现证据 | 状态 | 差距与说明 |
| --- | --- | --- | --- | --- |
| Office 能力以 Skill 承载流程，而不是注册大量细粒度 Office Tool | 第一章、第四章、第五章 | 当前没有 `word.create_document` 等 Office 细粒度工具；工具层主要是 `read_file`、`write_file`、`edit_file`、`run_command`、`Skill` 等通用入口 | `[x] 已对齐` | 继续禁止把 `office-ppt` 等 skill 名注册为 Tool；误调用走 CollisionGuard（协议边界设计）。 |
| Skill 包支持 `SKILL.md`、`references/`、`scripts/`、`assets/` | 第二章、第三章、第六章 | `internal/capabilities/skill/adapter/embedded/source.go` 允许读取 `references`、`assets`、`scripts` 下的 UTF-8 文本资源；`shared/local/skill/source.go` 也支持本地 Skill root | `[/] 部分实现` | 资源读取底座可用，但当前 embedded source 对非 UTF-8 二进制资源不支持；模板 `.docx`、图片、字体等资产需要通过文件安装路径或后续二进制 resource 机制补齐。 |
| Skill frontmatter 支持依赖、工具、产品、上下文等声明 | 第六章、第八章 | `internal/capabilities/skill/parser/markdown.go` 解析 `allowed-tools`、`dependencies`、`products`、`context`、`model` 等字段；`internal/capabilities/skill/model/model.go` 有 `Dependencies`、`Policy.Products`、`AllowedTools` | `[x] 已实现` | 字段模型已具备。注意当前依赖声明主要用于加载时检查和提示，不会自动扩大工具权限。 |
| `Skill` 网关、`read_skill_resource`、`search_skill_resources` 可作为 Skill 入口 | 第一章、第四章、第十一章 | CLI bootstrap 注册 `Skill` 网关与资源工具；DescriptionFunc 挂 catalog；CollisionGuard 纠偏 | `[x] 已对齐` | Phase 1 已落地；mention/fork 属 Phase 2。 |
| CLI 可加载内置 Skill 与本地/安装 Skill | 第七章、第十章、第十一章 | `products/cli/bootstrap/container.go` 构造 embedded system source、本地 host source、安装 roots，并启动 watcher 清理缓存 | `[x] 已实现` | 这为 Phase 1 内置 Office Skills 提供了直接落点。 |
| 内置 `office-word`、`office-excel`、`office-ppt`、`pdf-review` 四个 Skill | 第五章、第六章、第十一章 Phase 1 | `internal/capabilities/skill/adapter/embedded/skills` 已包含四个 Office Skill；embedded source 单测覆盖发现和资源读取 | `[x] 已实现` | 当前是最小可用版本，后续应扩展脚本、模板和样例。 |
| Office 脚本库：docx/xlsx/pptx/pdf 检查、转换、渲染 | 第六章、第七章、第十一章 Phase 2 | 四个 Skill 已提供 `inspect_docx.py`、`inspect_xlsx.py`、`inspect_pptx.py`、`inspect_pdf.py`，统一输出 JSON | `[/] 部分实现` | 已有轻量检查脚本；尚未覆盖复杂生成、转换、渲染、批注、修订、表单填充、OCR 和样例测试数据。Go 仓库不应直接 import Office 库，复杂处理继续作为 Skill scripts 经 Execution Runtime 运行。 |
| `run_command` 通过 Execution Runtime 执行 Office 脚本并复用审批、路径解析、超时、锁和沙箱 | 第一章、第七章、第八章 | `run_command` 依赖 `ExecutionRunner`、`PathResolver`、`Approval`、`ResourceLocker`、`SandboxProfile`；CLI bootstrap 在外部 sandbox 模式下注入 genesis-sandbox HTTP runner，在本地平台模式下注入 local sandbox runner | `[/] 部分实现` | 通用执行链路和外部 sandbox 主路径已具备；仍缺 Office 依赖探测、命令白名单/策略模板、LibreOffice 可用性检查和 Office 专用错误解释。 |
| Local / Sandbox / Cloud 三类执行后端 | 第七章 | `internal/capabilities/execution/service/runner.go` 支持 disabled/optional/required sandbox；`shared/local/execution/sandbox_runner.go` 对接本地平台沙箱；`internal/capabilities/sandbox/adapter/http` 已实现 genesis-sandbox `CommandClient`；CLI 可按配置注入外部 sandbox runner | `[/] 部分实现` | Local 与外部 CommandRunner 主路径已有；sandbox 文件 backend、Enterprise/Cloud 装配、profile 健康检查和 Office 专属 service 尚未落地。 |
| Enterprise 不依赖宿主机本地 Office 环境，通过 bootstrap 注入沙箱/连接策略 | 第七章、第十章 | `products/enterprise/bootstrap/container.go` 当前只调用 shared builder，未注入 Skill source、Office execution、sandbox endpoint、RBAC、审计或外部 connector | `[ ] 未实现` | Enterprise 仍是薄装配，后续不能复用 CLI 的宿主机本地执行默认策略。 |
| Desktop 本地 Office 能力与 GUI 管理入口 | 第七章、第十章、Phase 4 | `products/desktop/bootstrap/execute.go` 明确返回 `genesis-desktop 暂未实现` | `[ ] 未实现` | Desktop 相关能力暂不具备，Office Plugin 可视化管理应后置。 |
| MCP 作为 Microsoft Graph、Google Drive、企业文档系统等外部连接协议 | 第二章、第八章 | 代码中有 credential / connection 基础服务，但未见 MCP Registry / MCP Adapter 或 Microsoft Graph / Google Drive connector 实现 | `[ ] 未实现` | 当前只能把 MCP 作为 Skill dependency 元数据表达，不能实际连接外部 Office SaaS。 |
| Plugin / Marketplace 安装、禁用、升级、来源治理 | 第二章、第九章、Phase 4 | `internal/capabilities/package/marketplace/service.go` 支持 marketplace add/list/update/remove、catalog、install、enable/disable、uninstall；CLI 命令也有 `skill install`、`skill marketplace`；安装 root 会纳入 CLI Skill source | `[/] 部分实现` | Skill Marketplace 已具备，不是完整 Plugin 系统。当前更接近“Skill package marketplace”，还缺 `.genesis-plugin/plugin.json`、MCP 配置声明、UI 元数据、企业发布、签名和回滚治理。 |
| 产品边界：Office 逻辑不反向 import `products/*`，Tool 层不直接 import Office 库、Docker/Wails/DB | 第十章 | 当前没有 Office 专属代码；现有 Skill、execution、filesystem、marketplace 基本在 `internal/capabilities` 和 `shared/local`，CLI 仅做装配 | `[x] 已对齐` | 后续新增 Office 代码时应保持：领域流程进 Skill，脚本走 Skill resources，产品差异只在 bootstrap 注入。 |
| 验证与测试 | 第六章、第十一章 | Skill parser、service、`Skill` 网关、collision、run_command、sandbox runner、skillmarket、HTTP sandbox adapter 和 embedded Office Skill 发现均有单测；CLI Profile/`allowed-tools` 对齐有校验 | `[/] 部分实现` | 协议单测已补；仍缺 Office 端到端样例与 CLI smoke。 |

### 13.3 差距反思

实现缺口：

- 最大缺口已经从“Office 专属内容资产缺失”转为“脚本深度和产品化验证不足”：四个内置 Skill、最小检查脚本和验证清单已具备，但复杂编辑/生成/渲染/OCR 还需要扩展。
- 当前 Skill resource 读取偏文本化，适合 `SKILL.md`、检查清单、脚本源码；不适合直接通过 `read_skill_resource` 读取 `.docx`、图片、字体等二进制模板。Phase 1 应先把模板作为可选文件路径或 marketplace 安装文件使用，Phase 2 再决定是否扩展二进制资源访问模型。
- `Skill` 能检查依赖，但依赖声明还不是 Execution policy 的强约束来源；Office 所需的 `python`、`libreoffice`、远程沙箱、MCP connector 仍需要明确的策略映射。
- 模型把 `office-ppt` 当 Tool 调用的协议缺口已由 Phase 1 CollisionGuard + `Skill` 网关修复；残留风险是模型可能多一轮才纠正（除非开启 auto_rewrite）。
- CLI 装配较完整，Enterprise 与 Desktop 明显落后。Office 能力若先做本地 CLI，要避免把 CLI 的本地宿主机假设泄漏到通用能力层。

文档缺口：

- 文档把 `Plugin` 与 `Skill Marketplace` 的关系写得偏理想化；当前实现是 Skill Marketplace，不是完整 Plugin。后续应在 `docs/产品分发架构设计.md` 或本文档 Phase 4 前置说明二者关系。
- 文档没有明确 binary assets 的处理策略。Office 模板、图片、字体、样例文件是否作为 Skill 包文件、外部 workspace 文件、marketplace assets，还是通过 resource id 暴露，需要在 Phase 2 前明确。
- 文档没有列出 Office 脚本的错误输出契约。为了让 Agent 能修复失败，应规定脚本输出 JSON 字段，例如 `ok`、`errors`、`warnings`、`artifacts`、`diagnostics`。

架构漂移风险：

- 如果急于把 `create_docx`、`set_cell` 等能力做成 Tool，会破坏本文档“Office 是领域工作流，不是工具清单”的原则。
- 如果在 Enterprise bootstrap 中直接复用 CLI 本地 runner，会违反企业不依赖宿主机 Office 环境、业务层不暴露宿主机绝对路径的边界。
- 如果把 Office Python 依赖塞进 Go capability 包，会让 Tool 层绑定具体格式库，增加跨产品维护成本。

### 13.4 后续分阶段执行计划

#### Phase A：最小内置 Office Skills

目标：让模型能发现并加载四个 Office Skill，先形成正确流程与边界。当前最小版本已完成。

执行项：

- 在 `internal/capabilities/skill/adapter/embedded/skills` 下新增 `office-word`、`office-excel`、`office-ppt`、`pdf-review`。
- 每个 Skill 先包含 `SKILL.md`、`references/validation-checklist.md` 和最小 `scripts/README.md` 或脚本占位说明。
- Frontmatter 声明 `allowed-tools`：`read_file`、`write_file`、`edit_file`、`run_command`、`read_skill_resource`、`search_skill_resources`。
- Frontmatter 声明 `dependencies.tools`，包括 `run_command`、`python`，PDF/转换类可声明 `libreoffice` 或 `poppler` 为可选依赖。
- 增加 embedded source 单测，确保四个 Skill 能被 `SystemFS()` 发现、解析和加载。

验收标准：

- `genesis-cli` 的 Skills catalog 能出现四个 Office Skill（目标挂在 `Skill` 工具 description）。
- `Skill(skill=...)`能加载四个 Skill，并返回依赖报告。
- 误调用 `tool=office-ppt` 不得仅返回 Profile 未允许；应返回 `skill_tool_collision` 或成功加载。
- `go test ./internal/capabilities/skill/... ./products/cli/...` 通过。

当前状态：四个 embedded Skill 已加入，系统 Skill 发现和资源读取已有单测覆盖。

#### Phase B：Office 脚本 MVP 与样例校验

目标：让每个 Skill 至少有一个可执行、可验证的最小脚本。当前已提供第一版 inspect 脚本。

执行项：

- `office-word`：已新增 `inspect_docx.py`，检查基础 docx 结构、表格、图片、批注、页眉页脚和 OCR 提示。
- `office-excel`：已新增 `inspect_xlsx.py`，检查 xlsx/csv/tsv、sheet、公式、错误单元格、媒体和 OCR 提示。
- `office-ppt`：已新增 `inspect_pptx.py`，检查 slide 数、媒体、备注、可抽取文本和 OCR 提示。
- `pdf-review`：已新增 `inspect_pdf.py`，检查页数、文本层、加密状态、样本文本和 OCR 提示；已新增 `extract_pdf_text.py`、`list_pdf_form_fields.py`、`render_pdf_pages.py` 覆盖文本、表单字段和页面预览。
- `office-word`：已新增 `convert_docx_to_pdf.py`，用于 LibreOffice PDF 预览验证。
- `office-ppt`：已新增 `render_pptx_preview.py`，用于 PDF 和图片预览 QA。
- `office-excel`：已新增 `recalc_xlsx.py`，用于 LibreOffice 公式重算和错误扫描。
- 统一脚本输出 JSON 契约：`ok`、`artifacts`、`warnings`、`errors`、`diagnostics`。
- 增加 `evals/` 或测试样例，覆盖成功、文件不存在、依赖缺失、格式错误。

验收标准：

- 通过 `run_command` 可运行脚本并得到结构化 JSON。
- 依赖缺失时错误信息可被 Agent 读懂并给出下一步建议。
- 不新增 Office 细粒度 Tool。

#### Phase C：依赖与执行策略治理

目标：把 Office 脚本运行纳入可审计、可审批、可配置的执行策略。

执行项：

- 在 Execution policy 中增加 Office 常见命令分类建议：`python` 脚本、`libreoffice --headless`、PDF 渲染命令。
- 增加 CLI 依赖探测命令或 doctor 检查，报告 Python 包、LibreOffice、Poppler 可用性。
- 明确 `SandboxOptional` / `SandboxRequired` 在 Office 场景下的默认配置和失败提示。
- 为大文件、未知来源文件、高风险文档预留强制 sandbox 的策略入口。

验收标准：

- Office Skill 加载时能提示依赖状态；执行时仍由 ToolGateway / Approval / Execution policy 最终裁决。
- `SandboxRequired` 不可用时失败关闭；`SandboxOptional` 降级时结果包含 warning/trace/audit。

#### Phase D：Office Execution Adapter 抽象

目标：把高频脚本沉淀为产品无关的 Office 处理服务接口，但仍不暴露大量 LLM Tool。

执行项：

- 新增 `internal/capabilities/office/contract`，定义 `InspectDocument`、`ConvertDocument`、`RenderPreview` 等粗粒度接口。
- 新增 Local adapter，通过 Execution Runtime 调脚本，不直接在 Go 中绑定 Office Python 库。
- 新增 Sandbox adapter，对接 `internal/capabilities/execution/adapter/sandbox` 或 genesis-sandbox client。
- Skill 仍是流程入口；Adapter 只作为脚本复用、错误标准化和产品执行差异的沉淀层。

验收标准：

- CLI 可选择 local adapter，Enterprise 可注入 sandbox/cloud adapter。
- Adapter 单测覆盖命令构造、错误映射、超时和路径策略。
- LLM 暴露面仍是 Skill + 通用 Tool，而不是大量 Office Tool。

#### Phase E：Enterprise / Desktop 产品装配

目标：让 Office 能力按产品边界分发，而不是复用 CLI 假设。

执行项：

- Enterprise bootstrap 注入企业 Skill source、租户级启停策略、审计 sink、sandbox endpoint/credential、外部 connector 占位。
- Enterprise 文件路径只暴露 workspace-relative path、sandbox path 或 resource id。
- Desktop bootstrap 接入本地 Skill source、GUI Skill 管理入口和本地 runner；仅在用户明确允许时使用宿主机 Office/LibreOffice。
- 补充产品隔离检查，防止 `internal/capabilities/office` 反向 import `products/*`。

验收标准：

- CLI、Desktop、Enterprise 的 Office 执行策略可独立配置。
- Enterprise 不依赖宿主机绝对路径或本地 Office 环境。
- 产品隔离脚本通过。

#### Phase F：Office Plugin / Marketplace 化

目标：当四个 Skills 与脚本稳定后，打包为可安装 Office 能力包。

执行项：

- 先用现有 Skill Marketplace 生成 `genesis-office-skills` 包，包含四个 Skill、脚本、references、样例。
- 再设计 `.genesis-plugin/plugin.json`，描述 Skills、assets、可选 MCP、命令依赖、UI 元数据、版本、签名和企业发布信息。
- 增加安装、禁用、升级、版本锁定、回滚和来源审计测试。
- Desktop 增加可视化管理入口；Enterprise 增加租户级发布策略。

验收标准：

- 用户可通过 CLI 安装 Office Skills 包，并被当前 Skill source 自动发现。
- 企业可按租户/项目发布或禁用 Office 能力。
- Plugin manifest 与当前 Skill Marketplace 兼容关系在文档中明确。

