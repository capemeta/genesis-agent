# office-word（docx）技能迁移设计

> 状态：**已实施**（2026-07-14；review-fix-rereview 含实施复查）  
> 日期：2026-07-14  
> 原则：**优先 Anthropic 成熟方法**；仅在 Genesis 硬约束处做最小适配。  
> 源：`D:\workspace\go\go-project\anthropics-skills\skills\docx`  
> 目标：`internal/capabilities/skill/adapter/embedded/skills/office-word`  
> 试点先例：`office-ppt` ← Anthropic `pptx`（见 `docs/通用第三方Skill执行模型与office-ppt迁移设计.md`）  
> 关联：
>
> - `docs/通用第三方Skill执行模型与office-ppt迁移设计.md`（**权威执行模型**：verbatim + Skill Session + bridge）
> - `docs/Office能力与Skills设计.md`（Office 能力分层；其中 §7.1.1 对 DOCX「后续独立实现」的旧判断**被本文推翻**）
> - `docs/Anthropic-Office-Skills完整能力迁移设计.md` §4.2（**废止**：保留 inspect/`path_contract` 的旧迁移动作）
> - `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`
> - `docs/Skill三模式执行与依赖闭环设计.md`

---

## 0. TL;DR

把 Anthropic 官方 `docx` Skill **按 office-ppt 同款范式**迁入 `office-word`：

1. **Skill 内容以 Anthropic 为准**：`scripts/**`、`LICENSE.txt` **按字节照搬**；`SKILL.md` 工作流与 Dependencies 列表 **verbatim**（可保留原文 `npm install -g` / `pip install` 作为依赖说明）。装包纪律（禁止 ad-hoc install、走 `install_skill_dependencies`）只放在 **`<skill_runtime_bridge>`**，不写进技能正文。
2. **先整包删除、再全新迁入（硬约束）**：现有 `office-word/` **全部是旧分叉**，禁止在目录内原地改文件「修修补补」。必须先删除整个 `embedded/skills/office-word/`，再从 anthropics `docx/` 重建；同步改写依赖旧资源路径的单测（如 `source_test.go` 中的 `inspect_docx.py`）。
3. **宿主适配不下沉进技能**：执行走已落地的 `run_skill_command(skill, command[, inputs])` + `<skill_runtime_bridge>`；技能正文继续写 `python scripts/...` / `node` / `pandoc`。
4. **frontmatter 最小附加**：`name: office-word`（保持现名）；`description`/`license` 用 Anthropic **全文**；补 `allowed-tools` + `dependencies.runtime`（与 office-ppt 同构）。
5. **自包含**：本包自带完整 `scripts/office/**`，**不**走 `_office_common`；源取自 **anthropics `docx/`**，不要从 `office-ppt` 拷贝（`soffice.py` 等文件可能已有本地漂移）。

一句话：docx 迁移 = **删光旧 office-word → 照搬 Anthropic docx → frontmatter 声明依赖 + bridge 承载装包纪律**，不做原地增量改写。

---

## 1. 从第一性原理

### 1.1 真问题

用户要的是：**能创建、读取、编辑、批注、接受修订、校验并可做 PDF/图片预览的 `.docx` 交付能力**。  
Anthropic `docx` 已在生产环境验证的方法是：

| 任务 | Anthropic 方法 |
| --- | --- |
| 读内容 | `pandoc`（含 `--track-changes`）或 unpack 看 XML |
| 从零创建 | **docx-js**（Node）写完整脚本 → 生成 → `scripts/office/validate.py` |
| 编辑已有 | unpack → 直接改 XML（Edit 工具）→ `comment.py` / 修订标记 → pack |
| 接受修订 | `scripts/accept_changes.py`（LibreOffice macro） |
| 视觉 QA | `soffice` 转 PDF → `pdftoppm` |

这些不是「细粒度 Word Tool」，而是**可移植技能包**。Genesis 不应再自研一套 inspect-only 流程去替代它。

### 1.2 现状失败真正缺什么

| 已有 | 缺口 |
| --- | --- |
| `office-word` 最小中文 Skill + `inspect_docx.py` / `convert_docx_to_pdf.py` | **没有** Anthropic 级创建（docx-js）、批注、修订、unpack/pack/validate 闭环 |
| office-ppt 已按 verbatim 范式落地 | office-word 仍停在「Genesis 自研最小版」，与执行模型文档目标不一致 |
| `run_skill_command` + bridge + materialize | Word 侧资产未对齐，底座空转 |

根因不是 Runner 不够，而是 **office-word 内容资产仍是分叉占位，不是 Anthropic docx**。

### 1.3 非目标（本迁移不做）

- 不把 `office-word` / `docx` 注册为 LLM Tool。
- 不在 Go 中绑定 `python-docx` / docx-js。
- 不引入 Microsoft Graph / Google Docs（MCP 另案）。
- 不把 SKILL 正文改写成 `run_skill_command(...)` 示例。
- 不为「中文友好」重写 Anthropic 教程（与 office-ppt 一致：英文成熟提示优先）。
- 不强制把超长 `SKILL.md` 拆成 `docxjs.md`（Anthropic 本身内嵌；拆分会破坏内链与触发行为，除非后续证明必要）。

### 1.4 失败条件

- 对旧 `office-word/` **原地改文件 / 局部替换**（易漏改、残留 `path_contract`/`inspect_*`/旧 SKILL 措辞）。
- 迁移后仍以 `inspect_docx.py` 为主路径，Anthropic 流程沦为附件。
- 为对齐 Genesis 而改写 `comment.py` / `pack.py` / XML 教程，引入与 Anthropic 分叉的维护面。
- 回退到 `_office_common` 硬编码共享，破坏「第三方技能自包含 drop-in」。
- 在正文出现 `office-basic` 镜像名、`INPUT_DIR`/`OUTPUT_DIR` 强制契约、「Genesis 硬约束」整节。
- 从 `office-ppt/scripts/office` 拷贝 OOXML 工具链冒充 docx 源（应以 anthropics `docx` 为唯一源；本地 ppt 可能已漂移）。
- 单测仍断言旧资源（`inspect_docx.py`、`validation-checklist.md`）却宣称迁移完成。

### 1.5 「verbatim」的精确边界

| 层级 | 要求 |
| --- | --- |
| `scripts/**`、`LICENSE.txt` | **按字节**对齐 anthropics `docx/` |
| `SKILL.md` 工作流 + Dependencies 列表 | **verbatim**（含原文 `npm install -g` / `pip install` 依赖说明）；不插入 `install_skill_dependencies` / 「禁止 ad-hoc install」等 Genesis 装包纪律 |
| 装包 / 执行纪律 | **仅** `<skill_runtime_bridge>`（及 frontmatter `dependencies.runtime` 声明）；office-ppt / office-word 共用，不按技能复制 |
| frontmatter | Genesis 必需字段（§4）；`description` 用 Anthropic **完整字符串** |

---

## 2. 与 office-ppt 迁移的对齐关系

office-ppt 是已验证试点。office-word **复用同一执行模型**，只替换技能资产：

| 维度 | office-ppt（先例） | office-word（本文） |
| --- | --- | --- |
| 源技能 | Anthropic `pptx` | Anthropic `docx` |
| Genesis 名 | `office-ppt` | `office-word`（保持现名，避免 catalog/文档大面积改动） |
| 内容策略 | 脚本按字节 + 正文/Dependencies verbatim + 装包纪律在 bridge + 自包含 `scripts/office` | **同**（§1.5） |
| 删除自造 | path_contract、inspect、run_pptxgen_script 等 | **整目录删除**旧 `office-word/` 后重建（禁止原地改） |
| 创建栈 | Node `pptxgenjs` | Node `docx`（docx-js，npm 包名 `docx`） |
| 读内容 | `python -m markitdown` | `pandoc` |
| 编辑 | unpack / editing.md / clean / pack | unpack → XML 编辑 → comment.py → pack（教程在 SKILL.md 内；**无**独立 editing.md） |
| 视觉 QA | thumbnail.py + soffice + pdftoppm | soffice + pdftoppm（**无** thumbnail 网格脚本，照搬 Anthropic） |
| 执行入口 | bridge → `run_skill_command(command=原文)` | **同** |
| frontmatter | `allowed-tools` + `dependencies.runtime` | **同构**，依赖集合按 docx 替换 |

**不另造 Word 专用执行模型。** 若执行层仍有 WorkspaceFS 等缺口，归 office-ppt 迁移文档与运行时改造，不在 Word 技能里打补丁。

---

## 3. 源资产清单与迁移动作

### 3.1 直接照搬（verbatim）

| 目标路径（`office-word/`） | 来源（anthropics `docx/`） | 说明 |
| --- | --- | --- |
| `SKILL.md` 正文 | `SKILL.md` 正文 | 工作流 + Dependencies **verbatim**；frontmatter 见 §4；装包纪律见 bridge |
| `LICENSE.txt` | `LICENSE.txt` | 合规一并带上 |
| `scripts/__init__.py` | 同 | verbatim |
| `scripts/accept_changes.py` | 同 | LibreOffice 接受全部修订 |
| `scripts/comment.py` | 同 | 批注 boilerplate；依赖 `scripts/templates/` |
| `scripts/templates/**` | 同 | comments*.xml、people.xml；**按字节** materialize |
| `scripts/office/**` | 同 | pack/unpack/validate/soffice/helpers/validators/schemas **自包含、按字节**；源必须是 anthropics `docx`，禁止从 office-ppt 复制 |

### 3.2 旧包处置：整目录删除再迁入（禁止原地改）

现有仓库内 `office-word` **全部视为过时分叉**，不是可增量升级的基线：

```text
internal/capabilities/skill/adapter/embedded/skills/office-word/
  SKILL.md                         # 旧中文 + 旧 run_skill_command(script=…) 模型
  references/validation-checklist.md
  scripts/inspect_docx.py
  scripts/convert_docx_to_pdf.py
  scripts/path_contract.py
```

**硬约束**：

1. **先删除整个目录** `embedded/skills/office-word/`（含全部子文件），不要逐文件「改成 Anthropic 风格」。
2. **再新建同名目录**，从 `anthropics-skills/skills/docx/` **按字节拷入** §3.1 资产，然后只做 §4 frontmatter（正文 Dependencies 保持 Anthropic）。
3. **同步改测试与引用**：凡硬编码旧路径的测试必须改写，至少包括 `internal/capabilities/skill/adapter/embedded/source_test.go`（当前断言 `inspect_docx.py`、`validation-checklist.md`）。仓库内其它文档对旧脚本的描述在实施切片交叉引用步骤更新，不挡技能落盘。
4. **禁止**在旧文件上 diff 式迁移、禁止保留「暂时留着 inspect 备用」的双轨文件。

理由：旧包与 Anthropic 在执行模型、脚本集合、SKILL 语义上全面分叉；原地改极易残留 `path_contract`、旧 `allowed-tools` 措辞或漏拷 `templates/`/`schemas/`，验收成本高于整删重建。

### 3.3 明确不新增

| 项 | 态度 |
| --- | --- |
| 在旧 `office-word/` 上原地修改 / 局部替换 | **禁止**（见 §3.2） |
| `docxjs.md` / 把创建教程拆出 SKILL | 默认不做；保持 Anthropic 单文件结构 |
| Genesis 包装 `run_docx_script.js` | 不做；与 ppt 终态一致，直接 `node foo.js` |
| 新 `generate_docx` Tool | 禁止 |
| 中文化重写 SKILL 正文 | 禁止（可选后续另开「中文补充 reference」，不得替换主路径） |
| 迁移后仍保留 `inspect_docx.py`「备用」 | **禁止** |

### 3.4 迁移后目录

```text
office-word/
├── SKILL.md              # Anthropic 正文 verbatim + §4 frontmatter
├── LICENSE.txt
└── scripts/
    ├── __init__.py
    ├── accept_changes.py
    ├── comment.py
    ├── templates/        # comments*.xml, people.xml
    └── office/           # 自包含 OOXML 工具链 + schemas
```

---

## 4. frontmatter：最小附加（对齐 office-ppt）

保留 Anthropic 的 `description`、`license`。Genesis 需要的治理字段与 office-ppt **同构**：

```yaml
---
name: office-word
description: >-
  Use this skill whenever the user wants to create, read, edit, or manipulate Word documents (.docx files).
  Triggers include: any mention of 'Word doc', 'word document', '.docx', or requests to produce professional
  documents with formatting like tables of contents, headings, page numbers, or letterheads. Also use when
  extracting or reorganizing content from .docx files, inserting or replacing images in documents, performing
  find-and-replace in Word files, working with tracked changes or comments, or converting content into a
  polished Word document. If the user asks for a 'report', 'memo', 'letter', 'template', or similar deliverable
  as a Word or .docx file, use this skill. Do NOT use for PDFs, spreadsheets, Google Docs, or general coding
  tasks unrelated to document generation.
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
      - name: defusedxml
        import: defusedxml
      - name: lxml
        import: lxml
    node:
      - name: docx
        require: docx
    system:
      - name: pandoc
        command: pandoc
      - name: libreoffice
        command: soffice
      - name: poppler
        command: pdftoppm
---
```

> 实施时 `description` 以 anthropics `docx/SKILL.md` frontmatter **原文粘贴**为准（上表为可读换行，不得删减触发句）。  
> `lxml` 为硬依赖：`scripts/office/validators/*` 与 `validate.py` / `pack.py` 校验路径直接 `import lxml.etree`；漏声明会导致创建后 QA 假失败。

### 4.1 字段说明

| 字段 | 规则 |
| --- | --- |
| `name` | **必须** `office-word`（Genesis catalog / CollisionGuard / 文档约定）；Anthropic 原名 `docx` 不作为 Skill 名注册 |
| `description` | Anthropic **完整**原文；强触发 `.docx` / Word / report / memo / tracked changes / comments |
| `allowed-tools` | 与 office-ppt 对齐；编辑 XML 依赖 `edit_file` / `read_file` / `write_file` |
| `dependencies.runtime` | 声明式；**不含**镜像名；**npm/pip 包**（`docx`/`defusedxml`/`lxml`）缺省走 `install_skill_dependencies` 或镜像预装；**system 命令**（pandoc/soffice/pdftoppm）只能由 Profile/镜像提供 |
| 正文工作流 / Dependencies | **零** Genesis 装包纪律措辞；原文 `npm install -g` 可保留（由 bridge 解释为依赖说明） |
| 装包纪律 | 仅 `<skill_runtime_bridge>` + frontmatter `dependencies.runtime` |

### 4.2 对 SKILL 正文中「Install: npm install -g docx」的处理

**不改正文。** Anthropic Creating / Dependencies 中的 `npm install -g` / `pip install` 视为依赖清单。执行期由 `<skill_runtime_bridge>` 禁止用 `run_skill_command` 执行安装，并导向 `install_skill_dependencies`（仅已声明包）或 profile 补齐。

不得为了装包去改正文里的 docx-js API 教程。

### 4.3 对「Use the Edit tool」的处理

Anthropic 编辑步骤要求用 Edit 工具直接改 XML。Genesis 映射：

- 工具面已有 `edit_file`（及 `read_file`/`write_file`）；
- **不**把正文里的 “Edit tool” 改成 Genesis 工具名；
- 模型经 bridge + `allowed-tools` 使用 `edit_file` 即可。

### 4.4 CollisionGuard 与别名

- Catalog / CollisionGuard 注册名是 `office-word`（与 `office-ppt` 相同模式）。
- 模型若把 Anthropic 原名 `docx` 当 Tool 调用：**不在本迁移强制**增加 `docx`→`office-word` 别名（`pptx`→`office-ppt` 亦未强制）；属可选增强，见 §11。
- 把 `office-word` 当 Tool 调用时，既有 CollisionGuard 必须能命中并提示 `Skill(skill="office-word")`。

---

## 5. 运行时行为（技能不可见）

执行模型权威文档已定义；本文只固定 Word 侧约定：

```text
Skill(skill="office-word", ...)
  → 加载 verbatim 正文 + 注入 <skill_runtime_bridge>

首次脚本：
  run_skill_command(
    skill="office-word",
    command="python scripts/office/unpack.py document.docx unpacked/",
    inputs=[...]   # 可选
  )
  → materialize 完整 office-word 包到 Skill Session 工作目录
  → cwd=工作目录，相对路径 verbatim 可解析

创建示例：
  write_file("$WORK_DIR/create_report.js")   # 顶层 docx-js 脚本
  run_skill_command(
    skill="office-word",
    command="node create_report.js",
    inputs=["$WORK_DIR/create_report.js"]
  )
  → 产物写工作目录相对路径（如 report.docx）
  → validate / pandoc / soffice+pdftoppm 按 SKILL 原文继续
```

### 5.1 Profile

- frontmatter `dependencies.runtime` → ProfileResolver → office 类镜像（`office-basic` 等名称**只存在于注册表**）。
- 文档内嵌扫描图且需 OCR → `office-ocr`（由运行时/意图升级，不写进技能正文）。
- **取消**对 office-word 的「仅 inspect 脚本」特例。

### 5.2 产物与门禁

- 禁止 `write_file` 伪造 `.docx`（binarygate 已有）。
- 交付前 OOXML 门禁；`scripts/office/validate.py` 作为技能内 QA（Anthropic 创建后必跑）。
- 视觉检查：`soffice --convert-to pdf` + `pdftoppm`；缺依赖时如实说明，不伪造预览。

---

## 6. docx 特有能力与约束

### 6.1 从零创建（docx-js）

- 模型按 SKILL 内教程写 **顶层 Node 脚本**（`require('docx')` + `Packer.toBuffer`）。
- 关键规则（保持 Anthropic）：显式纸张尺寸、表格双宽度、`ShadingType.CLEAR`、禁止 unicode 子弹、PageBreak 必须在 Paragraph 内等。
- Genesis 仅约束：中间脚本落 `$WORK_DIR`；最终 `.docx` 写工作目录相对路径；经 `run_skill_command` 执行。

### 6.2 编辑 / 批注 / 修订

- 流程保持 Anthropic 三步：unpack → 改 XML / `comment.py` → pack（`--original` + validate）。
- `comment.py` 默认 `--author Claude`：**照搬**，不改为 Genesis 品牌名（保证与上游教程、模板一致）。企业若需审计作者名，用 `comment.py --author "..."`（技能已支持），由任务提示覆盖，**不改脚本默认值**。
- 修订 XML 中 `w:author="Claude"` 同理：正文要求照搬；用户显式要求时可换名。

### 6.3 接受修订

- `python scripts/accept_changes.py input.docx output.docx` verbatim。
- 依赖 LibreOffice；失败时归类为依赖/环境问题，不回退自研替代实现。

### 6.4 读与预览

| 能力 | 命令（技能原文） |
| --- | --- |
| 文本（含修订） | `pandoc --track-changes=all document.docx -o output.md` |
| 原始 XML | `python scripts/office/unpack.py document.docx unpacked/` |
| `.doc` → `.docx` | `python scripts/office/soffice.py --headless --convert-to docx document.doc` |
| 视觉预览 | `soffice` → PDF → `pdftoppm -jpeg -r 150` |

### 6.5 与 office-ppt 共享脚本的关系

docx 与 pptx 的 `scripts/office/**` 在 Anthropic 侧本就各自一份（高度同源）。Genesis：

- **各自自包含**（与 office-ppt 终态一致）；
- 禁止实施期偷偷改回 `_office_common`；
- 若未来体积成问题，只允许**运行时只读共享缓存**，不得改技能内容结构。

---

## 7. 许可

Anthropic `LICENSE.txt` 为 Proprietary。实施前须确认仓库/产品再分发权利；法务结论若为「不可原样拷贝」，则改为能力对齐重写（本文默认路径是 **copy + 最小 frontmatter**，与 office-ppt 一致）。设计层不绕过该约束。

---

## 8. 实施切片

| 顺序 | 项 | 验收要点 |
| --- | --- | --- |
| 0 | **整目录删除** `embedded/skills/office-word/` | 路径不存在；无残留 `inspect_docx`/`path_contract`/`convert_docx` |
| 1 | 新建空 `office-word/`，按字节拷入 anthropics `docx` 的 scripts/、LICENSE、SKILL 正文 | unpack/pack/comment/accept_changes/templates/schemas 齐全 |
| 2 | 写 frontmatter（§4）；正文 Dependencies 保持 Anthropic | `name=office-word`；runtime 含 `docx`/`defusedxml`/`lxml`/`pandoc`/`libreoffice`/`poppler`；正文无 `install_skill_dependencies` 纪律句 |
| 3 | 核对 office-basic 镜像契约 | 镜像/清单含 pandoc、npm `docx`、lxml、defusedxml（缺则补清单，**不改技能**） |
| 4 | 改写 embedded 单测（勿留旧资源断言） | SystemFS 发现 `office-word`；断言新资源（如 `scripts/comment.py` / `scripts/office/validate.py`），**不再**断言 `inspect_docx.py` |
| 5 | 冒烟：create（docx-js）→ validate；unpack→pack；pandoc 读 | `run_skill_command` + bridge；validate 不因缺 lxml 失败 |
| 6 | 依赖闭环 | 缺 `docx`/`lxml` → `dependency_missing`；可装已声明 npm/pip 包 |
| 7 | 文档交叉引用 | 更新 `Office能力与Skills设计.md` §7.1.1；标注旧迁移设计 §4.2 废止；沙箱 office-basic 清单补项 |

**不在本切片做**：Excel/PDF 迁移、新 Tool、中文 SKILL 重写、`_office_common` 回归、在旧目录上原地 patch。

---

## 9. DoD

1. `Skill(skill="office-word")` 加载后，模型看到的主流程与 Anthropic docx **同构**（create / edit / comment / accept / validate / preview）。
2. 按 SKILL 原文命令经 `run_skill_command` 可完成：从零生成合法 `.docx` → `validate.py` 通过。
3. unpack → 编辑 → pack 多步在同一 Skill Session 工作目录不断链；`scripts/templates/**` 与 `scripts/office/schemas/**` 按字节可用（批注/校验不因 UTF-8 资源读取损坏）。
4. 技能工作流/Dependencies **不出现** Genesis 装包纪律句、镜像名、强制 `INPUT_DIR`/`OUTPUT_DIR`；装包纪律仅在 bridge。
5. 误调用 `tool=office-word` 时 CollisionGuard 返回 `skill_tool_collision`（或等价纠偏）；**不**要求本切片实现 `docx` 别名。
6. `go test` 覆盖 embedded 发现与关键资源可读（断言新资产，无旧 `inspect_docx`）；CLI 冒烟至少一条：docx-js 生成 + validate，或 unpack/pack。
7. 仓库内 **不存在** 旧脚本文件名：`inspect_docx.py`、`convert_docx_to_pdf.py`、`path_contract.py`（在 office-word 下）。

---

## 10. 方案对照

| 方案 | 评价 |
| --- | --- |
| **A. 整删旧包 + Anthropic 重建 + office-ppt 外壳（本文）** | 成熟度最高、无漏改残留 → **采用** |
| B. 在旧 office-word 上原地改 / 局部替换 | 易漏改、双轨残留 → **禁止** |
| C. 保留 inspect + 局部吸收 docx-js（旧迁移设计 §4.2） | 双轨、能力残缺 → **废止** |
| D. 中文重写 + 自研脚本宣称更适合多模型 | 未证明优于 Anthropic → **拒绝** |
| E. 细粒度 `word.*` Tools | 违背 Office 分层 → **禁止** |

---

## 11. 残余风险（接受）

| 风险 | 处置 |
| --- | --- |
| 默认批注/修订作者名为 `Claude` | 照搬；任务级 `comment.py --author` / 用户显式要求覆盖 |
| `accept_changes.py` 使用固定 LibreOffice profile 路径 | 与 Anthropic 相同；沙箱内通常可写；失败则报依赖/环境错 |
| npm 包 `docx` / pip `defusedxml`/`lxml` 缺失 | `install_skill_dependencies` 或镜像预装；**validate 缺 lxml 会直接炸** |
| **system** 依赖 pandoc / soffice / pdftoppm 缺失 | **只能** Profile/镜像补齐；`install_skill_dependencies` 不管系统包；office-basic 契约须显式含 pandoc |
| SKILL.md 很长（内嵌 docx-js 教程） | 接受；与 Anthropic 一致；靠 context 截断策略（若未来拆分须另开设计） |
| Proprietary 许可 | 实施前法务确认；否则改为 rewrite 路线 |
| 模型把 `docx`（Anthropic 名）当 Tool 调用 | 与 `pptx`→`office-ppt` 同级残余；可选后续加别名，本迁移不阻塞 |
| 模型仍可能 `npm install -g` 或 `write_file` 假 docx | bridge + binarygate + 依赖闭环已有；与 ppt 同级残余 |
| 中文场景更偏好 A4，而教程强调 US Letter | Anthropic 已说明 docx-js 默认 A4且要求显式设纸张；保持原文，由任务需求选尺寸 |
| Windows 宿主默认 GBK 导致 `validate.py` 读 XSD 编码失败 | 设 `PYTHONUTF8=1` 或依赖 UTF-8 office 镜像；可选后续由 runner 注入（不改技能脚本） |

---

## 12. 与其它文档的关系

| 文档 | 关系 |
| --- | --- |
| `通用第三方Skill执行模型与office-ppt迁移设计.md` | **执行模型权威**；本文是 Word 资产迁移专项 |
| `Anthropic-Office-Skills完整能力迁移设计.md` §4.2 / 共享方案 A | **对 office-word 废止**；以本文 + ppt 终态（自包含）为准 |
| `Office能力与Skills设计.md` §7.1.1 DOCX 行 | 实施切片第 7 步应改为「按本文迁移」，删除「后续独立实现 Genesis 版」的过时结论 |
| `2026-07-10-office-ppt-from-scratch-anthropic-aligned.md` | PPT 从零生成专项；Word 无对等 JSON-DSL 弯路，直接 docx-js 顶层脚本 |

---

## 13. 审查记录（review-fix-rereview）

| 轮次 | 从第一性原理的发现 | 处置 |
| --- | --- | --- |
| R1 | 「verbatim」与 office-ppt 实装（Dependencies 已改写）矛盾，实施会左右摇摆 | 新增 §1.5 精确边界；TL;DR/DoD 同步 |
| R1 | 目标路径反斜杠笔误；`description` 用 `...` 截断有歧义 | 修正路径；frontmatter 给完整 description 并注明以源文件粘贴为准 |
| R1 | 把 pandoc 写成可走 `install_skill_dependencies` 不成立 | 区分 npm/pip vs system；§4.1/§11 修正 |
| R1 | DoD 要求 `tool=docx` 纠偏，但 CollisionGuard 只认 catalog 名，且 ppt 也未做 `pptx` 别名 | §4.4 + DoD#5：本切片只保证 `office-word`；`docx` 别名列残余 |
| R1 | 未强调源必须是 anthropics docx、模板/schemas 按字节 | §0/§1.4/§3.1/DoD#3 补强 |
| R2 | `validate`/`pack` 依赖 `lxml`，frontmatter 未声明 → 创建后 QA 必挂 | runtime.python 增加 lxml；切片/残余风险同步 |
| R2 | Creating 节 `npm install -g` 不在 Dependencies 节，§1.5 边界过窄 | 允许删除/改写该 Install **一行**；§3.1/§4.2 对齐 |
| R2 | office-basic 镜像清单未覆盖 pandoc/docx/lxml，实施易漏 | 切片新增「核对镜像契约」；交叉引用沙箱文档 |
| R3 | §4.1 仍写「仅 Dependencies 节可改」与 §1.5 冲突 | 改为「Dependencies + Creating Install 一行」 |
| R3 | 无新的正确性/边界问题 | 曾收敛 |
| R4 | 用户指出旧 office-word 应整删再迁，原地改易漏 | §0/§1.4/§3.2/§8/§9/§10 升级为「整目录删除→重建」硬约束；单测旧路径一并改写 |
| 实施 | 2026-07-14 已落地：整删旧包、迁入 anthropics docx、frontmatter/Dependencies、单测与交叉文档 | 冒烟：docx-js 生成 + `PYTHONUTF8=1` 下 validate 通过 + pandoc 可读 |
| 实施复查 | Windows 默认 GBK 下 validate 可能因 XSD 读入报编码错 | **残余**：宿主需 UTF-8（`PYTHONUTF8=1`）或依赖 office 镜像 UTF-8 locale；不改正文脚本 |
| R5 | 装包纪律写进各 Skill Dependencies 与 bridge 重复，破坏 verbatim | 正文恢复 Anthropic Dependencies；纪律强化进 `<skill_runtime_bridge>`；office-ppt 同步 |

**可选 polish（不阻断收敛）**：为 `docx`/`pptx` 增加 CollisionGuard 别名；中文补充 reference；运行时只读共享 `scripts/office`；`office-ppt` 补声明 `lxml`；在 runner 注入 `PYTHONUTF8=1`。

**残余风险**：见 §11；另接受 Windows 非 UTF-8 locale 下 schema validate 编码问题（镜像内通常无此问题）。

---
