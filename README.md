# Genesis Agent

English | [简体中文](./README.zh-CN.md)

Genesis Agent is a Go-based autonomous agent framework inspired by Claude Code, designed for both personal productivity and enterprise-grade AI automation.

It provides a universal Agent Loop that can be configured with prompts, tools, MCP servers, skills, memory, and permissions to support a wide range of enterprise tasks, including workflow automation, knowledge work, file processing, command-line tool execution, and code-assisted task completion.

Genesis Agent is not intended, at this stage, to be a full-featured AI coding product. Code execution is mainly used as a capability for completing enterprise tasks, such as processing files, invoking CLI tools, transforming data, automating operations, and implementing task-specific logic when needed.

Genesis Agent supports three usage modes:

* **Enterprise Mode**: a full enterprise-grade system built with React, Ant Design Pro, and a Go backend API. It includes multi-tenancy, RBAC, auditability, tool governance, agent configuration, skill management, and an extensible agent runtime for enterprise AI applications.
* **CLI Mode**: a personal Claude Code-like command-line agent for local tasks, file operations, command execution, tool invocation, and code-assisted automation.
* **Desktop Workbench Mode**: a personal desktop agent workspace for managing tasks, tools, skills, conversations, files, and local workflows.

In the future, Genesis Agent will support multiple multi-agent collaboration patterns, including planning, delegation, reflection, handoff, parallel execution, human-in-the-loop workflows, and long-running autonomous agents. It will also support integration with social and enterprise messaging platforms, enabling agents to interact with users, teams, tools, and business systems through familiar communication channels.

With these capabilities, Genesis Agent aims to make it easy to build enterprise-grade OpenClaw-like products for complex task execution, team collaboration, business automation, and intelligent operational workflows.

The goal of Genesis Agent is to become a powerful, extensible, and production-ready autonomous agent platform for enterprise task agents, personal automation agents, multi-agent systems, and future long-running agent applications.
