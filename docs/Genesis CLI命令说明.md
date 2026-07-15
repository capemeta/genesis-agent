# Genesis CLI 命令说明

本文记录当前仓库已实现或已暴露的 Genesis 命令，并说明 CLI / Desktop / Enterprise 的支持状态。

## 一、启动方式

### `start.bat`

`start.bat` 是 Windows 本地启动脚本，不是独立产品命令。

| 用法 | 说明 |
| --- | --- |
| `start.bat` | 不带参数时启动 `genesis-cli chat`。 |
| `start.bat <args...>` | 带参数时把参数原样转发给 `genesis-cli`。 |

示例：

```bat
start.bat
start.bat chat
start.bat run "你好"
start.bat skill create report-skill --evals
start.bat skill marketplace add dir:D:\skills-market
start.bat skill install document-skills@anthropic-agent-skills
```

脚本会设置仓库内 Go 缓存目录：`GOCACHE=.gocache`、`GOMODCACHE=.gomodcache`、`GOTMPDIR=.gotmp`。如果存在 `start.local.bat`，会先加载它，适合放本机 API Key 或临时环境变量。

## 二、产品支持矩阵

| 产品 | 当前入口 | 命令支持 | 说明 |
| --- | --- | --- | --- |
| CLI | `cmd/genesis-cli` / `start.bat` | 支持本文列出的 CLI 命令 | 当前主要可用产品入口。 |
| Desktop | `cmd/genesis-desktop` | 暂不支持 | 当前返回“暂未实现”，没有 Skill 安装 UI。 |
| Enterprise | `cmd/genesis-enterprise` | 只支持 HTTP API server 启动参数 | 不支持 CLI 的 `skill create/install` 等子命令；企业 Skill DB/UI/发布治理尚未实现。 |

## 三、全局 CLI 参数

所有 `genesis-cli` 子命令都支持：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--config, -c <dir>` | `configs` | 配置目录，包含 `config.yaml`、`llm.yaml`，以及可选的 `mcp.yaml`、`hooks.yaml`、`config.local.yaml`；当前默认值相对进程工作目录。 |
| `--sandbox <mode>` | `disabled` | 命令执行沙箱策略：`disabled`、`optional`、`required`。 |
| `--help, -h` | 无 | 显示帮助。 |

示例：

```bat
start.bat --config configs chat
start.bat --sandbox required run "执行测试"
```

`start.bat` 会先切换到脚本所在目录，因此源码开发期的默认 `configs` 会解析为仓库同级目录。直接运行已打包的 `genesis-cli.exe` 时，当前实现仍以启动时工作目录为准；需要通过 `--config <目录>` 显式指定。安装包自动按 exe 同级 `configs/` 定位属于既定发行目标，尚待加载器实现，详见 `docs/产品分发架构设计.md` 的“安装包配置分发契约”。

## 四、基础命令

### `chat`

启动交互式 TUI 对话。

```bat
start.bat chat
```

| 参数 | 说明 |
| --- | --- |
| 无专属参数 | 只支持全局参数。 |

不带参数运行 `start.bat` 时，等价于 `start.bat chat`。

### `run <消息>`

执行一次非交互式 Agent 推理，适合脚本调用。

```bat
start.bat run "总结这个项目"
start.bat run --json "现在有哪些工具？"
start.bat run --quiet "只输出最终答案"
```

| 参数 | 说明 |
| --- | --- |
| `<消息>` | 必填，要发送给 Agent 的用户输入。 |
| `--json` | 输出 JSON，适合脚本解析。JSON 模式下错误也会进入 JSON 输出。 |
| `--quiet, -q` | 只输出最终回答文本，适合重定向或管道。 |

### `help [command]`

显示命令帮助。也可以给任意命令加 `--help`。

```bat
start.bat help
start.bat help skill
start.bat skill create --help
```

| 参数 | 说明 |
| --- | --- |
| `[command]` | 可选，要查看帮助的命令路径。 |

### `completion <shell>`

生成 shell 自动补全脚本。这是 Cobra 默认命令。

```bat
start.bat completion powershell
start.bat completion bash
```

| 参数 | 说明 |
| --- | --- |
| `<shell>` | 必填，支持 `bash`、`zsh`、`fish`、`powershell`。 |

### `config`

查看或校验当前配置。

```bat
start.bat config
start.bat config --validate
```

| 参数 | 说明 |
| --- | --- |
| `--validate` | 仅校验配置是否有效，不打印配置内容，适合 CI。 |

### `configure`

交互式配置本机密钥，保存到 `~/.genesis-agent/cli/config.yaml`。

```bat
start.bat configure
```

| 参数 | 说明 |
| --- | --- |
| 无专属参数 | 交互式询问 Qwen、搜索服务 API Key、可选 Skill 根目录和 DPAPI 加密设置。 |

### `tools`

列出当前 Agent 已注册工具。

```bat
start.bat tools
```

| 参数 | 说明 |
| --- | --- |
| 无专属参数 | 只支持全局参数。 |

### `version`

显示版本信息。

```bat
start.bat version
start.bat version --verbose
```

| 参数 | 说明 |
| --- | --- |
| `--verbose, -v` | 显示 Git commit、构建时间、Go 版本、OS/Arch。 |

## 五、Skill 创建、校验和打包

当前没有独立的 `create-skill` 命令；创建 Skill 的实际命令是：

```bat
start.bat skill create <name>
```

`skill-creator` 是内置给 Agent 读取的系统 Skill，不需要安装，也不会出现在 `skill list` 中。

### `skill create <name>`

创建 Genesis Skill 脚手架。

```bat
start.bat skill create office-report --description "Use this skill when creating Office reports."
start.bat skill create office-report --path .genesis\skills --resources references,scripts --evals
```

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `<name>` | 必填 | Skill 名称。会规范化为小写 kebab-case，例如 `Office Report` 变成 `office-report`。 |
| `--path <dir>` | `.genesis/skills` | Skill 创建根目录。最终目录是 `<path>/<name>`。 |
| `--description <text>` | 自动生成 | 写入 `SKILL.md` frontmatter 的 `description`。这是模型判断何时加载 Skill 的关键触发描述。 |
| `--author <name>` | `Genesis` | 写入 `metadata.author`。 |
| `--resources <list>` | 空 | 创建资源目录，支持 `references,scripts,assets`，逗号分隔。不会自动写内容。 |
| `--evals` | 关闭 | 创建 `evals/evals.json` 初稿。用于本地评测样例，不会影响 Skill 运行，也不会被 `skill package` 打包。 |
| `--force` | 关闭 | 覆盖已存在 Skill 目录；会拒绝覆盖根目录、当前目录、用户 home 等高风险路径。 |

`--evals` 加或不加的区别：

| 是否加 `--evals` | 生成内容 | 适合场景 | 后续影响 |
| --- | --- | --- | --- |
| 不加 | 只生成 `SKILL.md`，以及你通过 `--resources` 指定的目录。 | 先快速写一个个人草稿。 | `skill validate` 不会检查 eval 文件；不影响安装和加载。 |
| 加 | 额外生成 `evals/evals.json`，内含一个可替换的评测样例。 | 想沉淀团队复用 Skill，或希望以后验证 Skill 效果。 | `skill validate` 和 `skill eval validate` 会检查 eval schema；`skill package` 默认排除根目录 `evals/`。 |

简单说：`--evals` 是“创建评测样例文件”的开关，不是安装开关，也不是启用 Skill 的开关。

### `skill validate <skill-dir>`

校验本地 Skill 目录。

```bat
start.bat skill validate .genesis\skills\office-report
```

| 参数 | 说明 |
| --- | --- |
| `<skill-dir>` | 必填，包含 `SKILL.md` 的 Skill 目录。 |

校验内容包括 `SKILL.md`、frontmatter、资源引用、脚本风险、密钥/绝对路径风险，以及存在时的 `evals/evals.json`。

### `skill package <skill-dir>`

把本地 Skill 打成可安装的 Genesis marketplace 目录包。

```bat
start.bat skill package .genesis\skills\office-report
start.bat skill package .genesis\skills\office-report --out dist\office-report --version 0.1.0 --force
```

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `<skill-dir>` | 必填 | 要打包的本地 Skill 目录。 |
| `--out <dir>` | `dist/<package>` | 输出目录。 |
| `--marketplace <name>` | `<package>-marketplace` | 生成的 marketplace 名称。 |
| `--package <name>` | Skill 名称 | 生成的 package 名称。安装时使用这个名称。 |
| `--version <ver>` | `0.1.0` | package version。 |
| `--force` | 关闭 | 覆盖输出目录；会拒绝高风险覆盖路径。 |

输出结构：

```text
<out>/
  .genesis/marketplace.json
  skills/<skill-name>/SKILL.md
```

根目录 `evals/` 不会被打包进 marketplace 包；它用于本地开发和评测。`skill package` 会检查 `skill-card.md`，缺失或不完整时输出发布治理 warning，但当前不阻断打包。

### `skill card generate <skill-dir>`

生成 `skill-card.md` 发布治理卡片。它不替代 `SKILL.md`，主要记录 owner、license、部署地域、依赖、风险缓解、输出、版本和伦理/合规说明。

```bat
start.bat skill card generate .genesis\skills\office-report --owner "Team AI" --license Apache-2.0
start.bat skill card generate .genesis\skills\office-report --version 0.2.0 --force
```

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `<skill-dir>` | 必填 | 包含 `SKILL.md` 的 Skill 目录。 |
| `--owner <name>` | `TODO` | 维护人或团队。 |
| `--license <text>` | `TODO` | 许可证或使用条款。 |
| `--version <ver>` | Skill version 或 `0.1.0` | 写入 Skill Card 的版本。 |
| `--force` | 关闭 | 覆盖已存在的 `skill-card.md`。 |

### `skill card validate <skill-dir>`

校验 `skill-card.md` 是否包含发布治理需要的核心章节。

```bat
start.bat skill card validate .genesis\skills\office-report
```

| 参数 | 说明 |
| --- | --- |
| `<skill-dir>` | 必填，包含 `skill-card.md` 的 Skill 目录。 |

缺少 `skill-card.md` 当前只输出 warning，不会失败；个人草稿可以忽略。团队、marketplace 或企业发布应补齐，企业策略后续可以把缺失卡片升级为阻断。

## 六、Skill Eval 命令

### `skill eval validate <skill-dir>`

单独校验 `evals/evals.json`。

```bat
start.bat skill eval validate .genesis\skills\office-report
```

| 参数 | 说明 |
| --- | --- |
| `<skill-dir>` | 必填，包含 `evals/evals.json` 的 Skill 目录。 |

此命令只检查 eval 文件，不运行模型、不生成结果。

### `skill eval validate-run <run-dir>`

校验 eval run 产物契约。

```bat
start.bat skill eval validate-run .genesis\eval-runs\office-report\run-001
```

| 参数 | 说明 |
| --- | --- |
| `<run-dir>` | 必填，包含 eval run 产物的目录。 |

会检查：

- `grading.json`
- `outputs/metrics.json`
- `timing.json`

当前只校验产物，不负责运行 eval。

## 七、Skill Marketplace 命令

### `skill marketplace add <source>`

添加 marketplace 来源。

```bat
start.bat skill marketplace add dir:D:\skills-market
start.bat skill marketplace add file:D:\skills-market\.genesis\marketplace.json
start.bat skill marketplace add github:owner/repo@main#skills
start.bat skill marketplace add https://example.com/skills.zip
```

| 参数 | 说明 |
| --- | --- |
| `<source>` | 必填，marketplace 来源。 |

支持的 source 形式：

| 形式 | 示例 | 说明 |
| --- | --- | --- |
| `dir:<path>` | `dir:D:\skills-market` | 本地 marketplace 目录。 |
| `file:<path>` | `file:D:\marketplace.json` | 本地 marketplace manifest 文件。 |
| `github:<owner/repo>` | `github:anthropics/skills@main#skills` | GitHub 仓库，可带 ref 和子目录。 |
| `git:<url>` | `git:https://github.com/owner/repo.git@main#path` | 当前会尽量转换为 GitHub zip 下载。 |
| `url:<url>` | `url:https://example.com/marketplace.json` | 远程 `.json` 或 `.zip`。 |
| 直接路径或 URL | `D:\skills-market` | 自动识别目录、文件、GitHub shorthand 或 URL。 |

### `skill marketplace list`

列出已添加的 marketplace 来源。

```bat
start.bat skill marketplace list
```

| 参数 | 说明 |
| --- | --- |
| 无专属参数 | 只支持全局参数。 |

### `skill marketplace update <name>`

刷新 marketplace cache。

```bat
start.bat skill marketplace update anthropic-agent-skills
```

| 参数 | 说明 |
| --- | --- |
| `<name>` | 必填，marketplace 名称。 |

### `skill marketplace remove <name>`

移除 marketplace 来源。

```bat
start.bat skill marketplace remove anthropic-agent-skills
```

| 参数 | 说明 |
| --- | --- |
| `<name>` | 必填，marketplace 名称。 |

## 八、Skill 安装和管理命令

### `skill list`

列出 marketplace 中可安装的 Skill package。

```bat
start.bat skill list
```

| 参数 | 说明 |
| --- | --- |
| 无专属参数 | 如果没有添加 marketplace，输出为空是正常的。 |

注意：`skill list` 不列出内置 system skill，也不列出本地草稿目录。内置 `skill-creator` 不会出现在这里。

### `skill search <query>`

搜索 marketplace。

```bat
start.bat skill search document
```

| 参数 | 说明 |
| --- | --- |
| `<query>` | 必填，按 package 名称、描述、marketplace 名称搜索。 |

### `skill show <package[@marketplace]>`

查看 package 详情。

```bat
start.bat skill show document-skills@anthropic-agent-skills
```

| 参数 | 说明 |
| --- | --- |
| `<package[@marketplace]>` | 必填。若 package 名称在多个 marketplace 中重复，必须带 `@marketplace`。 |

### `skill install <package[@marketplace]>`

安装技能包。

```bat
start.bat skill install document-skills@anthropic-agent-skills
start.bat skill install office-report --scope project
start.bat skill install office-report --scope user --force
```

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `<package[@marketplace]>` | 必填 | 要安装的 package。名称有歧义时必须写 `package@marketplace`。 |
| `--scope <scope>` | `user` | 安装范围：`user` 或 `project`。 |
| `--force` | 关闭 | 已安装时覆盖重装。 |

安装来源会记录 source provenance，包括来源类型、地址、域名、ref、subpath、package_source、content_hash 等。

### `skill installed`

列出已安装技能包。

```bat
start.bat skill installed
```

| 参数 | 说明 |
| --- | --- |
| 无专属参数 | 输出已安装 package、scope、启用状态和包含的 skills。 |

### `skill enable <package@marketplace>`

启用已安装技能包。

```bat
start.bat skill enable document-skills@anthropic-agent-skills
```

| 参数 | 说明 |
| --- | --- |
| `<package@marketplace>` | 必填，必须写完整 spec。 |

### `skill disable <package@marketplace>`

禁用已安装技能包。

```bat
start.bat skill disable document-skills@anthropic-agent-skills
```

| 参数 | 说明 |
| --- | --- |
| `<package@marketplace>` | 必填，必须写完整 spec。 |

### `skill uninstall <package@marketplace>`

卸载技能包。

```bat
start.bat skill uninstall document-skills@anthropic-agent-skills
```

| 参数 | 说明 |
| --- | --- |
| `<package@marketplace>` | 必填，必须写完整 spec。 |

## 九、常见流程

### 创建并本地校验一个 Skill

```bat
start.bat skill create office-report --resources references,scripts --evals
start.bat skill validate .genesis\skills\office-report
start.bat skill eval validate .genesis\skills\office-report
start.bat skill card generate .genesis\skills\office-report --owner "Team AI" --license Apache-2.0
start.bat skill card validate .genesis\skills\office-report
```

### 打包成本地 marketplace 并安装

```bat
start.bat skill package .genesis\skills\office-report --out dist\office-report --force
start.bat skill marketplace add dir:%CD%\dist\office-report
start.bat skill list
start.bat skill install office-report@office-report-marketplace --scope project
```

### 从外部 marketplace 安装

```bat
start.bat skill marketplace add github:owner/repo@main#path-to-marketplace
start.bat skill list
start.bat skill install package-name@marketplace-name
```

## 十、Enterprise 命令

Enterprise 当前入口是 HTTP API server，不共享 CLI 的 Skill 管理子命令。

```bat
go run cmd\genesis-enterprise\main.go --config configs --host 0.0.0.0 --port 8080
```

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--config, -c <dir>` | `configs` | 配置目录，包含 `config.yaml`。 |
| `--host <host>` | 服务默认配置 | 监听地址。 |
| `--port, -p <port>` | `8080` | 监听端口。 |

当前 Enterprise HTTP API 说明在命令 help 中声明，包括 `/v1/runs`、`/v1/tools`、`/health`、`/readiness` 等；Skill 的企业 DB、租户发布、审批、签名、UI 管理尚未实现。

## 十一、Desktop 命令

Desktop 当前入口仍是占位：

```bat
go run cmd\genesis-desktop\main.go
```

| 参数 | 说明 |
| --- | --- |
| 无可用业务参数 | 当前会加载 desktop 配置后返回“暂未实现”。 |

Desktop 暂不支持本文 CLI 命令，也没有 Skill 安装 UI。





