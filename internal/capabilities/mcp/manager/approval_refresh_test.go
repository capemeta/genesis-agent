package manager

import (
	"context"
	"sync/atomic"
	"testing"

	mcpstore "genesis-agent/internal/capabilities/mcp/adapter/store"
	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

type noopFactory struct{}

func (noopFactory) Build(context.Context, model.McpServerConfig) (contract.Transport, error) {
	return nil, nil
}

type closeTrackingDialed struct{ closed atomic.Int32 }

func (d *closeTrackingDialed) Close() error  { d.closed.Add(1); return nil }
func (*closeTrackingDialed) Underlying() any { return nil }

func TestSyncRevokesConnectedProjectServerAfterApprovalRejected(t *testing.T) {
	ctx := context.Background()
	store := mcpstore.NewMemory()
	def := model.McpServerDefinition{
		Origin:    model.OriginProject,
		ConfigKey: "project-server-v1",
		Config:    model.McpServerConfig{Name: "project-server", Enabled: true},
	}
	if err := store.Put(ctx, def.Config.Name, contract.ApprovalApproved); err != nil {
		t.Fatal(err)
	}
	mgr, err := New(Options{Factory: noopFactory{}, ApprovalStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close(ctx) }()

	dialed := &closeTrackingDialed{}
	mgr.mu.Lock()
	mgr.wanted[def.Config.Name] = def
	mgr.sessions[def.Config.Name] = &managedSession{
		def:     def,
		session: &session{dialed: dialed},
		state: model.ServerState{
			Name: def.Config.Name, Status: model.ServerStatusReady, Origin: def.Origin, ConfigKey: def.ConfigKey,
		},
	}
	mgr.mu.Unlock()

	if err := store.Put(ctx, def.Config.Name, contract.ApprovalRejected); err != nil {
		t.Fatal(err)
	}
	states, err := mgr.Sync(ctx, []model.McpServerDefinition{def})
	if err != nil {
		t.Fatal(err)
	}
	if dialed.closed.Load() != 1 {
		t.Fatalf("reject 后应关闭已连接 session，实际关闭次数=%d", dialed.closed.Load())
	}
	if len(states) != 1 || states[0].Status != model.ServerStatusDisabled {
		t.Fatalf("reject 后应处于 disabled，实际=%+v", states)
	}
}
