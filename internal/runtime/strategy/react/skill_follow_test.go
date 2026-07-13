package react

import (
	"encoding/json"
	"strings"
	"testing"

	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime"
)

func TestAnnotateSkillFollowPrerequisitesAndQA(t *testing.T) {
	rc := runtime.NewRunContext(&domain.Run{ID: "r1"}, &domain.Agent{})
	registerSkillInjectionFollow(rc, `## Creating from Scratch

Read [guide.md](guide.md) before create.

## Design Ideas
Read [design.md](design.md).

## QA (Required)

`+"```bash\n"+`python scripts/verify.py out.json
`+"```\n")

	wrote := annotateSkillFollowHints(rc, "write_file", `{"path":"$WORK_DIR/build.py","content":"x"}`, `{"path":"build.py","size":1}`)
	var wroteObj map[string]any
	if err := json.Unmarshal([]byte(wrote), &wroteObj); err != nil {
		t.Fatal(err)
	}
	unread, _ := wroteObj["prerequisites_unread"].([]any)
	got := map[string]bool{}
	for _, u := range unread {
		if s, ok := u.(string); ok {
			got[s] = true
		}
	}
	if len(unread) != 2 || !got["guide.md"] || !got["design.md"] {
		t.Fatalf("expected guide.md + design.md unread, got %s", wrote)
	}
	if wroteObj["warning"] == nil || wroteObj["skill_follow"] != "prerequisites_unread" {
		t.Fatalf("expected prominent soft-gate warning, got %s", wrote)
	}

	_ = annotateSkillFollowHints(rc, "read_skill_resource", `{"name":"demo-skill","resource":"demo-skill/guide.md"}`, `{"resource":"demo-skill/guide.md","content":"..."}`)
	_ = annotateSkillFollowHints(rc, "read_skill_resource", `{"name":"demo-skill","resource":"demo-skill/design.md"}`, `{"resource":"demo-skill/design.md","content":"..."}`)
	wrote2 := annotateSkillFollowHints(rc, "write_file", `{"path":"$WORK_DIR/build.py"}`, `{"ok":true}`)
	if strings.Contains(wrote2, "prerequisites_unread") {
		t.Fatalf("should clear unread after read: %s", wrote2)
	}

	runOut := annotateSkillFollowHints(rc, "run_skill_command", `{"skill":"demo-skill","command":"python build.py"}`, `{"ok":true,"produced":["out.json"],"artifacts":[{"name":"out.json","path":"/tmp/out.json","kind":"json","ok":true}]}`)
	var runObj map[string]any
	if err := json.Unmarshal([]byte(runOut), &runObj); err != nil {
		t.Fatal(err)
	}
	if runObj["qa_pending"] != true {
		t.Fatalf("expected qa_pending, got %s", runOut)
	}
	hint, _ := runObj["qa_hint"].(string)
	if !strings.Contains(hint, "verify.py") {
		t.Fatalf("qa_hint should list skill QA command, got %s", hint)
	}

	qaOut := annotateSkillFollowHints(rc, "run_skill_command", `{"command":"python scripts/verify.py out.json"}`, `{"ok":true}`)
	if strings.Contains(qaOut, "prerequisites_unread") {
		t.Fatalf("QA command should not get prerequisites hint: %s", qaOut)
	}
	runOut2 := annotateSkillFollowHints(rc, "run_skill_command", `{"command":"python build.py"}`, `{"ok":true,"produced":["out.json"],"artifacts":[{"name":"out.json","kind":"json"}]}`)
	if strings.Contains(runOut2, `"qa_pending":true`) {
		t.Fatalf("qa should be cleared: %s", runOut2)
	}
}
