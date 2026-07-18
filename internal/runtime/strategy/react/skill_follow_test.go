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
	registerSkillInjectionFollow(rc, "## Creating from Scratch\nRead [guide.md](guide.md) before create.\n## QA (Required)\n```bash\npython scripts/verify.py out.json\n```")
	wrote := annotateSkillFollowHints(rc, "write_file", `{"path":"$WORK_DIR/build.py"}`, `{"ok":true}`)
	var payload map[string]any
	if err := json.Unmarshal([]byte(wrote), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["skill_follow"] != "prerequisites_unread" {
		t.Fatalf("expected prerequisite hint: %s", wrote)
	}
	_ = annotateSkillFollowHints(rc, "read_skill_resource", `{"resource":"guide.md"}`, `{"resource":"guide.md"}`)
	produced := annotateSkillFollowHints(rc, "run_skill_command", `{"command":"python build.py"}`, `{"ok":true,"produced":[{"candidate_id":"p1","name":"out.json","media_type":"application/json"}]}`)
	if !strings.Contains(produced, `"qa_pending":true`) {
		t.Fatalf("expected qa pending: %s", produced)
	}
	if strings.Contains(produced, "publish_artifact") || strings.Contains(produced, "run:/") {
		t.Fatalf("不得恢复旧发布路径提示: %s", produced)
	}
	_ = annotateSkillFollowHints(rc, "run_skill_command", `{"command":"python scripts/verify.py out.json"}`, `{"ok":true}`)
	after := annotateSkillFollowHints(rc, "run_skill_command", `{"command":"python build.py"}`, `{"ok":true}`)
	if strings.Contains(after, `"qa_pending":true`) {
		t.Fatalf("QA 成功后应清除提示: %s", after)
	}
}

func TestAnnotateSkillFollowTracksQAEnvironmentFailureWithTrailingSummary(t *testing.T) {
	rc := runtime.NewRunContext(&domain.Run{ID: "r2"}, &domain.Agent{})
	registerSkillInjectionFollow(rc, "## QA (Required)\n```bash\npython scripts/thumbnail.py out.pptx\n```")
	content := `{"ok":false,"failure_kind":"dependency_missing","error":"soffice not found"}` + "\n工具执行失败: command failed"
	out := annotateSkillFollowHints(rc, "run_skill_command", `{"command":"python scripts/thumbnail.py out.pptx"}`, content)
	if !strings.Contains(out, `"qa_failed":true`) || !rc.SkillFollow.ShouldBlockQA("python scripts/thumbnail.py out.pptx") {
		t.Fatalf("QA 失败应被结构化记录: %s", out)
	}
}

func TestToolTimingMetadataExtractsSkillPhasesOnly(t *testing.T) {
	result := `{"ok":true,"duration_ms":3749,"approval_duration_ms":2000,"staging_duration_ms":500,"execution_duration_ms":1100}`
	got := toolTimingMetadata("run_skill_command", result)
	if got["duration_ms"] != "3749" || got["approval_duration_ms"] != "2000" || got["staging_duration_ms"] != "500" || got["execution_duration_ms"] != "1100" {
		t.Fatalf("unexpected timing: %+v", got)
	}
	if other := toolTimingMetadata("run_command", result); other != nil {
		t.Fatalf("非 Skill 工具不应套用阶段耗时: %+v", other)
	}
}
