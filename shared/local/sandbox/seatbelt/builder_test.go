package seatbelt

import (
	"strings"
	"testing"
)

func TestBuildUsesFixedSandboxExecPathAndPreservesArgv(t *testing.T) {
	plan, err := Build(BuildOptions{Command: CommandSpec{Argv: []string{"/bin/echo", "hello world", "-n"}}, Network: NetworkDisabled})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Program != Path {
		t.Fatalf("Program = %s, want %s", plan.Program, Path)
	}
	if len(plan.Args) < 5 || plan.Args[len(plan.Args)-3] != "/bin/echo" || plan.Args[len(plan.Args)-2] != "hello world" || plan.Args[len(plan.Args)-1] != "-n" {
		t.Fatalf("argv not preserved: %#v", plan.Args)
	}
}

func TestBuildProfileIncludesProtectedMetadataDeny(t *testing.T) {
	profile := BuildProfile(FileSystemPolicy{WritableRoots: []string{"/work"}, ProtectedMetadataPaths: []string{"/work/.git"}}, NetworkDisabled, nil, nil)
	if !strings.Contains(profile, "deny file-write*") || !strings.Contains(profile, "/work/.git") {
		t.Fatalf("profile missing protected metadata deny:\n%s", profile)
	}
}
