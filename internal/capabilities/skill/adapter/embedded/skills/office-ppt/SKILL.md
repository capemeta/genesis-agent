---
name: office-ppt
description: 处理 PowerPoint 演示文稿的内置 Skill。适用于创建、读取、编辑、优化、拆分合并、套用模板、生成讲稿备注、转换和视觉验证 .pptx 文件。用户提到 slides、deck、presentation、PPT 或 .pptx 文件时，应使用本 Skill。
short-description: PPT 创建、编辑、模板和视觉 QA
version: 0.3.3
allowed-tools:
  - read_file
  - write_file
  - edit_file
  - run_skill_script
  - install_skill_dependencies
  - list_skill_resources
  - read_skill_resource
  - search_skill_resources
dependencies:
  tools:
    - type: tool
      value: run_skill_script
      description: 在受控执行环境中 materialize 并运行 PPT/OpenXML/LibreOffice 脚本
    - type: tool
      value: install_skill_dependencies
      description: 安装本 Skill 声明的 runtime 包（须审批）
    - type: command
      value: python
      description: 执行 pptx 检查、编辑与预览脚本
    - type: command
      value: node
      description: pptxgenjs 从零生成
    - type: command
      value: libreoffice
      description: 转 PDF（soffice；office-basic 镜像通常含）
    - type: command
      value: pdftoppm
      description: PDF 转预览图（Poppler；office-basic 镜像通常含）
  runtime:
    node:
      - name: pptxgenjs
        require: pptxgenjs
    python:
      - name: pillow
        import: PIL
    system:
      - name: libreoffice
        command: soffice
      - name: poppler
        command: pdftoppm
  install_hints:
    - npm install pptxgenjs
    - pip install pillow
context: inline
model: inherit
products:
  - cli
  - desktop
  - enterprise
---

# Office PPT Skill

对齐 Anthropic `pptx`：读参考 → 写脚本/改模板 → **必须 QA**。Genesis 仅换执行入口。

## Quick Reference

| 任务 | 做法 |
|------|------|
| 从零创建 | 读 `references/pptxgenjs.md` + `references/design.md` → `write_file("$WORK_DIR/deck_gen.js")` → `run_pptxgen_script.js` |
| 读/分析 | `inspect_pptx.py` / `extract_pptx_text.py`；总览用 `thumbnail.py` |
| 模板编辑 | 读 `references/editing.md` |
| 视觉 QA（必须） | `render_pptx_preview.py`（或 `thumbnail.py`）→ 看图 → 修 → 再验证 |

脚本一律：`run_skill_script(skill="office-ppt", script="office-ppt/scripts/…", args=[…], inputs=[…])`。  
`create_pptx.js` 仅 smoke，禁止正式多页交付。

## Genesis 硬约束

1. 只用 `run_skill_script`（禁止 `run_command`/`python -c`/裸 `node` 跑业务脚本）。
2. 禁止 `write_file` 假 `.pptx/.docx/.xlsx/.pdf`；交付物由脚本写入 `OUTPUT_DIR`。
3. 中间脚本写 `$WORK_DIR/…`（禁止仓库根）；`inputs` 可传 `$WORK_DIR/deck_gen.js`；最终 `.pptx` 在 `OUTPUT_DIR`。
4. `script` 必须是可执行 resource id（先 `list_skill_resources`）；禁止把 `path_contract`/`helpers`/`validators`/`schemas` 当入口。
5. `dependency_missing` 且 `retryable`：按 `suggested_install` 装 npm/pip 后**同参重跑**。`soffice`/`pdftoppm` 属 system 依赖（不可对话期安装）；缺则按返回说明环境缺口，**勿假装已做视觉 QA**，也**勿臆测当前是沙箱或已预装**。`sandbox_violation` 勿当缺包。
6. Profile：普通用 `office-basic`；截图/OCR 用 `office-ocr`（由运行时选镜像，不是当前会话事实）。

## 读取

1. `inspect_pptx.py` 看页数/媒体/文本。  
2. 需要全文/备注：`extract_pptx_text.py --format markdown`。  
3. 视觉总览：`thumbnail.py`；逐页细看：`render_pptx_preview.py`。

## 从零创建

1. 读 `references/pptxgenjs.md` 与 `references/design.md`。  
2. `write_file("$WORK_DIR/deck_gen.js")`：顶层 pptxgenjs；中文在源码字符串；输出 `path.join(process.env.OUTPUT_DIR||".","Name.pptx")`。  
3. `run_skill_script(..., script="office-ppt/scripts/run_pptxgen_script.js", args=["deck_gen.js"], inputs=["$WORK_DIR/deck_gen.js"])`。  
4. 引用返回的 `artifacts[].path`。  
5. **必须**做下方 QA；未完成至少一轮 fix-and-verify 不得交付。

## 模板编辑

读 `references/editing.md`：`thumbnail`+`inspect` → `unpack` → 改结构/XML → `clean` → `pack` → 再 QA。

## QA（必须）

**默认有问题。** 第一次渲染几乎从不正确；零问题通常说明看得不够仔细。

### 内容 QA

```text
run_skill_script(skill="office-ppt", script="office-ppt/scripts/extract_pptx_text.py",
  args=["output.pptx","--format","markdown"], inputs=["$OUTPUT_DIR/output.pptx"])
```

查缺页、错字、顺序；模板任务再搜 `xxxx`/`lorem`/`ipsum`/`this.*(page|slide).*layout`，有命中先修。

### 视觉 QA

**必须看图，不要只看代码。** 用下方脚本转图（需本机或执行环境 PATH 上有 `soffice`+`pdftoppm`）。有子代理时把预览图交给子代理审查。

```text
run_skill_script(skill="office-ppt", script="office-ppt/scripts/render_pptx_preview.py",
  args=["output.pptx"], inputs=["$OUTPUT_DIR/output.pptx"])
```

产物在 `$OUTPUT_DIR`（`slide-1.png`…）。用 `read_file` 读图，按 `references/validation-checklist.md` 找：重叠、溢出、裁切、间距过近（<0.3"）、边距不足（<0.5"）、低对比、`colW` 右溢出、占位符残留。总览也可用 `thumbnail.py`。

### Verification Loop

1. 生成 → 转图 → 检查  
2. **列出问题**（若没有，再更挑剔地看）  
3. 修 JS/XML → **再验证受影响页**  
4. 重复直到一轮无新问题  

**未完成至少一轮「发现问题 → 修复 → 再验证」前，不得宣称交付成功。**  
同时确认 `ok` 与 `artifacts[].ok`；勿用同一错误参数死循环。
