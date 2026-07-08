---
name: office-excel
description: 处理 Excel 和表格文件的内置 Skill。适用于读取、清洗、编辑、建模、公式、格式、图表、透视、CSV/TSV 转换和 .xlsx/.xlsm/.csv/.tsv 交付物。用户要求最终交付电子表格，或明确提到 workbook、spreadsheet、Excel、xlsx、csv、tsv 时，应使用本 Skill。
short-description: Excel 表格、公式、模型和验证
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
      description: 在受控执行环境中运行 Excel/OpenXML/LibreOffice 脚本
    - type: command
      value: python
      description: 执行表格检查脚本
    - type: command
      value: libreoffice
      description: 可选，公式重算和打开性校验
context: inline
model: inherit
products:
  - cli
  - desktop
  - enterprise
---

# Office Excel Skill

## 使用时机

使用本 Skill 处理 `.xlsx`、`.xlsm`、`.csv`、`.tsv` 或明确要求电子表格交付物的任务：

- 读取、清洗、重排、合并、拆分表格数据。
- 创建或编辑 workbook、sheet、公式、格式、筛选、图表和模型。
- 处理财务模型、预算表、运营报表、数据质量检查。
- CSV/TSV 与 Excel 之间转换，并保留字段语义。

不要用于最终交付 Word/PPT/HTML 报告的任务；即使中间有表格，也应由目标交付物 Skill 主导。

## Profile 规则

- 结构化表格读取、清洗、公式、格式和图表：`office-basic`。
- 公式重算属于 Office workload，仍使用 `office-basic`，通常需要 LibreOffice。
- 嵌入图片表格、票据照片或截图并需要识别文字时，使用 `office-ocr`。

## 推荐流程

1. 明确输出是否必须是电子表格；如果只是分析结论，不要过度生成 xlsx。
2. 读取 `references/validation-checklist.md`，确认公式、格式和错误检查要求。
3. 对已有文件先运行 `python scripts/inspect_xlsx.py <file>`；CSV/TSV 会给出列数、行数和样例。
4. 公式必须用 Excel 公式表达，避免把可更新模型硬编码成静态数字。
5. 输入文件默认从 `INPUT_DIR` 解析，只传文件名即可；最终工作簿默认写入 `OUTPUT_DIR`，不要覆盖 `INPUT_DIR` 中的原始输入。
6. 使用公式后运行 `python scripts/recalc_xlsx.py <file.xlsx> [output.xlsx] [timeout_seconds]` 重算并检查 `#REF!`、`#DIV/0!`、`#VALUE!`、`#N/A`、`#NAME?`；不传 `output.xlsx` 时写入 `OUTPUT_DIR/<原文件名>`。
7. 修改既有模板时优先保持原有样式和约定。
