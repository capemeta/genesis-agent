package bubblewrap

import (
	"path/filepath"
	"testing"
)

func TestBuildPreservesArgvAndAddsFilesystemArgs(t *testing.T) {
	plan, err := Build(BuildOptions{BwrapPath: "/usr/bin/bwrap", Command: []string{"/bin/echo", "hello world", "-n"}, WritableRoots: []string{"/work"}, ReadOnlyRoots: []string{"/work/.git"}, DenyReadPaths: []PathMask{{Path: "/secret", Kind: PathMaskDir}}, Network: NetworkDisabled, MountProc: true})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	joined := stringsJoin(plan.Args)
	for _, want := range []string{"--ro-bind", "/", "--bind", "/work", "--tmpfs", "/secret", "--unshare-net", "--"} {
		if !containsArg(plan.Args, want) {
			t.Fatalf("args missing %s: %s", want, joined)
		}
	}
	if plan.Args[len(plan.Args)-3] != "/bin/echo" || plan.Args[len(plan.Args)-2] != "hello world" || plan.Args[len(plan.Args)-1] != "-n" {
		t.Fatalf("argv not preserved: %#v", plan.Args)
	}
}

func TestIsTrustedHelperPathRejectsWorkspace(t *testing.T) {
	workspace := filepath.Join("C:", "work", "repo")
	helper := filepath.Join(workspace, "bin", "bwrap.exe")
	ok, reason := IsTrustedHelperPath(helper, HelperTrustOptions{WorkspaceRoots: []string{workspace}})
	if ok || reason == "" {
		t.Fatalf("expected helper rejection, ok=%v reason=%q", ok, reason)
	}
}

func TestBuildNoNewPrivsAddsFlag(t *testing.T) {
	plan, err := Build(BuildOptions{
		BwrapPath:  "/usr/bin/bwrap",
		Command:    []string{"/bin/echo"},
		NoNewPrivs: true,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !containsArg(plan.Args, "--no-new-privs") {
		t.Fatalf("expected --no-new-privs in args: %v", plan.Args)
	}
}

func TestBuildWithoutNoNewPrivsNoFlag(t *testing.T) {
	plan, err := Build(BuildOptions{
		BwrapPath:  "/usr/bin/bwrap",
		Command:    []string{"/bin/echo"},
		NoNewPrivs: false,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if containsArg(plan.Args, "--no-new-privs") {
		t.Fatalf("unexpected --no-new-privs in args: %v", plan.Args)
	}
}

func TestBuildProxyEndpointPreservedButNotInArgv(t *testing.T) {
	// ProxyEndpoint 是预留字段，当前不写入 argv
	plan, err := Build(BuildOptions{
		BwrapPath:     "/usr/bin/bwrap",
		Command:       []string{"/bin/echo"},
		Network:       NetworkProxyOnly,
		ProxyEndpoint: "127.0.0.1:8118",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	joined := stringsJoin(plan.Args)
	if containsArg(plan.Args, "127.0.0.1:8118") {
		t.Fatalf("ProxyEndpoint should NOT appear in argv until bridge impl: %s", joined)
	}
	// network-only mode 应有 --unshare-net
	if !containsArg(plan.Args, "--unshare-net") {
		t.Fatalf("expected --unshare-net for proxy_only: %s", joined)
	}
}


func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func stringsJoin(args []string) string {
	out := ""
	for _, arg := range args {
		out += arg + " "
	}
	return out
}
