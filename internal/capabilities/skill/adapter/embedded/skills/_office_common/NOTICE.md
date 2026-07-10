# NOTICE — Office shared scripts

本目录 `scripts/office/` 源自 Anthropic skills 仓库中 `pptx`/`docx`/`xlsx` 共享的 OOXML 工具树（unpack/pack/validate/soffice/helpers/validators/schemas）。

- 上游声明：`license: Proprietary`（见源 Skill `LICENSE.txt`）。
- Genesis 策略：在已确认可原样迁移的前提下，保留脚本主体；仅做路径契约（`path_contract` / `INPUT_DIR`/`OUTPUT_DIR`/`WORK_DIR`）等最小适配。
- 请勿在未同步三端的情况下分叉修改本树；业务差异放在各 `office-*` Skill 专有脚本中。
