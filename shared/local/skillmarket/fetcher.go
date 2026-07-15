package skillmarket

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	marketcontract "genesis-agent/internal/capabilities/package/marketplace/contract"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

type Fetcher struct {
	cacheBase    string
	httpClient   *http.Client
	allowedHosts []string // 重定向目标白名单；空则仅允许同主机跳转
}

// NewFetcher 创建 Fetcher；client 可为 nil。不会修改传入 client（含 http.DefaultClient）。
func NewFetcher(cacheBase string, client *http.Client) *Fetcher {
	return NewFetcherWithHosts(cacheBase, client, nil)
}

// NewFetcherWithHosts 在下载时按 allowedHosts 校验重定向目标（与 skills.install.allowed_hosts 对齐）。
// allowedHosts 为空时：禁止跨主机重定向（同主机 http↔https / 路径跳转仍允许）。
func NewFetcherWithHosts(cacheBase string, client *http.Client, allowedHosts []string) *Fetcher {
	hosts := normalizeRedirectHosts(allowedHosts)
	return &Fetcher{
		cacheBase:    cacheBase,
		httpClient:   wrapHTTPClient(client, hosts),
		allowedHosts: hosts,
	}
}

func wrapHTTPClient(base *http.Client, allowedHosts []string) *http.Client {
	var out http.Client
	if base != nil {
		out = *base // 浅拷贝，避免污染 DefaultClient
	}
	if out.Timeout == 0 {
		out.Timeout = 60 * time.Second
	}
	out.CheckRedirect = makeRedirectChecker(allowedHosts)
	return &out
}

func makeRedirectChecker(allowedHosts []string) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("重定向次数过多")
		}
		if req.URL.Hostname() == "" {
			return fmt.Errorf("拒绝无主机名的重定向")
		}
		// Host 含端口：避免 127.0.0.1:A → 127.0.0.1:B 被当成同主机。
		if strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			return nil
		}
		if len(allowedHosts) > 0 && hostAllowedRedirect(req.URL.Hostname(), allowedHosts) {
			return nil
		}
		return fmt.Errorf("拒绝跨主机重定向: %s -> %s（目标不在 skills.install.allowed_hosts）", via[0].URL.Host, req.URL.Host)
	}
}

func normalizeRedirectHosts(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	seen := map[string]struct{}{}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		h = strings.TrimPrefix(h, "https://")
		h = strings.TrimPrefix(h, "http://")
		h = strings.Trim(h, "/")
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

func hostAllowedRedirect(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if host == a {
			return true
		}
		// GitHub / 兼容 forge 的常见下载子域（archive URL 常 302 到此）。
		if host == "codeload."+a {
			return true
		}
		if a == "github.com" {
			switch host {
			case "raw.githubusercontent.com", "objects.githubusercontent.com", "codeload.github.com":
				return true
			}
		}
	}
	return false
}

func (f *Fetcher) Fetch(ctx context.Context, req marketcontract.FetchRequest) (marketcontract.FetchResult, error) {
	if req.Existing != nil && !req.Refresh && req.Existing.InstallLocation != "" {
		manifest, err := readMarketplaceFromDirectory(req.Existing.InstallLocation)
		if err == nil {
			hash, _ := directoryHash(req.Existing.InstallLocation)
			return marketcontract.FetchResult{Manifest: manifest, InstallLocation: req.Existing.InstallLocation, LastRevision: req.Existing.LastRevision, ContentHash: hash}, nil
		}
	}
	if err := os.MkdirAll(f.cacheBase, 0o755); err != nil {
		return marketcontract.FetchResult{}, err
	}
	tempDir, err := os.MkdirTemp(f.cacheBase, "tmp-*")
	if err != nil {
		return marketcontract.FetchResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tempDir)
		}
	}()
	if err := f.populate(ctx, req.Source, tempDir); err != nil {
		return marketcontract.FetchResult{}, err
	}
	manifest, err := readMarketplaceFromDirectory(tempDir)
	if err != nil {
		// 允许无 marketplace：单 Skill / 多 Skill 目录由 Service Detect 合成。
		manifest = marketmodel.Manifest{}
	}
	name := safeCacheName(manifest.Name)
	if name == "" {
		name = cacheNameFromSource(req.Source)
	}
	if name == "" {
		return marketcontract.FetchResult{}, fmt.Errorf("marketplace name无效")
	}
	dest := filepath.Join(f.cacheBase, name)
	if req.Existing != nil && req.Existing.Name != "" {
		dest = req.Existing.InstallLocation
	}
	if dest == "" {
		return marketcontract.FetchResult{}, fmt.Errorf("marketplace cache目录为空")
	}
	if !isWithin(f.cacheBase, dest) {
		return marketcontract.FetchResult{}, fmt.Errorf("拒绝替换cache根目录外路径: %s", dest)
	}
	if err := os.RemoveAll(dest); err != nil {
		return marketcontract.FetchResult{}, err
	}
	if err := os.Rename(tempDir, dest); err != nil {
		return marketcontract.FetchResult{}, err
	}
	committed = true
	hash, _ := directoryHash(dest)
	return marketcontract.FetchResult{Manifest: manifest, InstallLocation: dest, ContentHash: hash}, nil
}

func cacheNameFromSource(source marketmodel.MarketplaceSource) string {
	switch source.Type {
	case marketmodel.SourceTypeGitHub:
		return safeCacheName("github-" + strings.ReplaceAll(source.Repo, "/", "-"))
	case marketmodel.SourceTypeURL:
		sum := sha256.Sum256([]byte(source.URL))
		return "url-" + hex.EncodeToString(sum[:8])
	case marketmodel.SourceTypeDirectory, marketmodel.SourceTypeFile:
		sum := sha256.Sum256([]byte(source.Path))
		return "local-" + hex.EncodeToString(sum[:8])
	case marketmodel.SourceTypeGit:
		sum := sha256.Sum256([]byte(source.URL))
		return "git-" + hex.EncodeToString(sum[:8])
	default:
		return ""
	}
}

func (f *Fetcher) RemoveCache(ctx context.Context, record marketmodel.MarketplaceRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.InstallLocation == "" {
		return nil
	}
	if !isWithin(f.cacheBase, record.InstallLocation) {
		return fmt.Errorf("拒绝删除cache根目录外路径: %s", record.InstallLocation)
	}
	return os.RemoveAll(record.InstallLocation)
}

func (f *Fetcher) populate(ctx context.Context, source marketmodel.MarketplaceSource, tempDir string) error {
	switch source.Type {
	case marketmodel.SourceTypeDirectory:
		return copyDir(source.Path, tempDir)
	case marketmodel.SourceTypeFile:
		if err := os.MkdirAll(filepath.Join(tempDir, ".genesis"), 0o755); err != nil {
			return err
		}
		return copyFile(source.Path, filepath.Join(tempDir, ".genesis", "marketplace.json"))
	case marketmodel.SourceTypeGitHub:
		return f.downloadGitHub(ctx, source, tempDir)
	case marketmodel.SourceTypeURL:
		return f.downloadURL(ctx, source.URL, tempDir)
	case marketmodel.SourceTypeGit:
		if repo := githubRepoFromURL(source.URL); repo != "" {
			source.Type = marketmodel.SourceTypeGitHub
			source.Repo = repo
			return f.downloadGitHub(ctx, source, tempDir)
		}
		return fmt.Errorf("git source暂只支持可转换为GitHub zip的URL: %s", source.URL)
	default:
		return fmt.Errorf("不支持的marketplace source type: %s", source.Type)
	}
}

func githubRepoFromURL(input string) string {
	input = strings.TrimSuffix(strings.TrimSpace(input), ".git")
	if strings.HasPrefix(input, "git@github.com:") {
		return strings.TrimPrefix(input, "git@github.com:")
	}
	if parsed, err := url.Parse(input); err == nil && parsed.Host == "github.com" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	return ""
}

func (f *Fetcher) downloadGitHub(ctx context.Context, source marketmodel.MarketplaceSource, tempDir string) error {
	parts := strings.Split(source.Repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("GitHub repo格式应为owner/repo: %s", source.Repo)
	}
	host := strings.TrimSpace(source.Host)
	if host == "" {
		host = "github.com"
	}
	refs := []string{strings.TrimSpace(source.Ref)}
	if refs[0] == "" {
		refs = []string{"main", "master"}
	}
	var last error
	for _, ref := range refs {
		for _, zipURL := range githubZipCandidates(host, parts[0], parts[1], ref) {
			if err := f.downloadAndExtractZip(ctx, zipURL, tempDir, source.SubPath); err == nil {
				return nil
			} else {
				last = err
			}
		}
	}
	return last
}

func githubZipCandidates(host, owner, repo, ref string) []string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		host = "github.com"
	}
	var out []string
	refCandidates := []string{ref}
	if !strings.HasPrefix(ref, "refs/") {
		refCandidates = []string{"refs/heads/" + ref, "refs/tags/" + ref, ref}
	}
	if host == "github.com" {
		for _, candidate := range refCandidates {
			out = append(out, fmt.Sprintf("https://codeload.github.com/%s/%s/zip/%s", owner, repo, candidate))
		}
		return out
	}
	// GitHub Enterprise / 兼容主机：优先官方 archive，其次 codeload 子域
	for _, candidate := range refCandidates {
		out = append(out,
			fmt.Sprintf("https://%s/%s/%s/archive/%s.zip", host, owner, repo, candidate),
			fmt.Sprintf("https://codeload.%s/%s/%s/zip/%s", host, owner, repo, candidate),
		)
	}
	return out
}

func (f *Fetcher) downloadURL(ctx context.Context, rawURL, tempDir string) error {
	data, contentType, disposition, err := f.fetchMeta(ctx, rawURL)
	if err != nil {
		return err
	}
	lowerURL := strings.ToLower(rawURL)
	ct := strings.ToLower(contentType)
	disp := strings.ToLower(disposition)

	// 魔数优先：避免错误 Content-Type / 后缀把 zip 当 json 写入。
	if isZipMagic(data) {
		return extractZipBytes(data, tempDir, "")
	}
	looksJSON := strings.HasSuffix(lowerURL, ".json") ||
		strings.Contains(ct, "application/json") ||
		strings.Contains(ct, "text/json")
	if looksJSON {
		if err := os.MkdirAll(filepath.Join(tempDir, ".genesis"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(tempDir, ".genesis", "marketplace.json"), data, 0o644)
	}
	looksZip := strings.HasSuffix(lowerURL, ".zip") ||
		strings.Contains(ct, "application/zip") ||
		strings.Contains(ct, "application/x-zip") ||
		strings.Contains(disp, ".zip")
	if looksZip {
		return extractZipBytes(data, tempDir, "")
	}
	return fmt.Errorf("url marketplace无法识别为.zip或.json（content-type=%s）: %s", contentType, rawURL)
}

func isZipMagic(data []byte) bool {
	// ZIP local header / empty archive / spanned：均以 PK 开头。
	return len(data) >= 2 && data[0] == 'P' && data[1] == 'K'
}

func (f *Fetcher) downloadAndExtractZip(ctx context.Context, rawURL, tempDir, subPath string) error {
	data, err := f.fetch(ctx, rawURL)
	if err != nil {
		return err
	}
	return extractZipBytes(data, tempDir, subPath)
}

func extractZipBytes(data []byte, tempDir, subPath string) error {
	zipPath := filepath.Join(tempDir, "download.zip")
	if err := os.WriteFile(zipPath, data, 0o600); err != nil {
		return err
	}
	defer os.Remove(zipPath)
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	prefix := ""
	if len(reader.File) > 0 {
		first := filepath.ToSlash(reader.File[0].Name)
		if idx := strings.Index(first, "/"); idx >= 0 {
			prefix = first[:idx+1]
		}
	}
	if subPath != "" {
		if strings.Contains(subPath, "..") {
			return fmt.Errorf("marketplace sub_path不能包含..: %s", subPath)
		}
		prefix += strings.Trim(subPath, "/") + "/"
	}
	extracted := 0
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
		}
		if name == "" {
			continue
		}
		if strings.Contains(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("zip entry越界: %s", file.Name)
		}
		dest, err := safeJoin(tempDir, "./"+name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeOut := out.Close()
		closeIn := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOut != nil {
			return closeOut
		}
		if closeIn != nil {
			return closeIn
		}
		extracted++
	}
	if extracted == 0 {
		return fmt.Errorf("zip中没有可用文件")
	}
	return nil
}

func (f *Fetcher) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	data, _, _, err := f.fetchMeta(ctx, rawURL)
	return data, err
}

func (f *Fetcher) fetchMeta(ctx context.Context, rawURL string) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", "", err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("下载失败 %s: %s", rawURL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
	if err != nil {
		return nil, "", "", err
	}
	return data, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"), nil
}

func copyDir(src, dst string) error {
	root, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("marketplace目录不允许符号链接: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dest := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		return copyFile(path, dest)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func directoryHash(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		_, _ = h.Write([]byte(filepath.ToSlash(rel)))
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(h, file)
		return err
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func safeCacheName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, "/", "-")
	return name
}
