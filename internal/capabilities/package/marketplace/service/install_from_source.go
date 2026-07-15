package service

import (
	"context"
	"fmt"
	"strings"

	marketcontract "genesis-agent/internal/capabilities/package/marketplace/contract"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

// InstallFromSource 从 URL / github / dir 或 package@marketplace 安装 skill-package。
func (s *Service) InstallFromSource(ctx context.Context, req marketcontract.InstallFromSourceRequest) marketcontract.InstallFromSourceResult {
	input := strings.TrimSpace(req.SourceInput)
	if input == "" {
		return failResult("validation_failed", "source不能为空")
	}
	if req.Scope == "" {
		req.Scope = marketmodel.InstallScopeUser
	}
	if req.Product == "" {
		req.Product = "cli"
	}

	// package@marketplace 走既有 Install
	if isPackageSpecInput(input) {
		record, err := s.Install(ctx, input, req.Scope, req.Force)
		if err != nil {
			return failResult("install_failed", err.Error())
		}
		return successFromRecords([]marketmodel.InstallRecord{record}, s.reloadEffective(ctx))
	}

	source, err := s.parser.Parse(input)
	if err != nil {
		return failResult("validation_failed", err.Error())
	}
	if err := s.checkSourcePolicy(ctx, source, req); err != nil {
		return failResult("policy_denied", err.Error())
	}

	fetched, err := s.fetcher.Fetch(ctx, marketcontract.FetchRequest{Source: source, Refresh: true})
	if err != nil {
		return failResult("fetch_failed", err.Error())
	}

	detected := detectInstallTargets(fetched.InstallLocation, fetched.Manifest, source, req.Package, req.SkillPath, req.SkillPaths)
	if detected.FailureKind != "" || detected.NeedsChoice {
		return marketcontract.InstallFromSourceResult{
			NeedsChoice: detected.NeedsChoice,
			Candidates:  detected.Candidates,
			Message:     detected.Message,
			FailureKind: firstNonEmpty(detected.FailureKind, "needs_choice"),
		}
	}

	manifest := detected.Manifest
	if strings.TrimSpace(manifest.Name) == "" {
		manifest.Name = syntheticMarketplaceName(source)
	}
	// 合并 detect 选出的 packages 到 manifest
	manifest.Packages = detected.Packages

	record := marketmodel.MarketplaceRecord{
		Name:            manifest.Name,
		Source:          source,
		InstallLocation: fetched.InstallLocation,
		LastUpdated:     s.now().UTC(),
		LastRevision:    firstNonEmpty(fetched.LastRevision, fetched.ContentHash),
	}
	if err := s.registry.Put(ctx, record); err != nil {
		return failResult("install_failed", err.Error())
	}

	var installed []marketmodel.InstallRecord
	for _, pkg := range detected.Packages {
		one, err := s.installResolvedPackage(ctx, record, manifest, pkg, req.Scope, req.Force, fetched.ContentHash, fetched.LastRevision)
		if err != nil {
			return failResult("install_failed", err.Error())
		}
		installed = append(installed, one)
	}
	return successFromRecords(installed, s.reloadEffective(ctx))
}

func (s *Service) installResolvedPackage(ctx context.Context, marketplace marketmodel.MarketplaceRecord, manifest marketmodel.Manifest, pkg marketmodel.Package, scope marketmodel.InstallScope, force bool, contentHash, lastRevision string) (marketmodel.InstallRecord, error) {
	if scope != marketmodel.InstallScopeUser && scope != marketmodel.InstallScopeProject {
		return marketmodel.InstallRecord{}, fmt.Errorf("不支持的install scope: %s", scope)
	}
	resolvedSpec := marketmodel.PackageSpec(pkg.Name, marketplace.Name)
	if _, ok, err := s.installs.Get(ctx, resolvedSpec); err != nil {
		return marketmodel.InstallRecord{}, err
	} else if ok && !force {
		return marketmodel.InstallRecord{}, fmt.Errorf("package %q 已安装，使用--force重新安装", resolvedSpec)
	}
	install, err := s.installer.Install(ctx, marketcontract.InstallRequest{
		Marketplace: marketplace,
		Manifest:    manifest,
		Package:     pkg,
		Scope:       scope,
		Force:       force,
		Enabled:     true,
	})
	if err != nil {
		return marketmodel.InstallRecord{}, err
	}
	install.Spec = resolvedSpec
	install.Package = pkg.Name
	install.PackageType = pkg.Type
	if install.PackageType == "" {
		install.PackageType = marketmodel.PackageTypeSkillPackage
	}
	install.Marketplace = marketplace.Name
	install.Version = pkg.Version
	install.ContentHash = firstNonEmpty(contentHash, install.ContentHash)
	provenance := marketmodel.NewSourceProvenance(marketplace, pkg, firstNonEmpty(lastRevision, marketplace.LastRevision), install.ContentHash)
	install.SourceProvenance = &provenance
	if install.SourceMarketplacePath == "" {
		install.SourceMarketplacePath = provenance.Address
	}
	if install.InstalledAt.IsZero() {
		install.InstalledAt = s.now().UTC()
	}
	install.UpdatedAt = s.now().UTC()
	install.Capabilities = s.normalizeCapabilityIndex(install)
	if err := s.installs.Put(ctx, install); err != nil {
		_ = s.installer.Uninstall(ctx, install)
		return marketmodel.InstallRecord{}, err
	}
	if err := s.capabilities.PutPackageCapabilities(ctx, install.Spec, install.Capabilities); err != nil {
		_, _, _ = s.installs.Delete(ctx, install.Spec)
		_ = s.installer.Uninstall(ctx, install)
		return marketmodel.InstallRecord{}, err
	}
	if err := s.notifyAdapters(ctx, install.Capabilities, true); err != nil {
		_ = s.capabilities.DeletePackage(ctx, install.Spec)
		_, _, _ = s.installs.Delete(ctx, install.Spec)
		_ = s.installer.Uninstall(ctx, install)
		return marketmodel.InstallRecord{}, err
	}
	return install, nil
}

func (s *Service) checkSourcePolicy(ctx context.Context, source marketmodel.MarketplaceSource, req marketcontract.InstallFromSourceRequest) error {
	// CLI --allow-url：显式放行非 GitHub 远程 URL（仍须对话 Approval 覆盖对话路径）。
	if req.AllowURL && (source.Type == marketmodel.SourceTypeURL || source.Type == marketmodel.SourceTypeGit) {
		return nil
	}
	if s.policy != nil {
		return s.policy.Check(ctx, source, req.Product)
	}
	if source.Type == marketmodel.SourceTypeURL {
		return fmt.Errorf("拒绝任意公网 URL 安装；请使用 GitHub 来源或 --allow-url")
	}
	return nil
}

func (s *Service) reloadEffective(ctx context.Context) string {
	if s.reloader == nil {
		return "next_turn"
	}
	if err := s.reloader.Reload(ctx); err != nil {
		return "next_turn"
	}
	return "hot"
}

func isPackageSpecInput(input string) bool {
	if strings.Contains(input, "://") {
		return false
	}
	for _, prefix := range []string{"github:", "git:", "url:", "file:", "dir:"} {
		if strings.HasPrefix(input, prefix) {
			return false
		}
	}
	_, marketplace, err := marketmodel.SplitPackageSpec(input)
	return err == nil && marketplace != ""
}

func failResult(kind, message string) marketcontract.InstallFromSourceResult {
	return marketcontract.InstallFromSourceResult{FailureKind: kind, Message: message}
}

func successFromRecords(records []marketmodel.InstallRecord, effective string) marketcontract.InstallFromSourceResult {
	skills := make([]string, 0)
	specs := make([]string, 0, len(records))
	seen := map[string]struct{}{}
	for _, record := range records {
		specs = append(specs, record.Spec)
		for _, skill := range record.Skills {
			if _, ok := seen[skill]; ok {
				continue
			}
			seen[skill] = struct{}{}
			skills = append(skills, skill)
		}
	}
	return marketcontract.InstallFromSourceResult{
		Records:   records,
		Skills:    skills,
		Specs:     specs,
		Effective: effective,
		Message:   fmt.Sprintf("已安装 %d 个包", len(records)),
	}
}
