# 运行日志分类与滚动设计

> 状态：已实施（与代码对齐）  
> 日期：2026-07-09  
> 触发：单文件 `agent.log` 无滚动；双路径残留；需明确日志分类与滚动策略。
---

## 一、第一性原理

### 1.1 要解决什么

1. **排障**：某次 Run 里 Loop / LLM / Tool / Skill 怎么走的。
2. **治理取证**：谁批准/拒绝了什么（审批、权限、策略）。
3. **计量**：Token、工具调用等用量事件。
4. **可运维**：按天可查、有大小上限、有保留、不撑爆磁盘。

这三类问题的**读者、保留策略、是否可删、是否可篡改预期**都不同，因此应分通道；但不应按 `llm`/`tool`/`skill` 等组件拆文件。

### 1.2 不变量

| ID | 不变量 |
| --- | --- |
| I1 | 分类按**消费目的**，不按业务组件。 |
| I2 | 默认落盘根目录：`.genesis/logs/`。 |
| I3 | 每类日志进程内**唯一 Writer**；禁止 CLI 与 shared builder 双开。 |
| I4 | 同类日志：按日滚动 + 同日大小续卷 + 保留天数。 |
| I5 | `audit` / `usage` 不得被当成可随意删的排障日志；保留期默认更长。 |
| I6 | Trace（Span）不是日志文件通道；继续走 Tracer。 |
| I7 | 开发阶段不兼容 `.agents/logs`。 |
| I8 | 同一次任务跨 `agent`/`audit`/`usage` 必须可用同一 `run_id` 检索；`session_id` 为辅键。 |

### 1.3 失败条件

- 拆出 `llm.log`/`tool.log`/`skill.log`，一次故障要跨文件拼时间线。
- 只有一个无限增长的 `agent.log`。
- 两个组件同时滚动同一文件。
- 把审批真相只写在短保留的 `agent.log` 里。
- 同一次 Run 的 agent/audit/usage 无法用同一 `run_id` 对齐（含审批决策未进 audit）。

---

## 二、分类结论：3 类（最佳实践）

在「大约 5 类、也可以 3 类」之间，**选 3 类**。

理由：Genesis 代码里已经有三条稳定缝——`Logger`、`AuditSink`、`UsageSink`。再拆第 4/5 类（`error`/`http`/`sandbox`）要么是过滤视图，要么是字段，不是独立消费者。

| 类 | 文件前缀 | 写什么 | 谁读 | 默认保留 |
| --- | --- | --- | --- | --- |
| **agent** | `agent.log` | Run/Loop/LLM/Tool/Skill/Sandbox 等运行叙事；含 `run_id`、`component` 字段 | 开发者排障 | 14 天 |
| **audit** | `audit.log` | 审批决策、权限拒绝、策略结果、Skill 加载授权等治理事件 | 安全/合规/排权限问题 | 90 天 |
| **usage** | `usage.log` | Token、工具调用次数、Skill 加载次数等计量事件 | 用量统计/成本 | 90 天 |

### 2.1 明确不做的“类”

| 候选 | 为何不做独立文件 |
| --- | --- |
| `error.log` | 只是 `agent` 的 level 过滤；需要时用工具筛 `ERROR` |
| `llm.log` / `tool.log` / `skill.log` | 组件维度；用 `component=` 字段过滤 |
| `http.log` | 出站 HTTP 属于运行叙事，写 `agent` 并带 `component=http` |
| `trace.log` | Span 走 Tracer；与日志生命周期不同 |
| `access.log` | Enterprise HTTP 接入层产品日志，不属于 runtime 三类核心 |

若未来 Enterprise 网关需要 access log，放在 `products/enterprise` 装配，不塞进这套核心三类。

### 2.2 事件归属规则（写死）

| 事件 | 归属 |
| --- | --- |
| Run 启动/结束、迭代、工具执行、LLM 调用、Skill 注入、SubAgent 委派启停、降级 warning | **agent** |
| Approval Approve/Deny、Profile 拒绝、Policy 决策、Skill 授权结果 | **audit** |
| TokenUsage、工具调用计数、Skill load/search 计量 | **usage** |
| 同时需要排障文案 + 审计记录 | **两边都写**：agent 一句摘要；audit 结构化事件（无密钥明文） |

密钥、Authorization、Cookie、完整 secret **三类日志都禁止写入**。

### 2.3 关联键契约（写死）

| 字段 | 角色 | 规则 |
| --- | --- | --- |
| `run_id` | **主键** | 一次**父** Agent Run；三类日志顶层字段；Run 入口注入 context，Sink 兜底提升。SubAgent 子执行的日志仍带父 `run_id`，另用 `child_run_id` / `agent_id` 区分 |
| `session_id` | 辅键 | 跨多次 Run 的会话；可与 `run_id` 同时存在；**Spawn 子智能体必须继承父 `session_id`，禁止漏传** |

传播规则：

1. `ReactLoopEngine.Start` 注入 `run_id`（及 `session_id`）到 context。
2. Gateway / Skill / Approval 写 audit/usage 时经 `correl.Enrich` 提升顶层字段。
3. agent 通道跨组件日志用 `correl.AttachLogger(ctx, log)` 附加关联键（Approval、execution runner 等）。
4. file Sink 再兜底一次，防止调用方漏填。

检索同一次任务：在三个文件中按同一 `run_id` 过滤即可。不要用时间窗猜测，不要另造第四个 batch id。

---

## 三、目录与滚动契约

```text
.genesis/logs/
  agent.log
  agent.2026-07-08.log
  agent.2026-07-08.1.log
  audit.log
  audit.2026-07-08.log
  usage.log
  usage.2026-07-08.log
```

规则（三类共用）：

1. 活跃文件固定为 `<name>.log`。
2. 跨日：切换为 `<name>.YYYY-MM-DD.log`。
3. 同日超过 `max_size_mb`：`<name>.YYYY-MM-DD.N.log`（N 从 1 递增）。
4. 启动与每次写入路径检查日切；删除早于 `retain_days` 的历史文件。
5. `compress` 默认 `false`（本机 CLI 简单优先；可配置开启 gzip 历史卷）。

---

## 四、格式

| 类 | 默认格式 | 原因 |
| --- | --- | --- |
| agent | **text**（人类可读，含 key=value） | 本地打开即读 |
| audit | **jsonl**（一行一个事件） | 便于检索、对账、后续进 DB |
| usage | **jsonl** | 便于聚合计量 |

`agent` 可配置 `format: json`；audit/usage 固定 jsonl，不允许改成随意文本导致难解析。

---

## 五、配置（一次性完整）

```yaml
log:
  level: info                 # 作用于 agent 通道
  dir: .genesis/logs          # 三类日志根目录；相对路径相对配置目录父目录
  rotate:
    daily: true
    max_size_mb: 100
    retain_days: 14           # agent 默认
    compress: false
  channels:
    agent:
      enabled: true
      file: agent.log
      format: text            # text | json
      retain_days: 14
    audit:
      enabled: true
      file: audit.log
      format: jsonl
      retain_days: 90
      level: info             # 审计事件通常全量落
    usage:
      enabled: true
      file: usage.log
      format: jsonl
      retain_days: 90
```

兼容迁移：

- 旧配置仅有 `log.path`：若指向 `.../agent.log`，则 `dir=其目录`，`channels.agent.file=agent.log`；audit/usage 仍启用默认文件名。
- `log.path: ""`：使用上表默认。
- 废弃默认 `.agents/logs`。

---

## 六、装配与依赖边界

```text
internal/platform/logger
  -> 滚动 Writer、多 channel Fan-out 工厂
  -> NewRuntimeLogging(cfg) => { AgentLogger, AuditFileSink, UsageFileSink }

products/<product>/bootstrap
  -> 只创建一次 RuntimeLogging
  -> 注入 shared builder / 各能力域

internal/bootstrap.BuildAgentService
  -> 接收已构造的 Logger / AuditSink / UsageSink
  -> 不再自己 NewFileLogger
```

约束：

- `capabilities/*` 不直接打开日志文件。
- Audit/Usage 的内存 Sink 可换成「内存 + 文件」或「仅文件」；文件实现放 `capabilities/audit/adapter/file`、`capabilities/usage/adapter/file`，底层复用 `platform/logger` 的滚动 Writer。
- Gateway / Skill / Approval 继续打到注入的 Sink，不关心文件名。

---

## 七、字段最小集

### agent（text 示例）

```text
[20:15:01] [INFO ] Run启动  run_id=run-1 session_id=s1 component=runtime
[20:15:02] [WARN ] Skill名被当作Tool调用，已同轮改写  run_id=run-1 component=skill requested=office-ppt
```

常用字段：`run_id`、`session_id`、`component`、`tool`、`skill`、`error`。

### audit（jsonl）

```json
{"ts":"2026-07-09T20:15:02+08:00","type":"approval.decision","action":"skill.load","resource":"Skill(office-ppt)","decision":"approved","run_id":"run-1","tool":"Skill"}
```

### usage（jsonl）

```json
{"ts":"2026-07-09T20:15:03+08:00","type":"llm.tokens","run_id":"run-1","model":"...","prompt_tokens":120,"completion_tokens":80,"total_tokens":200}
```

---

## 八、实施清单（一次做完，不再分期）

1. 扩展 `LogConfig`（`dir` + `rotate` + `channels`；兼容旧 `path`）。
2. `platform/logger` 实现按日/大小/保留的安全 Writer；三类文件工厂。
3. `audit`/`usage` 增加 file adapter（jsonl + 滚动）。
4. CLI bootstrap 唯一创建并注入；shared builder 删除自建文件日志。
5. 默认目录 `.genesis/logs/`；文档与 `config.yaml` 注释同步。
6. 单测：日切命名、大小续卷、retain、单 Writer、旧 `path` 兼容、禁止密钥字段的基础红线（文档+必要时测试辅助）。

---

## 九、决策摘要

1. **一次性落地 3 类日志：`agent` / `audit` / `usage`。**  
2. **不按组件拆文件；不单独做 error/http/trace 文件。**  
3. **统一按日滚动 + 大小续卷 + 分类保留期（agent 14 天，audit/usage 90 天）。**  
4. **agent 默认 text；audit/usage 固定 jsonl。**  
5. **Logger/Sink 只创建一次并注入；禁止双 Writer。**

残留风险（可接受）：

- 本机多进程同写同一文件可能交错；CLI 单进程为主。
- 文件 audit/usage 仍非 WORM/DB；Enterprise 强合规可再接数据库，但通道分类与字段契约保持不变。
