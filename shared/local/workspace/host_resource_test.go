package workspace

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type hostFixedIDs struct{ value string }

func (g hostFixedIDs) Generate() string { return g.value }

func TestHostResolverAndReaderSurviveRestartWithoutLeakingPhysicalPath(t *testing.T) {
	stateRoot := t.TempDir()
	workRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(workRoot, "deck.pptx"), []byte("ppt-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	locatorStore, _ := NewHostLocatorStore(stateRoot)
	resolver, _ := NewHostBackendResourceResolver(locatorStore, hostFixedIDs{value: "one"})
	execution := hostExecution(workRoot)
	ref, err := resolver.ResolveProducedResource(context.Background(), workcontract.BackendResourceRequest{TenantID: "tenant", RunID: "run", Execution: execution, ObservedPath: "deck.pptx", ObservedName: "deck.pptx", Size: 8, Availability: workmodel.ResourceAvailabilityDurable})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(ref)
	if ref.Path != "" || strings.Contains(string(encoded), filepath.ToSlash(workRoot)) || strings.Contains(string(encoded), filepath.Base(workRoot)) {
		t.Fatalf("ResourceRef leaked physical path: %s", encoded)
	}
	restartedStore, _ := NewHostLocatorStore(stateRoot)
	reader, _ := NewHostResourceReader(restartedStore)
	handle, err := reader.Open(hostRunContext(ref.Scope), ref)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(handle.Reader)
	_ = handle.Reader.Close()
	if err != nil || string(data) != "ppt-data" || handle.Version != ref.Version {
		t.Fatalf("read = %q, version=%q, err=%v", data, handle.Version, err)
	}
}

func TestHostReaderRejectsReplacementAndContentMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, filename string)
	}{
		{name: "replacement", mutate: func(t *testing.T, filename string) {
			tmp := filename + ".new"
			if err := os.WriteFile(tmp, []byte("same-data"), 0o600); err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS == "windows" {
				if err := os.Remove(filename); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Rename(tmp, filename); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "content mutation", mutate: func(t *testing.T, filename string) {
			if err := os.WriteFile(filename, []byte("evil-data"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateRoot, workRoot := t.TempDir(), t.TempDir()
			filename := filepath.Join(workRoot, "out.bin")
			if err := os.WriteFile(filename, []byte("same-data"), 0o600); err != nil {
				t.Fatal(err)
			}
			store, _ := NewHostLocatorStore(stateRoot)
			resolver, _ := NewHostBackendResourceResolver(store, hostFixedIDs{value: test.name})
			ref, err := resolver.ResolveProducedResource(context.Background(), workcontract.BackendResourceRequest{TenantID: "tenant", RunID: "run", Execution: hostExecution(workRoot), ObservedPath: "out.bin", ObservedName: "out.bin", Size: 9, Availability: workmodel.ResourceAvailabilityDurable})
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(t, filename)
			reader, _ := NewHostResourceReader(store)
			if _, err := reader.Open(hostRunContext(ref.Scope), ref); !localWorkspaceErrorIs(err, workcontract.ErrCodeProducedResourceVersionConflict) {
				t.Fatalf("mutation error = %v", err)
			}
		})
	}
}

func TestHostReaderDetectsMutationDuringStreaming(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reader opens without write sharing, preventing this mutation before it occurs")
	}
	stateRoot, workRoot := t.TempDir(), t.TempDir()
	filename := filepath.Join(workRoot, "stream.bin")
	if err := os.WriteFile(filename, []byte("original-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := NewHostLocatorStore(stateRoot)
	resolver, _ := NewHostBackendResourceResolver(store, hostFixedIDs{value: "stream"})
	ref, err := resolver.ResolveProducedResource(context.Background(), workcontract.BackendResourceRequest{TenantID: "tenant", RunID: "run", Execution: hostExecution(workRoot), ObservedPath: "stream.bin", ObservedName: "stream.bin", Size: 13, Availability: workmodel.ResourceAvailabilityDurable})
	if err != nil {
		t.Fatal(err)
	}
	reader, _ := NewHostResourceReader(store)
	handle, err := reader.Open(hostRunContext(ref.Scope), ref)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Reader.Close()
	buffer := make([]byte, 2)
	if _, err := io.ReadFull(handle.Reader, buffer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("modified-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(handle.Reader); !localWorkspaceErrorIs(err, workcontract.ErrCodeProducedResourceVersionConflict) {
		t.Fatalf("stream mutation error = %v", err)
	}
}

func TestHostResolverRejectsSymlinkBoundary(t *testing.T) {
	workRoot, outside := t.TempDir(), t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workRoot, "linked.txt")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store, _ := NewHostLocatorStore(t.TempDir())
	resolver, _ := NewHostBackendResourceResolver(store, hostFixedIDs{value: "link"})
	_, err := resolver.ResolveProducedResource(context.Background(), workcontract.BackendResourceRequest{TenantID: "tenant", RunID: "run", Execution: hostExecution(workRoot), ObservedPath: "linked.txt", ObservedName: "linked.txt", Size: 6, Availability: workmodel.ResourceAvailabilityDurable})
	if !localWorkspaceErrorIs(err, workcontract.ErrCodePathNamespaceMismatch) {
		t.Fatalf("symlink boundary error = %v", err)
	}
}

func TestHostReaderRejectsPathComponentReplacedBySymlink(t *testing.T) {
	stateRoot, workRoot, outside := t.TempDir(), t.TempDir(), t.TempDir()
	subdir := filepath.Join(workRoot, "sub")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "out.txt"), []byte("trusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "out.txt"), []byte("trusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := NewHostLocatorStore(stateRoot)
	resolver, _ := NewHostBackendResourceResolver(store, hostFixedIDs{value: "component"})
	ref, err := resolver.ResolveProducedResource(context.Background(), workcontract.BackendResourceRequest{TenantID: "tenant", RunID: "run", Execution: hostExecution(workRoot), ObservedPath: "sub/out.txt", ObservedName: "out.txt", Size: 7, Availability: workmodel.ResourceAvailabilityDurable})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(subdir, subdir+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, subdir); err != nil {
		t.Skipf("directory symlink/junction unavailable: %v", err)
	}
	reader, _ := NewHostResourceReader(store)
	if _, err := reader.Open(hostRunContext(ref.Scope), ref); !localWorkspaceErrorIs(err, workcontract.ErrCodeProducedResourceVersionConflict) {
		t.Fatalf("replaced path component error = %v", err)
	}
}

func hostExecution(workRoot string) workmodel.PreparedExecutionSnapshot {
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeSession, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", RunID: "run"}}
	return workmodel.PreparedExecutionSnapshot{Binding: binding, Backend: execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Authority: "host"}, Workspace: execmodel.ExecutionWorkspace{WorkDir: workRoot, InputDir: workRoot, OutputDir: workRoot, TmpDir: workRoot}}
}

func hostRunContext(scope workmodel.ResourceScope) context.Context {
	prepared := workmodel.PreparedRun{Manifest: workmodel.RunManifest{RunID: "run", Scope: scope}}
	return workcontract.WithPreparedRun(context.Background(), prepared)
}

func TestHostResolverUsesOutputDirForReservationPath(t *testing.T) {
	stateRoot := t.TempDir()
	workRoot := t.TempDir()
	outputRoot := t.TempDir()
	target := filepath.Join(outputRoot, "reserved", "d1", "deck.pptx")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("ppt-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	locatorStore, _ := NewHostLocatorStore(stateRoot)
	resolver, _ := NewHostBackendResourceResolver(locatorStore, hostFixedIDs{value: "resv"})
	execution := hostExecution(workRoot)
	execution.Workspace.OutputDir = outputRoot
	ref, err := resolver.ResolveProducedResource(context.Background(), workcontract.BackendResourceRequest{
		TenantID: "tenant", RunID: "run", Execution: execution,
		ObservedPath: "reserved/d1/deck.pptx", ObservedName: "deck.pptx", Size: 8,
		Availability: workmodel.ResourceAvailabilityDurable,
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, _ := NewHostResourceReader(locatorStore)
	handle, err := reader.Open(hostRunContext(ref.Scope), ref)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(handle.Reader)
	_ = handle.Reader.Close()
	if err != nil || string(data) != "ppt-data" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}
