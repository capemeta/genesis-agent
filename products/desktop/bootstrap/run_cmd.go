package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"genesis-agent/internal/app"
	"genesis-agent/products/desktop/internal/attach"

	"github.com/spf13/cobra"
)

func addRunCommand(root *cobra.Command, parent context.Context) {
	var configDir string
	var attachPaths []string
	cmd := &cobra.Command{
		Use:   "run [prompt...]",
		Short: "最小无 UI Run：--attach 选文件后进入 Attachments（Wails 待接入）",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewContainer(configDir, false)
			if err := c.Init(parent); err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			svc := c.Service()
			if svc == nil {
				return fmt.Errorf("AgentService 未装配")
			}
			input := strings.TrimSpace(strings.Join(args, " "))
			if input == "" {
				input = "请分析附件"
			}
			atts, err := attach.FromPaths(attachPaths)
			if err != nil {
				return err
			}
			session, err := svc.CreateSession(parent, app.SessionScope{
				TenantID: "dev", UserID: "desktop-user", AppID: "desktop-default",
			})
			if err != nil {
				return err
			}
			res, err := svc.RunOnce(parent, app.RunRequest{
				SessionID: session.ID, AppID: "desktop-default", TenantID: "dev",
				UserID: "desktop-user", Input: input, Attachments: atts,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "status=%s answer=%s\n", res.Run.Status, res.Run.FinalAnswer)
			return nil
		},
	}
	cmd.Flags().StringVarP(&configDir, "config", "c", "configs", "配置目录路径")
	cmd.Flags().StringArrayVar(&attachPaths, "attach", nil, "本地附件路径（可重复）")
	root.AddCommand(cmd)
}
