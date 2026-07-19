// Package http 提供 genesis-sandbox HTTP API 的产品无关客户端适配。
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

type workspaceFileInfo struct {
	Path        string    `json:"path"`
	SandboxPath string    `json:"sandbox_path,omitempty"`
	Environment string    `json:"environment,omitempty"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Size        int64     `json:"size,omitempty"`
	SHA256      string    `json:"sha256,omitempty"`
	MIME        string    `json:"mime,omitempty"`
	ModTime     time.Time `json:"mod_time,omitempty"`
}

type workspaceListResult struct {
	Path      string              `json:"path"`
	Entries   []workspaceFileInfo `json:"entries"`
	Truncated bool                `json:"truncated"`
	Limit     int                 `json:"limit"`
}

func (c *Client) ReadFile(ctx context.Context, req sandboxcontract.FileRequest, opts fscontract.ReadOptions) ([]byte, error) {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	resp, err := c.doRaw(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/files", query, "", nil, nil)
	if err != nil {
		return nil, fsErrorFromExec(err, workspacePath)
	}
	defer resp.Body.Close()
	reader := io.Reader(resp.Body)
	if opts.MaxBytes > 0 {
		reader = io.LimitReader(resp.Body, opts.MaxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fscontract.NewError(fscontract.ErrCodeInvalidInput, workspacePath, fmt.Errorf("读取sandbox session文件失败: %w", err))
	}
	if opts.MaxBytes > 0 && int64(len(data)) > opts.MaxBytes {
		return nil, fscontract.NewError(fscontract.ErrCodeTooLarge, workspacePath, fmt.Errorf("文件超过读取上限: %d", opts.MaxBytes))
	}
	return data, nil
}

func (c *Client) WriteFile(ctx context.Context, req sandboxcontract.WriteFileRequest) error {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	headers := make(http.Header)
	// 对齐 SDK UploadSessionFileConditional：If-Match 与 If-None-Match 互斥。
	if ifMatch := normalizeContentHash(req.Options.ExpectedHash); ifMatch != "" {
		headers.Set("If-Match", ifMatch)
	} else if !req.Options.Overwrite {
		headers.Set("If-None-Match", "*")
	}
	resp, err := c.doRaw(ctx, http.MethodPut, "/v1/sessions/"+url.PathEscape(sessionID)+"/files", query, "application/octet-stream", bytes.NewReader(req.Content), headers)
	if err != nil {
		return fsErrorFromExec(err, workspacePath)
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) ListDir(ctx context.Context, req sandboxcontract.ListDirRequest) ([]fsmodel.DirEntry, error) {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	query.Set("recursive", "false")
	if req.Options.MaxEntries > 0 {
		query.Set("limit", strconv.Itoa(req.Options.MaxEntries))
	}
	var result workspaceListResult
	if err := c.doSessionJSON(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/files:list", query, nil, &result); err != nil {
		return nil, fsErrorFromExec(err, workspacePath)
	}
	return dirEntriesFromWorkspace(result.Entries), nil
}

func (c *Client) Walk(ctx context.Context, req sandboxcontract.WalkRequest) (*fsmodel.WalkOutcome, error) {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	query.Set("recursive", "true")
	if req.Options.MaxEntries > 0 {
		query.Set("limit", strconv.Itoa(req.Options.MaxEntries))
	}
	var result workspaceListResult
	if err := c.doSessionJSON(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/files:list", query, nil, &result); err != nil {
		return nil, fsErrorFromExec(err, workspacePath)
	}
	entries := dirEntriesFromWorkspace(result.Entries)
	out := &fsmodel.WalkOutcome{Root: workspacePath, Entries: entries, Truncated: result.Truncated}
	for _, entry := range entries {
		if entry.Type == fsmodel.EntryTypeDir {
			out.DirsSeen++
		} else {
			out.FilesSeen++
			out.BytesSeen += entry.Size
		}
	}
	if result.Truncated {
		out.LimitCause = "max_entries"
	}
	return out, nil
}

func (c *Client) Stat(ctx context.Context, req sandboxcontract.FileRequest) (*fsmodel.FileStat, error) {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	var info workspaceFileInfo
	if err := c.doSessionJSON(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/files:stat", query, nil, &info); err != nil {
		return nil, fsErrorFromExec(err, workspacePath)
	}
	stat := fileStatFromWorkspace(info, req.Path)
	return &stat, nil
}

func (c *Client) MkdirAll(ctx context.Context, req sandboxcontract.MkdirRequest) error {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	if req.Options.Parents {
		query.Set("parents", "true")
	}
	if err := c.doSessionJSON(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/dirs", query, nil, nil); err != nil {
		return fsErrorFromExec(err, workspacePath)
	}
	return nil
}

func (c *Client) Remove(ctx context.Context, req sandboxcontract.RemoveRequest) error {
	sessionID, workspacePath, err := sessionFileTarget(req.Workspace, req.Path)
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("path", workspacePath)
	query.Set("recursive", strconv.FormatBool(req.Options.Recursive))
	if err := c.doSessionJSON(ctx, http.MethodDelete, "/v1/sessions/"+url.PathEscape(sessionID)+"/files", query, nil, nil); err != nil {
		return fsErrorFromExec(err, workspacePath)
	}
	return nil
}

func (c *Client) doSessionJSON(ctx context.Context, method, apiPath string, query url.Values, body io.Reader, out any) error {
	resp, err := c.doRaw(ctx, method, apiPath, query, "application/json", body, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("解析sandbox session文件响应失败: %w", err))
	}
	return nil
}

func sessionFileTarget(workspace sandboxcontract.WorkspaceRef, resolved fsmodel.ResolvedPath) (string, string, error) {
	sessionID := strings.TrimSpace(workspace.Metadata["session_id"])
	if sessionID == "" {
		sessionID = strings.TrimSpace(workspace.ID)
	}
	if sessionID == "" {
		return "", "", fscontract.NewError(fscontract.ErrCodeInvalidInput, "", fmt.Errorf("sandbox session id不能为空"))
	}
	workspacePath, err := cleanWorkspacePath(firstNonEmpty(resolved.WorkspaceRel, resolved.BackendPath, resolved.DisplayPath, resolved.RawPath))
	if err != nil {
		return "", "", err
	}
	return sessionID, workspacePath, nil
}

func cleanWorkspacePath(raw string) (string, error) {
	p := strings.TrimSpace(filepath.ToSlash(raw))
	if p == "" || p == "." {
		return ".", nil
	}
	if strings.HasPrefix(p, "/workspace/") {
		p = strings.TrimPrefix(p, "/workspace/")
	} else if p == "/workspace" {
		return ".", nil
	} else if strings.HasPrefix(p, "/") {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, fmt.Errorf("sandbox session文件路径必须是workspace相对路径或/workspace内路径"))
	}
	if strings.Contains(p, ":") {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, fmt.Errorf("sandbox session文件路径不能是宿主绝对路径"))
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return "", fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, fmt.Errorf("sandbox session文件路径不能包含.."))
		}
	}
	clean := path.Clean("/" + p)
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." {
		return ".", nil
	}
	return clean, nil
}
func dirEntriesFromWorkspace(in []workspaceFileInfo) []fsmodel.DirEntry {
	out := make([]fsmodel.DirEntry, 0, len(in))
	for _, info := range in {
		out = append(out, fsmodel.DirEntry{
			Name:       firstNonEmpty(info.Name, path.Base(info.Path)),
			Path:       info.Path,
			Type:       entryTypeFromKind(info.Kind),
			Size:       info.Size,
			ModifiedAt: info.ModTime,
		})
	}
	return out
}

func fileStatFromWorkspace(info workspaceFileInfo, resolved fsmodel.ResolvedPath) fsmodel.FileStat {
	hash := strings.TrimSpace(info.SHA256)
	if hash != "" && !strings.HasPrefix(strings.ToLower(hash), "sha256:") {
		hash = "sha256:" + hash
	}
	return fsmodel.FileStat{
		Path:       resolved,
		Type:       entryTypeFromKind(info.Kind),
		Size:       info.Size,
		ModifiedAt: info.ModTime,
		Hash:       hash,
	}
}

func entryTypeFromKind(kind string) fsmodel.EntryType {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "file":
		return fsmodel.EntryTypeFile
	case "dir", "directory":
		return fsmodel.EntryTypeDir
	case "symlink", "link":
		return fsmodel.EntryTypeSymlink
	default:
		return fsmodel.EntryTypeOther
	}
}

func fsErrorFromExec(err error, workspacePath string) error {
	if err == nil {
		return nil
	}
	if isNotFoundExecError(err) {
		return fscontract.NewError(fscontract.ErrCodeNotFound, workspacePath, err)
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "if-none-match") || strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "already_exists") {
		return fscontract.NewError(fscontract.ErrCodeAlreadyExists, workspacePath, err)
	}
	if strings.Contains(msg, "if-match") || strings.Contains(msg, "precondition") ||
		strings.Contains(msg, "version changed") || strings.Contains(msg, "conflict") {
		return fscontract.NewError(fscontract.ErrCodeModifiedExternally, workspacePath, err)
	}
	switch execcontract.CodeOf(err) {
	case execcontract.ErrCodePermissionDenied:
		return fscontract.NewError(fscontract.ErrCodePermissionDenied, workspacePath, err)
	case execcontract.ErrCodeInvalidInput:
		return fscontract.NewError(fscontract.ErrCodeInvalidPath, workspacePath, err)
	default:
		return fscontract.NewError(fscontract.ErrCodeInvalidInput, workspacePath, err)
	}
}

// normalizeContentHash 把 FileStat.Hash（可能带 sha256: 前缀）转成服务端 If-Match 接受的裸摘要。
func normalizeContentHash(raw string) string {
	hash := strings.TrimSpace(raw)
	hash = strings.Trim(hash, `"'`)
	if hash == "" {
		return ""
	}
	lower := strings.ToLower(hash)
	if strings.HasPrefix(lower, "sha256:") {
		return strings.TrimSpace(hash[len("sha256:"):])
	}
	return hash
}

func isNotFoundExecError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "not_found") ||
		strings.Contains(msg, "sandbox_not_found") ||
		strings.Contains(msg, "no such") ||
		strings.Contains(msg, "could not find the file") ||
		strings.Contains(msg, "cannot find the file") ||
		strings.Contains(msg, "404")
}
