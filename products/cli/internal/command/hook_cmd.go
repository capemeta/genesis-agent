package command

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	hookmodel "genesis-agent/internal/capabilities/hook/model"
	hookservice "genesis-agent/internal/capabilities/hook/service"
	"genesis-agent/internal/platform/config"
)

func newHookCmd(configDirRef *string) *cobra.Command {
	cmd := &cobra.Command{Use: "hook", Short: "查看 Hook 的有效配置"}
	cmd.AddCommand(newHookListCmd(configDirRef))
	return cmd
}

func newHookListCmd(configDirRef *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出 CLI 环境当前有效的 Hook",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadHookConfig(*configDirRef, "cli")
			if err != nil {
				return fmt.Errorf("加载 Hook 配置失败: %w", err)
			}
			items := hookservice.ListEffectiveHandlers(cfg, hookmodel.ScopeContext{Channel: "cli", Environment: "local"})
			if asJSON {
				data, err := json.MarshalIndent(items, "", "  ")
				if err != nil {
					return fmt.Errorf("编码 Hook 列表失败: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}
			if len(items) == 0 {
				fmt.Println("当前 CLI 环境没有有效 Hook")
				return nil
			}
			for _, item := range items {
				trust := "trusted"
				if !item.Trusted {
					trust = "untrusted"
				}
				fmt.Printf("%s\t%s\t%s\t%s\t%s\n", item.Event, item.Matcher, item.Name, strings.ToLower(item.Type), trust)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 输出，适合脚本调用")
	return cmd
}
