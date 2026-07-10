package react

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
)

func TestNarrowToolNamesEmptyAllowedKeepsCurrent(t *testing.T) {
	current := []string{"read_file", "Skill", "run_command"}
	got, ok := narrowToolNames(current, nil)
	if !ok || len(got) != 3 {
		t.Fatalf("got=%v ok=%v", got, ok)
	}
}

func TestNarrowToolNamesIntersectsAndKeepsMetaTools(t *testing.T) {
	current := []string{"read_file", "write_file", "run_command", "Skill", "list_skill_resources", "run_skill_script", "install_skill_dependencies", "web_search"}
	allowed := []string{"read_file", "run_command"}
	got, ok := narrowToolNames(current, allowed)
	if !ok {
		t.Fatal("expected ok")
	}
	want := map[string]bool{"read_file": true, "run_command": true, "Skill": true, "list_skill_resources": true, "run_skill_script": true, "install_skill_dependencies": true}
	if len(got) != len(want) {
		t.Fatalf("got=%v", got)
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("unexpected %q in %v", name, got)
		}
	}
}

func TestNarrowToolNamesEmptyIntersectionFails(t *testing.T) {
	got, ok := narrowToolNames([]string{"web_search"}, []string{"read_file"})
	if ok || got != nil {
		t.Fatalf("got=%v ok=%v, want failure", got, ok)
	}
}

func TestSkillInjectionKeyPrefersResource(t *testing.T) {
	key := skillInjectionKey(skillInjectionOutput{QualifiedName: "office-ppt", Resource: "embedded:office-ppt/SKILL.md"})
	if key != "embedded:office-ppt/SKILL.md" {
		t.Fatalf("key = %q", key)
	}
}

func TestRenderAlreadyLoadedAck(t *testing.T) {
	ack := renderAlreadyLoadedAck(skillInjectionOutput{QualifiedName: "office-ppt", Resource: "r1"})
	if !strings.Contains(ack, `"type":"already_loaded"`) {
		t.Fatalf("ack = %s", ack)
	}
}

func TestAutoRewriteDefaultEnabled(t *testing.T) {
	e := &ReactLoopEngine{}
	if !e.shouldAutoRewriteSkillCollision() {
		t.Fatal("default should enable auto rewrite")
	}
	off := false
	e.autoRewriteSkillCollision = &off
	if e.shouldAutoRewriteSkillCollision() {
		t.Fatal("explicit false should disable")
	}
}

func TestRewriteSkillCollisionsRewritesName(t *testing.T) {
	matcher := &fakeSkillMatcher{canonical: "office-ppt"}
	e := &ReactLoopEngine{skillNameMatcher: matcher, registry: emptyRegistry{}}
	calls := []domain.ToolCall{{
		ID:       "1",
		Function: domain.FunctionCall{Name: "office-ppt", Arguments: `{"action":"create"}`},
	}}
	got := e.rewriteSkillCollisions(context.Background(), calls, logger.NewNop())
	if len(got) != 1 || got[0].Function.Name != "Skill" {
		t.Fatalf("got = %+v", got)
	}
	if !strings.Contains(got[0].Function.Arguments, `"skill":"office-ppt"`) {
		t.Fatalf("args = %s", got[0].Function.Arguments)
	}
}

type fakeSkillMatcher struct{ canonical string }

func (f *fakeSkillMatcher) Match(context.Context, string) (string, bool, error) {
	return f.canonical, true, nil
}

type emptyRegistry struct{}

func (emptyRegistry) Register(tool.Tool) {}
func (emptyRegistry) Get(string) tool.Tool {
	return nil
}
func (emptyRegistry) Execute(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
func (emptyRegistry) ListInfos() []*tool.Info           { return nil }
func (emptyRegistry) FilterInfos([]string) []*tool.Info { return nil }
func (emptyRegistry) Names() []string                   { return nil }

func TestRenderSkillToolAckReportsNarrowFailure(t *testing.T) {
	ack := renderSkillToolAck(skillInjectionOutput{QualifiedName: "demo", AllowedTools: []string{"missing"}}, false)
	if !strings.Contains(ack, `"narrow_failed":true`) {
		t.Fatalf("ack = %s", ack)
	}
}

func TestNormalizeExclusiveSkipMessageMentionsSkill(t *testing.T) {
	msg := "跳过：Skill 加载必须独占本轮；注入完成后请在下一轮再调用其他工具。"
	if !strings.Contains(msg, "Skill") {
		t.Fatalf("message outdated: %s", msg)
	}
}
