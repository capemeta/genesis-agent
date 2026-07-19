package grep

import (
	"encoding/json"
	"testing"
)

func TestGrepEmptyMatchesContract(t *testing.T) {
	out := output{
		OK:      true,
		Root:    ".",
		Pattern: "lorem|ipsum",
		Matches: make([]match, 0),
	}
	out.MatchCount = len(out.Matches)
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		OK         bool    `json:"ok"`
		Matches    []match `json:"matches"`
		MatchCount int     `json:"match_count"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.MatchCount != 0 || got.Matches == nil || len(got.Matches) != 0 {
		t.Fatalf("empty grep result = %+v (raw=%s)", got, string(raw))
	}
}
