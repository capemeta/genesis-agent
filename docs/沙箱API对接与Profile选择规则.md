# 沙箱 API 对接与 Profile 选择规则

## 1. 定位

本文定义 `genesis-agent` 调用独立 `genesis-sandbox` 服务时的对接边界、SDK/HTTP 选择、Runtime Profile 选择规则，以及代码执行、Skill 脚本、Office、浏览器等能力的默认映射。

本文不描述 `genesis-sandbox` 服务内部实现。服务端 API、参数和部署细节以 `D:\workspace\go\genesis-sandbox\docs\客户端调用-SDK-MCP-部署运维指南.md` 和 `D:\workspace\go\genesis-sandbox\api\openapi.yaml` 为准；本仓库只保留产品无关 contract、adapter 和产品 bootstrap 注入。

## 2. 边界原则

- `shared/local/sandbox` 只做 CLI/Desktop 本地主机平台沙箱：macOS Seatbelt、Linux bubblewrap/Landlock、Windows process constrained。
- `genesis-sandbox` API client 放在 `internal/capabilities/sandbox/adapter/http` 或后续等价 adapter；不得放进 `shared/local`，也不得放进 `products/enterprise` 私有目录。
- CLI/Desktop/Enterprise 只在 bootstrap 注入 endpoint、credential、workspace、租户/用户上下文和默认执行策略。
- 文件工具仍依赖 `FileSystemBackend`；命令工具仍依赖 `ExecutionRunner` / `SandboxRunner`；Skill 只声明资源和脚本，不直接越过 ToolGateway 调用宿主机命令。
- Docker/genesis-sandbox 模式下，工具和 UI 只能使用 workspace-relative path、sandbox path 或 resource id，不展示宿主机绝对路径。
- `SandboxRequired` 不满足时必须失败；`SandboxOptional` 降级必须产生 warning/trace/audit，不能静默退回无沙箱。

## 3. 开关与配置优先级

外部 `genesis-sandbox` 必须是可关闭、可开启、可按会话覆盖的能力。Codex 类本地执行可以完全不依赖 Docker 或远程 sandbox，因此 Genesis 的默认值必须保持外部 sandbox 关闭。

顶层运行时配置使用 `sandbox`，不要把 endpoint/key 写进 `policy.sandbox`：

```yaml
sandbox:
  enabled: false
  mode: local_host
  default_execution: disabled
  allow_session_override: true
  base_url: ${GENESIS_SANDBOX_BASE_URL}
  api_key: ${GENESIS_SANDBOX_API_KEY}
  api_key_env: GENESIS_SANDBOX_API_KEY
  workspace_id: ""
  default_runtime_profile: code-polyglot-basic
  timeout: 60s
```

字段语义：

| 字段 | 说明 |
| --- | --- |
| `enabled` | 是否启用外部 sandbox API。默认 `false`，表示完全可以不使用 Docker/genesis-sandbox。 |
| `mode` | `local_host`、`local_platform_sandbox`、`docker_sandbox`、`remote_sandbox`。只有后两者走 genesis-sandbox API。 |
| `default_execution` | 全局默认执行策略：`disabled`、`optional`、`required`。 |
| `allow_session_override` | 是否允许每次对话或 HTTP run 请求覆盖 sandbox 策略。企业可按租户/角色关闭。 |
| `base_url` | genesis-sandbox 服务地址，例如 `http://127.0.0.1:18010`。 |
| `api_key` / `api_key_env` | Bearer Token 来源。生产优先 `api_key_env`，不把明文 key 写进仓库配置。 |
| `workspace_id` | 外部 sandbox workspace 标识；为空时由产品 bootstrap 或 sandbox 服务分配。 |
| `default_runtime_profile` | 普通代码/命令默认 profile，当前推荐 `code-polyglot-basic`。 |
| `timeout` | sandbox API client 默认超时，不替代单次 job timeout。 |

优先级从高到低：

1. 会话级覆盖：CLI `--sandbox`、HTTP `/v1/runs` 请求体里的 `sandbox` 字段、未来 Desktop UI 会话设置。
2. Agent/App/Profile 策略：特定 Agent 或任务模板绑定的 sandbox 要求。
3. 顶层 `sandbox` 全局配置。
4. 默认值：外部 sandbox 关闭，本地执行仍受 approval、PathResolver、ToolGateway、资源锁约束。

会话级覆盖规则：

- 用户可以把本次对话从 `disabled` 提升到 `optional` 或 `required`，前提是产品策略允许。
- 用户可以把本次对话降到 `disabled`，但如果 Agent/Profile/租户策略要求 `required`，必须拒绝或进入审批，不得静默放宽。
- `SandboxRequired` 找不到可用外部 sandbox 或本地平台沙箱时必须失败。
- `SandboxOptional` 可以降级，但必须把降级原因写入 tool result warning、trace 和 audit。
- HTTP API 中允许传入完整 `SandboxProfile`，但产品层必须在进入 ToolGateway 前做租户、角色、Agent 策略裁剪。

## 4. SDK 与 HTTP 怎么选

`genesis-agent` 是 Go 服务，默认优先使用 Go SDK 或基于同一 DTO 的 HTTP adapter：

| 场景 | 推荐调用方式 | 说明 |
| --- | --- | --- |
| agent 后端长期集成 | Go SDK / HTTP adapter | 复用 context、timeout、错误包装、认证注入，适合产品 bootstrap 管理 |
| 跨语言外部系统调用 sandbox | HTTP API | 稳定边界，便于调试、网关治理和多语言接入 |
| MCP 工具生态接入 sandbox | sandbox MCP server | 给外部 MCP client 用；`genesis-agent` 内部不应绕过自身 ToolGateway 直接靠 MCP 执行 |
| Skill 脚本执行 | `SandboxRunner` 经 adapter 调用 SDK/HTTP | Skill 只负责触发说明和资源引用，执行仍走权限、审计、profile 选择；当前已实现 HTTP `CommandClient` 主路径 |

对 `genesis-agent` 内部代码，最佳实践是定义产品无关端口：

```text
run_command / skill_script / office tool
  -> ToolGateway / Approval / Policy / ResourceLocker
  -> ExecutionRunner 或能力专属 service
  -> internal/capabilities/sandbox/contract
  -> internal/capabilities/sandbox/adapter/http 或 sdk
  -> genesis-sandbox HTTP API
```

不要在工具、Skill service 或产品 UI 中直接 new SDK client。SDK client 的创建只应发生在产品 bootstrap 或通用 adapter 内。

## 5. Profile 选择总规则

选择 Profile 不按“语言名称”优先，而按 `task_type + operation + risk + trust + 是否需要 GUI/Office/Browser` 决定。语言只是 profile 内部执行参数。

| 工作类型 | task_type | operation | 默认 runtime_profile | 网络 | 说明 |
| --- | --- | --- | --- | --- | --- |
| 普通命令/代码执行 | `code` 或 `shell` | `run_code` / `run_shell` | `code-polyglot-basic` | `none` | `run_command`、临时代码、数据处理脚本默认走这里 |
| 高风险不可信代码 | `code` | `run_code` / `run_shell` | `code-python-isolated` | `none` | 需要 runsc/gVisor 隔离；不可用时按策略 fail closed |
| 平台工具脚本 | `tool` | `run_tool` / `run_shell` | `tool-basic` | `none` | 工具本身可信但依赖外部进程，例如转换器、解析器 |
| Skill 运行期脚本 | `skill` | `run_skill` | `skill-polyglot-basic` | `none` | 用户/第三方 Skill 的 `scripts/` 默认走这里 |
| Skill 依赖构建 | `build` | `build_dependencies` | `skill-build-polyglot` | `allowlist` | 安装、锁定、解析依赖只允许在注册期/构建期执行 |
| Office/PDF 常规处理 | `office` | Office 常规操作 | `office-basic` | `none` | PDF/Office 文本提取、预览、docx/pptx/xlsx 处理 |
| OCR | `office` | `ocr_pdf` / `ocr_image` | `office-ocr` | `none` | OCR 是按内容/操作触发，不是 PDF 专属；避免把 OCR 依赖塞进普通 Office 镜像 |
| Headless 浏览器 | `browser` | `browser_run` 等 | `browser-playwright` | `allowlist` | Playwright 自动化、截图、页面抽取 |
| GUI 浏览器 | `browser` | `browser_gui` / `vnc_session` | `browser-chrome` | `allowlist` | 需要 noVNC/Chromium GUI 时使用 |
| 桌面会话 | `desktop` | `desktop_session` / `novnc_session` | `browser-desktop` | `allowlist` | XFCE/noVNC 桌面，不作为普通命令默认环境 |

早期 `code-python-basic`、`skill-python-basic` 这类按语言拆分的名字不再作为独立选择规则。Python/Node/TypeScript 通过 `language` 参数进入 polyglot profile；只有隔离等级、依赖集、GUI/Office/OCR 等真实差异才应该形成独立 runtime profile。

### 5.1 Office Skill 脚本 Profile 判定

Office Skill 的脚本不能简单按 Skill 名字写死映射。正确做法是区分两个概念：

- **调用来源**：脚本来自 Skill、Tool、Office service，决定权限、资源读取、审计字段和是否可信。
- **工作负载类型**：脚本实际处理什么对象、需要什么系统依赖，决定 runtime profile。

因此，一个来自 Skill 的脚本如果在处理 `.docx`、`.xlsx`、`.pptx`、`.pdf`，或依赖 LibreOffice、Poppler、PDF/OCR/Office 库，它的 `task_type` 应按 `office` 判定，`metadata.source=skill`、`metadata.skill_id=<id>` 记录来源；不要因为它来自 Skill 就固定走 `skill-polyglot-basic`。

判定信号按优先级使用：

1. **显式能力声明**：Skill / Plugin manifest 声明 `capabilities`、`file_types`、`operations`、`runtime.requirements`、`preferred_profiles`。这是最高优先级。
2. **任务对象和用户意图**：输入/输出文件扩展名、MIME、resource kind、用户请求的动作，例如转换、预览、批注、公式重算、OCR。
3. **脚本和依赖静态扫描**：脚本引用 `soffice`、`libreoffice`、`pdftoppm`、`pdftotext`、`qpdf`、`tesseract`，或依赖 `python-docx`、`openpyxl`、`python-pptx`、`pypdf`、`pdfplumber`、`pandas`、`Pillow`、`markitdown[pptx]`、`pptxgenjs` 等。
4. **产品策略**：企业可按来源、文件大小、租户策略、数据分级把 `office-basic` 升级为远程 `required`，或把 OCR、大文件和未知来源文档强制隔离。
5. **名称兜底**：Skill 名称、目录名、脚本名只作为弱信号，不能作为唯一规则。

Profile 判定应发生在 ToolGateway、Office service 或 Skill script runner 进入 `SandboxRunner` 之前；adapter 只消费已经确定的 `SandboxProfile`。如果用户直接通过 `run_command` 执行一个 Office 脚本，上层也应根据命令上下文、目标文件和依赖扫描生成 Office profile，而不是让 sandbox adapter 在最后一层猜测。

推荐 manifest 形态：

```yaml
runtime:
  workload: office
  preferred_profiles:
    default: office-basic
    ocr: office-ocr
  network: none
capabilities:
  - office.document
  - office.spreadsheet
file_types:
  - .docx
  - .xlsx
  - .pptx
  - .pdf
operations:
  - inspect
  - extract_text
  - convert_to_pdf
  - preview
requirements:
  commands:
    - python
    - libreoffice
    - pdftoppm
  python:
    - python-docx
    - openpyxl
    - pypdf
```

运行期依赖必须预构建或由 profile 提供。Office 运行期不应在对话中临时 `pip install` / `npm install`；确实需要解析依赖时进入注册期/安装期，使用 `skill-build-polyglot` 并产出 lock、缓存或镜像层。远程 Skill 脚本解压到 `/workspace/tmp/skills/<pkg>/scripts` 后执行，Node 默认不会向 `/opt/genesis-sandbox/image/node_modules` 查找；因此 adapter 必须为远程 Office/Skill 脚本注入 `NODE_PATH=/opt/genesis-sandbox/image/node_modules:...`，否则会把镜像已预装的 `pptxgenjs` 等包误判为 `dependency_missing`。

### 5.2 Office 操作到 Profile 的映射

| 操作 | 典型信号 | task_type | operation | runtime_profile |
| --- | --- | --- | --- | --- |
| 读取/检查 Office 文件 | `.docx`、`.xlsx`、`.pptx`，OpenXML 解包、结构检查 | `office` | `inspect` | `office-basic` |
| 生成/编辑 Office 文件 | 生成 docx/pptx/xlsx、写入批注、更新表格、创建图表 | `office` | `generate_docx` / `generate_pptx` / `process_xlsx` | `office-basic` |
| 转换为 PDF 或渲染预览 | `soffice --headless`、`pdftoppm`、缩略图、页面截图 | `office` | `convert_to_pdf` / `preview` | `office-basic` |
| PDF 文本/表格提取 | `pypdf`、`pdfplumber`、`pdftotext`、表单字段读取 | `office` | `extract_text` / `inspect` | `office-basic` |
| PDF 表单填充/批注 | `pdftk`、`qpdf`、reportlab、表单字段写入 | `office` | `fill_form` | `office-basic` |
| Excel 公式重算 | LibreOffice headless、受限用户配置、临时 socket | `office` | `process_xlsx` | `office-basic` |
| 扫描件或嵌入图片 OCR | `tesseract`、`pdf2image`、图像 OCR、无文本层 PDF、Word/PPT/Excel 内嵌截图或拍照内容 | `office` | `ocr_pdf` / `ocr_image` | `office-ocr` |

`office-basic` 与 `office-ocr` 的边界不是文件扩展名，而是内容和操作。普通 Word/PPT/Excel 的结构化文本、表格、公式、批注、样式和版式处理使用 `office-basic`；当任务要求识别图片里的文字、扫描页、截图表格、拍照票据、PPT 截图、Word 内嵌扫描件、Excel 内嵌图片，或检测到文档缺少可抽取文本层时，升级到 `office-ocr`。

`office-ocr` 单独存在是为了隔离 OCR 的体积、系统依赖和性能成本；普通 Office profile 不应默认打包 OCR 全家桶。

### 5.3 Anthropic Office Skills 参考结论

参考 `D:\workspace\go\go-project\anthropics-skills\skills\pdf`、`docx`、`pptx`、`xlsx` 后，能看到一个稳定模式：

- `pdf` 类 Skill 同时覆盖文本抽取、页面渲染、表单字段读取/填充、扫描件 OCR；普通 PDF 处理走 `office-basic`，检测到 OCR、扫描件或缺少文本层时升级 `office-ocr`。
- `docx` 类 Skill 以 OpenXML 解包、批注、修订接受、结构校验和 LibreOffice 打开性检查为主；即使脚本来自 Skill，也应判为 `office-basic`。但如果 Word 里嵌入扫描页、截图或拍照文档，并且任务要求读取图片文字，则升级 `office-ocr`。
- `pptx` 类 Skill 同时依赖 Python、Node/PPTX 生成库、LibreOffice 转 PDF、Poppler 缩略图；这说明 profile 不能按语言拆，应该按“Office 演示文稿处理”走 `office-basic`。但如果幻灯片主要是截图、扫描图或需要识别图片文字，则升级 `office-ocr`。
- `xlsx` 类 Skill 需要 openpyxl/pandas 等库，也可能通过 LibreOffice 做公式重算；公式重算依然是 Office workload，不是普通 Python 脚本。只有当表格内容以图片、截图、票据照片等形式嵌入并需要识别时，才使用 `office-ocr`。

这些目录只能作为规则校验样例，不能变成硬编码白名单。Genesis 的判定逻辑应面向任意未来 Skill：只要声明或扫描结果表明它处理 Office/PDF 工作负载，就选择 Office profile；只有纯通用脚本才走 `skill-polyglot-basic`。

## 6. `run_command` 怎么走

`run_command` 是通用命令执行工具，不等同于 Skill 脚本，也不等同于 Office/Browser 专用工具。

默认规则：

- CLI 不传 `--sandbox` 时使用顶层 `sandbox` 配置；传入 `--sandbox disabled|optional|required` 时作为本次会话覆盖。
- CLI/Desktop 选择 `docker_sandbox` 或 `remote_sandbox` 时，`run_command` 通过 `SandboxRunner` 调用 `genesis-sandbox`。
- Enterprise 默认应走远程 `genesis-sandbox`，不使用宿主机 `shared/local` 执行。
- 普通 shell 命令映射为 `task_type=shell`、`operation=run_shell`、`runtime_profile=code-polyglot-basic`、`language=shell`。
- 高风险或不可信代码按策略升级到 `code-python-isolated` 或等价高隔离 profile。
- 命令是否只读、是否危险、是否需要网络仍由 `execution/policy`、Approval 和 ToolScheduler 判断；profile 只负责执行环境，不替代权限决策。

当前代码中的 `execmodel.SandboxProfile` 应承载这些字段：

```text
mode
provider
workspace_id
runtime_profile
task_type
operation
language
risk_level
metadata
```

adapter 只负责把这些字段转换为 `LeaseRequest` / `ExecJobRequest`，不在 adapter 里重新猜业务语义。

## 7. Skill 脚本怎么走

Skill 的 `SKILL.md`、`references/`、`assets/` 是上下文资源；`scripts/` 是可执行资源。执行脚本时必须走 `run_skill_script` → SkillScriptService（Materialize → Stage → Profile → ExecutionRunner/SandboxSession），不允许模型或 Skill loader 直接在宿主机上按 embed 内部路径运行脚本，也不应默认用 `run_command` 拼 `python scripts/...`。

默认规则：

| Skill 来源 | 运行期脚本 | 依赖构建 | 网络 |
| --- | --- | --- | --- |
| 内置系统 Skill | 优先 `run_skill_script`；Office/PDF 按第 5 节选 `office-basic`/`office-ocr`；纯通用脚本可走 `skill-polyglot-basic` | 构建期固定 | 默认关闭 |
| 项目/用户 Skill | 同入口；Materialize 优先复制 `base_directory/scripts`；Office/PDF/OCR/Browser 按工作负载选择 | `skill-build-polyglot` | 运行期默认关闭，构建期 allowlist |
| 第三方/Marketplace Skill | 同入口；高风险可升级高隔离；专门工作负载按声明和扫描结果选择 | 必须注册期构建，不在对话运行期临时安装 | 按声明和策略裁决，默认关闭 |

Skill 依赖安装必须和运行期执行分离：

- 注册期/安装期：解析依赖、生成 lock、构建缓存或镜像层，使用 `skill-build-polyglot`。
- 运行期：只执行已声明脚本，默认无网络。纯通用脚本使用 `skill-polyglot-basic`；Office/PDF/OCR/Browser 等专门工作负载按第 5 节选择对应 profile。
- 对话运行中**默认禁止**临时 `pip install` / `npm install`。例外：经专用工具 `install_skill_dependencies`（或等价授权通道）审批后，使用 `skill-build-polyglot` + 网络 allowlist 安装，并写入审计；装完由 Agent 再调 `run_skill_script`。完整协议见 `docs/Skill三模式执行与依赖闭环设计.md`。
- 交付物（`.pptx/.docx/.xlsx/.pdf`）须通过产物门禁；`write_file` 禁止纯文本冒充这些扩展名。

实现落点见 `docs/superpowers/specs/2026-07-09-skill-script-execution-design.md`。
## 8. Office 与浏览器怎么走

Office、OCR、浏览器不应通过普通 `run_command` 临时拼命令来做默认路径。它们应有能力域服务或工具封装，再由该服务选择 sandbox profile。

| 能力 | 推荐入口 | Profile |
| --- | --- | --- |
| PDF/Office 文本提取、预览、转换 | Office 能力工具或 service | `office-basic` |
| OCR PDF/图片 | OCR 专用工具或 Office service 子操作 | `office-ocr` |
| Headless 页面抽取/截图/表单 | Browser 能力工具 | `browser-playwright` |
| 需要可视化 GUI 的浏览器 | Browser GUI service | `browser-chrome` |
| 完整桌面会话 | Desktop session service | `browser-desktop` |

这样做能把依赖、资源限制、产物、Viewer/noVNC 地址、审计字段都收敛在专用能力里，而不是散落到 shell 命令。

### 8.1 执行工作空间与本地文件输入

本节只保留关键结论；完整设计见 `docs/执行工作空间与Sandbox文件路径契约.md`。

路径规范不能绑定为“沙箱路径规范”，而必须是统一的 **Execution Workspace Contract**。Agent、Tool、Skill 只依赖逻辑目录；具体目录由执行 backend 映射：

| 逻辑目录 | 含义 | 本地 backend 映射 | sandbox backend 映射 |
| --- | --- | --- | --- |
| `WORK_DIR` | 本次执行工作目录 | 项目工作区或 `.genesis/runs/<run_id>/work` | `/workspace` |
| `INPUT_DIR` | 输入文件目录 | `.genesis/runs/<run_id>/input` | `/workspace/input` |
| `OUTPUT_DIR` | 成果物目录 | `.genesis/runs/<run_id>/output` | `/workspace/output` |
| `TMPDIR` | 临时目录 | `.genesis/runs/<run_id>/tmp` | `/workspace/tmp` |
| `SKILL_DIR` | 本次 materialize 的 Skill 包根 | `WORK_DIR/skills/<pkg>` | `/workspace/tmp/skills/<pkg>`（远程 job 内由脚本包解压后） |

执行器必须把这些目录注入环境变量。Agent 生成代码、内置 Skills 和外部脚本都应优先使用 `INPUT_DIR`、`OUTPUT_DIR`、`TMPDIR`、`SKILL_DIR`，而不是硬编码 `/workspace`、`/tmp`、用户 HOME、Windows 盘符路径或项目根目录。

当用户要求“读取本地 `C:\...` 或 `/Users/...` 的某个文件内容并执行代码”时，处理规则如下：

1. **先解析本地路径**：由 PathResolver 和文件权限/审批系统确认该本地文件是否允许读取。
2. **根据 backend 分流**：
   - 本地 backend：把文件复制或映射到本次 `INPUT_DIR`，代码读取 `INPUT_DIR/<safe_name>`。
   - sandbox backend：不得把宿主机绝对路径传给 sandbox 内代码；必须先通过 sandbox 输入 staging 流程上传为 artifact，再以 `input_artifact_ids` 挂载到 `/workspace/input/<safe_name>`。
3. **重写执行上下文**：传给代码/Skill 的不是原始本地绝对路径，而是逻辑输入路径，例如 `$env:INPUT_DIR\foo.xlsx` 或 `os.path.join(os.environ["INPUT_DIR"], "foo.xlsx")`。
4. **保留溯源**：执行请求 metadata/manifest 记录 `source_local_path -> input_name -> artifact_id/resource_id` 的映射，便于审计和错误诊断。

严禁让 sandbox 内代码直接读取宿主机路径，例如：

```text
C:\Users\alice\Desktop\data.xlsx
D:\project\secret.csv
/Users/alice/data.csv
```

它们在 sandbox 内没有意义，也会破坏安全边界。正确做法是：

```text
本地文件 -> 权限审批 -> stage 到 INPUT_DIR -> 代码读取 INPUT_DIR 下的文件
```

执行前校验按 backend 和执行意图区分：

- **远程 genesis-sandbox / docker sandbox / 任务型本地执行：strict**。如果检测到读取宿主机绝对路径、输入未 staged 到 `INPUT_DIR`、最终成果写到非 `OUTPUT_DIR`，直接返回结构化错误给 Agent，让 Agent 重写代码或让用户修正脚本。
- **内置 Skills：managed-strict**。脚本必须逐步改成只依赖逻辑目录；CI/评测应覆盖路径契约。
- **本地编程工具模式：permission-only**。参考 Codex 的本地工作路径模型，以真实 `cwd` 为默认工作区，按 `workspace_roots` / `writable_roots` / 受保护元数据路径 / 审批策略控制读写，不强制把项目读写改成 `INPUT_DIR` / `OUTPUT_DIR`。

路径 preflight 的实现应采用可插拔 analyzer 架构，而不是让 `run_command` 或 sandbox adapter 直接理解所有语言：

```text
ExecutionRunner
-> PathValidator
-> Analyzer Registry
   -> shell_text
   -> python_source
   -> javascript_source
   -> go_source
   -> java_source
   -> powershell_source
   -> shell_script_source
   -> skill_manifest
```

关键边界：

- Analyzer 是质量门，不是安全边界；未发现违规不代表可绕过 PathResolver、审批、本地沙箱或远程 sandbox 文件系统。
- `Runner` 只调用 `PathValidator`，不硬编码 Python/JS/PowerShell 语义。
- 产品侧可以在 bootstrap 中注入自己的 validator 或 analyzer registry，例如 Enterprise 增加更严格的 JS/PowerShell 检查。
- strict 模式发现明确违规时 fail closed；permission-only 本地编程模式不因真实项目路径被 analyzer 命中而拒绝。

结构化路径错误示例：

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

这个契约适用于不走远程 sandbox 的任务型本地执行。区别只是目录映射不同：本地任务执行使用 `.genesis/runs/<run_id>/...`，远程 sandbox 使用 `/workspace/...`。

但本地编程助手模式不同。它类似 Codex/Claude Code：命令在真实 `cwd` 中执行，允许读写授权工作区内的真实项目路径，工作区外访问通过 PathResolver 和审批扩展授权范围。此模式仍可注入 `WORK_DIR`、`INPUT_DIR`、`OUTPUT_DIR`、`TMPDIR`，但不把这些目录作为硬性路径校验条件；只有当任务是在“处理输入并返回成果物”时，才切到 strict workspace contract。

### 8.2 sandbox 产物如何返回本地

外部 sandbox 执行生成的文件必须同时支持两种返回方式：

- **远程引用**：`Result.Artifacts[].id` 保留 sandbox artifact id，SDK/HTTP 调用方可以继续按 artifact id 下载或交给后续远程步骤处理。
- **本地落盘**：CLI/Desktop 可配置本地 artifact root，把 sandbox artifact 下载到本机受控目录，并在 `Result.Artifacts[].local_path` 返回路径。

CLI 默认本地目录：

```text
.genesis/artifacts/<workspace_id-or-job_id>/<artifact_name>
```

设计约束：

- artifact 文件名必须做 basename 和字符白名单清洗，禁止 `../`、绝对路径、盘符等越界写入。
- 默认不写入用户 HOME，也不覆盖源文件目录；用户需要替换源文件时，应由文件工具在权限审批后显式写入。
- 结果 JSON 必须同时保留 `id`、`workspace_id`、`job_id`、`name`、`size`、`sha256`、`mime`、`remote_url`、`local_path`，便于 SDK、UI 和后续工具继续处理。
- Office Skill 脚本内部返回的 `artifacts` 字段只是脚本级候选产物；远程 sandbox 服务确认后的 `output_artifacts` 才是跨进程/跨机器可下载的稳定产物。

SDK 方式调用 Skill 时推荐链路：

1. Tool/Skill 选择 `runtime_profile`，通过 `/v1/sandboxes:lease` 获取 sandbox，并启动后台续租。
2. 把输入文件以 resource id、本地受控输入目录或 job file 形式交给执行器；如果 backend 是 sandbox，执行器必须通过 `UploadJobFile` / `input_artifact_ids` 或等价输入 staging API 挂载到 `/workspace/input`。
3. Skill 脚本只在 sandbox 内生成或修改文件，并在 stdout JSON 中声明候选 `artifacts`。
4. 通过 `/v1/jobs` 提交绑定 `sandbox_id` 的 job，并按 `GET /v1/jobs/{job_id}` 轮询到终态；即使提交响应已经是终态，也应再取一次完整 JobResult，避免遗漏异步收集的 `output_artifacts`。
5. genesis-sandbox 服务把候选产物登记为 `output_artifacts`，返回 artifact id。
6. 客户端根据场景选择：保留远程 `id/remote_url`、下载到 `.genesis/artifacts/...`，或由后续工具继续在远端消费。
7. 任务结束必须释放 sandbox：先停止续租，再 `POST /v1/sandboxes/{id}/release`；release 失败再 `DELETE /v1/sandboxes/{id}` destroy。
8. 如果用户要求把生成文档放回项目目录，必须再经过本仓库文件工具的 PathResolver、权限审批和 Freshness 检查，不能由 sandbox adapter 直接覆盖本地源文件。

当前实现状态：

- `internal/capabilities/sandbox/adapter/http` 已解析 `output_artifacts` 并支持下载 `/v1/artifacts/{id}`。
- `internal/capabilities/sandbox/adapter/http` 按 `sdks/go/sandbox` 的生产模式实现 lease、后台 renew、job poll、GetJob 产物收集、release/destroy；不再保留裸 `/v1/jobs` 作为主执行路径。
- CLI 外部 sandbox 模式默认把 artifact materialize 到 `.genesis/artifacts/...`。
- genesis-sandbox SDK/HTTP 当前可用的是 `UploadJobFile`、`ListJobArtifacts`、`DownloadArtifact` 和 Workspace 生命周期；没有稳定 workspace 文件 CRUD 端点。因此 `FileSystemClient` 的 workspace 文件读写方法暂时返回明确 unsupported；不要伪造 `/v1/workspaces/{id}/files` 这类未公开 API。

## 9. 产品默认策略

| 产品 | 默认执行 | 可选增强 | 说明 |
| --- | --- | --- | --- |
| CLI | 不传 `--sandbox` 时读取顶层 `sandbox` 配置；默认本地宿主机 + approval + PathResolver | 会话级 `--sandbox` 覆盖；配置可切到 `docker_sandbox` / `remote_sandbox` | 本地开发低摩擦，但 required 必须 fail closed |
| Desktop | 本地宿主机或本地平台沙箱 | 可切到 `docker_sandbox` / `remote_sandbox` | GUI 审批和文件选择器由 Desktop 产品负责 |
| Enterprise | 默认推荐远程 `genesis-sandbox`，但仍由顶层/租户策略显式开启 | HTTP run 请求可传会话级 `sandbox` 字段，受 RBAC 裁剪 | 不直接访问服务器宿主机文件系统，不使用 `shared/local` 执行 |

## 10. 当前实现状态与下一步

已落地：

1. `execmodel.SandboxProfile` 已承载 `runtime_profile`、`task_type`、`operation`、`language`、`risk_level`、`metadata`，作为工具到 sandbox adapter 的稳定执行意图。
2. `internal/capabilities/sandbox/adapter/http` 已实现同步命令执行主路径：Bearer 认证、lease/renew/release 生命周期、绑定 sandbox 的 `POST /v1/jobs`、`GET /v1/jobs/{id}` 轮询和产物收集、超时、stdout/stderr/exit_code、错误码映射。
3. CLI sandbox config 已支持 `mode=docker_sandbox|remote_sandbox`、`endpoint`、`api_key`、`workspace_id`、`default_runtime_profile`、会话级覆盖。
4. CLI bootstrap 在外部 sandbox 模式下已注入 genesis-sandbox HTTP `CommandClient` 和 execution sandbox runner；本地平台模式继续使用 `shared/local` runner。
5. 内置 Office Skills 已提供最小 Skill 包和 JSON inspect 脚本，作为 Office profile 规则的第一批落地点。
6. `execution/pathcontract` 已实现可插拔 PathValidator/Analyzer Registry，默认包含 `shell_text`、`python_source`、`javascript_source`、`go_source`、`java_source`、`powershell_source`、`shell_script_source` 和 `skill_manifest` 分析器；`execution/service.Runner` 支持产品侧注入自定义 validator。
7. `run_skill_script` + SkillScriptService 已落地：Materialize（embed/磁盘）、本地任务工作空间、`SKILL_DIR` 注入、Office profile 覆盖、产物门禁；CLI 已接线；远程 genesis-sandbox 走 Session StageInput，Skill 脚本以 zip 包上传到 `/workspace/input` 并在 job 内解压到 `/workspace/tmp/skills/<pkg>` 后执行。
8. Skill 远程 **optional** 在 SessionClient 缺失或 `sandbox_unavailable` 时可降级本地并写 warning（`skill_script_sandbox_fallback`）；**required** fail closed。远程脚本包 zip staging、job 内解压、绝对入口路径已有单测。
9. Enterprise bootstrap 可按顶层 `sandbox` 配置注入本地 disabled / local-platform / genesis-sandbox SessionClient（不再写死仅 disabled）；生产 headless ask 审批仍为过渡。
10. `skills.enable_preflight` / `skills.auto_retry_after_install`（默认 false）已接入 CLI 与 Enterprise（经 skillstack Options）；`auto_retry` 需产品注入 `DependencyInstaller` 后才真正装包。
11. Preflight：npm/pip 缺失硬失败；system（soffice 等）LookPath 缺失仅 warning 后继续，避免 Skill 级 system 声明误伤不依赖 soffice 的脚本。

### office-basic 镜像应对齐的包清单（契约；构建在 genesis-sandbox 仓）

> 对齐 Codex `Dockerfile.secure`：**镜像预装工具链/运行时**，不是对话期 per-skill pip。勿把 SDK `_runtime_setup.py` 自举当成 Skill 依赖安装。

| 类别 | 包/命令 | 用途 |
| --- | --- | --- |
| Runtime | `python3`、`node` | 跑 Office Skill 脚本 |
| Node | `pptxgenjs` | `create_pptx.js` 从零生成 |
| Python | `Pillow`（`PIL`） | 缩略图/预览图处理 |
| System | `soffice` / LibreOffice | PPT/DOCX/XLSX→PDF |
| System | `pdftoppm`（Poppler） | PDF→预览图 |
| 可选 | `python-pptx`、`pypdf`、`pdfplumber`、`openpyxl`、`python-docx` | inspect/编辑类脚本 |

本仓 DoD：文档已列；镜像构建与 profile 健康检查属 sandbox 仓任务。

仍需继续：

1. `FileSystemClient` 的 workspace 文件 CRUD 尚未接通，因为 genesis-sandbox OpenAPI 当前未暴露稳定 workspace 文件端点；已接通 artifact 下载/本地落盘主路径。
2. Enterprise 租户/用户/RBAC/credential 与人工审批 requester 仍需产品化；headless ask 仅为过渡。
3. Office/Browser 后续应沉淀能力专属 service（生成/编辑库封装），避免长期依赖模型临时拼 shell 或临时 Python 生成逻辑；`run_skill_script` 已覆盖脚本执行主路径。
4. 启动或健康检查应调用 `GET /v1/runtime-profiles`，发现目标 profile 不可用时按 `SandboxRequired` / `SandboxOptional` 语义处理。
5. 远程 StageInput 不再依赖服务端保留嵌套路径名；当前客户端以扁平 zip 输入规避 `/workspace/input/<safe_name>` 差异。后续若 sandbox 暴露稳定 workspace 文件 API，可替换 staging 实现但保持 `SKILL_DIR` 契约。
6. Office OCR 升级（`office-ocr`）尚未由 SkillScript 自动判定，当前 Office 默认 `office-basic`。
7. `auto_retry_after_install` 的 Installer 端口需产品注入真实 `install_skill_dependencies` 适配器后才生效。
