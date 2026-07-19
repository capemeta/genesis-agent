# 文件路径授权广度与项目持久化设计

## 目标

对项目外（及需 ask 的）文件操作，审批决策拆成正交两维：

1. **时间作用域** `GrantScope`：`once` / `session` / `project`（`turn` 预留，本轮 UI 不暴露直至有明确清场点）
2. **路径广度** `PathGrantMode`：`exact`（仅此文件）/ `directory`（直接父目录及子树）

选择 `project` 时，授权写入工作区 `.genesis/grants.yaml`，跨进程生效。

## 不变量

- hard deny 永远优先于任何 grant（含已持久化 project grant）。
- grant 只能放宽 `ask`，不能绕过 `deny` / protected。
- 读写动作分离：`file.read` grant 不覆盖 `file.write`。
- `once` 不缓存、不持久化；路径广度仅对可记忆 scope 有意义。
- 目录类资源（`list`/`walk`）不展示「文件 vs 文件夹」二选一，按路径本身授予。
- `.genesis/` 已 gitignore；`grants.yaml` 可含宿主机绝对路径，不入库。

## 数据模型

```go
type PathGrantMode string // exact | directory

type Decision struct {
    Type     DecisionType
    Scope    GrantScope
    PathMode PathGrantMode // 可记忆 scope 时有效
    Reason   string
}

type RuntimeGrant struct {
    Action Action
    Scope  GrantScope
    Path   string // 归一化后的授权根路径
}
```

`directory` 模式下，对文件请求将授权根设为 `filepath.Dir(backendPath)`；`exact` 则为文件自身。

## 持久化

```yaml
version: 1
grants:
  - action: file.read
    path: "D:/data/reports"
    scope: project
```

- 仅持久化 `scope=project` 的文件 grant。
- 启动时 Load → 并入 `RuntimeFilePermissions`；Remember project 后原子写回（tmp+rename）。
- 父子目录合并算法与现有 runtime 一致。

## 产品交互（CLI）

文件 ask 且 scope 允许时：

| 键 | 语义 |
|----|------|
| Y/O | 本次 |
| S | 本会话 · 本文件 |
| D | 本会话 · 本文件夹 |
| P | 本项目 · 本文件（落盘） |
| F | 本项目 · 本文件夹（落盘） |
| N | 拒绝 |
| A | 中止 |

非文件审批保持原 once/session。

## 装配

CLI/Desktop：`workspaceRoot/.genesis/grants.yaml` → `RuntimeFilePermissions` + `ApprovalService` 包装。  
Enterprise：本阶段可复用同文件 store（本地工作区）；DB GrantStore 仍属后续阶段。

## 非目标

- 通用（非文件）GrantStore 全量替换
- 祖先路径多级选择器
- tenant/global 交互授权
- turn grant 清场与 UI（待有 Turn 边界钩子后再做）

## 实现约束（审查确认）

- CLI 选项仅在 `file.*` 动作上展示「本项目」；非文件动作尚无 project 持久化，禁止空选项。
- project 落盘通过 `persistMu` 串行化，避免与 `store.mu` / `RuntimeFilePermissions.mu` 死锁，并防止并行 Save 互相覆盖。
- Desktop/Enterprise 当前仅 view_image 路径接入同一 store；CLI 全量文件工具已接入。
