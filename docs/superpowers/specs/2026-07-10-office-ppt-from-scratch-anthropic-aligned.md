# Office PPT 从零生成：对齐 Anthropic 的最佳实践（最终版）

> 状态：**最终版**（review-fix-rereview 反思收敛，2026-07-10）  
> 原则：**优先 Anthropic 成熟方法**；仅在 Genesis 硬约束处做最小适配。  
> 权威本文；下列旧稿勿再当主设计：  
> - `2026-07-10-office-ppt-from-scratch-best-practice.md`（JSON deck 为主，**已废止**）  
> - `2026-07-10-office-ppt-create-deck-best-practice.md`（跳转页）  
> 关联：`anthropics-skills/skills/pptx`、`docs/Anthropic-Office-Skills完整能力迁移设计.md` §5、`docs/Skill三模式执行与依赖闭环设计.md`

---

## 0. 审查与反思记录

| 轮次 | 从第一性原理的发现 | 处置 |
| --- | --- | --- |
| 重评 | JSON deck 不能证明优于 Anthropic → 不应作默认 | 主路径改回写 pptxgen JS |
| R1 | `module.exports = run(ctx)` 与 Anthropic/`pptxgenjs.md` **顶层脚本**教程不一致 → 模型照抄必失败 | §2 改为 **plain script + `node` 子进程**（最贴 Anthropic） |
| R1 | §1.2 误写「用户 JS 必须写到 OUTPUT_DIR」 | 纠正：源码在 `$WORK_DIR` → stage 进 `INPUT_DIR`；**仅 `.pptx` 进 `OUTPUT_DIR`** |
| R1 | Agent 通常不知道本 Run 的 `WORK_DIR` 绝对路径 | **已修订**：`write_file("$WORK_DIR/...")` + PathResolver 按 RunID 展开；禁止写仓库根 |
| R3 | 仓库根残留 `deck_gen.js` 非最佳实践 | 中间脚本强制 `$WORK_DIR`；提示词注入本 Run 逻辑目录 |
| R1 | 多份 specs 互相矛盾（JSON 版仍自称最终版） | 本文标权威；旧稿标废止 |
| R2 | `require(user.js)` 有模块缓存，改文件再跑可能脏 | runner 用 **每次新 `node` 进程** 执行 staged 脚本 |
| R2 | 迁移文仍写 `run_skill_script`/`run_command` 并列 | 产品主路径只认固定 ResourceID runner；`run_command` 不写进 SKILL 默认 |

**可选 polish**：JSON deck 辅路径整段可从实施切片删除以免复发（本文仍保留「明确不做默认」一句）。  
**残余风险**：模型写烂 pptxgen / 同级代码执行面——与 Anthropic 相同，靠 QA + 审批，不靠 DSL 假装更安全。

---

## 1. 从第一性原理

### 1.1 真问题

用户要：**内容正确、版式可用的多页 `.pptx`**。  
Anthropic 已验证的方法是「模型写完整 pptxgen 脚本 + 直接用 Node 跑 + 视觉 QA」，不是「调用一个单页壳」或「填 JSON DSL」。

### 1.2 现状失败真正缺什么

| 已有 | 缺口 |
| --- | --- |
| `pptxgenjs.md`、设计原则、thumbnail/inspect | **无法把 Agent 写出的 JS 当作业务脚本执行**（`script` 必须是 Skill ResourceID） |
| `create_pptx.js` | 自研最小壳，**能力不等于** Anthropic 从零生成 |
| `stageInputs` 只认 workspace 根 | QA 接力断（与生成策略正交，但必须修） |

### 1.3 Genesis 硬约束（只逼换入口，不逼换方法）

| 约束 | 含义 | 正确适配 |
| --- | --- | --- |
| I2 | 业务入口 = `run_skill_script(ResourceID)` | 固定 `run_pptxgen_script.js`，内部 `node` 跑用户脚本 |
| I1 | 逻辑目录 | 用户脚本经 `inputs` → `INPUT_DIR`；产物 → `OUTPUT_DIR` |
| 审批/审计 | 可追踪 | 仍走 `run_skill_script`，不另开裸 `run_command` 主路径 |
| Windows argv | 中文易坏 | 文案写在 **JS 源文件**（UTF-8），不进 `args[]` |

### 1.4 失败条件

- 再把 `create_pptx.js` 或 JSON deck 写成从零生成默认。  
- 要求模型先学一套与 `pptxgenjs.md` 冲突的 `run(ctx)` 导出才能跑通。  
- 为对齐 Anthropic 而默认放开业务 `run_command node`。

---

## 2. 推荐主路径（Anthropic-aligned）

与 Anthropic 的对应关系：

| Anthropic | Genesis |
| --- | --- |
| 读 `pptxgenjs.md` | 同（保留主教程） |
| 写出 `foo.js`（顶层脚本） | `write_file("$WORK_DIR/deck_gen.js")`（本 Run work；禁止仓库根） |
| `node foo.js` | `run_skill_script(script=run_pptxgen_script.js, args=["deck_gen.js"], inputs=["$WORK_DIR/deck_gen.js"])` |
| 写 `Presentation.pptx` | 写 `path.join(process.env.OUTPUT_DIR, "….pptx")` |
| thumbnail / 目视 QA | `inspect` / `thumbnail` / checklist（`inputs` 可指本 Run 产物） |

```text
1. 读 references/pptxgenjs.md
2. write_file("$WORK_DIR/deck_gen.js")   # 顶层脚本；落本 Run work，禁止仓库根
3. run_skill_script(
     script = "office-ppt/scripts/run_pptxgen_script.js",
     args   = ["deck_gen.js"],
     inputs = ["$WORK_DIR/deck_gen.js"]
   )
4. runner：node $INPUT_DIR/deck_gen.js（继承已注入的 OUTPUT_DIR 等环境变量）
5. 门禁收集 OUTPUT_DIR → inspect / thumbnail
```

### 2.1 Runner 契约（`run_pptxgen_script.js`）

**默认执行模型 = Anthropic 同款顶层脚本**，不是强制 `module.exports`。

```javascript
// deck_gen.js —— 与 pptxgenjs.md 同构，仅输出路径改用 OUTPUT_DIR
const pptxgen = require("pptxgenjs");
const path = require("path");

const pres = new pptxgen();
pres.layout = "LAYOUT_16x9";
// … 多页 addSlide / addText / addTable …
const out = path.join(process.env.OUTPUT_DIR || ".", "Ultra5_Comparison.pptx");
pres.writeFile({ fileName: out }).then(() => {
  console.log(JSON.stringify({ ok: true, output: "Ultra5_Comparison.pptx" }));
});
```

Runner 职责：

1. `scriptPath = path.join(process.env.INPUT_DIR, path.basename(args[0]))`；不存在则失败 JSON。  
2. 缺 `pptxgenjs` 时输出既有 `dependency_missing`（可在 spawn 前 `require` 探测，或依赖子进程失败再 classify）。  
3. **`child_process.spawnSync(process.execPath, [scriptPath], { env: process.env, encoding: "utf8" })`**（或等价）；把子进程 stdout/stderr 原样汇入；非 0 退出 → `ok:false`。  
4. 每次新进程，避免 `require` 缓存导致改文件不生效。  
5. 不 `eval`；不拼 shell；不把用户路径当 ResourceID。

**可选（非默认）**：若文件导出 `module.exports = async function(ctx)`，runner 可走 in-process 调用并注入 `ctx.pptxgen`——仅作扩展，**SKILL/教程不得以此为默认**，以免与 Anthropic 教程分叉。

### 2.2 `pptxgenjs.md` 最小改写（保持 Agent 主教程）

文首增加 Genesis 调用块（其余 API 教程尽量不动）：

```text
## Genesis 执行方式（替代直接 node / bash）
1. write_file("$WORK_DIR/your_script.js")（禁止写仓库根）
2. run_skill_script(
     script="office-ppt/scripts/run_pptxgen_script.js",
     args=["your_script.js"],
     inputs=["$WORK_DIR/your_script.js"])
3. 最终 .pptx 必须写入 process.env.OUTPUT_DIR
4. 禁止 write_file 假 .pptx；禁止用 create_pptx.js 冒充多页交付
```

示例里的 `writeFile` 目标一律改为 `path.join(process.env.OUTPUT_DIR || ".", "….pptx")`。

### 2.3 SKILL.md Quick Reference

| 任务 | 做法 |
| --- | --- |
| **从零创建** | 读 `pptxgenjs.md` → 写 JS → `run_pptxgen_script.js` |
| 模板编辑 | `editing.md` + unpack/add_slide/clean/pack |
| 单页探活 | `create_pptx.js`（smoke only，非交付默认） |

### 2.4 模板编辑

与 Anthropic 一致；**禁止**把 unpack 路径写成从零生成默认。

---

## 3. 平台配套（与方法正交，必须做）

| 项 | 做法 |
| --- | --- |
| `stageInputs` | 解析顺序：绝对路径 → `$OUTPUT_DIR/` → workspace → 本 Run `OUTPUT_DIR` → `INPUT_DIR` → `WORK_DIR`；失败列出逻辑根 |
| 同 Run | 同一 `runID` 下 `PrepareLocalTask` 目录稳定；远程下载须落回本 Run `output` |
| 中文 | 主路径文案只在 JS 文件内 |

---

## 4. 辅路径与明确不做

| 项 | 态度 |
| --- | --- |
| `create_pptx.js` | 仅 CI/探活；stdout 带 warning；SKILL 不标为从零生成 |
| JSON `create_deck` | **不做默认**；无强需求则不实施，避免再次抢主路径 |
| 裸 `run_command node` | 非产品主路径 |
| 自研 DSL 宣称更优 | 禁止 |
| 阉割 `pptxgenjs.md` | 禁止 |
| 新 `generate_pptx` Tool | 禁止 |
| 改三模式 sandbox | 禁止 |

---

## 5. 实施切片

| 顺序 | 项 |
| --- | --- |
| 1 | 实现 `run_pptxgen_script.js`（spawn `node` + INPUT_DIR 脚本） |
| 2 | 改 SKILL.md / pptxgenjs.md 文首与示例输出路径 / tool 描述 |
| 3 | `stageInputs` 多根解析 + 单测 |
| 4 | `create_pptx.js` 降级 warning + SKILL 去主路径表述 |
| 5 | （默认跳过）JSON deck |

---

## 6. DoD

1. 按 `pptxgenjs.md` 风格写出含中文的多页**顶层** JS → 一次 `run_pptxgen_script` → 合法多页 `.pptx`，门禁通过。  
2. 修改该 JS 后再跑，结果反映新内容（无 require 缓存问题）。  
3. `inputs=["out.pptx"]` 能解析本 Run OUTPUT。  
4. `mode=disabled` 全流程不依赖业务 `run_command`。  
5. 缺 `pptxgenjs` → `dependency_missing`。  
6. SKILL「从零创建」指向写 JS + `run_pptxgen_script`，不指向 `create_pptx.js`。

---

## 7. 方案对照（反思后）

| 方案 | 评价 |
| --- | --- |
| Anthropic：`node foo.js` | 方法正确；入口违 I2 |
| 固定 runner + **顶层脚本**（本文） | 方法对齐 + 入口合规 → **采用** |
| 强制 `run(ctx)` 导出 | 额外协议，教程易分叉 → **不作为默认** |
| 仅 `create_pptx.js` | 能力错误 → 现状根因 |
| JSON deck 默认 | 未证明更优 → **撤回** |

---

## 8. 残余风险（接受）

- 劣质 pptxgen / 视觉问题：与 Anthropic 相同，靠 thumbnail/inspect 迭代。  
- 用户脚本与 Skill 脚本同级执行面：靠 sandbox + 审批，不靠换 DSL。  
- 模型仍可能误写仓库根：靠 SKILL + 提示词 + `write_file` 描述约束；PathResolver 对 `$WORK_DIR` 强制进本 Run。
