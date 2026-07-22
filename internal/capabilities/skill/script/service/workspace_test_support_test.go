package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type testSkillService struct{ skillcontract.Service }

func (s testSkillService) GetBinding(ctx context.Context, req skillcontract.BindingLookup) (skillmodel.InvocationBinding, error) {
	resolved, err := s.Service.Resolve(ctx, skillcontract.ResolveRequest{Name: req.Handle})
	if err != nil {
		return skillmodel.InvocationBinding{}, err
	}
	return s.Service.CreateBinding(ctx, skillcontract.BindingRequest{
		Resolved: resolved, TenantID: req.TenantID, RunID: req.RunID,
		ToolPolicy:            skillmodel.EffectiveToolPolicy{Base: []string{"run_skill_command"}, Allowed: []string{"run_skill_command"}, Required: []string{"run_skill_command"}},
		ExecutionPolicy:       skillmodel.EffectiveExecutionPolicy{ExecutionMode: skillmodel.ExecutionModePerCall},
		PolicySnapshotVersion: "test/v1",
	})
}

func (s testSkillService) GetPackageSnapshot(ctx context.Context, digest string) (skillmodel.SkillPackageSnapshot, []skillmodel.SkillPackageFile, error) {
	return s.Service.(skillcontract.PackageSnapshotReader).GetPackageSnapshot(ctx, digest)
}

func testRuntimeSkillFS(source fstest.MapFS) fstest.MapFS {
	out := fstest.MapFS{}
	for name, file := range source {
		out[name] = file
		if !strings.HasSuffix(name, "/SKILL.md") {
			continue
		}
		pkg := strings.TrimSuffix(name, "/SKILL.md")
		out[name] = &fstest.MapFile{Data: []byte("---\nname: " + pkg + "\ndescription: Runtime command test skill\n---\nDemo")}
		out[pkg+"/genesis.skill.yaml"] = &fstest.MapFile{Data: []byte("schema: genesis.skill/v1\nskill: " + pkg + `
runtime_profiles:
  default:
    sandbox: {required: true, execution_mode: per_call}
    dependencies:
      runtime:
        node: [{name: pptxgenjs, require: pptxgenjs}]
        system: [{name: libreoffice, command: soffice}]
invocations:
  - id: default
    handle: ` + pkg + `
    description: Execute runtime command tests
    agent_mode: main
    runtime_profile: default
    request:
      task: {required: false}
      inputs: {min_items: 0, max_items: 64, access: read_only}
    prompt: {skill_body: include}
    tool_policy: {allow: [run_skill_command], required: [run_skill_command]}
    result: {kind: message}
`)}
	}
	return out
}

type testProvisioner struct{}

func (testProvisioner) Prepare(_ context.Context, req workcontract.PrepareRequest) (workcontract.PreparedExecution, error) {
	base := filepath.Join(req.StateRoot.Path, "runtime", "runs", req.Binding.Owner.RunID)
	w := execmodel.ExecutionWorkspace{WorkDir: filepath.Join(base, "work", req.Binding.ID), InputDir: filepath.Join(base, "input", req.Binding.ID), OutputDir: filepath.Join(base, "output", req.Binding.ID), TmpDir: filepath.Join(base, "tmp", req.Binding.ID)}
	for _, dir := range []string{w.WorkDir, w.InputDir, w.OutputDir, w.TmpDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return workcontract.PreparedExecution{}, err
		}
	}
	return workcontract.PreparedExecution{Binding: req.Binding, Workspace: w}, nil
}

type testSnapshotReader map[workmodel.WorkspacePath][]byte

func (r testSnapshotReader) OpenSnapshot(_ context.Context, path workmodel.WorkspacePath) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r[path])), nil
}

func testInputManifest(binding execmodel.ExecutionBinding, name string, content []byte) (workmodel.InputManifest, testSnapshotReader) {
	path := workmodel.WorkspacePath("runs/" + binding.Owner.RunID + "/input/input-test/" + name)
	digest := sha256.Sum256(content)
	manifest := workmodel.InputManifest{RunID: binding.Owner.RunID, BindingID: binding.ID, Inputs: []workmodel.InputRef{{ID: "input-test", Name: name, Alias: workmodel.WorkspacePath(name), Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), StagedPath: path}}}
	return manifest, testSnapshotReader{path: append([]byte(nil), content...)}
}

func testBinding(runID string) execmodel.ExecutionBinding {
	return execmodel.ExecutionBinding{ID: runID + "-skill-demo", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant-test", UserID: "user-test", SessionID: "agent-session-test", RunID: runID}}
}

func testStateRoot(root string) workmodel.StateRoot {
	return workmodel.StateRoot{ID: "test-state", Authority: "host", Path: filepath.Join(root, ".genesis")}
}
