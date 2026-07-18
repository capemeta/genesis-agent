package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

const hostRunFileScheme = "run-file"

type HostLocator struct {
	ID            string                  `json:"id"`
	TenantID      string                  `json:"tenant_id"`
	RunID         string                  `json:"run_id"`
	BindingID     string                  `json:"binding_id"`
	Authority     string                  `json:"authority"`
	CanonicalRoot string                  `json:"canonical_root"`
	CanonicalPath string                  `json:"canonical_path"`
	Identity      hostFileIdentity        `json:"identity"`
	Size          int64                   `json:"size"`
	SHA256        string                  `json:"sha256"`
	Version       string                  `json:"version"`
	MediaType     string                  `json:"media_type,omitempty"`
	Scope         workmodel.ResourceScope `json:"scope"`
	CreatedAt     time.Time               `json:"created_at"`
}

func (l HostLocator) validate() error {
	if strings.TrimSpace(l.ID) == "" || strings.TrimSpace(l.RunID) == "" || strings.TrimSpace(l.BindingID) == "" || strings.TrimSpace(l.Authority) == "" {
		return fmt.Errorf("host locator 缺少 identity/run/binding/authority")
	}
	if !filepath.IsAbs(l.CanonicalRoot) || !filepath.IsAbs(l.CanonicalPath) || !withinLocalPath(l.CanonicalPath, l.CanonicalRoot) {
		return fmt.Errorf("host locator canonical path/root 无效")
	}
	if l.Identity.empty() || l.Size < 0 || strings.TrimSpace(l.SHA256) == "" || l.Version != "sha256:"+l.SHA256 || l.CreatedAt.IsZero() {
		return fmt.Errorf("host locator identity/size/version 无效")
	}
	return nil
}

type HostLocatorStore struct {
	stateRoot string
	mu        sync.RWMutex
}

func NewHostLocatorStore(stateRoot string) (*HostLocatorStore, error) {
	root, err := localStateRoot(stateRoot)
	if err != nil {
		return nil, err
	}
	return &HostLocatorStore{stateRoot: root}, nil
}

func (s *HostLocatorStore) Create(ctx context.Context, locator HostLocator) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := locator.validate(); err != nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	data, err := json.MarshalIndent(locator, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	filename := s.filename(locator.TenantID, locator.RunID, locator.ID)
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return workcontract.NewError(workcontract.ErrCodeProducedResourceConflict, fmt.Errorf("host locator %s 已存在", locator.ID))
		}
		return err
	}
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(filename)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *HostLocatorStore) Get(ctx context.Context, tenantID, runID, locatorID string) (HostLocator, error) {
	if err := ctx.Err(); err != nil {
		return HostLocator{}, err
	}
	s.mu.RLock()
	data, err := os.ReadFile(s.filename(tenantID, runID, locatorID))
	s.mu.RUnlock()
	if err != nil {
		if os.IsNotExist(err) {
			return HostLocator{}, workcontract.NewError(workcontract.ErrCodeProducedResourceNotFound, fmt.Errorf("host locator %s 不存在", locatorID))
		}
		return HostLocator{}, err
	}
	var locator HostLocator
	if err := json.Unmarshal(data, &locator); err != nil {
		return HostLocator{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("解析 host locator: %w", err))
	}
	if locator.TenantID != strings.TrimSpace(tenantID) || locator.RunID != strings.TrimSpace(runID) || locator.ID != strings.TrimSpace(locatorID) {
		return HostLocator{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("host locator scope 不匹配"))
	}
	if err := locator.validate(); err != nil {
		return HostLocator{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	return locator, nil
}

func (s *HostLocatorStore) filename(tenantID, runID, locatorID string) string {
	return filepath.Join(s.stateRoot, "runtime", "runs", storageKey(tenantID), storageKey(runID), "host-locators", storageKey(locatorID)+".json")
}

type HostResourceIDGenerator interface{ Generate() string }

// HostBackendResourceResolver turns a trusted relative observation into a persisted opaque locator.
type HostBackendResourceResolver struct {
	locators *HostLocatorStore
	ids      HostResourceIDGenerator
	now      func() time.Time
}

func NewHostBackendResourceResolver(locators *HostLocatorStore, ids HostResourceIDGenerator) (*HostBackendResourceResolver, error) {
	if locators == nil || ids == nil {
		return nil, fmt.Errorf("host backend resource resolver 缺少 locator store/id generator")
	}
	return &HostBackendResourceResolver{locators: locators, ids: ids, now: time.Now}, nil
}

func (r *HostBackendResourceResolver) ResolveProducedResource(ctx context.Context, req workcontract.BackendResourceRequest) (workmodel.ResourceRef, error) {
	if err := req.Execution.Backend.Validate(); err != nil ||
		(req.Execution.Backend.Kind != execmodel.BackendKindHost && req.Execution.Backend.Kind != execmodel.BackendKindLocalSandbox) ||
		req.Execution.Backend.Authority != "host" {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, fmt.Errorf("host resolver 收到非 host-backed backend: %v", err))
	}
	if strings.TrimSpace(req.Execution.Binding.ID) == "" || req.Execution.Binding.ID != strings.TrimSpace(req.Execution.Binding.ID) || req.Execution.Binding.Owner.RunID != req.RunID || req.Execution.Binding.Owner.TenantID != req.TenantID {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("host resolver execution scope 不匹配"))
	}
	if req.Availability != workmodel.ResourceAvailabilityDurable {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("Host produced resource 必须声明 durable"))
	}
	if err := req.ObservedPath.Validate(); err != nil {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	rootValue := strings.TrimSpace(req.Execution.Workspace.WorkDir)
	observed := string(req.ObservedPath)
	if strings.HasPrefix(observed, "reserved/") {
		rootValue = strings.TrimSpace(req.Execution.Workspace.OutputDir)
		if rootValue == "" {
			return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("Host OutputDir 未准备，无法解析 reservation"))
		}
	}
	root, candidate, file, info, identity, err := resolveAndOpenHostCandidate(rootValue, req.ObservedPath)
	if err != nil {
		return workmodel.ResourceRef{}, err
	}
	defer file.Close()
	digest, size, err := hashOpenHostFile(file)
	if err != nil {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	after, err := file.Stat()
	if err != nil {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	afterIdentity, err := hostIdentityFromOpenFile(file, after)
	if err != nil || afterIdentity != identity || after.Size() != info.Size() || size != info.Size() || req.Size != size {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("Host produced resource 在登记期间发生变化"))
	}
	mediaType := strings.TrimSpace(req.MediaType)
	if mediaType == "" {
		mediaType = mime.TypeByExtension(filepath.Ext(candidate))
	}
	scope := workmodel.ResourceScope{TenantID: req.Execution.Binding.Owner.TenantID, ProjectID: req.Execution.Binding.Owner.ProjectID, UserID: req.Execution.Binding.Owner.UserID}
	version := "sha256:" + digest
	if prefer := req.PreferSource; prefer != nil &&
		prefer.Authority == req.Execution.Backend.Authority &&
		prefer.Scheme == hostRunFileScheme &&
		prefer.Version == version &&
		prefer.Scope == scope &&
		strings.TrimSpace(prefer.ID) != "" {
		existing, getErr := r.locators.Get(ctx, req.TenantID, req.RunID, prefer.ID)
		if getErr == nil && existing.SHA256 == digest && existing.Size == size && existing.CanonicalPath == candidate && existing.Identity == identity {
			outMedia := existing.MediaType
			if strings.TrimSpace(req.MediaType) != "" {
				outMedia = strings.TrimSpace(req.MediaType)
			}
			return workmodel.ResourceRef{Authority: existing.Authority, Scheme: hostRunFileScheme, ID: existing.ID, Version: existing.Version, MediaType: outMedia, Scope: existing.Scope}, nil
		}
		if getErr != nil && !hostLocatorNotFound(getErr) {
			return workmodel.ResourceRef{}, getErr
		}
	}
	locatorID := "host-locator-" + r.ids.Generate()
	locator := HostLocator{ID: locatorID, TenantID: req.TenantID, RunID: req.RunID, BindingID: req.Execution.Binding.ID, Authority: req.Execution.Backend.Authority, CanonicalRoot: root, CanonicalPath: candidate, Identity: identity, Size: size, SHA256: digest, Version: version, MediaType: mediaType, Scope: scope, CreatedAt: r.now().UTC()}
	if err := r.locators.Create(ctx, locator); err != nil {
		return workmodel.ResourceRef{}, err
	}
	return workmodel.ResourceRef{Authority: locator.Authority, Scheme: hostRunFileScheme, ID: locator.ID, Version: locator.Version, MediaType: locator.MediaType, Scope: locator.Scope}, nil
}

// HostResourceReader reloads the persisted locator and revalidates path, scope, version and identity.
type HostResourceReader struct{ locators *HostLocatorStore }

func NewHostResourceReader(locators *HostLocatorStore) (*HostResourceReader, error) {
	if locators == nil {
		return nil, fmt.Errorf("host resource reader 缺少 locator store")
	}
	return &HostResourceReader{locators: locators}, nil
}

func (r *HostResourceReader) Open(ctx context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	if err := ctx.Err(); err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if ref.Authority == "" || ref.Scheme != hostRunFileScheme || ref.ID == "" || ref.Version == "" {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("Host ResourceRef 无效"))
	}
	// The locator key is tenant/run scoped; run ID is intentionally not encoded in the opaque ref.
	// Callers must attach it through the trusted context.
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok || prepared.Manifest.RunID == "" || prepared.Manifest.Scope != ref.Scope {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("Host reader 缺少可信 Run scope"))
	}
	locator, err := r.locators.Get(ctx, ref.Scope.TenantID, prepared.Manifest.RunID, ref.ID)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if locator.Authority != ref.Authority || locator.Scope != ref.Scope {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("Host locator authority/scope 不匹配"))
	}
	if locator.Version != ref.Version {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("Host ResourceRef version 不匹配"))
	}
	file, info, identity, err := reopenHostLocator(locator)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	digest, size, err := hashOpenHostFile(file)
	if err != nil {
		_ = file.Close()
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	after, statErr := file.Stat()
	var afterIdentity hostFileIdentity
	var identityErr error
	if statErr == nil {
		afterIdentity, identityErr = hostIdentityFromOpenFile(file, after)
	}
	if statErr != nil || identityErr != nil || identity != locator.Identity || afterIdentity != locator.Identity || info.Size() != locator.Size || after.Size() != locator.Size || size != locator.Size || digest != locator.SHA256 {
		_ = file.Close()
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("Host resource identity/size/hash 已变化"))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return workcontract.ResourceHandle{}, err
	}
	verified := &verifiedHostReader{file: file, hash: sha256.New(), locator: locator}
	return workcontract.ResourceHandle{Reader: verified, Size: locator.Size, Version: locator.Version, MediaType: locator.MediaType}, nil
}

type verifiedHostReader struct {
	file    *os.File
	hash    hash.Hash
	locator HostLocator
	read    int64
	done    bool
}

func (r *verifiedHostReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n, err := r.file.Read(p)
	if n > 0 {
		_, _ = r.hash.Write(p[:n])
		r.read += int64(n)
	}
	if err != io.EOF {
		return n, err
	}
	r.done = true
	info, statErr := r.file.Stat()
	var identity hostFileIdentity
	var identityErr error
	if statErr == nil {
		identity, identityErr = hostIdentityFromOpenFile(r.file, info)
	}
	digest := hex.EncodeToString(r.hash.Sum(nil))
	if statErr != nil || identityErr != nil || identity != r.locator.Identity || info.Size() != r.locator.Size || r.read != r.locator.Size || digest != r.locator.SHA256 {
		return n, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("Host resource 在流式读取期间发生变化"))
	}
	return n, io.EOF
}

func (r *verifiedHostReader) Close() error { return r.file.Close() }

func resolveAndOpenHostCandidate(rootValue string, observed workmodel.WorkspacePath) (string, string, *os.File, os.FileInfo, hostFileIdentity, error) {
	root, err := filepath.Abs(strings.TrimSpace(rootValue))
	if err != nil || strings.TrimSpace(rootValue) == "" {
		return "", "", nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("Host workspace root 无效"))
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	candidate := filepath.Join(root, filepath.FromSlash(string(observed)))
	canonical, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", "", nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	if canonical != candidate || !withinLocalPath(canonical, root) {
		return "", "", nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("Host produced path 包含链接或越过 workspace root"))
	}
	if err := rejectUnsafeHostPath(root, canonical); err != nil {
		return "", "", nil, nil, hostFileIdentity{}, err
	}
	file, info, identity, err := openHostFileNoFollow(canonical)
	if err != nil {
		return "", "", nil, nil, hostFileIdentity{}, err
	}
	return root, canonical, file, info, identity, nil
}

func reopenHostLocator(locator HostLocator) (*os.File, os.FileInfo, hostFileIdentity, error) {
	root, err := filepath.EvalSymlinks(locator.CanonicalRoot)
	if err != nil || root != locator.CanonicalRoot {
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("Host locator root 已变化"))
	}
	canonical, err := filepath.EvalSymlinks(locator.CanonicalPath)
	if err != nil || canonical != locator.CanonicalPath || !withinLocalPath(canonical, root) {
		return nil, nil, hostFileIdentity{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("Host locator path 已变化或越界"))
	}
	if err := rejectUnsafeHostPath(root, canonical); err != nil {
		return nil, nil, hostFileIdentity{}, err
	}
	return openHostFileNoFollow(canonical)
}

func rejectUnsafeHostPath(root, candidate string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("Host path 越界"))
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		unsafe, err := unsafeHostPathComponent(current)
		if err != nil {
			return workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
		}
		if unsafe {
			return workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("Host path component 是 symlink/reparse point: %s", current))
		}
	}
	return nil
}

func hashOpenHostFile(file *os.File) (string, int64, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func withinLocalPath(candidate, root string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func hostLocatorNotFound(err error) bool {
	var classified *workcontract.Error
	return errors.As(err, &classified) && classified.Code == workcontract.ErrCodeProducedResourceNotFound
}
