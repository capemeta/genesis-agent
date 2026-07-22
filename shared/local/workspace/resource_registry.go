package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ResourceRegistry 在产品控制面把已审批的本地路径冻结为版本化 ResourceRef。
// execution backend 只消费 ResourceRef，永远不重新解释模型提交的裸路径。
type ResourceRegistry struct {
	projectRoot string
	mu          sync.RWMutex
	resources   map[string]LocalResource
}

var requestFilePattern = regexp.MustCompile(`(?i)(?:^|[\s"'（(])([^\s"'，。；：、（）()]+?\.(?:md|markdown|txt|csv|tsv|json|ya?ml|pdf|docx?|xlsx?|pptx?|html?|xml|go|py|js|ts|java|sql|png|jpe?g))`)
var requestOutputPattern = regexp.MustCompile(`(?i)(?:文件名(?:称)?为|命名为|取名为|名称为|另存为|保存为|重命名为|改名为|输出为|导出为|生成(?:一个|一份)?|创建(?:一个|一份)?|制作(?:一个|一份)?|save\s+as|rename\s+to|export\s+as)\s*["']?([^\s"'，。；：、（）()]+\.(?:md|markdown|txt|csv|tsv|json|ya?ml|pdf|docx?|xlsx?|pptx?|html?|xml|png|jpe?g))`)

var requestInputPrefixes = []string{"请帮我", "请基于", "请根据", "请修改", "请编辑", "请打开", "请读取", "请分析", "请处理", "请使用", "请参考", "请把", "基于", "根据", "修改", "编辑", "打开", "读取", "分析", "处理", "使用", "参考", "把"}

func NewResourceRegistry(projectRoot string) (*ResourceRegistry, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("resource registry 缺少 project root")
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &ResourceRegistry{projectRoot: filepath.Clean(root), resources: map[string]LocalResource{}}, nil
}

// ResolveInputs 将一次已批准工具调用中的路径转换为稳定资源引用。
func (r *ResourceRegistry) ResolveInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error) {
	return r.resolveInputs(ctx, inputs, false)
}

// ResolveAvailableInputs 用于“可能来自 Skill 包，也可能来自当前工作区”的命令入口。
// 它只忽略精确路径不存在；任何越界、权限或文件类型错误仍 fail closed。
func (r *ResourceRegistry) ResolveAvailableInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error) {
	return r.resolveInputs(ctx, inputs, true)
}

func (r *ResourceRegistry) resolveInputs(ctx context.Context, inputs []string, skipMissing bool) ([]workmodel.ResourceRef, error) {
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return nil, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("解析 execution 输入缺少 PreparedRun"))
	}
	workspace := prepared.Execution.Workspace
	refs := make([]workmodel.ResourceRef, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, raw := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(filepath.ToSlash(raw), "/workspace/") {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("执行面路径不能作为本地输入: %s", raw))
		}
		candidate, _, err := workmodel.ExpandLogicalPath(raw, workspace)
		if err != nil {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, err)
		}
		alias, aliasErr := aliasWithinWorkspace(candidate, workspace)
		if aliasErr != nil {
			return nil, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, fmt.Errorf("输入不属于当前 execution workspace: %s", raw))
		}
		ref, real, err := r.register(ctx, candidate, alias, prepared.Manifest.Scope)
		if err != nil {
			if skipMissing && os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		key := registryPathKey(real)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}
	return refs, nil
}

// PlanRequestInputs 只解析 prompt 中精确出现、位于已授权项目根且当前存在的普通文件。
// 不存在的引用通常是交付目标，因此不会被猜测或自动创建为输入。
func (r *ResourceRegistry) PlanRequestInputs(ctx context.Context, req workcontract.RequestInputRequest) ([]workmodel.ResourceRef, error) {
	// 先移除明确的输出目标片段，避免目标文件恰好已存在时被误绑定成输入。
	matches := requestFilePattern.FindAllStringSubmatch(requestOutputPattern.ReplaceAllString(req.Prompt, " "), -1)
	refs := make([]workmodel.ResourceRef, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw := strings.TrimSpace(match[1])
		if raw == "" || filepath.IsAbs(raw) {
			continue
		}
		for _, alias := range requestAliasCandidates(raw) {
			candidate := filepath.Join(r.projectRoot, filepath.FromSlash(string(alias)))
			real, err := filepath.EvalSymlinks(candidate)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("解析请求输入 %q: %w", raw, err)
			}
			if !within(real, r.projectRoot) {
				return nil, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, fmt.Errorf("请求输入越过 project root: %s", raw))
			}
			ref, real, err := r.register(ctx, real, alias, req.Scope)
			if err != nil {
				return nil, err
			}
			key := registryPathKey(real)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				refs = append(refs, ref)
			}
			break
		}
	}
	return refs, nil
}

// requestAliasCandidates 只移除有限的请求动词前缀；每个候选仍必须在项目根精确存在。
// 原文候选优先，避免真实文件名恰好以“修改/分析”等词开头时被错误截断。
func requestAliasCandidates(raw string) []workmodel.WorkspacePath {
	values := []string{raw}
	for _, prefix := range requestInputPrefixes {
		if strings.HasPrefix(raw, prefix) && len(raw) > len(prefix) {
			values = append(values, strings.TrimPrefix(raw, prefix))
		}
	}
	result := make([]workmodel.WorkspacePath, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if filepath.IsAbs(value) {
			continue
		}
		alias := workmodel.WorkspacePath(filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.ReplaceAll(value, `\`, "/")))))
		if err := alias.Validate(); err != nil {
			continue
		}
		key := strings.ToLower(string(alias))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, alias)
	}
	return result
}

func (r *ResourceRegistry) register(ctx context.Context, candidate string, alias workmodel.WorkspacePath, scope workmodel.ResourceScope) (workmodel.ResourceRef, string, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.ResourceRef{}, "", err
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return workmodel.ResourceRef{}, "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return workmodel.ResourceRef{}, "", err
	}
	file, info, identity, err := openHostFileNoFollow(real)
	if err != nil {
		return workmodel.ResourceRef{}, "", err
	}
	digestValue, size, err := hashOpenHostFile(file)
	if err != nil {
		_ = file.Close()
		return workmodel.ResourceRef{}, "", err
	}
	after, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return workmodel.ResourceRef{}, "", statErr
	}
	afterIdentity, identityErr := hostIdentityFromOpenFile(file, after)
	if identityErr != nil || afterIdentity != identity || after.Size() != info.Size() || size != info.Size() {
		_ = file.Close()
		return workmodel.ResourceRef{}, "", workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("资源在登记期间发生变化"))
	}
	if closeErr := file.Close(); closeErr != nil {
		return workmodel.ResourceRef{}, "", closeErr
	}
	version := "sha256:" + digestValue
	idMaterial := strings.Join([]string{registryPathKey(real), version, scope.TenantID, scope.ProjectID, scope.UserID}, "\x00")
	digest := sha256.Sum256([]byte(idMaterial))
	id := "local-" + hex.EncodeToString(digest[:16])
	resource := LocalResource{Path: real, Version: version, SHA256: digestValue, Size: size, MediaType: mime.TypeByExtension(filepath.Ext(real)), Scope: scope, Identity: identity}
	r.mu.Lock()
	r.resources[id] = resource
	r.mu.Unlock()
	return workmodel.ResourceRef{Authority: "host", Scheme: "file", ID: id, Version: version, Scope: scope, Path: string(alias)}, real, nil
}

func aliasWithinWorkspace(candidate string, workspace execmodel.ExecutionWorkspace) (workmodel.WorkspacePath, error) {
	type rootAlias struct{ root, prefix string }
	roots := []rootAlias{{workspace.WorkDir, ""}, {workspace.InputDir, "input"}, {workspace.OutputDir, "output"}, {workspace.TmpDir, "tmp"}}
	for _, item := range roots {
		if strings.TrimSpace(item.root) == "" || !within(candidate, item.root) {
			continue
		}
		rel, err := filepath.Rel(item.root, candidate)
		if err != nil {
			return "", err
		}
		alias := filepath.ToSlash(rel)
		if item.prefix != "" {
			alias = filepath.ToSlash(filepath.Join(item.prefix, rel))
		}
		path := workmodel.WorkspacePath(alias)
		if err := path.Validate(); err != nil {
			return "", err
		}
		return path, nil
	}
	// 如果是宿主机绝对路径且文件存在，使用 Base 文件名作为 staging 别名
	if filepath.IsAbs(candidate) {
		base := filepath.Base(candidate)
		path := workmodel.WorkspacePath(base)
		if err := path.Validate(); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("path 不在 execution workspace")
}

// Open 按 ResourceRef 打开并复核文件身份版本，审批后变化会稳定失败。
func (r *ResourceRegistry) Open(ctx context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	if err := ctx.Err(); err != nil {
		return workcontract.ResourceHandle{}, err
	}
	r.mu.RLock()
	resource, ok := r.resources[ref.ID]
	r.mu.RUnlock()
	if !ok || ref.Authority != "host" || ref.Scheme != "file" {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, fmt.Errorf("资源未注册: %s", ref.ID))
	}
	if ref.Scope != resource.Scope {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, fmt.Errorf("资源 scope 不匹配: %s", ref.ID))
	}
	file, info, identity, err := openHostFileNoFollow(resource.Path)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if identity != resource.Identity || info.Size() != resource.Size || ref.Version != resource.Version {
		_ = file.Close()
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("资源版本已变化: %s", ref.ID))
	}
	digest, size, hashErr := hashOpenHostFile(file)
	after, statErr := file.Stat()
	if hashErr != nil || statErr != nil {
		_ = file.Close()
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("读取资源版本失败: %s", ref.ID))
	}
	afterIdentity, identityErr := hostIdentityFromOpenFile(file, after)
	if identityErr != nil || identity != afterIdentity || size != resource.Size || digest != resource.SHA256 {
		_ = file.Close()
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("资源内容或身份已变化: %s", ref.ID))
	}
	if _, err := file.Seek(0, 0); err != nil {
		_ = file.Close()
		return workcontract.ResourceHandle{}, err
	}
	return workcontract.ResourceHandle{Reader: file, Size: resource.Size, Version: resource.Version, MediaType: resource.MediaType}, nil
}

func within(candidate, root string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func registryPathKey(value string) string {
	return strings.ToLower(filepath.Clean(value))
}
