# office-excel（xlsx）技能迁移设计

> 状态：**已实施**（2026-07-14；review-fix-rereview 含实施复查）  
> 日期：2026-07-14  
> 原则：**优先 Anthropic 成熟方法**；仅在 Genesis 硬约束处做最小适配。  
> 源：`D:\workspace\go\go-project\anthropics-skills\skills\xlsx`  
> 目标：`internal/capabilities/skill/adapter/embedded/skills/office-excel`  
> 试点先例：`office-ppt` ← Anthropic `pptx`；`office-word` ← Anthropic `docx`  
> 关联：
>
> - `docs/通用第三方Skill执行模型与office-ppt迁移设计.md`（**权威执行模型**：verbatim + Skill Session + bridge）
> - `docs/office-word-docx技能迁移设计.md`（同款「整删旧包 → 照搬」专项先例）
> - `docs/Office能力与Skills设计.md`（Office 能力分层；其中 Excel「inspect + recalc_xlsx」最小版描述**被本文推翻**）
> - `docs/Anthropic-Office-Skills完整能力迁移设计.md` §4.3（**废止**：保留 inspect/`path_contract`、与 `recalc_xlsx` 合并对齐的旧迁移动作）
> - `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`
> - `docs/Skill三模式执行与依赖闭环设计.md`
> - `docs/沙箱API对接与Profile选择规则.md`

---

## 0. TL;DR

把 Anthropic 官方 `xlsx` Skill **按 office-ppt / office-word 同款范式**迁入 `office-excel`：

1. **Skill 内容以 Anthropic 为准**：`scripts/**`、`LICENSE.txt` **按字节照搬**；`SKILL.md` 工作流正文 **verbatim**（含 pandas/openpyxl 示例与财务约定）。装包纪律（禁止 ad-hoc install、走 `install_skill_dependencies`）只放在 **`<skill_runtime_bridge>`**，不写进技能正文。
2. **先整包删除、再全新迁入（硬约束）**：现有 `office-excel/` **全部是旧分叉**（中文硬约束、`script=` resource-id、`inspect_xlsx`/`recalc_xlsx`/`path_contract`、自研 validation-checklist），禁止在目录内原地改文件「修修补补」。必须先删除整个 `embedded/skills/office-excel/`，再从 anthropics `xlsx/` 重建；同步改写依赖旧资源路径的单测与文档引用。
3. **宿主适配不下沉进技能**：执行走已落地的 `run_skill_command(skill, command[, inputs])` + `<skill_runtime_bridge>`；技能正文继续写 `python scripts/recalc.py …` / 内联 `openpyxl`/`pandas` 示例。
4. **frontmatter 最小附加**：`name: office-excel`（保持现名）；`description`/`license` 用 Anthropic **全文**；补 `allowed-tools` + `dependencies.runtime`（与 office-ppt 同构，依赖集合按 xlsx 替换）。
5. **自包含**：本包自带完整 `scripts/office/**`，**不**走 `_office_common`；源取自 **anthropics `xlsx/`**，不要从 `office-ppt` / `office-word` 拷贝（`soffice.py` 等可能已有本地漂移）。

一句话：xlsx 迁移 = **删光旧 office-excel → 照搬 Anthropic xlsx → frontmatter 声明依赖 + bridge 承载装包纪律**，不做原地增量改写，也不保留 `inspect_xlsx`/`recalc_xlsx` 双轨。

---

## 1. 从第一性原理

### 1.1 真问题

用户要的是：**能读、洗、建、改、公式建模、格式化、图表，并可对公式做重算与错误扫描的电子表格交付能力**（`.xlsx` / `.xlsm` / `.csv` / `.tsv`）。  
Anthropic `xlsx` 已在生产环境验证的方法是：

| 任务 | Anthropic 方法 |
| --- | --- |
| 数据分析 / 清洗 / 简单导出 | **pandas**（`read_excel` / `to_excel` / `describe` 等） |
| 从零创建 / 复杂格式 / 公式 | **openpyxl**（Workbook、样式、公式字符串） |
| 编辑已有工作簿 | **openpyxl** `load_workbook`（保留公式与格式） |
| 公式重算 + 错误扫描 | `scripts/recalc.py`（LibreOffice macro + openpyxl 扫 `#REF!` 等） |
| 财务模型约定 | SKILL 正文内嵌（蓝输入/黑公式、数字格式、假设外置、零公式错误） |

这些不是「细粒度 Excel Tool」，而是**可移植技能包**。Genesis 不应再自研一套 `inspect_xlsx` JSON 契约 + 改名 `recalc_xlsx` 去替代它。

### 1.2 现状失败真正缺什么

| 已有 | 缺口 |
| --- | --- |
| `office-excel` 最小中文 Skill + `inspect_xlsx.py` / `recalc_xlsx.py` + `path_contract` | **没有** Anthropic 级财务模型约定、公式优先纪律、`recalc.py` JSON 契约与成熟教程 |
| office-ppt / office-word 已按 verbatim 范式落地 | office-excel 仍停在「Genesis 自研最小版 + 宿主泄漏进正文」 |
| `run_skill_command` + bridge + materialize | Excel 侧资产未对齐，底座空转 |

根因不是 Runner 不够，而是 **office-excel 内容资产仍是分叉占位，不是 Anthropic xlsx**。

旧 `recalc_xlsx.py` 与 Anthropic `recalc.py` **不是「可合并的小差异」**：

- 旧脚本依赖 `path_contract`、自研 XML 扫错、与 LibreOffice 调用方式分叉；
- Anthropic 脚本走 `office.soffice` 环境、宏注入、`openpyxl` 扫错、稳定 JSON 输出。

合并对齐只会制造第三套实现。正确动作是：**整删后只留 Anthropic `recalc.py`**。

### 1.3 非目标（本迁移不做）

- 不把 `office-excel` / `xlsx` 注册为 LLM Tool。
- 不在 Go 中绑定 `openpyxl` / `pandas`。
- 不引入 Microsoft Graph / Google Sheets API（MCP 另案）。
- 不把 SKILL 正文改写成 `run_skill_command(...)` 示例。
- 不为「中文友好」重写 Anthropic 教程（与 office-ppt 一致：英文成熟提示优先）。
- 不强制把财务模型节拆成 `references/financial-model.md`（Anthropic 本身内嵌；拆分会破坏触发与上下文完整性，除非后续证明必要）。
- 不保留 `inspect_xlsx.py`「统一诊断」双轨，也不把 `recalc.py` 改名为 `recalc_xlsx.py`。

### 1.4 失败条件

- 对旧 `office-excel/` **原地改文件 / 局部替换**（易漏改、残留 `path_contract`/`inspect_*`/`recalc_xlsx`/旧 SKILL 措辞）。
- 迁移后仍以 `inspect_xlsx.py` 为主路径，Anthropic 流程沦为附件。
- 「合并」`recalc_xlsx.py` 与 `recalc.py`，制造 Genesis 分叉重算实现。
- 为对齐 Genesis 而改写 `recalc.py` / `soffice.py` 读 `INPUT_DIR`/`OUTPUT_DIR`。
- 回退到 `_office_common` 硬编码共享，破坏「第三方技能自包含 drop-in」。
- 在正文出现 `office-basic` 镜像名、强制 `INPUT_DIR`/`OUTPUT_DIR` 契约、「Genesis 硬约束」整节、`script=` resource-id 示例。
- 从 `office-ppt`/`office-word` 的 `scripts/office` 拷贝冒充 xlsx 源（应以 anthropics `xlsx` 为唯一源）。
- 单测仍断言旧资源（`inspect_xlsx.py`、`recalc_xlsx.py`、`validation-checklist.md`）却宣称迁移完成。

### 1.5 「verbatim」的精确边界

| 层级 | 要求 |
| --- | --- |
| `scripts/**`、`LICENSE.txt` | **按字节**对齐 anthropics `xlsx/` |
| `SKILL.md` 工作流正文 | **verbatim**（含 pandas/openpyxl 示例、财务约定、`python scripts/recalc.py`）；不插入 Genesis 装包纪律 |
| 装包 / 执行纪律 | **仅** `<skill_runtime_bridge>`（及 frontmatter `dependencies.runtime` 声明）；与 ppt/word 共用，不按技能复制 |
| frontmatter | Genesis 必需字段（§4）；`description` 用 Anthropic **完整字符串** |

---

## 2. 与 office-ppt / office-word 迁移的对齐关系

office-ppt 是已验证试点；office-word 是同款「整删重建」先例。office-excel **复用同一执行模型**，只替换技能资产：

| 维度 | office-ppt | office-word | office-excel（本文） |
| --- | --- | --- | --- |
| 源技能 | Anthropic `pptx` | Anthropic `docx` | Anthropic `xlsx` |
| Genesis 名 | `office-ppt` | `office-word` | `office-excel`（保持现名） |
| 内容策略 | 脚本按字节 + 正文 verbatim + 装包纪律在 bridge + 自包含 `scripts/office` | **同** | **同**（§1.5） |
| 删除自造 | path_contract、inspect、包装脚本等 | **整目录删除**后重建 | **整目录删除**后重建（禁止原地改） |
| 创建栈 | Node `pptxgenjs` | Node `docx` | **Python `openpyxl`**（主路径）；简单表也可用 pandas |
| 读/分析 | `python -m markitdown` | `pandoc` | **pandas**（正文示例）；无 markitdown/pandoc 硬依赖 |
| 编辑 | unpack / editing.md / pack | unpack → XML → comment → pack | **openpyxl load/save**（正文流程）；`scripts/office` 仍自包含照搬，供 OOXML 级操作可选 |
| 公式闭环 | N/A（演示文稿） | N/A | **`scripts/recalc.py`**（LibreOffice + JSON 错误报告） |
| 视觉 QA | thumbnail + soffice + pdftoppm | soffice + pdftoppm | **非主路径**；Anthropic xlsx **不**要求 pdftoppm；交付靠公式零错误 + 模型约定 |
| 执行入口 | bridge → `run_skill_command(command=原文)` | **同** | **同** |
| frontmatter | `allowed-tools` + `dependencies.runtime` | **同构** | **同构**，依赖集合按 xlsx（`openpyxl`/`pandas`/`defusedxml`/`libreoffice`；**无**强制 `lxml`） |

**不另造 Excel 专用执行模型。** 若执行层仍有 WorkspaceFS 等缺口，归 office-ppt 迁移文档与运行时改造，不在 Excel 技能里打补丁。

---

## 3. 源资产清单与迁移动作

### 3.1 直接照搬（verbatim）

| 目标路径（`office-excel/`） | 来源（anthropics `xlsx/`） | 说明 |
| --- | --- | --- |
| `SKILL.md` 正文 | `SKILL.md` 正文 | 工作流 + 财务约定 + 库示例 **verbatim**；frontmatter 见 §4 |
| `LICENSE.txt` | `LICENSE.txt` | 合规一并带上 |
| `scripts/recalc.py` | 同 | 公式重算主入口；**禁止**改名为 `recalc_xlsx.py` |
| `scripts/office/**` | 同 | pack/unpack/validate/soffice/helpers/validators/schemas **自包含、按字节**；源必须是 anthropics `xlsx`，禁止从 ppt/word 复制 |

> 注：Anthropic `xlsx` 根下 **没有** `scripts/__init__.py`、没有独立 `editing.md` / `references/`。**不要发明**这些文件「补齐结构」。

### 3.2 旧包处置：整目录删除再迁入（禁止原地改）

现有仓库内 `office-excel` **全部视为过时分叉**，不是可增量升级的基线：

```text
internal/capabilities/skill/adapter/embedded/skills/office-excel/
  SKILL.md                         # 旧中文 + 旧 run_skill_command(script=…) 模型
  references/validation-checklist.md
  scripts/inspect_xlsx.py
  scripts/recalc_xlsx.py
  scripts/path_contract.py
```

**硬约束**：

1. **先删除整个目录** `embedded/skills/office-excel/`（含全部子文件），不要逐文件「改成 Anthropic 风格」。
2. **再新建同名目录**，从 `anthropics-skills/skills/xlsx/` **按字节拷入** §3.1 资产，然后只做 §4 frontmatter。
3. **同步改测试与引用**：凡硬编码旧路径的测试必须改写（至少 `source_test.go` 应对齐新资源如 `scripts/recalc.py` / `scripts/office/soffice.py`，**不再**断言 `inspect_xlsx.py`）。仓库内其它文档对旧脚本的描述在实施切片交叉引用步骤更新，不挡技能落盘。
4. **禁止**在旧文件上 diff 式迁移；**禁止**保留「暂时留着 inspect 备用」或「recalc_xlsx 别名包装」的双轨文件。

理由：旧包与 Anthropic 在执行模型、脚本集合、SKILL 语义、重算实现上全面分叉；原地改极易残留 `path_contract`、旧 `allowed-tools` 措辞或漏拷 `scripts/office/schemas/`，验收成本高于整删重建。

### 3.3 明确不新增

| 项 | 态度 |
| --- | --- |
| 在旧 `office-excel/` 上原地修改 / 局部替换 | **禁止**（见 §3.2） |
| 把 `recalc.py` 改名为 `recalc_xlsx.py` 或做别名包装 | **禁止** |
| 迁移后仍保留 `inspect_xlsx.py`「备用」 | **禁止** |
| Genesis 包装 `run_openpyxl_script.py` | 不做；直接按正文写顶层 `.py` 再 `python foo.py` / 或 `python scripts/recalc.py` |
| 新 `generate_xlsx` / `excel.*` Tool | **禁止** |
| 中文化重写 SKILL 正文 | **禁止**（可选后续另开「中文补充 reference」，不得替换主路径） |
| 强制拆出 `references/financial-model.md` | 默认不做；保持 Anthropic 单文件结构 |
| `_office_common` / 从 ppt/word 拷贝 `scripts/office` | **禁止** |
| 旧 Genesis frontmatter 字段回流 | **禁止**回填 `short-description` / `version` / `context` / `model` / `products` / `dependencies.tools`（旧式 tool 依赖列表）；只保留 §4 同构字段 |

### 3.4 迁移后目录

```text
office-excel/
├── SKILL.md              # Anthropic 正文 verbatim + §4 frontmatter
├── LICENSE.txt
└── scripts/
    ├── recalc.py         # 公式重算入口
    └── office/           # 自包含 OOXML 工具链 + schemas
```

---

## 4. frontmatter：最小附加（对齐 office-ppt）

保留 Anthropic 的 `description`、`license`。Genesis 需要的治理字段与 office-ppt **同构**：

```yaml
---
name: office-excel
description: >-
  Use this skill any time a spreadsheet file is the primary input or output. This means any task where the user wants to:
  open, read, edit, or fix an existing .xlsx, .xlsm, .csv, or .tsv file (e.g., adding columns, computing formulas,
  formatting, charting, cleaning messy data); create a new spreadsheet from scratch or from other data sources; or
  convert between tabular file formats. Trigger especially when the user references a spreadsheet file by name or path
  — even casually (like "the xlsx in my downloads") — and wants something done to it or produced from it. Also trigger
  for cleaning or restructuring messy tabular data files (malformed rows, misplaced headers, junk data) into proper
  spreadsheets. The deliverable must be a spreadsheet file. Do NOT trigger when the primary deliverable is a Word
  document, HTML report, standalone Python script, database pipeline, or Google Sheets API integration, even if tabular
  data is involved.
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
      - name: openpyxl
        import: openpyxl
      - name: pandas
        import: pandas
      - name: defusedxml
        import: defusedxml
    system:
      - name: libreoffice
        command: soffice
---
```

> 实施时 `description` 以 anthropics `xlsx/SKILL.md` frontmatter **原文粘贴**为准（上表为可读换行，不得删减触发句）。  
> **依赖分层（勿照搬 word/ppt 的 lxml）**：  
> - `openpyxl` / `pandas`：业务主路径硬依赖；  
> - `defusedxml`：自包含 `scripts/office` 的 unpack/pack/helpers 使用；声明以免可选 OOXML 路径假失败；  
> - `libreoffice`（`soffice`）：`recalc.py` 硬依赖，**只能** Profile/镜像提供；  
> - **`lxml`：不作为 office-excel 硬依赖声明**。上游 `validate.py` 对 `.xlsx` **不支持**（落入 `Validation not supported`）；`pack.py` 对 `.xlsx` 跳过 schema validators。Excel 交付 QA 主路径是 **`recalc.py` JSON**，不是 XSD validate。勿为「与 word 对称」硬加 `lxml`/`pandoc`/`poppler`/`node`。

### 4.1 字段说明

| 字段 | 规则 |
| --- | --- |
| `name` | **必须** `office-excel`（Genesis catalog / CollisionGuard / 文档约定）；Anthropic 原名 `xlsx` 不作为 Skill 名注册 |
| `description` | Anthropic **完整**原文；强触发 spreadsheet / `.xlsx` / `.xlsm` / `.csv` / `.tsv` / 清洗表格 |
| `allowed-tools` | 与 office-ppt 对齐；模型写顶层 Python 脚本依赖 `write_file`/`edit_file`/`read_file` |
| `dependencies.runtime` | 声明式；**不含**镜像名；pip 包缺省走 `install_skill_dependencies` 或镜像预装；**system** `soffice` 只能由 Profile/镜像提供 |
| 正文工作流 | **零** Genesis 装包纪律措辞；零 `run_skill_command` / `office-basic` / `INPUT_DIR` |
| 装包纪律 | 仅 `<skill_runtime_bridge>` + frontmatter `dependencies.runtime` |

### 4.2 对正文「库用法示例」的处理

Anthropic 正文直接给 `import pandas` / `from openpyxl import Workbook` 代码块。**不改正文。**  
执行期由 bridge 把「需要跑的命令」导向 `run_skill_command`；缺包时走 `install_skill_dependencies`（仅已声明包）或 profile 补齐。

不得为了装包去改正文里的 openpyxl/pandas API 教程，也不得把示例改写成 `run_skill_command(script=…)`。

### 4.3 对「顶层 Python 脚本」的处理

创建/编辑工作流常是：Agent 写工作目录下的 `.py` → `python create_model.py` → `python scripts/recalc.py output.xlsx`。

Genesis 映射：

- 脚本落 `$WORK_DIR`（Skill Session 工作目录）；
- 经 `run_skill_command(skill="office-excel", command="python create_model.py")` 等执行；
- **不**引入 Genesis 包装入口脚本。

### 4.4 CollisionGuard 与别名

- Catalog / CollisionGuard 注册名是 `office-excel`。
- 模型若把 Anthropic 原名 `xlsx` 当 Tool 调用：**不在本迁移强制**增加 `xlsx`→`office-excel` 别名（与 `pptx`/`docx` 同级可选增强）。
- 把 `office-excel` 当 Tool 调用时，既有 CollisionGuard 必须能命中并提示 `Skill(skill="office-excel")`。

---

## 5. 运行时行为（技能不可见）

执行模型权威文档已定义；本文只固定 Excel 侧约定：

```text
Skill(skill="office-excel", ...)
  → 加载 verbatim 正文 + 注入 <skill_runtime_bridge>

创建 + 重算示例：
  write_file("$WORK_DIR/create_model.py")   # 顶层 openpyxl 脚本
  run_skill_command(
    skill="office-excel",
    command="python create_model.py",
    inputs=[...]   # 可选
  )
  run_skill_command(
    skill="office-excel",
    command="python scripts/recalc.py output.xlsx 30"
  )
  → materialize 完整 office-excel 包到 Skill Session 工作目录
  → cwd=工作目录，相对路径 verbatim 可解析
  → recalc 返回 JSON（success / errors_found + locations）
```

### 5.1 Profile

- frontmatter `dependencies.runtime` → ProfileResolver → office 类镜像（`office-basic` 等名称**只存在于注册表**）。
- 公式重算仍是 Office workload（LibreOffice），**不是** `skill-polyglot-basic`。
- 表格以嵌入图片/截图/票据照片形式存在且需 OCR → `office-ocr`（由运行时/意图升级，不写进技能正文）。
- **取消**对 office-excel 的「仅 inspect 脚本」特例。

### 5.2 产物与门禁

- 禁止 `write_file` 伪造 `.xlsx`（binarygate 已有）；CSV/TSV 文本除外（与 Anthropic「交付物必须是 spreadsheet」及既有门禁一致）。
- 有公式的交付物：按 SKILL **必须**跑 `scripts/recalc.py`，并根据 JSON 修错至零公式错误——**这是 Excel 主 QA，替代 word/ppt 的 validate.py 角色**。
- 上游 `scripts/office/validate.py` **不支持** `.xlsx`（帮助文案虽提到 xlsx，实现会报 `Validation not supported`）。**禁止**在 Genesis 文档或冒烟里把「xlsx → validate.py」当成验收门禁；仍按字节迁入该脚本（自包含包完整性），但不把它写成 Excel 交付标准。

---

## 6. xlsx 特有能力与约束

### 6.1 公式优先（硬语义，照搬）

- **Always use Excel formulas instead of calculating values in Python and hardcoding them.**
- Genesis **不得**在技能正文弱化该纪律；运行时也不应提供「Python 预计算更省事」的替代教程覆盖主路径。

### 6.2 从零创建 / 编辑（openpyxl + pandas）

- 模型按 SKILL 内示例写 **顶层 Python 脚本** 或在受控命令中执行等价逻辑。
- pandas：分析、清洗、简单导出；openpyxl：公式、格式、多 sheet、保留既有模板风格。
- `data_only=True` 警告（保存会永久丢失公式）：**照搬原文**，不删不改。

### 6.3 公式重算（`scripts/recalc.py`）

- 命令：`python scripts/recalc.py <excel_file> [timeout_seconds]`。
- 行为保持 Anthropic：首次配置 LibreOffice macro、重算全表、扫描错误、输出 JSON。
- 失败时归类为依赖/环境问题，**不**回退到旧 `recalc_xlsx.py` 或自研 XML-only 扫错冒充「已重算」。
- **导入路径不变量（勿「修」）**：`recalc.py` 使用 `from office.soffice import …`。Anthropic 约定以 `python scripts/recalc.py` 启动时，`sys.path[0]` 为 `scripts/`，从而解析 `scripts/office/`。实施时**禁止**为「修好 import」而改脚本、改包结构或要求额外 `PYTHONPATH` 写进技能正文；若 Runner 改变 cwd/启动方式导致 import 失败，修 **Runner**，不改技能。

### 6.4 财务模型约定

- 颜色约定、数字格式、假设外置、硬编码溯源注释等：**全部保留在 SKILL 正文**。
- 不拆独立 reference，除非另开设计证明上下文过长必须拆分。

### 6.5 与 `scripts/office/**` 的关系

xlsx 包内带有与 pptx/docx 同源的 OOXML 工具树。Genesis：

- **各自自包含**（与 office-ppt 终态一致）；
- 禁止实施期偷偷改回 `_office_common`；
- 主业务路径是 openpyxl/pandas + `recalc.py`；`soffice.py` 被 `recalc.py` **实际依赖**，必须按字节迁入；
- unpack/pack/validate/helpers/validators/schemas **仍须整树迁入**（避免半包），但须认知：上游 **validate 不支持 xlsx**；xlsx 的 pack 在 validate 开启时也会跳过 schema validators。勿发明「xlsx SchemaValidator」补丁冒充上游能力。

### 6.6 Windows / 宏路径残余（接受）

Anthropic `recalc.py` 的 LibreOffice macro 目录常量仅覆盖 Darwin / Linux。Windows 宿主或非标准 LibreOffice profile 可能失败——这是**上游技能自身边界**，本迁移不为此改写脚本；优先依赖 office 镜像（Linux）跑通。若产品强制 Windows 本地重算，另开专项设计，不阻塞本迁移。

---

## 7. 许可

Anthropic `LICENSE.txt` 为 Proprietary。实施前须确认仓库/产品再分发权利；法务结论若为「不可原样拷贝」，则改为能力对齐重写（本文默认路径是 **copy + 最小 frontmatter**，与 office-ppt 一致）。设计层不绕过该约束。

---

## 8. 实施切片

| 顺序 | 项 | 验收要点 |
| --- | --- | --- |
| 0 | **整目录删除** `embedded/skills/office-excel/` | 路径不存在；无残留 `inspect_xlsx`/`recalc_xlsx`/`path_contract`/`validation-checklist` |
| 1 | 新建空 `office-excel/`，按字节拷入 anthropics `xlsx` 的 scripts/、LICENSE、SKILL 正文 | `recalc.py` + `scripts/office/**`（含 schemas）齐全；对 `scripts/**` 与源做哈希/逐文件比对一致 |
| 2 | 写 frontmatter（§4）；正文保持 Anthropic | `name=office-excel`；runtime 含 `openpyxl`/`pandas`/`defusedxml`/`libreoffice`；**不含**强制 `lxml`；正文无 Genesis 硬约束/`script=` 示例 |
| 3 | 核对 office-basic 镜像契约 | 镜像/清单含 openpyxl、pandas、defusedxml、LibreOffice（缺则补清单，**不改技能**）；不必为 excel 强加 lxml |
| 4 | 改写 embedded 单测（勿留旧资源断言） | SystemFS 发现 `office-excel`；断言 `scripts/recalc.py` 与 `scripts/office/soffice.py`（recalc 真实依赖），**不再**断言 `inspect_xlsx.py`/`recalc_xlsx.py`/`validation-checklist.md`；若扩展 `smoke_test.go`，同步把旧脚本名列入 forbidden |
| 5 | 冒烟：openpyxl 生成含公式的 xlsx → `python scripts/recalc.py` → JSON `success` 或可定位 `errors_found` | `run_skill_command` + bridge；相对路径可解析；确认 `from office.soffice` 在默认启动方式下可用（§6.3） |
| 6 | 依赖闭环 | 缺 `openpyxl`/`pandas` → `dependency_missing`；可装已声明 pip 包；缺 soffice → 环境/Profile 错误（不可 pip 装） |
| 7 | 文档交叉引用 | 更新 `Office能力与Skills设计.md` 中 Excel 行与 Phase 笔记；在 `Anthropic-Office-Skills完整能力迁移设计.md` 文首权威替代表增加本文（废止 §4.3）；沙箱 office-basic 清单核对 openpyxl/pandas |

**不在本切片做**：PDF 迁移、新 Tool、中文 SKILL 重写、`_office_common` 回归、在旧目录上原地 patch、为 Windows 宏路径改写 `recalc.py`。

---

## 9. DoD

1. `Skill(skill="office-excel")` 加载后，模型看到的主流程与 Anthropic xlsx **同构**（pandas 分析 / openpyxl 创建编辑 / 公式优先 / `recalc.py` / 财务约定）。
2. 按 SKILL 原文命令经 `run_skill_command` 可完成：从零生成含公式的合法 `.xlsx` → `recalc.py` 返回可解析 JSON（**不以** `validate.py` 作为 xlsx 验收）。
3. 多步（写脚本 → 生成 → 重算 → 修错 → 再重算）在同一 Skill Session 工作目录不断链；`scripts/**` 与 anthropics `xlsx/scripts/**` **按字节一致**（含 `office/schemas/**`）。
4. 技能工作流 **不出现** Genesis 装包纪律句、镜像名、强制 `INPUT_DIR`/`OUTPUT_DIR`、`script=` resource-id；装包纪律仅在 bridge。
5. 误调用 `tool=office-excel` 时 CollisionGuard 返回 `skill_tool_collision`（或等价纠偏）；**不**要求本切片实现 `xlsx` 别名。
6. `go test` 覆盖 embedded 发现与关键资源可读（断言新资产，无旧 `inspect_xlsx`/`recalc_xlsx`）；CLI 冒烟至少一条：openpyxl 生成 + recalc。
7. 仓库内 **不存在** 旧脚本/清单文件名：`inspect_xlsx.py`、`recalc_xlsx.py`、`path_contract.py`、`references/validation-checklist.md`（在 office-excel 下）。

---

## 10. 方案对照

| 方案 | 评价 |
| --- | --- |
| **A. 整删旧包 + Anthropic 重建 + office-ppt 外壳（本文）** | 成熟度最高、无漏改残留 → **采用** |
| B. 在旧 office-excel 上原地改 / 局部替换 | 易漏改、双轨残留 → **禁止** |
| C. 保留 inspect + 合并对齐 `recalc_xlsx`（旧迁移设计 §4.3） | 双轨、能力残缺、维护第三套重算 → **废止** |
| D. 中文重写 + 自研脚本宣称更适合多模型 | 未证明优于 Anthropic → **拒绝** |
| E. 细粒度 `excel.*` Tools | 违背 Office 分层 → **禁止** |

---

## 11. 残余风险（接受）

| 风险 | 处置 |
| --- | --- |
| LibreOffice macro 仅 Darwin/Linux 路径常量；上游 SKILL 亦写明 Linux/macOS | 照搬上游；优先 Linux office 镜像；Windows 本地另案 |
| Windows 上 `recalc.py`/`soffice.py`：无 timeout 包装，且 `socket.AF_UNIX` 在 Windows 直接 AttributeError | 上游行为；超时与 shim 靠 Linux 镜像；**禁止**为本机 Windows 改写技能脚本 |
| `openpyxl` / `pandas` / `defusedxml` 缺失 | `install_skill_dependencies` 或镜像预装 |
| **system** `soffice` 缺失 | **只能** Profile/镜像补齐；`install_skill_dependencies` 不管系统包 |
| 模型对 `.xlsx` 误跑 `validate.py` 得到 not supported | 上游行为；技能正文主路径是 recalc；不在 Genesis 侧「补」xlsx validator |
| Proprietary 许可 | 实施前法务确认；否则改为 rewrite 路线 |
| 模型把 `xlsx`（Anthropic 名）当 Tool 调用 | 与 pptx/docx 同级残余；可选后续加别名 |
| 模型仍可能用 Python 硬编码计算结果、或 `write_file` 假 xlsx | SKILL 纪律 + bridge + binarygate；与 ppt 同级残余 |
| 模型跳过 `recalc.py` 直接交付未计算值 | 接受为模型遵从风险；DoD 冒烟覆盖主路径，不在技能内加 Genesis 强制钩子 |
| `.xlsm` 触发但宏工作簿与 headless 重算边界 | 照搬触发描述；复杂 VBA 场景接受为上游能力边界 |
| 大表 pandas 内存 / openpyxl 性能 | 上游已有 read_only/write_only 提示；保持原文 |
| Windows 宿主默认 GBK 导致若误跑 `validate.py` 读 XSD 编码失败 | 与 word 同级宿主问题；Excel 主路径不依赖 validate；镜像 UTF-8 / `PYTHONUTF8=1` |
| Runner 改启动方式导致 `from office.soffice` 失败 | 修 Runner / cwd，不改 `recalc.py`（§6.3） |

---

## 12. 与其它文档的关系

| 文档 | 关系 |
| --- | --- |
| `通用第三方Skill执行模型与office-ppt迁移设计.md` | **执行模型权威**；本文是 Excel 资产迁移专项 |
| `office-word-docx技能迁移设计.md` | **同款整删重建先例**；Excel 对齐其结构与约束措辞 |
| `Anthropic-Office-Skills完整能力迁移设计.md` §4.3 | **对 office-excel 废止**；以本文 + ppt 终态（自包含 verbatim）为准 |
| `Office能力与Skills设计.md` Excel / Phase 笔记 | 实施切片第 7 步应改为「按本文迁移」，删除「inspect + recalc_xlsx 已足够」的过时结论 |
| `沙箱API对接与Profile选择规则.md` | Profile 仍按 workload；实施时核对 office-basic 含 openpyxl/pandas |

---

## 13. 审查记录（review-fix-rereview）

| 轮次 | 从第一性原理的发现 | 处置 |
| --- | --- | --- |
| R1 | 目标路径笔误（`adapter\embedded`）会误导实施落盘位置 | 修正为正斜杠路径 |
| R1 | 旧 Genesis frontmatter（`products`/`dependencies.tools` 等）可能被「习惯性」加回，再次泄漏宿主 | §3.3 明确禁止回流 |
| R1 | `recalc.py` 的 `from office.soffice` 依赖「以 scripts/ 为 sys.path[0]」；实施易误改技能「修 import」 | §6.3 定为不变量：修 Runner 不改技能；切片/残余同步 |
| R1 | Windows 无 timeout 包装、macro 路径仅 Unix 族——文档原先只提 macro 目录，低估上游边界 | §11 补全；优先镜像 |
| R1 | 交叉引用未点名更新旧迁移设计文首权威表，易继续按 §4.3「合并 recalc」实施 | 切片第 7 步显式要求 |
| R2 | §0「可保留库示例」措辞像可选项，与 verbatim 硬要求矛盾 | 改为正文 verbatim（含示例） |
| R2 | 照搬 word 把 `lxml`+`validate.py` 写成 Excel QA，但上游 validate **不支持 .xlsx** | 去掉 lxml 硬依赖；§5.2/§6.5/DoD/切片纠正：主 QA=`recalc.py` |
| R2 | 缺「scripts 按字节比对」验收，易口头宣称 verbatim | 切片#1 + DoD#3 要求哈希/逐文件一致 |
| R3 | 单测验收写「或 validate.py」易把不支持 xlsx 的脚本当成主资产信号 | 改为断言 `recalc.py` + `soffice.py` |
| R3 | §2 frontmatter 依赖集合漏 `defusedxml` / 未点明无 lxml | 对齐 §4 |
| R3 | 无新的正确性/边界/可行性 actionable 项 | 设计曾收敛 |
| 实施 | 2026-07-14 已落地：整删旧包、迁入 anthropics xlsx、frontmatter、source/smoke 单测、交叉文档 | openpyxl 生成冒烟通过；`scripts/**` 哈希对齐源；Windows 宿主 `recalc.py` 因上游 `socket.AF_UNIX` 崩溃（§11 残余） |
| 实施复查 | Windows 本地 LibreOffice 重算不可用；不得为此改写 `soffice.py`/`recalc.py` | **残余**：优先 Linux office 镜像跑 recalc；宿主仅验证资产与 openpyxl 创建 |
| 实施复查 | 旧迁移设计能力差距表仍写「recalc_xlsx 需对齐」易误导后续读者 | 已更新为「已照搬 / 已关闭」 |

**可选 polish（不阻断收敛）**：为 `xlsx` 增加 CollisionGuard 别名；中文补充 reference；运行时只读共享 `scripts/office`；在 runner 注入 `PYTHONUTF8=1`；Windows LibreOffice macro/`AF_UNIX` 专项。

**残余风险**：见 §11（上游 Windows/macro/timeout/`AF_UNIX`、模型跳过 recalc、误跑 validate、许可等）。
