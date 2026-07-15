---
title: Windows 本地平台沙箱实现设计
status: 已实现 Phase A/B/C
---

# Windows 本地平台沙箱实现设计

> 状态：**已实现 Phase A/B/C**（L1 Token+Job、L2 Unelevated ACL 强隔离以及 L3 Phase C 本地网络隔离均已成功落地）；**Phase D 待规划**。
> 更新日志：
> - 2026-07-14：Phase C 本地网络隔离完成。采用离线受限账号 `GenesisSandboxUser` + WFP 出站阻断规则（PowerShell-based）+ DPAPI 密钥管理架构，达成普通用户免提权运行时禁网且兼容通用开发工具链的目标。
> - 2026-07-12：Phase B L2 ACL 隔离完成。通过 Win32 API 动态操作 DACL 机制，实现无提权要求下的文件系统强隔离。

---

## 1. 目标与非目标

### 1.1 目标

在 Windows 上提供与 macOS Seatbelt / Linux bubblewrap **语义可对齐、能力显式分级** 的本地沙箱，使：

1. `sandbox.mode=local_platform_sandbox` + `default_execution=optional|required` 在 Windows 上有真实可用路径；
2. **文件系统可写区 / 只读 / deny-read** 可被平台真实表达，不再用 Restricted Token「冒充」FS 沙箱；
3. 网络隔离作为后续等级真实实现（WFP / offline identity），禁止 env 伪禁网；
4. `SandboxRequired`（CLI：`required`）能力不足时 **fail-closed**；`SandboxAuto`（CLI：`optional`）降级必须带 warning/trace/audit，且 **不得逆标** enforcement；
5. 与 Codex Windows sandbox 安全语义对齐，实现落在 Genesis 包边界内。

### 1.2 非目标
- 不替换 `genesis-sandbox` / `office-basic` 镜像（LibreOffice/Poppler 等仍优先容器）；
- 不宣称 Windows 与 macOS/Linux 字节级等价；
- 不做企业 RBAC/多租户；不做容器内再次 Windows AppContainer；
- Phase B/C 不强上 Codex Elevated NUX / private desktop / 完整 WFP 控制面板服务。

---

## 2. 现状与实现进度对照 (2026-07-14)

状态约定：`[x]` 已实现 · `[/]` 部分实现 · `[ ]` 未实现

| 设计点 | 状态 | 实现证据 / 缺口 |
| --- | :---: | --- |
| L1 Token + Job 执行路径 | `[x]` | `spawn.go` + `execution/runner_windows.go` `RunArgvProcessConstrained` |
| FS/网策略对 L1 诚实拒绝 | `[x]` | `windows/policy.go` + `windows_policy_test.go` |
| AppContainer 探测与隔离 | `[x]` | `EvaluateAppContainerSupport` |
| `required` 能力不足 fail-closed | `[x]` | `manager.go` + CLI Windows 测例 |
| `optional` 降级 + enforcement 不虚标 | `[x]` | 降到 `none`，非「L1 却标 filesystem」 |
| 审计 `sandbox.kind` / `enforcement_level` | `[x]` | `Plan.CompleteAuditTags` |
| 审计 `sandbox.windows_level` | `[x]` | `CompleteAuditTags` 写入 `token_job` / `unelevated_acl` / `genesis_sandbox_user` |
| `required`+FS/NET 失败文案指向 setup | `[x]` | 明确指引运行 `genesis-cli sandbox windows-setup` |
| 错误码包内稳定 | `[x]` | execution 透传 `sandbox_policy_unsupported` 且可选降级完美支持 |
| L2 ACL setup / readiness / SetupErrorCode | `[x]` | `acl_setup.go` / `readiness.go` / `sandbox_cmd.go` |
| BuildPlan：需 FS 时走 L2，需 NET 时走 L3 | `[x]` | `platform_windows.go` 进行分支指派 |
| `required` + workspace FS/NET 在 Windows 成功 | `[x]` | 只要管理员 setup --network 完成即可直接运行 |
| Phase C 网络隔离 | `[x]` | `network.go` + `secrets.go` + DPAPI 管理 |
| Phase D Elevated / AppContainer / UX | `[ ]` | 待未来规划 |
| §5 目标文件拆分（`token.go` / `job.go` / `spawn.go`…） | `[x]` | 已完成彻底重构，废弃 `runner_windows.go` |
| `pathutil` 在 grant 前规范化 | `[x]` | 引入 `pathutil` 的绝对路径解析 |

---

## 3. 参考结论（Codex / Kode）

### 3.1 Codex（主参考）

参考路径：
- `D:\workspace\go\go-project\codex\codex-rs\windows-sandbox-rs\`
- `D:\workspace\go\go-project\codex\codex-rs\core\src\windows_sandbox.rs`

| Codex 等级 | 含义 | 映射到本项目 |
| --- | --- | --- |
| Disabled | 不沙箱 | L0 |
| RestrictedToken / Unelevated | 低权限身份 + ACL grant | **L2（Phase B）** |
| Elevated + setup helper | identity、ACL、网络 identity | **L3（Phase C）** |

必借鉴：
1. **Setup 与 Run 分离**；setup 失败不静默降级；
2. FS 策略落到 **ACL / SID**，不是只卡 argv；
3. 网络用真实隔离（通过隔离账户在 WFP/防火墙级封禁），不用「删环境变量」；
4. 路径经最终路径规范化（Genesis `pathutil`）；
5. readiness / 失败码可观测。

---

## 4. 能力分级（产品可见）

| Level | `Type` | `EnforcementLevel` | `windows_level` | 可承诺 | 不可承诺 |
| --- | --- | --- | --- | --- | --- |
| L0 | `none` | `none` | `disabled` | 宿主直接执行 | 任何隔离 |
| L1 | `windows_process_constrained` | `process_constrained` | `token_job` | 降权 Token + Job 杀树 | FS 隔离、网络隔离 |
| L2 | `windows_process_constrained` | `filesystem` | `unelevated_acl` | 限制写 workspace 外部 | 网络隔离 |
| L3 | `windows_process_constrained` | `filesystem_network` | `genesis_sandbox_user` | FS 隔离 + 真实禁网 | 桌面会话级别隔离 |

---

## 5. 分阶段实现计划

### Phase A — 契约与诚实标签（已完成）
1. 审计：`sandbox.kind`、`sandbox.enforcement_level` 正常记录；
2. 审计：`sandbox.windows_level` 区分记录 `token_job` 和 `unelevated_acl`；
3. 保持 L1 fail-closed 矩阵单测；
4. 失败文案包含友好引导，指示运行 `windows-setup`；
5. 错误码统一由 execution 透传为 `sandbox_policy_unsupported` 和支持 optional 降级。

### Phase B — L2 Unelevated ACL MVP（已完成）
1. 使用 Restricted Token 配合 `SidsToRestrict` 降权；
2. 对 `WritableRoots` 授权 Capability SID 写，Workspace 外部默认拒绝写入；
3. Readiness 可观测性；
4. 路径规范化校验。

### Phase C — 网络隔离（L3 子集，已完成）
1. **技术选型与 ADR 确立**：出于工具链（git, python）兼容性考虑，确立以 **离线本地用户账号 (GenesisSandboxUser) + 密码 DPAPI 安全保护 + WFP 防火墙拦截** 为技术栈。
2. 禁网策略真实生效（出站连接在 `NetworkDisabled` 下被防火墙封锁，loopback 连接除外）。
3. 补充 `TestLocalUserNetworkIsolation` 禁网探测与 DPAPI 保护测试。

### Phase D — Elevated / AppContainer / UX（待规划）
1. Elevated setup helper (参考 Codex 交互设计)；
2. AppContainer 作为可选强隔离容器环境；
3. 完善 CLI/Desktop setup 图形化引导与日志审计。

---

## 6. 代码落点与测试

| 路径 | 职责 | 状态 |
| --- | --- | --- |
| `shared/local/sandbox/windows/*` | 平台实现 | **已完成**：拆分为 `token.go` / `job.go` / `spawn.go` / `acl_setup.go` / `readiness.go` / `policy.go` / `detector_*.go` / `network.go` / `secrets.go` |
| `shared/local/sandbox/platform_windows.go` | Detect/BuildPlan | **已完成**：FS 策略下生成 L2 规划，NET 策略下生成 L3 规划，并进行 readiness 检测 |
| `shared/local/sandbox/manager.go` | Preference 与降级 | **已完成**：auto 降级至 `none` 逻辑完备 |
| `shared/local/sandbox/model.go` | Type / Enforcement / Plan | **已完成**：`windows_level` 正常上报 AuditTags |
| `shared/local/execution` | 执行 Plan | **已完成**：受限子进程通过 `PrepareRestrictedCommand` 安全拉起 |
| `products/cli` | 命令行及 setup 入口 | **已完成**：`genesis-cli sandbox windows-setup --network` 一键初始化 |

---

## 7. 验收清单

- [x] L1 不乱标 FS/网策略（策略要求 FS/网时拒绝或降级，不冒充）；
- [x] audit 完整一致（kind/enforcement/windows_level）；
- [x] L2 setup + spawn 在干净 Windows 可复现；
- [x] `required` + workspace FS/NET 在 Windows 可成功；
- [x] 错误码：包内稳定；产品观测面已正确区分并支持 fallback；
- [x] optional 降级报 warning，且 enforcement 不虚标；
- [x] Setup 入口可用（CLI `--network` 标志完备）；
- [x] 单元测试覆盖网络阻断及 DPAPI 口令安全。
