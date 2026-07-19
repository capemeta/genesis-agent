package prompt

import (
	"strings"
	"testing"
)

func TestComposeChildSystem(t *testing.T) {
	got := ComposeChildSystem("你是探索专家。", RuntimeContractInput{
		ReadOnly: true, Capabilities: []string{"read_file", "grep"}, MaxTurns: 8, PathFormat: "workspace-relative",
	})
	for _, want := range []string{
		"独立子智能体",
		"InheritedRuntimeContract",
		"只读",
		"read_file, grep",
		"max_turns=8",
		"角色说明",
		"你是探索专家。",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDelegationUserInput(t *testing.T) {
	got := RenderDelegationUserInput(EnvelopeView{
		Objective: "查配置",
		Background: []BackgroundMessage{
			{Role: "user", Content: "请检查"},
		},
	})
	for _, want := range []string{"[背景 user]", "请检查", BoundaryMessage, "[委派任务]", "查配置", "[期望输出]", "[回传约束]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestParseAgentMentions(t *testing.T) {
	got := ParseAgentMentions("请 @run-agent-explore 再看一眼，也可 @agent-plan，重复 @run-agent-explore")
	if len(got) != 2 || got[0] != "explore" || got[1] != "plan" {
		t.Fatalf("unexpected mentions: %#v", got)
	}
}

func TestAgentMentionReminder(t *testing.T) {
	got := AgentMentionReminder("explore", "run-agent-explore")
	for _, want := range []string{"<system-reminder>", `subagent_type="explore"`, "Task", "勿向用户复述"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestUnknownAgentMentionReminder(t *testing.T) {
	got := UnknownAgentMentionReminder("nope", "run-agent-nope")
	for _, want := range []string{"不存在", "不要调用 Task", `subagent_type="nope"`, "勿向用户复述"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "必须调用 Task") {
		t.Fatalf("unknown reminder must not force Task:\n%s", got)
	}
}

func TestSkillForkDefinitionName(t *testing.T) {
	if SkillForkDefinitionName("office-ppt") != "skill-fork:office-ppt" {
		t.Fatal(SkillForkDefinitionName("office-ppt"))
	}
}
