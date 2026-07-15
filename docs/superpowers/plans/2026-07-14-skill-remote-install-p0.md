# Skill 远程安装 P0 Implementation Plan

> **For agentic workers:** Use executing-plans / implement task-by-task. Steps use checkbox syntax.

**Goal:** 支持从 GitHub URL / source 安装单 Skill 或 marketplace 包；CLI + 对话工具；三产品共用解析与编排契约。

**Architecture:** internal parser（远程语义）→ marketplace.Service.InstallFromSource → skillmarket Fetcher/Installer（CLI）；工具只调 port；Enterprise 注入 deny-all AllowedSourcePolicy。

**Tech Stack:** Go、现有 package/marketplace、shared/local/skillmarket、CLI bootstrap

---

### Task 1: Internal remote Source parser

**Files:**
- Create: `internal/capabilities/package/marketplace/parser/parser.go`
- Create: `internal/capabilities/package/marketplace/parser/parser_test.go`
- Modify: `shared/local/skillmarket/parser.go` 委托 remote 解析

- [x] Parser 单测：tree/blob/shorthand/`..` 拒绝
- [x] 实现并通过测试
- [x] skillmarket Parser 包装 dir/file + 委托

### Task 2: Contract ports + ActionSkillInstall

- [x] 增加 AllowedSourcePolicy、CatalogReloader、InstallFromSource 类型
- [x] 增加 ActionSkillInstall

### Task 3: Detect + InstallFromSource

- [x] 内存 fetcher fixture：单 Skill 合成安装
- [x] 多 Skill NeedsChoice；policy deny；pkg@market 委托

### Task 4: CLI install URL + default policy

- [x] `genesis skill install <url>` 走 InstallFromSource

### Task 5: 对话工具 install_skill_from_source

- [x] 工具单测：approval deny / 成功委托
- [x] 跑相关 go test

### Task 6: Verify

- [x] 相关包 go test 通过
