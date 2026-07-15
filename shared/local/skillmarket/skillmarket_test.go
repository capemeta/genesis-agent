package skillmarket

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	capmodel "genesis-agent/internal/capabilities/capability/model"
	marketcontract "genesis-agent/internal/capabilities/package/marketplace/contract"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	marketservice "genesis-agent/internal/capabilities/package/marketplace/service"
)

func TestParserParsesGitHubShorthand(t *testing.T) {
	source, err := NewParser().Parse("owner/repo@v1#market")
	if err != nil {
		t.Fatal(err)
	}
	if source.Type != marketmodel.SourceTypeGitHub || source.Repo != "owner/repo" || source.Ref != "v1" || source.SubPath != "market" {
		t.Fatalf("unexpected source: %+v", source)
	}
}

func TestReadMarketplaceRejectsUnsafeCapabilityPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".genesis", "marketplace.json"), `{"name":"bad","packages":[{"name":"bad-pack","type":"skill-package","source":"./","capabilities":[{"type":"skill","name":"bad","path":"../escape"}]}]}`)
	if _, err := readMarketplaceFromDirectory(root); err == nil {
		t.Fatal("expected unsafe path error")
	}
}

func TestReadMarketplaceAcceptsPackageCapabilities(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "demo"), "demo")
	writeFile(t, filepath.Join(root, ".genesis", "marketplace.json"), `{"name":"local","packages":[{"name":"demo-pack","type":"skill-package","source":"./","capabilities":[{"type":"skill","name":"demo","path":"./skills/demo"}]}]}`)
	manifest, err := readMarketplaceFromDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "local" || len(manifest.Packages) != 1 || manifest.Packages[0].Name != "demo-pack" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if got := manifest.Packages[0].Capabilities[0]; got.Type != capmodel.CapabilityTypeSkill || got.Path != "./skills/demo" {
		t.Fatalf("unexpected capability: %+v", got)
	}
}

func TestReadPluginManifestAsPluginPackage(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "docx"), "docx")
	writeSkill(t, filepath.Join(root, "skills", "pdf"), "pdf")
	writeFile(t, filepath.Join(root, "plugin.json"), `{"name":"document-plugin","description":"Documents","version":"0.1.0","capabilities":[{"type":"skill","name":"docx","path":"./skills/docx"},{"type":"skill","name":"pdf","path":"./skills/pdf"}]}`)
	manifest, err := readMarketplaceFromDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "document-plugin-marketplace" || len(manifest.Packages) != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	pkg := manifest.Packages[0]
	if pkg.Type != marketmodel.PackageTypePlugin || len(pkg.Capabilities) != 2 {
		t.Fatalf("unexpected plugin package: %+v", pkg)
	}
}

func TestInstallerInstallsAndProjectsCapabilities(t *testing.T) {
	cache := t.TempDir()
	marketRoot := filepath.Join(cache, "market")
	writeSkill(t, filepath.Join(marketRoot, "skills", "demo"), "demo")
	writeFile(t, filepath.Join(marketRoot, "skills", "demo", "references", "guide.md"), "# Guide\n")
	manifest := marketmodel.Manifest{Name: "local", Packages: []marketmodel.Package{{Name: "demo-pack", Type: marketmodel.PackageTypeSkillPackage, Source: "./", Capabilities: []capmodel.CapabilityManifest{{Type: capmodel.CapabilityTypeSkill, Name: "demo", Path: "./skills/demo"}}}}}
	installer := NewInstaller(InstallerOptions{UserInstalledDir: filepath.Join(t.TempDir(), "installed")})
	record, err := installer.Install(context.Background(), marketcontract.InstallRequest{Marketplace: marketmodel.MarketplaceRecord{Name: "local", InstallLocation: marketRoot}, Manifest: manifest, Package: manifest.Packages[0], Scope: marketmodel.InstallScopeUser, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Skills) != 1 || record.Skills[0] != "demo" || len(record.SkillRoots) != 1 {
		t.Fatalf("unexpected record: %+v", record)
	}
	if len(record.Capabilities) != 2 {
		t.Fatalf("expected skill and skill-resource capabilities: %+v", record.Capabilities)
	}
	if _, err := os.Stat(filepath.Join(record.InstallRoot, "skills", "demo", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := installer.Uninstall(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(record.InstallRoot); !os.IsNotExist(err) {
		t.Fatalf("expected removed package dir, err=%v", err)
	}
}

func TestInstallerProjectsNonSkillCapabilities(t *testing.T) {
	cache := t.TempDir()
	marketRoot := filepath.Join(cache, "market")
	writeFile(t, filepath.Join(marketRoot, "tools", "preview", "tool.json"), `{"name":"preview"}`)
	writeFile(t, filepath.Join(marketRoot, "mcp", "graph.json"), `{"name":"graph"}`)
	manifest := marketmodel.Manifest{Name: "local", Packages: []marketmodel.Package{{
		Name:   "office-plugin",
		Type:   marketmodel.PackageTypePlugin,
		Source: "./",
		Capabilities: []capmodel.CapabilityManifest{
			{Type: capmodel.CapabilityTypeTool, Name: "preview", Path: "./tools/preview", Entrypoint: "tool.json"},
			{Type: capmodel.CapabilityTypeMCP, Name: "graph", Path: "./mcp/graph.json"},
		},
	}}}
	installer := NewInstaller(InstallerOptions{UserInstalledDir: filepath.Join(t.TempDir(), "installed")})
	record, err := installer.Install(context.Background(), marketcontract.InstallRequest{Marketplace: marketmodel.MarketplaceRecord{Name: "local", InstallLocation: marketRoot}, Manifest: manifest, Package: manifest.Packages[0], Scope: marketmodel.InstallScopeUser, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Skills) != 0 || len(record.SkillRoots) != 0 {
		t.Fatalf("non-skill package should not create skill roots: %+v", record)
	}
	if len(record.Capabilities) != 2 {
		t.Fatalf("unexpected capability projection: %+v", record.Capabilities)
	}
	seen := map[capmodel.CapabilityType]bool{}
	for _, capability := range record.Capabilities {
		seen[capability.Type] = true
	}
	if !seen[capmodel.CapabilityTypeTool] || !seen[capmodel.CapabilityTypeMCP] {
		t.Fatalf("missing non-skill capabilities: %+v", record.Capabilities)
	}
}
func TestServiceInstallPersistsSourceProvenanceAndCapabilityIndex(t *testing.T) {
	root := t.TempDir()
	marketRoot := filepath.Join(root, "market")
	writeSkill(t, filepath.Join(marketRoot, "skills", "demo"), "demo")
	writeFile(t, filepath.Join(marketRoot, "skills", "demo", "references", "guide.md"), "# Guide\n")
	writeFile(t, filepath.Join(marketRoot, ".genesis", "marketplace.json"), `{"name":"local","packages":[{"name":"demo-pack","type":"skill-package","source":"./","capabilities":[{"type":"skill","name":"demo","path":"./skills/demo"}]}]}`)

	capabilityStore := NewCapabilityIndexStore(filepath.Join(root, "capabilities.json"))
	svc, err := marketservice.New(marketservice.Options{
		Registry:     NewRegistryStore(filepath.Join(root, "marketplaces.json")),
		Installs:     NewInstallStore(filepath.Join(root, "installs.json")),
		Capabilities: capabilityStore,
		Parser:       NewParser(),
		Fetcher:      NewFetcher(filepath.Join(root, "cache"), nil),
		Installer: NewInstaller(InstallerOptions{
			UserInstalledDir: filepath.Join(root, "installed"),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMarketplace(context.Background(), "dir:"+marketRoot); err != nil {
		t.Fatal(err)
	}
	record, err := svc.Install(context.Background(), "demo-pack@local", marketmodel.InstallScopeUser, false)
	if err != nil {
		t.Fatal(err)
	}
	if record.SourceProvenance == nil {
		t.Fatalf("missing source provenance: %+v", record)
	}
	if record.SourceProvenance.Type != marketmodel.SourceTypeDirectory || record.SourceProvenance.Address == "" {
		t.Fatalf("unexpected source provenance: %+v", record.SourceProvenance)
	}
	if record.ContentHash == "" || record.SourceProvenance.ContentHash == "" {
		t.Fatalf("missing content hash: %+v", record)
	}
	capabilities, err := capabilityStore.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities) != 2 {
		t.Fatalf("unexpected capability index: %+v", capabilities)
	}
	if _, err := svc.SetEnabled(context.Background(), record.Spec, false); err != nil {
		t.Fatal(err)
	}
	capabilities, err = capabilityStore.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range capabilities {
		if capability.Enabled {
			t.Fatalf("capability should be disabled: %+v", capabilities)
		}
	}
}

func TestDownloadURLZipByContentType(t *testing.T) {
	// 模拟 openskills 类下载站：URL 无 .zip 后缀，但 Content-Type 为 application/zip。
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("frontend-design/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "---\nname: frontend-design\ndescription: demo\n---\n\n# Demo\n"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)

	fetcher := NewFetcher(t.TempDir(), srv.Client())
	tempDir := t.TempDir()
	if err := fetcher.downloadURL(context.Background(), srv.URL+"/api/download?slug=frontend-design", tempDir); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(tempDir, "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("expected extracted SKILL.md: %v", err)
	}
}

func TestFetcherAllowsGitHubCodeloadRedirect(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://codeload.github.com/org/repo/zip/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	viaReq, err := http.NewRequest(http.MethodGet, "https://github.com/org/repo/archive/main.zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	check := makeRedirectChecker([]string{"github.com"})
	if err := check(req, []*http.Request{viaReq}); err != nil {
		t.Fatalf("codeload redirect should be allowed: %v", err)
	}
}

func TestFetcherRejectsCrossHostRedirect(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/pack.zip", http.StatusFound)
	}))
	t.Cleanup(good.Close)

	fetcher := NewFetcherWithHosts(t.TempDir(), nil, []string{"github.com"})
	err := fetcher.downloadURL(context.Background(), good.URL+"/api/download", t.TempDir())
	if err == nil {
		t.Fatal("expected cross-host redirect to be rejected")
	}
	if !strings.Contains(err.Error(), "跨主机重定向") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetcherAllowsSameHostRedirect(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("demo/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "---\nname: demo\ndescription: demo\n---\n\n# Demo\n"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/api/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base+"/files/pack.zip", http.StatusFound)
	})
	mux.HandleFunc("/files/pack.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(buf.Bytes())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL

	fetcher := NewFetcher(t.TempDir(), nil)
	tempDir := t.TempDir()
	if err := fetcher.downloadURL(context.Background(), srv.URL+"/api/download", tempDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "SKILL.md")); err != nil {
		t.Fatalf("expected extracted SKILL.md: %v", err)
	}
}

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+name+"\ndescription: demo skill\n---\n\n# Demo\n")
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
