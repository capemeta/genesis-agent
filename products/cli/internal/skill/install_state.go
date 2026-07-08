package skill

import (
	"context"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	localskill "genesis-agent/shared/local/skill"
	"genesis-agent/shared/local/skillmarket"
)

func InstalledSkillRoots(ctx context.Context) ([]localskill.Root, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	records, err := skillmarket.NewInstallStore(paths.InstallFile).List(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	roots := make([]localskill.Root, 0, len(records))
	for _, record := range records {
		if !record.Enabled {
			continue
		}
		if record.Scope == marketmodel.InstallScopeProject && record.ProjectPath != "" && record.ProjectPath != paths.Workspace {
			continue
		}
		for _, root := range record.SkillRoots {
			if root == "" {
				continue
			}
			if _, ok := seen[root]; ok {
				continue
			}
			seen[root] = struct{}{}
			roots = append(roots, localskill.Root{Path: root, Scope: skillmodel.ScopePlugin})
		}
	}
	return roots, nil
}
