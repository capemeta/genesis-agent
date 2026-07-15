# Skill 远程安装设计（URL / GitHub → CLI / Desktop / Enterprise）

> 状态：P0 已实现（2026-07-14）；P1 Desktop / P1.5 私有仓 / P2 Enterprise Import **未完成**  
> 日期：2026-07-14  
> 入口决策：方案 D — **底层统一 Installer；CLI + 对话期工具都暴露**；覆盖 **CLI / Desktop / Enterprise**，统一与独立并重  
> 实现计划：`docs/superpowers/plans/2026-07-14-skill-remote-install-p0.md`  
> 关联：`docs/Skills设计.md` §3.4–3.6 / AllowedSourcePolicy、`docs/内置能力与SkillCreator设计.md` §11.3、`docs/项目目录与边界说明.md`、`docs/superpowers/specs/2026-07-09-skill-tool-protocol-boundary-design.md`  
> 参考实现：Codex `skill-installer`（单 Skill GitHub URL）；Kode `skillMarketplace`（marketplace → plugin）；本仓库已有 `shared/local/skillmarket` + `internal/capabilities/package/marketplace`

### 实现进度总览（对照本文）

图例：`[x]` 已完成 · `[ ]` 未完成 · `[~]` 部分完成

| 项 | 状态 | 说明 |
| --- | --- | --- |
| §3.1 / §4.1 internal 远程 Parser（tree/blob） | `[x]` | `internal/capabilities/package/marketplace/parser` |
| §3.1 / §5 `InstallFromSource` 编排 | `[x]` | `service/install_from_source.go` |
| §3.1 / §4.2 PackageDetector（单 Skill 合成 / 多 Skill 候选） | `[x]` | `service/detect.go` |
| §3.1 contract：`AllowedSourcePolicy` / `CatalogReloader` / InstallFromSource 类型 | `[x]` | Reloader **接口已有**，产品侧 **未注入** → 运行时多为 `effective=next_turn` |
| §3.1 / §6.2 对话工具 `install_skill_from_source` | `[x]` | CLI Profile + bootstrap 已注册 |
| §0.3 / §6.2 `ActionSkillInstall` | `[x]` | `approval/model` |
| §3.2 / §6.1 CLI `skill install <url>` | `[x]` | 含 `--skill-path` / `--allow-url`；**`--json` 未做** |
| §3.2 CLI `AllowGitHub` / `allowed_hosts` 策略 | `[x]` | forge + 下载站共用；URL Content-Type/魔数；重定向受白名单约束 |
| §3.2 Enterprise `DenyAllRemote` 策略桩 | `[x]` | `products/enterprise/internal/skill`；**未**接 Import API / DB Store |
| §4.3 GitHub zip-first（CLI/Desktop Fetcher） | `[x]` | 含下载站直接 zip；跨主机重定向拒绝（同主机或白名单内） |
| §6.1 CLI `--from` 显式 flag | `[ ]` | 位置参数已够用，flag 未加 |
| §6.1 CLI `--json` 结构化错误输出 | `[ ]` | |
| §6.3 Desktop UI「从 URL 安装」+ 确认框 | `[ ]` | P1 |
| §6.3 Desktop bootstrap 注册工具 / Reloader | `[ ]` | P1 |
| §8 `CatalogReloader` hot path（会话内立即可见） | `[ ]` | P1；当前明确 `next_turn` |
| §4.3 / §10 P1.5 私有仓 token / git sparse fallback | `[ ]` | |
| §6.4 Enterprise Import API + pending→published | `[ ]` | P2 |
| §6.4 Enterprise 自有 Fetcher/InstallStore（禁 shared/local） | `[ ]` | P2；目前仅 Policy 桩 |
| §6.4 Enterprise 对话工具按租户开关 | `[ ]` | P2；当前默认不注册（符合设计） |
| §7 Enterprise 审计表 / RBAC / allowlist DB | `[ ]` | P2 |
| §11 全量测试清单（Force/provenance/边界 import 检查等） | `[~]` | Parser / Detect / 工具审批 / policy deny 已有；其余未齐 |

### 审查记录（从第一性原理）

| 轮次 | 发现 | 处置 |
| --- | --- | --- |
| R1 | URL Parser 若只放 `shared/local`，Enterprise 无法复用 → 三产品语义分裂 | **拍板**：纯远程解析进 `internal/.../marketplace`；`dir:`/`file:` 本地解析仅 CLI/Desktop 包装 |
| R1 | 文档自造 `SourcePolicy`，与 `Skills设计.md` 的 `AllowedSourcePolicy` 不一致 | 统一命名为 `AllowedSourcePolicy` |
| R1 | `InstallFromSourceResult` 未含 `effective`，与工具契约矛盾 | 结果结构补齐 |
| R1 | marketplace 根 URL 多 package 时「默认装哪个」未定义 | 写死：单 package 才可默认；否则 `NeedsChoice` |
| R1 | Approval Action 含糊「或复用」 | **新增** `ActionSkillInstall`（RiskHigh），不复用 `ActionCommandExec` |
| R2 | P0 只提 CLI，三产品易再次分叉 | P0 强制：同一 contract + memory fixture 三产品可测；Enterprise 注入 deny-by-default Policy 桩 |
| R2 | Detector 若假设本地绝对路径，Enterprise 难适配 | Detector 只消费 `FetchResult` + 相对路径列举（Fetcher 已物化的 InstallLocation 或内容句柄），编排层不写死家目录 |
| R3 | `allowed_hosts` 含下载站时，带 query 的 API 被误判为 forge `owner/repo` | Parser：query / 非 tree·blob → URL 通道 |
| R3 | URL 通道只认 `.zip` 后缀，openskills 类下载失败 | Fetcher：Content-Type / Disposition / ZIP 魔数 |
| R3 | `http.DefaultClient` 跟随跨域重定向，白名单可被绕过 | `NewFetcherWithHosts` + CheckRedirect（同 Host 或白名单 Hostname） |
| R3 | `github.com` archive URL 302 到 `codeload.github.com` 被误拒 | 白名单放行官方下载子域 / `codeload.<forge>` |

---

## 0. 从第一性原理

### 0.1 真问题

用户说：「下载并安装这个技能 `https://github.com/.../tree/.../skills/foo`」。

系统必须：

1. **正确理解地址**（repo / ref / 子路径；marketplace 包 vs 单 Skill 目录）。
2. **安全拉取并校验**（有 `SKILL.md`、无路径逃逸、可审计 provenance）。
3. **写入产品侧安装状态**，并让后续回合能 `Skill(skill=...)` 加载。
4. **在三产品上体验一致、治理可差异化**：CLI headless、Desktop GUI、Enterprise 多租户审核。

### 0.2 最小必要结果

| 角色 | 最小成功 | 状态 |
| --- | --- | --- |
| 终端用户（CLI） | `genesis skill install <url-or-spec>` 能装 marketplace 包 **或** 单 Skill URL | `[x]` |
| 对话用户（CLI/Desktop） | Agent 调用受控工具完成安装；默认经 Approval | `[x]` CLI；`[ ]` Desktop |
| 企业管理员 | 可配置来源 allowlist；公网 URL 默拒；导入可走 pending → published | `[~]` 仅 deny 桩；Import/allowlist `[ ]` |
| 运行时 | 安装不污染 Engine；Tool 只调 port；不静默扩权 | `[x]` |

### 0.3 硬约束与不变量

| ID | 不变量 |
| --- | --- |
| I1 | Skill 是知识包，不是 Tool；安装 ≠ 授权执行。 |
| I2 | **安装编排在产品无关层**；**下载/写盘/查 DB 在适配层**；Runtime 不直接 `git clone` / HTTP。 |
| I3 | `install_skill_from_source`（装 Skill 本体）≠ `install_skill_dependencies`（装 runtime 包）。 |
| I4 | `internal/...` 不得 import `shared/local` / `products/*`。 |
| I5 | `shared/local/skillmarket` 仅服务 CLI/Desktop 本地主机；**Enterprise 不得依赖它做默认安装路径**。 |
| I6 | 所有远程安装必须写 `SourceProvenance`（type/address/domain/repo/ref/sub_path/hash）。 |
| I7 | 对话安装必须走 ToolGateway + Approval + Audit；不得靠 `run_command` + curl 旁路。 |
| I8 | 安装后 Catalog 可热刷新则刷新；否则明确「下一回合可用」，禁止假装已注入。 |

### 0.4 失败条件

- Agent 用 shell/`run_command` 自行 git clone 到任意目录并当 Skill 用。
- 只支持 marketplace.json，无法装「仅含 SKILL.md 的 GitHub 目录」（Codex 已支持的主路径）。
- 三产品各写一套下载逻辑，URL 语义不一致。
- Enterprise 直接复用 `shared/local` 写宿主机目录，破坏租户隔离。
- 把「装 Skill」和「装 npm/pip」混成一个工具。

### 0.5 非目标（本期）

- 不做完整企业签名验签 UI（预留字段即可）。
- 不做任意 Git 主机的完整协议支持（非 GitHub 的 `git:` 可二期；本期 GitHub zip + 可转换 URL）。
- 不做「装完立即强制 `Skill()` 自动加载」；由用户/Agent 显式加载。
- 不引入 Codex 式 Python 脚本作为主路径（可作对照，不以脚本为契约）。

---

## 1. 现状与差距

| 能力 | 设计时现状 | 当前实现 | 状态 |
| --- | --- | --- | --- |
| Marketplace fetch/install | 已有 | 仍复用 | `[x]` |
| 单 Skill GitHub URL / tree·blob 解析 | 无 | internal parser + Detect 合成 | `[x]` |
| CLI `skill install` 支持 URL | 仅 `package@marketplace` | 已扩展 URL / github / dir | `[x]` |
| 对话期安装 Skill 本体 | 无 | `install_skill_from_source`（CLI） | `[x]` |
| Desktop UI | 未接 | 未做 | `[ ]` |
| Enterprise 治理安装 | 弱 | 仅 DenyAll 策略桩 | `[~]` / P2 `[ ]` |

结论（设计期）：**不是从零造 marketplace**，而是补「源地址解析 + 单 Skill 合成包 + 三产品入口 + 对话工具」。P0 内核与 CLI 入口已齐；Desktop / Enterprise 产品面未齐。

---

## 2. 方案对比与推荐

### 方案 A — 仅 Codex 式 meta-skill + 脚本

- 优点：实现快、与 Codex 行为接近。  
- 缺点：安装旁路 ToolGateway；三产品难统一治理；与现有 marketplace/InstallRecord 双轨。  
- **不采纳为主路径**（可作为对照测试夹具，不进产品契约）。

### 方案 B — 只扩展 marketplace，强制用户提供 marketplace.json

- 优点：模型简单。  
- 缺点：无法满足「给一个 skill 目录 URL」；与 Codex/用户直觉不符。  
- **不采纳为唯一路径**。

### 方案 C（推荐）— 统一 Source Install Pipeline + 双入口 + 三产品适配

```text
SourceResolver  →  ContentFetcher(port)  →  PackageDetector
       →  marketplace.Service.Install*  →  InstallRecord / CapabilityIndex
       →  CatalogRefresh(port)
```

- CLI / Desktop / 对话工具 / Enterprise Admin API **共用同一编排契约**。  
- Fetcher / Store / Policy **按产品注入**。  
- 单 Skill 目录通过 **合成 ephemeral skill-package** 进入现有 Install 管道，避免第二套落盘模型。

**采纳方案 C，入口取 D：CLI + 对话工具。**

---

## 3. 统一与独立：分层矩阵

### 3.1 什么必须统一（一份语义）

| 层 | 位置 | 统一内容 |
| --- | --- | --- |
| 模型 | `internal/capabilities/package/marketplace/model` | `MarketplaceSource`、`SourceProvenance`、`InstallRecord`、InstallScope |
| 编排 | `internal/capabilities/package/marketplace/service` | `InstallFromSource(ctx, req)`：解析 → fetch → detect → install → index |
| 契约 port | `package/marketplace/contract` | `SourceParser`、`Fetcher`、`Installer`、`InstallStore`、`AllowedSourcePolicy`、`CatalogReloader` |
| 纯解析 | `internal/capabilities/package/marketplace/parser`（新建，无 I/O） | GitHub tree/blob、shorthand、`github:`/`git:`/`url:`；**三产品共用** |
| 对话工具 schema | `internal/capabilities/skill/tool/install_skill_from_source` | 工具名、参数、结果 JSON、`failure_kind`；**只调 port** |
| 校验 / Detector | service 内纯函数或同包 `detect` | 必须有 `SKILL.md`；相对路径；禁 `..`/symlink；name 规则；输入为 fetch 根上的相对布局 |

**解析分层（统一 vs 独立的关键钉）：**

```text
internal/.../marketplace/parser   # 远程 URL / github 语义（统一）
        ↑ 包装
shared/local/skillmarket.Parser   # 追加 dir:/file: + os.Stat（仅 CLI/Desktop）
products/enterprise/...           # 复用 internal parser；禁止 dir: 宿主机路径作为用户安装源
```

### 3.2 什么必须独立（产品适配）

| 产品 | 入口 | Fetcher / 持久化 | 策略差异 | Catalog 生效 | 状态 |
| --- | --- | --- | --- | --- | --- |
| **CLI** | `genesis skill install <source>`；对话工具 | `shared/local/skillmarket`：文件 Store + ZIP HTTP；包装 internal parser | `AllowedSourcePolicy`：默认 allow `github.com`；公网非 GH URL 需 `--allow-url` 或 Approval | bootstrap 重载 `InstalledSkillRoots`；同进程尽量热刷新 | `[x]`（热刷新仍多为 next_turn） |
| **Desktop** | GUI「从 URL 安装」；对话工具 | **复用同一** `shared/local/skillmarket`（禁止再写一套 fetcher） | 同 CLI 策略 + UI 强制确认；scope 选 user/project | UI 刷新 Installed + `CatalogReloader` | `[ ]` P1 |
| **Enterprise** | Admin Import API / 技能广场；对话工具默认关 | **`products/enterprise/internal/skill`**：实现同一 `Fetcher`/`InstallStore` contract；DB + 受控存储；**禁止 import shared/local/skillmarket** | deny-by-default allowlist；公网 URL 默拒；`pending_review` → `published` | 仅 published + 租户绑定；查询带 `tenant_id` | `[~]` 仅 Policy 桩；其余 P2 `[ ]` |

### 3.3 依赖方向（不可违反）

```text
cmd/<product>
  → products/<product>/bootstrap
       → internal/capabilities/package/marketplace/service   (编排)
       → internal/capabilities/skill/tool/...               (对话工具，仅 schema+port 调用)
       → shared/local/skillmarket                           (仅 CLI/Desktop)
       → products/enterprise/internal/skill/*               (仅 Enterprise)
```

- `internal` 永不感知 Wails / PostgreSQL / 本地绝对路径策略细节。  
- Desktop **不要**再复制一份 fetcher；与 CLI 共享 `shared/local`。  
- Enterprise **不要** import `shared/local/skillmarket`；实现自己的 `Fetcher`/`InstallStore` 满足同一 contract。

### 3.4 产品 Persistence Profile（已有概念落地）

沿用 `ProductCapabilityProtocol`：

| Product | Driver | DefaultScope | AllowedSourcePolicy 默认 |
| --- | --- | --- | --- |
| cli | file | user | allow `github:github.com/*`；deny `url:*` unless `--allow-url` / Approval |
| desktop | file | user | 同 CLI；每次远程安装 UI confirm（即便 github） |
| enterprise | postgres（目标） | tenant/project | deny all remote；仅 allowlist 命中可 Import；对话工具默认不注册 |

---

## 4. 源地址与包形态

### 4.1 输入归一化（扩展现有 Parser）

支持并**保留子路径**（对齐 Codex，修当前丢 path 的 bug）：

| 输入 | 解析结果 |
| --- | --- |
| `https://github.com/org/repo/tree/main/skills/foo` | github, repo=`org/repo`, ref=`main`, sub_path=`skills/foo` |
| `https://github.com/org/repo/blob/main/skills/foo/SKILL.md` | 归一到目录 `skills/foo` |
| `github:org/repo@v1#skills/foo` | 同上 |
| `org/repo@v1#skills/foo` | 同上 |
| `https://example.com/pack.zip` | url（策略可能拒绝） |
| `dir:D:\skills\foo` | directory（仅 CLI/Desktop） |
| `package@marketplace` | **非 Source 安装**；走现有 `Install(spec)` |

### 4.2 PackageDetector（拉取后）

对 fetch 落盘根（或 sub_path 根）按优先级检测：

1. 存在 `.genesis/marketplace.json` / `.claude-plugin/marketplace.json` / `.kode-plugin/marketplace.json` → **标准 marketplace**：  
   - 请求带 `package` / 可唯一解析的 package 名 → 装该包；  
   - **仅当 manifest 中 skill-package 恰好 1 个**时可默认安装；  
   - 多个 package → `NeedsChoice=true`，列出 `package@marketplace` 候选，**禁止静默全装**。  
2. 存在 plugin manifest → **不**由 `skill install` 静默当 skill 装完；返回明确错误，指引 `plugin install`（避免 skill/plugin 入口混淆）。  
3. 根目录或指定 `skill_path` 含 `SKILL.md` → **单 Skill**：合成

```json
{
  "name": "github-<owner>-<repo>",
  "packages": [{
    "name": "<skill-name>",
    "type": "skill-package",
    "source": "./",
    "capabilities": [{ "type": "skill", "name": "<skill-name>", "path": "./" }]
  }]
}
```

合成 marketplace `name` 固定规则：`github-<owner>-<repo>`（非法字符替换为 `-`）；同 repo 多次安装不同 skill 时靠 package/skill 名区分，InstallRecord.spec 仍为 `package@marketplace`。

4. 目录下多个子目录各含 `SKILL.md` 且无 marketplace → **多 Skill 提示**：`NeedsChoice` + `Candidates`（相对路径）；须 `skill_path` / `SkillPaths`；禁止静默全装。Enterprise 批量仅在策略显式允许且 Approval 通过时启用。

Skill 名：优先 frontmatter `name`，否则目录 basename；须符合现有 name 规则；与目录名不一致则安装失败（与现有校验一致）。

### 4.3 拉取策略（Fetcher 实现差异，语义统一）

| 步骤 | CLI/Desktop（shared/local） | Enterprise |
| --- | --- | --- |
| GitHub | `codeload.github.com/.../zip/ref`（现有）；401/403/404 可二期加 git sparse（对齐 Codex auto） | 同源协议或企业代理；凭证走企业 secret，不进 Tool 参数 |
| 校验 | zip 防穿越、禁 symlink、大小上限（现有 64MiB 可配置） | 同规则 + 企业配额 |
| 缓存 | `~/.genesis-agent/<product>/marketplaces` | 租户级 object key / DB blob 元数据 |

本期 CLI/Desktop：**zip-first**；git fallback 标为 Phase 1.5（私有仓）。

---

## 5. 编排 API（产品无关）

在 `marketservice.Service` 增加：

```go
type InstallFromSourceRequest struct {
    SourceInput string                 // URL / github: / dir: / 或已是 package@marketplace
    Scope       marketmodel.InstallScope
    Force       bool
    Package     string                 // marketplace 多包时指定
    SkillPath   string                 // 多 skill 目录时指定
    SkillPaths  []string               // 显式批量（需策略允许）
    AllowURL    bool                   // CLI 非交互；对话路径由 Approval 替代
    Product     string                 // cli|desktop|enterprise，供 AllowedSourcePolicy
}

type InstallFromSourceResult struct {
    Records     []marketmodel.InstallRecord
    Skills      []string
    Specs       []string
    NeedsChoice bool
    Candidates  []string               // skill 相对路径或 package@marketplace
    Effective   string                 // "hot" | "next_turn"
    Message     string
    FailureKind string                 // 失败时：policy_denied|needs_choice|validation_failed|...
}
```

流程：

1. `AllowedSourcePolicy.Check(ctx, source, product)` — 拒绝 → `failure_kind=policy_denied`。  
2. 若输入是 `pkg@market` → 委托现有 `Install`（不经过远程 fetch）。  
3. `SourceParser.Parse` → `Fetcher.Fetch`（必须尊重 `ref`/`sub_path`）。  
4. `Detect`（基于 FetchResult.InstallLocation 的相对布局）→ 合成或读 manifest；多候选则提前返回 `NeedsChoice`，不写盘安装。  
5. `Installer.Install` + `InstallStore` + CapabilityIndex。  
6. 写满 `SourceProvenance`（address 规范化为可审计字符串）。  
7. `CatalogReloader.Reload(ctx)`：实现成功 → `Effective=hot`；未注入或失败 → `Effective=next_turn`（仍算安装成功，不得伪装已注入当前 Catalog）。

---

## 6. 双入口设计

### 6.1 CLI

扩展现有命令，**不另造平行命令体系**：

```powershell
# 兼容旧：
genesis skill install <package>@<marketplace> [--scope user|project] [--force] [--json]

# 新增同源：
genesis skill install <url-or-github-source> [--scope user|project] [--force] [--skill-path <rel>] [--allow-url] [--json]
genesis skill install --from <source> ...   # 可选显式 flag，与位置参数等价
```

行为：

- 解析失败 / 多候选 / 策略拒绝 → 非 0 退出；`--json` 输出 `failure_kind`。  
- 与 `plugin install` 区分：skill 命令只装 skill-package（或合成 skill-package）；组合 plugin 仍走 `plugin install`。

### 6.2 对话工具：`install_skill_from_source`

| 项 | 约定 |
| --- | --- |
| 工具名 | `install_skill_from_source`（固定；进 Profile） |
| 参数 | `source`（必填）、`scope`（可选 user/project）、`skill_path`、`force`、`reason` |
| 实现位置 | `internal/capabilities/skill/tool/install_skill_from_source` |
| 依赖 | `InstallFromSource` 编排接口由 **bootstrap 注入**（通常即 marketplace `Service`） |
| 审批 | **新增** `approvalmodel.ActionSkillInstall`，`RiskHigh`；Resource metadata：`domain`/`repo`/`ref`/`sub_path`/`address`；**禁止**复用 `ActionCommandExec`（避免与装依赖命令混淆） |
| 成功结果 | `{ ok, skills[], specs[], effective: "hot"\|"next_turn", provenance }` |
| 失败 | `{ ok:false, failure_kind, message, candidates? }` |

**禁止**：工具内 HTTP；禁止建议模型用 `run_command` 安装。  
System 短规则可加 1 行：用户给 Skill URL 时调用本工具，勿 shell 下载。

与 Codex 差异：Codex 用 meta-skill+脚本；Genesis 用 **一等 Tool + 统一 InstallRecord**，更利于 Enterprise 审计。

### 6.3 Desktop

- UI 调用与 CLI **同一** `InstallFromSource`（via bootstrap service），不直连 GitHub。  
- 确认对话框展示：source address、将安装的 skill 名、dependencies 摘要、scope。  
- 对话工具与 CLI 共用；Approval 走桌面确认通道。

### 6.4 Enterprise

- **管理面**：`POST /api/.../skills/import`（示意）body=`source` → 创建 `pending_review` 记录，不立即进用户 Catalog。  
- **运行时 Source**：只读 `published` + 绑定。  
- **对话工具**：默认 Profile **关闭**；租户策略开启时仍要 Approval，且 source 必须命中 allowlist。  
- 普通用户「安装」若指向未发布来源 → 拒绝或转「申请导入」工单（产品策略，编排返回明确 `failure_kind=governance_blocked`）。

---

## 7. 安全与治理

| 控制点 | CLI/Desktop | Enterprise |
| --- | --- | --- |
| 来源策略 | `AllowedSourcePolicy`：默认 allow github.com；deny 任意 url | allowlist repo/host；deny url；默认 deny 全部直至管理员配置 |
| 内容校验 | SKILL.md、路径、symlink、大小 | 同左 + 许可证/敏感声明 warning |
| 权限 | 安装不扩大 Profile；`allowed-tools` 仍求交 | 同左 + RBAC 谁可 import/publish |
| 审计 | 本地 install 日志 + Trace | 审计表含 tenant_id、actor、provenance、approval_id |
| 冲突 | 同名 skill：拒绝或要求 `--force` 覆盖同 spec | 租户内唯一；跨租户隔离 |

`install_skill_dependencies` 仍只在「Skill 已存在于 Catalog」后使用；远程安装成功不自动装 npm/pip。

---

## 8. 生效与热更新

| 模式 | 行为 |
| --- | --- |
| hot | bootstrap 已注入 `CatalogReloader`：更新 Skill Source roots / Capability adapters，当前会话 Catalog 立即可见 |
| next_turn | 无法热更新时：工具结果明确告知；下一用户回合重建 container 后可见（对齐 Codex 文案） |

Desktop/Enterprise UI 在 hot 成功后刷新列表；CLI TUI 若有会话内 registry，走同一 Reloader。

---

## 9. 目录落点（实现时）

| 路径 | 职责 | 状态 |
| --- | --- | --- |
| `internal/capabilities/package/marketplace/parser` | **三产品共用**远程 Source 解析（tree/blob） | `[x]` |
| `internal/capabilities/package/marketplace/service` | `InstallFromSource` 编排 + Detector | `[x]` |
| `internal/capabilities/package/marketplace/contract` | `AllowedSourcePolicy`、`CatalogReloader`、既有 Fetcher/Installer/Store | `[x]` 类型；Reloader 实现 `[ ]` |
| `internal/capabilities/package/marketplace/policy` | `AllowGitHub` / `DenyAllRemote` | `[x]` |
| `internal/capabilities/skill/tool/install_skill_from_source` | 对话工具（无 HTTP） | `[x]` |
| `internal/.../approval`（或既有 approval model） | 新增 `ActionSkillInstall` | `[x]` |
| `shared/local/skillmarket` | 包装 parser（+dir/file）、Fetcher、文件 Store（**仅 CLI/Desktop**） | `[x]`（Fetcher 支持无 manifest） |
| `products/cli/internal/command/skill_cmd.go` | CLI 参数扩展 | `[x]`（缺 `--json` / `--from`） |
| `products/cli/bootstrap` | 注入 local 适配 + 工具 + 默认 Policy | `[x]`（Reloader 未注） |
| `products/desktop/bootstrap` | 注入 local 适配 + 工具 + Reloader + 默认 Policy | `[ ]` P1 |
| `products/enterprise/internal/skill` | 企业 Fetcher/Store、Import API、治理状态机、租户 Policy | `[~]` 仅 Policy 桩 |
| `products/enterprise/bootstrap` | 注入企业实现；**默认不注册**对话安装工具 | `[~]` 未注册工具符合设计；未注入完整安装栈 |

**不**把远程安装塞进 `internal/capabilities/skill/service` 主加载路径；保持「管理安装」与「运行加载」分离。

---

## 10. 分期

| 阶段 | 交付 | 验收 | 状态 |
| --- | --- | --- | --- |
| **P0** | internal parser（tree/blob）；Detector 单 Skill 合成；`InstallFromSource`；`ActionSkillInstall`；CLI `skill install <url>`；CLI 注册对话工具；**contract + 本地 dir fixture 单测**；Enterprise deny-all Policy 桩 | 公开 GitHub skill 目录 URL：CLI 与对话可装；Enterprise 桩策略拒绝未 allow 来源 | `[x]` 已完成（2026-07-14） |
| **P1** | Desktop UI 入口 + 确认框；`CatalogReloader` hot path；多 skill / 多 package 候选交互打磨；CLI `--json`（可选） | Desktop 与 CLI 同一 source 安装结果一致 | `[ ]` 未完成 |
| **P1.5** | 私有仓：token / git sparse fallback（仍在 Fetcher 适配层） | 私有 repo 可装 | `[ ]` 未完成 |
| **P2** | Enterprise Import API + pending/published + allowlist DB；对话工具按租户开关 | 未审核技能不可被普通用户 `Skill()` 加载 | `[ ]` 未完成 |

---

## 11. 测试要点

- Parser：`/tree/`、`/blob/.../SKILL.md`、shorthand、非法 `..`。  
- Detector：marketplace / 单 skill / 多 skill 候选。  
- Install：Force、已存在冲突、provenance 字段完整。  
- 工具：Approval deny；策略 deny URL；与 `install_skill_dependencies` 参数隔离。  
- 边界：`internal` 包导入检查不含 `shared/local`；enterprise 包导入检查不含 `shared/local/skillmarket`。  
- 三产品契约测试：同一 `InstallFromSourceRequest` fixture，分别用 memory fetcher 跑 service（不依赖真网）。

---

## 12. 与既有文档对齐

- 补充而非替代 `Skills设计.md` §3.4：增加「单 Skill URL 合成 package」与对话工具。  
- 遵守「第三方安装属产品管理能力」：编排在 marketplace service，下载在适配层，**对话只是管理能力的受控入口**，不是 Runtime 自研安装器。  
- 与 Skill 协议边界一致：新工具是 Tool 名；skill 名仍不得进 function schema。

---

## 13. 残余风险（接受）

1. 无 git fallback 时，部分私有仓/非常规 ref 装失败 — P1.5 处理。  
2. 热更新在部分长会话策略下仍需 next_turn — `Effective` 字段显式化，不算安装失败。  
3. Enterprise 对象存储未就绪前，可用「管理员 CI 预装 + DB 元数据」过渡，但不得临时改用 `shared/local`。  
4. Desktop 产品代码面若尚未有 skill UI 壳，P1 可先接 bootstrap service API，UI 后补；契约不因 UI 延期而分叉。
