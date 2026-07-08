// Package bootstrap 装配 Genesis Desktop 产品入口。
package bootstrap

import (
	"context"
	"fmt"

	platformconfig "genesis-agent/internal/platform/config"
)

// Execute 是 Desktop 产品薄入口的占位执行函数。
func Execute(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := platformconfig.LoadWithOptions("configs", platformconfig.LoadOptions{Product: "desktop", EnsureUserConfig: true}); err != nil {
		return err
	}
	return fmt.Errorf("genesis-desktop 暂未实现")
}
