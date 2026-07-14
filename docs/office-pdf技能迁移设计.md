# office-pdf（pdf）技能迁移设计

> 状态：**已实施**（2026-07-14；review-fix-rereview 含实施复查）  
> 日期：2026-07-14  
> 原则：**优先 Anthropic 成熟方法**；仅在 Genesis 硬约束处做最小适配。  
> 源：`D:\workspace\go\go-project\anthropics-skills\skills\pdf`  
> 目标：`internal/capabilities/skill/adapter/embedded/skills/office-pdf`  
> 试点先例：`office-ppt` ← Anthropic `pptx`；`office-word` ← Anthropic `docx`  
> 关联：
>
> - `docs/通用第三方Skill执行模型与office-ppt迁移设计.md`（**权威执行模型**：verbatim + Skill Session + bridge）
> - `docs/office-word-docx技能迁移设计.md`（同款「整删旧包 → 照搬」专项先例）
> - `docs/Office能力与Skills设计.md`（Office 能力分层；其中 `pdf-review` 命名与最小脚本描述**被本文推翻**）
> - `docs/Anthropic-Office-Skills完整能力迁移设计.md` §7 / Phase 4（**废止**对 PDF 的「改写为 run_skill_command + 硬约束进正文」旧迁移动作；执行模型以 ppt 终态为准）
> - `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`
> - `docs/Skill三模式执行与依赖闭环设计.md`
> - `docs/沙箱API对接与Profile选择规则.md`

---

## 0. TL;DR

把 Anthropic 官方 `pdf` Skill **按 office-ppt / office-word 同款范式**迁入 `office-pdf`：

1. **Skill 内容以 Anthropic 为准**：`scripts/**`、`LICENSE.txt`、`SKILL.md` 正文、forms/reference 内容 **按字节照搬**；落盘文件名按 §3.5 与 SKILL 交叉引用对齐为 `FORMS.md` / `REFERENCE.md`。装包纪律只放在 **`<skill_runtime_bridge>`**，不写进技能正文。
2. **先整包删除、再全新迁入（硬约束）**：现有 `pdf-review/` **全部是旧分叉**（`path_contract`、`inspect_pdf`、中文硬约束、`script=` resource-id 模型），禁止在目录内原地改文件。必须先删除整个 `embedded/skills/pdf-review/`，再从 anthropics `pdf/` 重建为 `office-pdf/`；同步改写所有硬编码 `pdf-review` / 旧脚本名的单测与文档引用。
3. **宿主适配不下沉进技能**：执行走已落地的 `run_skill_command(skill, command[, inputs])` + `<skill_runtime_bridge>`；技能正文继续写 `python scripts/...` / `pdftotext` / `qpdf` / `node`（pdf-lib）等原文命令。
4. **frontmatter 最小附加**：`name: office-pdf`；`description`/`license` 用 Anthropic **全文**；补 `allowed-tools` + `dependencies.runtime`（与 office-ppt 同构）。
5. **自包含、无 OOXML 共享树**：Anthropic `pdf` **不**含 `scripts/office/**`；本包**不**引入 `_office_common`，也不从 ppt/word 拷贝 OOXML 工具链。

一句话：PDF 迁移 = **删光旧 pdf-review → 照搬 Anthropic pdf → 注册为 office-pdf + frontmatter 声明依赖 + bridge 承载装包纪律**，不做原地增量改写。

---

## 1. 从第一性原理

### 1.1 真问题

用户要的是：**能读、抽表、合并/拆分、旋转、水印、加密、从零创建、填表、OCR、抽图并可做页面预览的 `.pdf` 交付能力**。  
Anthropic `pdf` 已在生产环境验证的方法是：

| 任务 | Anthropic 方法 |
| --- | --- |
| 读文本 / 元数据 / 合并拆分 / 旋转 / 水印 / 加密 | **pypdf**（库内联代码）或 **qpdf** / **pdftk**（CLI） |
| 版式文本 / 表格 | **pdfplumber**（可接 pandas） |
| 从零创建 | **reportlab**（Canvas / Platypus）；高级见 reference（pdf-lib） |
| 填可填表单 | `forms.md` + `scripts/check_fillable_fields.py` → `extract_form_field_info.py` → `fill_fillable_fields.py` |
| 填不可填（批注叠字） | `fill_pdf_form_with_annotations.py` + 坐标校验脚本 |
| 页面图 / 视觉 QA | `convert_pdf_to_images.py`（pdf2image）或 `pdftoppm` / pypdfium2 |
| 扫描件 OCR | `pdf2image` + **pytesseract**（正文写明；依赖 Poppler） |
| 抽内嵌图 | `pdfimages`（poppler-utils） |

这些不是「细粒度 PDF Tool」，而是**可移植技能包**。Genesis 不应再用自研 `inspect_pdf` JSON 契约去替代它。

### 1.2 现状失败真正缺什么

| 已有 | 缺口 |
| --- | --- |
| `pdf-review` 最小中文 Skill + inspect/extract/list_form/render + `path_contract` | **没有** Anthropic 级创建（reportlab）、合并拆分教程、forms 闭环、reference 高级能力 |
| office-ppt / office-word 已按 verbatim 范式落地 | pdf-review 仍停在「Genesis 自研最小版 + 宿主泄漏进正文」 |
| `run_skill_command` + bridge + materialize | PDF 侧资产未对齐，底座空转 |

根因不是 Runner 不够，而是 **pdf-review 内容资产仍是分叉占位，不是 Anthropic pdf**。

### 1.3 非目标（本迁移不做）

- 不把 `office-pdf` / `pdf` / `pdf-review` 注册为 LLM Tool。
- 不在 Go 中绑定 pypdf / reportlab / pdf-lib。
- 不引入 Adobe Acrobat / 商业 PDF API（MCP 另案）。
- 不把 SKILL 正文改写成 `run_skill_command(...)` 示例。
- 不为「中文友好」重写 Anthropic 教程。
- 不保留旧 `inspect_pdf.py`「统一 JSON 诊断」双轨。
- 不把 OCR 能力拆成独立 Skill（Anthropic 写在同一 pdf 包内；Profile 升级由运行时处理）。

### 1.4 失败条件

- 对旧 `pdf-review/` **原地改文件 / 局部替换**（易漏改、残留 `path_contract`/`inspect_*`/旧 SKILL 措辞）。
- 迁移后仍以 `inspect_pdf.py` 为主路径，Anthropic 流程沦为附件。
- 为对齐 Genesis 而改写 forms 脚本或把 `INPUT_DIR`/`OUTPUT_DIR` 写进脚本。
- 在正文出现 `office-basic` 镜像名、「Genesis 硬约束」整节、`script=` resource-id 示例。
- 从 ppt/word 拷贝无关 OOXML 树冒充 pdf 资产。
- 单测仍断言 `pdf-review` / `inspect_pdf.py` 却宣称迁移完成。
- 仅迁 `scripts/` 而漏掉 `forms.md` / `reference.md`（填表与高级路径断裂）。

### 1.5 「verbatim」的精确边界

| 层级 | 要求 |
| --- | --- |
| `scripts/**`、`LICENSE.txt` | **按字节**对齐 anthropics `pdf/` |
| `SKILL.md` 工作流正文 | **verbatim**（含文中 `pip install` 示例注释）；不插入 Genesis 装包纪律 |
| `forms.md` / `reference.md` 正文内容 | **按字节**；文件名若需与 SKILL 交叉引用在 Linux 下可解析，允许 §3.5 的**唯一**路径大小写对齐 |
| 装包 / 执行纪律 | **仅** `<skill_runtime_bridge>`（及 frontmatter `dependencies.runtime`） |
| frontmatter | Genesis 必需字段（§4）；`description` 用 Anthropic **完整字符串** |

---

## 2. 与 office-ppt / office-word 迁移的对齐关系

| 维度 | office-ppt | office-word | office-pdf（本文） |
| --- | --- | --- | --- |
| 源技能 | Anthropic `pptx` | Anthropic `docx` | Anthropic `pdf` |
| Genesis 名 | `office-ppt` | `office-word` | **`office-pdf`**（取代 `pdf-review`） |
| 内容策略 | 脚本按字节 + 正文 verbatim + 纪律在 bridge | **同** | **同**（§1.5） |
| 删除自造 | 旧耦合稿 | **整目录删除** | **整目录删除** `pdf-review/` 后重建 |
| 创建栈 | Node `pptxgenjs` | Node `docx` | Python **reportlab**；高级 Node **pdf-lib**（reference） |
| 读内容 | markitdown | pandoc | pdfplumber / pypdf / pdftotext |
| 领域专项 | editing.md / pptxgenjs.md | 批注/修订/validate | **forms.md** + **reference.md** |
| OOXML `scripts/office` | 自包含 | 自包含 | **无**（不引入） |
| 执行入口 | bridge → `run_skill_command(command=原文)` | **同** | **同** |

**不另造 PDF 专用执行模型。** 若执行层仍有 WorkspaceFS 等缺口，归 office-ppt 迁移文档与运行时改造，不在 PDF 技能里打补丁。

### 2.1 为何改名为 `office-pdf` 而非保留 `pdf-review`

| 方案 | 评价 |
| --- | --- |
| **A. `office-pdf`（采用）** | 与 `office-ppt` / `office-word` / `office-excel` 同族；名称覆盖创建/填表/OCR，不再暗示「仅审阅」；整删重建时改名成本最低 |
| B. 保留 `pdf-review` | 减少文档字符串替换，但名实不符，且与 office-* 命名不一致 |
| C. 注册 Anthropic 原名 `pdf` | 过短、易与通用词碰撞；CollisionGuard / catalog 风格与现有 office-* 不一致 |

实施期必须全局把 `pdf-review` 引用切到 `office-pdf`（至少：embedded 单测、`allowed_tools_align_test`、Office 相关 docs 交叉引用）。

---

## 3. 源资产清单与迁移动作

### 3.1 直接照搬（verbatim）

| 目标路径（`office-pdf/`） | 来源（anthropics `pdf/`） | 说明 |
| --- | --- | --- |
| `SKILL.md` 正文 | `SKILL.md` 正文 | Overview / Quick Start / 库与 CLI / Common Tasks / Quick Reference **verbatim**；frontmatter 见 §4 |
| `LICENSE.txt` | `LICENSE.txt` | 合规一并带上 |
| `forms.md` 或 `FORMS.md` | `forms.md` | 填表主路径；文件名见 §3.5 |
| `reference.md` 或 `REFERENCE.md` | `reference.md` | 高级库与排障；文件名见 §3.5 |
| `scripts/check_fillable_fields.py` | 同 | 是否可填 |
| `scripts/extract_form_field_info.py` | 同 | 字段 JSON |
| `scripts/extract_form_structure.py` | 同 | pdfplumber 结构辅助 |
| `scripts/fill_fillable_fields.py` | 同 | 可填字段写入 |
| `scripts/fill_pdf_form_with_annotations.py` | 同 | 不可填：FreeText 批注 |
| `scripts/convert_pdf_to_images.py` | 同 | pdf2image 渲染 |
| `scripts/create_validation_image.py` | 同 | 填表 QA 叠加框 |
| `scripts/check_bounding_boxes.py` | 同 | 坐标/重叠校验 |

### 3.2 旧包处置：整目录删除再迁入（禁止原地改）

现有仓库内 `pdf-review` **全部视为过时分叉**，不是可增量升级的基线：

```text
internal/capabilities/skill/adapter/embedded/skills/pdf-review/
  SKILL.md                         # 旧中文 + run_skill_command(script=…) + 镜像名硬约束
  references/validation-checklist.md
  scripts/inspect_pdf.py
  scripts/extract_pdf_text.py
  scripts/list_pdf_form_fields.py
  scripts/render_pdf_pages.py
  scripts/path_contract.py
```

**硬约束**：

1. **先删除整个目录** `embedded/skills/pdf-review/`（含全部子文件），不要逐文件「改成 Anthropic 风格」。
2. **再新建** `embedded/skills/office-pdf/`，从 `anthropics-skills/skills/pdf/` **按字节拷入** §3.1 资产，然后只做 §4 frontmatter + §3.5 文件名对齐（若需要）。
3. **同步改测试与引用**：至少包括  
   - `internal/capabilities/skill/adapter/embedded/source_test.go`（`pdf-review` → `office-pdf`；断言新资源如 `scripts/fill_fillable_fields.py` / `FORMS.md`）  
   - `products/cli/internal/profile/allowed_tools_align_test.go`（skill 名列表）  
   - 文档交叉引用（实施切片内更新，不挡技能落盘）
4. **禁止**保留「暂时留着 inspect 备用」的双轨文件。

理由：旧包与 Anthropic 在执行模型、脚本集合、SKILL 语义上全面分叉；原地改极易残留 `path_contract`、旧 `allowed-tools` 或漏拷 forms/reference，验收成本高于整删重建。

### 3.3 明确不新增

| 项 | 态度 |
| --- | --- |
| 在旧 `pdf-review/` 上原地修改 / 局部替换 | **禁止**（见 §3.2） |
| 保留 `inspect_pdf.py` 作为「稳定 JSON 门面」 | **禁止** |
| Genesis 包装 `run_pdf_script.py` | 不做 |
| 新 `generate_pdf` / `pdf.*` Tool | 禁止 |
| 中文化重写 SKILL 正文 | 禁止 |
| `_office_common` / 从 ppt 拷贝 schemas | 禁止 |
| 把 OCR 拆成独立 embedded skill | 禁止（本包内；Profile 运行时升级） |

### 3.4 迁移后目录

```text
office-pdf/
├── SKILL.md                 # Anthropic 正文 verbatim + §4 frontmatter
├── LICENSE.txt
├── FORMS.md                 # 或 forms.md（§3.5）
├── REFERENCE.md             # 或 reference.md（§3.5）
└── scripts/
    ├── check_fillable_fields.py
    ├── check_bounding_boxes.py
    ├── convert_pdf_to_images.py
    ├── create_validation_image.py
    ├── extract_form_field_info.py
    ├── extract_form_structure.py
    ├── fill_fillable_fields.py
    └── fill_pdf_form_with_annotations.py
```

### 3.5 唯一允许的非脚本适配：交叉引用文件名大小写

Anthropic 源盘文件名为小写 `forms.md` / `reference.md`，但 `SKILL.md` Next Steps 写的是 `FORMS.md` / `REFERENCE.md`。在 macOS/Windows 默认大小写不敏感盘上可碰巧工作；**Linux 沙箱**上模型按 SKILL 去读会失败。

**决策（最小适配）**：

- 正文内容仍按字节来自 anthropics；
- **落盘文件名与 SKILL.md 交叉引用对齐为 `FORMS.md`、`REFERENCE.md`**（内容来自小写源文件）；
- **不**改正文里的链接文字；
- 除此以外禁止改 SKILL / forms / reference / scripts 正文。

若未来上游统一大小写，再随上游收敛。

---

## 4. frontmatter：最小附加（对齐 office-ppt）

保留 Anthropic 的 `description`、`license`。Genesis 需要的治理字段与 office-ppt **同构**：

```yaml
---
name: office-pdf
description: >-
  Use this skill whenever the user wants to do anything with PDF files. This includes reading or
  extracting text/tables from PDFs, combining or merging multiple PDFs into one, splitting PDFs apart,
  rotating pages, adding watermarks, creating new PDFs, filling PDF forms, encrypting/decrypting PDFs,
  extracting images, and OCR on scanned PDFs to make them searchable. If the user mentions a .pdf file
  or asks to produce one, use this skill.
license: Proprietary. LICENSE.txt has complete terms
allowed-tools:
  - Skill
  - list_skill_resources
  - read_skill_resource
  - search_skill_resources
  - run_skill_command
  - install_skill_dependencies
  - read_file
  - write_file
  - edit_file
dependencies:
  runtime:
    python:
      # —— 主路径（任意 .py 的 preflight 都会检查；镜像须预装）——
      - name: pypdf
        import: pypdf
      - name: pdfplumber
        import: pdfplumber
      - name: reportlab
        import: reportlab
      - name: pdf2image
        import: pdf2image
      - name: Pillow
        import: PIL
      # —— OCR（技能 description 一等能力；见 §4.4）——
      - name: pytesseract
        import: pytesseract
    node:
      # 仅当 command 为 .js/.mjs 时 preflight；不影响 forms 的 .py
      - name: pdf-lib
        require: pdf-lib
    system:
      # name 与 LookPath 命令对齐（避免同名 poppler 覆盖 whitelist key）
      - name: pdftoppm
        command: pdftoppm
      - name: pdftotext
        command: pdftotext
      - name: pdfimages
        command: pdfimages
      - name: qpdf
        command: qpdf
      - name: tesseract
        command: tesseract
---
```

> 实施时 `description` 以 anthropics `pdf/SKILL.md` frontmatter **原文粘贴**为准（上表为可读换行，不得删减触发句）。  
> `pdftk`：Anthropic 标明「if available」——**不**强制写入 `dependencies.runtime.system`；镜像可选预装，缺失时走 qpdf/pypdf 路径即可。  
> `pdftoppm`/`pdftotext`/`pdfimages` 均来自 **poppler-utils** 包，但 frontmatter 按**可执行命令**各声明一条，以便 LookPath 与 `system:` whitelist key 不互相覆盖。  
> OCR：`pytesseract`（pip）+ `tesseract`（system）+ `pdf2image`（需 Poppler）。实际 OCR 任务应由 ProfileResolver 升级到 `office-ocr`（名称只在注册表），**不写进技能正文**。

### 4.1 字段说明

| 字段 | 规则 |
| --- | --- |
| `name` | **必须** `office-pdf`；Anthropic 原名 `pdf`、旧名 `pdf-review` 不作为注册名 |
| `description` | Anthropic **完整**原文；强触发 `.pdf` / merge / split / forms / OCR |
| `allowed-tools` | 与 office-ppt 对齐；填表需读图与写 JSON，依赖 `read_file` / `write_file` / `edit_file` |
| `dependencies.runtime` | 声明式；**不含**镜像名；pip/npm 缺省走 `install_skill_dependencies` 或镜像预装；system 命令只能由 Profile/镜像提供 |
| 正文工作流 | **零** Genesis 装包纪律措辞 |
| 装包纪律 | 仅 `<skill_runtime_bridge>` + frontmatter |

### 4.2 对正文中 `pip install` / OCR 注释的处理

**不改正文。** 例如「Requires: pip install pytesseract pdf2image」视为依赖说明。执行期由 bridge 禁止用 `run_skill_command` 做 ad-hoc install，并导向 `install_skill_dependencies`（仅已声明包）或 profile 补齐。

### 4.3 CollisionGuard 与别名

- Catalog / CollisionGuard 注册名是 `office-pdf`。
- 模型若把 Anthropic 原名 `pdf` 或旧名 `pdf-review` 当 Tool 调用：**本切片不强制**别名（与 `pptx`→`office-ppt` 同级）；可选增强见 §13 polish。
- 把 `office-pdf` 当 Tool 调用时，既有 CollisionGuard 必须能命中并提示 `Skill(skill="office-pdf")`。

### 4.4 依赖分层与 preflight 语义（实施硬约束）

当前 `preflightRuntime` 行为（代码事实，不可在技能里绕过）：

| 触发 | 检查范围 |
| --- | --- |
| `command` 涉及 `.py` | **全部** `runtime.python`（不是按 import 图裁剪） |
| `command` 涉及 `.js/.mjs/.cjs/.ts` | **全部** `runtime.node` |
| 任意 command | **全部** `runtime.system`（缺失仅 warning，不硬失败） |

因此：

| 层级 | 包/命令 | 策略 |
| --- | --- | --- |
| **frontmatter 必选 Python** | pypdf、pdfplumber、reportlab、pdf2image、Pillow、pytesseract | 必须进 frontmatter；**office-basic 镜像须预装**（否则任意 forms `.py` 都会 `dependency_missing`） |
| **frontmatter 必选 Node** | pdf-lib | 仅 JS 路径触发；可声明 |
| **frontmatter 必选 system** | pdftoppm、pdftotext、pdfimages、qpdf、tesseract | LookPath；缺则 warning；OCR 实跑仍需 tesseract 在 office-ocr |
| **不进 frontmatter（本切片）** | pandas、pypdfium2 | 仅 REFERENCE 高级示例；若写入 runtime.python，会迫使**所有** `.py` preflight 装齐 → **故意不声明**，避免拖垮主路径。需要时由镜像静默提供或后续单开「扩展白名单」设计 |
| **optional CLI** | pdftk | 不声明 |

**禁止**把「高级才用的 pip 包」和「主路径包」不加区分地堆进 frontmatter 却不保证镜像预装——那会让 `check_fillable_fields.py` 因缺 `pandas` 而假失败。

---

## 5. 运行时行为（技能不可见）

```text
Skill(skill="office-pdf", ...)
  → 加载 verbatim 正文 + 注入 <skill_runtime_bridge>

首次脚本：
  run_skill_command(
    skill="office-pdf",
    command="python scripts/check_fillable_fields.py form.pdf",
    inputs=[...]   # 可选
  )
  → materialize 完整 office-pdf 包到 Skill Session 工作目录
  → cwd=工作目录，相对路径 verbatim 可解析

创建示例：
  write_file("$WORK_DIR/create_report.py")   # 顶层 reportlab 脚本
  run_skill_command(
    skill="office-pdf",
    command="python create_report.py",
    inputs=["$WORK_DIR/create_report.py"]
  )
  → 产物写工作目录相对路径（如 report.pdf）
  → 填表 / pdftoppm / qpdf 按 SKILL / FORMS 原文继续
```

### 5.1 Profile

- frontmatter `dependencies.runtime` → ProfileResolver → office 类镜像（`office-basic` 等名称**只存在于注册表**）。
- 扫描件 / 无文本层 / 明确 OCR → 运行时升级 `office-ocr`（不写进技能正文）。
- **取消**对 pdf-review「仅 inspect 脚本」的任何特例。

### 5.2 产物与门禁

- 禁止 `write_file` 伪造 `.pdf`（binarygate 已有）。
- 交付前 PDF 魔数/可打开性门禁；技能内 QA 按 Anthropic：渲染页图、`check_bounding_boxes`、抽关键文本等。
- 缺依赖时如实说明，不伪造预览或「已 OCR」。

### 5.3 与 Word/PPT「转 PDF 预览」的关系

- `office-word` / `office-ppt` 的视觉 QA（soffice → PDF → pdftoppm）**仍属各自技能**；不强制为预览去 `Skill(office-pdf)`。
- 用户任务**以 PDF 本身为交付物或主操作对象**时，才加载 `office-pdf`。
- 禁止在 office-pdf 正文增加「从 docx/pptx 转换」Genesis 专有流程（上游未作为本 skill 主路径）。

---

## 6. PDF 特有能力与约束

### 6.1 从零创建（reportlab / pdf-lib）

- 模型按 SKILL 内教程写 **顶层 Python 脚本**（reportlab）；复杂布局可跟 REFERENCE 用 pdf-lib（Node）。
- **硬规则（保持 Anthropic）**：禁止用 Unicode 上下标字符（会显示黑块），改用 `<sub>` / `<super>` 或手动定位。
- Genesis 仅约束：中间脚本落工作目录；最终 `.pdf` 写相对路径；经 `run_skill_command` 执行。

### 6.2 填表闭环（FORMS.md）

严格按上游顺序，**禁止跳步**（命令以 FORMS.md 原文为准；下表补 `.py` 仅作实施者对照，**不改正文**）：

1. `python scripts/check_fillable_fields.py <file.pdf>`（上游示例偶发省略 `.py`，见 §11）
2. 可填：`extract_form_field_info.py` → `convert_pdf_to_images.py` → 写 `field_values.json` → `fill_fillable_fields.py`
3. 不可填：结构/视觉估坐标 → `fill_pdf_form_with_annotations.py` → `check_bounding_boxes.py` / `create_validation_image.py`

脚本之间存在同目录相对 import（`fill_fillable_fields.py` → `from extract_form_field_info import get_field_info`）：

- **必须**完整 materialize 整个 `scripts/`；
- 命令形态保持 `python scripts/<name>.py ...`（Python 会把 `scripts/` 放进 `sys.path[0]`，相对 import 才能解析）；
- 禁止只拷单个脚本、禁止改写成 `python -m` 包导入（除非上游先改）。

### 6.3 OCR

- 正文路径：`pdf2image.convert_from_path` + `pytesseract.image_to_string`。
- 运行时：OCR 意图 → `office-ocr` Profile；缺 tesseract/poppler 时失败并说明，**不**静默降级为瞎编文本。

### 6.4 CLI 工具优先级

| 能力 | 优先（技能原文） | 备注 |
| --- | --- | --- |
| 合并/拆分/旋转/解密 | pypdf 或 qpdf | pdftk 可选 |
| 纯文本抽取 | pdftotext / pdfplumber | 大文件参考 REFERENCE 性能提示 |
| 渲染 | pdftoppm 或 convert_pdf_to_images / pypdfium2 | |
| 抽图 | pdfimages | |

---

## 7. 许可

Anthropic `LICENSE.txt` 为 Proprietary。与 office-ppt / office-word 相同：实施前须确认仓库/产品再分发权利；产品侧若已确认「允许原样迁移 + 最小适配」，则可 copy；否则改为能力对齐重写。设计层不绕过该约束。

---

## 8. 实施切片

| 顺序 | 项 | 验收要点 |
| --- | --- | --- |
| 0 | **整目录删除** `embedded/skills/pdf-review/` | 路径不存在；无残留 `inspect_pdf`/`path_contract`/`extract_pdf_text` |
| 1 | 新建 `office-pdf/`，按字节拷入 anthropics `pdf` 的 scripts/、LICENSE、SKILL 正文、forms/reference | 8 个脚本 + FORMS/REFERENCE 齐全；`go:embed all:skills` 自动收录，**无需**改 embed 代码 |
| 2 | §3.5 文件名对齐（若采用大写） | Linux 下按 SKILL 链接可读 |
| 3 | 写 frontmatter（§4）；正文保持 Anthropic | `name=office-pdf`；runtime 含核心 pip + poppler/qpdf/tesseract 等；正文无装包纪律句 |
| 4 | 核对 office-basic / office-ocr 镜像契约 | **basic 须含** §4 frontmatter 全部 pip（含 pytesseract）+ poppler 三命令 + qpdf + 可选 pdf-lib；**ocr 须含** tesseract 二进制；pandas/pypdfium2 可选预装但不进 frontmatter |
| 5 | 改写引用面 | `source_test.go`、`allowed_tools_align_test.go`；docs：`Office能力与Skills设计.md`、`Anthropic-Office-Skills完整能力迁移设计.md`、`capability-package-marketplace-plugin-design.md`（凡 `pdf-review` → `office-pdf`） |
| 6 | 冒烟 | （a）reportlab 顶层脚本生成可打开 PDF；（b）若有样例可填 PDF：check → extract → fill 链；（c）`pdftoppm` 或 `convert_pdf_to_images.py` 出页图。无样例表单时（b）可 skip 但须在切片记录 |
| 7 | 依赖闭环 | 刻意缺 `pypdf` → `dependency_missing`；`install_skill_dependencies` 可装已声明包；缺 `qpdf` 仅 warning 不硬失败 |
| 8 | 文档交叉引用收尾 | 标注旧「硬约束进正文 / resource-id」迁移动作对 PDF **废止** |

**不在本切片做**：Excel 迁移、新 Tool、中文 SKILL、`_office_common`、把 pandas/pypdfium2 塞进 frontmatter、OCR Runner 自动升级若尚未落地则只保证「声明 + 人工/策略可升」。

---

## 9. DoD

1. `Skill(skill="office-pdf")` 加载后，模型看到的主流程与 Anthropic pdf **同构**（读写/合并拆分/创建/填表/OCR/reference）。
2. 按 SKILL / FORMS 原文命令经 `run_skill_command` 可完成：从零生成合法 `.pdf`；若具备可填样例 PDF，则走完填表脚本链（无样例时填表冒烟可 skip，但 scripts 资源必须存在且可 materialize）。
3. 多步填表/渲染在同一 Skill Session 工作目录不断链；`scripts/` 完整 materialize。
4. 技能正文 **不出现** Genesis 装包纪律句、镜像名、强制 `INPUT_DIR`/`OUTPUT_DIR`、`script=` resource-id。
5. 误调用 `tool=office-pdf` 时 CollisionGuard 纠偏；**不**要求本切片实现 `pdf` / `pdf-review` 别名。
6. `go test`：embedded 发现 `office-pdf`；CLI allowed-tools 对齐；无旧 `pdf-review` 目录。
7. 仓库内 **不存在** 旧脚本：`inspect_pdf.py`、`extract_pdf_text.py`、`list_pdf_form_fields.py`、`render_pdf_pages.py`、`path_contract.py`（在已删除的 pdf-review 下）。

---

## 10. 方案对照

| 方案 | 评价 |
| --- | --- |
| **A. 整删 pdf-review + Anthropic 重建为 office-pdf + ppt 外壳（本文）** | 成熟度最高、无漏改残留 → **采用** |
| B. 在旧 pdf-review 上原地改 / 局部替换 | 易漏改、双轨残留 → **禁止** |
| C. 保留 inspect JSON 门面 + 局部吸收 forms | 双轨、能力残缺 → **废止** |
| D. 中文重写 + 自研脚本 | 未证明优于 Anthropic → **拒绝** |
| E. 细粒度 `pdf.*` Tools | 违背 Office 分层 → **禁止** |
| F. 保留名 `pdf-review` 只换内容 | 名实不符 → **不采用**（见 §2.1） |

---

## 11. 残余风险（接受）

| 风险 | 处置 |
| --- | --- |
| Proprietary 许可 | 与 ppt/word 同；产品已允则可 copy，否则 rewrite |
| preflight 对任意 `.py` 检查全部 pip | §4.4：frontmatter 只留主路径+OCR；pandas/pypdfium2 不进；镜像预装 frontmatter 全套 |
| `pytesseract` 进 basic 镜像但无 tesseract 二进制 | import 可通过；OCR 实跑需 office-ocr；接受「包在、引擎可缺」 |
| `pdftk` 未强制 | 接受；上游标 optional |
| SKILL 引用 `FORMS.md` 与源盘小写不一致 | §3.5 落盘对齐 |
| forms 示例命令偶发缺 `.py` 后缀 | **照搬**上游；若实跑失败属上游提示瑕疵，不在本迁移「修教程」 |
| 模型把 `pdf` / `pdf-review` 当 Tool | 可选别名；本切片不阻塞 |
| 模型仍可能 `pip install` 或 `write_file` 假 pdf | bridge + binarygate + 依赖闭环 |
| 中文 PDF 默认英文字体导致缺字黑块 | **已缓解**：`scripts/register_cjk_font.py` 跨平台探测 + bridge 强制注册 CJK；**不**往技能包塞字体文件（沙箱镜像已有 fonts-noto-cjk）；Linux 优先 wqy 等 TrueType 友好字体，Noto CJK TTC（CFF/PostScript outlines）注册失败时自动回退 |
| OCR 自动 Profile 升级未接线 | 与 Anthropic-Office 文档 Phase 4 残余一致；本迁移保证声明与手工/策略可升 |
| Windows 宿主字体/中文 reportlab | 保持上游教程；中文交付由任务选字体，不改技能包 |
| `fill_fillable_fields` 相对 import | 依赖完整 materialize + `python scripts/...` cwd 语义；§6.2 |
| 无上游附带的样例可填 PDF | 冒烟（b）可 skip；不在技能包内自造非 Anthropic 样例资产（避免分叉） |
| REFERENCE 用 pandas/pypdfium2 但未进白名单 | 接受；高级路径依赖镜像预装或后续扩展白名单设计 |

---

## 12. 与其它文档的关系

| 文档 | 关系 |
| --- | --- |
| `通用第三方Skill执行模型与office-ppt迁移设计.md` | **执行模型权威**；本文是 PDF 资产迁移专项 |
| `office-word-docx技能迁移设计.md` | 同款「整删 → 照搬」模板；PDF 无 OOXML 树 |
| `Anthropic-Office-Skills完整能力迁移设计.md` Phase 4 / §7 | **对 PDF 迁移动作废止**「硬约束进正文 + resource-id」；以本文 + ppt 终态为准 |
| `Office能力与Skills设计.md` | 实施切片第 8 步：`pdf-review` → `office-pdf`，能力描述改为 Anthropic 对齐 |
| `沙箱API对接与Profile选择规则.md` | Profile 仍按工作负载；OCR → office-ocr |

---

## 13. 审查记录（review-fix-rereview）

| 轮次 | 从第一性原理的发现 | 处置 |
| --- | --- | --- |
| R1 | `runtime.system` 用 `poppler-pdftotext` 等人造名，且多条同名 `poppler` 会覆盖 `system:` whitelist key | 改为按可执行命令各声明一条（`pdftoppm`/`pdftotext`/`pdfimages`） |
| R1 | 把 pandas/pypdfium2 与主路径包一并写入 frontmatter，会因「任意 .py 检查全部 pip」导致 forms 脚本假失败 | 新增 §4.4 preflight 语义；pandas/pypdfium2 **不进** frontmatter |
| R1 | OCR 是 description 一等能力，但 tesseract 与 pytesseract 分层不清 | pytesseract 进 frontmatter（镜像预装）；tesseract 进 system；OCR 实跑靠 office-ocr |
| R1 | §6.2 写带 `.py` 的命令，与「照搬上游（偶发无后缀）」未对齐 | 标明对照用；残余风险保留上游瑕疵 |
| R1 | 引用面只写了部分测试，漏 `capability-package-marketplace-plugin-design.md` | 切片第 5 步列全 |
| R1 | 相对 import 成功条件未写清 | §6.2 补 `sys.path[0]=scripts/` 机制 |
| R2 | TL;DR 仍写小写 forms/reference，与 §3.5 决策不一致 | TL;DR 改为明确落盘 `FORMS.md`/`REFERENCE.md` |
| R2 | §4.3 别名「见 §11」指错（§11 是残余风险） | 改为见 §13 polish |
| R2 | DoD#2 强制填表链与「无样例可 skip」矛盾 | DoD#2 改为有样例才强制；无样例时保证资源可 materialize |
| R2 | 未写明 embed 机制，实施者可能误改 Go 装配 | 切片第 1 步注明 `go:embed all:skills` 自动收录 |
| R3 | 通读后无新的正确性/边界 actionable 项 | **设计收敛** |
| 实施 | 2026-07-14 落地：整删 `pdf-review`；迁入 anthropics `pdf`→`office-pdf`；FORMS/REFERENCE 大小写对齐；frontmatter §4；单测/CLI align/交叉文档 | `go test` embedded+profile 通过；reportlab 冒烟生成可打开 PDF；forms 填表冒烟因无样例 PDF skip；8 脚本 py_compile 通过 |
| 实施复查 R1 | 脚本/LICENSE/FORMS/REFERENCE 内容与源 SHA256 一致；SKILL 正文 verbatim；正文无 Genesis 泄漏；仓库 Go 代码无 `pdf-review` | 通过（frontmatter 含 `run_skill_command` 属预期，非泄漏） |
| 实施复查 R2 | 对照 DoD：catalog 名、资源断言、旧目录不存在、文档交叉引用已更新 | **收敛**；残余见 §11 |

**可选 polish（不阻断收敛）**：为 `pdf`/`pdf-review` 增加 CollisionGuard 别名；中文补充 reference；Runner 自动 OCR Profile 升级；为 pandas/pypdfium2 做「扩展白名单」而不进入默认 preflight；镜像清单在 genesis-sandbox 仓按 `docs/沙箱API对接与Profile选择规则.md` 落地构建。

**残余风险**：见 §11。另接受：宿主未预装 frontmatter pip 时本地 preflight 会 `dependency_missing`（镜像/install 闭环覆盖）。
