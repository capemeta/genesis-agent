package manager

import (
	"context"
	"time"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

func (m *Manager) healthLoop(ctx context.Context) {
	defer m.healthWG.Done()
	ticker := time.NewTicker(m.healthEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runHealthChecks(ctx)
			m.reconnectFailed(ctx)
			m.refreshChangedTools(ctx)
		}
	}
}

// reconnectFailed 对失败且已过退避期的 server 自动重连（对齐 Kode failed 30s 退避）。
func (m *Manager) reconnectFailed(ctx context.Context) {
	type candidate struct {
		def model.McpServerDefinition
	}
	m.mu.RLock()
	list := make([]candidate, 0)
	for _, ms := range m.sessions {
		if ms == nil {
			continue
		}
		if ms.state.Status != model.ServerStatusFailed && ms.state.Status != model.ServerStatusNeedsAuth {
			continue
		}
		if !ms.def.Config.Enabled || ms.def.DisabledReason != "" {
			continue
		}
		if time.Since(ms.lastAttempt) < m.failBackoff {
			continue
		}
		list = append(list, candidate{def: ms.def})
	}
	m.mu.RUnlock()
	for _, c := range list {
		m.connectOne(ctx, c.def)
	}
}

func (m *Manager) runHealthChecks(ctx context.Context) {
	type candidate struct {
		name string
		sess *session
		def  model.McpServerDefinition
	}
	m.mu.RLock()
	list := make([]candidate, 0)
	for name, ms := range m.sessions {
		if ms.session == nil || ms.state.Status != model.ServerStatusReady {
			continue
		}
		// Kode：距上次健康检查 < 5s 则跳过（ticker 本身约等于该间隔）。
		if !ms.lastHealth.IsZero() && time.Since(ms.lastHealth) < m.healthEvery {
			continue
		}
		list = append(list, candidate{name: name, sess: ms.session, def: ms.def})
	}
	m.mu.RUnlock()

	for _, c := range list {
		pingCtx, cancel := context.WithTimeout(ctx, defaultPingTimeout)
		err := c.sess.Ping(pingCtx)
		cancel()
		m.mu.Lock()
		ms := m.sessions[c.name]
		if ms == nil || ms.session != c.sess {
			m.mu.Unlock()
			continue
		}
		if err == nil {
			ms.lastHealth = time.Now()
			ms.state.LastHealthPing = ms.lastHealth
			m.mu.Unlock()
			continue
		}
		// ping 失败：关闭并标记 failed，下次 Sync/健康周期可按退避重连。
		sess := ms.session
		def := ms.def
		ms.session = nil
		m.mu.Unlock()
		if sess != nil {
			_ = sess.Close(context.Background())
		}
		m.setFailed(def, err, false)
	}
}

func (m *Manager) refreshChangedTools(ctx context.Context) {
	type refresh struct {
		name string
		sess *session
		def  model.McpServerDefinition
	}
	m.mu.Lock()
	list := make([]refresh, 0)
	for name, ms := range m.sessions {
		if !ms.refreshPending || ms.session == nil || ms.state.Status != model.ServerStatusReady {
			continue
		}
		ms.refreshPending = false
		list = append(list, refresh{name: name, sess: ms.session, def: ms.def})
	}
	m.mu.Unlock()

	for _, item := range list {
		tools, err := item.sess.ListTools(ctx)
		if err != nil {
			continue
		}
		tools = filterTools(tools, item.def.Config)
		m.mu.Lock()
		ms := m.sessions[item.name]
		if ms == nil || ms.session != item.sess {
			m.mu.Unlock()
			continue
		}
		ms.state.Tools = tools
		ms.state.ToolCount = len(tools)
		state := ms.state
		listeners := append([]contract.StateListener(nil), m.listeners...)
		m.mu.Unlock()
		for _, l := range listeners {
			if l != nil {
				l.OnMCPEvent(ctx, model.LifecycleEvent{Kind: model.EventToolsChanged, Server: item.name, State: state, At: time.Now()})
			}
		}
	}
}
