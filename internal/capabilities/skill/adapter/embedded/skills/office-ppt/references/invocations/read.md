# PPT 只读提取入口 (office-ppt-read)

本入口专门用于提取并总结绑定 PPTX 中的文字、演讲者备注、表格和内容结构。

- 【硬规则与边界】：本入口仅用于只读分析与摘要。**严禁在此入口下尝试解包(unpack)、修改 XML、重新打包(pack)或编辑 PPTX 文件**。
- 【任务路由】：若用户的目标是创建、修改、重色或编辑重新打包 PPTX，本入口无法完成，应终止当前入口并在返回消息中告知用户改用"office-ppt"技能（创建/编辑模式）。
- 【任务执行】：使用 `run_skill_command` 调用 MarkItDown 或 python-pptx 单行指令提取 PPTX 内容。输入文件名直接使用 inputs 中的 `alias`（如 `输入别名.pptx`）。
- 【标准提取命令模板】：

1. **快速转换为 Markdown (MarkItDown，推荐首选)**：
```bash
python -m markitdown 输入别名.pptx
```
如果 `markitdown` 未安装或缺少 pptx 拓展，调用 `install_skill_dependencies` 安装：
`packages: [{"manager": "pip", "name": "markitdown[pptx]"}]`

2. **幻灯片、备注与表格精细提取 (python-pptx 单行指令)**：
```bash
python -c "from pptx import Presentation; prs=Presentation('输入别名.pptx'); [print(f'=== 幻灯片 {i+1} ===\n' + (f'[备注]: {s.notes_slide.notes_text_frame.text.strip()}\n' if s.has_notes_slide and s.notes_slide.notes_text_frame and s.notes_slide.notes_text_frame.text.strip() else '') + '\n'.join([sh.text_frame.text.strip() for sh in s.shapes if sh.has_text_frame and sh.text_frame.text.strip()])) for i,s in enumerate(prs.slides)]"
```

- 不创建、修改、渲染或发布 PPT，不调用候选选择工具。
- 明确披露无法从 OOXML/文本提取器读取的视觉内容；不得把文本提取结果描述成视觉检查。
- 提取完成后直接将总结消息返回给用户，无需提交物理交付物。

