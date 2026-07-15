package parser_test

import (
	"strings"
	"testing"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	marketparser "genesis-agent/internal/capabilities/package/marketplace/parser"
)

func TestParseGitHubTreeURL(t *testing.T) {
	src, err := marketparser.ParseRemote("https://github.com/org/repo/tree/main/skills/foo")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != marketmodel.SourceTypeGitHub || src.Repo != "org/repo" || src.Ref != "main" || src.SubPath != "skills/foo" {
		t.Fatalf("unexpected: %+v", src)
	}
}

func TestParseGitHubBlobSKILLURL(t *testing.T) {
	src, err := marketparser.ParseRemote("https://github.com/org/repo/blob/v1/skills/foo/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if src.Repo != "org/repo" || src.Ref != "v1" || src.SubPath != "skills/foo" {
		t.Fatalf("unexpected: %+v", src)
	}
}

func TestParseGitHubShorthand(t *testing.T) {
	src, err := marketparser.ParseRemote("org/repo@v1#skills/foo")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != marketmodel.SourceTypeGitHub || src.Repo != "org/repo" || src.Ref != "v1" || src.SubPath != "skills/foo" {
		t.Fatalf("unexpected: %+v", src)
	}
}

func TestParseRejectsParentPath(t *testing.T) {
	if _, err := marketparser.ParseRemote("org/repo#../escape"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseGitHubCompatibleHost(t *testing.T) {
	src, err := marketparser.ParseRemoteWith(
		"https://git.example.com/org/repo/tree/main/skills/foo",
		marketparser.Options{GitHosts: []string{"github.com", "git.example.com"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != marketmodel.SourceTypeGitHub || src.Host != "git.example.com" || src.Repo != "org/repo" || src.Ref != "main" || src.SubPath != "skills/foo" {
		t.Fatalf("unexpected: %+v", src)
	}
}

func TestParseOpenSkillsDownloadAsURL(t *testing.T) {
	src, err := marketparser.ParseRemoteWith(
		"https://openskills.cc/api/download?slug=anthropics-skills-frontend-design&locale=zh&source=copy",
		marketparser.Options{GitHosts: []string{"github.com", "openskills.cc"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != marketmodel.SourceTypeURL {
		t.Fatalf("expected url type for download API, got %+v", src)
	}
	if !strings.Contains(src.URL, "openskills.cc/api/download") {
		t.Fatalf("url=%s", src.URL)
	}
}

func TestParseForgeRequiresTreeOrExactRepo(t *testing.T) {
	// 无 tree 的深层路径不再当 forge
	src, err := marketparser.ParseRemote("https://github.com/org/repo/skills/foo")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != marketmodel.SourceTypeURL {
		t.Fatalf("expected url, got %+v", src)
	}
	// 标准 tree 仍为 forge
	src, err = marketparser.ParseRemote("https://github.com/org/repo/tree/main/skills/foo")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != marketmodel.SourceTypeGitHub || src.SubPath != "skills/foo" {
		t.Fatalf("unexpected: %+v", src)
	}
}

