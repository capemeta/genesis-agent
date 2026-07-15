// Package skill 装配 CLI 产品的 Package marketplace 管理服务。
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	capcontract "genesis-agent/internal/capabilities/capability/contract"
	marketpolicy "genesis-agent/internal/capabilities/package/marketplace/policy"
	marketservice "genesis-agent/internal/capabilities/package/marketplace/service"
	platformconfig "genesis-agent/internal/platform/config"
	"genesis-agent/shared/local/skillmarket"
)

type Paths struct {
	ConfigHome          string
	Workspace           string
	CacheDir            string
	MarketplaceFile     string
	InstallFile         string
	CapabilityIndexFile string
	UserSkillsDir       string
	ProjectSkillsDir    string
	UserInstalledDir    string
	ProjectInstalledDir string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return Paths{}, fmt.Errorf("无法定位用户主目录")
	}
	workspace, err := os.Getwd()
	if err != nil {
		return Paths{}, err
	}
	configHome := filepath.Join(home, ".genesis-agent", "cli")
	return Paths{
		ConfigHome:          configHome,
		Workspace:           workspace,
		CacheDir:            filepath.Join(configHome, "marketplaces"),
		MarketplaceFile:     filepath.Join(configHome, "marketplaces.json"),
		InstallFile:         filepath.Join(configHome, "installed-packages.json"),
		CapabilityIndexFile: filepath.Join(configHome, "capability-index.json"),
		UserSkillsDir:       filepath.Join(configHome, "skills"),
		ProjectSkillsDir:    filepath.Join(workspace, ".genesis", "skills"),
		UserInstalledDir:    filepath.Join(configHome, "installed", "packages"),
		ProjectInstalledDir: filepath.Join(workspace, ".genesis", "installed", "packages"),
	}, nil
}

// MarketplaceOptions 控制 marketplace 与 runtime adapter 的共享。
type MarketplaceOptions struct {
	Adapters     capcontract.RuntimeAdapterRegistry
	Install      platformconfig.SkillsInstallConfig
	ConfigDir    string // 可选；空则不从磁盘重载，仅用 Install 字段
	SkipLoadCfg  bool   // 测试用：跳过 LoadWithOptions
}

func NewMarketplaceService() (*marketservice.Service, Paths, error) {
	return NewMarketplaceServiceWith(MarketplaceOptions{})
}

// NewMarketplaceServiceWith 创建 marketplace 服务；Adapters 非空时可与 MCP/Tool RuntimeAdapter 热更新闭环。
func NewMarketplaceServiceWith(opts MarketplaceOptions) (*marketservice.Service, Paths, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, Paths{}, err
	}
	installCfg := opts.Install
	if !opts.SkipLoadCfg {
		configDir := strings.TrimSpace(opts.ConfigDir)
		if configDir == "" {
			if _, err := os.Stat("configs"); err == nil {
				configDir = "configs"
			} else {
				configDir = paths.ConfigHome
			}
		}
		if cfg, err := platformconfig.LoadWithOptions(configDir, platformconfig.LoadOptions{Product: "cli"}); err == nil && cfg != nil {
			installCfg = cfg.Skills.Install
		}
	}
	hosts := installCfg.EffectiveAllowedHosts()
	svc, err := marketservice.New(marketservice.Options{
		Registry:     skillmarket.NewRegistryStore(paths.MarketplaceFile),
		Installs:     skillmarket.NewInstallStore(paths.InstallFile),
		Capabilities: skillmarket.NewCapabilityIndexStore(paths.CapabilityIndexFile),
		Parser:       skillmarket.NewParserWithHosts(hosts),
		Fetcher:      skillmarket.NewFetcherWithHosts(paths.CacheDir, nil, hosts),
		Installer: skillmarket.NewInstaller(skillmarket.InstallerOptions{
			UserInstalledDir:    paths.UserInstalledDir,
			ProjectInstalledDir: paths.ProjectInstalledDir,
			ProjectPath:         paths.Workspace,
		}),
		Policy: marketpolicy.AllowHosts{
			Hosts:      hosts,
			AllowLocal: installCfg.EffectiveAllowLocal(),
		},
		Adapters: opts.Adapters,
	})
	if err != nil {
		return nil, Paths{}, err
	}
	return svc, paths, nil
}
