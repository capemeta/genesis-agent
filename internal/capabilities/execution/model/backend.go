package model

import (
	"fmt"
	"strings"
)

// BackendKind identifies the execution environment without exposing a resource locator.
type BackendKind string

const (
	BackendKindHost         BackendKind = "host"
	BackendKindLocalSandbox BackendKind = "local_sandbox"
	BackendKindRemote       BackendKind = "remote"
)

// ExecutionBackendRef is the immutable backend identity captured with an execution.
// Credentials, clients and other live runtime objects must never be stored here.
type ExecutionBackendRef struct {
	Kind       BackendKind `json:"kind"`
	Provider   string      `json:"provider,omitempty"`
	InstanceID string      `json:"instance_id,omitempty"`
	Authority  string      `json:"authority"`
}

// Validate verifies the stable, non-secret backend identity.
func (r ExecutionBackendRef) Validate() error {
	switch r.Kind {
	case BackendKindHost, BackendKindLocalSandbox, BackendKindRemote:
	default:
		return fmt.Errorf("execution backend kind 无效: %q", r.Kind)
	}
	if strings.TrimSpace(r.Authority) == "" || r.Authority != strings.TrimSpace(r.Authority) {
		return fmt.Errorf("execution backend authority 无效")
	}
	if strings.ContainsAny(r.Authority, "\\/\x00") {
		return fmt.Errorf("execution backend authority 包含非法字符")
	}
	if r.Provider != strings.TrimSpace(r.Provider) || r.InstanceID != strings.TrimSpace(r.InstanceID) {
		return fmt.Errorf("execution backend provider/instance_id 必须规范化")
	}
	return nil
}

// ResolveBackendKind classifies a provider string into a stable BackendKind.
func ResolveBackendKind(provider string) BackendKind {
	p := strings.TrimSpace(strings.ToLower(provider))
	switch p {
	case "host", "local-host", "local_host":
		return BackendKindHost
	case "local", "local-platform", "local_platform", "local_platform_sandbox", "bwrap", "landlock", "seatbelt", "windows":
		return BackendKindLocalSandbox
	case "genesis-sandbox", "docker", "remote", "remote_sandbox", "remote-sandbox":
		return BackendKindRemote
	default:
		return BackendKindRemote
	}
}
