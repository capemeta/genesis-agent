package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workspacememory "genesis-agent/internal/capabilities/workspace/adapter/memory"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	enterprisebootstrap "genesis-agent/products/enterprise/bootstrap"
)

type testArtifactControl struct{}

func (testArtifactControl) RegisterProducedResource(context.Context, workcontract.RegisterProducedResourceRequest) (workmodel.ProducedResourceDescriptor, error) {
	return workmodel.ProducedResourceDescriptor{}, nil
}
func (testArtifactControl) BindRemoteSession(context.Context, string, string, string, sandboxcontract.WorkspaceRef, time.Time) error {
	return nil
}
func (testArtifactControl) LoadExecutionSession(context.Context, string) (sandboxcontract.WorkspaceRef, bool, error) {
	return sandboxcontract.WorkspaceRef{}, false, nil
}
func (testArtifactControl) SaveExecutionSession(context.Context, string, sandboxcontract.WorkspaceRef) error {
	return nil
}
func (testArtifactControl) DeleteExecutionSession(context.Context, string) error { return nil }
func (testArtifactControl) InitializeRun(context.Context, artifactcontract.RunInitializationRequest) error {
	return nil
}
func (testArtifactControl) FinalizeRequired(context.Context, string, string) (artifactmodel.FinalizationResult, error) {
	return artifactmodel.FinalizationResult{}, nil
}
func (testArtifactControl) SelectAndFinalize(context.Context, string, string, string, string) (artifactmodel.DeliveryResult, error) {
	return artifactmodel.DeliveryResult{}, nil
}
func (testArtifactControl) EvaluateCompletion(context.Context, string, string) (artifactcontract.CompletionDecision, error) {
	return artifactcontract.CompletionDecision{Complete: true}, nil
}
func (testArtifactControl) RecordPassed(context.Context, artifactcontract.QAPassRequest) error {
	return nil
}
func (testArtifactControl) Reserve(context.Context, artifactcontract.ReserveRequest) (artifactcontract.ReserveResult, error) {
	return artifactcontract.ReserveResult{}, nil
}
func (testArtifactControl) CreateDeliverable(context.Context, artifactmodel.DeliverableSpec) error {
	return nil
}
func (testArtifactControl) ListDeliverables(context.Context, string, string) ([]artifactmodel.DeliverableSpec, error) {
	return nil, nil
}

func TestEnterpriseContainerWiresSkillTools(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 最小可校验配置；日志落到临时目录，避免与开发机 .genesis/logs 文件锁冲突。
	llmCfg := []byte(`llm:
  timeout: 30s
  providers:
    test:
      type: openai
      base_url: http://127.0.0.1:9
      auth:
        type: api_key
        api_key: test-key
  models:
    fast:
      provider: test
      model: test-model
  router:
    default: fast
`)
	cfg := []byte(`log:
  level: info
  format: text
  dir: logs
`)
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "llm.yaml"), llmCfg, 0o644); err != nil {
		t.Fatal(err)
	}
	c := enterprisebootstrap.NewContainer(enterprisebootstrap.ContainerOptions{
		ConfigDirRef: &configDir,
		Quiet:        true,
		Dependencies: enterprisebootstrap.Dependencies{
			RunManifests: workspacememory.NewManifestStore(), ProducedResources: testArtifactControl{},
			RemoteSessions: testArtifactControl{}, Reservations: testArtifactControl{}, Deliverables: testArtifactControl{}, ArtifactRuns: testArtifactControl{}, Finalizer: testArtifactControl{},
			Completion: testArtifactControl{},
			QAEvidence: testArtifactControl{},
		},
	})
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if c.Service() == nil {
		t.Fatal("service is nil")
	}
}

func TestEnterpriseContainerRequiresTenantRunManifestStore(t *testing.T) {
	c := enterprisebootstrap.NewContainer(enterprisebootstrap.ContainerOptions{})
	err := c.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "RunManifestStore 未配置") {
		t.Fatalf("expected fail-closed tenant store error, got %v", err)
	}
}
