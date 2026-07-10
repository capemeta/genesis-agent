package model

import "testing"

func TestSkillDecisionKey(t *testing.T) {
	if got := SkillDecisionKey("office-ppt"); got != "Skill(office-ppt)" {
		t.Fatalf("got = %q", got)
	}
	if got := SkillDependenciesDecisionKey("office-ppt"); got != "Skill(office-ppt)+dependencies" {
		t.Fatalf("got = %q", got)
	}
}
