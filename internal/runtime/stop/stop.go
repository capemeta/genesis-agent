package stop

import (
	"context"
	"genesis-agent/internal/runtime"
)

// Condition 终止条件判断接口，决定 Loop 是否结束
type Condition interface {
	ShouldStop(ctx context.Context, rc *runtime.RunContext) (bool, error)
}
