package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureUserSkillsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	if err := EnsureUserSkillsDir(); err != nil {
		t.Fatalf("EnsureUserSkillsDir: %v", err)
	}
	dir := filepath.Join(home, ".genesis-agent", "cli", "skills")
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("user skills dir missing: info=%v err=%v", info, err)
	}
}
