# Genesis Agent -- Office / Tool / Skill / MCP 架构总结

## 1. 核心结论

在现代 Agent 系统中（参考 Codex / Claude Code），Office
能力不应拆成大量细粒度 Tool，而应采用：

-   **Skill：负责编排与工作流**
-   **Tool：提供统一原子能力入口**
-   **Execution Runtime：负责真正执行（沙箱 / 本地 / 云）**
-   **MCP：作为外部系统接入协议**

------------------------------------------------------------------------

## 2. 分层架构

    Agent
      ↓
    Skill Layer（业务逻辑/流程编排）
      ↓
    Tool Router（统一入口）
      ↓
    Execution Adapter（执行分发）
      ↓
    Sandbox / Local / Cloud Runtime

------------------------------------------------------------------------

## 3. 各组件职责

### 3.1 Skill（推荐作为核心设计）

职责： - 定义"做什么 & 怎么做" - 多步骤工作流（Word / Excel / PPT /
PDF） - 定义工具使用策略与约束

特点： - 不直接执行 - 可组合 - 面向业务场景（如：生成报告、财务分析）

------------------------------------------------------------------------

### 3.2 Tool（统一原子能力）

职责： - 提供最小能力入口 - 例如： - file.read - file.write - exec.run

原则： - 不做 Office 细粒度能力拆分 - 不做复杂业务逻辑

------------------------------------------------------------------------

### 3.3 Execution Runtime（核心执行层）

职责： - 真实执行逻辑 - 运行 Python / LibreOffice / PDF 引擎 -
支持不同环境： - Local CLI - Desktop - Web Sandbox - Enterprise VM

------------------------------------------------------------------------

### 3.4 MCP（外部扩展协议）

职责： - 连接外部系统 - Microsoft Graph / Google Docs / ERP -
插件化扩展能力

不是： - 本地执行机制 - Skill 替代品

------------------------------------------------------------------------

## 4. Office 能力最佳实践

### 推荐模式

    Skill（Word/Excel/PPT/PDF）
      ↓
    调用 Tool（file / exec）
      ↓
    Execution（sandbox脚本 / runtime）

### 不推荐

-   ❌ 每个 Office 操作都做 Tool（爆炸）
-   ❌ Skill 直接执行文件操作
-   ❌ MCP 替代本地 runtime

------------------------------------------------------------------------

## 5. Codex / Claude Code 设计风格

核心特点：

-   Skill 驱动领域逻辑
-   Tool 只做通用能力
-   Execution 通过 sandbox 或本地 runtime
-   MCP 用于外部系统接入

------------------------------------------------------------------------

## 6. 关键设计原则

### 6.1 分层原则

-   Skill ≠ Tool ≠ Execution ≠ MCP

### 6.2 解耦原则

-   Tool 不绑定执行方式
-   Skill 不绑定 runtime
-   MCP 不参与本地执行

### 6.3 扩展原则

-   Execution 可替换（local / sandbox / cloud）
-   MCP 可插拔
-   Skill 可组合

------------------------------------------------------------------------

## 7. 最终推荐架构

    Genesis Agent
        ↓
    Skill System
        ↓
    Tool Router
        ↓
    Execution Layer
        ├── Local Runtime
        ├── Sandbox Runtime
        ├── Cloud Runtime
        └── MCP Adapters

------------------------------------------------------------------------

## 8. 一句话总结

Office / 文档能力的本质是：

> Skill 负责"流程"，Tool 负责"入口"，Execution 负责"执行"，MCP
> 负责"扩展"。
