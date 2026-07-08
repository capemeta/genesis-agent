// Package skill 装配 CLI 产品的 Package marketplace 管理服务。
package skill

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	marketservice "genesis-agent/internal/capabilities/package/marketplace/service"
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

func NewMarketplaceService() (*marketservice.Service, Paths, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, Paths{}, err
	}
	svc, err := marketservice.New(marketservice.Options{
		Registry:     skillmarket.NewRegistryStore(paths.MarketplaceFile),
		Installs:     skillmarket.NewInstallStore(paths.InstallFile),
		Capabilities: skillmarket.NewCapabilityIndexStore(paths.CapabilityIndexFile),
		Parser:       skillmarket.NewParser(),
		Fetcher:      skillmarket.NewFetcher(paths.CacheDir, http.DefaultClient),
		Installer: skillmarket.NewInstaller(skillmarket.InstallerOptions{
			UserInstalledDir:    paths.UserInstalledDir,
			ProjectInstalledDir: paths.ProjectInstalledDir,
			ProjectPath:         paths.Workspace,
		}),
	})
	if err != nil {
		return nil, Paths{}, err
	}
	return svc, paths, nil
}
