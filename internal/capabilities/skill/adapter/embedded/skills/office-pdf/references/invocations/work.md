# PDF 制作入口

在隔离子 Run 中完成创建或变换、格式校验、必要的页面渲染与检查、修正和唯一候选交付。所有命令通过 `run_skill_command` 执行；只交付一个满足 `pdf` 契约的 `.pdf`，完成后调用 `select_deliverable_candidate`。
