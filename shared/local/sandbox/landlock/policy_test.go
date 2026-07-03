package landlock

import "testing"

func TestEvaluateSupportRejectsNetworkAndDenyRead(t *testing.T) {
	res := EvaluateSupport(FileSystemPolicy{AllowFullDiskRead: true, UnreadablePaths: []string{"/secret"}}, NetworkDisabled)
	if res.Supported || len(res.Reasons) < 2 {
		t.Fatalf("expected unsupported with reasons, got %#v", res)
	}
}

func TestEvaluateSupportAllowsWriteRestrictedReadFullDisk(t *testing.T) {
	res := EvaluateSupport(FileSystemPolicy{AllowFullDiskRead: true, WritableRoots: []string{"/work"}}, NetworkFullAccess)
	if !res.Supported {
		t.Fatalf("expected supported, got %#v", res)
	}
}
