// Package model 定义正式 Artifact 与交付目标模型。
package model

import (
	"time"

	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ArtifactRef 是跨执行引用的正式交付对象。
type ArtifactRef struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Kind       string                  `json:"kind,omitempty"`
	Size       int64                   `json:"size"`
	SHA256     string                  `json:"sha256"`
	MIME       string                  `json:"mime,omitempty"`
	Producer   string                  `json:"producer"`
	RunID      string                  `json:"run_id"`
	Scope      workmodel.ResourceScope `json:"scope"`
	StorageRef workmodel.ResourceRef   `json:"storage_ref"`
}

// Manifest 固化 Artifact 的生产与校验证据。
type Manifest struct {
	ArtifactRef
	ProducerVersion string               `json:"producer_version,omitempty"`
	Inputs          []workmodel.InputRef `json:"inputs,omitempty"`
	GateVersion     string               `json:"gate_version"`
	CreatedAt       time.Time            `json:"created_at"`
}

// DeliveryTargetKind 描述用户可见交付位置语义。
type DeliveryTargetKind string

const (
	DeliveryArtifactOnly  DeliveryTargetKind = "artifact_only"
	DeliverySourceSibling DeliveryTargetKind = "source_sibling"
	DeliveryProjectRoot   DeliveryTargetKind = "project_root"
	DeliveryProjectPath   DeliveryTargetKind = "project_path"
	DeliveryExplicitPath  DeliveryTargetKind = "explicit_path"
	DeliveryProductInbox  DeliveryTargetKind = "product_inbox"
)

// DeliveryTarget 是控制面解析后的交付目标。
type DeliveryTarget struct {
	Kind     DeliveryTargetKind    `json:"kind"`
	Resource workmodel.ResourceRef `json:"resource"`
	Name     string                `json:"name"`
}

// DeliveryResult 返回实际用户可见位置；内部 runs 路径不得出现在这里。
type DeliveryResult struct {
	Artifact ArtifactRef           `json:"artifact"`
	Target   DeliveryTarget        `json:"target"`
	Resource workmodel.ResourceRef `json:"resource"`
	Display  string                `json:"display,omitempty"`
}
