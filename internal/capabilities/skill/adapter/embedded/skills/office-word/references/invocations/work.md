# Word 制作入口

在隔离子 Run 中完成创建或编辑、结构校验、必要的 PDF 渲染检查、修正和唯一候选交付。所有命令通过 `run_skill_command` 执行；只交付一个满足 `document` 契约的 `.docx`，完成后调用 `select_deliverable_candidate`。
