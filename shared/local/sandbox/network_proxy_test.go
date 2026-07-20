package sandbox

import (
	"context"
	"os"
	"testing"
)

func TestNetworkProxyOnlyPlan(t *testing.T) {
	mgr := NewManager()

	cwd, _ := os.Getwd()
	req := BuildRequest{
		Preference: PreferenceAuto,
		Command: CommandSpec{
			Argv: []string{"curl", "https://api.github.com"},
			Env:  map[string]string{"EXISTING_ENV": "1"},
			Cwd:  cwd,
		},
		Profile: Profile{
			Network:          NetworkProxyOnly,
			ProxyPorts:       []int{3128, 8080},
			AllowUnixSockets: []string{"/tmp/proxy.sock"},
			ProxyEnv: map[string]string{
				"HTTP_PROXY":  "http://127.0.0.1:3128",
				"HTTPS_PROXY": "http://127.0.0.1:3128",
			},
		},
	}

	plan, err := mgr.BuildPlan(context.Background(), req)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	if plan.NetworkPolicy != NetworkProxyOnly {
		t.Errorf("NetworkPolicy = %v, want %v", plan.NetworkPolicy, NetworkProxyOnly)
	}

	// Verify ProxyEnv injection
	if plan.Command.Env["HTTP_PROXY"] != "http://127.0.0.1:3128" {
		t.Errorf("HTTP_PROXY = %q, want %q", plan.Command.Env["HTTP_PROXY"], "http://127.0.0.1:3128")
	}
	if plan.Command.Env["HTTPS_PROXY"] != "http://127.0.0.1:3128" {
		t.Errorf("HTTPS_PROXY = %q, want %q", plan.Command.Env["HTTPS_PROXY"], "http://127.0.0.1:3128")
	}
	if plan.Command.Env["EXISTING_ENV"] != "1" {
		t.Errorf("EXISTING_ENV = %q, want %q", plan.Command.Env["EXISTING_ENV"], "1")
	}

	// Verify Audit tags
	if plan.AuditTags["sandbox.effective_network_policy"] != "proxy_only" {
		t.Errorf("Audit tag network_policy = %q, want %q", plan.AuditTags["sandbox.effective_network_policy"], "proxy_only")
	}
}
