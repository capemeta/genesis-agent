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

// PermissionMode 描述产品侧与运行时的权限许可模式。
type PermissionMode string

const (
	PermissionModePlan           PermissionMode = "plan"
	PermissionModeReadOnly       PermissionMode = "read_only"
	PermissionModeProtectedWrite PermissionMode = "protected_write"
	PermissionModeAgent          PermissionMode = "agent"
	PermissionModeWorkspaceAuto  PermissionMode = "workspace_auto"
	PermissionModeFullAccess     PermissionMode = "full_access"
)

type permissionModeKey struct{}

// WithPermissionMode 将 PermissionMode 注入 Context。
func WithPermissionMode(ctx context.Context, mode PermissionMode) context.Context {
	return context.WithValue(ctx, permissionModeKey{}, NormalizeMode(string(mode)))
}

// FromContext 从 Context 中提取 PermissionMode。
func FromContext(ctx context.Context) (PermissionMode, bool) {
	if ctx == nil {
		return "", false
	}
	if v, ok := ctx.Value(permissionModeKey{}).(PermissionMode); ok {
		return v, true
	}
	return "", false
}

// ModeRank 返回 PermissionMode 的权限等级。数值越大，权限越高（越宽）。
// 光谱（宽→严）：full_access > workspace_auto > agent > protected_write > read_only/plan
func ModeRank(mode PermissionMode) int {
	switch NormalizeMode(string(mode)) {
	case PermissionModePlan:
		return 1
	case PermissionModeReadOnly:
		return 1
	case PermissionModeProtectedWrite:
		return 2
	case PermissionModeAgent:
		return 3
	case PermissionModeWorkspaceAuto:
		return 4
	case PermissionModeFullAccess:
		return 5
	default:
		return 3
	}
}

// NarrowPermissionMode 实施子 Agent / Turn 权限单向降权法则：
// 子任务只能保持或收窄权限（Narrow），绝不允许向更高等级提权（Escalate）。
func NarrowPermissionMode(parent, requested PermissionMode) PermissionMode {
	parentNorm := NormalizeMode(string(parent))
	requestedNorm := NormalizeMode(string(requested))
	if ModeRank(requestedNorm) > ModeRank(parentNorm) {
		return parentNorm
	}
	return requestedNorm
}

// RuntimeGrant 是一次运行时目录或文件授权。
type RuntimeGrant struct {
	Action approvalmodel.Action
	Scope  approvalmodel.GrantScope
	Path   string
}

// RuntimeFilePermissions 保存会话/项目级文件授权集合。
type RuntimeFilePermissions struct {
	mu           sync.RWMutex
	persistMu    sync.Mutex // 串行化 project 落盘，避免与 store 锁形成死锁
	grants       []RuntimeGrant
	projectStore ProjectGrantStore
}

// NewRuntimeFilePermissions 创建运行时文件权限集合。
func NewRuntimeFilePermissions() *RuntimeFilePermissions { return &RuntimeFilePermissions{} }

// SetProjectStore 注入项目级授权持久化；可在 LoadProject 前调用。
func (p *RuntimeFilePermissions) SetProjectStore(store ProjectGrantStore) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.projectStore = store
}

// LoadProject 从持久化存储加载 project grant。
func (p *RuntimeFilePermissions) LoadProject(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.persistMu.Lock()
	defer p.persistMu.Unlock()

	p.mu.RLock()
	store := p.projectStore
	p.mu.RUnlock()
	if store == nil {
		return nil
	}
	loaded, err := store.Load(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, grant := range loaded {
		if grant.Scope != approvalmodel.GrantScopeProject {
			continue
		}
		p.grants = mergeGrant(p.grants, grant)
	}
	return nil
}

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

// Remember 记录用户授予的可记忆权限；once 不缓存；project 额外落盘。
func (p *RuntimeFilePermissions) Remember(ctx context.Context, req approvalmodel.Request, decision approvalmodel.Decision) error {
	if p == nil || !isFileRequest(req) || decision.Type != approvalmodel.DecisionApprovedForScope {
		return nil
	}
	if !isMemorableScope(decision.Scope) {
		return nil
	}
	path := resolveGrantPath(req, decision.PathMode)
	if path == "" {
		return nil
	}
	grant := RuntimeGrant{Action: req.Action, Scope: decision.Scope, Path: path}
	p.mu.Lock()
	p.grants = mergeGrant(p.grants, grant)
	needPersist := decision.Scope == approvalmodel.GrantScopeProject && p.projectStore != nil
	p.mu.Unlock()
	if needPersist {
		return p.persistProject(ctx)
	}
	return nil
}

func (p *RuntimeFilePermissions) persistProject(ctx context.Context) error {
	p.persistMu.Lock()
	defer p.persistMu.Unlock()

	p.mu.RLock()
	store := p.projectStore
	snapshot := projectGrantsSnapshot(p.grants)
	p.mu.RUnlock()
	if store == nil {
		return nil
	}
	return store.Save(ctx, snapshot)
}

// ClearScope 清除指定时间作用域的内存授权（不影响已落盘 project，除非随后 Save）。
func (p *RuntimeFilePermissions) ClearScope(scope approvalmodel.GrantScope) {
	if p == nil || scope == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.grants[:0]
	for _, grant := range p.grants {
		if grant.Scope == scope {
			continue
		}
		out = append(out, grant)
	}
	p.grants = out
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
		if remErr := s.perms.Remember(ctx, req, decision); remErr != nil {
			return approvalmodel.Decision{}, remErr
		}
	}
	return decision, nil
}

func isMemorableScope(scope approvalmodel.GrantScope) bool {
	switch scope {
	case approvalmodel.GrantScopeTurn, approvalmodel.GrantScopeSession, approvalmodel.GrantScopeProject:
		return true
	default:
		return false
	}
}

func resolveGrantPath(req approvalmodel.Request, mode approvalmodel.PathGrantMode) string {
	path := requestPath(req)
	if path == "" {
		return ""
	}
	path = normalizeGrantPath(path)
	if mode != approvalmodel.PathGrantDirectory {
		return path
	}
	// 目录资源本身已是授权根；文件则提升到直接父目录。
	if isDirectoryResource(req) {
		return path
	}
	parent := filepath.Dir(path)
	if parent == "" || parent == "." || parent == path {
		return path
	}
	return normalizeGrantPath(parent)
}

func isDirectoryResource(req approvalmodel.Request) bool {
	if req.Resource.Type == "directory" {
		return true
	}
	switch req.Action {
	case approvalmodel.ActionFileList, approvalmodel.ActionFileWalk:
		return true
	default:
		return false
	}
}

func projectGrantsSnapshot(grants []RuntimeGrant) []RuntimeGrant {
	out := make([]RuntimeGrant, 0, len(grants))
	for _, grant := range grants {
		if grant.Scope == approvalmodel.GrantScopeProject {
			out = append(out, grant)
		}
	}
	return out
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
