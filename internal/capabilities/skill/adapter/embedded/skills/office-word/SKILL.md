---
name: office-word
description: 处理 Word 文档的内置 Skill。适用于创建、读取、编辑、审阅、重排、批注、接受修订、套用模板、转换和验证 .docx/.doc 文件。用户要求生成报告、合同、备忘录、信函、投标文件或带目录、页眉页脚、表格和图片的专业 Word 文件时，应使用本 Skill。
short-description: Word 文档创建、编辑、审阅和验证
version: 0.1.0
allowed-tools:
  - read_file
  - write_file
  - edit_file
  - run_command
  - read_skill_resource
  - search_skill_resources
dependencies:
  tools:
    - type: tool
      value: run_command
      description: 在受控执行环境中运行 Word/OpenXML/LibreOffice 脚本
    - type: command
      value: python
      description: 执行 docx 检查脚本
    - type: command
      value: libreoffice
      description: 可选，转换、打开性校验和接受修订
context: inline
model: inherit
products:
  - cli
  - desktop
  - enterprise
---

# Office Word Skill

## 使用时机

使用本 Skill 处理 `.docx`、`.doc` 或明确要求 Word 交付物的任务，包括：

- 创建报告、合同、信函、备忘录、投标文件、模板化文档。
- 读取、提取、重排或合并 Word 内容。
- 编辑标题、段落、表格、页眉页脚、图片、目录、样式。
- 处理批注、修订、占位符、模板变量和可打开性验证。
- 将 Word 转 PDF 或图片预览以做版式检查。

不要用于 PDF 原生编辑、电子表格模型或幻灯片设计；必要时调用对应 Skill。

## Profile 规则

- 结构化 Word 处理：`task_type=office`，`runtime_profile=office-basic`。
- 内嵌扫描页、截图、拍照材料并需要识别图片文字：`runtime_profile=office-ocr`。
- 脚本来自 Skill 只记录 `metadata.source=skill`；不要因此降级为 `skill-polyglot-basic`。

## 推荐流程

1. 确认交付格式、纸张尺寸、语言、模板、品牌规范和是否要保留现有样式。
2. 读取 `references/validation-checklist.md`，先建立验收点。
3. 对已有 `.docx` 先运行 `python scripts/inspect_docx.py <file.docx>`，再决定是 OpenXML 编辑、库生成还是 LibreOffice 转换。
4. 创建新文档时优先使用结构化库或模板，不要把复杂版式拼成纯文本。
5. 修改现有文档时优先保留原模板的样式、编号、页眉页脚和表格宽度。
6. 输入文件默认从 `INPUT_DIR` 解析，只传文件名即可；最终产物默认写入 `OUTPUT_DIR`。
7. 需要视觉验证时运行 `python scripts/convert_docx_to_pdf.py <file.docx> [output_dir]` 转 PDF；不传 `output_dir` 时写入 `OUTPUT_DIR`。
8. 生成后必须验证：可打开性、标题层级、目录、表格、图片、页眉页脚、关键文本和 PDF 预览。

## 模型提示

通用模型容易遗漏版式验证。完成 Word 任务前，必须明确回答“我检查了什么”，不要只说“已生成”。
