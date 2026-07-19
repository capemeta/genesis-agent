package service

import "testing"

func TestParseVisualChecklist(t *testing.T) {
	t.Parallel()
	got, ok := ParseVisualChecklist(`done [VISUAL_CHECKLIST: layout=ok, contrast=ok, overflow=none]`)
	if !ok || !got.Passed {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	bad, ok := ParseVisualChecklist(`[VISUAL_CHECKLIST: layout=ok, contrast=reject, overflow=none]`)
	if !ok || bad.Passed {
		t.Fatalf("reject should fail: %+v", bad)
	}
	if _, ok := ParseVisualChecklist("no checklist"); ok {
		t.Fatal("expected miss")
	}
}

func TestParseExpertVisualJSON(t *testing.T) {
	t.Parallel()
	got, ok := ParseExpertVisualJSON(`here {"passed":true,"defects":[]} end`)
	if !ok || !got.Passed {
		t.Fatalf("%+v", got)
	}
	fail, ok := ParseExpertVisualJSON(`{"passed":true,"defects":["gap"]}`)
	if !ok || fail.Passed {
		t.Fatalf("%+v", fail)
	}
}
