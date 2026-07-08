---
name: office-ppt
description: 处理 PowerPoint 演示文稿的内置 Skill。适用于创建、读取、编辑、优化、拆分合并、套用模板、生成讲稿备注、转换和视觉验证 .pptx 文件。用户提到 slides、deck、presentation、PPT 或 .pptx 文件时，应使用本 Skill。
short-description: PPT 创建、编辑、模板和视觉 QA
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
      description: 在受控执行环境中运行 PPT/OpenXML/LibreOffice 脚本
    - type: command
      value: python
      description: 执行 pptx 检查脚本
    - type: command
      value: libreoffice
      description: 可选，转换 PDF 用于预览
    - type: command
      value: pdftoppm
      description: 可选，把 PDF 预览渲染为图片
context: inline
model: inherit
products:
  - cli
  - desktop
  - enterprise
---

# Office PPT Skill

## 使用时机

使用本 Skill 处理 `.pptx` 或明确要求演示文稿交付物的任务，包括：

- 创建 pitch deck、汇报、培训课件、会议演示。
- 读取、摘要、提取幻灯片文字、备注、图片和结构。
- 基于模板编辑、增删页、替换占位符、统一样式。
- 转 PDF/图片做视觉 QA，检查重叠、溢出、低对比和占位符残留。

## Profile 规则

- 普通 PPT 结构、文本、模板、转换和视觉预览：`office-basic`。
- 幻灯片主要是截图、扫描图，或用户要求识别图片文字：`office-ocr`。
- 不按 Python/Node 语言拆 profile；PPT 生成库、LibreOffice 和 Poppler 属于 Office workload。

## 推荐流程

1. 明确受众、场景、页数、主题、品牌限制和输出格式。
2. 如果有模板，先检查缩略图和版式，不要随意覆盖既有设计系统。
3. 读取 `references/validation-checklist.md`，建立内容和视觉 QA 清单。
4. 对已有 `.pptx` 先运行 `python scripts/inspect_pptx.py <file.pptx>`。
5. 输入文件默认从 `INPUT_DIR` 解析，只传文件名即可；最终产物默认写入 `OUTPUT_DIR`。
6. 生成或编辑后，运行 `python scripts/render_pptx_preview.py <file.pptx> [output_dir] [dpi]` 转 PDF 和图片预览；不传 `output_dir` 时写入 `OUTPUT_DIR`。
7. 至少执行一次“转换为 PDF/图片预览 -> 发现问题 -> 修复 -> 再验证”的闭环。

## 设计原则

- 每页应有清晰的信息层级和视觉元素，不要默认生成白底大段项目符号。
- 颜色、图标、图表、图片必须服务主题，不要只为了装饰。
- 不要让文本与图形重叠；不要让标题、脚注、来源和页码互相挤压。
