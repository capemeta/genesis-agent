package skillmarket

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	marketcontract "genesis-agent/internal/capabilities/package/marketplace/contract"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

type Fetcher struct {
	cacheBase  string
	httpClient *http.Client
}

func NewFetcher(cacheBase string, client *http.Client) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Fetcher{cacheBase: cacheBase, httpClient: client}
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
		return marketcontract.FetchResult{}, err
	}
	name := safeCacheName(manifest.Name)
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

func (f *Fetcher) downloadGitHub(ctx context.Context, source marketmodel.MarketplaceSource, tempDir string) error {
	parts := strings.Split(source.Repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("GitHub repo格式应为owner/repo: %s", source.Repo)
	}
	refs := []string{strings.TrimSpace(source.Ref)}
	if refs[0] == "" {
		refs = []string{"main", "master"}
	}
	var last error
	for _, ref := range refs {
		candidates := []string{ref}
		if !strings.HasPrefix(ref, "refs/") {
			candidates = []string{"refs/heads/" + ref, "refs/tags/" + ref}
		}
		for _, candidate := range candidates {
			url := fmt.Sprintf("https://codeload.github.com/%s/%s/zip/%s", parts[0], parts[1], candidate)
			if err := f.downloadAndExtractZip(ctx, url, tempDir, source.SubPath); err == nil {
				return nil
			} else {
				last = err
			}
		}
	}
	return last
}

func (f *Fetcher) downloadURL(ctx context.Context, rawURL, tempDir string) error {
	lower := strings.ToLower(rawURL)
	if strings.HasSuffix(lower, ".json") {
		data, err := f.fetch(ctx, rawURL)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(tempDir, ".genesis"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(tempDir, ".genesis", "marketplace.json"), data, 0o644)
	}
	if strings.HasSuffix(lower, ".zip") {
		return f.downloadAndExtractZip(ctx, rawURL, tempDir, "")
	}
	return fmt.Errorf("url marketplace只支持.json或.zip: %s", rawURL)
}

func (f *Fetcher) downloadAndExtractZip(ctx context.Context, rawURL, tempDir, subPath string) error {
	data, err := f.fetch(ctx, rawURL)
	if err != nil {
		return err
	}
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
		return fmt.Errorf("zip中没有可用文件: %s", rawURL)
	}
	return nil
}

func (f *Fetcher) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载失败 %s: %s", rawURL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
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
