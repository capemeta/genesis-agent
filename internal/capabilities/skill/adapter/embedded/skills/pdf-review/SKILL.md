---
name: pdf-review
description: 处理 PDF 文件的内置 Skill。适用于读取、摘要、文本或表格抽取、页面预览、拆分合并、表单检查与填充、转换、生成 PDF，以及扫描件或图片文字 OCR。只要用户明确提到 PDF 输入或 PDF 输出，并且任务不是单纯文件搬运，就应优先使用本 Skill。
short-description: PDF 解析、审阅、表单和 OCR 工作流
version: 0.1.0
allowed-tools:
  - read_file
  - write_file
  - edit_file
  - run_skill_command
  - list_skill_resources
  - read_skill_resource
  - search_skill_resources
dependencies:
  tools:
    - type: tool
      value: run_skill_command
      description: 在受控执行环境中 materialize 并运行 PDF 检查、转换、表单和 OCR 脚本
    - type: command
      value: python
      description: 执行 PDF 脚本
    - type: command
      value: pdftotext
      description: 可选，Poppler 文本抽取
    - type: command
      value: pdftoppm
      description: 可选，渲染页面预览
    - type: command
      value: qpdf
      description: 可选，拆分、合并、旋转和解密
context: inline
model: inherit
products:
  - cli
  - desktop
  - enterprise
---

# PDF Review Skill

## 使用时机

使用本 Skill 处理以下任务：

- 读取、摘要、审阅或抽取 PDF 文本、表格、图片、页面结构。
- 合并、拆分、旋转、加水印、加密/解密或生成 PDF。
- 填写或检查 PDF 表单字段。
- 从 Word/PPT/Excel/HTML/Markdown 转换为 PDF 后做可打开性和页面预览检查。
- 识别扫描件、无文本层 PDF、图片 PDF 或拍照文档中的文字。

不要用于普通 `.docx`、`.xlsx`、`.pptx` 编辑；这些应分别使用 Office Word/Excel/PPT Skill，必要时再转换为 PDF 做预览验证。

## Profile 规则

- 普通 PDF 文本、结构、表单、合并拆分、页面预览：`task_type=office`，`runtime_profile=office-basic`。
- 扫描件、无文本层页面、图片文字识别：`task_type=office`，`runtime_profile=office-ocr`。
- OCR 不是 PDF 专属。如果 PDF 是由 Word/PPT/Excel 的截图或扫描图转换而来，也按内容升级到 `office-ocr`。
- 运行期默认无网络；不要在对话中临时安装依赖。

## 硬约束（必须遵守）

1. **执行脚本必须用 `run_skill_command`**，不要用 `run_command` 拼 `python scripts/...`，也不要用 `python -c`。
2. **禁止用 `write_file` 写入 `.pdf` 冒充交付物**。
3. `script` 参数必须是 resource id（如 `pdf-review/scripts/inspect_pdf.py`）。

## 推荐流程

1. 明确输入 PDF、输出目标、是否需要保留版式、是否需要 OCR。
2. 先用 `run_skill_command` 调用 `pdf-review/scripts/inspect_pdf.py` 判断页数、文本层和是否疑似扫描件。
3. 有文本层时优先用文本抽取脚本；表格优先使用 `pdfplumber` 或服务端等价能力。
4. 无文本层或用户明确要求读图片文字时，切换到 `office-ocr`。
5. 修改或生成 PDF 后必须做验证：可打开性、页数、关键文本、页面预览或表单字段；检查返回的 `ok` 与 `artifacts`。

## 常用资源

- 读取本 Skill 的 `references/validation-checklist.md` 获取验收清单。
- 输入文件默认从环境变量 `INPUT_DIR` 解析；只传文件名即可，例如 `report.pdf`。
- 输出文件默认写入环境变量 `OUTPUT_DIR`；不要写入 `/tmp` 或项目根目录作为最终成果。
- `run_skill_command(..., script="pdf-review/scripts/inspect_pdf.py", args=["report.pdf"], inputs=[...])` 获取统一 JSON 诊断。
- `run_skill_command(..., script="pdf-review/scripts/extract_pdf_text.py", ...)` 抽取文本；无文本时切换 OCR。
- `run_skill_command(..., script="pdf-review/scripts/list_pdf_form_fields.py", ...)` 检查可填写表单字段。
- `run_skill_command(..., script="pdf-review/scripts/render_pdf_pages.py", args=["report.pdf"], ...)` 渲染页面预览图。

## 输出要求

最终回复应说明：

- 使用的是普通 PDF 处理还是 OCR。
- 生成或修改的文件路径。
- 验证过的页数、关键文本、表单字段或预览结果。
- 如果 OCR、字体、加密、权限或依赖不可用，要明确失败原因和最小下一步。

