package list_dir

import (
	"encoding/json"
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/filesystem/model"
)

func TestFilterEntriesByType(t *testing.T) {
	entries := []model.DirEntry{
		{Name: "src", Type: model.EntryTypeDir},
		{Name: "README.md", Type: model.EntryTypeFile},
	}
	got, err := filterEntries(entries, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "src" {
		t.Fatalf("filterEntries() = %+v", got)
	}
}

func TestFilterEntriesRejectsUnknownType(t *testing.T) {
	if _, err := filterEntries(nil, "directory"); err == nil {
		t.Fatal("err = nil, want invalid input")
	}
}

func TestBuildOutputNamesIncludesAuthoritativeCount(t *testing.T) {
	entries := []model.DirEntry{
		{Name: "a", Type: model.EntryTypeDir},
		{Name: "b", Type: model.EntryTypeDir},
	}
	got := buildOutput("D:/", "dir", "names", entries, false)
	if got.ReturnedCount != 2 || got.Truncated || got.Names == nil || len(*got.Names) != 2 || (*got.Names)[1] != "b" || got.Entries != nil {
		t.Fatalf("buildOutput() = %+v", got)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(data)
	if !strings.Contains(jsonText, `"returned_count":2`) || !strings.Contains(jsonText, `"names":["a","b"]`) || strings.Contains(jsonText, "modified_at") {
		t.Fatalf("json = %s", jsonText)
	}
}

func TestTruncateEntriesUsesProbeEntry(t *testing.T) {
	entries := []model.DirEntry{{Name: "a"}, {Name: "b"}, {Name: "probe"}}
	got, truncated := truncateEntries(entries, 2)
	if !truncated || len(got) != 2 || got[1].Name != "b" {
		t.Fatalf("truncateEntries() = %+v, %t", got, truncated)
	}
}

func TestBuildOutputCompactOmitsFullMetadata(t *testing.T) {
	entries := []model.DirEntry{{Name: "a", Path: "D:/a", Type: model.EntryTypeDir, Size: 42}}
	got := buildOutput("D:/", "dir", "compact", entries, true)
	compact, ok := got.Entries.([]compactEntry)
	if !ok || len(compact) != 1 || compact[0].Name != "a" || got.ReturnedCount != 1 || !got.Truncated {
		t.Fatalf("buildOutput() = %+v", got)
	}
}

func TestNormalizeMaxEntriesReservesTruncationProbe(t *testing.T) {
	if got, err := normalizeMaxEntries(0); err != nil || got != defaultMaxEntries {
		t.Fatalf("normalizeMaxEntries(0) = %d, %v", got, err)
	}
	if _, err := normalizeMaxEntries(maxEntriesLimit + 1); err == nil {
		t.Fatal("err = nil, want invalid input")
	}
}

func TestNormalizeDetailRejectsUnknownValue(t *testing.T) {
	if _, err := normalizeDetail("verbose"); err == nil {
		t.Fatal("err = nil, want invalid input")
	}
}
