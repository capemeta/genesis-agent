package command

import (
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/parser"
	cliskill "genesis-agent/products/cli/internal/skill"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "浏览、安装和管理 Skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newSkillCardCmd(),
		newSkillCreateCmd(),
		newSkillPackageCmd(),
		newSkillEvalCmd(),
		newSkillListCmd(),
		newSkillSearchCmd(),
		newSkillShowCmd(),
		newSkillInstallCmd(),
		newSkillInstalledCmd(),
		newSkillEnableCmd(true),
		newSkillEnableCmd(false),
		newSkillUninstallCmd(),
		newSkillValidateCmd(),
		newSkillMarketplaceCmd(),
	)
	return cmd
}

func newSkillListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出技能广场中的可安装 Skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			capabilities, err := svc.ListCapabilities(cmd.Context(), capmodel.CapabilityQuery{Types: []capmodel.CapabilityType{capmodel.CapabilityTypeSkill}})
			if err != nil {
				return err
			}
			printSkillCapabilities(capabilities)
			cards, err := svc.Catalog(cmd.Context(), "")
			if err != nil {
				return err
			}
			printCatalog(filterSkillCatalog(cards))
			return nil
		},
	}
}

func newSkillSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "搜索技能广场",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			capabilities, err := svc.ListCapabilities(cmd.Context(), capmodel.CapabilityQuery{Query: args[0], Types: []capmodel.CapabilityType{capmodel.CapabilityTypeSkill}})
			if err != nil {
				return err
			}
			printSkillCapabilities(capabilities)
			cards, err := svc.Catalog(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printCatalog(filterSkillCatalog(cards))
			return nil
		},
	}
}

func newSkillShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <package[@marketplace]>",
		Short: "查看技能包详情",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			cards, err := svc.Catalog(cmd.Context(), "")
			if err != nil {
				return err
			}
			cards = filterSkillCatalog(cards)
			pkg, marketplace, err := marketmodel.SplitPackageSpec(args[0])
			if err != nil {
				return err
			}
			matches := make([]marketmodel.CatalogCard, 0, 1)
			for _, card := range cards {
				if card.Package.Name == pkg && (marketplace == "" || card.Marketplace == marketplace) {
					matches = append(matches, card)
				}
			}
			if len(matches) == 0 {
				return fmt.Errorf("未找到技能包: %s", args[0])
			}
			if len(matches) > 1 {
				choices := make([]string, 0, len(matches))
				for _, match := range matches {
					choices = append(choices, marketmodel.PackageSpec(match.Package.Name, match.Marketplace))
				}
				return fmt.Errorf("技能包名称有歧义，请使用 package@marketplace，候选: %s", strings.Join(choices, ", "))
			}
			printCardDetail(matches[0])
			return nil
		},
	}
}

func newSkillInstallCmd() *cobra.Command {
	var scope string
	var force bool
	cmd := &cobra.Command{
		Use:   "install <package[@marketplace]>",
		Short: "安装技能包",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			record, err := svc.Install(cmd.Context(), args[0], marketmodel.InstallScope(scope), force)
			if err != nil {
				return err
			}
			fmt.Printf("已安装 %s，scope=%s，capabilities=%d\n", record.Spec, record.Scope, len(record.Capabilities))
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", string(marketmodel.InstallScopeUser), "安装范围：user 或 project")
	cmd.Flags().BoolVar(&force, "force", false, "覆盖已安装技能")
	return cmd
}

func newSkillInstalledCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "installed",
		Short: "列出已安装技能包",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			records, err := svc.Installed(cmd.Context())
			if err != nil {
				return err
			}
			for _, record := range records {
				state := "disabled"
				if record.Enabled {
					state = "enabled"
				}
				fmt.Printf("%s\t%s\t%s\t%s\tcapabilities=%d\n", record.Spec, record.PackageType, record.Scope, state, len(record.Capabilities))
			}
			return nil
		},
	}
}

func newSkillEnableCmd(enabled bool) *cobra.Command {
	use := "enable <package@marketplace>"
	short := "启用已安装技能包"
	if !enabled {
		use = "disable <package@marketplace>"
		short = "禁用已安装技能包"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			record, err := svc.SetEnabled(cmd.Context(), args[0], enabled)
			if err != nil {
				return err
			}
			fmt.Printf("%s 已%s\n", record.Spec, map[bool]string{true: "启用", false: "禁用"}[enabled])
			return nil
		},
	}
}

func newSkillUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <package@marketplace>",
		Short: "卸载技能包",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			if err := svc.Uninstall(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("已卸载 %s\n", args[0])
			return nil
		},
	}
}

func newSkillValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <skill-dir>",
		Short: "校验本地 Skill 目录",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillPath, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			info, err := os.Stat(skillPath)
			if err != nil {
				return fmt.Errorf("读取Skill目录失败: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("Skill路径必须是目录: %s", skillPath)
			}
			source := contract.ParseSource{
				DirectoryName: filepath.Base(skillPath),
				DisplayPath:   skillPath,
				BaseDirectory: skillPath,
			}
			result := parser.NewValidator().ValidateSkillFS(os.DirFS(skillPath), source)
			if result.Metadata.Name != "" {
				fmt.Printf("Skill: %s\nDescription: %s\n", result.Metadata.Name, result.Metadata.Description)
			}
			if len(result.Findings) == 0 {
				fmt.Println("校验通过：未发现问题")
				return nil
			}
			for _, finding := range result.Findings {
				if finding.Path == "" {
					fmt.Printf("%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Message)
					continue
				}
				fmt.Printf("%s\t%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Path, finding.Message)
			}
			if result.HasErrors() {
				return fmt.Errorf("Skill校验失败")
			}
			fmt.Println("校验通过：存在warning/info，请按需处理")
			return nil
		},
	}
}
func newSkillMarketplaceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "marketplace", Short: "管理 Skill Marketplace 来源", RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	cmd.AddCommand(
		&cobra.Command{Use: "add <source>", Short: "添加 marketplace", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			record, err := svc.AddMarketplace(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("已添加 marketplace %s\n", record.Name)
			return nil
		}},
		&cobra.Command{Use: "list", Short: "列出 marketplace 来源", RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			records, err := svc.ListMarketplaces(cmd.Context())
			if err != nil {
				return err
			}
			for _, record := range records {
				fmt.Printf("%s\t%s\t%s\n", record.Name, record.Source.Type, record.InstallLocation)
			}
			return nil
		}},
		&cobra.Command{Use: "update <name>", Short: "刷新 marketplace cache", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			record, err := svc.UpdateMarketplace(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("已更新 marketplace %s\n", record.Name)
			return nil
		}},
		&cobra.Command{Use: "remove <name>", Short: "移除 marketplace 来源", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			if err := svc.RemoveMarketplace(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("已移除 marketplace %s\n", args[0])
			return nil
		}},
	)
	return cmd
}

func filterSkillCatalog(cards []marketmodel.CatalogCard) []marketmodel.CatalogCard {
	out := make([]marketmodel.CatalogCard, 0, len(cards))
	for _, card := range cards {
		for _, capability := range card.Package.Capabilities {
			if capability.Type == capmodel.CapabilityTypeSkill {
				out = append(out, card)
				break
			}
		}
	}
	return out
}
func printSkillCapabilities(capabilities []capmodel.CapabilityIndexRecord) {
	for _, capability := range capabilities {
		state := "disabled"
		if capability.Enabled {
			state = "enabled"
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", capability.Name, capability.Spec, state, capability.ResourcePath)
	}
}
func printCatalog(cards []marketmodel.CatalogCard) {
	for _, card := range cards {
		state := "not-installed"
		if card.Installed {
			state = "disabled"
			if card.Enabled {
				state = "enabled"
			}
		}
		if card.Package.Name == "" {
			fmt.Printf("%s\t%s\n", card.Marketplace, state)
		} else {
			fmt.Printf("%s@%s\t%s\t%s\n", card.Package.Name, card.Marketplace, state, card.Package.Description)
		}
		for _, warning := range card.Warnings {
			fmt.Printf("  warning: %s\n", warning)
		}
	}
}

func printCardDetail(card marketmodel.CatalogCard) {
	fmt.Printf("Name: %s\nType: %s\nMarketplace: %s\nVersion: %s\nDescription: %s\n", card.Package.Name, card.Package.Type, card.Marketplace, card.Package.Version, card.Package.Description)
	if len(card.Package.Capabilities) > 0 {
		names := make([]string, 0, len(card.Package.Capabilities))
		for _, capability := range card.Package.Capabilities {
			names = append(names, string(capability.Type)+":"+capability.Name)
		}
		fmt.Printf("Capabilities: %s\n", strings.Join(names, ", "))
	}
	if len(card.Package.Commands) > 0 {
		fmt.Printf("Commands: %s\n", strings.Join(card.Package.Commands, ", "))
	}
	fmt.Printf("Installed: %v\nEnabled: %v\n", card.Installed, card.Enabled)
}
