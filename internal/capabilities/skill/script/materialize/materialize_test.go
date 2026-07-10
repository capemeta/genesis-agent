package materialize_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	"genesis-agent/internal/capabilities/skill/script/materialize"
)

func TestMaterializeEmbeddedOfficePPT(t *testing.T) {
	svc := newEmbeddedSkillService(t)
	catalog := skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI}
	meta, err := svc.Resolve(context.Background(), skillcontract.ResolveRequest{CatalogRequest: catalog, Name: "office-ppt"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", string(meta.PackageID))
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	mat := &materialize.Materializer{Service: svc, SharedScriptsFS: shared}
	result, err := mat.MaterializePackageScripts(context.Background(), catalog, meta, skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.ScriptsDir == "" || len(result.Files) == 0 {
		t.Fatalf("result=%+v", result)
	}
	for _, name := range []string{
		"inspect_pptx.py",
		"path_contract.py",
		"thumbnail.py",
		"add_slide.py",
		"clean.py",
		"office/unpack.py",
		"office/pack.py",
		"office/__init__.py",
	} {
		if _, err := os.Stat(filepath.Join(result.ScriptsDir, filepath.FromSlash(name))); err != nil {
			t.Fatalf("missing %s: %v (files=%d)", name, err, len(result.Files))
		}
	}
	if len(result.Files) < 50 {
		t.Fatalf("expected shared office tree merged, got %d files", len(result.Files))
	}
}

func newEmbeddedSkillService(t *testing.T) skillcontract.Service {
	t.Helper()
	systemFS, err := embedded.SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, skillmodel.ScopeSystem, systemFS, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	return skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
}
