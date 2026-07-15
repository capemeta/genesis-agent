package transport

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stdioTransport struct {
	cfg model.McpServerConfig
}

func (t *stdioTransport) Kind() model.McpTransportType { return model.McpTransportStdio }

func (t *stdioTransport) Dial(ctx context.Context, opts contract.ConnectOptions) (contract.DialedSession, error) {
	cmd := exec.CommandContext(ctx, t.cfg.Command, t.cfg.Args...)
	if cwd := strings.TrimSpace(t.cfg.Cwd); cwd != "" {
		cmd.Dir = cwd
	}
	// 保留当前进程环境，并允许 cfg.Env 覆盖（调用方可显式收紧）。
	cmd.Env = mergeEnv(os.Environ(), t.cfg.Env)
	configureProcessGroup(cmd)

	clientOpts := &mcp.ClientOptions{KeepAlive: 0} // 健康检查由 Manager 统一调度
	if opts.OnToolsChanged != nil {
		clientOpts.ToolListChangedHandler = func(context.Context, *mcp.ToolListChangedRequest) {
			opts.OnToolsChanged()
		}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "genesis-agent", Version: "1.0.0"}, clientOpts)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("连接 stdio mcp server %q 失败: %w", t.cfg.Name, err)
	}
	return &sdkDialed{session: session}, nil
}

type sdkDialed struct {
	session *mcp.ClientSession
}

func (d *sdkDialed) Close() error {
	if d == nil || d.session == nil {
		return nil
	}
	return d.session.Close()
}

func (d *sdkDialed) Underlying() any {
	if d == nil {
		return nil
	}
	return d.session
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	index := make(map[string]int, len(base))
	out := append([]string(nil), base...)
	for i, kv := range out {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			index[strings.ToUpper(kv[:eq])] = i
		}
	}
	for k, v := range overrides {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		entry := key + "=" + v
		if i, ok := index[strings.ToUpper(key)]; ok {
			out[i] = entry
		} else {
			out = append(out, entry)
			index[strings.ToUpper(key)] = len(out) - 1
		}
	}
	return out
}

var _ contract.Transport = (*stdioTransport)(nil)
