# Word 只读提取入口 (office-word-read)

本入口专门用于提取并总结绑定 Word 文档（.docx, .doc）中的正文段落、标题与表格。

- 【硬规则与边界】：本入口仅用于只读分析与摘要。**严禁在此入口下尝试渲染、编辑或创建交付文件**。
- 【任务执行】：使用 `run_skill_command` 调用 MarkItDown 或 Python 单行指令提取 Word 内容。输入文件名直接使用 inputs 中的 `alias`（如 `输入别名.docx`）。
- 【标准提取命令模板】：

1. **快速转换为 Markdown (MarkItDown，推荐首选)**：
```bash
python -m markitdown 输入别名.docx
```

2. **段落与表格精细提取 (python-docx 单行指令)**：
```bash
python -c "import docx; d=docx.Document('输入别名.docx'); [print(f'[{p.style.name}] {p.text.strip()}') for p in d.paragraphs if p.text.strip()]; [print(f'=== 表格 {i+1} ===\n' + '\n'.join([' | '.join([c.text.strip() for c in r.cells]) for r in t.rows])) for i,t in enumerate(d.tables)]"
```

- 提取完成后直接将总结消息返回给用户，无需提交物理交付物。

