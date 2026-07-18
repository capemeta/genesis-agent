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
	// stdio 子进程默认不继承宿主环境；仅注入显式白名单和配置值。
	cmd.Env = selectedEnv(t.cfg.InheritEnv, t.cfg.Env)
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

func selectedEnv(inherit []string, values map[string]string) []string {
	merged := make(map[string]string, len(inherit)+len(values))
	for _, name := range inherit {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if value, ok := os.LookupEnv(name); ok {
			merged[name] = value
		}
	}
	for name, value := range values {
		name = strings.TrimSpace(name)
		if name != "" {
			merged[name] = value
		}
	}
	out := make([]string, 0, len(merged))
	for name, value := range merged {
		out = append(out, name+"="+value)
	}
	return out
}

var _ contract.Transport = (*stdioTransport)(nil)
