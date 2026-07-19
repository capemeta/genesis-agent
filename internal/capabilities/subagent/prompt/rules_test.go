package prompt

import (
	"strings"
	"testing"
)

func TestSystemRulesProactive(t *testing.T) {
	got := SystemRules(PostureProactive)
	for _, want := range []string{
		"子智能体委派纪律",
		"Task(subagent_type=explore)",
		"主动",
		"不要为小事",
		"同一条",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("proactive missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "不算授权") {
		t.Fatalf("proactive should not use explicit_request_only wording:\n%s", got)
	}
}

func TestSystemRulesExplicitRequestOnly(t *testing.T) {
	got := SystemRules(PostureExplicitRequestOnly)
	for _, want := range []string{
		"子智能体委派纪律",
		"明确要求",
		"不算授权",
		"AGENTS.md",
		"Skill",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("explicit missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "优先 Task(subagent_type=explore)") {
		t.Fatalf("explicit should not force explore-first:\n%s", got)
	}
}

func TestSystemRulesNormalizesUnknownPostureToProactive(t *testing.T) {
	got := SystemRules("weird")
	if !strings.Contains(got, "Task(subagent_type=explore)") {
		t.Fatalf("unknown posture should fall back to proactive:\n%s", got)
	}
}

func TestRenderToolDescriptionIncludesAgentsAndUsage(t *testing.T) {
	agents := []AgentSummary{
		{Name: "explore", Description: "只读探索", WhenToUse: "广搜代码时"},
		{Name: "plan", Description: "只读规划", WhenToUse: "改前规划时"},
	}
	got, err := RenderToolDescription(agents, DescriptionOptions{Posture: PostureProactive, MaxConcurrent: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Task(subagent_type=...)",
		"<available_agents>",
		`name="explore"`,
		`when_to_use="广搜代码时"`,
		"When NOT to use",
		"max_concurrent=3",
		"proactively",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("tool description missing %q:\n%s", want, got)
		}
	}
}

func TestRenderToolDescriptionExplicitOmitsProactiveNudge(t *testing.T) {
	got, err := RenderToolDescription(nil, DescriptionOptions{Posture: PostureExplicitRequestOnly, MaxConcurrent: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "explicitly ask") && !strings.Contains(got, "明确要求") {
		t.Fatalf("explicit description missing authorization gate:\n%s", got)
	}
	if strings.Contains(got, "proactively") {
		t.Fatalf("explicit description should not say proactively:\n%s", got)
	}
}

func TestNormalizePosture(t *testing.T) {
	if NormalizePosture("") != PostureProactive {
		t.Fatal("empty -> proactive")
	}
	if NormalizePosture(string(PostureExplicitRequestOnly)) != PostureExplicitRequestOnly {
		t.Fatal("explicit preserved")
	}
	if NormalizePosture("EXPLICIT_REQUEST_ONLY") != PostureExplicitRequestOnly {
		t.Fatal("case insensitive")
	}
}
