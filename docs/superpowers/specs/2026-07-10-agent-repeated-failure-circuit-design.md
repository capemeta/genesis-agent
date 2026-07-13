# Agent 重复失败防护设计（Repeat Guard）

> 状态：**最终版最佳实践**（完整能力一次定稿，不分一期/二期裁剪）。  
> 关联：`docs/agent loop设计.md` §3.8 RunContext、§4.5 RuntimePolicy、§4.7 错误分类；`docs/agent loop设计-SSE与重试策略设计.md` RetryPolicy；Gate A `failure_kind` / `suggested_action`。

## 1. 第一性原理

### 1.1 真问题

ReAct 跨迭代对 **同一工具调用身份** 反复执行并得到 **同类失败**，空转耗尽 `max_iterations`。  
日志实证：`create_pptx.js` 同参连续十余次 `PATH_CONTRACT` / `ok=false`，直至 50 轮失败。

### 1.2 不是什么问题

| 问题 | 归属 | 本设计是否负责 |
| --- | --- | --- |
| LLM 429 / 网关超时同一次调用内重试 | Infra `RetryPolicy`（L0） | 否 |
| 模型偶发换参再试 | 正常探索 | 否（身份变了就放行） |
| 装依赖后再跑同一脚本 | 合法再试 | 是（事件清零后放行） |
| 参数微调但任务无任何进展 | 换皮空转 | 是（进展门禁） |

### 1.3 不变量

1. **平台硬约束优先于 Prompt**；文案只辅助。  
2. **预执行匹配键只能是调用身份**，不能依赖尚未产生的 `failure_kind`。  
3. **失败可观测**：拦截也必须把结构化 JSON 交给模型（`(content, nil)`，禁止只回 Go error 短句）。  
4. **合法再试有明确放行条件**（身份变化 / 事件清零 / 进展恢复），不是「永远禁止重试」。  
5. **与现有 `failure_kind`、`suggested_action`、`RuntimePolicy`、`progress` 对齐**，不另造平行错误体系。

---

## 2. 分层（最终架构）

```text
L0  Infra Retry          同一次 Execute 内的瞬时故障（已有 RetryPolicy）
L1  Call-Identity Guard  同一 (tool, args) 连续失败 → 硬拦截
L2  Progress Gate        多轮无实质进展 → 强制换策略 / partial_complete
L3  Prompt / Skill       收到 repeated_failure / no_progress 时的行为说明
```

- L0 与 L1/L2 **正交**：L0 重试不对模型暴露、不写入 Guard 计数。  
- L1 防「原样盲重试」；L2 防「换皮盲重试」。两者都要，缺一不可（最终版）。  
- L3 不能单独成立。

命名：**Repeat Guard**（行为环防空转），不用 SRE「熔断器」隐喻，避免与依赖健康探测混淆。

---

## 3. 核心模型

### 3.1 调用身份（Call Key）

预执行与入账共用：

```text
call_key = hash(tool_name + "\0" + canonical_json(normalize_args(args)))
```

`normalize_args`（最终规则）：

1. 解析为 JSON object；失败则对原始字符串 trim 后哈希。  
2. Object key 字典序；字符串 trim；数字/布尔保持规范 JSON。  
3. 若值是绝对路径且落在本次 Run 的 `WORK_DIR` / `INPUT_DIR` / `OUTPUT_DIR` / `TMPDIR` / `SKILL_DIR` 下，改写为逻辑前缀形式（如 `$OUTPUT_DIR/foo.pptx`）。  
4. 默认忽略字段（最终黑名单）：`request_id`、`trace_id`、`timestamp`、`nonce`、`client_request_id`。  
5. 工具 Info / ParameterSchema 可声明 `x-repeat-guard-ignore: true` 的字段（扩展点，与黑名单合并）。

**注意：** `failure_kind` **不进入** `call_key`。它只存在于该 key 的状态记录中。

### 3.2 Run 级状态（挂在 RunContext）

```text
RepeatGuardState {
  calls: map[call_key] -> CallFailureState
  progress: ProgressWindow
}

CallFailureState {
  consecutive_failures  int
  total_failures        int
  last_failure_kind     string
  last_suggested_action string
  last_error_excerpt    string   // 截断
  last_result_excerpt   string   // 截断，供拦截时回传
  blocked               bool     // 已对模型返回过 repeated_failure
}

ProgressWindow {
  stagnant_iterations   int      // 连续「无进展」的 ReAct 迭代数
  last_progress_at_iter int
  signals_seen          ...      // 调试用
}
```

- 仅内存；Run 结束释放。  
- 若 Run **Resume**（审批后继续），必须随 Run 可恢复状态一并恢复 Guard（与 RunContext 重建同生命周期）。  
- L0 内部重试 **不** 修改本状态。

### 3.3 RuntimePolicy 配置（最终）

与现有 `MaxConsecutiveFail` 并列，职责分离：

| 字段 | 含义 | 默认 |
| --- | --- | --- |
| `MaxIdenticalToolFailures` | 同一 `call_key` 连续失败达到该值后，**下一次同 key 调用硬拦截** | `2` |
| `MaxStagnantIterations` | 连续无进展迭代达到该值 → 触发 L2 | `5` |
| `RepeatGuardEnabled` | 总开关 | `true` |

语义澄清：

- `MaxIdenticalToolFailures = 2`：允许失败 2 次进入模型上下文；**第 3 次同 key 调用**在 Execute 前拦截。  
- `= 0` 或 `RepeatGuardEnabled=false`：关闭 L1（L2 可独立关：`MaxStagnantIterations=0`）。  
- 现有 `MaxConsecutiveFail`：任意工具连续失败上限（不要求同 args）；与 L1 同时生效，先到先触发。

---

## 4. L1：Call-Identity Guard（硬闸）

### 4.1 执行前

```text
key = call_key(tool, args)
st  = state.calls[key]

if enabled && st.consecutive_failures >= MaxIdenticalToolFailures:
  return JSON ToolResult (err = nil):
    ok: false
    failure_kind: "repeated_failure"
    retryable: false
    suggested_action: "change_strategy_or_ask_user"
    prior:
      tool, call_key_prefix, failure_kind, count, suggested_action, error_excerpt
    hint: 明确禁止再次提交相同调用；列出允许动作（改参 / 先完成 prior.suggested_action / 问用户）
  并发 progress：已拦截重复失败
  不调用 registry.Execute
  不增加 consecutive_failures（避免 repeated_failure 自我叠加）
  标记 st.blocked = true
```

### 4.2 执行后入账

仅当真实 Execute 完成（含业务失败）且结果对模型可见时：

```text
if success (工具契约 ok=true 或等价成功):
  delete state.calls[key]          // 或清零
  record_progress(success_tool)
else:
  st.consecutive_failures++
  st.total_failures++
  st.last_failure_kind = ...
  st.last_result_excerpt = truncate(result)
```

`failure_kind` 解析顺序：结果 JSON `failure_kind` → 从 error 推导（与日志字段同一套）→ `tool_error`。

### 4.3 事件清零（合法再试，最终必须支持）

| 事件 | 清零范围 |
| --- | --- |
| 任意 `call_key` **成功** | 该 key |
| `install_skill_dependencies` **成功** | 所有 `last_failure_kind=dependency_missing` 的条目；若结果带 `skill`，优先清该 skill 相关 `run_skill_command` key |
| 用户 **新的批准决策**（approval granted / scope 刷新） | 与该 resource/tool 匹配的条目；保守策略可清整 Run 的 `approval_denied` |
| 显式 `RepeatGuard.Reset(run)`（测试/运维） | 全部 |
| 模型换了实质 args | 自然新 key，无需清零 |

清零后允许再次 Execute 同一逻辑脚本——这就是「极少数情况就是要再试」的平台表达。

### 4.4 并行工具调用

同一迭代并行多个 ToolCall 时：

- 对 `state.calls` 加 Run 内互斥锁。  
- 同一 `call_key` 并行第二次：第二次在入队/执行前若已见失败计数达标则拦截；若两次同时首次失败，两次都执行并都入账（连续计数按完成顺序 +1），下轮再拦。  
- 不要求跨并行完美去重执行，只保证 **不会无限轮次** 重复。

---

## 5. L2：Progress Gate（防换皮空转）

### 5.1 进展信号（任一即算有进展）

在每轮 ReAct 迭代结束时评估：

1. 至少一个工具 **成功**  
2. 出现 **新的** `failure_kind`（相对本 Run 已见集合有新增）——允许探索新失败面  
3. 成功写入/交付 **新产物**（artifact 路径集合增长；或 `write_file`/`apply_patch` 成功且 path 未见过）  
4. **用户介入**（审批决策、用户新消息注入）  
5. Guard **事件清零** 发生（安装成功等）  
6. LLM 产出 **最终答案**（本轮将结束）

不把「又一次失败但 kind 相同、仅 args 微调」算进展。

### 5.2 无进展

若本轮无任何进展信号：`stagnant_iterations++`，否则清零并记录 `last_progress_at_iter`。

当 `stagnant_iterations >= MaxStagnantIterations`：

**必须**执行（最终行为，不是可选项）：

1. 向对话注入一条 **系统观察消息**（或等价 tool-less 观察块），`failure_kind` 语义用：  
   `no_progress`（也可作为 `system` 角色内容，字段写入 metadata）  
2. `suggested_action = summarize_blocker_and_ask_user_or_change_approach`  
3. 将 L1 阈值对后续调用视为 `min(MaxIdenticalToolFailures, 1)` 生效至再次出现进展（收紧，加速收敛）  
4. 若下一轮仍无进展且模型未给出最终答案：Evaluator / Loop 可走已有 **`partial_complete`** 语义结束 Run（`incomplete=true`），避免再烧到 `max_iterations`

L2 不替代 L1；L1 管同 key，L2 管「换 key 但仍原地打转」。

---

## 6. 与模型的契约（稳定 JSON）

### 6.1 `repeated_failure`

```json
{
  "ok": false,
  "failure_kind": "repeated_failure",
  "retryable": false,
  "suggested_action": "change_strategy_or_ask_user",
  "prior": {
    "tool": "run_skill_command",
    "failure_kind": "path_contract_violation",
    "count": 2,
    "suggested_action": "fix_script_paths",
    "error_excerpt": "EXECUTION_PATH_CONTRACT_VIOLATION: ..."
  },
  "hint": "平台已拦截相同调用。请更换参数/脚本，或先完成 prior.suggested_action，或向用户说明阻塞原因。禁止再次提交相同调用。"
}
```

### 6.2 `no_progress`（L2 注入）

```json
{
  "ok": false,
  "failure_kind": "no_progress",
  "retryable": false,
  "suggested_action": "summarize_blocker_and_ask_user_or_change_approach",
  "stagnant_iterations": 5,
  "hint": "连续多轮无实质进展。请总结阻塞、更换路线或询问用户；不要继续微调无效调用。"
}
```

### 6.3 System / Skill 硬规则（L3，最终必配）

短规则写入 skills_instructions / system：

- 收到 `failure_kind=repeated_failure`：禁止相同调用；必须改参或改策略。  
- 收到 `failure_kind=no_progress`：必须总结阻塞或问用户，禁止继续空转。  
- `dependency_missing`：先 `install_skill_dependencies`，成功后再用 **相同** 参数跑脚本（安装成功会清零 Guard）。

---

## 7. 模块边界与数据流

```text
                    ┌─────────────────────┐
  ToolCall ────────►│ Repeat Guard        │
                    │  (runtime/repeatguard)
                    └─────────┬───────────┘
                       pass   │ block → JSON repeated_failure
                              ▼
                    registry.Execute (L0 Retry 在其内)
                              │
                              ▼
                    Guard.Record(success|fail)
                              │
                    迭代结束 ──► Guard.EvaluateProgress()
                              │
                         stagnant? → 注入 no_progress / 收紧 / partial_complete
```

| 组件 | 位置 | 职责 |
| --- | --- | --- |
| `repeatguard` | `internal/runtime/repeatguard` | Call key、状态、L1/L2 纯逻辑 |
| 拦截 / 入账 | `react_loop.runToolCall` | 执行前 Check、执行后 Record |
| 进展评估 | `react_loop` 每轮末尾 | `EvaluateProgress` |
| 配置 | `RuntimePolicy` | 见 §3.3 |
| Progress 事件 | 已有 `progress` | 拦截与无进展对用户可见 |
| 日志 | `agent.log` | `failure_kind` + 截断 stdout/stderr；拦截打 `repeated_failure` |

**不**放进单个 Tool 实现内。

---

## 8. 可观测性与审计

1. 真实工具失败：`failure_kind` + 截断 `stdout`/`stderr`（已落地要求）。  
2. L1 拦截：`failure_kind=repeated_failure`，`prior_kind`，`count`，`call_key` 前缀。  
3. L2 触发：`failure_kind=no_progress`，`stagnant_iterations`。  
4. Progress：`已拦截重复失败: <tool>` / `运行无进展，请更换策略`。  
5. Audit（若启用）：记录 block/clear 事件，含 `run_id`、`tool`、`call_key` 前缀。

---

## 9. 与现有机制的关系

| 机制 | 关系 |
| --- | --- |
| `RetryPolicy` | L0；Guard 不计其内部次数 |
| `MaxConsecutiveFail` | 任意连续失败；与 L1 同时生效 |
| `max_iterations` | 最后保险；Guard 应使多数空转在此之前收敛 |
| `partial_complete` | L2 耗尽后的正式收口 |
| Gate A 失败 JSON | Guard 回传同一套字段风格 |
| `install_skill_dependencies` | 成功 → 事件清零 dependency 相关 key |

---

## 10. 验证标准（最终 DoD）

1. 同 `run_skill_command` + 同 args 连续失败 2 次后，第 3 次 **不执行** 并回 `repeated_failure`。  
2. 仅改 args（实质字段）→ 放行。  
3. `dependency_missing` → install 成功 → 同参再跑 **放行**。  
4. 连续微调 args 但无成功/无新 kind/无产物，达到 `MaxStagnantIterations` → 出现 `no_progress`，且不会默默烧到 max_iterations 才停（或停在 partial_complete）。  
5. L0 超时重试 3 次只对模型算 **一次** 工具失败入账。  
6. 单测覆盖：normalize、Check/Record、清零事件、进展信号、并行加锁。  
7. 回归：office-ppt 路径契约误报已修前提下，Guard 不误拦「安装后的第二次 run」。

---

## 11. 风险与对策

| 风险 | 对策 |
| --- | --- |
| normalize 误判两调用相同 | 阈值按「连续失败次数」；关键字段忽略名单可配置；日志打印 canonical args 摘要 |
| normalize 过松导致换皮 | L2 进展门禁收口 |
| 拦截用 Go error 丢掉结构 | **强制** `(JSON, nil)` |
| `repeated_failure` 被再次提交 | Check 命中不入账；L3 硬规则；L2 收紧阈值 |
| Resume 丢状态 | Guard 随 Run 可恢复状态持久化 |
| 与 MaxConsecutiveFail 重复 | 文档与配置命名区分；指标分开打点 |

---

## 12. 决策摘要（最终）

1. **最终形态 = L1 Call-Identity Guard + L2 Progress Gate + L3 文案**，外加 L0 RetryPolicy；一次定稿，不按「先做一半」裁能力。  
2. **预执行键 = 仅 call_key(tool, normalize(args))**；`failure_kind` 只作状态与清零条件。  
3. **合法再试靠事件清零与身份变化**，不靠「默认无限重试」。  
4. **拦截与无进展都必须结构化进入模型上下文**，并驱动 progress / 日志 /（可选）audit。  
5. 配置落入 **RuntimePolicy**，与 `MaxConsecutiveFail` 并列，模块为 **`internal/runtime/repeatguard`**。

