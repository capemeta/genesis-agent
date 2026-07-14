# Anthropic Office Skills → Genesis 完整能力迁移设计

> 状态：部分废止 / 被后续文档取代  
> 日期：2026-07-09（决策更新 2026-07-10；**2026-07-14 标注废止范围**）  
> 文档名建议简称：`Office Skills 完整能力迁移`  
> 源仓库：`D:\workspace\go\go-project\anthropics-skills\skills\{pptx,docx,xlsx,pdf}`  
> 目标：`internal/capabilities/skill/adapter/embedded/skills/{office-ppt,office-word,office-excel,office-pdf}`  
> 关联：`docs/Office能力与Skills设计.md`、`docs/沙箱API对接与Profile选择规则.md`、`docs/superpowers/specs/2026-07-09-skill-script-execution-design.md`
>
> **权威替代（勿再按本文实施）**：
>
> - 执行模型与 office-ppt：`docs/通用第三方Skill执行模型与office-ppt迁移设计.md`（自包含、verbatim、废止 `_office_common` / path_contract 改写）
> - office-word / docx：`docs/office-word-docx技能迁移设计.md`（**§4.2 及「保留 inspect/path_contract」已废止**；整删旧包后 verbatim 迁入）
> - office-pdf / pdf：`docs/office-pdf技能迁移设计.md`（**§4.4 / Phase 4「保留 inspect + path_contract 改写」已废止**；整删 `pdf-review` 后 verbatim 迁入 `office-pdf`）
> - office-excel / xlsx：`docs/office-excel-xlsx技能迁移设计.md`（**§4.3「保留 inspect + 合并 recalc_xlsx」已废止**；整删旧包后 verbatim 迁入）

### 已确认决策（2026-07-10）


| 议题                  | 决策                                                                                                              |
| ------------------- | --------------------------------------------------------------------------------------------------------------- |
| 共享 `scripts/office` | **方案 A**：pptx/docx/xlsx 三份字节级一致 → 单份 `_office_common/scripts/office`；materialize / ListResources 合并到 `office-`* |
| 许可策略                | **原样迁移 + 最小路径契约适配**；仅在有明确优化点时改写（如 `INPUT_DIR`/`OUTPUT_DIR`/`run_skill_command`）                                  |
| Phase 1 范围          | `office-ppt`：专有脚本 + references + SKILL.md + `_office_common` 接线                                                 |


## 1. 目标与非目标

### 1.1 目标

把 Anthropic 官方 `pptx` / `docx` / `xlsx` / `pdf` 四个 Skill 的**完整业务能力**迁入 Genesis 内置 Office Skills，使模型在 Genesis 上具备同等的：

- 从零生成（PPT/Word/Excel/PDF）
- 基于模板/已有文件编辑（unpack → 改 → pack）
- 读内容 / 结构检查 / 公式重算 / 表单填充
- 视觉预览与 QA 闭环（LibreOffice + Poppler / 缩略图）
- 领域流程知识（设计规范、财务模型约定、表单流程等）

同时必须适配 Genesis 已落地的执行契约：

- 入口：`run_skill_command`（禁止默认 `python -c` / 假 `write_file` 冒充交付物）
- 路径：`INPUT_DIR` / `OUTPUT_DIR` / `WORK_DIR` / `SKILL_DIR` / `TMPDIR`
- Profile：`office-basic` / `office-ocr`（按内容与操作，不按扩展名）
- Materialize：embed 与磁盘 Skill 同一 Runner

### 1.2 非目标（本迁移不做）

- 不把 Anthropic Skill 名注册成 Tool（仍走 `Skill(skill="office-ppt")`）。
- 不在 Go 代码里直接 import Office 库；复杂处理继续是 Skill scripts。
- 不一次引入 Microsoft Graph / Google Drive（仍属 MCP，另案）。
- 不把 Anthropic 的 Claude 专属 subagent 视觉 QA 协议原样搬进 Genesis；保留「预览图 + 清单」语义，产品侧再接多模态检查。

### 1.3 许可注意（硬约束）

Anthropic skills 声明 `license: Proprietary`（见各 Skill `LICENSE.txt`）。迁移前必须确认：

1. 仓库/产品是否有权使用与再分发这些脚本与文档；
2. 若仅作内部参考，应**重写**等价脚本与教程，而不是原样拷贝；
3. 本文按「能力对齐」设计；实施时以法务结论选择 **copy / rewrite / hybrid**。

---

## 2. 名称与映射


| Anthropic Skill | Genesis 内置 Skill（保持现名） | 说明                                   |
| --------------- | ---------------------- | ------------------------------------ |
| `pptx`          | `office-ppt`           | 不改名，避免 Profile/allowed-tools/文档大面积改动 |
| `docx`          | `office-word`          | 同上                                   |
| `xlsx`          | `office-excel`         | 同上                                   |
| `pdf`           | `office-pdf`           | **已迁**：取代旧名 `pdf-review`；见 `docs/office-pdf技能迁移设计.md` |


资源 ID 约定（迁移后）：

```text
office-ppt/scripts/<name>.py
office-ppt/references/<name>.md
office-word/scripts/...
office-excel/scripts/...
pdf-review/scripts/...   # 废止：现为 office-pdf/
```

共享 OOXML 工具抽到**单一物理副本**（已确认采用方案 A）：

```text
方案 A（已采用）：embedded 内建共享包
  internal/capabilities/skill/adapter/embedded/skills/_office_common/scripts/office/...
  各 Skill materialize 时把 _office_common 合并进 SKILL_DIR/scripts/office
  ListResources 对 office-* 叠加共享资源视图；catalog 跳过 _ 前缀包

方案 B（未采用）：每个 Skill 各带一份 office/（与 Anthropic 现状一致，体积大、易漂移）
```

---

## 3. 能力差距总览


| 能力域             | Anthropic                                        | Genesis 现状                         | 差距                  |
| --------------- | ------------------------------------------------ | ---------------------------------- | ------------------- |
| PPT 从零生成        | `pptxgenjs.md` + Node pptxgenjs                  | 仅流程文字，无教程/脚本                       | **大**               |
| PPT 模板编辑        | unpack/add_slide/clean/pack + `editing.md`       | 无                                  | **大**               |
| PPT 预览          | `thumbnail.py` 网格 + soffice/pdftoppm             | `render_pptx_preview.py`           | 中（缺网格缩略图）           |
| PPT 校验          | validators + XSD                                 | 轻量 `inspect_pptx.py` + 运行时门禁       | 中                   |
| Word 从零生成       | `docx-js` 教程                                     | 无                                  | **大**               |
| Word 编辑/批注/修订   | unpack/pack + `comment.py` + `accept_changes.py` | inspect + convert_to_pdf           | **大**               |
| Excel 生成/模型规范   | SKILL 内完整财务/格式约定 + openpyxl 流程                   | **已 verbatim** → `office-excel`（见 excel 迁移设计） | 已关闭                 |
| Excel 重算        | `recalc.py`                                      | **已照搬** `scripts/recalc.py`（废止 `recalc_xlsx`） | 已关闭                 |
| PDF 读/合并/拆分/水印等 | SKILL + reference 大量示例                           | inspect/extract/list_fields/render | **大**               |
| PDF 表单          | `forms.md` + 多脚本                                 | 仅 list fields                      | **大**               |
| 执行契约            | 直接 shell/`python scripts/...`                    | `run_skill_command` + workspace     | Genesis 更强，迁移时要改写入口 |


结论：**业务能力差距大；执行底座 Genesis 已具备。迁移重点是资产与 SKILL.md 流程，不是再造 Runner。**

---

## 4. 源资产清单（按 Skill）

### 4.1 `pptx` → `office-ppt`


| 类型  | 源路径                                                                            | 迁移动作                                                          |
| --- | ------------------------------------------------------------------------------ | ------------------------------------------------------------- |
| 主文档 | `SKILL.md`                                                                     | 合并进 `office-ppt/SKILL.md`：保留 Genesis 硬约束 + Anthropic 设计/QA 精华 |
| 教程  | `pptxgenjs.md`、`editing.md`                                                    | → `references/pptxgenjs.md`、`references/editing.md`           |
| 脚本  | `scripts/thumbnail.py`、`add_slide.py`、`clean.py`                               | → `scripts/`，改路径契约                                            |
| 共享  | `scripts/office/**`（unpack/pack/validate/soffice/helpers/validators + schemas） | → 共享或本包 `scripts/office/`                                     |
| 已有  | Genesis `inspect_pptx.py`、`render_pptx_preview.py`、`path_contract.py`          | **保留**；与 thumbnail/preview 去重或互相调用                            |


### 4.2 `docx` → `office-word`

> **【已废止 2026-07-14】** 下表「保留 inspect / path_contract」不再执行。  
> 权威方案见 `docs/office-word-docx技能迁移设计.md`：整目录删除旧 `office-word/` → 按字节迁入 Anthropic `docx` → 仅 frontmatter / Dependencies 最小适配。


| 类型  | 源路径                                                           | 迁移动作（废止，勿用）              |
| --- | ------------------------------------------------------------- | ------------------------ |
| 主文档 | `SKILL.md`（含 docx-js 创建、XML 编辑）                               | ~~合并进 `office-word/SKILL.md`~~ |
| 脚本  | `comment.py`、`accept_changes.py`                              | ~~→ `scripts/`~~         |
| 共享  | `scripts/office/**`                                           | ~~同 pptx / `_office_common`~~ |
| 已有  | `inspect_docx.py`、`convert_docx_to_pdf.py`、`path_contract.py` | ~~保留并对齐~~ → **删除后重建** |


### 4.3 `xlsx` → `office-excel`（**废止本节旧迁移动作**）

> **勿按本节实施。** 权威：`docs/office-excel-xlsx技能迁移设计.md`。  
> 正确路径：整删 `office-excel/` → 按字节迁入 Anthropic `xlsx` → frontmatter 最小附加；**禁止**保留 inspect、`recalc_xlsx` 合并对齐、path_contract。

| 类型  | 源路径 | 现迁移动作（以 office-excel 设计为准） |
| --- | --- | --- |
| 主文档 | `SKILL.md` | 正文 verbatim；frontmatter 最小附加 |
| 脚本  | `recalc.py`、`scripts/office/**` | 按字节拷入；保留原名 `recalc.py` |
| 旧包  | `inspect_xlsx` / `recalc_xlsx` / `path_contract` / validation-checklist | **整目录删除**，不保留 |


### 4.4 `pdf` → `office-pdf`（**废止本节旧迁移动作**）

> **勿按本节实施。** 权威：`docs/office-pdf技能迁移设计.md`。  
> 正确路径：整删 `pdf-review/` → 按字节迁入 Anthropic `pdf` → 注册为 `office-pdf`；**禁止**保留 inspect JSON 门面、path_contract、把 forms 改写进 `references/` 并塞进 `run_skill_command(script=…)`。

| 类型  | 源路径 | 现迁移动作（以 office-pdf 设计为准） |
| --- | --- | --- |
| 主文档 | `SKILL.md`、`forms.md`、`reference.md` | 正文 verbatim；落盘 `FORMS.md`/`REFERENCE.md`；frontmatter 最小附加 |
| 脚本  | 全部 `scripts/*.py` | 按字节拷入；不改 path_contract |
| 旧包  | `pdf-review/**` | **整目录删除**，不保留 |


---

## 5. Genesis 适配规则（迁移时必须改写）

### 5.1 执行入口


| Anthropic 写法                         | Genesis 写法                                                                                                      |
| ------------------------------------ | --------------------------------------------------------------------------------------------------------------- |
| `python scripts/thumbnail.py a.pptx` | `run_skill_command(skill="office-ppt", script="office-ppt/scripts/thumbnail.py", args=["a.pptx"], inputs=[...])` |
| `python -m markitdown x.pptx`        | 优先封装为 skill 脚本或声明依赖后由 `run_skill_command` 调包装脚本；禁止脆弱 `python -c`                                                 |
| `npm`/`npx` 写 pptxgenjs              | 生成脚本写入 `WORK_DIR` 或临时 JS，经 `run_skill_command`/`run_command`（受审批）执行；产物只进 `OUTPUT_DIR`                            |
| 直接改宿主机路径                             | 一律 `INPUT_DIR`/`OUTPUT_DIR`/`SKILL_DIR`                                                                         |


### 5.2 路径与工作空间

所有迁入脚本必须：

1. `from path_contract import resolve_input_path, resolve_output_dir`（或共享等价模块）；
2. 不写死 `/tmp`、盘符、项目根作为最终产物目录；
3. stdout 优先结构化 JSON：`ok` / `errors` / `warnings` / `artifacts` / `recommended_profile`。

### 5.3 Profile


| 操作                    | Profile                |
| --------------------- | ---------------------- |
| 常规生成/编辑/预览/文本抽取       | `office-basic`         |
| 扫描件、无文本层、明确 OCR、读图中文字 | `office-ocr`           |
| 纯通用脚本（极少）             | `skill-polyglot-basic` |


`SkillScriptService` 后续应消费：

- 脚本名 / operation（`ocr_*` → office-ocr）；
- inspect 输出的 `recommended_profile`；
- 用户意图中的 OCR 关键词。

本迁移文档要求：**脚本侧先输出推荐；Runner 侧第二阶段自动升级。**

### 5.4 依赖声明（SKILL frontmatter）

迁入后各 Skill `dependencies` 至少覆盖：


| Skill        | 典型依赖                                                                                                              |
| ------------ | ----------------------------------------------------------------------------------------------------------------- |
| office-ppt   | `run_skill_command`、`python`、`node`/`pptxgenjs`（生成）、`libreoffice`、`pdftoppm`、可选 `markitdown`                       |
| office-word  | `run_skill_command`、`python`、`node`/`docx`（docx-js）、`libreoffice`、`pandoc`（可选）                                     |
| office-excel | `run_skill_command`、`python`、`openpyxl`/`pandas`、`libreoffice`                                                     |
| pdf-review / office-pdf | 见 `docs/office-pdf技能迁移设计.md` §4：pypdf/pdfplumber/reportlab/pdf2image/Pillow/pytesseract、pdf-lib、pdftoppm/pdftotext/pdfimages/qpdf/tesseract |


依赖安装仍遵守：构建期 / 镜像层安装，运行期默认无网络。

### 5.5 交付物门禁

继续强制：

- `.pptx/.docx/.xlsx/.pdf` 不得由 `write_file`/`edit_file`/`apply_patch` 用纯文本冒充；
- `run_skill_command` 对 `OUTPUT_DIR` 产物做 OOXML/PDF 魔数门禁。

---

## 6. 共享 `scripts/office` 策略

Anthropic 在 pptx/docx/xlsx **各复制了一整份** `scripts/office`（含大量 XSD，单 Skill 约 50+ 文件）。迁移原则：

1. **逻辑上只维护一份** OOXML unpack/pack/validate/soffice/helpers/validators/schemas；
2. 首期若为降低风险可三份并存，但必须标注「同源、禁止分叉修改」；
3. validators 中跨格式文件（docx validator 出现在 pptx 包内）按「共享包」理解，不要在业务 Skill 里再发明第三套。

推荐目录（方案 A）：

```text
embedded/skills/
  _office_common/
    scripts/office/...
  office-ppt/
    SKILL.md
    references/
    scripts/          # ppt 专有：thumbnail, add_slide, clean, inspect, render_...
  office-word/
  office-excel/
  office-pdf/         # Anthropic pdf；无 OOXML office/；见 office-pdf 迁移设计
```

Materialize 规则：执行 `office-ppt` 脚本时，Materializer 将 `_office_common/scripts/office` 合并到 `SKILL_DIR/scripts/office`。

---

## 7. SKILL.md 合并原则

每个 Genesis Skill 的 `SKILL.md` 结构统一为：

1. **Frontmatter**：name/description/allowed-tools/dependencies/products（Genesis 字段）
2. **硬约束**：`run_skill_command`、禁止假交付物、INPUT/OUTPUT 契约
3. **Profile 规则**：office-basic / office-ocr
4. **Quick Reference 表**：任务 → 脚本 resource id（Anthropic 表改写成 Genesis 调用）
5. **领域流程**：从 Anthropic 吸收（设计、财务、表单等），删 Claude 专属措辞，改成 Genesis 工具名
6. **QA 闭环**：生成/编辑 → preview/thumbnail → 对照 checklist → 修复 → 再验证
7. **详细教程**：放到 `references/`，SKILL 正文只留入口链接

---

## 8. 分阶段实施计划

### Phase 0 — 许可与基线（阻塞项）

- [x] 确认 Proprietary 许可是否允许拷贝/改写/分发（产品侧已确认：**允许原样迁移 + 最小路径适配**；见 `_office_common/NOTICE.md`）
- [ ] 冻结源目录版本（commit/tag 或拷贝快照到 `third_party` 参考区）
- [x] 建立能力对照表测试样例（Phase 1：inspect 必跑 smoke；thumbnail/create 缺依赖 skip）

### Phase 1 — PPT 完整能力（优先，用户感知最强）

1. [x] 迁入：`thumbnail`、`add_slide`、`clean` + `_office_common/scripts/office/*`（方案 A）
2. [x] 迁入 references：`pptxgenjs.md`、`editing.md`（入口改为 `run_skill_command`）
3. [x] 重写 `office-ppt/SKILL.md` Quick Reference + 硬约束 + 设计/QA
4. [x] 保留并接线现有 inspect/render；`path_contract` 三端统一签名
5. [x] 单测：materialize 含 office 树；嵌套 `office/unpack.py` 相对路径；gate；catalog 跳过 `_office_common`
6. [x] CLI smoke：`inspect` 真实执行；`thumbnail`/`create_pptx`/`run_pptxgen_script` 缺依赖时 skip
7. [x] `run_pptxgen_script.js`（Anthropic 对齐主路径：顶层 pptxgen JS）+ `create_pptx.js` 降级为 smoke
8. [x] Enterprise：`shared/skillstack` + bootstrap 注入 embed Skills / `run_skill_command` / SharedScriptsFS（远程沙箱仍待后续）

### Phase 2 — Word 完整能力

1. 迁入 comment / accept_changes / office 共享
2. 吸收 docx-js 创建流程到 references
3. 对齐 convert/inspect

### Phase 3 — Excel 完整能力

1. 合并 recalc 脚本与财务/格式规范 references
2. 明确「生成用 openpyxl / 编辑保模板」流程进 SKILL
3. 公式错误扫描与 `recalc` 闭环

### Phase 4 — PDF 完整能力

1. [x] 迁入 forms/reference 与表单脚本（**按** `docs/office-pdf技能迁移设计.md`：整删 `pdf-review` → `office-pdf` verbatim；**不**做「保留 inspect JSON + path_contract」）
2. [x] 废除旧 inspect/extract/render 双轨（整目录删除，不再统一旧 JSON 契约）
3. [ ] OCR 路径：Runner 自动升级 `office-ocr`（仍属运行时残余；技能侧已声明 pytesseract/tesseract）

### Phase 5 — 收敛与产品化

1. [x] `_office_common` 去重（方案 A）
2. [x] Enterprise bootstrap 接线（embed Skills + SharedScriptsFS；headless Ask 自动批准；远程 sandbox/RBAC 仍待）
3. [x] 更新 `docs/Office能力与Skills设计.md` 实现状态表（office-pdf 已对齐）
4. [ ] 可选：Marketplace/Plugin 打包 Office 能力集

---

## 9. 验收标准（Definition of Done）

每个 Skill 达到「Anthropic 能力对齐」需同时满足：

1. **文档**：SKILL + references 覆盖 Anthropic 主路径（生成、编辑、预览、QA）
2. **脚本**：关键脚本可经 `run_skill_command` materialize 并执行（本地或 sandbox）
3. **契约**：无宿主机绝对路径泄漏进 strict 命令；产物进 `OUTPUT_DIR`；门禁生效
4. **Profile**：常规走 `office-basic`；OCR 场景可升级 `office-ocr`
5. **回归**：`allowed_tools` 与 CLI Profile 对齐测试通过；embed SystemFS 测试包含新资源
6. **许可**：法务结论已落实（copy 或 rewrite）

---

## 10. 风险与决策点


| 风险                         | 影响                      | 决策                                         |
| -------------------------- | ----------------------- | ------------------------------------------ |
| Proprietary 许可             | 不能直接拷贝                  | Phase 0 阻塞；否则改为能力重写                        |
| `scripts/office` 体积（XSD）   | embed 体积、materialize 耗时 | 方案 A 共享；或 schemas 按需裁剪                     |
| Node 依赖（pptxgenjs/docx-js） | 本地/沙箱需 Node             | 写入 sandbox profile / 依赖构建；CLI 文档说明         |
| markitdown / pandoc        | 额外依赖                    | 可先用 inspect 替代读路径，再可选增强                    |
| 与现有轻量脚本重复                  | 维护成本                    | 保留 Genesis JSON 契约为对外稳定面，内部可调 Anthropic 实现 |
| OCR 自动升级未接线                | 扫描件走错 profile           | Phase 4 与 Runner 小项一并做                     |


---

## 11. 建议实施顺序（最小闭环）

```text
Phase 0 许可
  -> Phase 1 office-ppt（生成教程 + unpack/pack/thumbnail）
  -> Phase 4 部分：office-ocr 推荐消费（可穿插）
  -> Phase 2 office-word
  -> Phase 3 office-excel
  -> Phase 4 office-pdf（已按 office-pdf 迁移设计落地）
  -> Phase 5 共享收敛与文档对齐
```

---

## 12. 相关路径速查


| 角色         | 路径                                                                   |
| ---------- | -------------------------------------------------------------------- |
| 源 pptx     | `D:\workspace\go\go-project\anthropics-skills\skills\pptx`           |
| 源 docx     | `...\skills\docx`                                                    |
| 源 xlsx     | `...\skills\xlsx`                                                    |
| 源 pdf      | `...\skills\pdf`                                                     |
| 目标         | `internal/capabilities/skill/adapter/embedded/skills/`               |
| 执行设计       | `docs/superpowers/specs/2026-07-09-skill-script-execution-design.md` |
| Profile 规则 | `docs/沙箱API对接与Profile选择规则.md` §5 / §7                                |
| Office 总设计 | `docs/Office能力与Skills设计.md`                                          |


---

## 13. 决策与残余风险（2026-07-10）

### 已确认

1. **许可策略**：原样迁移 + 最小路径契约适配（产品侧确认）。
2. **共享策略**：方案 A（`_office_common`）。
3. **Phase 1**：已编码并接线。

### 已处理的原残余风险


| 项                              | 状态                                                              |
| ------------------------------ | --------------------------------------------------------------- |
| Enterprise 未注入 SharedScriptsFS | 已通过 `shared/skillstack.BuildEmbedded` + Enterprise bootstrap 接线 |
| CLI smoke                      | `inspect` 必跑；`thumbnail`/`create_pptx`/`run_pptxgen_script` 缺依赖 skip |
| pptxgenjs 从零生成                 | 默认 `run_pptxgen_script.js`（写顶层 JS）；`create_pptx.js` 仅 smoke；教程见 `pptxgenjs.md` |
| Proprietary 标注                 | `_office_common/NOTICE.md` + 设计文档 Phase 0                       |


### 仍待后续


| 项                                            | 说明                                                                       |
| -------------------------------------------- | ------------------------------------------------------------------------ |
| Enterprise 远程 sandbox / 租户 credential / 人工审批 | 当前本地 Runner + **headless Ask 自动批准（session）**；生产需替换 requester 与远程 sandbox |
| 源版本冻结到 third_party                           | 可选治理项                                                                    |
| thumbnail/create 在 CI 全绿                     | 依赖镜像预装 soffice/Pillow/pptxgenjs                                          |



