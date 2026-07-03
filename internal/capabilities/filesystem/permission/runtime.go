package permission

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
)

// PermissionMode 描述产品侧对文件权限的默认策略。
type PermissionMode string

const (
	PermissionModeDefault     PermissionMode = "default"
	PermissionModeAcceptEdits PermissionMode = "accept_edits"
	PermissionModeDontAsk     PermissionMode = "dont_ask"
	PermissionModeYolo        PermissionMode = "yolo"
	PermissionModeBypass      PermissionMode = "bypass"
)

// RuntimeGrant 是一次运行时目录或文件授权。
type RuntimeGrant struct {
	Action approvalmodel.Action
	Scope  approvalmodel.GrantScope
	Path   string
}

// RuntimeFilePermissions 保存会话/项目级文件授权集合。
type RuntimeFilePermissions struct {
	mu     sync.RWMutex
	grants []RuntimeGrant
}

// NewRuntimeFilePermissions 创建运行时文件权限集合。
func NewRuntimeFilePermissions() *RuntimeFilePermissions { return &RuntimeFilePermissions{} }

// IsAllowed 判断请求是否命中已有运行时授权。
func (p *RuntimeFilePermissions) IsAllowed(req approvalmodel.Request) bool {
	_, ok := p.Match(req)
	return ok
}

// Match 返回命中的运行时授权。
func (p *RuntimeFilePermissions) Match(req approvalmodel.Request) (RuntimeGrant, bool) {
	if p == nil || !isFileRequest(req) {
		return RuntimeGrant{}, false
	}
	path := requestPath(req)
	if path == "" {
		return RuntimeGrant{}, false
	}
	action := req.Action
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, grant := range p.grants {
		if grant.Action != action {
			continue
		}
		if isWithinPath(path, grant.Path) {
			return grant, true
		}
	}
	return RuntimeGrant{}, false
}

// Remember 记录用户授予的 session/project 权限，once 不缓存。
func (p *RuntimeFilePermissions) Remember(req approvalmodel.Request, decision approvalmodel.Decision) {
	if p == nil || !isFileRequest(req) || decision.Type != approvalmodel.DecisionApprovedForScope {
		return
	}
	if decision.Scope != approvalmodel.GrantScopeSession && decision.Scope != approvalmodel.GrantScopeProject {
		return
	}
	path := requestPath(req)
	if path == "" {
		return
	}
	grant := RuntimeGrant{Action: req.Action, Scope: decision.Scope, Path: normalizeGrantPath(path)}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.grants = mergeGrant(p.grants, grant)
}

// Grants 返回当前授权快照，主要用于测试和调试。
func (p *RuntimeFilePermissions) Grants() []RuntimeGrant {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]RuntimeGrant, len(p.grants))
	copy(out, p.grants)
	return out
}

// ApprovalService 在通用 approval 前增加运行时文件授权目录匹配。
type ApprovalService struct {
	next  approvalcontract.Service
	perms *RuntimeFilePermissions
}

// NewApprovalService 创建文件权限感知的 approval service。
func NewApprovalService(next approvalcontract.Service, perms *RuntimeFilePermissions) *ApprovalService {
	return &ApprovalService{next: next, perms: perms}
}

// Authorize 优先命中运行时文件授权，否则委托通用 approval。
func (s *ApprovalService) Authorize(ctx context.Context, req approvalmodel.Request) (approvalmodel.Decision, error) {
	if s != nil && s.perms != nil {
		if grant, ok := s.perms.Match(req); ok {
			return approvalmodel.Decision{Type: approvalmodel.DecisionApprovedForScope, Scope: grant.Scope, Reason: "runtime file permission grant"}, nil
		}
	}
	if s == nil || s.next == nil {
		return approvalmodel.Decision{Type: approvalmodel.DecisionDenied, Scope: approvalmodel.GrantScopeOnce, Reason: "approval service not configured"}, nil
	}
	decision, err := s.next.Authorize(ctx, req)
	if err != nil {
		return approvalmodel.Decision{}, err
	}
	if s.perms != nil {
		s.perms.Remember(req, decision)
	}
	return decision, nil
}

func mergeGrant(grants []RuntimeGrant, grant RuntimeGrant) []RuntimeGrant {
	out := grants[:0]
	for _, existing := range grants {
		if existing.Action != grant.Action || existing.Scope != grant.Scope {
			out = append(out, existing)
			continue
		}
		if isWithinPath(grant.Path, existing.Path) {
			return grants
		}
		if isWithinPath(existing.Path, grant.Path) {
			continue
		}
		out = append(out, existing)
	}
	return append(out, grant)
}

func isFileRequest(req approvalmodel.Request) bool {
	return strings.HasPrefix(string(req.Action), "file.")
}

func requestPath(req approvalmodel.Request) string {
	metadata := req.Metadata
	if metadata == nil {
		metadata = req.Resource.Metadata
	}
	if metadata != nil {
		if backend := strings.TrimSpace(metadata["backend"]); backend != "" {
			return backend
		}
	}
	uri := req.Resource.URI
	if strings.HasPrefix(uri, "file://") {
		return strings.TrimPrefix(uri, "file://")
	}
	if strings.HasPrefix(uri, "workspace://") {
		return strings.TrimPrefix(uri, "workspace://")
	}
	return uri
}

func isWithinPath(path, root string) bool {
	path = normalizeGrantPath(path)
	root = normalizeGrantPath(root)
	if path == "" || root == "" {
		return false
	}
	if path == root {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(path, strings.TrimRight(root, sep)+sep)
}

func normalizeGrantPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
