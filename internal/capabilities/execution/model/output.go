package model

import "time"

// ExecutorOutputObject describes an execution-backend output object.
//
// It is not a Genesis Artifact: callers must register it as a produced
// resource and pass the Artifact publication gate before delivery. ID is an
// opaque backend locator and must not be exposed to the model as a path.
type ExecutorOutputObject struct {
	ID           string     `json:"id"`
	WorkspaceID  string     `json:"workspace_id,omitempty"`
	JobID        string     `json:"job_id,omitempty"`
	Name         string     `json:"name,omitempty"`
	Size         int64      `json:"size,omitempty"`
	SHA256       string     `json:"sha256,omitempty"`
	MediaType    string     `json:"media_type,omitempty"`
	Version      string     `json:"version,omitempty"`
	Availability string     `json:"availability,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}
