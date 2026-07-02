package freshness

import (
	"context"
	"runtime"
	"testing"
	"time"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
)

func TestMemoryTrackerDetectsModifiedHash(t *testing.T) {
	tracker := NewMemoryTracker()
	path := model.ResolvedPath{DisplayPath: "a.txt", BackendPath: "a.txt"}
	stat := model.FileStat{Path: path, Size: 3, ModifiedAt: time.Now()}

	if err := tracker.RecordRead(context.Background(), path, stat, "old"); err != nil {
		t.Fatal(err)
	}
	_, err := tracker.CheckBeforeWrite(context.Background(), path, stat, "new", "")
	if fscontract.CodeOf(err) != fscontract.ErrCodeModifiedExternally {
		t.Fatalf("error code = %q, want modified", fscontract.CodeOf(err))
	}
}

func TestMemoryTrackerExpectedHashWins(t *testing.T) {
	tracker := NewMemoryTracker()
	path := model.ResolvedPath{DisplayPath: "a.txt", BackendPath: "a.txt"}
	stat := model.FileStat{Path: path, Size: 3, ModifiedAt: time.Now()}

	check, err := tracker.CheckBeforeWrite(context.Background(), path, stat, "expected", "expected")
	if err != nil {
		t.Fatal(err)
	}
	if !check.Fresh {
		t.Fatal("Fresh = false, want true")
	}
}

func TestMemoryTrackerRejectsExistingFileWithoutReadRecord(t *testing.T) {
	tracker := NewMemoryTracker()
	path := model.ResolvedPath{DisplayPath: "a.txt", BackendPath: "a.txt"}
	stat := model.FileStat{Path: path, Size: 3, ModifiedAt: time.Now()}

	_, err := tracker.CheckBeforeWrite(context.Background(), path, stat, "hash", "")
	if fscontract.CodeOf(err) != fscontract.ErrCodeModifiedExternally {
		t.Fatalf("error code = %q, want modified", fscontract.CodeOf(err))
	}
}

func TestMemoryTrackerKeyCaseSensitivityFollowsPlatform(t *testing.T) {
	upper := model.ResolvedPath{BackendPath: "A.txt"}
	lower := model.ResolvedPath{BackendPath: "a.txt"}
	if runtime.GOOS == "windows" {
		if key(upper) != key(lower) {
			t.Fatalf("windows keys should be case-insensitive: %q != %q", key(upper), key(lower))
		}
		return
	}
	if key(upper) == key(lower) {
		t.Fatalf("non-windows keys should preserve case: %q == %q", key(upper), key(lower))
	}
}
