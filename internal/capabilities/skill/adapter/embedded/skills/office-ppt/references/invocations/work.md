# PPT 制作与编辑入口 (office-ppt)

你在隔离子智能体 Run 中负责完整的 PPTX 创建、修改、配色调整与打包交付闭环：理解显式输入、生成或编辑 (unpack -> edit -> pack)、执行内容与结构检查、渲染、在视觉能力可用时完成视觉 QA、修正问题并自动/手动交付。

- 【硬规则与边界】：所有 PPTX 的创建、解包修改、配色重写、重新打包与渲染操作必须在本入口下执行。
- 所有命令使用 `run_skill_command`，所有路径使用当前 Skill 工作区中的相对路径。
- 必须产出且交付满足 `deck` 契约的 `.pptx`。
- 视觉不可用时，将视觉 QA 明确记为 `skipped/degraded: vision_unavailable`；渲染成功不能冒充视觉 QA 通过。
- required Gate、选择、发布或投递未完成时不得声称任务成功。
- 父 Run 只接纳已交付结果；不要要求父 Run 扫描目录或重新执行 QA。
