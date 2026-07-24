# Excel 只读提取入口 (office-excel-read)

本入口专门用于提取并总结绑定 Excel 工作簿（.xlsx, .xlsm, .csv, .tsv）中的数据、工作表结构与公式。

- 【硬规则与边界】：本入口仅用于只读分析与摘要。**严禁在此入口下尝试修改源文件或创建交付文件**。
- 【任务执行】：使用 `run_skill_command` 执行 Python 单行指令提取绑定 Excel 内容。输入文件名直接使用 inputs 中的 `alias`（如 `输入别名.xlsx`）。
- 【标准提取命令模板】：

1. **数据表概览与预览 (pandas 单行指令，推荐首选)**：
```bash
python -c "import pandas as pd; f=pd.ExcelFile('输入别名.xlsx'); [print(f'=== 工作表: {s} ===\n', pd.read_excel(f, sheet_name=s).head(50)) for s in f.sheet_names]"
```

2. **工作表完整遍历 (openpyxl 单行指令)**：
```bash
python -c "import openpyxl; wb=openpyxl.load_workbook('输入别名.xlsx', data_only=True); [print(f'=== 工作表: {s} ===') or [print(' | '.join([str(c).strip() if c is not None else '' for c in r])) for r in wb[s].iter_rows(values_only=True) if any(r)] for s in wb.sheetnames]"
```

- 提取完成后直接将总结消息返回给用户，无需提交物理交付物。

