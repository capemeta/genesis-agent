package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"genesis-agent/internal/runtime"
)

func TestSkillFollowTracksUnreadAndQA(t *testing.T) {
	s := runtime.NewSkillFollowState()
	s.RegisterInjection(`## Creating from Scratch

Read [guide.md](guide.md) completely.

## Design Ideas

Read [design.md](design.md).

## Editing Workflow

Read [editing.md](editing.md).

## QA (Required)

` + "```bash\n" + `python scripts/verify.py output.json
` + "```\n")
	creating := s.UnreadCreatingRequired()
	if len(creating) != 2 || creating[0] != "design.md" || creating[1] != "guide.md" {
		t.Fatalf("creating unread=%v", creating)
	}
	all := s.UnreadRequired()
	if len(all) < 3 {
		t.Fatalf("all unread=%v", all)
	}
	if !s.RequiresQA() {
		t.Fatal("expected RequiresQA")
	}
	cmds := s.QACommands()
	if len(cmds) != 1 || !strings.Contains(cmds[0], "verify.py") {
		t.Fatalf("qa commands=%v", cmds)
	}
	s.MarkResourceRead("demo-skill/guide.md")
	s.MarkResourceRead("demo-skill/design.md")
	if len(s.UnreadCreatingRequired()) != 0 {
		t.Fatalf("creating should be cleared: %v", s.UnreadCreatingRequired())
	}
	if s.IsQACommand("python build.py") {
		t.Fatal("build should not match QA")
	}
	s.NoteExecutedCommand("python scripts/verify.py output.json", false)
	if s.QADone() {
		t.Fatal("failed QA must not mark done")
	}
	s.NoteExecutedCommand("python scripts/verify.py output.json", true)
	if !s.QADone() {
		t.Fatal("expected QA done")
	}
}

func TestSkillFollowBoundsDeterministicQAEnvironmentFailures(t *testing.T) {
	s := runtime.NewSkillFollowState()
	s.RegisterInjection("## QA (Required)\n```bash\npython scripts/thumbnail.py out.pptx\npython scripts/verify.py out.pptx\n```")
	s.NoteQAEnvironmentFailure("python scripts/thumbnail.py out.pptx", "dependency_missing")
	if !s.ShouldBlockQA("python scripts/thumbnail.py out.pptx") {
		t.Fatal("同一确定性环境失败 QA 必须立即阻止重试")
	}
	if s.ShouldBlockQA("python scripts/verify.py out.pptx") {
		t.Fatal("一个 QA 失败不应阻止其他声明 QA")
	}
	s.NoteQAEnvironmentFailure("python scripts/verify.py out.pptx", "unsupported_environment")
	if !s.ShouldBlockQA("python scripts/verify.py out.pptx") {
		t.Fatal("第二个环境失败后 QA 预算应耗尽")
	}
}

func TestSkillFollowReportsActualFailedQACommand(t *testing.T) {
	s := runtime.NewSkillFollowState()
	s.RegisterInjection("## QA (Required)\n```bash\npython scripts/thumbnail.py output.pptx\n```")
	actual := "python scripts/thumbnail.py ultra5-comparison-summary.pptx"
	s.NoteQAEnvironmentFailure(actual, "dependency_missing")
	failed := s.FailedQACommands()
	if len(failed) != 1 || failed[0] != actual {
		t.Fatalf("failed QA commands=%v", failed)
	}
	pending := s.PendingQACommands()
	if len(pending) != 1 || pending[0] != "python scripts/thumbnail.py output.pptx" {
		t.Fatalf("pending template should remain separately auditable: %v", pending)
	}
}

func TestSkillFollowWorkflowSectionPrereq(t *testing.T) {
	s := runtime.NewSkillFollowState()
	s.RegisterInjection(`## Workflow

Read [setup.md](setup.md) before running.

## QA

` + "```\n" + `./bin/check.sh
` + "```\n")
	unread := s.UnreadCreatingRequired()
	if len(unread) != 1 || unread[0] != "setup.md" {
		t.Fatalf("unread=%v", unread)
	}
	if !s.RequiresQA() {
		t.Fatal("expected RequiresQA")
	}
	cmds := s.QACommands()
	if len(cmds) != 1 || cmds[0] != "./bin/check.sh" {
		t.Fatalf("qa commands=%v", cmds)
	}
}

func TestSkillFollowParsesOfficePPTQAWithoutProse(t *testing.T) {
	path := filepath.Join("..", "capabilities", "skill", "adapter", "embedded", "skills", "office-ppt", "SKILL.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("office-ppt SKILL.md not available: %v", err)
	}
	// 模拟 Windows CRLF
	crlf := strings.ReplaceAll(string(raw), "\n", "\r\n")
	s := runtime.NewSkillFollowState()
	s.RegisterInjection(crlf)
	if !s.RequiresQA() {
		t.Fatal("expected RequiresQA")
	}
	cmds := s.QACommands()
	if len(cmds) < 1 {
		t.Fatalf("expected markitdown QA cmds, got %v", cmds)
	}
	for _, c := range cmds {
		if strings.Contains(c, "Visually inspect") || strings.Contains(c, "Overlapping elements") ||
			strings.Contains(c, "Check for missing") || strings.HasPrefix(c, "bash") {
			t.Fatalf("prose leaked into QA commands: %q", c)
		}
		if !strings.Contains(c, "markitdown") && !strings.Contains(c, "thumbnail.py") {
			t.Fatalf("unexpected QA cmd: %q", c)
		}
	}
	if !s.IsQACommand("python -m markitdown out.pptx") {
		t.Fatalf("should match markitdown with different filename; cmds=%v", cmds)
	}
	if !s.IsQACommand("python scripts/thumbnail.py out.pptx") {
		t.Fatalf("should match thumbnail visual QA; cmds=%v", cmds)
	}
	if s.IsQACommand("node create.js") {
		t.Fatal("node create must not count as QA")
	}
	if s.IsQACommand("python") {
		t.Fatal("bare python must not count as QA")
	}
	prereq := s.UnreadCreatingRequired()
	joined := strings.Join(prereq, ",")
	if !strings.Contains(joined, "pptxgenjs.md") || !strings.Contains(joined, "design.md") {
		t.Fatalf("expected pptxgenjs.md + design.md as prereqs, got %v", prereq)
	}
	s.NoteExecutedCommand("python -m markitdown out.pptx", true)
	if s.QADone() {
		t.Fatal("content QA alone must not clear visual QA pending")
	}
	s.NoteExecutedCommand("python scripts/thumbnail.py out.pptx", true)
	if !s.QADone() {
		t.Fatal("markitdown + thumbnail should complete required QA")
	}
}
