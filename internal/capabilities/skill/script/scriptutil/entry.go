package scriptutil

import (
	"path"
	"path/filepath"
	"strings"
)

// IsExecutableScriptEntry 判断 resource id / 文件名是否可作为 run_skill_command 入口。
// 辅助模块（如 path_contract.py）仍会 materialize 供 import，但禁止当作主脚本执行。
func IsExecutableScriptEntry(resourceOrName string) bool {
	raw := strings.TrimSpace(resourceOrName)
	if raw == "" {
		return false
	}
	slash := strings.ReplaceAll(raw, `\`, `/`)
	lowerPath := strings.ToLower(slash)
	// 共享 office 树中的 helpers/validators/schemas 不是入口。
	for _, blocked := range []string{"/helpers/", "/validators/", "/schemas/"} {
		if strings.Contains(lowerPath, blocked) {
			return false
		}
	}
	base := path.Base(slash)
	base = filepath.Base(base)
	lower := strings.ToLower(base)
	switch lower {
	case "path_contract.py", "__init__.py":
		return false
	}
	if strings.HasPrefix(lower, "_") {
		return false
	}
	if !strings.HasSuffix(lower, ".py") && !strings.HasSuffix(lower, ".js") && !strings.HasSuffix(lower, ".mjs") && !strings.HasSuffix(lower, ".cjs") {
		// 非脚本扩展名：交给上层校验；此处不拦
		return true
	}
	return true
}

