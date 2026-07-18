package artifact

import (
	"context"
	"fmt"
	"strings"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

// PolicyTargetPlanner maps trusted DeliveryPolicy identifiers to pre-authorized targets.
type PolicyTargetPlanner struct {
	targets map[string]artifactmodel.DeliveryTarget
}

func NewPolicyTargetPlanner(targets map[string]artifactmodel.DeliveryTarget) (*PolicyTargetPlanner, error) {
	cloned := make(map[string]artifactmodel.DeliveryTarget, len(targets))
	for policy, target := range targets {
		policy = strings.TrimSpace(policy)
		if policy == "" || strings.TrimSpace(target.Name) == "" || strings.TrimSpace(target.Resource.ID) == "" {
			return nil, fmt.Errorf("delivery policy target 无效")
		}
		if _, exists := cloned[policy]; exists {
			return nil, fmt.Errorf("delivery policy %s 重复", policy)
		}
		cloned[policy] = target
	}
	return &PolicyTargetPlanner{targets: cloned}, nil
}
func (p *PolicyTargetPlanner) PlanDelivery(ctx context.Context, spec artifactmodel.DeliverableSpec, artifact artifactmodel.ArtifactRef) (artifactmodel.DeliveryTarget, error) {
	if err := ctx.Err(); err != nil {
		return artifactmodel.DeliveryTarget{}, err
	}
	target, ok := p.targets[spec.DeliveryPolicy]
	if !ok {
		return artifactmodel.DeliveryTarget{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryTargetDenied, fmt.Errorf("delivery policy %q 未配置", spec.DeliveryPolicy))
	}
	if target.Name == "$artifact_name" {
		target.Name = artifact.Name
	}
	return target, nil
}

var _ artifactcontract.DeliveryTargetPlanner = (*PolicyTargetPlanner)(nil)
