package model

import "fmt"

// SkillDecisionKey 生成 Kode 式审批决策键，例如 Skill(office-ppt)。
func SkillDecisionKey(qualifiedName string) string {
	if qualifiedName == "" {
		return "Skill(*)"
	}
	return fmt.Sprintf("Skill(%s)", qualifiedName)
}

// SkillDependenciesDecisionKey 生成外部依赖审批键。
func SkillDependenciesDecisionKey(qualifiedName string) string {
	return SkillDecisionKey(qualifiedName) + "+dependencies"
}
