# Codex CLI 系统提示词架构分析

> 分析根目录：`D:\workspace\go\go-project\codex`（相对路径均相对于此根目录）

Codex 的提示词体系是**双层结构**：

1. **Base Instructions** — 通过 Responses API 的 `instructions` 字段（或 Responses Lite 下作为前缀 developer message）发送，会话级固定。
2. **Conversation Input Items** — 以 `developer` / `user` 角色的历史消息注入，由多个**带标记的片段（Fragment）**拼装，支持全量注入与增量 diff。

不存在独立的 `gpt_5_codex_prompt.md`；按模型区分的 prompt 主要内嵌在 `models.json` 的 `base_instructions` / `model_messages` 字段中。

---

## 1. 基础指令 / 系统提示词组织

### 1.1 三层 Base Instructions 源（优先级见 §4）

| 相对路径 | 常量/函数 | 行号 | 职责 |
|---|---|---|---|
| `codex-rs/models-manager/models.json` | 每模型 `base_instructions`、`model_messages.instructions_template` | 全文 | **主数据源**：按 slug 存储完整系统提示词；支持 `{{ personality }}` 模板 |
| `codex-rs/models-manager/prompt.md` | `BASE_INSTRUCTIONS` | `model_info.rs:16` | 未知模型 fallback 全文 |
| `codex-rs/protocol/src/prompts/base_instructions/default.md` | `BASE_INSTRUCTIONS_DEFAULT` | `models.rs:1367-1381` | `BaseInstructions` 结构体默认值 |

**模型选择逻辑**（`codex-rs/core/src/session/mod.rs`）：

```597:616:codex-rs/core/src/session/mod.rs
        // Resolve base instructions for the session. Priority order:
        // 1. config.base_instructions override
        // 2. conversation history => session_meta.base_instructions
        // 3. base_instructions for current model
        let model_info = models_manager
            .get_model_info(model.as_str(), &config.to_models_manager_config())
            .await;
        ...
        let base_instructions = config
            .base_instructions
            .clone()
            .or_else(|| conversation_history.get_base_instructions().map(|s| s.text))
            .unwrap_or_else(|| model_info.get_model_instructions(config.personality));
```

**Personality 模板渲染**（`codex-rs/protocol/src/openai_models.rs:460-478`）：

- 有 `model_messages.instructions_template` → 用 `{{ personality }}` 替换后作为最终 base instructions
- 无模板 → 直接用 `base_instructions` 字符串
- `get_model_instructions()` 在模型切换时也会生成 developer diff（`context_manager/updates.rs:171-186`）

**本地 fallback 人格模板**（`codex-rs/models-manager/src/model_info.rs:111-125`）：

- 仅 `gpt-5.2-codex` / `exp-codex-personality` 有本地 `instructions_template`

### 1.2 所有 `include_str!` 嵌入的 Markdown 提示词模板

#### A. 核心 Base / 模式模板

| 相对路径 | 导出常量 | Rust 文件:行号 |
|---|---|---|
| `codex-rs/models-manager/prompt.md` | `BASE_INSTRUCTIONS` | `models-manager/src/model_info.rs:16` |
| `codex-rs/protocol/src/prompts/base_instructions/default.md` | `BASE_INSTRUCTIONS_DEFAULT` | `protocol/src/models.rs:1367` |
| `codex-rs/collaboration-mode-templates/templates/default.md` | `DEFAULT` | `collaboration-mode-templates/src/lib.rs:2` |
| `codex-rs/collaboration-mode-templates/templates/plan.md` | `PLAN` | `lib.rs:1` |
| `codex-rs/collaboration-mode-templates/templates/execute.md` | `EXECUTE` | `lib.rs:3` |
| `codex-rs/collaboration-mode-templates/templates/pair_programming.md` | `PAIR_PROGRAMMING` | `lib.rs:4` |

#### B. `codex-rs/prompts/templates/` — 运行时片段模板

| 相对路径 | 常量 | Rust 文件:行号 | 职责 |
|---|---|---|---|
| `permissions/approval_policy/never.md` | `APPROVAL_POLICY_NEVER` | `prompts/src/permissions_instructions.rs:17-18` | 审批策略 |
| `permissions/approval_policy/unless_trusted.md` | `APPROVAL_POLICY_UNLESS_TRUSTED` | `:19-20` | 同上 |
| `permissions/approval_policy/on_request.md` | `APPROVAL_POLICY_ON_REQUEST_RULE` | `:21-22` | 同上 |
| `permissions/approval_policy/on_request_rule_request_permission.md` | `APPROVAL_POLICY_ON_REQUEST_RULE_REQUEST_PERMISSION` | `:23-24` | 同上 |
| `permissions/sandbox_mode/danger_full_access.md` | `SANDBOX_MODE_DANGER_FULL_ACCESS` | `:27-28` | 沙箱模式 |
| `permissions/sandbox_mode/workspace_write.md` | `SANDBOX_MODE_WORKSPACE_WRITE` | `:29-30` | 同上 |
| `permissions/sandbox_mode/read_only.md` | `SANDBOX_MODE_READ_ONLY` | `:31-32` | 同上 |
| `apply_patch_tool_instructions.md` | `APPLY_PATCH_TOOL_INSTRUCTIONS` | `prompts/src/apply_patch.rs:2-3` | 旧版 apply_patch 文本指令（现多为 tool spec） |
| `compact/prompt.md` | `SUMMARIZATION_PROMPT` | `prompts/src/compact.rs:1` | 历史压缩 |
| `compact/summary_prefix.md` | `SUMMARY_PREFIX` | `compact.rs:2` | 压缩摘要前缀 |
| `goals/continuation.md` | lazy `Template` | `prompts/src/goals.rs:7` | 目标续行 |
| `goals/budget_limit.md` | lazy `Template` | `goals.rs:15` | 预算限制 |
| `goals/objective_updated.md` | `Template` | `goals.rs:22` | 目标更新 |
| `realtime/backend_prompt.md` | `BACKEND_PROMPT` | `prompts/src/realtime.rs:1` | Realtime 后端 |
| `realtime/realtime_start.md` | `START_INSTRUCTIONS` | `realtime.rs:3` | Realtime 开始 |
| `realtime/realtime_end.md` | `END_INSTRUCTIONS` | `realtime.rs:2` | Realtime 结束 |
| `review/rubric.md` | `REVIEW_PROMPT` | `prompts/src/review_request.rs:9` | Code review |
| `review/exit_success.xml` | `render_review_exit_success` | `prompts/src/review_exit.rs:6` | Review 退出 |
| `review/exit_interrupted.xml` | `render_review_exit_interrupted` | `review_exit.rs:8` | 同上 |

#### C. 其他子系统嵌入模板

| 相对路径 | 用途 | Rust 引用 |
|---|---|---|
| `codex-rs/core/src/guardian/policy.md` | Guardian 固定策略 | `guardian/prompt.rs:696` |
| `codex-rs/core/src/guardian/policy_template.md` | Guardian 策略模板 | `guardian/prompt.rs:700` |
| `codex-rs/core/prompt_with_apply_patch_instructions.md` | 测试夹具（含 apply_patch 段落的完整 base） | `session/tests.rs:1212-1213` |
| `codex-rs/tui/prompt_for_init_command.md` | `/init` 命令用户提示 | `tui/.../slash_dispatch.rs:253` |
| `codex-rs/ext/web-search/web_run_description.md` | web_search tool 描述 | `ext/web-search/src/tool.rs:34` |
| `codex-rs/ext/image-generation/imagegen_description.md` | imagegen tool 描述 | `ext/image-generation/src/tool.rs:55` |
| `codex-rs/core/templates/search_tool/*.md` | search tool 描述 | `tools/spec_plan.rs` 引用 |
| `codex-rs/memories/write/templates/**/*.md` | 记忆写入各阶段 | `memories/write/src/prompts.rs` 等 |

### 1.3 Base Instructions 内嵌片段结构（`default.md` / `prompt.md` 内容）

单文件内已分段，**不是**运行时多文件拼接，而是写作时分节：

| 片段主题 | 在 `default.md` 中的位置 |
|---|---|
| 身份/能力声明 | L1-9 |
| AGENTS.md 规范 | L17-27 |
| Personality / 响应风格 | L13-15, L29-50 |
| Planning（`update_plan`） | L52-70 |
| 代码风格 / editing guidelines | L134+ |
| Sandbox and approvals（概念层） | 正文各节 |

**运行时额外注入**（不在 base 文件内）：

| 片段 | 注入角色 | 标记 | 源文件 |
|---|---|---|---|
| 权限/沙箱/审批 | developer | `<permissions instructions>` | `context/permissions_instructions.rs`（re-export `codex_prompts`） |
| 协作模式 | developer | `<collaboration_mode>` | `context/collaboration_mode_instructions.rs` |
| Skills | developer | skills 块 | `context/available_skills_instructions.rs` |
| Plugins/Apps | developer/user | 各自块 | `plugin_instructions.rs`, `apps_instructions.rs` |
| Multi-agent 模式 | user | multi-agent 块 | `multi_agent_mode_instructions.rs` |
| Personality spec（未 bake 时） | developer | personality 块 | `personality_spec_instructions.rs` |
| Token budget 上下文 | developer | token budget 块 | `token_budget_context.rs` |
| Guardian policy | 独立 developer message | 无统一 tag | `guardian/prompt.rs` |

---

## 2. 用户/项目自定义指令：AGENTS.md

### 2.1 全局用户指令（Codex Home）

| 相对路径 | 类型/函数 | 行号 | 职责 |
|---|---|---|---|
| `codex-rs/codex-home/src/instructions/mod.rs` | `CodexHomeUserInstructionsProvider` | 14-74 | 从 `~/.codex/` 读取 `AGENTS.override.md` → `AGENTS.md` |
| `codex-rs/ext/extension-api/src/user_instructions.rs` | `UserInstructionsProvider` trait | — | 扩展接口 |
| `codex-rs/cli/src/main.rs` | 实例化 provider | ~1973 | CLI 入口注入 |
| `codex-rs/app-server/src/message_processor.rs` | 同上 | ~371 | App Server 注入 |

### 2.2 项目 AGENTS.md 发现与合并

| 相对路径 | 函数/常量 | 行号 | 职责 |
|---|---|---|---|
| `codex-rs/core/src/agents_md.rs` | `DEFAULT_AGENTS_MD_FILENAME` | 37 | `"AGENTS.md"` |
| | `LOCAL_AGENTS_MD_FILENAME` | 39 | `"AGENTS.override.md"` |
| | `AGENTS_MD_SEPARATOR` | 43 | `"\n\n--- project-doc ---\n\n"` |
| | `load_project_instructions()` | 47-75 | 合并 user + 各 environment 项目 doc |
| | `read_agents_md()` | 83-145 | 按字节预算读取 |
| | `agents_md_paths()` | 149-220 | 从 project root → cwd 向上发现 |
| | `candidate_filenames()` | 222-236 | override → AGENTS.md → fallback 列表 |
| | `LoadedAgentsMd::text()` | 298-305 | 拼接最终文本 |
| | `contextual_user_fragment()` | 380-393 | 包装为 user fragment |
| `codex-rs/core/src/agents_md_manager.rs` | `AgentsMdManager::refresh()` | 31-44 | 按 environment 缓存 |
| | `get_loaded()` | 46-48 | 供 step 使用 |
| `codex-rs/core/src/session/session.rs` | 创建 `AgentsMdManager` | 902-905 | session 初始化时 refresh |
| `codex-rs/core/src/context/world_state/agents_md.rs` | `AgentsMdState` | 15-44 | WorldState 段，role=user |
| `codex-rs/core/src/context/user_instructions.rs` | `UserInstructions` | 4-30 | 渲染为 `# AGENTS.md instructions...<INSTRUCTIONS>...` |

**发现规则摘要**：

1. 从 cwd 向上找 project root（默认 marker `.git`，可配 `project_root_markers`）
2. 从 root 到 cwd 每层目录取第一个匹配文件（优先级：`AGENTS.override.md` > `AGENTS.md` > `project_doc_fallback_filenames`）
3. 全局 `~/.codex/AGENTS.md` 作为 `user_instructions` 前缀
4. 多 environment 时加 `for \`env_id\` with root ...` 标签
5. 总字节上限 `project_doc_max_bytes`（默认 32KiB，`config/mod.rs:203`）

### 2.3 `user_instructions` / `experimental_instructions_file` 注入路径

| 配置键 | TOML 字段 | 解析位置 | 效果 |
|---|---|---|---|
| `instructions` | `config_toml.rs:219` | `config/mod.rs:3585-3587` | 内联 base instructions 覆盖 |
| `model_instructions_file` | `config_toml.rs:241` | `config/mod.rs:3578-3584` | 读文件覆盖 base instructions |
| `base_instructions` | `config/mod.rs:676` | 运行时/API 直传 | 最高优先级覆盖 |
| `developer_instructions` | `config_toml.rs:223` | `config/mod.rs:3588` | **独立 developer message**，非 base |
| `experimental_realtime_start_instructions` | `config_toml.rs:415` | `context_manager/updates.rs:110-118` | Realtime 开始指令 |

**注意**：代码库中**没有** `experimental_instructions_file` 字段；等价能力是 `model_instructions_file` + `instructions`。

---

## 3. 环境上下文注入

| 相对路径 | 类型/函数 | 行号 | 职责 |
|---|---|---|---|
| `codex-rs/core/src/session/world_state.rs` | `build_world_state_for_step()` | 9-61 | 组装 WorldState |
| `codex-rs/core/src/context/world_state/environment.rs` | `EnvironmentsState` | 16-164 | 环境段主逻辑 |
| `codex-rs/core/src/context/environment_context.rs` | `FileSystemContext`, `NetworkContext` | — | 文件系统/网络子结构 |
| `codex-rs/core/src/agent/control.rs` | `format_environment_context_subagents()` | ~314 | subagents 列表 |
| `codex-rs/protocol/src/protocol.rs` | `ENVIRONMENT_CONTEXT_OPEN/CLOSE_TAG` | 102-103 | `<environment_context>` |

**渲染 XML 结构**（单 environment 时）：

```xml
<environment_context>
  <cwd>...</cwd>
  <shell>...</shell>
  <current_date>...</current_date>   <!-- 可选 -->
  <timezone>...</timezone>           <!-- 可选 -->
  <network>...</network>             <!-- 可选 -->
  <filesystem>...</filesystem>       <!-- 可选 -->
  <subagents>...</subagents>         <!-- 可选 -->
</environment_context>
```

- **角色**：`user`
- **开关**：`config.include_environment_context`（默认 true，`config/mod.rs:705-706`）
- **增量**：`EnvironmentsState::render_diff()` 仅在 cwd/shell/network/filesystem 等变化时重发（`environment.rs:97-145`）

---

## 4. Prompt 最终组装顺序

### 4.1 会话级：Base Instructions 决议

`Session::spawn_internal()` → `session/mod.rs:597-616`（见 §1.1）

### 4.2 首 turn：Context Items 全量注入

**入口**：`record_context_updates_and_set_reference_context_item()` — `session/mod.rs:3552-3636`

**调用链**：

```
run_turn (turn.rs:171)
  → record_context_updates_and_set_reference_context_item
    → build_world_state_for_step (world_state.rs)
    → build_initial_context_with_world_state_and_mcp (mod.rs:3142-3461)
```

**`build_initial_context_with_world_state_and_mcp` 拼装顺序**（developer_sections 合并为一个 developer message）：

| 顺序 | 内容 | 条件 |
|---|---|---|
| 1 | Model switch instructions | 恢复/切换模型 |
| 2 | Permissions instructions | `include_permissions_instructions` |
| 3 | `developer_instructions`（协作/自定义） | 非 guardian |
| 4 | Collaboration mode instructions | `include_collaboration_mode_instructions` |
| 5 | Realtime start/end | realtime 状态变化 |
| 6 | Personality spec | personality 未 bake 进 base |
| 7 | Apps instructions | apps enabled |
| 8 | Available skills instructions | `include_skill_instructions` |
| 9 | Recommended plugins | contextual user |
| 10 | Available plugins | developer |
| 11 | Extension thread/turn contributors | 扩展 API |
| 12 | Token budget context + guidance | Feature::TokenBudget |
| 13 | **WorldState.render_full()** | **AGENTS.md + environment_context** |
| 14 | Multi-agent usage hint | 独立 developer message |
| 15 | Multi-agent mode instructions | user message |
| 16 | Recommended plugins（user 段合并） | contextual user message |
| 17 | Guardian policy | 独立 developer message |

**输出消息类型**：

- `build_developer_update_item(developer_sections)` → 单个 developer `Message`
- `build_contextual_user_message(contextual_user_sections)` → 单个 user `Message`（AGENTS.md + environment 等在此）
- 部分段独立成额外 developer message（guardian、multi-agent hint、extension separate sections）

### 4.3 稳态 turn：仅 diff

`session/mod.rs:3587-3600`：

- `build_settings_update_items()` — 权限/协作/模型/人格/realtime 变化
- `world_state.render_history_diff()` — AGENTS.md / environment 变化

### 4.4 采样请求：发给模型

| 步骤 | 文件 | 函数 | 行号 |
|---|---|---|---|
| 构建 Prompt 结构 | `core/src/session/turn.rs` | `build_prompt()` | 1044-1059 |
| 取 base | `session/mod.rs` | `get_base_instructions()` | 1262-1267 |
| 取 history | `context_manager/history.rs` | `for_prompt()` | 141-144 |
| 动态 tools | `tools/spec_plan.rs` | `build_model_visible_specs_and_registry()` | 235+ |
| 构建 API 请求 | `core/src/client.rs` | `build_responses_request()` | 812-892 |

**API 映射**（`client.rs:829-850`）：

- **标准模式**：`instructions = base_instructions.text`，`input = history items`，`tools = 动态 tool specs`
- **Responses Lite**：`instructions = ""`，base instructions 作为 input 前缀 developer message，`tools` 放入 `AdditionalTools` item

---

## 5. apply_patch、动态工具说明、Reminder

### 5.1 apply_patch

| 层面 | 文件 | 说明 |
|---|---|---|
| 历史文本指令 | `prompts/templates/apply_patch_tool_instructions.md` | `APPLY_PATCH_TOOL_INSTRUCTIONS` 常量；**当前主要供测试/旧模型** |
| Tool spec | `core/src/tools/handlers/apply_patch_spec.rs` | `create_apply_patch_freeform_tool()`，含 Lark grammar |
| 运行时注册 | `tools/spec_plan.rs:765-769` | 当 `model_info.apply_patch_tool_type.is_some()` 时加入 router |
| Shell 拦截 | `tools/handlers/apply_patch.rs` | `intercept_apply_patch()` 从 shell 命令解析 patch |

新模型（如 gpt-5.5）在 `models.json` 设 `apply_patch_tool_type: "freeform"`，指令走 **tool description**，不再拼进 base text。

### 5.2 动态工具说明拼接

| 文件 | 函数 | 职责 |
|---|---|---|
| `tools/spec_plan.rs` | `plan_core_tools()` / `add_core_utility_tools()` | 按 feature、environment、model 决定工具集 |
| `tools/spec_plan.rs` | `build_model_visible_specs_and_registry()` | 生成 `Vec<ToolSpec>` |
| `tools/router.rs` | `model_visible_specs()` | 暴露给 `build_prompt` |
| 各 handler `*_spec.rs` | `create_*_tool()` | 单工具 description/schema |
| MCP/Plugin tools | runtime 合并 | 外部工具 description 动态加入 |

### 5.3 Reminder 注入

| Reminder 类型 | 文件 | 函数 | 触发时机 |
|---|---|---|---|
| Rollout budget | `session/rollout_budget.rs` | `maybe_record_reminder()` | turn 开始，token 阈值 |
| Current time | `session/time_reminder.rs` | `maybe_record_current_time_reminder()` | 按 interval/delivery_mode |
| Token budget | `session/token_budget.rs` | `maybe_record()` | 接近 compact 阈值 |
| Guardian follow-up | `guardian/review_session.rs` | `append_guardian_followup_reminder()` | 第二次 review |
| Hook additional context | `hook_runtime.rs` | post-hook developer messages | hook 回调 |

均以 `ContextualUserFragment::into()` → `record_conversation_items()` 写入 history。

---

## 6. 配置层覆盖 Prompt

### 6.1 config.toml 相关字段（`config/src/config_toml.rs` + `core/src/config/mod.rs`）

| 字段 | 行号 (toml/mod) | 覆盖对象 |
|---|---|---|
| `instructions` | toml:219 / mod:3587 | → `base_instructions` |
| `model_instructions_file` | toml:241 / mod:3578-3584 | 读文件 → `base_instructions` |
| `base_instructions` | mod:676 | 直接覆盖（API/运行时最高） |
| `developer_instructions` | toml:223 / mod:3588 | 独立 developer message |
| `guardian_policy_config` | mod:681-685 | Guardian 模板内 `# Policy Configuration` 段 |
| `compact_prompt` | toml:244 | 历史压缩 prompt |
| `experimental_compact_prompt_file` | toml:515 / mod:3616-3623 | 压缩 prompt 文件 |
| `experimental_realtime_start_instructions` | toml:415 | Realtime 开始 |
| `include_permissions_instructions` | toml:226 | 权限块开关 |
| `include_apps_instructions` | toml:229 | Apps 块开关 |
| `include_collaboration_mode_instructions` | toml:232 | 协作模式块开关 |
| `include_environment_context` | toml:235 / mod:3598 | 环境块开关 |
| `project_doc_max_bytes` | toml:291 | AGENTS.md 字节上限 |
| `project_doc_fallback_filenames` | toml:295 | AGENTS.md 备选文件名 |
| `skills.include_instructions` | 间接 | Skills 块开关 |

**解析链**（`config/mod.rs:3575-3587`）：

```
base_instructions = CLI/API base_instructions
  .or(file_base_instructions from model_instructions_file)
  .or(cfg.instructions)
```

**ModelsManager 级覆盖**（`models-manager/src/model_info.rs:55-60`）：

- `ModelsManagerConfig.base_instructions` 覆盖模型 catalog 中的值
- 若设置则清空 `model_messages`（禁用人格模板）

**Profile 支持**（`config/src/profile_toml.rs:43-55`）：`model_instructions_file`、各 `include_*` 开关。

---

## 7. 完整相关源码文件清单（按职责分组）

### 7.1 编排核心（必看）

| 相对路径 | 职责 | 关键符号:行号 |
|---|---|---|
| `codex-rs/core/src/session/mod.rs` | 会话创建、base 决议、initial context 拼装、context diff | `spawn_internal:500`, `build_initial_context_*:3142`, `record_context_updates_*:3552`, `get_base_instructions:1262` |
| `codex-rs/core/src/session/turn.rs` | turn 循环、`build_prompt`、reminder 触发 | `build_prompt:1044`, `run_turn:171` |
| `codex-rs/core/src/client.rs` | API 请求最终形态 | `build_responses_request:812` |
| `codex-rs/core/src/client_common.rs` | `Prompt` 结构体 | `struct Prompt:18` |
| `codex-rs/core/src/context_manager/updates.rs` | developer/user message 构建、settings diff | `build_developer_update_item:188`, `build_settings_update_items:235` |
| `codex-rs/core/src/context_manager/history.rs` | 历史规范化、token 估算 | `for_prompt:141` |
| `codex-rs/core/src/session/world_state.rs` | WorldState 构建入口 | `build_world_state_for_step:9` |

### 7.2 Base Instructions / 模型元数据

| 相对路径 | 职责 | 关键符号:行号 |
|---|---|---|
| `codex-rs/models-manager/models.json` | 每模型 base + personality 模板 | 各模型 `base_instructions` |
| `codex-rs/models-manager/prompt.md` | Fallback 全文 | — |
| `codex-rs/models-manager/src/model_info.rs` | Fallback + personality 本地模板 | `BASE_INSTRUCTIONS:16`, `with_config_overrides:23` |
| `codex-rs/models-manager/src/manager.rs` | 远程 catalog 刷新/合并 | `list_models` |
| `codex-rs/models-manager/src/collaboration_mode_presets.rs` | Plan/Default 模式 developer 指令 | `builtin_collaboration_mode_presets:16` |
| `codex-rs/protocol/src/openai_models.rs` | `ModelInfo`, `get_model_instructions` | `:460` |
| `codex-rs/protocol/src/models.rs` | `BaseInstructions` 类型 | `:1367-1381` |
| `codex-rs/protocol/src/protocol.rs` | AGENTS/environment 协议 tag | `:100-103` |

### 7.3 AGENTS.md

| 相对路径 | 职责 |
|---|---|
| `codex-rs/core/src/agents_md.rs` | 发现、读取、合并 |
| `codex-rs/core/src/agents_md_manager.rs` | 会话级缓存 |
| `codex-rs/core/src/agents_md_tests.rs` | 行为测试 |
| `codex-rs/codex-home/src/instructions/mod.rs` | 全局 AGENTS.md |
| `codex-rs/core/src/context/user_instructions.rs` | user fragment 渲染 |
| `codex-rs/core/src/context/world_state/agents_md.rs` | WorldState 段 |
| `docs/agents_md.md` | 用户文档 |

### 7.4 Context Fragments（developer/user 段）

| 相对路径 | 职责 |
|---|---|
| `codex-rs/context-fragments/src/fragment.rs` | `ContextualUserFragment` trait、`render()` |
| `codex-rs/core/src/context/mod.rs` | 所有 fragment 模块索引 |
| `codex-rs/core/src/context/collaboration_mode_instructions.rs` | 协作模式 |
| `codex-rs/core/src/context/permissions_instructions.rs` | 权限（re-export prompts crate） |
| `codex-rs/core/src/context/model_switch_instructions.rs` | 模型切换 |
| `codex-rs/core/src/context/personality_spec_instructions.rs` | 人格补充 |
| `codex-rs/core/src/context/available_skills_instructions.rs` | Skills |
| `codex-rs/core/src/context/available_plugins_instructions.rs` | Plugins |
| `codex-rs/core/src/context/apps_instructions.rs` | Apps/MCP connectors |
| `codex-rs/core/src/context/multi_agent_mode_instructions.rs` | 多 Agent 模式 |
| `codex-rs/core/src/context/token_budget_context.rs` | Token 预算 |
| `codex-rs/core/src/context/current_time_reminder.rs` | 时间 reminder |
| `codex-rs/core/src/context/contextual_user_message.rs` | fragment 识别注册表 |
| `codex-rs/core/src/context/world_state/environment.rs` | 环境上下文 |
| `codex-rs/core/src/context/world_state/mod.rs` | WorldState diff 框架 |

### 7.5 Prompts Crate（模板库）

| 相对路径 | 职责 |
|---|---|
| `codex-rs/prompts/src/lib.rs` | 公共导出 |
| `codex-rs/prompts/src/permissions_instructions.rs` | 权限模板渲染 |
| `codex-rs/prompts/src/apply_patch.rs` | apply_patch 文本 |
| `codex-rs/prompts/src/compact.rs` | 压缩 |
| `codex-rs/prompts/src/goals.rs` | 目标 |
| `codex-rs/prompts/src/realtime.rs` | Realtime |
| `codex-rs/prompts/src/review_request.rs` / `review_exit.rs` | Review |
| `codex-rs/prompts/templates/**` | 全部 markdown/xml 模板 |

### 7.6 工具动态拼接

| 相对路径 | 职责 |
|---|---|
| `codex-rs/core/src/tools/spec_plan.rs` | 工具规划主逻辑 |
| `codex-rs/core/src/tools/router.rs` | ToolRouter |
| `codex-rs/core/src/tools/handlers/*_spec.rs` | 各工具 schema/description |
| `codex-rs/core/src/tools/handlers/apply_patch_spec.rs` | apply_patch freeform spec |

### 7.7 配置

| 相对路径 | 职责 |
|---|---|
| `codex-rs/config/src/config_toml.rs` | TOML schema |
| `codex-rs/config/src/profile_toml.rs` | Profile 字段 |
| `codex-rs/core/src/config/mod.rs` | Config 解析与合并 |
| `codex-rs/models-manager/src/config.rs` | ModelsManager 覆盖 |

### 7.8 子系统专用 Prompt

| 相对路径 | 职责 |
|---|---|
| `codex-rs/core/src/guardian/prompt.rs` | Auto-review guardian prompt |
| `codex-rs/core/src/compact.rs` / `compact_remote*.rs` | 压缩时 prompt 使用 |
| `codex-rs/core/src/realtime_prompt.rs` | Realtime backend prompt |
| `codex-rs/core/src/prompt_debug.rs` | 调试 prompt 组装 |
| `codex-rs/memories/write/src/prompts.rs` | 记忆写入 |

### 7.9 测试（理解布局的参考）

| 相对路径 | 职责 |
|---|---|
| `codex-rs/core/tests/suite/model_visible_layout.rs` | AGENTS/environment 布局快照 |
| `codex-rs/core/tests/suite/prompt_caching.rs` | base + apply_patch 缓存行为 |
| `codex-rs/exec/tests/suite/agents_md.rs` | AGENTS.md 注入集成测试 |
| `codex-rs/core/src/config/config_loader_tests.rs` | `model_instructions_file` 覆盖测试 |

---

## 8. 可借鉴的设计要点（供 genesis-agent 参考）

1. **双层分离**：把「长期稳定的身份/能力/编码规范」放在 API `instructions`（或等价 system 层），把「随会话/环境变化的状态」做成带标记的 history items。避免每次改 cwd 就重写整份 system prompt。

2. **Fragment + Marker 协议**：每个注入段实现统一 trait（role + open/close tag + body），便于识别、去重、diff、回放。AGENTS.md 用 `# AGENTS.md instructions` + `<INSTRUCTIONS>`，环境用 `<environment_context>`，权限用 `<permissions instructions>`——结构清晰、可测试。

3. **全量 + 增量双模式**：首 turn `build_initial_context` 全量注入；稳态只发 `build_settings_update_items` + `world_state.render_diff`。Resume/fork 用 `reference_context_item` 做 baseline，显著省 token。

4. **优先级链清晰**：`config override > session persisted > model catalog > fallback file`。文件覆盖、内联、API 参数走同一条决议链，避免多处散落逻辑。

5. **按模型分 prompt，不按硬编码文件名**：主数据在 `models.json`（可远程刷新），本地 `prompt.md` 仅 fallback；personality 用 `{{ personality }}` 模板而非复制整份 prompt。

6. **工具说明与系统说明解耦**：`apply_patch` 从 base 正文迁移到 `ToolSpec.description` + 条件注册（`apply_patch_tool_type`）。系统 prompt 讲「何时/为何」，tool schema 讲「怎么调用」。

7. **项目文档发现可配置**：AGENTS.md 支持 override 文件名、fallback 列表、字节预算、多 environment 标签——适合 monorepo / 多沙箱。

8. **配置开关粒度细**：`include_permissions_instructions`、`include_environment_context`、`include_skill_instructions` 等可独立关闭，方便调试与子 Agent 裁剪上下文。

9. **扩展点统一**：`context_contributors`（thread/turn/world_state）让插件追加 fragment，不必改核心拼装函数。

10. **Rollout 持久化与 prompt 一致**：`base_instructions` 写入 session meta；context items 写入 rollout history——resume 时复现相同模型可见布局。

---

如需针对某条链路（例如「仅 AGENTS.md diff 逻辑」或「Responses Lite 前缀模式」）做更深逐行 walkthrough，可以指定模块我继续展开。