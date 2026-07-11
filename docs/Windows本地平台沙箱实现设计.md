# Windows 本地平台沙箱实现设计

> 状态：待实现（review-fix-rereview 修订，2026-07-11）  
> 范围：仅 CLI/Desktop 的 **本机 Windows 平台沙箱**（`shared/local/sandbox/windows`）。  
> 不覆盖：`genesis-sandbox` Docker/远程 API（见 `docs/沙箱API对接与Profile选择规则.md`）。  
> 总览与跨平台原则：见 `docs/本地沙箱设计.md`。本文是 Windows 专项落地规格。

---

## 0. 审查记录（从第一性原理）

| 轮次 | 发现 | 处置 |
| --- | --- | --- |
| R1 | 真问题不是「Windows 不能沙箱」，而是 **L1 Token+Job 不能诚实表达 FS/网策略**；产品 `required`+workspace FS 因此 fail-closed | 保持该诊断；L2 以 Codex Unelevated（Token+ACL）为主路径 |
| R1 | L2 写成「低权限用户 **或** AppContainer」选型悬空，易自创第三套 | **拍板**：Phase B = Codex Unelevated（Restricted Token + ACL grant）；AppContainer 并入后期强隔离，不与 L2 并行试错 |
| R1 | 拟造 `windows_acl_sandbox` 与现有 `model.Type` 不一致 | 用现有 Type + `windows_level` 元数据；必要时再扩枚举，禁止 silently 把 L2 标成 `windows_appcontainer` |
| R1 | `optional` 可降到 L1，若策略仍声明 FS 会被误解为「已隔离」 | 降级规则收紧：FS 策略未兑现时必须写 `unsupported_reasons`，且 **不得** 把 enforcement 标成 filesystem |
| R1 | Setup 谁触发未写清 | 增加 §5.3 Setup 入口 |
| R1 | 配置示例 `enabled: true` 与「enabled=外部 API」文档语义冲突 | §5.1 写明当前 CLI `FromRuntimeConfig` 的真实门闩行为，避免配错 |

**残余风险**：ACL/setup 工程复杂度高；本文不假装 Phase B 已等于 Codex Elevated。Office 重依赖仍走 genesis-sandbox。

---

## 1. 目标与非目标

### 1.1 目标

在 Windows 上提供与 macOS Seatbelt / Linux bubblewrap **语义可对齐、能力显式分级** 的本地沙箱，使：

1. `sandbox.mode=local_platform_sandbox` + `default_execution=optional|required` 在 Windows 上有真实可用路径；
2. **文件系统可写根 / 只读 / deny-read** 可被平台真实表达，不再用 Restricted Token「冒充」FS 沙箱；
3. 网络隔离作为后续等级真实实现（WFP / offline identity），禁止 env 伪禁网；
4. `SandboxRequired`（CLI：`required`）能力不足时 **fail-closed**；`SandboxAuto`（CLI：`optional`）降级必须带 warning/trace/audit，且 **不得虚标** enforcement；
5. 与 Codex Windows sandbox 安全语义对齐，实现落在 Genesis 包边界内。

### 1.2 非目标

- 不替代 `genesis-sandbox` / `office-basic` 镜像（LibreOffice/Poppler 等仍优先容器）。
- 不宣称 Windows 与 macOS/Linux 字节级等价。
- 不做企业 RBAC/多租户；不做容器内再套 Windows AppContainer。
- Phase B 不强制 Codex Elevated NUX / private desktop / 完整 WFP。

---

## 2. 现状（必须先承认的事实）

### 2.1 Genesis 已有

| 能力 | 状态 |
| --- | --- |
| Restricted Token 启动 | `windows/runner_windows.go`：`CreateRestrictedToken` + `SysProcAttr.Token` |
| Job Object 杀进程树 | 同文件：`CreateJobObject` + `AssignProcessToJobObject` + kill-on-close |
| 策略探测 | `EvaluateProcessConstrainedSupport`：一旦策略要求 **filesystem sandbox** 或 **network 隔离** → **不支持** |
| AppContainer | `EvaluateAppContainerSupport` **显式 fail-closed**（未完成） |
| CLI `required` | Windows 测例：`TestRunCommandRequiredSandboxFailsClosedOnUnsupportedWindowsPolicy` |

因此：对产品常见的「workspace 可写 +（常带）禁网」策略，当前 Windows 本地平台沙箱 **不可用**——不是 Detect 失败，是 **能力不足以诚实声明强隔离**。

### 2.2 为什么不能把 Token+Job 当强沙箱

Restricted Token 主要降低特权；Job Object 主要管生命周期/资源。它们：

- **不能**可靠表达「只能写 workspace、不能读指定敏感路径」；
- **不能**表达「禁止出站 / 仅代理」；
- 硬链接、junction、reparse point、短路径等仍可能绕过朴素路径判断。

不变量：`process_constrained ≠ filesystem/network sandbox`。

---

## 3. 参考结论（Codex / Kode）

### 3.1 Codex（主参考）

参考路径：

- `D:\workspace\go\go-project\codex\codex-rs\windows-sandbox-rs\`
- `D:\workspace\go\go-project\codex\codex-rs\core\src\windows_sandbox.rs`
- 打包辅助：`codex-windows-sandbox-setup.exe`、`codex-command-runner.exe`

| Codex 等级 | 含义 | 映射到本文 |
| --- | --- | --- |
| Disabled | 不沙箱 | L0 |
| RestrictedToken / Unelevated | 低权限身份 + ACL grant | **L2（Phase B）** |
| Elevated + setup helper | identity、ACL、网络 identity、可选 private desktop | L3 / Phase D |

必借鉴：

1. **Setup 与 Run 分离**；setup 失败不静默降级。  
2. FS 策略落到 **ACL / SID**，不是只改 argv。  
3. 网络用真实隔离，不用「删环境变量」。  
4. 路径经最终路径规范化（Genesis `pathutil`）。  
5. readiness / 失败码可观测。

### 3.2 Kode-CLI（对照）

Windows **不是** Kode 的一等 OS 沙箱目标（网络沙箱仅 darwin/linux）。Genesis **跟 Codex**，同时保留 genesis-sandbox 作重依赖后备。

---

## 4. 能力分级（产品可见）

| Level | `Type`（`shared/local/sandbox`） | `EnforcementLevel` | `windows_level`（audit） | 可承诺 | 不可承诺 |
| --- | --- | --- | --- | --- | --- |
| L0 | `none` | `none` | `disabled` | 宿主直接执行 | 任何隔离 |
| L1 | `windows_process_constrained` | `process_constrained` | `token_job` | 降权 Token + Job 杀树 | FS 根、禁网 |
| L2 | **仍用** `windows_process_constrained`（或日后显式新增 Type，须改 model） | `filesystem` | `unelevated_acl` | setup 后：WritableRoots 可写 + deny-read 生效 | 网络隔离 |
| L3 | `windows_appcontainer`（和/或 elevated 专用 Type） | `filesystem_network` | `elevated` / `appcontainer` | FS + 网络真实生效 | 与容器镜像能力同等 |

规则：

1. 策略 `RequiresFilesystemSandbox=true` → 至少 L2；否则 `sandbox_policy_unsupported` / `sandbox_unavailable`。  
2. 策略 `NetworkDisabled|ProxyOnly|Loopback` → 至少 L3；**禁止**用 env 伪禁网并标 `filesystem_network`。  
3. L1 **仅当**策略不要求 FS sandbox、且网络为 `full_access`（或不限制）。  
4. Plan 必须同时写清 `Type`、`EnforcementLevel`、`windows_level`、`unsupported_reasons`；禁止 L1 却标 `filesystem`。

---

## 5. 目标架构

```text
products/cli|desktop bootstrap
  → ExecutionRunner / SandboxRunner
  → shared/local/sandbox.Manager.BuildPlan
  → windowsBackend.Detect / BuildPlan
  → shared/local/sandbox/windows
        ├─ detect.go / policy.go
        ├─ token.go / job.go          # L1/L2 共用
        ├─ acl_setup.go               # L2：identity + ACL（Codex Unelevated 子集）
        ├─ readiness.go               # setup 完成态、失败码
        ├─ spawn.go                   # Cmd / helper
        └─ network.go                 # L3 后期

PathResolver / pathutil 在 grant 前规范化所有 root。
```

边界：只放 `shared/local/sandbox/windows`；Manager 产 Plan；执行在 `shared/local/execution`；不进 enterprise / genesis-sandbox HTTP adapter。

### 5.1 配置（含当前门闩）

```yaml
sandbox:
  # 注意：当前 CLI FromRuntimeConfig 在 enabled=false 时直接 DefaultConfig(local_host+disabled)，
  # 会忽略下方 mode。启用本地平台沙箱时必须 enabled=true（命名历史负担；改配置语义另开任务）。
  enabled: true
  mode: local_platform_sandbox
  default_execution: optional   # 或 required；对应 Preference auto / required
  allow_session_override: true
```

| execution（CLI） | Preference | Windows 行为 |
| --- | --- | --- |
| `disabled` | disabled | L0 |
| `optional` | auto | 优先满足策略所需最低 Level；不足则降级并 **warning + unsupported_reasons**；**不得虚标 enforcement** |
| `required` | required | 必须达到策略所需 Level；否则失败，禁止静默宿主执行 |

会话 `--sandbox` 只覆盖 execution；`mode` 仍由配置决定。

**optional 降级细则（可行动约束）：**

- 策略要 FS，但仅有 L1 → 可继续执行 **仅当** 产品允许 auto 降级；Plan 的 `EnforcementLevel` 必须是 `process_constrained` 或 `none`，并列出「FS 策略未兑现」。  
- 禁止：降级到 L1 却向用户/审计声称「已文件系统沙箱」。

### 5.2 与 genesis-sandbox 分工

| 场景 | 推荐 |
| --- | --- |
| 普通 shell / 轻量脚本，要本地 FS 隔离 | Windows L2 |
| Office（soffice/pdftoppm）等 | `remote_sandbox` / `docker_sandbox` + `office-basic` |
| 企业默认强隔离 | 远程 genesis-sandbox；本地 Windows 为 CLI/Desktop 增强 |

### 5.3 Setup 入口（Phase B 必须有）

L2 在首次可用前需要 setup（对齐 Codex setup/run 分离）：

| 入口 | 说明 |
| --- | --- |
| CLI 子命令（拟） | 如 `genesis-cli sandbox windows-setup`（名称实现时定）；幂等，写 readiness |
| 首次 BuildPlan | `required`：无 readiness → 失败并提示先 setup；`optional`：warning + 按 §5.1 降级 |
| Desktop（后期） | 引导 UI；本期可不做 |

Setup 产物放用户可读位置（如 `%LOCALAPPDATA%/genesis/sandbox/` 或 workspace `.genesis/sandbox/`），失败写稳定 `SetupErrorCode`。

---

## 6. 分阶段实现计划

### Phase A — 契约与诚实标签

1. 审计统一：`sandbox.kind`、`sandbox.enforcement_level`、`sandbox.windows_level`。  
2. 保持 L1 fail-closed 矩阵单测。  
3. Windows `required`+FS 失败文案指向本文 / genesis-sandbox / 「需完成 windows-setup」。

**DoD**：无「已沙箱」假阳性；错误可读。

### Phase B — L2 Unelevated ACL MVP（主交付）

**技术选型（已拍板）**：Codex **Unelevated / RestrictedToken + ACL grant**，不是 AppContainer。

1. Sandbox identity + Restricted Token（复用并强化现有 token 路径）。  
2. Setup：对 `WritableRoots` grant 写；`UnreadablePaths` / deny-read deny；默认不可写 workspace 外。  
3. Readiness + SetupErrorCode（§5.3）。  
4. Spawn：受限身份 + Job Object。  
5. Path：全部经 `pathutil`；与 PathResolver 一致。  
6. `BuildPlan`：需要 FS 时走 L2；无 readiness → unsupported（required 失败 / optional 按 §5.1）。

**DoD**：

- 沙箱进程可写 workspace 测文件，不能写测例指定的 workspace 外路径。  
- `required` + FS 在 setup 后通过；未 setup 失败并提示 setup。  
- 审计：`windows_level=unelevated_acl`，`enforcement=filesystem`。

### Phase C — 网络隔离（L3 子集）

1. 在 WFP vs Codex offline identity 中选型并记录 ADR。  
2. `NetworkDisabled` / `ProxyOnly` 真实生效。  
3. 与 optional/required 对齐。

**DoD**：出站探测在 Disabled 下失败。

### Phase D — Elevated / AppContainer / UX

1. Elevated setup helper（参考 Codex，不复制协议）。  
2. AppContainer 作为可选强隔离实现（对应现有 `TypeWindowsAppContainer`）。  
3. CLI/Desktop setup 引导；private desktop / conpty 按需。

---

## 7. 代码落点与测试

| 路径 | 职责 |
| --- | --- |
| `shared/local/sandbox/windows/*` | 平台实现 |
| `shared/local/sandbox/platform_windows.go` | Detect/BuildPlan |
| `shared/local/sandbox/manager.go` | Preference 与降级（虚标禁止） |
| `shared/local/sandbox/model.go` | 若需新 Type，显式扩展 |
| `shared/local/execution` | 执行 Plan |
| `products/cli` | 配置、`windows-setup` 命令、错误文案 |

测试：policy 矩阵；setup readiness；Windows 集成测 L2 边界；required fail-closed；optional 降级 warning + enforcement 诚实；非 Windows stub 不得假装 L2 通过。

---

## 8. 风险与决策

| 风险 | 缓解 |
| --- | --- |
| ACL/setup 复杂度 | 跟 Codex Unelevated 子集；不一次上 Elevated |
| `enabled` 语义混淆 | §5.1 明示；配置重构另开任务 |
| 虚标隔离 | Plan 字段约束 + 单测 |
| Junction/硬链接 | pathutil + 最终路径；关键 deny |
| Setup 权限 | Unelevated 能做则做；需管理员的进 Phase D |

---

## 9. 验收清单

- [ ] L1 不用于 FS/网策略；audit 一致  
- [ ] L2 setup + spawn 在干净 Windows 可复现  
- [ ] `local_platform_sandbox` + `required` + workspace FS 在 Windows **可成功**  
- [ ] 错误码：`sandbox_unavailable` / `sandbox_policy_unsupported` 稳定  
- [ ] optional 降级有 warning，且 enforcement **不虚标**  
- [ ] Setup 入口可用（CLI 或文档化临时流程）  
- [ ] `本地沙箱设计.md` / `文件系统设计方案.md` §9.5 交叉引用已更新  

---

## 10. 参考索引

- Genesis：`docs/本地沙箱设计.md`、`docs/文件系统设计方案.md` §9.5、`docs/沙箱API对接与Profile选择规则.md`
- Codex：`codex-rs/windows-sandbox-rs`、`codex-rs/core/src/windows_sandbox.rs`
- Kode：`BashTool/sandboxNetwork.ts`（Windows 非主路径对照）
