package skillmarket

import (
	"context"
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"os"
	"path/filepath"
	"sort"
	"time"

	marketcontract "genesis-agent/internal/capabilities/package/marketplace/contract"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

type InstallerOptions struct {
	UserInstalledDir    string
	ProjectInstalledDir string
	ProjectPath         string
}

type Installer struct {
	opts InstallerOptions
}

func NewInstaller(opts InstallerOptions) *Installer { return &Installer{opts: opts} }

func (i *Installer) Install(ctx context.Context, req marketcontract.InstallRequest) (marketmodel.InstallRecord, error) {
	if err := ctx.Err(); err != nil {
		return marketmodel.InstallRecord{}, err
	}
	if req.Package.Source == "" {
		req.Package.Source = "./"
	}
	base, err := safeJoin(req.Marketplace.InstallLocation, req.Package.Source)
	if err != nil {
		return marketmodel.InstallRecord{}, err
	}
	installRoot, err := i.packageInstallRoot(req.Scope, req.Marketplace.Name, req.Package.Name)
	if err != nil {
		return marketmodel.InstallRecord{}, err
	}
	if _, err := os.Stat(installRoot); err == nil && !req.Force {
		return marketmodel.InstallRecord{}, fmt.Errorf("package目标已存在: %s", installRoot)
	}
	if err := os.RemoveAll(installRoot); err != nil {
		return marketmodel.InstallRecord{}, err
	}
	if err := copyDir(base, installRoot); err != nil {
		return marketmodel.InstallRecord{}, err
	}
	projection, err := i.projectCapabilities(installRoot, req)
	if err != nil {
		_ = os.RemoveAll(installRoot)
		return marketmodel.InstallRecord{}, err
	}
	if len(projection.capabilities) == 0 {
		_ = os.RemoveAll(installRoot)
		return marketmodel.InstallRecord{}, fmt.Errorf("package %s 不包含可投影capability", req.Package.Name)
	}
	now := time.Now().UTC()
	return marketmodel.InstallRecord{
		Package:               req.Package.Name,
		PackageType:           req.Package.Type,
		Marketplace:           req.Marketplace.Name,
		Spec:                  marketmodel.PackageSpec(req.Package.Name, req.Marketplace.Name),
		Scope:                 req.Scope,
		Enabled:               req.Enabled,
		ProjectPath:           projectPathForScope(req.Scope, i.opts.ProjectPath),
		InstalledAt:           now,
		UpdatedAt:             now,
		Version:               req.Package.Version,
		Skills:                projection.skills,
		SkillRoots:            projection.skillRoots,
		Commands:              req.Package.Commands,
		Capabilities:          projection.capabilities,
		SourceMarketplacePath: sourcePathForRecord(req.Marketplace.Source),
		InstallRoot:           installRoot,
	}, nil
}

func (i *Installer) Uninstall(ctx context.Context, record marketmodel.InstallRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root := record.InstallRoot
	if root == "" {
		var err error
		root, err = i.packageInstallRoot(record.Scope, record.Marketplace, record.Package)
		if err != nil {
			return err
		}
	}
	base, err := i.scopeInstalledDir(record.Scope)
	if err != nil {
		return err
	}
	if !isWithin(base, root) {
		return fmt.Errorf("拒绝删除install根目录外路径: %s", root)
	}
	return os.RemoveAll(root)
}

type capabilityProjection struct {
	skills       []string
	skillRoots   []string
	capabilities []capmodel.CapabilityIndexRecord
}

func (i *Installer) projectCapabilities(installRoot string, req marketcontract.InstallRequest) (capabilityProjection, error) {
	projection := capabilityProjection{}
	skillRootSet := map[string]struct{}{}
	skillSet := map[string]struct{}{}
	for _, capability := range req.Package.Capabilities {
		capabilityPath, err := safeJoin(installRoot, capability.Path)
		if err != nil {
			return capabilityProjection{}, err
		}
		resourcePath, err := relSlash(installRoot, capabilityPath)
		if err != nil {
			return capabilityProjection{}, err
		}
		capabilityName := capability.Name
		if capabilityName == "" {
			capabilityName = filepath.Base(capabilityPath)
		}
		if capability.Type == capmodel.CapabilityTypeSkill {
			if err := validateSkillDir(capabilityPath); err != nil {
				return capabilityProjection{}, err
			}
			if _, ok := skillSet[capabilityName]; !ok {
				skillSet[capabilityName] = struct{}{}
				projection.skills = append(projection.skills, capabilityName)
			}
			root := filepath.Dir(capabilityPath)
			if _, ok := skillRootSet[root]; !ok {
				skillRootSet[root] = struct{}{}
				projection.skillRoots = append(projection.skillRoots, root)
			}
		}
		projection.capabilities = append(projection.capabilities, capmodel.CapabilityIndexRecord{
			Type:             capability.Type,
			Name:             capabilityName,
			Description:      capability.Description,
			Package:          req.Package.Name,
			PackageType:      string(req.Package.Type),
			Marketplace:      req.Marketplace.Name,
			Spec:             marketmodel.PackageSpec(req.Package.Name, req.Marketplace.Name),
			Scope:            string(req.Scope),
			Enabled:          req.Enabled,
			ResourcePath:     resourcePath,
			Entrypoint:       capability.Entrypoint,
			Runtime:          capability.Runtime,
			Products:         capability.Products,
			Permissions:      combinedPermissions(req.Package.Permissions, capability.Permissions),
			InstallRoot:      installRoot,
			ManifestMetadata: capability.Metadata,
		})
		if capability.Type != capmodel.CapabilityTypeSkill {
			continue
		}
		resources, err := projectSkillResources(installRoot, capabilityPath, capabilityName, req)
		if err != nil {
			return capabilityProjection{}, err
		}
		projection.capabilities = append(projection.capabilities, resources...)
	}
	sort.Strings(projection.skills)
	sort.Strings(projection.skillRoots)
	sort.SliceStable(projection.capabilities, func(i, j int) bool {
		left := string(projection.capabilities[i].Type) + projection.capabilities[i].ResourcePath
		right := string(projection.capabilities[j].Type) + projection.capabilities[j].ResourcePath
		return left < right
	})
	return projection, nil
}

func combinedPermissions(packagePermissions, capabilityPermissions []capmodel.Permission) []capmodel.Permission {
	out := make([]capmodel.Permission, 0, len(packagePermissions)+len(capabilityPermissions))
	out = append(out, packagePermissions...)
	out = append(out, capabilityPermissions...)
	return out
}
func projectSkillResources(installRoot, skillPath, skillName string, req marketcontract.InstallRequest) ([]capmodel.CapabilityIndexRecord, error) {
	var out []capmodel.CapabilityIndexRecord
	for _, dir := range []string{"references", "scripts", "assets"} {
		root := filepath.Join(skillPath, dir)
		stat, err := os.Stat(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !stat.IsDir() {
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			resourcePath, err := relSlash(installRoot, path)
			if err != nil {
				return err
			}
			skillRel, err := relSlash(skillPath, path)
			if err != nil {
				return err
			}
			out = append(out, capmodel.CapabilityIndexRecord{
				Type:         capmodel.CapabilityTypeSkillResource,
				Name:         skillName + ":" + skillRel,
				Package:      req.Package.Name,
				PackageType:  string(req.Package.Type),
				Marketplace:  req.Marketplace.Name,
				Spec:         marketmodel.PackageSpec(req.Package.Name, req.Marketplace.Name),
				Scope:        string(req.Scope),
				Enabled:      req.Enabled,
				ResourcePath: resourcePath,
				InstallRoot:  installRoot,
			})
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func relSlash(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return "./" + filepath.ToSlash(rel), nil
}

func (i *Installer) packageInstallRoot(scope marketmodel.InstallScope, marketplace, pkg string) (string, error) {
	base, err := i.scopeInstalledDir(scope)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, safeCacheName(marketplace), safeCacheName(pkg)), nil
}

func (i *Installer) scopeInstalledDir(scope marketmodel.InstallScope) (string, error) {
	switch scope {
	case marketmodel.InstallScopeUser, "":
		if i.opts.UserInstalledDir == "" {
			return "", fmt.Errorf("user installed packages目录未配置")
		}
		return i.opts.UserInstalledDir, nil
	case marketmodel.InstallScopeProject:
		if i.opts.ProjectInstalledDir == "" {
			return "", fmt.Errorf("project installed packages目录未配置")
		}
		return i.opts.ProjectInstalledDir, nil
	default:
		return "", fmt.Errorf("不支持的install scope: %s", scope)
	}
}

func projectPathForScope(scope marketmodel.InstallScope, projectPath string) string {
	if scope == marketmodel.InstallScopeProject {
		return projectPath
	}
	return ""
}

func sourcePathForRecord(source marketmodel.MarketplaceSource) string {
	switch source.Type {
	case marketmodel.SourceTypeDirectory, marketmodel.SourceTypeFile:
		return source.Path
	case marketmodel.SourceTypeGitHub:
		return "github:" + source.Repo
	case marketmodel.SourceTypeGit, marketmodel.SourceTypeURL:
		return source.URL
	default:
		return string(source.Type)
	}
}
