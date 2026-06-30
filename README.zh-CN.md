# Genesis Agent

[English](./README.md) | 简体中文

Genesis Agent 是一个基于 Go 的自主 Agent 框架，灵感来自 Claude Code，面向个人效率工具和企业级 AI 自动化场景而设计。

它提供一个通用的 Agent Loop，可通过提示词、工具、MCP Server、Skills、记忆和权限进行配置，用于支持多种企业级任务，包括工作流自动化、知识工作、文件处理、命令行工具调用，以及通过代码辅助完成任务。

Genesis Agent 在当前阶段并不定位为一个完整成熟的 AI 编程产品。代码执行主要作为完成企业任务的一种能力，用于处理文件、调用 CLI 工具、转换数据、自动化操作，以及在必要时实现特定任务逻辑。

Genesis Agent 支持三种使用模式：

* **企业模式**：基于 React、Ant Design Pro 和 Go 后端 API 构建的完整企业级系统。它包括多租户、RBAC、审计能力、工具治理、Agent 配置、Skill 管理，以及面向企业级 AI 应用的可扩展 Agent 运行时。
* **CLI 模式**：面向个人使用的 Claude Code-like 命令行 Agent，用于本地任务、文件操作、命令执行、工具调用和代码辅助自动化。
* **桌面工作台模式**：面向个人使用的桌面端 Agent 工作台，用于管理任务、工具、Skills、会话、文件和本地工作流。

未来，Genesis Agent 将支持多种多 Agent 协作范式，包括规划、委派、反思、交接、并行执行、人在回路工作流，以及长期运行的自主 Agent。同时，它还将支持接入社交软件和企业即时通讯平台，使 Agent 能够通过用户和团队熟悉的沟通渠道，与人员、团队、工具和业务系统进行协同。

基于这些能力，Genesis Agent 的目标是让企业能够更轻松地构建企业级 OpenClaw-like 产品，用于复杂任务执行、团队协作、业务自动化和智能运营工作流。

Genesis Agent 的最终目标是成为一个强大、可扩展、可生产落地的自主 Agent 平台，支撑企业任务 Agent、个人自动化 Agent、多 Agent 系统，以及未来长期运行的 Agent 应用。

