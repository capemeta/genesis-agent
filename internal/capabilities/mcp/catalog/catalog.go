package catalog

import (
	"context"
	"fmt"
	"sort"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

// Catalog 多来源合并 + 优先级 + 冲突消解（对齐 Codex catalog）。
type Catalog struct {
	Sources    []contract.DefinitionSource
	Filter     contract.RequirementsFilter
	OnConflict func(name string, winner, loser model.McpServerDefinition)
}

// New 创建 Catalog。
func New(sources []contract.DefinitionSource, filter contract.RequirementsFilter) *Catalog {
	return &Catalog{Sources: sources, Filter: filter}
}

// Merge 合并所有来源；后者（更高 Precedence）覆盖同名。
func (c *Catalog) Merge(ctx context.Context, env contract.RuntimeCatalogEnv) ([]model.McpServerDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type contender struct {
		def model.McpServerDefinition
	}
	byName := make(map[string][]contender)

	sources := append([]contract.DefinitionSource(nil), c.Sources...)
	sort.SliceStable(sources, func(i, j int) bool {
		return sources[i].Precedence() < sources[j].Precedence()
	})

	for _, src := range sources {
		if src == nil {
			continue
		}
		defs, err := src.List(ctx, env)
		if err != nil {
			return nil, fmt.Errorf("mcp catalog source precedence=%d: %w", src.Precedence(), err)
		}
		for _, def := range defs {
			name := def.Config.Name
			if name == "" {
				continue
			}
			def.Precedence = src.Precedence()
			byName[name] = append(byName[name], contender{def: def})
		}
	}

	out := make([]model.McpServerDefinition, 0, len(byName))
	for name, list := range byName {
		if len(list) == 0 {
			continue
		}
		winner := list[len(list)-1].def
		if len(list) > 1 {
			origins := make([]model.DefinitionOrigin, 0, len(list)-1)
			for _, ctd := range list[:len(list)-1] {
				origins = append(origins, ctd.def.Origin)
				if c.OnConflict != nil {
					c.OnConflict(name, winner, ctd.def)
				}
			}
			winner.OverriddenOrigins = origins
		}
		if winner.ConfigKey == "" {
			winner.ConfigKey = ComputeConfigKey(winner)
		}
		out = append(out, winner)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Config.Name < out[j].Config.Name })

	if c.Filter != nil {
		filtered, err := c.Filter.Filter(ctx, out)
		if err != nil {
			return nil, err
		}
		out = filtered
	}
	return out, nil
}

var _ contract.Catalog = (*Catalog)(nil)
