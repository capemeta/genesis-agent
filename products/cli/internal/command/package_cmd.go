package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	cliskill "genesis-agent/products/cli/internal/skill"
)

func newPackageCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "package", Short: "安装和管理 Package", RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	cmd.AddCommand(
		newPackageListCmd(),
		newPackageSearchCmd(),
		newPackageShowCmd(),
		newPackageInstallCmd(),
		newPackageInstalledCmd(),
		newPackageEnableCmd(true),
		newPackageEnableCmd(false),
		newPackageUninstallCmd(),
	)
	return cmd
}

func newPackageListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "列出 marketplace 中的可安装 Package", RunE: func(cmd *cobra.Command, args []string) error {
		svc, _, err := cliskill.NewMarketplaceService()
		if err != nil {
			return err
		}
		cards, err := svc.Catalog(cmd.Context(), "")
		if err != nil {
			return err
		}
		printCatalog(cards)
		return nil
	}}
}

func newPackageSearchCmd() *cobra.Command {
	return &cobra.Command{Use: "search <query>", Short: "搜索可安装 Package", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, _, err := cliskill.NewMarketplaceService()
		if err != nil {
			return err
		}
		cards, err := svc.Catalog(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		printCatalog(cards)
		return nil
	}}
}

func newPackageShowCmd() *cobra.Command {
	return &cobra.Command{Use: "show <package[@marketplace]>", Short: "查看 Package 详情", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, _, err := cliskill.NewMarketplaceService()
		if err != nil {
			return err
		}
		cards, err := svc.Catalog(cmd.Context(), "")
		if err != nil {
			return err
		}
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
			return fmt.Errorf("未找到package: %s", args[0])
		}
		if len(matches) > 1 {
			choices := make([]string, 0, len(matches))
			for _, match := range matches {
				choices = append(choices, marketmodel.PackageSpec(match.Package.Name, match.Marketplace))
			}
			return fmt.Errorf("package名称有歧义，请使用 package@marketplace，候选: %s", strings.Join(choices, ", "))
		}
		printCardDetail(matches[0])
		return nil
	}}
}

func newPackageInstallCmd() *cobra.Command {
	var scope string
	var force bool
	cmd := &cobra.Command{Use: "install <package[@marketplace]>", Short: "安装 Package", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, _, err := cliskill.NewMarketplaceService()
		if err != nil {
			return err
		}
		record, err := svc.InstallPackage(cmd.Context(), args[0], marketmodel.InstallScope(scope), force)
		if err != nil {
			return err
		}
		fmt.Printf("已安装 package %s，type=%s，capabilities=%d\n", record.Spec, record.PackageType, len(record.Capabilities))
		return nil
	}}
	cmd.Flags().StringVar(&scope, "scope", string(marketmodel.InstallScopeUser), "安装范围：user 或 project")
	cmd.Flags().BoolVar(&force, "force", false, "覆盖已安装 Package")
	return cmd
}

func newPackageInstalledCmd() *cobra.Command {
	return &cobra.Command{Use: "installed", Short: "列出已安装 Package", RunE: func(cmd *cobra.Command, args []string) error {
		svc, _, err := cliskill.NewMarketplaceService()
		if err != nil {
			return err
		}
		views, err := svc.ListPackages(cmd.Context())
		if err != nil {
			return err
		}
		for _, view := range views {
			state := "disabled"
			if view.Install.Enabled {
				state = "enabled"
			}
			fmt.Printf("%s\t%s\t%s\t%s\tcapabilities=%d\n", view.Install.Spec, view.Install.PackageType, view.Install.Scope, state, len(view.Capabilities))
		}
		return nil
	}}
}

func newPackageEnableCmd(enabled bool) *cobra.Command {
	use := "enable <package@marketplace>"
	short := "启用已安装 Package"
	if !enabled {
		use = "disable <package@marketplace>"
		short = "禁用已安装 Package"
	}
	return &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, _, err := cliskill.NewMarketplaceService()
		if err != nil {
			return err
		}
		record, err := svc.SetEnabled(cmd.Context(), args[0], enabled)
		if err != nil {
			return err
		}
		fmt.Printf("package %s 已%s\n", record.Spec, map[bool]string{true: "启用", false: "禁用"}[enabled])
		return nil
	}}
}

func newPackageUninstallCmd() *cobra.Command {
	return &cobra.Command{Use: "uninstall <package@marketplace>", Short: "卸载 Package", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, _, err := cliskill.NewMarketplaceService()
		if err != nil {
			return err
		}
		if err := svc.Uninstall(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Printf("已卸载 package %s\n", args[0])
		return nil
	}}
}
