# Skill 脚本执行契约设计

> 状态：已实现（CLI 已接线；Enterprise 已通过 shared/skillstack 接线 embed Skills + SharedScriptsFS）  
> 日期：2026-07-09（更新 2026-07-10）  
> 关联：`docs/沙箱API对接与Profile选择规则.md`、`docs/执行工作空间与Sandbox文件路径契约.md`

## 第一性原理

- Skill `scripts/` 是**可执行资源**（ResourceID），不是宿主机 path。
- 内置（embed）与用户磁盘 Skill **同一 Runner**；差异只在 Materialize 来源。
- `INPUT_DIR`/`OUTPUT_DIR`/`WORK_DIR`/`SKILL_DIR` 是逻辑契约；三种 backend 只换映射。
- Office/Skill 产物任务用 `local_task_workspace`（strict）；禁止模型用 `write_file` 伪造 `.pptx`。

## 入口

新增工具 `run_skill_script`：

```text
skill + script(ResourceID) + args[] + inputs[]
  -> SkillScriptService
  -> Materialize scripts -> SKILL_DIR
  -> Stage inputs -> INPUT_DIR
  -> Select profile (office-basic / skill-polyglot-...)
  -> ExecutionRunner + ExecutionWorkspace env
  -> Collect OUTPUT_DIR + 轻量产物门禁
```

## 目录映射

| 逻辑 | 本地任务 | 远程/Docker |
| --- | --- | --- |
| WORK_DIR | `.genesis/runs/<id>/work` | `/workspace` |
| INPUT_DIR | `.genesis/runs/<id>/input` | `/workspace/input` |
| OUTPUT_DIR | `.genesis/runs/<id>/output` | `/workspace/output` |
| TMPDIR | `.genesis/runs/<id>/tmp` | `/workspace/tmp` |
| SKILL_DIR | `WORK_DIR/skills/<pkg>` | 本地同左；远程将 scripts stage 到 `INPUT_DIR/skills/<pkg>` 并以之为 SKILL_DIR |

## 不变量

1. 不向模型暴露 embed 内部路径或宿主机绝对路径作为脚本 cwd 契约。
2. Materialize 必须包含同包 `scripts/` 依赖文件（如 `path_contract.py`）。
3. 非 0 退出码视为脚本失败；`.pptx` 等交付物必须通过格式门禁才可 `ok=true`。
4. Profile 按 workload 选择（office-ppt → office-basic），来源记入 metadata。
5. 远程判定看 `SandboxProfile.Provider=genesis-sandbox` + Mode optional/required，不是 Mode 字符串本身。
6. `write_file` 拒绝纯文本冒充 `.pptx/.docx/.xlsx/.pdf`。

## 实现落点

| 组件 | 路径 |
| --- | --- |
| Contract | `internal/capabilities/skill/script/contract` |
| Materialize | `internal/capabilities/skill/script/materialize` |
| Workspace | `internal/capabilities/skill/script/workspace` |
| Gate | `internal/capabilities/skill/script/gate` |
| Service | `internal/capabilities/skill/script/service` |
| Tool | `internal/capabilities/skill/tool/run_skill_script` |
| CLI 接线 | `products/cli/bootstrap/container.go` |
| 共享装配 | `shared/skillstack`（embed Skills + SharedScriptsFS） |
| Enterprise 接线 | `products/enterprise/bootstrap/container.go` |
