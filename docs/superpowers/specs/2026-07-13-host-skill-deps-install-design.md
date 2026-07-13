# 宿主 / 本地平台 Skill 依赖安装修复设计

> 状态：已审查收敛（review-fix-rereview，2026-07-13）  
> 关联：`docs/Skill三模式执行与依赖闭环设计.md` §6.5、`docs/本地沙箱设计.md`、`docs/Windows本地平台沙箱实现设计.md`  
> 证据：`.genesis/logs/agent.log`（`run-1783951692101223600`，Windows 宿主 `npm --prefix "..."` → ENOENT）

---

## 0. 审查记录（从第一性原理）

### 0.1 第一性原理

| 项 | 内容 |
| --- | --- |
| 真问题 | 无沙箱宿主上，对话期 `install_skill_dependencies` 在 Windows 生成带引号绝对路径的 `npm --prefix "..."`，经 `Shell:auto`→`cmd /c` 后路径被弄坏，装包失败；后续 `run_skill_command` 缺 `pptxgenjs` |
| 最小必要结果 | L0（宿主直跑）装包成功且 `NODE_PATH` 能看见 `.genesis/skill-deps/<skill>/node/node_modules`；Agent 可再跑同一脚本 |
| 硬约束 | **不得**改动已调通的 genesis-sandbox HTTP/session、远程路径约定（`/workspace`、镜像 `NODE_PATH`、stage/zip 等） |
| 不变量 | 三模式只换 backend；安装走专用通道；远程 workspace install 仍不可见则必须拒绝；禁止跨 backend 串味 |
| 失败条件 | 再引入 `--prefix "D:\..."` 类 shell 引号路径；或为「统一」去改 remote session / 远程路径解析 |

### 0.2 审查轮次

| 轮次 | 发现 | 处置 |
| --- | --- | --- |
| R1 | 草案强调「结构化 Argv / Shell:none」——但 `execmodel.Command` **无 Argv 字段**，本地 runner 一律 `ShellArgv` 包装；强推 Argv 会波及 execution 契约与 sandbox 适配，威胁面过大 | **拍板**：本期用 **cwd 相对安装**（无 `--prefix` 绝对路径），经现有 `Shell:auto` 即可；Argv 契约属后续可选增强 |
| R1 | 「L0/L2 共用 InstallExecutor」易被读成新建执行器 | 改为：**同一 InstallPlan 生成规则**；仍走现有 `ExecutionRunner`，仅按 SandboxProfile 分流 |
| R1 | 未写死冻结面，易在「顺手统一」时碰到 remote | 增加 §2 冻结清单 |
| R2 | 本地平台沙箱 WritableRoots 是否本期必做？ | **延期**：当前失败在 `mode=disabled`；L2 启用时须把 `.genesis/skill-deps` 列入可写根（§6 残余风险） |
| R2 | pip `--target` 若仍拼绝对路径+引号，Windows 可能同类故障 | InstallPlan 对 pip 同样 **cwd=python 目录 + `--target .`**（或 `--target=.`），禁止再 quote 绝对路径 |

**残余风险**：Windows 本地平台沙箱 L2（ACL）未落地前，`required`+FS 仍按既有文档 fail-closed；本期不假装已修好 L2 装包隔离。

---

## 1. 三层模型（必须区分）

| 层 | 配置 | 依赖安装（Gate B） | 业务脚本如何看见依赖 |
| --- | --- | --- | --- |
| **L0 宿主直跑** | `sandbox.mode=disabled` / `local_host` | `workspace` → `.genesis/skill-deps/<skill>/{node,python}` | `NODE_PATH` / `PYTHONPATH` 指向该落点（现有 `skillDependencyRoot`） |
| **本地平台沙箱** | `local-platform` + optional/required | **同一落点与同一命令形态**；经现有 Runner/SandboxRunner；须在可写 roots 内（L2 就绪后） | 同左 |
| **沙箱 API** | `genesis-sandbox` | **保持现状**：`remoteSandboxWorkspaceInstallUnsupported` → 拒 workspace install；依赖镜像/profile | `/opt/genesis-sandbox/image/node_modules` 等（**不改**） |

统一：安装意图、审批、审计、`failure_kind`、本地落点约定。  
区分：谁执行、远程是否允许 workspace install、路径命名空间（宿主 FS vs `/workspace`）。

---

## 2. 冻结面（禁止改动）

下列内容属已调通远程闭环，**本设计与后续实现一律不得修改行为或契约**（含「顺手重构」）：

1. genesis-sandbox **HTTP / Session / File** client 与 adapter  
2. `run_skill_command` 的 **`runRemote`** 路径：session 打开/复用、zip stage、job 内解压、`/workspace` / `/workspace/input|output|tmp`  
3. 远程 `NODE_PATH` / `PYTHONPATH` 分支（含 `/opt/genesis-sandbox/image/node_modules` 等）  
4. `remoteSandboxWorkspaceInstallUnsupported` 的拒绝语义与文案意图  
5. 远程产物回收、resource id、与 sandbox 仓的 profile/operation 约定  

允许改动的仅限：

- `internal/capabilities/skill/tool/install_skill_dependencies/**`（命令形态与 per-manager cwd）  
- 其单测  
- 必要时 **仅宿主侧** `NODE_PATH`/`PYTHONPATH` 注释或测试（若发现与 install 落点不一致）；**不得**改 remote 分支逻辑  

---

## 3. 根因与修复策略

### 3.1 根因

```text
npm install --prefix "D:\...\skill-deps\office-ppt\node" pptxgenjs
  → Windows cmd + npm.cmd 引号处理错误
  → mkdir ...\office-ppt\"D:\...\node" → ENOENT
```

### 3.2 策略（最佳实践，最小爆炸半径）

**InstallPlan（本地 L0 / 本地平台共用）**：

| Manager | Cwd | Command 字符串（无绝对路径、无引号包装路径） |
| --- | --- | --- |
| npm | `{installRoot}/node` | `npm install <pkg>...` |
| pip | `{installRoot}/python` | `python -m pip install --target . <pkg>...` |

其中 `installRoot = {workspace}/.genesis/skill-deps/<skillID>`（与现有 `skillDependencyInstallRoot` 一致）。

执行循环：

1. 仍先 `MkdirAll` 各 target 目录  
2. **每个 manager 一次** `Runner.Run`：`Cwd` = 上表目录；`Command` = 上表；`Shell: auto`（保持现有契约）  
3. `Workspace.WorkDir/TmpDir` 与该次 Cwd 对齐  
4. SandboxProfile：继续 `Operation=build_dependencies`、`RuntimeProfile=skill-build-polyglot`（本地语义）；**不**借此打开远程 session  

**明确不做（本期）**：

- 扩展 `execmodel.Command` 增加 Argv  
- 改 `shared/local/execution` 的 Shell 解析以「绕过 cmd」  
- 让远程也装到宿主 `skill-deps`  
- 为本地平台新建独立 InstallExecutor  

---

## 4. 与既有文档对齐

- 对齐 `Skill三模式…` §6.5：「本地平台与无沙箱落点相同」；本设计把命令形态收敛为 cwd 相对，避免 Windows shell 引号。  
- 对齐 `本地沙箱设计.md`「优先结构化 argv」的**精神**：本期用「命令内不含需引号的绝对路径」达到同等安全；完整 Argv 进 Command 契约另开任务。  
- 对齐 `Windows本地平台沙箱…`：隔离靠 Token/ACL/WritableRoots，不靠另写装包协议；Office 重依赖仍走 genesis-sandbox。

---

## 5. 实现与验证

### 5.1 代码

1. 改 `buildInstallCommands` → 改为返回 `{cwd, command}`（或等价结构），删除对 install 路径的 `quoteShellArg` 依赖（若 pip/npm 不再需要可删或仅保留给非路径用途）。  
2. 改执行循环使用 per-manager `Cwd`。  
3. 更新 `tool_test.go`：断言命令为 `npm install pptxgenjs` 且 cwd 以 `...\skill-deps\office-ppt\node` 结尾；**增加**模拟 Windows 路径的用例（即便在非 Windows CI 也拼 `D:\...` 风格字符串断言「命令中不含 `--prefix` / 不含引号绝对路径」）。  
4. 不修改 `script/service/service.go` 的 `runRemote` / 远程 `nodeRuntimeSearchPath`。

### 5.2 验收

| 用例 | 期望 |
| --- | --- |
| Windows L0：`install_skill_dependencies(office-ppt, pptxgenjs)` | exit 0；存在 `.genesis/skill-deps/office-ppt/node/node_modules/pptxgenjs` |
| 同会话再 `run_skill_command` `node create_*.js` | 不再 `Cannot find module 'pptxgenjs'`（脚本本身正确的前提下） |
| `provider=genesis-sandbox` + workspace install | 仍被拒绝，`failure_kind=install_scope_not_visible`（行为不变） |
| 远程 `run_skill_command` 回归 | 不因本改动失败（代码未触达） |

### 5.3 回滚

仅回滚 `install_skill_dependencies` 相关提交即可；远程路径无耦合。

---

## 6. 残余风险与延期

| 项 | 说明 |
| --- | --- |
| 本地平台 L2 WritableRoots | 启用 FS 沙箱后须 grant `.genesis/skill-deps`；属 Windows L2 落地任务，非本期阻塞 |
| Command.Argv 契约 | 长期更优；本期不引入 |
| npm 全局/用户 prefix 环境变量干扰 | 低概率；若出现再在 InstallPlan 的 Env 中钉死 `npm_config_prefix` 为空或指向 cwd（仍不碰 remote） |

---

## 7. 决策摘要

1. **选 A**：L0 与本地平台共用 InstallPlan / 落点；只换 Runner backend。  
2. **修法**：cwd 相对 `npm install` / `pip install --target .`，禁止 `--prefix "abs"`。  
3. **冻结**：genesis-sandbox HTTP/session/远程路径与拒绝语义零改动。  
)
