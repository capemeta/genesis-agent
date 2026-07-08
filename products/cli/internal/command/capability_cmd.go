package command

import (
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"strings"

	cliskill "genesis-agent/products/cli/internal/skill"
	"github.com/spf13/cobra"
)

func newCapabilityCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "capability", Short: "查看运行时 Capability 索引", RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	cmd.AddCommand(newCapabilityListCmd(), newCapabilitySearchCmd(), newCapabilityEnableCmd(true), newCapabilityEnableCmd(false))
	return cmd
}

func newCapabilityListCmd() *cobra.Command {
	var typ string
	var includeDisabled bool
	cmd := &cobra.Command{Use: "list", Short: "列出当前 CapabilityIndex", RunE: func(cmd *cobra.Command, args []string) error {
		query, err := capabilityQuery("", typ, includeDisabled)
		if err != nil {
			return err
		}
		return printCapabilities(cmd, query)
	}}
	cmd.Flags().StringVar(&typ, "type", "", "按 capability 类型过滤：skill、skill-resource、tool、mcp、subagent、resource")
	cmd.Flags().BoolVar(&includeDisabled, "all", false, "包含 disabled capability")
	return cmd
}

func newCapabilitySearchCmd() *cobra.Command {
	var typ string
	var includeDisabled bool
	cmd := &cobra.Command{Use: "search <query>", Short: "搜索当前 CapabilityIndex", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		query, err := capabilityQuery(args[0], typ, includeDisabled)
		if err != nil {
			return err
		}
		return printCapabilities(cmd, query)
	}}
	cmd.Flags().StringVar(&typ, "type", "", "按 capability 类型过滤：skill、skill-resource、tool、mcp、subagent、resource")
	cmd.Flags().BoolVar(&includeDisabled, "all", false, "包含 disabled capability")
	return cmd
}

func newCapabilityEnableCmd(enabled bool) *cobra.Command {
	use := "enable <capability-id>"
	short := "启用单个 Capability"
	if !enabled {
		use = "disable <capability-id>"
		short = "禁用单个 Capability"
	}
	return &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, _, err := cliskill.NewMarketplaceService()
		if err != nil {
			return err
		}
		record, err := svc.SetCapabilityEnabled(cmd.Context(), args[0], enabled)
		if err != nil {
			return err
		}
		fmt.Printf("capability %s 已%s\n", record.ID, map[bool]string{true: "启用", false: "禁用"}[enabled])
		return nil
	}}
}
func capabilityQuery(text, typ string, includeDisabled bool) (capmodel.CapabilityQuery, error) {
	query := capmodel.CapabilityQuery{Query: text, IncludeDisabled: includeDisabled}
	if strings.TrimSpace(typ) == "" {
		return query, nil
	}
	capabilityType := capmodel.CapabilityType(strings.TrimSpace(typ))
	switch capabilityType {
	case capmodel.CapabilityTypeSkill, capmodel.CapabilityTypeSkillResource, capmodel.CapabilityTypeTool, capmodel.CapabilityTypeMCP, capmodel.CapabilityTypeSubAgent, capmodel.CapabilityTypeResource:
		query.Types = []capmodel.CapabilityType{capabilityType}
		return query, nil
	default:
		return capmodel.CapabilityQuery{}, fmt.Errorf("不支持的capability type: %s", typ)
	}
}

func printCapabilities(cmd *cobra.Command, query capmodel.CapabilityQuery) error {
	svc, _, err := cliskill.NewMarketplaceService()
	if err != nil {
		return err
	}
	capabilities, err := svc.ListCapabilities(cmd.Context(), query)
	if err != nil {
		return err
	}
	for _, capability := range capabilities {
		state := "disabled"
		if capability.Enabled {
			state = "enabled"
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", capability.Name, capability.Type, capability.Spec, state, capability.ResourcePath)
	}
	return nil
}
