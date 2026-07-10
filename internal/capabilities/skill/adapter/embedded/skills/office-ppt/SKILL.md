---
name: office-ppt
description: 处理 PowerPoint 演示文稿的内置 Skill。适用于创建、读取、编辑、优化、拆分合并、套用模板、生成讲稿备注、转换和视觉验证 .pptx 文件。用户提到 slides、deck、presentation、PPT 或 .pptx 文件时，应使用本 Skill。
short-description: PPT 创建、编辑、模板和视觉 QA
version: 0.2.0
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
      description: 可选，pptxgenjs 从零生成
    - type: command
      value: libreoffice
      description: 可选，转换 PDF 用于预览
    - type: command
      value: pdftoppm
      description: 可选，把 PDF 预览渲染为图片
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

## 硬约束（必须遵守）

1. **执行脚本必须用 `run_skill_script`**，不要用 `run_command` 拼 `python scripts/...`，也不要用 `python -c`。
2. **禁止用 `write_file` 写入 `.pptx/.docx/.xlsx/.pdf` 冒充交付物**；这些扩展名只能由脚本或 pptxgenjs 写入 `OUTPUT_DIR`。
3. `script` 参数必须是**可执行业务脚本** resource id。先用 `list_skill_resources` 确认。
   - 允许示例：`office-ppt/scripts/inspect_pptx.py`、`thumbnail.py`、`create_pptx.js`、`add_slide.py`、`clean.py`、`office/unpack.py`、`office/pack.py`
   - **禁止**：`path_contract.py`、`office/helpers/*`、`office/validators/*`、`office/schemas/*`（仅供 import）
4. 输入文件通过 `inputs` stage 到 `INPUT_DIR`；脚本参数优先传文件名。中间解包目录用 `WORK_DIR`，最终 `.pptx` 进 `OUTPUT_DIR`。
5. 不要臆造不存在的脚本名；以 `list_skill_resources(skill="office-ppt")` 为准。
6. **依赖缺失闭环**：若 `run_skill_script` 返回 `failure_kind=dependency_missing` 且 `retryable=true`：
   - 按 `suggested_install` 调用 `install_skill_dependencies`（仅装 `dependencies.runtime` 声明的 npm/pip 包；须用户审批）。
   - 安装成功后 **用相同参数**再调一次 `run_skill_script`；不要先闲聊、不要用 `write_file` 伪造 `.pptx`。
   - `system` 依赖（soffice/pdftoppm）不可对话期安装，需预装镜像/本机环境。
   - 若 `failure_kind=sandbox_violation`：申请权限/换 sandbox mode，**不要**当成缺包去装依赖。

## Quick Reference

| 任务 | 调用 |
|------|------|
| 结构/文本检查 | `run_skill_script(skill="office-ppt", script="office-ppt/scripts/inspect_pptx.py", args=["file.pptx"], inputs=[...])` |
| 缩略图网格 | `run_skill_script(..., script="office-ppt/scripts/thumbnail.py", args=["file.pptx"], inputs=[...])` |
| 逐页预览图 | `run_skill_script(..., script="office-ppt/scripts/render_pptx_preview.py", args=["file.pptx"], inputs=[...])` |
| 解包编辑 | `run_skill_script(..., script="office-ppt/scripts/office/unpack.py", args=["file.pptx", "unpacked"], inputs=[...])` |
| 增页/复制页 | `run_skill_script(..., script="office-ppt/scripts/add_slide.py", args=["unpacked", "slide2.xml"])` |
| 清理孤儿资源 | `run_skill_script(..., script="office-ppt/scripts/clean.py", args=["unpacked"])` |
| 打包校验 | `run_skill_script(..., script="office-ppt/scripts/office/pack.py", args=["unpacked", "out.pptx", "--original", "file.pptx"])` |
| 从零生成 | `run_skill_script(..., script="office-ppt/scripts/create_pptx.js", args=["out.pptx", "标题", "副标题"])`（需 Node + pptxgenjs）；复杂版式读 `references/pptxgenjs.md` |
| 模板编辑流程 | 读 `references/editing.md` |
| 视觉 QA 清单 | 读 `references/validation-checklist.md` |

## 推荐流程

### 读取 / 分析

1. `inspect_pptx.py` 看页数、媒体、文本与 `recommended_profile`。
2. 需要视觉总览时跑 `thumbnail.py`；需要逐页细看时跑 `render_pptx_preview.py`。

### 从零创建

1. 简单标题页：`create_pptx.js`（需 Node + `pptxgenjs`）。
2. 复杂版式：读 `references/pptxgenjs.md`，按教程生成到 `OUTPUT_DIR`。
3. `inspect` → `thumbnail`/`render` → 对照 checklist → 修复 → 再验证。

### 模板编辑

1. 读 `references/editing.md`。
2. `thumbnail` + `inspect` 分析模板。
3. `office/unpack.py` → 结构改动（`add_slide.py` / 改 `sldIdLst`）→ 改 XML 文本 → `clean.py` → `office/pack.py`。
4. 再跑预览与 QA。

## 设计原则

**不要做无聊的幻灯片。** 白底大段项目符号不够用。

### 开始前

- **选与主题绑定的配色**：换到别的主题就「不合适」才算选对。
- **主色主导**：一色占 60–70% 视觉权重，1–2 个辅色 + 一个锐利强调色。
- **明暗对比**：标题/结尾深色、内容浅色（三明治），或全程深色高级感。
- **一个视觉母题贯穿**：圆角图框、色圈图标、单侧粗边等，全稿一致。

### 配色灵感

| Theme | Primary | Secondary | Accent |
|-------|---------|-----------|--------|
| Midnight Executive | `1E2761` | `CADCFC` | `FFFFFF` |
| Forest & Moss | `2C5F2D` | `97BC62` | `F5F5F5` |
| Coral Energy | `F96167` | `F9E795` | `2F3C7E` |
| Warm Terracotta | `B85042` | `E7E8D1` | `A7BEAE` |
| Ocean Gradient | `065A82` | `1C7293` | `21295C` |
| Charcoal Minimal | `36454F` | `F2F2F2` | `212121` |

### 每页

- 每页至少一个视觉元素（图/表/图标/形状）。
- 布局可轮换：双栏、图标行、2x2/2x3、半幅出血图。
- 数据用大数字 callout、对比栏、时间线。
- 标题 36–44pt，正文 14–16pt；边距 ≥0.5"；块间距 0.3–0.5"。

### 避免

- 每页同一版式；正文居中；默认蓝；标题下装饰线；低对比；纯文字页；文本框默认 padding 导致对齐漂移。

## QA（必须）

**默认有问题，你的工作是找出来。** 第一次渲染几乎从不完美。

1. 生成或编辑后至少做一次预览（`thumbnail` 或 `render_pptx_preview`）。
2. 对照 `references/validation-checklist.md` 查重叠、溢出、低对比、占位符残留。
3. 检查 `run_skill_script` 返回的 `ok` 与 `artifacts[].ok`；门禁失败不得宣称交付成功。
4. 修完后**再验证受影响页**；至少完成一轮「发现问题 → 修复 → 再验证」。
5. 若工具返回错误，**不要用同一错误参数死循环重试**；换合法脚本或向用户说明缺口。
