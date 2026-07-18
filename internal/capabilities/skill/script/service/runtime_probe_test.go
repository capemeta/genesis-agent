package service

import (
	"testing"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

func TestRedundantRuntimeProbeUsesDeclaredDependencies(t *testing.T) {
	deps := skillmodel.RuntimeDeps{
		Node:   []skillmodel.RuntimePackage{{Name: "pptxgenjs", Require: "pptxgenjs"}},
		Python: []skillmodel.RuntimePackage{{Name: "markitdown", Import: "markitdown"}},
	}
	for _, command := range []string{
		"node --version",
		`node -e "require('pptxgenjs'); console.log('ok')"`,
		`python -c "import markitdown; print('ok')"`,
	} {
		if reason, redundant := redundantRuntimeProbe(command, deps); !redundant || reason == "" {
			t.Fatalf("%q should be rejected as redundant, reason=%q", command, reason)
		}
	}
}

func TestRedundantRuntimeProbeDoesNotBlockBusinessOrSideEffectCommands(t *testing.T) {
	deps := skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs", Require: "pptxgenjs"}}}
	for _, command := range []string{
		"node build.js",
		`node -e "require('pptxgenjs'); require('fs').writeFileSync('x', 'y')"`,
		`node -e "require('another-package')"`,
	} {
		if reason, redundant := redundantRuntimeProbe(command, deps); redundant {
			t.Fatalf("%q must remain executable, reason=%q", command, reason)
		}
	}
}
