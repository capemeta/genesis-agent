package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type SessionFileReader struct {
	files   sandboxcontract.FileSystemClient
	store   RemoteLocatorStore
	renewer SessionRenewer
	now     func() time.Time
}

func NewSessionFileReader(files sandboxcontract.FileSystemClient, store RemoteLocatorStore) (*SessionFileReader, error) {
	if files == nil || store == nil {
		return nil, fmt.Errorf("session-file reader 缺少 files/store")
	}
	return &SessionFileReader{files: files, store: store, now: time.Now}, nil
}

// WithRenewer 在 locator 快照过期时尽力续租 live Session；可为 nil。
func (r *SessionFileReader) WithRenewer(renewer SessionRenewer) *SessionFileReader {
	if r != nil {
		r.renewer = renewer
	}
	return r
}

func (r *SessionFileReader) Open(ctx context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	locator, err := loadRemoteLocator(ctx, r.store, ref, SessionFileScheme)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if locator.ExpiresAt == nil || !locator.ExpiresAt.After(r.now()) {
		if err := r.renewIfPossible(ctx, locator); err != nil {
			return workcontract.ResourceHandle{}, err
		}
	}
	resolved := remoteResolvedPath(locator.Path, locator.Workspace.ID)
	before, err := r.files.Stat(ctx, sandboxcontract.FileRequest{Workspace: locator.Workspace, Path: resolved})
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if err := validateSessionStat(before, locator); err != nil {
		return workcontract.ResourceHandle{}, err
	}
	content, err := r.files.ReadFile(ctx, sandboxcontract.FileRequest{Workspace: locator.Workspace, Path: resolved}, fscontract.ReadOptions{MaxBytes: locator.Size + 1})
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	after, err := r.files.Stat(ctx, sandboxcontract.FileRequest{Workspace: locator.Workspace, Path: resolved})
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if err := validateSessionStat(after, locator); err != nil || before.Size != after.Size || !before.ModifiedAt.Equal(after.ModifiedAt) {
		if err != nil {
			return workcontract.ResourceHandle{}, err
		}
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("session-file 在读取期间变化"))
	}
	if int64(len(content)) != locator.Size || !matchesSHA256(locator.Version, content) {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("session-file content version 不一致"))
	}
	return workcontract.ResourceHandle{Reader: io.NopCloser(bytes.NewReader(content)), Size: locator.Size, Version: locator.Version, MediaType: locator.MediaType}, nil
}

func (r *SessionFileReader) renewIfPossible(ctx context.Context, locator RemoteLocator) error {
	if r.renewer == nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("session-file locator 已过期"))
	}
	if _, err := renewSessionFileLease(ctx, r.renewer, locator.Workspace); err != nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("session-file locator 已过期且续租失败: %w", err))
	}
	return nil
}

func validateSessionStat(stat *fsmodel.FileStat, locator RemoteLocator) error {
	if stat == nil || stat.Type != fsmodel.EntryTypeFile || stat.IsSymlink || stat.Size != locator.Size {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("session-file identity/size 已变化"))
	}
	if hash := strings.TrimSpace(stat.Hash); hash != "" && normalizeSHA256(hash) != normalizeSHA256(locator.Version) {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("session-file hash 已变化"))
	}
	return nil
}

// ExecutorObjectClient opens immutable remote executor objects as a stream.
type ExecutorObjectClient interface {
	OpenObject(ctx context.Context, objectID string) (io.ReadCloser, error)
}

// ArtifactByteDownloader adapts the legacy genesis-sandbox download endpoint.
// Its []byte contract buffers the complete object before MaxBytes can be
// enforced; replace it with a streaming HTTP OpenObject implementation for
// large/production objects.
type ArtifactByteDownloader interface {
	DownloadArtifact(ctx context.Context, artifactID string) ([]byte, error)
}

type BufferedExecutorObjectClient struct {
	downloader ArtifactByteDownloader
	maxBytes   int64
}

func NewBufferedExecutorObjectClient(downloader ArtifactByteDownloader, maxBytes int64) (*BufferedExecutorObjectClient, error) {
	if downloader == nil || maxBytes <= 0 {
		return nil, fmt.Errorf("buffered executor object client 缺少 downloader/max_bytes")
	}
	return &BufferedExecutorObjectClient{downloader: downloader, maxBytes: maxBytes}, nil
}

func (c *BufferedExecutorObjectClient) OpenObject(ctx context.Context, objectID string) (io.ReadCloser, error) {
	content, err := c.downloader.DownloadArtifact(ctx, objectID)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > c.maxBytes {
		return nil, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("executor object 超过 buffered adapter 限额"))
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

type ExecutorObjectReader struct {
	objects ExecutorObjectClient
	store   RemoteLocatorStore
}

func NewExecutorObjectReader(objects ExecutorObjectClient, store RemoteLocatorStore) (*ExecutorObjectReader, error) {
	if objects == nil || store == nil {
		return nil, fmt.Errorf("executor-object reader 缺少 objects/store")
	}
	return &ExecutorObjectReader{objects: objects, store: store}, nil
}

func (r *ExecutorObjectReader) Open(ctx context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	locator, err := loadRemoteLocator(ctx, r.store, ref, ExecutorObjectScheme)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if locator.ExpiresAt != nil && !locator.ExpiresAt.After(time.Now()) {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("executor-object locator 已过期"))
	}
	reader, err := r.objects.OpenObject(ctx, locator.ObjectID)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	verified := &versionVerifyingReader{reader: reader, expectedSize: locator.Size, expectedVersion: locator.Version, hash: sha256.New()}
	return workcontract.ResourceHandle{Reader: verified, Size: locator.Size, Version: locator.Version, MediaType: locator.MediaType}, nil
}

func loadRemoteLocator(ctx context.Context, store RemoteLocatorStore, ref workmodel.ResourceRef, scheme string) (RemoteLocator, error) {
	if ref.Authority != RemoteExecutorAuthority || ref.Scheme != scheme {
		return RemoteLocator{}, workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, fmt.Errorf("remote resource authority/scheme 不匹配"))
	}
	locator, err := store.Get(ctx, ref.ID, ref.Scope)
	if err != nil {
		return RemoteLocator{}, err
	}
	if locator.Authority != ref.Authority || locator.Scheme != ref.Scheme {
		return RemoteLocator{}, workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, fmt.Errorf("remote locator backend 不匹配"))
	}
	if locator.Version != ref.Version {
		return RemoteLocator{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("remote locator version 不匹配"))
	}
	return locator, nil
}

type versionVerifyingReader struct {
	reader          io.ReadCloser
	hash            hashWriter
	expectedSize    int64
	expectedVersion string
	read            int64
	verified        bool
}

type hashWriter interface {
	Write([]byte) (int, error)
	Sum([]byte) []byte
}

func (r *versionVerifyingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.read += int64(n)
		_, _ = r.hash.Write(p[:n])
		if r.read > r.expectedSize {
			return n, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("executor object size 超出版本记录"))
		}
	}
	if err == io.EOF && !r.verified {
		r.verified = true
		actual := "sha256:" + hex.EncodeToString(r.hash.Sum(nil))
		if r.read != r.expectedSize || (strings.HasPrefix(strings.ToLower(r.expectedVersion), "sha256:") && !strings.EqualFold(actual, r.expectedVersion)) {
			return n, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("executor object content version 不一致"))
		}
	}
	return n, err
}

func (r *versionVerifyingReader) Close() error { return r.reader.Close() }

func normalizeSHA256(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "sha256:"))
}

func matchesSHA256(version string, content []byte) bool {
	if !strings.HasPrefix(strings.ToLower(version), "sha256:") {
		return true
	}
	digest := sha256.Sum256(content)
	return strings.EqualFold(version, "sha256:"+hex.EncodeToString(digest[:]))
}
