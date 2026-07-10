package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	enterprisebootstrap "genesis-agent/products/enterprise/bootstrap"
)

func TestEnterpriseContainerWiresSkillTools(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 最小可校验配置；日志落到临时目录，避免与开发机 .genesis/logs 文件锁冲突。
	cfg := []byte(`llm:
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
log:
  level: info
  format: text
  dir: logs
`)
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	c := enterprisebootstrap.NewContainer(&configDir, true)
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if c.Service() == nil {
		t.Fatal("service is nil")
	}
}
