# CLI TUI 体验重构设计

> 状态：核心能力已实现，Windows 真实终端交互整改中
> 日期：2026-07-13  
> 范围：`products/cli` 交互式 `chat` TUI（Bubble Tea + Lip Gloss）  
> 目标：在 Cursor 内置终端 / bat(cmd) 双击启动等 Windows 常见环境下，达到接近 Claude Code / Codex 的可读性、可控性与「拿走内容」体验  
> 相关代码：`products/cli/internal/tui/chat/`、`products/cli/internal/tui/styles/`、`products/cli/internal/command/chat_cmd.go`  
> 相关契约：`docs/运行进度事件与前端展示契约.md`

---

## 一、问题与目标

### 1.1 已确认痛点

1. **无法选中文字**：用户在 bat 双击启动与 Cursor/VS Code 内置终端中，均无法对 TUI 内容做可靠鼠标拖选。
2. **根因**：全屏 Alt Screen + raw mode 下，这两类终端对原生选区支持差；即使启用 `WithMouseCellMotion` 支持滚轮，也不能把「终端拖选」当作主复制手段。推理期间 Spinner 高频重绘会进一步恶化选区稳定性。
3. **体验债**：厚重紫色顶栏/气泡、单行输入、Ctrl+C 直接退出、过程日志与最终回答抢视觉焦点、帮助栏静态且弱引导。

### 1.2 成功标准

| 标准 | 说明 |
| --- | --- |
| 复制可达 | 鼠标不被应用捕获，可使用终端原生拖选；也能用 `Ctrl+Y` 稳定把最近一条 Agent 回答写入系统剪贴板 |
| 中断语义正确 | Ctrl+C 取消本轮推理，不退出程序；退出有独立明确入口 |
| 主回答优先 | 工具/技能/sandbox 过程默认折叠，不淹没最终回答 |
| 输入可预期 | 多行编辑、发送/换行分离、输入历史、斜杠命令可发现 |
| 环境可用 | bat(cmd) 与 Cursor 内置终端均为一等公民 |

### 1.3 非目标（本设计不做）

- 不为拖选而维护 Plain/TUI 双渲染主路径；保留 Alt Screen，但默认不捕获鼠标
- 第一版不做像素级应用内鼠标圈选
- 不引入侧栏、多会话分屏、WebView 混合 UI
- 不改动 Run Engine / progress 协议语义（仅优化 CLI 呈现）

---

## 二、方案选择

对比过三条路线：

| 路线 | 摘要 | 结论 |
| --- | --- | --- |
| A 渐进打磨 | 现有布局上补 `/copy` 与小改 | 达不到目标体验上限 |
| **B 体验导向重构** | 保留 Bubble Tea，按 Codex/Claude Code 重做信息架构与交互 | **采用** |
| C TUI + Plain 双模式 | 另做可拖选纯文本模式 | 体验分裂、维护成本高 |

**采用 B**：复制走应用内系统剪贴板；布局采用「体验细节加强版」（见第三节）。

---

## 三、信息架构与视觉

### 3.1 单栏五层结构

```text
┌ header（极简 + 状态 chips）─────────────────────────────┐
│ genesis / chat          qwen3-max · sandbox · ui-ctx~% │
├ transcript（消息主区，可滚动）───────────────────────────┤
│  [你] 用户消息                                          │
│  [Ag] Agent 回答 + 行内动作提示（复制/展开过程）         │
│  ▸ 运行过程（默认折叠摘要）                              │
├ status / toast ─────────────────────────────────────────┤
├ composer（多行输入 + 底栏快捷键提示）───────────────────┤
└ contextual footer（随空闲/推理/审批切换）───────────────┘
```

### 3.2 视觉原则（基线 = 体验细节版）

1. **顶栏安静**：去掉满宽厚重紫色色块；用文字层级 + 小号 status chip（模型、sandbox、基于可见 UI transcript 字符估算的 `ui-ctx~` 占用）。该估算仅用于提示，不代表真实 LLM 装配用量，不用于计费或截断决策。
2. **消息去气泡化**：user/agent 用小头像块或安静标签，正文为正常前景色，避免大面积色块背景（利于阅读；也为将来选择模式留空间）。
3. **过程不抢戏**：运行过程默认折叠为一行摘要（步数 / tokens / 耗时 / 成败）；`o` 或点击等价快捷键展开。
4. **反馈可见**：复制成功、取消本轮、审批结果等用短时 toast（约 2s），不永久污染 transcript。
5. **帮助情境化**：footer 文案随模式变化（空闲 / 推理 / 审批），避免永远同一串快捷键。
6. **Windows 帧边界**：布局宽度固定预留最右侧 4 列，吸收 Windows 终端与渲染库对 CJK、符号及中英文混排的宽度估算偏差，禁止应用内容接近自动换行边界；Windows Console 启动时启用 `DISABLE_NEWLINE_AUTO_RETURN`，退出时恢复原模式，避免逐帧滚屏留下重影。

### 3.3 组件映射（实现边界）

| UI 区域 | 建议落点 | 备注 |
| --- | --- | --- |
| Program 入口 | `products/cli/internal/command/chat_cmd.go` | Alt Screen；鼠标策略见 §4.3 |
| Model / Update / View | `products/cli/internal/tui/chat/` | 可按文件职责拆分 composer、transcript、toast |
| 样式 | `products/cli/internal/tui/styles/` | 收敛为细节版调色板，清理旧气泡样式 |
| 剪贴板 | `products/cli/internal/tui/clipboard/`（新建） | 封装 OS clipboard，隔离 Windows 差异 |
| 审批 | 现有 `products/cli/internal/approval` | 交互呈现按 §6 加强，协议不变 |

目录边界：仅动 `products/cli`；不把 TUI 细节泄漏进 `internal/runtime`。

---

## 四、复制与选取体系

### 4.1 原则

> 用户要的是「拿走内容」，不是被迫先学习 TUI 特有操作。
> **主路径 = 终端原生拖选 + `Ctrl+Y` 一键复制回答**；应用内消息选择模式仅作键盘补充。

### 4.2 优先级

| 优先级 | 能力 | 行为 |
| --- | --- | --- |
| **P0** | 一键复制最近 Agent 回答 | `Ctrl+Y`；焦点不在「正在编辑输入」冲突时的 `y`；`/copy` |
| **P0** | 结果反馈 | 成功/失败 toast |
| **P0** | 可发现性 | Composer 底栏与 `/help` 常驻提示 |
| **P1** | 扩展复制 | `/copy last\|user\|all`；路径类内容可选择去装饰后复制 |
| **P1** | 终端原生选取 | 默认不捕获鼠标；消息区滚动使用 `↑/↓` 与 `PgUp/PgDn` |
| **P2** | 应用内选择模式 | `v` 进入，`j/k` 移动，起止标记，`y` 复制，`Esc` 退出 |

### 4.3 鼠标策略

- **默认不启用** `tea.WithMouseCellMotion()`，避免 xterm 鼠标跟踪拦截 Windows cmd / Cursor 终端的原生拖选。
- 主路径同时提供终端拖选和 `Ctrl+Y`；消息区滚动使用键盘，不以牺牲复制换取滚轮。
- Spinner / 进度刷新 **降频**（例如状态栏 8–10 Hz 上限，内容区按事件合并），降低闪烁与无效重绘。

### 4.4 复制内容规则

1. `Ctrl+Y` / 默认 `/copy`：复制最近一条 **assistant** 消息的可见正文（含已展示的思考与「最终回答」分段，与用户屏幕一致）。
2. 若无 assistant 消息：toast 提示无可复制内容，不报错退出。
3. 剪贴板写入失败（权限/环境）：toast 显示失败原因摘要，并建议 `/copy` 重试或检查终端环境。
4. 剪贴板后端按环境降级：本地优先原生剪贴板；WSL 失败后尝试 Windows PowerShell；SSH 会话或本地后端不可用时使用 OSC 52（含 tmux passthrough）。

---

## 五、交互与快捷键

### 5.1 中断 / 退出语义（对齐 Claude Code）

| 按键 | 行为 |
| --- | --- |
| `Ctrl+C` | 若正在推理：取消本轮（cancel context），回到可输入；**不退出** |
| `Ctrl+C`（空闲态） | 第一次：toast 提示「再按一次退出，或输入 `/quit`」；第二次（1s 内）：退出 TUI。**已拍板：二次确认方案。** |
| `Ctrl+D` 或 `/quit` `/exit` | 退出 TUI |
| `Esc` | 优先级从高到低：① 推理态 → 请求取消本轮（同 Ctrl+C），忽略输入框内容；② 空闲且 Composer 有内容 → 清空输入；③ 空闲且输入为空 → no-op。审批态：不作为「允许」，保持审批焦点。 |

> 实现注意：取消本轮需复用现有 `cancelFn`，并在取消后重建可取消的子 context，保证后续轮次仍可取消。

### 5.2 Composer

| 能力 | 约定 |
| --- | --- |
| 多行输入 | 替换单行 `textinput` 为 textarea（或等价 bubbles 组件） |
| 发送 | `Enter` |
| 换行 | `Shift+Enter` |
| 字数 | 底栏显示 `len/limit`（limit 可高于当前 2000，建议 4000–8000，与后端约束对齐时再定） |
| 输入历史 | `↑` / `↓` 在输入为空或历史浏览态下翻阅已发送 user 消息 |
| 斜杠命令 | 输入（可含前导空格的）`/` 自动显示匹配命令菜单；`↑` / `↓` 选择，`Enter` 填入，`Esc` 关闭，`Tab` 保留为补全快捷键 |
| 推理中 | Composer 禁用发送，边框改为非聚焦态；仍允许 `Ctrl+C` / `Esc` |

### 5.3 消息区快捷键（输入框为空或失焦时）

| 按键 | 行为 |
| --- | --- |
| `y` | 复制最近 Agent 回答（仅在 **Composer 失焦**或处于**历史浏览态**时生效，避免与正常打字冲突） |
| `o` | 展开/折叠最近一轮运行过程 |
| `↑↓` `PgUp/PgDn` | 滚动 transcript |
| `?` | 打开快捷键帮助（等同 `/help` 的键位篇） |

焦点判定原则：Bubble Tea 以「组件是否 Focus()」为准，不以内容是否为空为准。Composer 聚焦时，所有字符键优先交给输入框；Composer 失焦或处于历史浏览态时，消息区快捷键生效。

### 5.4 斜杠命令

保留并扩展：

| 命令 | 行为 |
| --- | --- |
| `/help` | 帮助（含复制与中断语义） |
| `/clear` | 清空会话与 UI 历史 |
| `/quit` | 退出 |
| `/copy` | 复制最近 Agent 回答 |
| `/copy user` | 复制最近用户输入 |
| `/copy all` | 复制整段可见 transcript（纯文本） |

---

## 六、运行过程、流式与审批

### 6.1 运行过程

- 进度事件仍来自现有 `OnProgress` / `progress.Event`。
- UI 层维护「当前 turn 的 activity 摘要」：默认折叠；展开后为条目列表（工具/技能/sandbox）。完成或失败后摘要包含步骤数、tokens、耗时和结果。
- 最终回答与 activity **分区**：activity 不写入 assistant 气泡正文（避免和最终回答混排）；与当前「system 过程摘要」可收敛为统一 activity 组件。

### 6.2 流式展示

- 继续支持 `assistant_draft` / `thinking` / `final_answer` 的现有契约。
- 流式更新应合并 delta，避免每 token 全量 `SetContent` 造成抖动（可按帧或按字符阈值节流）。
- 完成后更新 meta：步数、tokens、耗时。

### 6.3 审批

- 审批卡片保留 Y / S / N / A 语义。
- 审批态：**接管焦点**，隐藏或禁用 Composer，防止误把审批键当聊天发送。
- 选项可视化为明确按钮提示条；底部 footer 切换为审批帮助文案。

---

## 七、状态模型（逻辑）

```text
Mode = Idle | Running | Approval | HelpOverlay | Select(P2)

Idle:
  - Composer focused
  - Ctrl+Y / y / /copy 可用
  - Enter 发送 → Running
  - Ctrl+C 第一次：toast「再按一次退出」，第二次（1s 内）：Quit

Running:
  - cancelable via Ctrl+C / Esc（优先级：① 取消推理，不退出）
  - 取消后须重建 cancelFn：ctx, cancelFn = context.WithCancel(parentCtx)
  - progress → status + activity
  - stream → assistant bubble
  - complete/error → Idle (+ toast)

Approval:
  - keyboard Y/S/N/A
  - Esc 不作为「允许」，保持 Approval 态
  - resolve → back to Running or Idle
```

Toast 为正交短时状态，不改变 Mode。
实现机制：Toast 由一次性延迟命令在约 2 秒后发送 `clearToastMsg` 清除，不依赖 Ticker，不影响 Spinner 帧率。

---

## 八、分阶段交付

### Phase 1 — 复制与中断正确性（P0，已完成）

1. 系统剪贴板封装（`products/cli/internal/tui/clipboard/`）+ `Ctrl+Y` / `/copy` + toast  
2. Ctrl+C 取消本轮（不退出）+ 退出入口调整  
   - 取消后须重建可取消子 context：`ctx, cancelFn = context.WithCancel(parentCtx)`，保证后续轮次仍可取消  
   - 空闲态 Ctrl+C 二次确认：第一次 toast 提示，第二次（1s 内）退出  
3. Spinner/进度降频（状态栏 8–10 Hz 上限，`time.Tick` 节流或合并帧）  
4. `/help` 与 footer 文案更新（Ctrl+C 改为「取消」而非「退出」；复制快捷键加入 footer）  

### Phase 2 — 布局与 Composer（体验基线，已完成）

1. 顶栏 chips、去厚重气泡、activity 折叠组件  
2. 多行 Composer（Enter / Shift+Enter）  
3. 输入历史  
4. 审批焦点防误触  

### Phase 3 — 增强（已完成）

1. 终端原生拖选（默认不捕获鼠标）
2. `/copy` 子命令  
3. 上下文占用 chip（当前为基于可见 UI transcript 的 `ui-ctx~` 近似值；后续可替换为真实装配 token / provider usage）  
4. 应用内选择模式（P2）  
5. 斜杠命令菜单与补全（输入 `/` 自动展示，`↑/↓` 选择，`Enter` 填入，`Tab` 补全）  

---

## 九、测试与验收

### 9.1 自动化

- Update 层单测：无 assistant 时复制 toast、斜杠命令菜单与补全、选择模式、帮助覆盖层、完成 activity 摘要。
- 快捷键：Running 下 Ctrl+C 触发 cancel 且不 `tea.Quit`；`/quit` 与 Ctrl+D 退出。
- 布局：WindowSize 变化后完整 `View()` 不超过终端高宽；命令菜单开/关都不得增加额外空行或遗留旧帧；任何行都不得占用终端最右一列。

### 9.2 手工验收矩阵

| 环境 | 复制 | 取消本轮 | 多行输入 | 审批 |
| --- | --- | --- | --- | --- |
| Cursor 内置终端 | 原生拖选 + Ctrl+Y | Ctrl+C | Shift+Enter | Y/N |
| bat / cmd 窗口 | 原生拖选 + Ctrl+Y | Ctrl+C | Shift+Enter | Y/N |
| Windows Terminal（可选） | 原生拖选 + Ctrl+Y | 同上 | 同上 | 同上 |

---

## 十、已拍板记录

| 项 | 结论 |
| --- | --- |
| 复制失败形态 | 不能选中（非「能选不能拷」） |
| 主环境 | bat 双击 + Cursor/VS Code 终端 |
| 范围 | 较完整体验升级（对标 Claude Code / Codex） |
| 路线 | B 体验导向重构 |
| 布局基线 | 体验细节加强版（§1b） |
| 复制体系 | 终端原生拖选与应用内剪贴板并列为主路径；默认不捕获鼠标 |

---

## 十一、已关闭问题

1. ~~空闲态 `Ctrl+C`：已拍板，二次确认退出（§5.1）。~~ ✅ 已关闭  
2. ~~Composer 字符上限与后端/LLM 上下文策略的最终数值。~~ 当前设为 4000 字符；后续由后端约束统一调整。  
3. ~~上下文占用 chip 的数据源。~~ 当前使用可见 UI transcript 字符估算并标记为 `ui-ctx~`；后续可接入真实装配 token / provider usage。  

---

## 十二、参考

- 现实现：`products/cli/internal/command/chat_cmd.go`（Alt Screen，默认不捕获鼠标）
- 进度展示契约：`docs/运行进度事件与前端展示契约.md`  
- 目录边界：`docs/项目目录与边界说明.md`（产品 TUI 仅在 `products/cli`）  
- 头脑风暴稿：`.superpowers/brainstorm/30496-1783949541/content/`（本地，不入库）
