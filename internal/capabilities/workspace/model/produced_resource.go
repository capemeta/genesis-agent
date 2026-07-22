package model

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// ResourceAvailability describes whether a backend resource depends on a live lease.
type ResourceAvailability string

const (
	ResourceAvailabilityLeased  ResourceAvailability = "leased"
	ResourceAvailabilityDurable ResourceAvailability = "durable"
)

// ProducedResourceDescriptor is the immutable, persistent identity of an execution output.
// Source is trusted backend data and must not be projected to ordinary model tool output.
type ProducedResourceDescriptor struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	RunID     string `json:"run_id"`
	BindingID string `json:"binding_id"`

	LogicalRef string      `json:"logical_ref"`
	Source     ResourceRef `json:"source"`

	ObservedName string `json:"observed_name"`
	MediaType    string `json:"media_type,omitempty"`
	Role         string `json:"role,omitempty"`
	Size         int64  `json:"size"`

	Availability ResourceAvailability `json:"availability"`
	ExpiresAt    *time.Time           `json:"expires_at,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
}

// Validate enforces the immutable descriptor contract.
func (d ProducedResourceDescriptor) Validate() error {
	for field, value := range map[string]string{"id": d.ID, "run_id": d.RunID, "binding_id": d.BindingID} {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
			return fmt.Errorf("produced resource %s 无效", field)
		}
	}
	if d.TenantID != strings.TrimSpace(d.TenantID) {
		return fmt.Errorf("produced resource tenant_id 必须规范化")
	}
	logical := strings.ReplaceAll(strings.TrimSpace(d.LogicalRef), `\`, "/")
	if logical != d.LogicalRef || !strings.HasPrefix(logical, "run:/") {
		return fmt.Errorf("produced resource logical_ref 必须是规范化 run:/ 引用")
	}
	rel := strings.TrimPrefix(logical, "run:/")
	if err := WorkspacePath(rel).Validate(); err != nil {
		return fmt.Errorf("produced resource logical_ref 无效: %w", err)
	}
	if err := validatePersistentResourceRef(d.Source); err != nil {
		return fmt.Errorf("produced resource source 无效: %w", err)
	}
	if d.Source.Scope.TenantID != d.TenantID {
		return fmt.Errorf("produced resource source tenant scope 不一致")
	}
	name := strings.TrimSpace(d.ObservedName)
	if name == "" || name != d.ObservedName || name != path.Base(strings.ReplaceAll(name, `\`, "/")) || strings.ContainsAny(name, "\\/\x00") {
		return fmt.Errorf("produced resource observed_name 无效")
	}
	if d.MediaType != strings.TrimSpace(d.MediaType) || d.Size < 0 || d.CreatedAt.IsZero() {
		return fmt.Errorf("produced resource media_type/size/created_at 无效")
	}
	switch d.Availability {
	case ResourceAvailabilityLeased:
		if d.ExpiresAt == nil || d.ExpiresAt.IsZero() {
			return fmt.Errorf("leased produced resource 缺少 expires_at")
		}
	case ResourceAvailabilityDurable:
	default:
		return fmt.Errorf("produced resource availability 无效: %q", d.Availability)
	}
	return nil
}

func validatePersistentResourceRef(ref ResourceRef) error {
	if strings.TrimSpace(ref.Authority) == "" || ref.Authority != strings.TrimSpace(ref.Authority) ||
		strings.TrimSpace(ref.Scheme) == "" || ref.Scheme != strings.TrimSpace(ref.Scheme) ||
		strings.TrimSpace(ref.ID) == "" || ref.ID != strings.TrimSpace(ref.ID) ||
		strings.TrimSpace(ref.Version) == "" || ref.Version != strings.TrimSpace(ref.Version) {
		return fmt.Errorf("resource ref 缺少规范化 authority/scheme/id/version")
	}
	if strings.ContainsAny(ref.Authority+ref.Scheme, "\\/\x00") {
		return fmt.Errorf("resource ref authority/scheme 包含非法字符")
	}
	return nil
}
