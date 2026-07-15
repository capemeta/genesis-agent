package config

import (
	"fmt"
	"os"
	"path/filepath"

	hookmodel "genesis-agent/internal/capabilities/hook/model"
	hookservice "genesis-agent/internal/capabilities/hook/service"
	"gopkg.in/yaml.v3"
)

// LoadHookConfig 独立发现 Hook 配置，不再从通用 config.yaml 读取 hooks 段。
func LoadHookConfig(configDir, product string) (hookmodel.Config, error) {
	sources := []hookservice.ConfigSource{}
	add := func(path, name string, managed bool) error {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取 %s Hook 配置失败: %w", name, err)
		}
		var cfg hookmodel.Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("解析 %s Hook 配置失败: %w", name, err)
		}
		sources = append(sources, hookservice.ConfigSource{Name: name, Managed: managed, Config: cfg})
		return nil
	}
	if product != "enterprise" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			if err := add(filepath.Join(home, ".genesis-agent", product, "hooks.yaml"), "user", false); err != nil {
				return hookmodel.Config{}, err
			}
		}
		if err := add(filepath.Join(configDir, "hooks.yaml"), "project-shared", false); err != nil {
			return hookmodel.Config{}, err
		}
		if err := add(filepath.Join(filepath.Dir(configDir), ".genesis", "hooks.yaml"), "project", false); err != nil {
			return hookmodel.Config{}, err
		}
		if err := add(filepath.Join(configDir, "hooks.local.yaml"), "project-local", false); err != nil {
			return hookmodel.Config{}, err
		}
	}
	cfg := hookservice.MergeConfigSources(sources)
	hookservice.ApplyDefaults(&cfg, product)
	if err := hookservice.ValidateConfig(cfg); err != nil {
		return hookmodel.Config{}, err
	}
	return cfg, nil
}
