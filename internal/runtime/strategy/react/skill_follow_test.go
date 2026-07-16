package react

import (
	"encoding/json"
	"strings"
	"testing"

	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
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
	if hint, _ := runObj["delivery_hint"].(string); !strings.Contains(hint, "artifacts[].path") {
		t.Fatalf("expected delivery_hint, got %s", runOut)
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

func TestAnnotateSkillFollowWarnsRedeiveryWrite(t *testing.T) {
	rc := runtime.NewRunContext(&domain.Run{ID: "r2"}, &domain.Agent{})
	_ = annotateSkillFollowHints(rc, "run_skill_command", `{"command":"node build.js"}`,
		`{"ok":true,"produced":["deck.pptx"],"artifacts":[{"name":"deck.pptx","path":"/tmp/out/deck.pptx","kind":"pptx","ok":true}]}`)

	out := annotateSkillFollowHints(rc, "write_file",
		`{"path":"$OUTPUT_DIR/deck.pptx","content":""}`,
		`工具执行失败: invalid_input: 禁止用纯文本冒充 .pptx`)
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatal(err)
	}
	if obj["skill_follow"] != "delivery_complete" {
		t.Fatalf("expected delivery_complete, got %s", out)
	}
	hint, _ := obj["delivery_hint"].(string)
	if !strings.Contains(hint, "artifacts[].path") {
		t.Fatalf("expected delivery_hint, got %s", out)
	}
}

func TestAnnotateSkillFollowWarnsFinalTextWrittenToWork(t *testing.T) {
	rc := runtime.NewRunContext(&domain.Run{ID: "r3"}, &domain.Agent{})
	out := annotateSkillFollowHints(rc, "write_file",
		`{"path":"$WORK_DIR/年度总结.md","content":"summary"}`,
		`{"path":".genesis/runs/r3/work/年度总结.md","size":7}`)
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatal(err)
	}
	if obj["delivery_path_mismatch"] != true {
		t.Fatalf("expected delivery path warning, got %s", out)
	}
	if hint, _ := obj["delivery_path_hint"].(string); !strings.Contains(hint, "$OUTPUT_DIR") {
		t.Fatalf("expected actionable OUTPUT_DIR hint, got %s", out)
	}

	script := annotateSkillFollowHints(rc, "write_file",
		`{"path":"$WORK_DIR/extract_pdf.py","content":"print(1)"}`,
		`{"path":".genesis/runs/r3/work/extract_pdf.py","size":8}`)
	if strings.Contains(script, "delivery_path_mismatch") {
		t.Fatalf("intermediate script should not be treated as a final document: %s", script)
	}

	delivered := annotateSkillFollowHints(rc, "write_file",
		`{"path":"$OUTPUT_DIR/年度总结.md","content":"summary"}`,
		`{"path":".genesis/runs/r3/output/年度总结.md","size":7}`)
	if strings.Contains(delivered, "delivery_path_mismatch") || strings.Contains(delivered, "delivery_complete") {
		t.Fatalf("a new OUTPUT_DIR text deliverable should not be warned: %s", delivered)
	}
}

func TestAnnotateSkillFollowQAFailed(t *testing.T) {
	rc := runtime.NewRunContext(&domain.Run{ID: "r1"}, &domain.Agent{})
	registerSkillInjectionFollow(rc, `## Creating

Read [guide.md](guide.md).

## QA (Required)

`+"```bash\n"+`python scripts/thumbnail.py out.pptx
`+"```\n")
	_ = annotateSkillFollowHints(rc, "read_skill_resource", `{"resource":"guide.md"}`, `{"resource":"guide.md"}`)
	_ = annotateSkillFollowHints(rc, "run_skill_command", `{"command":"node build.js"}`,
		`{"ok":true,"artifacts":[{"name":"out.pptx","path":".genesis/runs/r1/output/out.pptx"}]}`)
	failed := annotateSkillFollowHints(rc, "run_skill_command",
		`{"command":"python scripts/thumbnail.py out.pptx"}`,
		`{"ok":false,"failure_kind":"dependency_missing","error":"soffice not found"}`)
	if !strings.Contains(failed, `"qa_failed":true`) {
		t.Fatalf("expected qa_failed: %s", failed)
	}
	if !strings.Contains(failed, `"qa_failure_kind":"dependency_missing"`) {
		t.Fatalf("expected qa_failure_kind: %s", failed)
	}
	if !rc.SkillFollow.IncompleteDelivery() {
		t.Fatal("expected IncompleteDelivery after failed QA with delivery")
	}
}

func TestApplySkillFollowIncomplete(t *testing.T) {
	rc := runtime.NewRunContext(&domain.Run{ID: "r1"}, &domain.Agent{})
	registerSkillInjectionFollow(rc, `## QA (Required)

`+"```\n"+`python scripts/verify.py out.json
`+"```\n")
	rc.SkillFollow.NoteDeliveredArtifacts([]string{"out.json"})
	if !applySkillFollowIncomplete(rc, logger.NewNop()) {
		t.Fatal("expected incomplete")
	}
	if !rc.Run.Incomplete {
		t.Fatal("Run.Incomplete should be true")
	}
	rc.SkillFollow.NoteExecutedCommand("python scripts/verify.py out.json", true)
	rc.Run.Incomplete = false
	if applySkillFollowIncomplete(rc, logger.NewNop()) {
		t.Fatal("QA done should not mark incomplete")
	}
}
