package collision

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRewriteArgsDropsForgedJSON(t *testing.T) {
	got := RewriteArgs("office-ppt", `{"action":"create","slides":3}`)
	var payload map[string]string
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["skill"] != "office-ppt" {
		t.Fatalf("skill = %q", payload["skill"])
	}
	if _, ok := payload["args"]; ok {
		t.Fatalf("forged JSON args should be dropped: %v", payload)
	}
}

func TestRewriteArgsKeepsPlainString(t *testing.T) {
	got := RewriteArgs("office-ppt", "make a comparison deck")
	if !strings.Contains(got, `"args":"make a comparison deck"`) {
		t.Fatalf("got = %s", got)
	}
}
