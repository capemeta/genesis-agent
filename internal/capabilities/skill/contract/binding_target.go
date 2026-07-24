package contract

import (
	"fmt"
	"strings"

	"genesis-agent/internal/capabilities/skill/model"
)

// ValidateBoundTarget 将模型可选提供的 handle/physical/package 参数降为一致性断言；
// Invocation 已激活后，真正身份只能来自不可变 Binding。
func ValidateBoundTarget(binding model.InvocationBinding, name string, pkg model.PackageID) error {
	name = strings.TrimSpace(name)
	if name != "" && name != binding.Handle && name != binding.PhysicalSkill {
		return fmt.Errorf("SKILL_INVOCATION_MISMATCH: skill=%q与当前激活的技能上下文 %q不一致。\nSuggested Action: 当前技能上下文为 %q。如需操作或为 %q 技能安装依赖，请先调用 Skill(skill=%q, task=...) 加载并激活目标技能。", name, binding.Handle, binding.Handle, name, name)
	}
	if pkg != "" && pkg != binding.Package.PackageID {
		return fmt.Errorf("SKILL_INVOCATION_MISMATCH: package=%q与当前binding package %q不一致", pkg, binding.Package.PackageID)
	}
	return nil
}
