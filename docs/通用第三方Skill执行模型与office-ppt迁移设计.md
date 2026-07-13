# 通用第三方 Skill 执行模型与 office-ppt 迁移设计

> 日期：2026-07-12
> 状态：设计已完成；genesis-agent 侧 Skill 执行模型与 office-ppt 迁移已按本文主方案落地，session scoped WorkspaceFS 远程 API 仍需 genesis-sandbox 侧配合完成最终闭环
> 试点：`internal/capabilities/skill/adapter/embedded/skills/office-ppt`，源头 `D:\workspace\go\go-project\anthropics-skills\skills\pptx`
> 关联文档：
> - `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`（Skill/Tool 协议边界，权威）
> - `docs/Skill调用原理对比-Kode-Codex-Genesis.md`（加载协议对比）
> - `docs/执行工作空间与Sandbox文件路径契约.md`（路径契约、SandboxSession）
> - `docs/沙箱API对接与Profile选择规则.md`（Profile 选择）
> - `docs/Skill三模式执行与依赖闭环设计.md`（依赖闭环）
> - `D:\workspace\go\genesis-sandbox\docs\session-workspacefs最终形态改造设计.md`（genesis-sandbox 侧 WorkspaceFS 最终形态改造）

---

## 0. TL;DR

当前 office-ppt 的问题不是「功能不对」，而是**把宿主执行机制泄漏进了技能内容本身**：`run_skill_command`、`office-basic 镜像`、`INPUT_DIR/OUTPUT_DIR`、`path_contract.py`、「Genesis 硬约束」全部写进了 `SKILL.md` 和脚本。这使得「拿一个成熟第三方技能原样放进来就能跑」的属性被破坏——每装一个技能都要人肉改写。

codex 与 Kode-CLI 的共同答案是：**技能内容 100% verbatim，宿主只在运行期注入极少量环境上下文（工作目录 / 技能根），用通用命令执行原语按 `SKILL.md` 原文跑脚本，把沙箱、路径、产物、审计等保障全部下沉到执行层，对技能不可见。**

本方案据此提出 Genesis 的目标模型：

1. **Skill 内容 verbatim**：office-ppt 直接照搬 Anthropic pptx（`SKILL.md` / `editing.md` / `pptxgenjs.md` / `scripts/` / `scripts/office/*`），删除所有 Genesis 自造脚本与耦合措辞。
2. **Skill Task Session**：技能任务开一个**持久工作目录**（本地 `.genesis/runs/<run>/work`；沙箱 `/workspace`），技能包 materialize 到该目录，`cwd` 设为该目录，多步命令共享状态（`unpack → 编辑 → pack` 不再断链）。
3. **verbatim 命令执行原语** `run_skill_command(skill, command)`：取代 `run_skill_command(skill, script=<resource_id>, inputs=[...])`。`command` 就是 `SKILL.md` 原文命令（`python scripts/thumbnail.py deck.pptx`）。
4. **宿主适配靠运行时 bridge，不靠改技能**：加载时注入 `<skill_runtime_bridge>` 把「文档里的 shell 命令」映射到 `run_skill_command`；技能文件一字不改。
5. **保障下沉到执行层**：沙箱 Profile（声明式解析，不再按技能名硬编码，不在技能正文出现镜像名）、产物门禁、假二进制拦截、审批、审计、路径 preflight 全部在 runtime 完成。
6. **统一路径/产物模型（三模式）**：保留沙箱 `/workspace/input|output|tmp` 三目录規範与 `/workspace/output` 自动回收给**无状态 job**；为 verbatim 技能与 Agent 迭代新增**会话式工作区 + WorkspaceFS 显式按路径读写**。终态**不是**「拷贝到 output」——而是以 genesis-sandbox 现有 session/execd 文件搬运底座和 genesis-agent `FileSystemClient` 端口为基础，补齐 session scoped WorkspaceFS 的端口、HTTP 协议、路径安全与审计。详见 §5.4。
7. **三端统一内核 + 端口注入**：CLI/Desktop/Enterprise 共用唯一 `BuildSkillStack` 与技能执行内核；执行/Session/WorkspaceFS/Profile/Sources/Approval 等差异全部走 `ExecStack` + Sources 注入，内核不含产品分支。需先消除 CLI 自研装配漂移。详见 §12。

---

## 1. 背景与第一性原理

### 1.1 现象

用户反馈（原话精炼）：

> office-ppt 迁移过来能生成 PPT，但和系统耦合了。`office-basic 镜像通常含`、`run_skill_command` 这些反复出现在 md 里。`run_skill_command` 是本项目才有的东西。担心以后装第三方技能，如果不能自动按 `SKILL.md` 规范执行、非要手工改写成 `run_skill_command`，就不优雅。要做到：第三方技能脚本**自动按 md 规范执行**，不依赖本项目的任何专有东西；input/output 路径、怎么 run 由项目内部处理好。

对比证据（详见 §3）：

- Anthropic `pptx/SKILL.md` 正文约 50 行，命令是 `python -m markitdown presentation.pptx`、`python scripts/thumbnail.py presentation.pptx`、`python scripts/office/unpack.py presentation.pptx unpacked/`——**零宿主耦合**。
- Genesis `office-ppt/SKILL.md` 把每条命令改写成 `run_skill_command(skill="office-ppt", script="office-ppt/scripts/...", args=[...], inputs=[...])`，新增「Genesis 硬约束」整节，出现 `office-basic`/`office-ocr` 镜像名、`INPUT_DIR/OUTPUT_DIR/WORK_DIR`、`path_contract.py`。脚本层也新增 `path_contract.py`、`run_pptxgen_script.js` 等 Genesis 专有物，并把 Anthropic 脚本改成读 `INPUT_DIR`、写 `OUTPUT_DIR`。

### 1.2 第一性原理（从零推导）

**Skill 的本质**：可移植的「任务知识 + 捆绑资源」包（`SKILL.md` frontmatter + 正文 + `scripts/`/`references/`/`assets/`）。它的**核心价值就是可移植**——从 Anthropic 生态、社区 marketplace 拿一个成熟技能，原样放进来即可用。一旦「必须改写才能跑」，技能生态就退化成了项目定制代码，价值坍塌。

**一个第三方技能要能原样运行，充要条件是什么？**

`SKILL.md` 会写 `python scripts/foo.py input.pptx`。要让它 verbatim 跑通，运行环境只需满足：

1. 有一个能执行命令行的原语；
2. `scripts/foo.py` 相对路径可解析（相对技能根）；
3. 运行时（python/node/libreoffice/poppler/markitdown/pptxgenjs…）可用；
4. 输入/输出文件用普通相对/绝对路径可达，且多步之间状态可持续。

**Genesis 想要而 codex/Kode 不强制的额外保障**（这些是正当的，但不该泄漏进技能）：

- 沙箱隔离（optional/required，不能静默降级）；
- 产物校验（真 OOXML/PDF，不是假文件）；
- 拦截 `write_file` 直接写假二进制；
- 每命令审批、多租户、审计、用量；
- Profile/镜像选择。

### 1.3 根因

Genesis 现有执行模型是**「无状态、按 resource-id、每次调用重新 staging」**：`run_skill_command(skill, script=<resource_id>, inputs=[...])` → 每次开/关 session、把 `inputs` 拷进一个全新的 `INPUT_DIR`、脚本必须把成果写进 `OUTPUT_DIR`。

这个模型有两处硬伤，直接逼出了耦合：

1. **按 resource-id 调用** ⇒ `SKILL.md` 里每条命令都得改写成「工具名 + resource-id + args + inputs」，正文被污染。
2. **无状态 + INPUT_DIR/OUTPUT_DIR 契约** ⇒ 脚本必须改成读 `INPUT_DIR`、写 `OUTPUT_DIR`（于是有了 `path_contract.py` 和一堆脚本改写）；而 `unpack → 编辑 → pack` 这种**多步共享状态**的成熟流程，在「每次调用重新 staging」下会断链，只能靠反复 `inputs` 重传硬凑。

**结论**：耦合的根因是执行模型（无状态 + resource-id + 强制 IO 目录），不是技能本身。修执行模型，技能就能 verbatim。

---

## 2. 反思两轮

### 2.1 第一轮反思：耦合分三层，每层都要「下沉」，而不是「改技能」

把 office-ppt 的耦合拆成三个正交层，逐层给出正确归属：

| 耦合层 | 现状（错误做法：写进技能） | 正确归属（下沉到运行时） | codex/Kode 对应 |
|---|---|---|---|
| **执行入口** | `SKILL.md` 每条命令改写成 `run_skill_command(...)` | 技能 verbatim 写 `python scripts/foo.py`；运行时 bridge 把它映射到通用命令原语 | codex：generic `shell_command` verbatim；Kode：`Bash` / `execute_script` verbatim |
| **路径契约** | 脚本改成读 `INPUT_DIR`、写 `OUTPUT_DIR`，新增 `path_contract.py` | 运行时提供**持久工作目录**当 `cwd`，技能用普通相对路径；IO 由项目在 session 边界处理 | codex：`workdir` + 相对路径；Kode：注入 `Base directory` |
| **Profile/镜像** | `SKILL.md` 出现 `office-basic`/`office-ocr` 镜像名、`recommended_profile` | 技能只声明**抽象运行时需求**（libreoffice/poppler/pptxgenjs…）；ProfileResolver 把需求映射到镜像；镜像名只存在于 Profile 注册表 | codex/Kode：平台沙箱透明包裹，技能不感知 |

**关键洞察**：Genesis 其实**已经有** `<skill_runtime_bridge>`（`react_loop.go`），它本就承诺「文档里说 `python scripts/foo.py` 时改用 `run_skill_command`，不要改第三方 `SKILL.md`」。也就是说——**bridge 已经能承担适配，技能本不必被改写**。office-ppt 迁移的错误在于「既上了 bridge、又改了技能」，双重适配，把耦合固化进了技能文件。所以修复的一半是：**彻底依赖 bridge，停止编辑技能正文与脚本**。

但只靠现有 bridge 不够，因为它映射到的 `run_skill_command(resource-id + inputs staging)` 依然是无状态模型，仍会逼出 `path_contract.py` 和脚本改写。所以另一半是：**把 bridge 的目标从「无状态 resource-id 执行」换成「持久 session + verbatim 命令执行」**。

### 2.2 第二轮反思：目标模型如何同时满足「verbatim」与「Genesis 保障」

第二轮要解决的真问题：codex/Kode 之所以能 verbatim，是因为它们**放弃了**强 IO 目录契约和强产物门禁（codex 只有平台沙箱 + `workdir`）。Genesis 想两者兼得，必须找到一个既 verbatim 又能保障的模型。逐个击破：

**(a) 多步共享状态 vs 无状态 staging** → 用 **Skill Task Session**：一次技能任务 = 一个持久工作目录（本地 `work`；沙箱 `/workspace`）。技能包 materialize 进去，`cwd` 指向它，`unpack/`、缩略图、`output.pptx` 都在同一目录，跨命令持久。这与 `docs/执行工作空间与Sandbox文件路径契约.md` §8「Office Skill 默认用 SandboxSession」完全一致。

**(b) verbatim 相对路径 vs Genesis IO 目录契约** → 关键取舍：**IO 目录环境变量仍然注入**（`WORK_DIR/INPUT_DIR/OUTPUT_DIR/TMPDIR`，供**愿意**用的 Genesis 原生任务代码使用），**但不强制技能使用**。第三方技能写 `cwd` 相对路径（`pres.writeFile({fileName:"Presentation.pptx"})`），因为 `cwd = 持久工作目录`，同样可被收集。即：
   - 「严格 OUTPUT_DIR 契约」是**给 Agent 自生成任务代码**的策略（保持 `docs/执行工作空间与Sandbox文件路径契约.md` §6 不变）；
   - 「verbatim 第三方技能」用 **session 工作目录收集**策略（本方案新增，见 §5.4）。
   两者按执行意图分流，不互相污染。

**(c) 产物如何在不「盲扫兜底」的前提下收集** → 采用**显式声明 + 命令级 diff**，禁止全局盲扫（遵守路径契约文档 §11「不做隐式兜底搬运」）：
   - `run_skill_command` 执行时记录**该命令新增/修改的文件**（命令级 diff），作为候选产物随结果返回；
   - Agent 按 `SKILL.md` 的 QA 流程最终会引用确定的成果文件路径（如 `output.pptx`）；运行时对**被引用的具体路径**做门禁校验（真 OOXML/PDF）并从 session 下载。
   - 收集永远是「命令产出的具体文件 + 显式引用」，绝不是对 `/workspace` 的盲扫。

**(d) verbatim `command` 字符串是否比 resource-id 白名单更不安全** → 这是一个需要正视的取舍：
   - resource-id 白名单其实是**虚假的安全感**（脚本接受任意参数，能力等价于任意执行）；
   - codex/Kode 都用通用 shell verbatim，安全由沙箱 + 审批 + 路径 preflight 保障，而非 id 白名单；
   - 保留一层**通用、非侵入**的护栏：运行时按技能 frontmatter 声明的解释器/命令白名单（`python`/`node`/`soffice`/`pdftoppm`…）校验 `command` 的可执行前缀，白名单外命令需额外审批。这给出了「resource-id 级别的约束」但不需要改技能。

**(e) DRY 的 `_office_common` 共享 vs 自包含** → 现状把 Anthropic 内嵌的 `scripts/office/*` 抽到 `_office_common`，并在 materialize 里硬编码 `SharedForPrefixes: ["office-"]`。这是又一处 Genesis 专有结构，且逼近「技能不自包含就跑不了」。为了「drop-in 第三方技能」的纯粹性，**改为自包含**：每个技能像 Anthropic 一样自带 `scripts/office/`。重复的成本可接受（技能是独立包，本就该自包含），换来的是零特例、零 `office-` 前缀硬编码。

**两轮反思后的定论**：目标模型 = **「Skill 内容 verbatim + 持久 Skill Session 工作目录 + verbatim 命令执行原语 + 保障全下沉执行层」**。它同时满足可移植性与 Genesis 的沙箱/产物/审计诉求，且与既有 `SandboxSession`、路径契约、Skill/Tool 协议边界一致。

---

## 3. 参考实现对照（结论）

详细调研见本次探索的四个子代理报告；此处只留可执行结论。

| 维度 | codex | Kode-CLI | 对 Genesis 的取用 |
|---|---|---|---|
| 技能=可执行原语？ | 否，纯文档+资源 | 否（CLI 主产品无专用 runner） | **取用**：技能只加载不执行，执行走通用原语 |
| 脚本执行 | generic `shell_command`，命令 verbatim，`workdir` 由 agent 设 | `Bash` verbatim；SDK 可选 `execute_script`（cwd=skill baseDir，verbatim） | **取用**：`run_skill_command(skill, command)` verbatim，cwd=session 工作目录 |
| 宿主注入 | 「相对路径相对 SKILL.md 目录解析」meta 指令 + 正文 verbatim | 运行期注入一行 `Base directory for this skill: <abs>` + 正文 verbatim | **取用+改造**：注入技能根（用 workspace-relative/sandbox 路径，**不用宿主机绝对路径**，遵守 AGENTS.md docker 边界）+ `<skill_runtime_bridge>` |
| 是否改写技能内容 | 否（超 8KB 才截断） | 否（只做 `$ARGUMENTS`/session-id templating） | **取用**：加载/运行分离，运行期只做 templating，绝不改正文/脚本 |
| Profile/镜像 | 平台沙箱透明 | 平台沙箱透明 | **改造**：声明式 ProfileResolver，镜像名只在 Profile 注册表 |
| 多步状态 | 同一 cwd 持久 | 同一 cwd 持久 | **取用**：Skill Session 持久 `/workspace`（对齐路径契约 §8） |
| Anthropic pptx 成熟度 | — | — | **照搬**：SKILL.md/editing.md/pptxgenjs.md/scripts 原样 |

Genesis 相对二者**保留的增量**（正当、且不进技能）：`Skill` 网关唯一加载入口、CollisionGuard、依赖预检/安装闭环、Approval、opaque locator、沙箱 required 不静默降级、产物门禁。

---

## 4. 目标架构

### 4.1 端到端流程

```text
LLM: Skill(skill="office-ppt", args="做竞品对比PPT")
  → SkillService.Resolve/Load（verbatim 正文；仅加载知识，无重副作用）
  → 记录技能绑定 + 分配逻辑工作目录标识（不开远程沙箱、不 materialize、不跑脚本）
  → React 注入：单份 <skill_injection>（verbatim 正文）+ <skill_runtime_bridge>（宿主适配说明）
                 + 一行 skill 根/工作目录（workspace-relative 或 sandbox 路径）

LLM 首次按 SKILL.md 原文执行脚本时：
  run_skill_command(skill="office-ppt", command="python -m markitdown deck.pptx")
  → 惰性打开 Skill Task Session（首次）：
       · 建持久工作目录（local: .genesis/runs/<run>/work；sandbox: /workspace）
       · materialize 技能包（scripts/ editing.md pptxgenjs.md office/…）到工作目录根
       · ProfileResolver(frontmatter.dependencies) → 选运行时镜像（office 类 → office 镜像）
       · 一次性 stage 用户输入文件到工作目录
  → 执行 verbatim 命令（cwd=工作目录）
  run_skill_command(skill="office-ppt", command="python scripts/thumbnail.py deck.pptx")
  ...（复用同一 session，cwd=工作目录，状态持久：unpacked/、缩略图、output.pptx 共存）
  → 每次执行：沙箱 profile + 审批 + 路径 preflight(advisory) + 命令级产物 diff(best-effort)
  → 结果含候选产物路径；Agent 引用最终 output.pptx
  → 运行时对被引用文件做门禁校验（真 OOXML/PDF）并回收为 artifact
```

### 4.2 工具面（LLM 可见）

| 工具 | 职责 | 相对现状的变化 |
|---|---|---|
| `Skill(skill, args[, resource])` | 唯一加载入口；仅加载知识 + 绑定技能 + 预留工作区标识（**不**开 session、**不** materialize） | 加载语义无重副作用 |
| `run_skill_command(skill, command[, timeout_ms])` | **verbatim** 执行技能命令，cwd=session 工作目录 | **取代** `run_skill_command(skill, script=<resource_id>, args, inputs)` |
| `install_skill_dependencies` | 构建期装 frontmatter 声明的依赖（须审批） | 保留，仍声明式 |
| `list/read/search_skill_resources` | references 渐进披露 | 保留 |

`run_skill_command` 输出（结构化 JSON）：`ok / command / exit_code / stdout / stderr / produced[] / failure_kind / suggested_install / retryable`。`produced[]` 是该命令新增/修改的候选文件（命令级 diff），不含全局盲扫。

### 4.3 与 Skill/Tool 协议边界的一致性

- 技能名仍**永不**进 function schema；只有 `Skill` 和 `run_skill_command` 等原语进 schema（`run_skill_command` 不是技能名，是通用原语）。符合 `2026-07-09-skill-tool-protocol-boundary-design.md`。
- CollisionGuard、`allowed-tools` 收窄、单份 injection、独占轮等既有机制不变。

---

## 5. 关键机制设计

### 5.1 Skill Task Session（持久工作目录）

- **懒打开**：`Skill(...)` 加载只做「加载知识 + 记录技能绑定 + 分配逻辑工作目录标识」，**不**立即开远程沙箱、**不**跑脚本（保持 `Skill` 加载语义无重副作用，符合协议边界文档）。session 在**第一次 `run_skill_command`** 时惰性打开；纯知识型技能（只读 references、不跑脚本）永不开 session。
- 生命周期绑定「一次技能任务」（可跨多次 `run_skill_command`）。
- 目录：本地 `.genesis/runs/<run>/work`（持久）；沙箱 `/workspace`（session 级持久）。`INPUT_DIR/OUTPUT_DIR/TMPDIR` 仍是 job 级，供原生任务代码使用。
- `cwd = 工作目录`；技能包 materialize 到该目录（见 5.2），使 `scripts/foo.py` 等相对路径 verbatim 可解析。
- 复用 `docs/执行工作空间与Sandbox文件路径契约.md` §8 的 `OpenSession/Run/Close`，Office/Skill 默认走 session。
- required 沙箱不满足 → fail-closed；optional 缺 session → 本地降级 + warning/trace/audit（对齐 AGENTS.md）。

**为什么用专用 `run_skill_command` 而非通用 `run_command`**：专用工具把「命令 ↔ 技能」显式绑定，运行时据此确定 profile、materialize 的技能包、权限范围与审计归属；支持同一任务内多个技能各自独立 session；也让通用 `run_command`（非技能执行）职责保持干净。命令仍 verbatim，`run_skill_command` 只是「带技能上下文的执行原语」，不是技能名进 schema。执行实现须委托 execution 能力域（`CommandRunner`/`SandboxRunner`），不在 tool 内直接 `exec.Command`（对齐 AGENTS.md）。

### 5.2 技能包 materialize 与相对路径

- 目标：让 `SKILL.md` 里的 `scripts/...`、`editing.md`、`assets/...` 相对当前 `cwd` 直接命中，无需任何改写。
- **确定做法（对齐 Anthropic/Claude 技能容器）**：把技能包完整 materialize 到 session 工作目录**根**（即工作目录内直接出现 `scripts/`、`editing.md`、`office/`…），`cwd = 工作目录`。用户输入文件 stage 到同一工作目录，产出文件也写在这里。于是 `python scripts/thumbnail.py deck.pptx`（`scripts/` 相对技能、`deck.pptx` 为用户文件）与 `pres.writeFile({fileName:"Presentation.pptx"})` 全部 verbatim 命中。技能文件是命令执行前就存在的，不会被命令级产物 diff 误判为产物。
- 二进制资源（`scripts/office/schemas/*.xsd` 等）必须**按字节拷贝**，不能走 UTF-8 `ReadResource`（现状已有此坑，须保证 embedded → 磁盘/沙箱按二进制落地）。
- 自包含：技能自带 `scripts/office/*`，**取消** `_office_common` 与 `SharedForPrefixes:["office-"]` 硬编码特例。
- 冲突规则：技能包文件先落盘；用户输入、Agent 生成脚本或后续产物若试图覆盖技能包保留路径（如 `SKILL.md`、`editing.md`、`pptxgenjs.md`、`scripts/**`、`assets/**`），运行时必须拒绝或要求改名，不能静默覆盖第三方技能内容。

### 5.3 加载期注入（宿主适配，不改技能）

`<skill_runtime_bridge>`（运行时注入，非技能内容）要点：

- 「`SKILL.md` 中的 shell 命令（如 `python scripts/foo.py ...`、`node ...`）改用 `run_skill_command(skill="<name>", command="<原文命令>")` 执行。」
- 「命令在技能工作目录中执行；技能脚本在 `scripts/...`（相对工作目录）；你的中间文件、用户输入文件都在该目录。」
- 「产出文件写在工作目录即可；无需 `INPUT_DIR/OUTPUT_DIR`；最终成果通过引用其路径交付，运行时会校验并回收。」
- 「不要为了运行而改写第三方 `SKILL.md` 或脚本；适配由运行时完成。」
- 一行工作目录/技能根提示：**workspace-relative 或 sandbox 路径**（docker/远程模式禁止宿主机绝对路径）。

### 5.4 三种模式统一路径与产物模型（重点，含沙箱契约改造）

这一节回答核心质疑：**「拷贝到 /workspace/output 再下载」不是最佳设计**。基于对 genesis-sandbox 真实契约的核对，给出正确的终态。

#### 5.4.1 genesis-sandbox 契约事实（核对结论）

| 目录 | 生命周期 | 是否自动回收 | 说明 |
|---|---|---|---|
| `/workspace`（WORK_DIR） | **lease 级持久**（跨 job 保留，直到 Release） | 否 | 会话工作桌；多步共享状态（`unpacked/`、缩略图、`output.pptx`）都放这里 |
| `/workspace/input`（INPUT_DIR） | **job 级**，每次执行后清空 | 否 | `input_artifact_ids` 注入点 |
| `/workspace/output`（OUTPUT_DIR） | **job 级**，每次执行后清空 | **是**（唯一自动回收） | 服务端 walk → `OutputArtifacts` → `DownloadArtifact(id)` |
| `/workspace/tmp`（TMPDIR） | **job 级**，每次执行后清空 | 否 | job 内临时 |

同一 lease 内 `cwd`、env、`/workspace` 根下文件跨 job 保留。**公开 API 目前没有「按路径读写 session workspace 文件」**；genesis-sandbox 已有 session 元数据、active sandbox 绑定、execd upload/download/list 与 sandbox-id 级 `UploadFile/DownloadFile` 底座，但最终形态还必须补齐 session scoped Service 方法、stat/mkdir/remove、严格 workspace-relative path 规整、软链越界防护、HTTP 路由、OpenAPI 与审计。genesis-agent 侧 `sandbox/contract` 的 `FileSystemClient`（`ReadFile/WriteFile/ListDir/Walk/Stat/MkdirAll/Remove`）与 `SandboxSession`（`OpenSession/StageInput/Run/Close`）端口已存在，HTTP adapter 里 `FileSystemClient` 全 stub 成 `unsupported`。

**关键判断**：方向不是把会话产物塞回 `/workspace/output`，而是把已有 session、execd 文件搬运和 agent 端 `FileSystemClient` 抽象收敛成正式的 **Session WorkspaceFS**。这需要两仓同步补齐协议与安全边界，也会成为通用 Agent 运行时长期需要的基础能力（Agent 多轮迭代要按路径读写工作区文件，不能只靠 job 级 artifact 模型）。

#### 5.4.2 两种 workload 形态，两套路径纪律（按执行意图分流）

| 形态 | 适用 | 路径纪律 | 产物 |
|---|---|---|---|
| **无状态任务 job**（保持现状最佳实践） | Agent 自生成一次性任务代码 | 严格：输入 → `/workspace/input`；成果 → `/workspace/output`；临时 → `/workspace/tmp` | `/workspace/output` **自动回收**（保持不变） |
| **会话式工作区**（本方案新增，修 verbatim 技能） | 多步 verbatim 技能、Agent 迭代任务 | `/workspace` 为持久工作桌，`cwd=/workspace`；技能包/输入/中间/成果都在此，用**普通相对路径** | 通过 **WorkspaceFS 显式按路径读取**声明的成果文件回收 |

**不改**沙箱三目录規範（`input/output/tmp` 依旧是自动回收标准，无状态 job 继续用）；**新增** WorkspaceFS 显式文件访问原语，供会话式工作区把文件按路径进/出。两者共存、按意图选择，互不污染。这也符合路径契约 §11「不做隐式兜底搬运」——WorkspaceFS 是**显式按声明路径取**，不是对 `/workspace` 盲扫。

#### 5.4.3 三种模式统一原语

genesis-agent 用统一的 **`WorkspaceFS` 端口**（即已存在的 `FileSystemClient`）+ **`SkillSession`**（即已存在的 `SandboxSession`），三种 backend 各自实现：

| 原语 | 无沙箱（host） | 本地平台沙箱 | genesis-sandbox 容器 |
|---|---|---|---|
| 建/持有会话工作区 | `.genesis/runs/<run>/work`（持久目录） | 同 host（宿主机目录 + 平台沙箱进程约束） | `OpenSession` 租一个 lease，`cwd=/workspace` |
| 放入文件（技能包/输入） | `os` 写入 work 目录 | 同 host | `WorkspaceFS.WriteFile(path)` 或「上传 zip + 解压」进 `/workspace` |
| 执行 verbatim 命令 | 直接执行，`cwd=work` | 平台沙箱包裹，`cwd=work` | `SkillSession.Run`（同 lease 多次），默认 `cwd=/workspace` |
| 取出成果（按声明路径） | `os` 读 work 目录路径 | 同 host | `WorkspaceFS.ReadFile("output.pptx")` |
| 门禁 + 落地 | `gate.CheckDelivery` → `.genesis/artifacts/...` | 同 host | 同左（下载后本地门禁） |

- **输入**：session 打开后**一次性**把技能包与用户输入写入工作区（不再每命令重复 staging）。技能用相对路径 `deck.pptx`。
- **成果**：Agent 按 `SKILL.md` 的 QA 流程最终引用确定成果路径（如 `output.pptx`）；运行时用 `WorkspaceFS.ReadFile` 按该**显式路径**取出、门禁校验、落地为 artifact。无 cp、不受 `/workspace/output` 约束、不盲扫。
- **produced[] 提示**：`run_skill_command` 可 best-effort 返回该命令新增/修改文件（host：mtime/快照 diff；远程：`WorkspaceFS.ListDir` 前后对比或 Agent 声明）仅作提示，不作自动回收依据。

#### 5.4.4 必需的接线改造（两仓一并改，直接落最终形态）

本项目处于开发阶段，不保留过渡形态，不兼容旧 `run_skill_command` 的 output-only 收集模型。远程 Skill/Agent 会话统一使用 **session scoped WorkspaceFS**：文件归属 `Session` 绑定的 active sandbox，API 以 `session_id` 为资源边界；客户端只传 workspace-relative path，由服务端统一映射到容器 `/workspace`。

- **genesis-sandbox**：新增 session 级 WorkspaceFS HTTP API，必须走 `app.Service`，不得把 execd 内部 `/files/*` 裸露给外部客户端。服务层负责租户/用户/session ownership、lease 状态、路径规整、软链越界防护、大小限制、审计，再复用 runtime driver 的 execd/Docker 文件能力：
  - `GET /v1/sessions/{id}/files?path=...`：下载单个文件。
  - `PUT /v1/sessions/{id}/files?path=...`：上传/覆盖单个文件，自动创建父目录。
  - `GET /v1/sessions/{id}/files:list?path=...&recursive=false|true&limit=...`：列目录；`recursive=true` 是 bounded walk，不做无限扫描。
  - `GET /v1/sessions/{id}/files:stat?path=...`：读取文件/目录元数据。
  - `POST /v1/sessions/{id}/dirs?path=...`：创建目录。
  - `DELETE /v1/sessions/{id}/files?path=...&recursive=false|true`：删除文件；目录删除必须显式 `recursive=true`。
  - `/workspace/output` 自动回收继续只服务无状态 job，和 WorkspaceFS 并存但不互相替代。
- **genesis-agent**：`sandbox/adapter/http` 把 `FileSystemClient`（现 `unsupported`）实现到上述 session scoped API；`ExecStack` 增加 `WorkspaceFS`；Skill 脚本服务改为跨多次 `run_skill_command` 复用同一 `SkillSession`，并用 `WorkspaceFS.ReadFile(声明路径)` 显式取回最终产物。
- **路径规范**：WorkspaceFS API 入参使用 workspace-relative path（如 `output.pptx`、`scripts/thumbnail.py`、`unpacked/ppt/slides/slide1.xml`）。服务端拒绝空路径、绝对路径、`..`、NUL、反斜杠规避、软链逃逸；响应中可同时返回 `path`（相对路径）与 `container_path`（诊断用 `/workspace/...`），业务契约只依赖相对路径。

### 5.5 Profile 声明式解析（去镜像名耦合）

- 技能 frontmatter 只声明**抽象运行时需求**（复用现有 `dependencies.runtime`：`system: [libreoffice, poppler]`、`node: [pptxgenjs]`、`python: [markitdown, pillow]`）。
- 新增 `ProfileResolver`：把「需求集合」映射到「可用 Profile/镜像」。镜像名（`office-basic` 等）只存在于 Profile 注册表 / 产品 bootstrap，**不出现在技能正文**。
- **取消** `inferProfile` 的技能名启发式（`office-*` → `office-basic`）。
- 校验器/护栏（5.6）也从 frontmatter 的 `dependencies.commands` 取解释器白名单。

### 5.6 安全护栏（通用、不侵入技能）

- 沙箱隔离（平台/docker/remote）：主安全边界。
- 命令前缀白名单：`command` 的可执行程序须在 frontmatter 声明的解释器/命令集合内，否则额外审批。
- 路径 preflight：对技能采用 **advisory**（提示而非拒绝），因为技能是相对 `cwd` 工作、被信任度较高；真正边界仍是沙箱。Agent 自生成代码维持 strict（路径契约 §6 不变）。
- 假二进制拦截：`filesystem/binarygate` 继续拦 `write_file` 直接写假 OOXML/PDF，倒逼走 `run_skill_command`（保留）。
- 产物门禁：交付前校验真 OOXML/PDF（保留）。

---

## 6. office-ppt 试点迁移方案（文件级）

原则：**照搬 Anthropic `pptx`，除非 100% 确定更优且有效**。

### 6.1 直接照搬（verbatim，字节级对齐 Anthropic）

| 目标路径（office-ppt/） | 来源（anthropics pptx/） | 说明 |
|---|---|---|
| `SKILL.md`（正文） | `SKILL.md` | 正文 verbatim；仅 frontmatter 见 6.3 |
| `editing.md` | `editing.md` | verbatim，**放技能根**（不再挪进 `references/`，保持与正文内链一致） |
| `pptxgenjs.md` | `pptxgenjs.md` | verbatim |
| `scripts/thumbnail.py` | `scripts/thumbnail.py` | verbatim |
| `scripts/add_slide.py` | `scripts/add_slide.py` | verbatim |
| `scripts/clean.py` | `scripts/clean.py` | verbatim |
| `scripts/office/**`（pack/unpack/validate/soffice/helpers/validators/schemas 全套） | `scripts/office/**` | verbatim、按字节；**自包含**，不用 `_office_common` |
| `LICENSE.txt` | `LICENSE.txt` | 一并带上（合规） |

### 6.2 删除（Genesis 自造、造成耦合）

- `scripts/path_contract.py` — 由 session `cwd` + 相对路径取代。
- `scripts/run_pptxgen_script.js`、`scripts/create_pptx.js` — Anthropic 直接 `node` + `require("pptxgenjs")` + `pres.writeFile({fileName:"..."})`，照搬即可，不需要 Genesis wrapper。
- `scripts/inspect_pptx.py`、`scripts/extract_pptx_text.py` — Anthropic 用 `python -m markitdown`；**恢复 markitdown**，由 office Profile 预装。删除自研替代。
- `scripts/render_pptx_preview.py` — Anthropic 用 `scripts/office/soffice.py --convert-to pdf` + `pdftoppm`，照搬即可。
- `references/design.md`、`references/validation-checklist.md` — 这些内容 Anthropic 原本内嵌在 `SKILL.md`（Design Ideas / QA），照搬正文即可，不外置、不中文化改写。
- `internal/capabilities/skill/adapter/embedded/skills/_office_common/` — 取消共享包与 `office-` 前缀特例。

> 关于「恢复 markitdown、删除自研脚本」是否属于「100% 确定更优」：这里遵循用户「照搬优先」原则——自研 `extract_pptx_text.py`/`inspect_pptx.py` 是**重新引入耦合的分叉**，且需持续维护 OOXML 解析；恢复 markitdown 是回到成熟上游，判定为「照搬更优」。若 office 镜像暂不含 markitdown，则由 ProfileResolver/镜像补齐，而非改技能。

### 6.3 frontmatter：最小化、命名空间化、可被其他 runtime 忽略

保留 Anthropic 的 `name`/`description`/`license`。Genesis 需要的元数据要么复用**生态标准字段**，要么放**命名空间前缀**，使其他运行时（codex/Kode/Claude）无视即可，技能仍 100% 可移植：

- `allowed-tools` 属生态标准字段（Claude/Kode 皆用），保留在顶层。
- 复用中性 `dependencies.runtime`（system/node/python）——供 ProfileResolver 与依赖闭环，不含镜像名。
- Genesis 独有治理项（产品可见范围、审批策略、`context`/`model` 等）统一放 `x-genesis:` 命名空间块（示例：`x-genesis: { products: [cli, desktop, enterprise], sandbox: office }`），不散落顶层。
- **正文与脚本零 Genesis 措辞**：不出现 `run_skill_command`/`run_skill_command`、镜像名、`INPUT_DIR/OUTPUT_DIR`、「Genesis 硬约束」。这些行为由 bridge + 运行时承担。

> 判定：frontmatter 增量是「元数据附加」，不改变正文语义，且被命名空间隔离，不破坏 verbatim/可移植性；属可接受的最小耦合，且是声明式治理所必需。

### 6.4 迁移后目录（试点结果）

```
office-ppt/
├── SKILL.md          # Anthropic 正文 verbatim + x-genesis 命名空间 frontmatter
├── editing.md        # verbatim
├── pptxgenjs.md      # verbatim
├── LICENSE.txt
└── scripts/
    ├── thumbnail.py add_slide.py clean.py      # verbatim
    └── office/**                                # verbatim、自包含（pack/unpack/validate/soffice/helpers/validators/schemas）
```

---

## 7. 运行时改造清单（实现对照）

> 下面同时作为实现对照清单使用：genesis-agent 仓内已完成的项按最终形态直接替换；涉及 genesis-sandbox session scoped WorkspaceFS API 的项仍需两仓联动收尾。

1. **执行工具**：新增 `run_skill_command(skill, command[, timeout_ms])`；删除/替换 `run_skill_command` 的 resource-id + inputs staging 语义。
   - 归属：`internal/capabilities/skill/tool/run_skill_command/`（新）、`internal/capabilities/skill/script/service/`（改）。
2. **Skill Session（复用已实现的 `SandboxSession`，改为跨调用复用 + 惰性打开）**：首次 `run_skill_command` 惰性打开；本地 `work` 持久、沙箱 `/workspace` 持久；**跨多次 `run_skill_command` 复用同一 session**（现状每次开/关）。
   - 归属：`internal/capabilities/skill/script/service/`（session 复用与生命周期）、`internal/capabilities/sandbox/contract/client.go`（`SessionClient`/`SandboxSession` 已存在）、`internal/capabilities/execution/service/runner.go`。
3. **materialize 自包含**：按字节落盘；删除 `_office_common` 合并与 `SharedForPrefixes` 硬编码。
   - 归属：`internal/capabilities/skill/script/materialize/`、`internal/capabilities/skill/adapter/embedded/`（删 `common.go` 特例、`_office_common/`）。
4. **ProfileResolver**：`dependencies.runtime` → Profile/镜像；删除 `inferProfile` 技能名启发式；镜像名只留在 Profile 注册表/bootstrap。
   - 归属：`internal/capabilities/skill/script/service/service.go`、`internal/capabilities/execution/model`、`products/*/bootstrap`。
5. **runtime bridge 改写**：映射目标改为 `run_skill_command`；补「工作目录/技能根一行注入（workspace-relative/sandbox 路径）」。
   - 归属：`internal/runtime/strategy/react/react_loop.go`、`skill_support.go`。
6. **WorkspaceFS 打通（关键，两仓一并改，最终形态）**：genesis-agent 把 `sandbox/adapter/http` 里 stub 的 `FileSystemClient`（`ReadFile/WriteFile/ListDir/Stat/MkdirAll/Remove`）实现到 genesis-sandbox 新暴露的 session scoped WorkspaceFS API；产物用 `WorkspaceFS.ReadFile(声明路径)` 显式取出 + 门禁校验回收；`produced[]` 走命令级 diff/`ListDir` 前后对比，仅作提示。
   - genesis-agent 归属：`internal/capabilities/sandbox/adapter/http/client.go`（去 `unsupportedFileSystem` stub）、`internal/capabilities/skill/script/service/service.go`（`collectArtifacts` 改按声明路径取）、`internal/capabilities/skill/script/gate/`。
   - **genesis-sandbox 归属（一并改）**：`internal/runtime/port/types.go` 增加 WorkspaceFS DTO/Service 方法；`internal/app/session_file_service.go` 落地 session ownership、lease 状态、workspace-relative path 规整、软链越界防护、大小限制和审计；`internal/interfaces/http/router.go` + handler 暴露 `GET/PUT/DELETE /v1/sessions/{id}/files?path=`、`GET /v1/sessions/{id}/files:list`、`GET /v1/sessions/{id}/files:stat`、`POST /v1/sessions/{id}/dirs`；runtime driver 复用 execd/Docker 文件能力；`/workspace/output` 自动回收保持不变。
7. **护栏**：命令前缀白名单（frontmatter `dependencies.commands`）；技能路径 preflight 降为 advisory；保留 binarygate 与门禁。
   - 归属：`internal/capabilities/execution/pathcontract`、`internal/capabilities/filesystem/binarygate`。
8. **frontmatter 解析**：支持 `x-genesis:` 命名空间；其余 verbatim。
   - 归属：`internal/capabilities/skill/adapter/embedded/source.go`、skill frontmatter parser。
9. **三端统一装配（消除漂移，见 §12）**：把 CLI 自研的 `buildSkillService`/`productExecStack` 与 `skillstack.BuildEmbedded` 合并为**唯一** `skillstack.BuildSkillStack(sources, execStack, options)`；三端只注入不同 `Sources` + `ExecStack`（新增 `WorkspaceFS`）+ 策略；`BuildEmbedded` 退化为「embedded-only sources」的薄封装。
   - 归属：`shared/skillstack/stack.go`、`products/cli/bootstrap/container.go`（删 `productExecStack` 重复、改调 `BuildSkillStack`）、`products/enterprise/bootstrap/container.go`、`products/desktop/bootstrap/`。

---

## 8. 与现有文档/契约的关系

- `docs/执行工作空间与Sandbox文件路径契约.md`：本方案**沿用**其三目录規範（`/workspace/input|output|tmp`）与 SandboxSession（§8），无状态 job 的 `/workspace/output` 自动回收**保持不变**；**新增**「会话式工作区 + WorkspaceFS 显式按路径取文件」用于 verbatim 技能与 Agent 迭代任务。需在该文档补：(1) 两种 workload 形态与两套路径纪律（本方案 §5.4.2）；(2) WorkspaceFS 作为 session 级文件原语（区别于 `/workspace/output` 自动回收，非兜底盲扫）。
- `docs/沙箱API对接与Profile选择规则.md`：`inferProfile` 名称启发式 → `ProfileResolver` 声明式；需同步修订。
- `docs/Skill三模式执行与依赖闭环设计.md`：`install_skill_dependencies` 与依赖闭环保留；执行入口由 `run_skill_command` → `run_skill_command`，需同步措辞。
- `docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`：协议边界不变（技能名不进 schema）；`run_skill_command` 作为新原语补充。
- `docs/Skill调用原理对比-Kode-Codex-Genesis.md`：加载协议不变；执行段落更新为 session + verbatim。

---

## 9. 参考文件清单（后续开发照此借鉴）

### 9.1 Anthropic pptx（照搬源）
- `D:\workspace\go\go-project\anthropics-skills\skills\pptx\SKILL.md`
- `D:\workspace\go\go-project\anthropics-skills\skills\pptx\editing.md`
- `D:\workspace\go\go-project\anthropics-skills\skills\pptx\pptxgenjs.md`
- `D:\workspace\go\go-project\anthropics-skills\skills\pptx\scripts\**`（含 `office/pack.py`、`office/unpack.py`、`office/soffice.py`、`office/validators/**`、`office/schemas/**`）
- `D:\workspace\go\go-project\anthropics-skills\skills\pptx\LICENSE.txt`

### 9.2 codex（verbatim 执行 + workdir + 多源发现）
- `codex-rs/core-skills/src/loader.rs`（多源发现、`SKILL.md`、frontmatter 修复）
- `codex-rs/core-skills/src/render.rs`（`SKILLS_HOW_TO_USE_*`：相对路径相对 SKILL.md 解析、渐进披露）
- `codex-rs/core/src/tools/handlers/shell/shell_command.rs`（verbatim 命令 + `workdir` 解析）
- `codex-rs/core/src/tools/handlers/mod.rs`（`resolve_workdir_base_path`、相对路径 base）
- `codex-rs/skills/src/lib.rs`（bundled 解压到 `.system`）
- `codex-rs/core-skills/src/invocation_utils.rs`、`core/src/skills.rs`（隐式检测仅遥测，不改控制流）
- `codex-rs/sandboxing/src/manager.rs`（平台沙箱透明包裹）

### 9.3 Kode-CLI（Skill 网关 + 运行期 base dir 注入 + verbatim）
- `Kode-CLI/apps/cli/src/services/customCommands/loader.ts`（多源发现）
- `Kode-CLI/apps/cli/src/services/customCommands/discovery.ts`（progressive disclosure、`getPromptForCommand`：注入 `Base directory for this skill:`、`$ARGUMENTS` templating）
- `Kode-CLI/packages/tools/src/tools/interaction/SkillTool/SkillTool.tsx`（`Skill` 网关 + `<available_skills>`）
- `Kode-CLI/kode-agent-sdk/src/tools/scripts.ts`（`execute_script`：cwd=skill baseDir、verbatim、脚本类型推断）
- `Kode-CLI/kode-agent-sdk/src/core/skills/sandbox-file-manager.ts`（skill 级 workDir + enforceBoundary）
- `Kode-CLI/packages/builtin-skills/skills/webapp-testing/SKILL.md`（verbatim `python scripts/...` 写法样例）

### 9.4 Genesis 现有（改造对象）
- `internal/capabilities/skill/tool/skill/tool.go`（`Skill` 网关，仅加载知识并记录技能绑定/逻辑工作区标识，不打开 session）
- `internal/capabilities/skill/tool/run_skill_command/tool.go`（替换为 `run_skill_command`）
- `internal/capabilities/skill/script/service/service.go`（session、ProfileResolver、去 `inferProfile` 启发式）
- `internal/capabilities/skill/script/workspace/workspace.go`、`logical.go`（持久工作目录）
- `internal/capabilities/skill/script/materialize/materialize.go`（自包含、按字节）
- `internal/capabilities/skill/adapter/embedded/source.go`、`common.go`、`system.go`（去 `_office_common` 特例）
- `internal/capabilities/skill/script/gate/gate.go`、`failure_classify.go`（产物门禁、命令级 diff）
- `internal/runtime/strategy/react/react_loop.go`、`skill_support.go`（bridge、injection）
- `internal/capabilities/execution/service/runner.go`（env 注入、cwd）
- `internal/capabilities/sandbox/contract/client.go`（`FileSystemClient`/`SandboxSession`/`SessionClient` **端口已存在**，直接用）
- `internal/capabilities/sandbox/adapter/http/client.go`（`FileSystemClient` 现全 stub 成 `unsupportedFileSystem`，需落地到新端点；`OpenSession`/`StageInput`/`DownloadArtifact` 已实现）
- `products/cli/bootstrap/container.go`、`shared/skillstack/stack.go`（装配）

### 9.5 genesis-sandbox（一并改；session/execd 底座已存在，需补最终 WorkspaceFS API）
- `D:\workspace\go\genesis-sandbox\internal\interfaces\http\router.go`（新增 `/v1/sessions/{id}/files*`、`/files:list`、`/files:stat`、`/dirs` 路由）
- `D:\workspace\go\genesis-sandbox\internal\app\session_file_service.go`（新增 session scoped WorkspaceFS service，复用现有 `UploadFile`/`DownloadFile` 能力但不复用其 sandbox-id API 形态）
- `D:\workspace\go\genesis-sandbox\internal\execd\server.go`（补齐 `/files/stat|mkdir|remove`，修正 `/files/list` 为默认非递归 + bounded walk，并用真实路径校验防软链逃逸）
- `D:\workspace\go\genesis-sandbox\internal\runtime\port\types.go`（`CollectOutputDir="/workspace/output"`、cwd/env 跨 job 持久、`WorkspaceReset`）
- `D:\workspace\go\genesis-sandbox\docs\未来设计.md`（三目录約定）、`sdks\go\sandbox\session.go`（lease 复用、`RunSequential`）
- `D:\workspace\go\genesis-sandbox\api\openapi.yaml`（同步新增端点契约）

---

## 10. 风险与残留

1. **markitdown/依赖是否在 office 镜像内**：若镜像缺 markitdown/pptxgenjs/poppler，需在 Profile/镜像补齐（归属 genesis-sandbox 仓与 Profile 注册表），不得回退到「改技能」。属外部依赖，须在落地前确认镜像清单。
2. **WorkspaceFS 需两仓一并改**：直接落最终形态，genesis-sandbox 暴露 session scoped WorkspaceFS API，genesis-agent 落地 `FileSystemClient` 并接入 SkillSession（§5.4.4/§7.6）。不提供收尾 `cp`、不借 `/workspace/output` 作为会话产物通道、不保留旧 output-only 执行模型。
3. **verbatim `command` 的安全边界**：以沙箱 + 命令前缀白名单 + 审批 + 审计为准；接受「不再有 resource-id 白名单」的取舍（§2.2(d)）。
4. **自包含导致的重复**（多个 office 技能各带 `scripts/office`）：接受，换取零特例与可移植性；若未来体积成问题，可在**运行时层**做只读共享缓存（不进技能内容），而非回退共享包。
5. **本地无 soffice/pdftoppm**：视觉 QA 缺失时按技能原文「如实说明未完成」，不伪造（Anthropic 与现状一致）。

---

## 11. 非目标

- 不为让第三方技能运行而编辑其 `SKILL.md`/脚本。
- 不在技能正文出现镜像名、`run_skill_command`、`INPUT_DIR/OUTPUT_DIR`。
- 不做 `/workspace` 盲扫或收尾脚本兜底收集产物（WorkspaceFS 是显式按声明路径取）。
- 不废弃沙箱 `/workspace/input|output|tmp` 三目录規範与 `/workspace/output` 自动回收；会话式工作区是**新增**能力，两者按 workload 形态并存。
- 不把技能名注册为 Tool。
- 不为兼容旧 `run_skill_command`/`_office_common`/`path_contract.py` 保留过渡分支（开发期直接替换）。
- 不在通用内核里按产品类型分支（`if product == enterprise`）；差异只走端口注入（§12）。

---

## 12. 三端（CLI / Desktop / Enterprise）统一与独立的平衡

本节回答「是否考虑了三端统一与独立的平衡」。结论：**技能执行内核完全统一，产品差异只通过端口注入**，与 `docs/产品分发架构设计.md`「一个共享内核 + 三条产品线」一致。

### 12.1 现状（必须先纠正的漂移）

- Enterprise：`products/enterprise/bootstrap` → `skillstack.BuildEmbedded(Options{ExecStack})`，注入缝干净。
- CLI：**没有**用 `skillstack`，而是自研 `buildSkillService` + `productExecStack`（与 `skillstack.ExecStack` 字段重复）。
- 风险：新的 `run_skill_command` + SkillSession + WorkspaceFS 语义若只改一处，另一处会漂移；两端行为不一致。
- 结论：**先把三端收敛到唯一 `skillstack.BuildSkillStack`**（§7.9），再加新能力。

### 12.2 统一 vs 独立划线

| 归属 | 内容 | 位置 |
|---|---|---|
| **统一内核**（产品无关，只依赖端口） | Skill 加载协议（网关/catalog/collision/injection/bridge）、SkillSession 生命周期、`run_skill_command` verbatim 语义、materialize 自包含、产物门禁、失败分类、路径 preflight、ProfileResolver「需求→抽象 profile」映射、`BuildSkillStack` builder | `internal/capabilities/skill/**`、`shared/skillstack` |
| **按产品注入**（端口实现 + 策略 + 数据） | 执行 backend、Session backend、WorkspaceFS backend、沙箱模式默认值、Profile→镜像/可用性注册表、Skill Sources、Approval/RBAC/租户/审计/用量、artifact 落地根、输入授权方式 | `products/<product>/bootstrap`、`shared/local`、`sandbox/adapter/http` |

**注入缝 = `ExecStack`**（扩展）：`{ Runner, SessionClient, WorkspaceFS(新增=FileSystemClient), WorkspaceRef, Sandbox }` + `Sources []skill.Source` + `Options{Product, Environment, Approval, EnabledTools/Skills, ...}`。内核只见端口，永不 `if product`。

### 12.3 三端 × 三模式矩阵

| 执行模式 | CLI | Desktop | Enterprise | WorkspaceFS 实现 |
|---|---|---|---|---|
| 无沙箱（宿主直跑） | 默认 | 默认 | **禁止**（红线：企业不碰宿主 FS） | 本地 os 端口（`.genesis/runs/<run>/work`） |
| 本地平台沙箱（Seatbelt/bwrap/JobObject） | 可选 `ModePlatform` | 可选 | 一般不用 | 同上（平台沙箱只约束**执行进程**，不改运行时 FS 端口） |
| genesis-sandbox 容器 | 可选 `ModeDocker/Remote` | 可选 | **强制** `Required` | HTTP `FileSystemClient`（§5.4.4 打通） |

要点：
- **无沙箱与本地平台沙箱共用同一个 WorkspaceFS 本地实现**——平台沙箱只隔离被执行的命令进程，Agent 运行时仍在宿主机读写 `.genesis/runs/<run>/work`。故 WorkspaceFS 只需两种实现（本地 os / 远程 HTTP），对齐 `shared/local` vs `sandbox/adapter`。
- **三种模式对 CLI/Desktop 都可选**（`buildSandboxStack` 已支持 disabled/platform/docker/remote）；Enterprise 强制 genesis-sandbox。
- SkillSession 语义统一：本地=持久 `.genesis/runs/<run>/work` 目录会话；远程=genesis-sandbox Session（内部绑定 active lease/sandbox）。内核只感知 `session_id` + WorkspaceFS，不依赖 lease 细节。

### 12.4 各端独立注入清单

| 维度 | CLI | Desktop | Enterprise |
|---|---|---|---|
| Skill Sources | embedded + 本地磁盘 + 已装/marketplace | 同 CLI | embedded（+ 未来租户 marketplace，RBAC 可见性过滤） |
| 执行/Session/WorkspaceFS backend | 本地（可选 docker/remote） | 本地（可选 docker/remote） | genesis-sandbox（租户级 lease + 远程 FS） |
| 沙箱默认 | disabled，可选 platform/docker | disabled，可选 | required |
| Profile→镜像/可用性 | 本地工具探测（有无 soffice 等）| 同 CLI | 沙箱镜像目录（office 镜像等） |
| 输入授权 | WorkDir 权限 | GUI 文件选择器授权 | PathResolver + RBAC + upload staging |
| Approval/审计/用量 | 本地 | 本地 + 桌面通知 | 企业审批流 + PostgreSQL 审计/Usage |
| artifact 落地 | `.genesis/artifacts` | `.genesis/artifacts`（+ 画布展示） | 租户 artifact store |

### 12.5 Desktop 增量最小

Desktop 与 CLI 在**技能执行层几乎等价**（同 `shared/local`），差异只在 GUI 输入授权与多 Agent 可视化——这些不进技能执行内核。因此三端统一后，Desktop 复用 CLI 同一套 `BuildSkillStack`，几乎零额外成本。

### 12.6 隔离红线校验（沿用 §产品分发架构设计.md 六/十）

- `internal/capabilities/skill/**` 不 import `products/**`；差异全走端口。✓
- Enterprise 不用宿主 FS backend；WorkspaceFS 只注入 HTTP 实现。✓
- CLI/Desktop 可 import `internal/capabilities/sandbox/adapter/http`（属 internal、产品无关）以支持 docker/remote 选项。✓
- `BuildSkillStack` 内不得出现产品类型分支；只消费注入端口。✓







