> **Superseded：** 产物登记、Deliverable 发布与 durable executor object 以 [`2026-07-17-produced-resource-artifact-delivery-architecture.md`](superpowers/specs/2026-07-17-produced-resource-artifact-delivery-architecture.md) 为准；下文部分 `OutputArtifacts`/`DownloadArtifact` 自动回收描述已过时。

# 执行工作空间与 Sandbox 文件路径契约

## 1. 结论

代码执行的文件路径规范不能绑定为“沙箱路径规范”，而必须定义为统一的 **Execution Workspace Contract**。

任务型代码执行、Skill 运行期脚本和远程 sandbox 只认逻辑目录：

- `WORK_DIR`：本次执行工作目录。
- `INPUT_DIR`：本次执行输入文件目录。
- `OUTPUT_DIR`：本次执行成果物目录。
- `TMPDIR`：本次执行临时目录。

不同执行 backend 负责把逻辑目录映射到真实目录。任务型代码不论运行在本地、平台沙箱、Docker sandbox 还是远程 genesis-sandbox，都应优先通过环境变量读写文件，而不是硬编码 `/workspace`、`/tmp`、用户 HOME、Windows 盘符或项目根目录。

但这里必须区分两类本地执行：

- **本地编程工具模式**：类似 Claude Code / Codex，目标是读取和修改用户授权的项目工作区。它的安全边界是 PathResolver、权限审批、Freshness、审计和本地进程隔离；不应强制要求所有读写都经过 `INPUT_DIR` / `OUTPUT_DIR` / `TMPDIR`。本模式采用 Codex-like 的 `cwd + writable_roots + protected metadata + approval` 工作路径模型。
- **本地任务运行模式**：类似“运行一段代码处理输入文件并返回成果物”。它应尽量使用 `INPUT_DIR` / `OUTPUT_DIR` / `TMPDIR`，以便重放、审计和后续切换到远程 sandbox。

在远程 genesis-sandbox 中，目录生命周期是契约的一部分：

- `/workspace` 是 session 级工作书桌，同一 sandbox session 内的不同 job 可以共享这里的中间文件。
- `/workspace/input` 是每个 job 的输入抽屉，系统挂载输入文件，job 结束后会清空。
- `/workspace/output` 是每个 job 的成果物抽屉，唯一会被回传为 artifacts，job 结束后会清空。
- `/workspace/tmp` 是每个 job 的临时抽屉，job 结束后会清空。

## 2. 核心原则

1. **路径策略先区分执行意图**：任务型执行和远程 sandbox 使用逻辑目录契约；本地编程工具使用 Codex-like 权限工作区模型。
2. **本地路径不能泄漏进 sandbox 代码**：如果用户要求读取本地 `C:\...`、`D:\...`、`/Users/...` 文件，必须先经 PathResolver、权限审批和输入 staging，再让代码读取 `INPUT_DIR` 下的文件。
3. **任务型成果物只从标准输出目录回收**：远程 sandbox、docker sandbox、Office/Skill 和本地任务运行的最终交付文件必须写入 `OUTPUT_DIR`；sandbox-service 只应把 `/workspace/output` 收集为 `output_artifacts`。
4. **客户端不猜容器路径**：客户端下载产物只能依赖 `JobResult.OutputArtifacts` 和 `DownloadArtifact`，不能扫描或拼接容器内部路径。
5. **不做隐式兜底搬运**：sandbox-service 只扫描 `/workspace/output`；客户端也不在用户代码后拼接 Python/Node/Shell 收尾脚本。
6. **路径错误显式失败**：代码把最终成果写到非 `OUTPUT_DIR` 时，不自动收集；返回结构化诊断，让 Agent、Skill 或用户代码修正。
7. **中间状态放 WORK_DIR，不放 OUTPUT_DIR/TMPDIR**：多轮 Agent 任务中的中间文件、缓存索引、草稿脚本、解析结果应放在 `WORK_DIR` 根目录或其子目录；最终交付文件才写入 `OUTPUT_DIR`。
8. **本地编程模式不套远程 sandbox 目录纪律**：当执行目标是编辑项目文件、运行测试、读取源码、生成补丁时，允许直接访问已授权工作区内的真实路径；工作区外访问走审批，不因没有使用 `INPUT_DIR` / `OUTPUT_DIR` 而拒绝。

## 3. 目录映射

| 逻辑目录 | 含义 | 本地 backend 映射 | sandbox backend 映射 |
| --- | --- | --- | --- |
| `WORK_DIR` | 本次执行工作目录 | 项目工作区或 `.genesis/runs/<run_id>/work` | `/workspace` |
| `INPUT_DIR` | 输入文件目录 | `.genesis/runs/<run_id>/input` | `/workspace/input` |
| `OUTPUT_DIR` | 成果物目录 | `.genesis/runs/<run_id>/output` | `/workspace/output` |
| `TMPDIR` | 临时目录 | `.genesis/runs/<run_id>/tmp` | `/workspace/tmp` |

sandbox backend 目录生命周期：

| 目录 | 生命周期 | 是否跨 job 保留 | 用途 |
| --- | --- | --- | --- |
| `/workspace` | sandbox session | 是 | 多步任务共享状态、中间文件、脚本、缓存索引 |
| `/workspace/input` | 单个 job | 否，job 结束后清空 | 本次 job 的输入文件 |
| `/workspace/output` | 单个 job | 否，job 结束后清空 | 本次 job 要回传的最终成果物 |
| `/workspace/tmp` | 单个 job | 否，job 结束后清空 | 本次 job 的临时文件 |

执行器必须注入环境变量：

```text
WORK_DIR=<backend-specific-work-dir>
INPUT_DIR=<backend-specific-input-dir>
OUTPUT_DIR=<backend-specific-output-dir>
TMPDIR=<backend-specific-temp-dir>
GENESIS_WORKSPACE=<backend-specific-work-dir>
```

Agent 生成代码时应使用：

```python
import os

input_path = os.path.join(os.environ["INPUT_DIR"], "data.csv")
output_path = os.path.join(os.environ["OUTPUT_DIR"], "report.csv")
tmp_path = os.path.join(os.environ["TMPDIR"], "scratch.tmp")
state_path = os.path.join(os.environ["WORK_DIR"], "analysis_state.json")
```

不要生成：

```python
open("C:\\Users\\alice\\Desktop\\data.csv")
open("/workspace/output/report.csv")
open("/tmp/report.csv")
```

`/workspace/output` 在 sandbox 内是有效目录，但 Agent 生成代码仍应优先使用 `OUTPUT_DIR`，这样同一段代码可以在本地 backend 和 sandbox backend 之间切换。

多 job 任务注意：

- 需要跨 job 复用的中间文件写入 `WORK_DIR`，例如 `WORK_DIR/analysis_state.json`。
- 每个 job 的输入文件从 `INPUT_DIR` 读取；不要假设上一个 job 的 `INPUT_DIR` 还存在。
- 每个 job 需要回传给客户端的最终文件写入 `OUTPUT_DIR`；不要假设上一个 job 的 `OUTPUT_DIR` 文件还存在。
- 单 job 内部临时缓存写入 `TMPDIR`；不要用 `TMPDIR` 保存跨 job 状态。

### 3.1 本地执行模式分层

本地 backend 不应只有一种路径策略。最佳实践是把本地执行分成两种模式，由执行意图决定是否启用严格工作空间契约。

| 本地模式 | 典型场景 | 路径策略 | 成果物策略 |
| --- | --- | --- | --- |
| `local_coding_workspace` | 编程助手读写项目、运行测试、修改源码、生成补丁 | 权限优先。允许访问授权工作区内真实路径；工作区外路径必须经 PathResolver 和用户审批 | 以 git diff、文件修改、命令输出为主；可选把报告/导出文件写入 `OUTPUT_DIR` |
| `local_task_workspace` | 数据处理、Office 转换、一次性脚本、需要返回下载文件 | 契约优先。输入 staged 到 `INPUT_DIR`，最终产物写 `OUTPUT_DIR`，临时文件写 `TMPDIR` | 从 `OUTPUT_DIR` 收集 artifact manifest |
| `local_platform_sandbox` | Windows constrained process、macOS Seatbelt、Linux bwrap/Landlock | 仍按任务类型选择 `local_coding_workspace` 或 `local_task_workspace`；平台沙箱只提供进程/文件系统限制 | 同左 |

因此，本地沙箱或本地进程隔离不是“远程 sandbox API 的本地版”。它更像受权限约束的宿主机执行器：

- 对编程任务，不强制把源码复制到 `INPUT_DIR`，也不要求生成文件都放 `OUTPUT_DIR`。
- 对用户明确上传文件并要求“处理后给我一个结果文件”的任务，优先启用 `local_task_workspace`，这样本地和远程 sandbox 行为一致。
- 对工作区外文件，例如 `C:\Users\alice\Desktop\data.xlsx`，即使是本地执行也必须先由 PathResolver 判断是否允许访问；需要时弹出审批或走文件选择器授权。
- 本地执行不能因为方便而绕过权限系统直接散读任意绝对路径。

路径校验策略也随模式变化：

| 策略 | 适用场景 | 行为 |
| --- | --- | --- |
| `strict_workspace_contract` | 远程 genesis-sandbox、docker sandbox、Office/Skill 任务运行、本地任务运行 | 发现输入/输出/临时目录不符合契约时拒绝执行，返回结构化诊断 |
| `advisory_workspace_contract` | 本地编程任务中生成独立成果物，例如报告、压缩包、导出表格 | 不因未使用 `OUTPUT_DIR` 拒绝，但给 Agent 提示“最终可下载成果建议写入 OUTPUT_DIR” |
| `permission_only` | 本地编程工具读写项目、运行构建/测试 | 不检查 `INPUT_DIR` / `OUTPUT_DIR` / `TMPDIR` 使用情况；只执行工作区边界、审批、危险命令、Freshness 和审计规则 |

简单判定：

- 用户意图是“改项目/读源码/跑测试/提交 patch”时，默认 `permission_only`。
- 用户意图是“运行代码处理这些输入并返回文件”时，默认 `strict_workspace_contract`。
- 用户意图两者都有时，代码编辑步骤使用 `permission_only`，产物生成步骤使用 `advisory_workspace_contract` 或 `strict_workspace_contract`。

### 3.2 Codex-like 本地工作路径模型

参考 `D:\workspace\go\go-project\codex` 的本地执行语义，本地沙箱的路径模型不应模拟远程 `/workspace/input|output|tmp`，而应围绕当前工作目录和权限 profile 建模：

| 概念 | Genesis 建议 | Codex-like 含义 |
| --- | --- | --- |
| `cwd` | 本次命令的真实当前目录，默认是项目根或用户选择目录 | 默认工作区，也是 `workspace-write` 下的默认可写根 |
| `workspace_roots` | 会话级授权项目根，可有多个 | 用于判断哪些目录属于本轮编程上下文 |
| `writable_roots` | 除 `cwd` 外用户或策略显式授权的可写目录 | 允许写入的额外路径，例如生成目录、临时目录、外部示例工程 |
| `read_only_subpaths` | 可写根下仍保持只读的子路径 | 保护 `.git`、`.genesis`、`.agents`、`.codex`、锁定的配置目录等元数据 |
| `TMPDIR` | 本地临时目录，可作为默认可写根之一 | 给构建、测试、编译缓存使用，不作为最终成果目录 |
| `network_policy` | 默认 restricted，按审批或策略放开 | 网络不是路径问题，但应和同一次命令权限一起审计 |

推荐本地权限 profile：

| Profile | 文件系统语义 | 典型用途 |
| --- | --- | --- |
| `read_only` | 可读授权范围，不能写项目文件 | 搜索、查看、审查、解释代码 |
| `workspace_write` | 可读授权范围，可写 `cwd` / `workspace_roots` / `writable_roots`，但保护元数据子路径 | 默认编程助手模式，修改代码、运行格式化、生成项目文件 |
| `request_write` | 初始按 `workspace_write` 尝试；命中工作区外写入或受保护路径时请求审批 | 需要读取或生成到用户指定外部目录 |
| `danger_full_access` | 全盘读写 | 只用于用户显式选择或企业受控任务，不作为默认值 |

本地命令执行的主路径：

```text
resolve cwd
-> 计算 effective workspace_roots / writable_roots / read_only_subpaths
-> 选择本地沙箱 backend
-> 在 cwd 中执行命令
-> 沙箱拒绝或策略命中时，返回 denial reason
-> 按 approval policy 决定是否请求用户授权、追加临时 writable_root 或拒绝
-> 审计最终执行权限
```

关键约束：

- `cwd` 是命令语义的一部分，不能随意改成 `.genesis/runs/<run_id>/work`，否则相对路径、构建工具、测试快照和项目脚本都会失真。
- `workspace_write` 默认允许写当前项目，但不等于允许改所有元数据。`.git`、`.genesis`、`.agents`、`.codex` 等目录默认只读；确需修改时必须有显式工具或审批。
- `writable_roots` 必须是绝对路径或经 PathResolver 解析后的路径，并带来源：配置、会话授权、用户本次审批、工具临时授权。
- 工作区外读写不靠字符串路径白名单放行，应由 PathResolver、权限引擎和审批流产生新的授权根或单次授权。
- 本地模式下可以注入 `WORK_DIR=cwd`、`TMPDIR=<local temp>`，但 `INPUT_DIR` / `OUTPUT_DIR` 只是任务型脚本的辅助目录，不是编程命令的强制目录。

本地编程模式的推荐默认值：

```text
cwd = 用户打开的项目根或当前 shell 目录
workspace_roots = [cwd] + 用户显式添加的项目根
writable_roots = workspace_roots + 本地临时目录
read_only_subpaths = .git, .genesis, .agents, .codex, 以及策略声明的受保护路径
network = restricted
approval = on_request
path_policy = permission_only
```

如果本地沙箱返回“写入被拒绝”，不要把命令自动改写到 `OUTPUT_DIR`，而是按意图处理：

- 目标是修改项目文件：请求用户批准扩大 `writable_roots`，或提示该路径不在授权工作区。
- 目标是生成独立成果物：建议 Agent 改为写 `OUTPUT_DIR`，或请求用户授权写入指定目录。
- 目标是构建/测试缓存：优先把缓存目录配置到 `TMPDIR` 或项目内受控缓存目录。

## 4. 本地文件输入进入执行环境的规则

当用户说“读取本地 C 盘某个文件内容并执行代码”时，要先判断执行 backend 和任务意图。

远程 sandbox / docker sandbox / 本地任务运行模式的流程必须是：

```text
用户本地路径
-> PathResolver 解析
-> 权限/审批/审计
-> 输入 staging
-> 代码读取 INPUT_DIR 下的文件
```

### 4.1 本地 backend

本地 backend 要先判断执行意图。

如果是 `local_task_workspace`，本地执行不需要上传 artifact，但仍要遵守输入目录契约：

```text
C:\Users\alice\Desktop\data.xlsx
-> .genesis/runs/<run_id>/input/data.xlsx
-> 代码读取 INPUT_DIR/data.xlsx
```

这样做的原因：

- 避免代码直接散读宿主机任意路径。
- 便于审计本次执行到底用了哪些输入。
- 便于后续重放、缓存和产物 manifest。

如果是 `local_coding_workspace`，例如用户说“读取项目里的配置并修改代码”“运行测试”“查看 C 盘某个已授权目录下的文件内容”，不应强制复制到 `INPUT_DIR` 后再读。正确流程是：

```text
目标路径
-> PathResolver 判断是否在授权工作区内
-> 工作区外路径触发审批或文件选择器授权
-> 审批通过后追加为单次可读路径、会话 workspace_root 或 writable_root
-> 本地进程按授权路径读取/写入
-> Freshness/审计记录真实路径访问
```

此时 `INPUT_DIR` / `OUTPUT_DIR` / `TMPDIR` 仍可注入，供脚本生成临时报告或导出物使用，但不是硬性校验条件。

### 4.2 Sandbox backend

sandbox 内代码不能读取宿主机绝对路径。正确流程是：

```text
C:\Users\alice\Desktop\data.xlsx
-> PathResolver/权限审批
-> 上传为 input artifact
-> ExecJob 带 input_artifact_ids
-> sandbox-service 挂载到 /workspace/input/data.xlsx
-> 代码读取 INPUT_DIR/data.xlsx
```

现有 genesis-sandbox Go SDK 已支持：

```text
UploadJobFile(ctx, jobID, name, content)
ExecJob(ctx, ExecJobRequest{InputArtifactIDs: [...]})
DownloadArtifact(ctx, artifactID)
```

Genesis Agent 侧不应把 `UploadJobFile` 细节暴露给业务层，应封装为：

```text
StageInputs(local_files | resources) -> []InputArtifactRef
Run(request{InputArtifactIDs: ...})
```

如果未来 sandbox-service 提供更直接的输入上传 API，例如 `POST /v1/artifacts` 或 `POST /v1/workspaces/{id}/input`，只替换 staging 实现，不改变上层契约。

## 5. 成果物下载与本地落盘

成果物主路径：

```text
代码/脚本写 OUTPUT_DIR
-> sandbox-service 收集 /workspace/output
-> JobResult.OutputArtifacts
-> DownloadArtifact
-> 本地受控 artifact root
```

本地默认落盘目录：

```text
.genesis/artifacts/<workspace_id-or-job_id>/<artifact_name>
```

产物返回结构必须尽量包含：

```json
{
  "id": "artifact id",
  "workspace_id": "workspace id",
  "job_id": "job id",
  "name": "report.pdf",
  "size": 12345,
  "sha256": "...",
  "mime": "application/pdf",
  "remote_url": "...",
  "local_path": ".genesis/artifacts/ws-1/report.pdf"
}
```

如果用户要求把产物放回项目目录，不能由 sandbox adapter 直接覆盖。必须再走文件工具的 PathResolver、权限审批、Freshness 检查和审计。

## 6. 执行前路径校验

执行前路径校验不是安全边界，而是质量门。真正的安全边界仍由权限系统、PathResolver 和 sandbox 文件系统策略负责。由于本项目处于开发阶段，不需要兼容旧代码，远程 sandbox 和任务运行模式下的路径违规应尽早失败并返回可修复诊断。

校验器输入必须包含执行模式：

```text
backend = local_host | local_platform_sandbox | docker_sandbox | remote_sandbox
workspace_mode = local_coding_workspace | local_task_workspace | sandbox_session_workspace
path_policy = strict_workspace_contract | advisory_workspace_contract | permission_only
```

不要用同一套路径硬规则处理所有代码执行。远程 sandbox 需要严格路径契约，是因为本地文件需要 staging、成果物需要 artifact 下载；本地编程工具需要的是“授权范围内真实文件可读写”，否则会破坏编程助手的基本体验。

### 6.1 Agent 生成代码：按执行模式选择

Agent 自己生成代码时，若运行在远程 sandbox、docker sandbox、Office/Skill 任务、本地任务运行模式，应启用严格校验。发现以下情况应拒绝执行，并返回结构化错误给 Agent 让其重写：

- 读取宿主机绝对路径：`C:\...`、`D:\...`、`/Users/...`、`/home/...`。
- 最终成果写到非 `OUTPUT_DIR`。
- 读取输入文件但没有使用 `INPUT_DIR`。
- 写临时文件但没有使用 `TMPDIR`，且该文件不是最终成果。
- 需要跨 job 复用的中间文件写入 `INPUT_DIR`、`OUTPUT_DIR` 或 `TMPDIR`，而不是 `WORK_DIR`。

若运行在本地编程工具模式，Agent 生成代码不因使用项目真实路径而失败。此时校验重点变为：

- 工作区内路径：允许，但必须经过 Freshness 检查，避免覆盖用户并发修改。
- 工作区外路径：必须由 PathResolver 判定并触发审批；未经授权不得读取或写入。
- 生成独立交付文件：建议写入 `OUTPUT_DIR` 或用户明确指定且已授权的位置。
- 临时文件：建议写入 `TMPDIR` 或系统允许的临时目录；不能把临时缓存散落到项目根目录，除非任务本身就是生成项目文件。

错误示例：

```json
{
  "code": "EXECUTION_PATH_CONTRACT_VIOLATION",
  "message": "检测到代码试图在 sandbox 中读取宿主机绝对路径。",
  "violations": [
    {
      "path": "C:\\Users\\alice\\Desktop\\data.xlsx",
      "reason": "sandbox 内不能访问宿主机绝对路径",
      "fix": "先把该文件作为输入上传到 INPUT_DIR，再读取 INPUT_DIR/data.xlsx"
    }
  ],
  "contract": {
    "input_dir": "INPUT_DIR",
    "output_dir": "OUTPUT_DIR",
    "tmp_dir": "TMPDIR"
  }
}
```

### 6.2 内置 Skills：managed-strict

内置 Skills 是受控资产，应逐步改成只依赖逻辑目录：

- 读取输入文件：`INPUT_DIR`。
- 写最终文件：`OUTPUT_DIR`。
- 写临时缓存：`TMPDIR`。
- 写跨步骤中间状态：`WORK_DIR`。

CI 或 skill eval 应检查常见硬编码路径，尤其是 Office/PDF 脚本中的输入输出路径。

### 6.3 用户代码和第三方脚本：远程 explicit-fail，本地 permission-first

本项目不兼容旧代码。用户自己提供的代码、第三方脚本或外部 Skill 在远程 sandbox、docker sandbox 和任务运行模式下也必须遵守执行工作空间契约。推荐策略：

1. 注入 `WORK_DIR`、`INPUT_DIR`、`OUTPUT_DIR`、`TMPDIR`。
2. 设置工作目录为 `WORK_DIR`。
3. 如果检测到宿主机绝对路径、写最终成果到非 `OUTPUT_DIR`、读取输入未使用 `INPUT_DIR`、跨 job 中间状态未使用 `WORK_DIR`，直接返回结构化错误。
4. 如果执行成功但 `OUTPUT_DIR` 没有产物，返回诊断和修复建议，不扫描其他目录兜底。
5. 用户需要运行不符合规范的脚本时，应先改脚本或由 Agent 生成 wrapper，把输入输出路径显式参数化。

但在本地编程工具模式下，用户脚本可能天然就是项目脚本，例如 `go test ./...`、`npm run build`、`python tools/generate.py --out internal/generated`。这类脚本不应被 `OUTPUT_DIR` 规则拦截。它们应按本地权限执行：

- 默认工作目录是项目工作区或用户选定目录。
- 工作区内读写由权限策略、Freshness、资源锁和审计控制。
- 工作区外读写必须审批。
- 如果脚本目的是生成给用户下载/查看的独立文件，Agent 应优先把输出参数指向 `OUTPUT_DIR`；否则允许它按项目约定写到仓库内生成目录。

诊断示例：

```json
{
  "code": "NO_OUTPUT_ARTIFACTS",
  "message": "代码执行成功，但没有在 OUTPUT_DIR 发现可回传文件。",
  "hints": [
    "请把最终文件写入环境变量 OUTPUT_DIR 指向的目录",
    "输入文件位于环境变量 INPUT_DIR 指向的目录",
    "临时文件请写入环境变量 TMPDIR 指向的目录"
  ]
}
```

### 6.4 Path Preflight Analyzer 架构

更深层的语言级路径分析有价值，但不应硬塞进通用 runner，更不能把静态分析误当成安全边界。最佳实践是把它做成可插拔的 **Path Preflight Analyzer** 管线：

```text
Runner
-> PathValidator
-> Analyzer Registry
   -> shell_text analyzer
   -> python_source analyzer
   -> javascript_source analyzer
   -> go_source analyzer
   -> java_source analyzer
   -> powershell_source analyzer
   -> shell_script_source analyzer
   -> skill_manifest analyzer
-> structured violations
```

职责边界：

| 组件 | 职责 | 不应做的事 |
| --- | --- | --- |
| `Runner` | 注入逻辑目录、调用 `PathValidator`、根据 sandbox 策略选择 backend | 解析具体编程语言 AST |
| `PathValidator` | 根据 `PathPolicy` 决定是否启用强校验，聚合分析器结果 | 直接执行命令或修改用户代码 |
| `Analyzer Registry` | 注册和编排多种语言/脚本分析器，去重诊断 | 参与权限授权决策 |
| 语言分析器 | 在能可靠拿到源码时做语言特定分析，例如源码字面量、脚本参数、Skill manifest 中的路径声明 | 声称覆盖所有动态表达式或替代 sandbox |

核心不变量：

- 静态分析只用于早失败和给 Agent 可修复反馈，不作为“允许访问某路径”的依据。
- 未发现违规不等于安全；最终仍由 PathResolver、权限系统、本地沙箱和远程 sandbox 文件系统策略兜底。
- 发现明确违规时，在 `strict_workspace_contract` 下应拒绝执行，并返回 `EXECUTION_PATH_CONTRACT_VIOLATION`。
- 本地 `permission_only` 编程模式不启用严格目录契约；它由 workspace roots、writable roots、protected metadata、Freshness 和审批流约束。
- 新语言分析器只能挂到 registry 或产品注入的 validator，不能把语言细节写进 runner。

默认内置分析器：

| Analyzer | 输入来源 | 能发现的问题 | 限制 |
| --- | --- | --- | --- |
| `shell_text` | 命令字符串 | 明显宿主机绝对路径、UNC 路径、非 `/workspace` 的 Unix 绝对路径、`/tmp` 等 | 不理解语言语义 |
| `python_source` | `python -c` 源码、可读取的 `.py` 脚本 | Python 字符串字面量中的宿主机路径、UNC 路径、非 `/workspace` 的 Unix 绝对路径、`/tmp` 等 | 不追踪任意动态拼接、外部配置、运行时输入 |
| `javascript_source` | `node -e`、`tsx/ts-node/bun/deno`、可读取的 `.js/.ts/.tsx/.jsx/.mjs/.cjs/.mts/.cts` | JS/TS 字符串和模板字面量中的违规路径 | 不执行类型推导，不追踪变量拼接 |
| `go_source` | `go run` 后可读取的 `.go` 文件，或 `go run .` / `go run ./dir` 的目录顶层 `.go` 文件 | Go 普通字符串和 raw string 中的违规路径 | 不递归扫描整个 module，不解析 build tag 或生成代码 |
| `java_source` | `java/javac` 后可读取的 `.java` 文件 | Java 字符串和 text block 中的违规路径 | 不解析 classpath、配置文件或运行时参数 |
| `powershell_source` | `pwsh/powershell -Command/-File`、可读取的 `.ps1/.psm1` | PowerShell 脚本源码中的未加引号或加引号违规路径 | 只做行级注释剥离，不执行 PowerShell AST 语义求值 |
| `shell_script_source` | `sh/bash/zsh -c`、可读取的 `.sh/.bash/.zsh` | Shell 脚本源码中的未加引号或加引号违规路径 | 只做行级注释剥离，不展开变量、glob 或命令替换 |
| `skill_manifest` | 可读取的 `SKILL.md/skill.md/*.skill.yaml/*.skill.yml` | Skill manifest、Markdown、脚本说明中的违规路径 | 不替代 Skill parser 的结构化校验 |

后续扩展规则：

1. 高价值语言优先：Python、JavaScript/TypeScript、Go、Java、PowerShell、Shell、Office Skill manifest。
2. 每个分析器必须声明输入来源和限制，避免“全语言完整 AST 无漏洞”这种不可验证承诺。
3. 语言分析器应输出统一的 `Violation`，至少包含 `fragment`、`location`、`reason`、`fix`。
4. 对用户脚本和第三方脚本，远程 strict 模式下宁可显式失败，也不自动重写路径或隐式搬运成果物。
5. CI 可对内置 Skills 启用 analyzer 作为质量门，防止 Office/PDF 脚本重新引入硬编码 `/tmp`、Windows 盘符或宿主机 HOME。

### 6.5 Agent 路径提示：控制面统一 + 生效 backend 地图

> 审查结论（review-fix-rereview）：「按环境区分提示词」**可行**，但**禁止**做成「每种 backend 教一套 `inputs` 绝对路径」。正确做法是控制面词汇全环境同一套，仅把**已生效** backend 的物理映射作为执行面说明；降级必须刷新说明，且与 `stageInputs` 硬校验配套。

#### 6.5.1 第一性原理

| 项 | 内容 |
| --- | --- |
| 根问题 | 模型把**执行面**路径（如 `/workspace/...`）写进**控制面**参数（`run_skill_command.inputs`、部分 `write_file.path`），导致宿主机 stage 失败 |
| 最小目标 | 任意 backend（含 optional 降级后）下，tool JSON 路径可解析；脚本内路径可移植 |
| 不变量 | 逻辑目录契约；业务路径不回传宿主机绝对路径；`SandboxRequire` 不静默降级；optional 降级必须 warning/trace/audit |
| 失败条件 | 按环境教不同 `inputs` 写法；仅在 Run 开始的 System 里写死远程地图；降级后不刷新；无硬校验只靠 prompt |

#### 6.5.2 两平面（全环境同一规则）

| 平面 | 载体 | 合法形态 | 禁止 |
| --- | --- | --- | --- |
| **控制面** | `inputs`、`write_file.path`、其它需 PathResolver/stage 的 tool 参数 | `$WORK_DIR/...`、`$INPUT_DIR/...`、`$OUTPUT_DIR/...`、工作区相对路径 | `/workspace/...`、盘符/`/Users`/`/home` 绝对路径、把 path_map 右侧抄进 tool JSON |
| **执行面** | `run_skill_command.command`、命令内脚本、`os.environ["WORK_DIR"]` 等 | cwd 相对名、包内 `scripts/...`、进程环境变量名（非 `$WORK_DIR` 字面量） | 硬编码宿主机路径；在远程脚本里写 `.genesis/runs/...`；**把 `$WORK_DIR`/`$INPUT_DIR`/`$OUTPUT_DIR`/`$TMPDIR`/`$SKILL_DIR` 写进 command**（本地宿主与远程 sandbox API 均不展开） |

`inputs` 语义（冻结）：仅表示「把**控制面已存在**的文件 stage 进本次 Skill 工作目录，供 command 用**相对文件名**访问」。文件已在同一执行 cwd / 仅跑包内脚本时，**省略 inputs**。

正确组合（全 backend 相同）：`write_file("$WORK_DIR/foo.py")` → `run_skill_command(command="python foo.py", inputs=["$WORK_DIR/foo.py"])`。错误示例：`command="python $WORK_DIR/foo.py"`（会触发 `COMMAND_LOGICAL_PREFIX_FORBIDDEN`）。

`python -c` / `node -e`：**默认禁止多行/长串内联**（`COMMAND_INLINE_RISKY`）；仅极短单行探测可放行。检查、生成、循环读写一律走 `$WORK_DIR` 脚本文件。

#### 6.5.3 按环境区分什么、不区分什么

Backend 枚举与 §6 / Skill 三模式对齐，不另造「三环境」平行分类：

`local_host` | `local_platform_sandbox` | `docker_sandbox` | `remote_sandbox`

| 内容 | 是否随 backend 变 | 说明 |
| --- | --- | --- |
| 控制面写法（`$WORK_DIR` / 相对路径） | **否** | 降级零成本；本地平台沙箱与无沙箱**同一展示契约**（见 Skill 三模式 §4.3.1） |
| `execution_backend` + `degraded` + 逻辑→物理 **path_map** | **是** | 只说明「当前进程里 env 对应哪里」；标注 **RHS 禁止写入 tool JSON** |
| 编程模式 `permission_only` | 不适用本小节 skill/task 地图 | 不把远程 `/workspace` 地图注入本地编码工具上下文 |

**不推荐**：远程提示「`inputs` 用 `/workspace/...`」、本地提示「`inputs` 用 `D:\...`」。那会在 optional 降级时直接教错。

#### 6.5.4 注入时机（对齐现有提示词架构）

现状：`BuildSystem` **仅在 Run 开始调用一次**，主循环不重建 System（见 `提示词分层设计方案.md`）。因此**不能**把「最终生效 backend」只写进 Run 级 System 就以为闭环。

| 时机 | 注入什么 | 通道 |
| --- | --- | --- |
| Skill 加载 / 稳定 bridge | **静态控制面规则**（逻辑前缀、`inputs` 禁 `/workspace`、中间脚本 `$WORK_DIR` + 相对 command） | `<skill_runtime_bridge>` |
| 每次 `run_skill_command` 返回 | 至少：生效 `execution_backend`、`degraded`、`runtime_profile` | tool result metadata（及既有 warning） |
| 首次成功选定 backend，或 `degraded`/backend **发生变化**时 | 完整逻辑→物理 **path_map** + 「RHS 禁止写入 tool JSON」 | 同上；降级时加「旧地图作废」一句 |
| `SandboxRequire` 失败 | 不注入「假装仍在远程」的地图 | fail closed |

说明：不必每个成功响应都重复完整 path_map（省 token）；但 **backend/degraded 每次都要有**，以便模型在降级后立刻感知。现有实现若已用 `backend=remote_session` 等别名，对外可保留，文档枚举以 §6 为准并允许等价映射。

`path_map` 示例（执行面说明，非 inputs 模板）：

```text
execution_backend: remote_sandbox
degraded: false
path_map:
  WORK_DIR   -> /workspace
  INPUT_DIR  -> /workspace/input
  OUTPUT_DIR -> /workspace/output
  TMPDIR     -> /workspace/tmp
note: 脚本内用环境变量；inputs/write_file 仍只用 $WORK_DIR/... ，禁止把右侧路径写入 tool JSON
```

降级到本地后同一结构，右侧改为 `.genesis/runs/<run_id>/...` 的**工作区相对**展示（禁止盘符绝对路径进模型契约）。

#### 6.5.5 与硬校验配套（提示词非唯一防线）

| 层 | 行为 |
| --- | --- |
| `stageInputs` / `resolveStageSource` | 控制面参数若呈执行面绝对根（如 `/workspace` 前缀）→ 结构化错误（建议码 `INPUT_PATH_NAMESPACE_MISMATCH`），附修复提示；勿再堆一长串宿主 tried 路径冒充「友好」 |
| `run_skill_command.command` | 若含 `$WORK_DIR` 等逻辑前缀字面量 → `COMMAND_LOGICAL_PREFIX_FORBIDDEN`；若 `python -c`/`node -e` 多行、过长或嵌套引号 → `COMMAND_INLINE_RISKY`（本地与远程同一规则） |
| pathcontract 源码扫描 | 继续拦脚本内宿主机绝对路径等（既有） |
| bridge / tool Description | 与上表措辞一致，避免示例教坏 |

#### 6.5.6 验收要点

1. 远程成功跑 Skill：tool result 含 `execution_backend=remote_sandbox`（或实现等价名）与 path_map；`inputs` 仍为 `$WORK_DIR/...` 或省略。
2. optional 远程不可用：降级 warning + 新 path_map；随后 `inputs` 用逻辑前缀仍成功。
3. required 远程不可用：失败，无「本地地图」误导。
4. `inputs=["/workspace/..."]`：早失败，错误可修复，不误伤合法 `$WORK_DIR`。
5. 本地平台沙箱：path_map / 回传路径与无沙箱一致，不出现第二套 `/workspace` 业务路径。

## 7. Sandbox-Service Artifact 收集策略

sandbox-service 的 artifact 收集策略应保持严格、简单和可审计：**只扫描 `/workspace/output`**。

推荐流程：

```text
执行用户命令
-> 用户命令显式写 /workspace/output
-> sandbox-service 扫描 /workspace/output
-> 生成 output_artifacts
-> 客户端按 artifact_id 下载
```

这意味着：

- 写到 `/workspace/output` 的文件才是成果物。
- 写到 `/workspace/tmp`、`/tmp`、`/workspace` 顶层、用户 HOME 或其他路径的文件默认不回传。
- sandbox-service 不负责猜测哪些非标准路径文件是用户想要的成果。
- 如果没有 `output_artifacts`，客户端返回 `NO_OUTPUT_ARTIFACTS` 诊断，让 Agent 或用户修正代码。
- `/workspace/output` 是 job 级目录，产物被收集后会清空；需要在后续 job 继续使用的文件，应同时或另行写入 `WORK_DIR`。

禁止行为：

- `/workspace/input`。
- 自动扫描 `/tmp`。
- 自动扫描 `/workspace/tmp`。
- 自动把 `/workspace` 顶层文件复制到 `/workspace/output`。
- 在客户端给用户代码拼接收尾脚本。
- 为了“方便”回传非 `OUTPUT_DIR` 文件。

无产物诊断示例：

```json
{
  "code": "NO_OUTPUT_ARTIFACTS",
  "message": "代码执行完成，但 /workspace/output 没有可回传成果物。",
  "hints": [
    "请把最终文件写入环境变量 OUTPUT_DIR 指向的目录",
    "Python 示例：open(os.path.join(os.environ[\"OUTPUT_DIR\"], \"result.csv\"), \"w\")",
    "不要把最终成果写入 /tmp、/workspace/tmp 或 /workspace 顶层"
  ]
}
```

## 8. Sandbox Session 与多任务复用

`sdks/go/sandbox` 的成熟模式是：

```text
sandbox.New
-> Lease
-> 后台 Renew
-> ExecJob / GetJob poll
-> Close
   -> Release
   -> Release 失败则 Destroy
```

当前单次 `CommandClient.RunCommand` 可以按每次命令 lease/release 实现主路径。但如果要支持多任务共享状态，例如依赖安装、连续脚本执行、同一 workspace 下多步处理，应新增 `SandboxSession` 端口，而不是把长会话状态塞进单次命令接口。

对 Agent 场景，`SandboxSession` 不应只理解为“复杂任务才用”。大模型通常会一轮轮探索、写代码、修正、生成中间文件和验证结果，只要任务具有开放式迭代或文件处理倾向，就应默认使用 session。session 内只有 `WORK_DIR` 根目录能跨 job 保留，`INPUT_DIR`、`OUTPUT_DIR`、`TMPDIR` 都是 job 级目录。

默认执行模式：

| 场景 | 默认执行模式 |
| --- | --- |
| 单次 shell 命令 | `RunCommand` |
| Agent 写代码处理文件 | `SandboxSession` |
| 用户上传文件后执行代码 | `SandboxSession` |
| Office Skill | `SandboxSession` |
| 多步 Plan-Execute | `SandboxSession` |
| 一次性 inspect/extract | `RunCommand` 可接受 |
| 需要安装/构建依赖 | `SandboxSession` |
| 交互式调试 | `SandboxSession` |

判定原则：

- 明确是原子命令、无输入文件、无跨步状态、无后续调试预期时，使用 `RunCommand`。
- 只要涉及 Agent 生成代码处理文件、用户上传文件、多步 Skill、Office/PDF 生成校验、依赖准备或交互式调试，默认使用 `SandboxSession`。
- 如果一开始按 `RunCommand` 执行，但同一 Agent task 中出现第二次相关执行需求，应升级为 `SandboxSession`，后续步骤共享 `WORK_DIR`。

建议端口：

```text
OpenSession(ctx, options) -> SandboxSession
SandboxSession.StageInputs(ctx, files) -> []InputArtifactRef
SandboxSession.Run(ctx, request) -> Result
SandboxSession.Close(ctx)
```

这样可以对齐 SDK 的 `RunSequential` 和 `RunBatchThrottled` 思路，同时保持 Genesis Agent 内部契约独立于 genesis-sandbox SDK 类型。

## 9. 与 Profile 选择的关系

Profile 选择解决“用什么 runtime 执行”的问题，执行工作空间契约解决“输入输出文件怎么进入和离开执行环境”的问题。两者相关但不能耦合。

示例：

| 场景 | Profile | 文件路径契约 |
| --- | --- | --- |
| 普通 Python 代码 | `code-polyglot-basic` | 输入从 `INPUT_DIR`，产物写 `OUTPUT_DIR` |
| Office Skill | `office-basic` | 文档输入 staged 到 `INPUT_DIR`，跨步骤中间文件写 `WORK_DIR`，生成文档写 `OUTPUT_DIR` |
| OCR | `office-ocr` | 图片/PDF staged 到 `INPUT_DIR`，OCR 结果写 `OUTPUT_DIR` |
| 本地执行 | 无外部 sandbox profile | 仍使用本地 `.genesis/runs/<run_id>/input|output|tmp` |

## 10. 推荐落地顺序

1. 在 execution model 中引入 `ExecutionWorkspace` / `InputArtifactRef` / `ArtifactCollectionPolicy` 等内部模型。
2. 为本地 backend 实现 `.genesis/runs/<run_id>/input|output|tmp` 准备和本地产物收集。
3. 为 sandbox backend 封装 `UploadJobFile` / `input_artifact_ids` / `DownloadArtifact`，不要暴露底层 SDK 细节给 Tool/Skill。
4. HTTP adapter 提交 job 时注入 `WORK_DIR`、`INPUT_DIR`、`OUTPUT_DIR`、`TMPDIR`。
5. 增加 Agent 生成代码的 path preflight validator，strict 模式下返回结构化错误。
6. 改造内置 Office Skills 脚本，统一使用逻辑目录。
7. 客户端在没有 `output_artifacts` 时返回 `NO_OUTPUT_ARTIFACTS` 诊断，不从其他目录兜底收集。
8. 新增 `SandboxSession` 端口，作为 Agent 代码执行、Office/Skill 多步处理和文件处理任务的默认执行上下文；单次 `RunCommand` 只保留给明确原子命令。
9. 按 §6.5：稳定 bridge 只保留控制面规则；`run_skill_command` result 回传生效 `execution_backend`/`degraded`/`path_map`；optional 降级刷新地图；`stageInputs` 对执行面绝对根做 `INPUT_PATH_NAMESPACE_MISMATCH` 硬校验。（**已落地**）
10. 与 `提示词分层设计方案` 对齐：路径地图走 tool result / notify，不依赖 Run 级 System 重建（待 TurnEnhancer 落地前的务实通道）。

## 11. 非目标

- 不在客户端拼接 Python/Node/Shell 收尾代码。
- 不在 sandbox-service 中扫描 `/tmp`、`/workspace/tmp` 或 `/workspace` 顶层作为默认产物来源。
- 不让 sandbox adapter 直接覆盖项目目录文件。
- 不伪造未暴露的 workspace 文件 CRUD API。
- 不把 genesis-sandbox SDK 类型直接泄漏到 Genesis Agent 通用能力契约中。
