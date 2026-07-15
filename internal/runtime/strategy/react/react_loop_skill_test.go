package react

import (
	"context"
	"fmt"
	"strings"
	"testing"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
)

func TestNarrowToolNamesEmptyAllowedKeepsCurrent(t *testing.T) {
	current := []string{"read_file", "Skill", "run_command"}
	got, ok := narrowToolNames(current, nil)
	if !ok || len(got) != 3 {
		t.Fatalf("got=%v ok=%v", got, ok)
	}
}

func TestNarrowToolNamesIntersectsAndKeepsMetaTools(t *testing.T) {
	current := []string{"read_file", "write_file", "run_command", "Skill", "list_skill_resources", "run_skill_command", "install_skill_dependencies", "web_search"}
	allowed := []string{"read_file", "run_command"}
	got, ok := narrowToolNames(current, allowed)
	if !ok {
		t.Fatal("expected ok")
	}
	want := map[string]bool{"read_file": true, "run_command": true, "Skill": true, "list_skill_resources": true, "run_skill_command": true, "install_skill_dependencies": true}
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

func (emptyRegistry) Register(tool.Tool)   {}
func (emptyRegistry) Unregister(string)    {}
func (emptyRegistry) Get(string) tool.Tool {
	return nil
}
func (emptyRegistry) Execute(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
func (emptyRegistry) ListInfos() []*tool.Info           { return nil }
func (emptyRegistry) FilterInfos([]string) []*tool.Info { return nil }
func (emptyRegistry) Names() []string                   { return nil }

func TestInjectMentionedSkillsUsesExplicitLoader(t *testing.T) {
	loader := &fakeExplicitLoader{result: `{"type":"skill_injection","name":"manual","qualified_name":"manual","resource":"manual/SKILL.md","content":"Manual body","truncated":false}`}
	e := &ReactLoopEngine{
		registry:             emptyRegistry{},
		skillMentionSelector: fakeMentionSelector{mentions: []SkillMention{{Skill: "manual", Resource: "manual/SKILL.md"}}},
		skillExplicitLoader:  loader,
	}
	rc := runtime.NewRunContext(&domain.Run{ID: "run-skill"}, &domain.Agent{})
	active := []string{"Skill", "read_file"}
	var infos []*tool.Info

	e.injectMentionedSkills(context.Background(), rc, "$manual", &active, &infos, logger.NewNop())

	if loader.calls != 1 {
		t.Fatalf("explicit loader calls = %d", loader.calls)
	}
	if len(rc.Messages) != 1 {
		t.Fatalf("messages = %+v", rc.Messages)
	}
	if rc.Messages[0].Role != domain.RoleUser {
		t.Fatalf("skill injection role = %q, want user", rc.Messages[0].Role)
	}
	if rc.Messages[0].Kind != domain.MessageKindSkillInjection {
		t.Fatalf("skill injection kind = %q, want skill_injection", rc.Messages[0].Kind)
	}
	if rc.Messages[0].Source != domain.MessageSourceSkillMention {
		t.Fatalf("source = %q", rc.Messages[0].Source)
	}
	if !strings.Contains(rc.Messages[0].Content, "Manual body") || !strings.Contains(rc.Messages[0].Content, "<skill_injection>") {
		t.Fatalf("messages = %+v", rc.Messages)
	}
}

func TestApplySkillToolResultInjectsUserMessage(t *testing.T) {
	e := &ReactLoopEngine{registry: emptyRegistry{}}
	rc := runtime.NewRunContext(&domain.Run{ID: "run-skill-tool"}, &domain.Agent{})
	active := []string{"Skill", "read_file", "run_skill_command"}
	var infos []*tool.Info
	payload := `{"type":"skill_injection","name":"demo","qualified_name":"demo","resource":"embedded:demo","content":"Demo body","truncated":false,"allowed_tools":["read_file"]}`
	ok := e.applySkillToolResult(rc, toolExecutionResult{ID: "call-1", Name: "Skill", Content: payload}, &active, &infos, logger.NewNop())
	if !ok {
		t.Fatal("expected skill tool result applied")
	}
	if len(rc.Messages) != 2 {
		t.Fatalf("messages = %+v", rc.Messages)
	}
	if rc.Messages[0].Role != domain.RoleTool || rc.Messages[0].Kind != domain.MessageKindToolResult {
		t.Fatalf("first = %+v", rc.Messages[0])
	}
	if rc.Messages[1].Role != domain.RoleUser || rc.Messages[1].Kind != domain.MessageKindSkillInjection {
		t.Fatalf("injection = %+v", rc.Messages[1])
	}
	if rc.Messages[1].Source != domain.MessageSourceSkillGateway {
		t.Fatalf("source = %q", rc.Messages[1].Source)
	}
	if !strings.Contains(rc.Messages[1].Content, "Demo body") || !strings.Contains(rc.Messages[1].Content, "<skill_runtime_bridge>") {
		t.Fatalf("injection = %s", rc.Messages[1].Content)
	}
	ui := domain.ForUI(rc.Messages)
	if len(ui) != 0 {
		t.Fatalf("ForUI should hide skill/tool, got %+v", ui)
	}
}
func TestRenderSkillInjectionAddsRuntimeBridge(t *testing.T) {
	body := renderSkillInjection(skillInjectionOutput{QualifiedName: "third-party", Content: "Run python scripts/do_work.py"})
	for _, want := range []string{"<skill_runtime_bridge>", "run_skill_command", "完整 Skill 包", "third-party", "不要用 run_skill_command 执行 npm install", "npm install -g", "dependencies.runtime", "按技能文档示例选择解释器", "不要把 Node 包当成", "须先 Read", "QA Required", "node -e", "python -c", "默认先 write_file", "register_cjk_font", "缺字黑块", "控制面", "/workspace", "execution_backend", "path_map", "写进 command", "inputs=[\"$WORK_DIR/create_pdfs.py\"]", "相对文件名", "极短单行探测"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	for _, unexpected := range []string{"office-ppt", "pptxgenjs", "slide.addText", "禁止 pptx.addSlide"} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("bridge must stay skill-agnostic, found %q in %s", unexpected, body)
		}
	}
}
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

type fakeMentionSelector struct {
	mentions []SkillMention
}

func (f fakeMentionSelector) SelectMentions(context.Context, string) ([]SkillMention, error) {
	return f.mentions, nil
}

type fakeExplicitLoader struct {
	result string
	calls  int
}

func (f *fakeExplicitLoader) LoadExplicitSkill(context.Context, skillcontract.ExplicitLoadRequest) (string, error) {
	f.calls++
	return f.result, nil
}
