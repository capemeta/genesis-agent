package manager_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/manager"
	"genesis-agent/internal/capabilities/mcp/model"
)

type slowFactory struct {
	delay time.Duration
	dials atomic.Int32
}

func (f *slowFactory) Build(context.Context, model.McpServerConfig) (contract.Transport, error) {
	return &slowTransport{delay: f.delay, dials: &f.dials}, nil
}

type slowTransport struct {
	delay time.Duration
	dials *atomic.Int32
}

func (t *slowTransport) Kind() model.McpTransportType { return model.McpTransportStdio }

func (t *slowTransport) Dial(ctx context.Context, _ contract.ConnectOptions) (contract.DialedSession, error) {
	t.dials.Add(1)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(t.delay):
	}
	return &fakeDialed{}, nil
}

type fakeDialed struct{}

func (f *fakeDialed) Close() error    { return nil }
func (f *fakeDialed) Underlying() any { return nil } // 故意无效，连接会失败；本测试只验证异步不阻塞

func TestSyncAsyncReturnsBeforeDialCompletes(t *testing.T) {
	t.Parallel()
	factory := &slowFactory{delay: 800 * time.Millisecond}
	mgr, err := manager.New(manager.Options{Factory: factory, ConnectBatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close(context.Background()) }()

	defs := []model.McpServerDefinition{{
		Config: model.McpServerConfig{
			Name:           "slow",
			Enabled:        true,
			Type:           model.McpTransportStdio,
			Command:        "noop",
			StartupTimeout: 5 * time.Second,
		},
		ConfigKey: "slow-1",
	}}
	start := time.Now()
	states, err := mgr.SyncAsync(context.Background(), defs)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("SyncAsync should not wait for dial, elapsed=%s", elapsed)
	}
	if len(states) != 1 || states[0].Status != model.ServerStatusStarting {
		t.Fatalf("expected starting, got %+v", states)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if factory.dials.Load() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("后台 Dial 未启动")
}
