# PDF 只读提取入口 (office-pdf-read)

本入口专门用于提取并总结绑定 PDF 中的文字、表格与元数据。

- 【硬规则与边界】：本入口仅用于只读分析与摘要。**严禁在此入口下尝试修改、编辑或重新生成 PDF 文件**。
- 【任务执行与最佳实践】：
  1. 禁止搜索或盲猜不存在的 `extract_pdf.py` 脚本。
  2. 请直接使用 `run_skill_command` 提交下述标准单行 Python 指令（无换行、无嵌套多重引号，避免 Shell 语法转义错误）。
  3. 输入文件名直接使用 inputs 中的 `alias`（如 `3-人资部.pdf`）。

- 【标准单行 Python 提取命令模板】：

1. **提取元数据与全量正文 (pypdf)**：
```bash
python -c "import pypdf; r=pypdf.PdfReader('输入别名.pdf'); [print(f'=== 第 {i+1} 页 ===\n{p.extract_text()}') for i,p in enumerate(r.pages) if p.extract_text() and p.extract_text().strip()]"
```

2. **精确提取表格内容 (pdfplumber)**：
```bash
python -c "import pdfplumber; p=pdfplumber.open('输入别名.pdf'); [print(f'=== 第 {i+1} 页 ===') or [print(' | '.join(str(c).strip() if c else '' for c in r)) for r in t] for i,page in enumerate(p.pages) for t in [page.extract_tables()] if t]; p.close()"
```

- 明确披露无法从文本提取器读取的视觉内容；不得把文本提取结果描述成视觉检查。
- 提取完成后直接将总结消息返回给用户，无需提交物理文件交付物。


