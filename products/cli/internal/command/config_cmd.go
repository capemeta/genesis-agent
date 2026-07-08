package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"genesis-agent/internal/platform/config"
)

// newConfigCmd 创建 config 子命令
// config 命令读取并显示当前配置，支持校验模式
func newConfigCmd(configDirRef *string) *cobra.Command {
	var validate bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "查看当前配置信息",
		Long: `显示 Agent Runtime 的当前配置，包括 LLM 服务商、模型、运行策略等。

API Key 等敏感信息仅显示已配置/未配置状态，不打印实际值。
使用 --validate 仅校验配置有效性（适合 CI/部署前检查）。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadWithOptions(*configDirRef, config.LoadOptions{Product: "cli", EnsureUserConfig: true})
			if err != nil {
				return fmt.Errorf("配置加载失败: %w", err)
			}

			if validate {
				fmt.Println("✅ 配置校验通过")
				return nil
			}

			printConfig(cfg)
			return nil
		},
	}

	cmd.Flags().BoolVar(&validate, "validate", false, "仅校验配置是否有效，不打印内容（适合 CI）")

	return cmd
}

// printConfig 美化打印配置信息（脱敏处理敏感字段）
func printConfig(cfg *config.Config) {
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8B5CF6")).
		Bold(true).
		Width(22)
	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F9FAFB"))
	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7C3AED")).
		Bold(true)
	dividerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4B5563"))

	row := func(label, value string) {
		fmt.Printf("  %s %s\n", labelStyle.Render(label), valueStyle.Render(value))
	}

	// 敏感字段脱敏：显示头尾各4位，中间用 * 替代
	masked := func(s string) string {
		if s == "" {
			return "❌ 未配置"
		}
		if len(s) <= 8 {
			return "****（已配置）"
		}
		return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
	}

	divider := dividerStyle.Render(strings.Repeat("─", 50))

	fmt.Println()
	fmt.Println(sectionStyle.Render("▸ LLM 配置"))
	fmt.Println(divider)
	resolved, err := cfg.LLM.ResolveRoute("chat")
	if err != nil {
		row("状态", err.Error())
	} else {
		row("默认路由", resolved.Alias)
		row("Provider", resolved.ProviderName)
		row("Provider 类型", resolved.ProviderKind)
		row("模型", resolved.Model)
		row("策略", resolved.Strategy)
		if resolved.BaseURL != "" {
			row("API Endpoint", resolved.BaseURL)
		}
		if resolved.AuthType == "api_key" {
			row("API Key", masked(resolved.APIKey))
		} else {
			row("认证", resolved.AuthType)
		}
	}
	if cfg.LLM.Timeout > 0 {
		row("请求超时", cfg.LLM.Timeout.String())
	}

	fmt.Println()
	fmt.Println(sectionStyle.Render("▸ Agent 运行策略"))
	fmt.Println(divider)
	row("最大迭代次数", fmt.Sprintf("%d", cfg.Agent.MaxIterations))
	if cfg.Agent.SystemPrompt != "" {
		prompt := cfg.Agent.SystemPrompt
		if len(prompt) > 80 {
			prompt = prompt[:80] + "..."
		}
		row("系统提示词", prompt)
	}

	fmt.Println()
	fmt.Println()
	fmt.Println(sectionStyle.Render("▸ Web 搜索/获取"))
	fmt.Println(divider)
	row("Brave API Key", masked(cfg.Web.BraveAPIKey))
	row("Tavily API Key", masked(cfg.Web.TavilyAPIKey))
	row("Exa API Key", masked(cfg.Web.ExaAPIKey))
	row("SerpAPI Key", masked(cfg.Web.SerpAPIKey))
	if cfg.Web.SearXNGBaseURL != "" {
		row("SearXNG Base URL", cfg.Web.SearXNGBaseURL)
	} else {
		row("SearXNG Base URL", "未配置")
	}

	fmt.Println()
	fmt.Println(sectionStyle.Render("▸ Skills 配置"))
	fmt.Println(divider)
	row("用户配置文件", defaultUserConfigPath())
	row("用户 Skills 目录", filepath.Join(defaultConfigHome(), "cli", "skills"))
	row("额外来源数量", fmt.Sprintf("%d", len(cfg.Skills.Sources)))
	if len(cfg.Skills.Enabled) > 0 {
		row("显式启用", strings.Join(cfg.Skills.Enabled, ", "))
	}
	if len(cfg.Skills.Disabled) > 0 {
		row("显式禁用", strings.Join(cfg.Skills.Disabled, ", "))
	}
	fmt.Println(sectionStyle.Render("▸ 基础设施"))
	fmt.Println(divider)
	row("日志级别", cfg.Log.Level)
	if cfg.Server.Port > 0 {
		row("HTTP 服务（预留）", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port))
	}
	fmt.Println()
}

func defaultConfigHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join("~", ".genesis-agent")
	}
	return filepath.Join(home, ".genesis-agent")
}

func defaultUserConfigPath() string {
	return filepath.Join(defaultConfigHome(), "cli", "config.yaml")
}
