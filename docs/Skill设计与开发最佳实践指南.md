# Genesis Agent Skill 设计与开发最佳实践指南

> 适用版本：Genesis Agent v1.0+  
> 适用对象：内置 Skill 开发者、团队 Skill 维护者、第三方 Skill 移植者

本指南总结了 Genesis Agent 在 Skill 架构演进、多 Invocation 隔离、沙箱挂载及提示词工程实践中沉淀的核心经验与硬纪律，旨在帮助开发者设计出**高稳定、零幻觉、跨平台且生产级可控**的 Skill 扩展包。

---

## 一、 架构定位与核心认知

### 1.1 Skill 的本质
- **技能（Skill）是任务知识与流程包，不是独立可执行原语（Tool）**。
- 大模型加载技能必须经由统一网关工具 `Skill(skill="<handle>", task="...", inputs=[...])`，**严禁把技能名（如 `office-ppt`）当作独立 Tool 调用**。

### 1.2 主 Agent 与 Fork 子 Agent 的双层架构
Genesis Agent 引入了主侧委派与子 Agent 隔离空间：

| 维度 | 主 Agent (`context=main` / `AudienceRoot`) | Fork 子 Agent (`skill-fork:` / `AudienceSkillFork`) |
|---|---|---|
| **角色** | 任务调度者与总控台 | 隔离沙箱内的具体任务执行者 |
| **工具权限** | 拥有完整的全局工具链（`write_file`, `Task`, `Skill` 等），**不受 `tool_policy.allow` 限制** | 仅保留最小安全工具白名单（如 `run_skill_command`, `install_skill_dependencies`） |
| **文件读写** | 拥有宿主机与项目工作区全量读写权限 | 限制在当前 Invocation 的专属 **Skill 工作目录 (WorkDir)** 内 |
| **生命周期** | 与用户会话同行 | 任务完成或返回总结后自动销毁 (`status=completed`) |

---

## 二、 底层运行机制解密

理解系统的底层物理机制，是避免编写“无效/误导指引”的前提：

### 2.1 InputStager 与 CAS 文件暂存机制
当用户在对话中提供待处理文件时（如 `E:\文档\3-人资部.pdf`）：
1. **自动识别与哈希**：主 Agent 调用 `Skill(inputs=["E:\\文档\\3-人资部.pdf"])` 时，系统内核 `InputResolver` 与 `InputStager` 自动对该文件进行 CAS 内容哈希校验与版本绑定。
2. **静默挂载与别名映射**：文件被静默只读挂载/暂存至当前 Skill 的工作目录下，别名简化为基础文件名（如 `3-人资部.pdf`）。
3. **铁律**：**完全支持并鼓励主 Agent 直接传递宿主机绝对路径**！系统会在后台完成暂存，**严禁大模型手动跑 `run_command` 或 `Copy-Item`/`cp` 搬运文件**。

### 2.2 多后端自适应沙箱 (Multi-Backend Degradation)
`run_skill_command` 在不同环境下会自动选择安全物理后端：
- `remote_sandbox`：Docker 隔离容器；
- `local_platform_sandbox`：本地平台进程沙箱（AppContainer / seatbelt / bwrap）；
- `local_host`：宿主机受控进程降级；

无论后端如何变化，**当前命令的执行目录 (cwd) 始终钉在 Skill 工作目录下**。因此在 `command` 命令行中，**直接使用相对文件名别名（如 `3-人资部.pdf`）**，严禁拼接宿主机物理盘符路径或硬编码环境敏感路径。

### 2.3 第三方 Skill 无 YAML 自动 Fork 推导
对于无 `genesis.skill.yaml` 侧车的开源 Skill（参照 Kode / Anthropic Agent Skills 规范）：
- **包含代码/脚本**（存在 `scripts/` 目录或 `.py`/`.js`/`.sh`/`.ps1` 文件）：运行时自动推导为 `agent_mode: {mode: fork}`，赋予安全的默认子 Agent 工具白名单 (`[run_skill_command, install_skill_dependencies, list_skill_resources, read_skill_resource]`)。
- **纯文档/知识库**（仅 Markdown 和静态资源）：自动推导为 `agent_mode: {mode: main}` (Inline Prompt 展开)。

---

## 三、 Skill 提示词与指引编写三大避坑铁律

### 避坑铁律 1：禁止在指引中教大模型使用“未授权工具”
- **典型错误**：在只读提取 Skill (`office-pdf-read`) 的 `read.md` 指引中写：“*建议先用 `write_file("parse.py", ...)` 写入脚本，再用 `run_skill_command` 执行*”。
- **后果**：`skill-fork` 子 Agent 进沙箱后，发现工具列表中根本没有 `write_file`。大模型为了遵循指引，会在 `run_skill_command` 参数中拼凑 `command="write_file(...)"`，导致连续 6 轮调用失败！
- **正确做法**：指引内容必须与 `genesis.skill.yaml` 中配置的 `tool_policy.allow` 保持 100% 严格对齐。只读提取环境仅提供 `run_skill_command`，指引中就只能指导模型使用 `run_skill_command`！

### 避坑铁律 2：避免多行 `python -c` 内联代码引发 Shell 字符串转义爆炸
- **典型错误**：指引模型在 `run_skill_command(command="python -c \"with open('foo.py') as f:\n f.write('...')\"")` 中提交带换行符和多重嵌套引号的代码。
- **后果**：跨平台 Shell (PowerShell / CMD / Bash) 解释器会破坏多重单双引号和换行符，引发 `SyntaxError: unterminated string literal`。
- **正确做法**：
  1. 对于**只读提取/分析任务**：在指引中直接提供验证过的 **单行 Python 列表推导式 (One-liner)**（外层双引号 `"..."`，内层单引号 `'...'`，零换行符）。
  2. 对于**复杂生成/制作任务**：由包内自带的 `scripts/` 脚本执行，或利用系统自带的 `autoRewriteRiskyInlineCommand` 机制。

### 避坑铁律 3：去黑话化与跨平台术语规范
- **典型错误**：在提示词中使用“*WorkDir Binding Locator*”、“*ResourceRef*”、“*在 Linux 沙箱 `/workspace` 中*”、“*Windows 盘符*”。
- **后果**：黑话导致大模型产生幻觉；平台相关词汇破坏跨平台移植性。
- **正确做法**：
  - 用 **“宿主机绝对路径”** 统一描述 Windows/macOS/Linux 的物理路径。
  - 用 **“Skill 工作目录”** 统一描述沙箱或本地的执行环境。
  - 用 **“文件名别名 (如 3-人资部.pdf)”** 统一描述暂存后的待处理文件。

---

## 四、 规范模板范例

### 4.1 只读提取 Invocation 指引模板 (`references/invocations/read.md`)

```markdown
# PDF 只读提取入口 (office-pdf-read)

本入口专门用于提取并总结绑定 PDF 中的文字、表格与元数据。

- 【硬规则与边界】：本入口仅用于只读分析与摘要。**严禁在此入口下尝试修改、编辑或重新生成 PDF 文件**。
- 【任务执行与最佳实践】：
  1. 禁止搜索或盲猜不存在的 `extract_pdf.py` 脚本。
  2. 请直接使用 `run_skill_command` 提交下述标准单行 Python 指令（无换行、无嵌套多重引号，避免 Shell 语法转义错误）。
  3. 输入文件名直接使用 inputs 中的 `alias`（如 `3-人资部.pdf`）。

- 【标准单行 Python 提取命令模板】：

1. **提取元数据与全量正文 (pypdf)**：

python -c "import pypdf; r=pypdf.PdfReader('输入别名.pdf'); [print(f'=== 第 {i+1} 页 ===\n{p.extract_text()}') for i,p in enumerate(r.pages) if p.extract_text() and p.extract_text().strip()]"


2. **精确提取表格内容 (pdfplumber)**：

python -c "import pdfplumber; p=pdfplumber.open('输入别名.pdf'); [print(f'=== 第 {i+1} 页 ===') or [print(' | '.join(str(c).strip() if c else '' for c in r)) for r in t] for i,page in enumerate(r.pages) for t in [page.extract_tables()] if t]; p.close()"


- 提取完成后直接将总结消息返回给用户，无需提交物理文件交付物。
```

---

## 五、 Skill 开发者发布前 10 项自检清单 (Checklist)

- [ ] 1. **工具契约一致性**：指引文档中引用的工具在 `genesis.skill.yaml` 的 `tool_policy.allow` 中均已授权。
- [ ] 2. **无幻觉脚本**：指引中未提及任何不存在的 `.py` / `.sh` 脚本文件名。
- [ ] 3. **绝对路径兼容**：确认 `Skill(inputs=[...])` 的描述允许直接接收用户提供的宿主机绝对路径。
- [ ] 4. **单行提取验证**：只读提取模板中的 `python -c` 代码已在命令行中实际跑通，且不含多余换行与转义引号。
- [ ] 5. **绝对路径屏蔽**：`run_skill_command` 的命令示例中无任何 `C:\`、`/Users/` 或 `/workspace/` 等绝对路径硬编码。
- [ ] 6. **依赖显式声明**：Skill 所需的第三方 Python 库（如 `pypdf`, `pdfplumber`）已在 `genesis.skill.yaml` 的 `dependencies` 中声明，便于 `runtime_probe` 自动预检。
- [ ] 7. **交付类型匹配**：只读类 (`read`) Invocation 的 `result.kind` 设为 `message`；制作类 (`work`) 设为 `deliverables`。
- [ ] 8. **跨平台兼容**：命令不依赖特定 Shell 的专属 Cmdlet（如 PowerShell 的 `Out-File`、`Copy-Item`），统一使用标准 `python` / `node` / `bash` 命令。
- [ ] 9. **去架构黑话**：提示词中不包含 `ResourceRef`、`Binding Locator` 等内部后端术语。
- [ ] 10. **独立隔离安全**：隔离子 Agent 不拥有修改宿主机项目代码的能力，确保安全性。
