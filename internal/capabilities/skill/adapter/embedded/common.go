package embedded

import (
	"io/fs"
)

// OfficeCommonPackage 是共享 OOXML 工具包目录名（无 SKILL.md，不进入 catalog）。
const OfficeCommonPackage = "_office_common"

// OfficeCommonScriptsFS 返回 _office_common/scripts 子树，供 materialize 合并到 office-* Skill。
func OfficeCommonScriptsFS() (fs.FS, error) {
	return fs.Sub(systemSkills, "skills/"+OfficeCommonPackage+"/scripts")
}
