package repeatguard

import (
	"strings"
	"sync"
	"testing"

	"genesis-agent/internal/domain"
)

func TestTruncatedJSONCallsShareStableIdentity(t *testing.T) {
	first := BuildCallKey("write_file", `{"path":"a.js","content":"first`, PathRoots{}, nil)
	second := BuildCallKey("write_file", `{"path":"b.js","content":"different`, PathRoots{}, nil)
	if first.CallKey != second.CallKey {
		t.Fatalf("truncated calls should share identity: %s != %s", first.CallKey, second.CallKey)
	}
}

func TestNormalizeArgsStableAndIgnoresNoise(t *testing.T) {
	a := `{"b":"  x ","a":1,"request_id":"r1","nonce":"n"}`
	b := `{"a":1,"b":"x","trace_id":"t"}`
	ka := BuildCallKey("run_skill_command", a, PathRoots{}, nil)
	kb := BuildCallKey("run_skill_command", b, PathRoots{}, nil)
	if ka.CallKey != kb.CallKey {
		t.Fatalf("expected same call key, got %s vs %s\n%q\n%q", ka.CallKey, kb.CallKey, ka.Canonical, kb.Canonical)
	}
}

func TestNormalizeArgsPathRewrite(t *testing.T) {
	roots := PathRoots{OutputDir: `D:\ws\output`, WorkDir: `D:\ws`}
	ka := BuildCallKey("write_file", `{"path":"D:\\ws\\output\\deck.pptx"}`, roots, nil)
	kb := BuildCallKey("write_file", `{"path":"D:/ws/output/deck.pptx"}`, roots, nil)
	if ka.CallKey != kb.CallKey {
		t.Fatalf("path rewrite mismatch: %s vs %s (%q / %q)", ka.CallKey, kb.CallKey, ka.Canonical, kb.Canonical)
	}
	if !strings.Contains(ka.Canonical, "$OUTPUT_DIR") {
		t.Fatalf("expected logical prefix in canonical: %q", ka.Canonical)
	}
}

func TestNormalizeArgsDifferentArgsDifferentKey(t *testing.T) {
	ka := BuildCallKey("run_skill_command", `{"script":"a.js"}`, PathRoots{}, nil)
	kb := BuildCallKey("run_skill_command", `{"script":"b.js"}`, PathRoots{}, nil)
	if ka.CallKey == kb.CallKey {
		t.Fatal("expected different keys for different args")
	}
}

func TestL1BlocksThirdIdenticalFailure(t *testing.T) {
	g := New(Config{Enabled: true, MaxIdenticalToolFailures: 2, MaxStagnantIterations: 0})
	args := `{"script":"create_pptx.js","skill":"office-ppt"}`
	fail := `{"ok":false,"failure_kind":"path_contract_violation","error":"bad path"}`

	for i := 0; i < 2; i++ {
		if g.Check("run_skill_command", args, nil).Blocked {
			t.Fatalf("attempt %d should not block", i+1)
		}
		g.Record("run_skill_command", args, fail, nil, nil)
	}
	check := g.Check("run_skill_command", args, nil)
	if !check.Blocked {
		t.Fatal("third identical call should be blocked")
	}
	if !strings.Contains(check.Content, `"failure_kind":"repeated_failure"`) {
		t.Fatalf("unexpected content: %s", check.Content)
	}
	// 拦截路径不 Record；再次 Check 仍拦截
	if !g.Check("run_skill_command", args, nil).Blocked {
		t.Fatal("should remain blocked without success/clear")
	}
}

func TestChangedArgsNotBlocked(t *testing.T) {
	g := New(Config{Enabled: true, MaxIdenticalToolFailures: 2, MaxStagnantIterations: 0})
	fail := `{"ok":false,"failure_kind":"path_contract_violation"}`
	g.Record("run_skill_command", `{"script":"a.js"}`, fail, nil, nil)
	g.Record("run_skill_command", `{"script":"a.js"}`, fail, nil, nil)
	if g.Check("run_skill_command", `{"script":"b.js"}`, nil).Blocked {
		t.Fatal("different args must be allowed")
	}
}

func TestInstallSuccessClearsDependencyMissing(t *testing.T) {
	g := New(Config{Enabled: true, MaxIdenticalToolFailures: 2, MaxStagnantIterations: 0})
	args := `{"script":"create_pptx.js","skill":"office-ppt"}`
	fail := `{"ok":false,"failure_kind":"dependency_missing","suggested_action":"install_then_retry"}`
	g.Record("run_skill_command", args, fail, nil, nil)
	g.Record("run_skill_command", args, fail, nil, nil)
	if !g.Check("run_skill_command", args, nil).Blocked {
		t.Fatal("expected blocked before install")
	}
	g.Record("install_skill_dependencies", `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}]}`,
		`{"ok":true,"skill":"office-ppt"}`, nil, nil)
	if g.Check("run_skill_command", args, nil).Blocked {
		t.Fatal("same args should be allowed after install clear")
	}
}

func TestProgressGateInjectAndPartialComplete(t *testing.T) {
	g := New(Config{Enabled: true, MaxIdenticalToolFailures: 2, MaxStagnantIterations: 2})

	// 预热：登记 failure_kind，避免首轮「新 kind」算进展
	g.BeginIteration()
	g.Record("run_skill_command", `{"script":"seed.js"}`, `{"ok":false,"failure_kind":"path_contract_violation"}`, nil, nil)
	if dec := g.EndIteration(0, false); !dec.HadProgress {
		t.Fatal("seed new kind should be progress")
	}

	// 两轮：换参但同 kind → 无进展 → 第 2 轮达阈值注入 no_progress
	for i := 1; i <= 2; i++ {
		g.BeginIteration()
		args := `{"script":"a.js","n":` + string(rune('0'+i)) + `}`
		g.Record("run_skill_command", args, `{"ok":false,"failure_kind":"path_contract_violation"}`, nil, nil)
		dec := g.EndIteration(i, false)
		if dec.HadProgress {
			t.Fatalf("iter %d should not have progress", i)
		}
		if i < 2 && dec.InjectNoProgress {
			t.Fatal("too early inject")
		}
		if i == 2 && !dec.InjectNoProgress {
			t.Fatalf("expected inject at iter 2, got %+v", dec)
		}
	}

	g.BeginIteration()
	g.Record("run_skill_command", `{"script":"a.js","n":9}`, `{"ok":false,"failure_kind":"path_contract_violation"}`, nil, nil)
	dec := g.EndIteration(3, false)
	if !dec.PartialComplete {
		t.Fatalf("expected partial_complete, got %+v", dec)
	}
}

func TestParallelRecordLocked(t *testing.T) {
	g := New(Config{Enabled: true, MaxIdenticalToolFailures: 2, MaxStagnantIterations: 0})
	args := `{"x":1}`
	fail := `{"ok":false,"failure_kind":"tool_error"}`
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g.Check("t", args, nil)
			g.Record("t", args, fail, nil, nil)
		}()
	}
	wg.Wait()
	if !g.Check("t", args, nil).Blocked {
		t.Fatal("expected blocked after many parallel failures")
	}
}

func TestApprovalDeniedClear(t *testing.T) {
	g := New(Config{Enabled: true, MaxIdenticalToolFailures: 1, MaxStagnantIterations: 0})
	args := `{"skill":"office-ppt"}`
	g.Record("install_skill_dependencies", args, `{"ok":false,"failure_kind":"approval_denied"}`, nil, nil)
	if !g.Check("install_skill_dependencies", args, nil).Blocked {
		t.Fatal("expected block")
	}
	g.ClearApprovalDenied()
	if g.Check("install_skill_dependencies", args, nil).Blocked {
		t.Fatal("expected allow after approval clear")
	}
}

func TestConfigFromPolicyDefaults(t *testing.T) {
	cfg := ConfigFromPolicy(domain.RuntimePolicy{})
	if !cfg.Enabled || cfg.MaxIdenticalToolFailures != 2 || cfg.MaxStagnantIterations != 5 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	zero := 0
	off := false
	cfg = ConfigFromPolicy(domain.RuntimePolicy{
		RepeatGuardEnabled:       &off,
		MaxIdenticalToolFailures: &zero,
		MaxStagnantIterations:    &zero,
	})
	if cfg.Enabled || cfg.MaxIdenticalToolFailures != 0 || cfg.MaxStagnantIterations != 0 {
		t.Fatalf("explicit off not respected: %+v", cfg)
	}
}

func TestParseOutcomeOKFalse(t *testing.T) {
	o := ParseOutcome("t", `{"ok":false,"failure_kind":"dependency_missing"}`, nil)
	if o.Success || o.FailureKind != "dependency_missing" {
		t.Fatalf("%+v", o)
	}
}

func TestL1TightenedAfterNoProgress(t *testing.T) {
	g := New(Config{Enabled: true, MaxIdenticalToolFailures: 2, MaxStagnantIterations: 1})
	g.BeginIteration()
	dec := g.EndIteration(0, false)
	if !dec.InjectNoProgress || !dec.L1Tightened {
		t.Fatalf("%+v", dec)
	}
	args := `{"a":1}`
	fail := `{"ok":false,"failure_kind":"tool_error"}`
	g.BeginIteration()
	g.Record("t", args, fail, nil, nil)
	if !g.Check("t", args, nil).Blocked {
		t.Fatal("tightened L1 should block after 1 failure")
	}
}

func TestResetClearsAll(t *testing.T) {
	g := New(Config{Enabled: true, MaxIdenticalToolFailures: 1, MaxStagnantIterations: 0})
	g.Record("t", `{"a":1}`, `{"ok":false,"failure_kind":"tool_error"}`, nil, nil)
	g.Reset()
	if g.Check("t", `{"a":1}`, nil).Blocked {
		t.Fatal("reset should clear blocks")
	}
}
