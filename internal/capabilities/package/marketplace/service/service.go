// Package service 提供 Package Marketplace 的产品无关编排。
package service

import (
	"context"
	"fmt"
	capcontract "genesis-agent/internal/capabilities/capability/contract"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"sort"
	"strings"
	"time"

	capservice "genesis-agent/internal/capabilities/capability/service"
	marketcontract "genesis-agent/internal/capabilities/package/marketplace/contract"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

// Service 编排 marketplace 来源、广场 catalog、安装状态与能力索引。
type Service struct {
	registry     marketcontract.RegistryStore
	installs     marketcontract.InstallStore
	capabilities marketcontract.CapabilityIndexStore
	parser       marketcontract.SourceParser
	fetcher      marketcontract.Fetcher
	installer    marketcontract.Installer
	capRegistry  *capservice.Registry
	now          func() time.Time
}

type Options struct {
	Registry     marketcontract.RegistryStore
	Installs     marketcontract.InstallStore
	Capabilities marketcontract.CapabilityIndexStore
	Parser       marketcontract.SourceParser
	Fetcher      marketcontract.Fetcher
	Installer    marketcontract.Installer
	Adapters     capcontract.RuntimeAdapterRegistry
	Now          func() time.Time
}

func New(opts Options) (*Service, error) {
	if opts.Registry == nil {
		return nil, fmt.Errorf("marketplace registry不能为空")
	}
	if opts.Installs == nil {
		return nil, fmt.Errorf("install store不能为空")
	}
	if opts.Capabilities == nil {
		return nil, fmt.Errorf("capability index store不能为空")
	}
	if opts.Parser == nil {
		return nil, fmt.Errorf("source parser不能为空")
	}
	if opts.Fetcher == nil {
		return nil, fmt.Errorf("fetcher不能为空")
	}
	if opts.Installer == nil {
		return nil, fmt.Errorf("installer不能为空")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	capRegistry, err := capservice.NewRegistry(capservice.Options{Store: opts.Capabilities, Adapters: opts.Adapters})
	if err != nil {
		return nil, err
	}
	return &Service{registry: opts.Registry, installs: opts.Installs, capabilities: opts.Capabilities, capRegistry: capRegistry, parser: opts.Parser, fetcher: opts.Fetcher, installer: opts.Installer, now: opts.Now}, nil
}

func (s *Service) AddMarketplace(ctx context.Context, input string) (marketmodel.MarketplaceRecord, error) {
	source, err := s.parser.Parse(input)
	if err != nil {
		return marketmodel.MarketplaceRecord{}, err
	}
	fetched, err := s.fetcher.Fetch(ctx, marketcontract.FetchRequest{Source: source, Refresh: true})
	if err != nil {
		return marketmodel.MarketplaceRecord{}, err
	}
	name := strings.TrimSpace(fetched.Manifest.Name)
	if name == "" {
		return marketmodel.MarketplaceRecord{}, fmt.Errorf("marketplace manifest缺少name")
	}
	if existing, ok, err := s.registry.Get(ctx, name); err != nil {
		return marketmodel.MarketplaceRecord{}, err
	} else if ok {
		return marketmodel.MarketplaceRecord{}, fmt.Errorf("marketplace %q 已存在: %s", name, existing.InstallLocation)
	}
	record := marketmodel.MarketplaceRecord{
		Name:            name,
		Source:          source,
		InstallLocation: fetched.InstallLocation,
		LastUpdated:     s.now().UTC(),
		LastRevision:    firstNonEmpty(fetched.LastRevision, fetched.ContentHash),
	}
	if err := s.registry.Put(ctx, record); err != nil {
		_ = s.fetcher.RemoveCache(ctx, record)
		return marketmodel.MarketplaceRecord{}, err
	}
	return record, nil
}

func (s *Service) ListMarketplaces(ctx context.Context) ([]marketmodel.MarketplaceRecord, error) {
	records, err := s.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return records, nil
}

func (s *Service) UpdateMarketplace(ctx context.Context, name string) (marketmodel.MarketplaceRecord, error) {
	record, ok, err := s.registry.Get(ctx, strings.TrimSpace(name))
	if err != nil {
		return marketmodel.MarketplaceRecord{}, err
	}
	if !ok {
		return marketmodel.MarketplaceRecord{}, fmt.Errorf("marketplace %q 不存在", name)
	}
	fetched, err := s.fetcher.Fetch(ctx, marketcontract.FetchRequest{Source: record.Source, Existing: &record, Refresh: true})
	if err != nil {
		return marketmodel.MarketplaceRecord{}, err
	}
	if fetched.Manifest.Name != record.Name {
		return marketmodel.MarketplaceRecord{}, fmt.Errorf("marketplace名称不匹配: 期望%s，实际%s", record.Name, fetched.Manifest.Name)
	}
	record.InstallLocation = fetched.InstallLocation
	record.LastUpdated = s.now().UTC()
	record.LastRevision = firstNonEmpty(fetched.LastRevision, fetched.ContentHash)
	if err := s.registry.Put(ctx, record); err != nil {
		return marketmodel.MarketplaceRecord{}, err
	}
	return record, nil
}

func (s *Service) RemoveMarketplace(ctx context.Context, name string) error {
	record, ok, err := s.registry.Delete(ctx, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("marketplace %q 不存在", name)
	}
	return s.fetcher.RemoveCache(ctx, record)
}

func (s *Service) Catalog(ctx context.Context, query string) ([]marketmodel.CatalogCard, error) {
	records, err := s.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	installs, err := s.installs.List(ctx)
	if err != nil {
		return nil, err
	}
	installed := map[string]marketmodel.InstallRecord{}
	for _, record := range installs {
		installed[record.Spec] = record
	}
	query = strings.ToLower(strings.TrimSpace(query))
	cards := make([]marketmodel.CatalogCard, 0)
	for _, record := range records {
		fetched, err := s.fetcher.Fetch(ctx, marketcontract.FetchRequest{Source: record.Source, Existing: &record})
		if err != nil {
			cards = append(cards, marketmodel.CatalogCard{Marketplace: record.Name, Availability: "load_error", Warnings: []string{err.Error()}})
			continue
		}
		for _, pkg := range fetched.Manifest.Packages {
			text := strings.ToLower(pkg.Name + " " + pkg.Description + " " + string(pkg.Type) + " " + record.Name)
			if query != "" && !strings.Contains(text, query) {
				continue
			}
			spec := marketmodel.PackageSpec(pkg.Name, record.Name)
			card := marketmodel.CatalogCard{Package: pkg, Marketplace: record.Name, Availability: "available"}
			if install, ok := installed[spec]; ok {
				card.Installed = true
				card.Enabled = install.Enabled
				card.InstallScope = install.Scope
			}
			cards = append(cards, card)
		}
	}
	sort.SliceStable(cards, func(i, j int) bool {
		left := cards[i].Marketplace + "/" + cards[i].Package.Name
		right := cards[j].Marketplace + "/" + cards[j].Package.Name
		return left < right
	})
	return cards, nil
}

func (s *Service) Install(ctx context.Context, spec string, scope marketmodel.InstallScope, force bool) (marketmodel.InstallRecord, error) {
	return s.install(ctx, spec, scope, force, marketmodel.PackageTypeSkillPackage)
}

func (s *Service) InstallPackage(ctx context.Context, spec string, scope marketmodel.InstallScope, force bool) (marketmodel.InstallRecord, error) {
	return s.install(ctx, spec, scope, force, "")
}

func (s *Service) InstallPlugin(ctx context.Context, spec string, scope marketmodel.InstallScope, force bool) (marketmodel.InstallRecord, error) {
	return s.install(ctx, spec, scope, force, marketmodel.PackageTypePlugin)
}

func (s *Service) install(ctx context.Context, spec string, scope marketmodel.InstallScope, force bool, requiredType marketmodel.PackageType) (marketmodel.InstallRecord, error) {
	pkgName, marketplace, err := s.resolvePackageSpec(ctx, spec)
	if err != nil {
		return marketmodel.InstallRecord{}, err
	}
	if scope == "" {
		scope = marketmodel.InstallScopeUser
	}
	if scope != marketmodel.InstallScopeUser && scope != marketmodel.InstallScopeProject {
		return marketmodel.InstallRecord{}, fmt.Errorf("不支持的install scope: %s", scope)
	}
	resolvedSpec := marketmodel.PackageSpec(pkgName, marketplace)
	if _, ok, err := s.installs.Get(ctx, resolvedSpec); err != nil {
		return marketmodel.InstallRecord{}, err
	} else if ok && !force {
		return marketmodel.InstallRecord{}, fmt.Errorf("package %q 已安装，使用--force重新安装", resolvedSpec)
	}
	record, ok, err := s.registry.Get(ctx, marketplace)
	if err != nil {
		return marketmodel.InstallRecord{}, err
	}
	if !ok {
		return marketmodel.InstallRecord{}, fmt.Errorf("marketplace %q 不存在", marketplace)
	}
	fetched, err := s.fetcher.Fetch(ctx, marketcontract.FetchRequest{Source: record.Source, Existing: &record})
	if err != nil {
		return marketmodel.InstallRecord{}, err
	}
	pkg, ok := findPackage(fetched.Manifest, pkgName)
	if !ok {
		return marketmodel.InstallRecord{}, fmt.Errorf("package %q 不存在于marketplace %q", pkgName, marketplace)
	}
	if requiredType != "" && pkg.Type != requiredType {
		return marketmodel.InstallRecord{}, fmt.Errorf("package %q 类型是%s，不是%s", resolvedSpec, pkg.Type, requiredType)
	}
	install, err := s.installer.Install(ctx, marketcontract.InstallRequest{Marketplace: record, Manifest: fetched.Manifest, Package: pkg, Scope: scope, Force: force, Enabled: true})
	if err != nil {
		return marketmodel.InstallRecord{}, err
	}
	install.Spec = resolvedSpec
	install.Package = pkgName
	install.PackageType = pkg.Type
	install.Marketplace = marketplace
	install.Version = pkg.Version
	install.ContentHash = firstNonEmpty(fetched.ContentHash, install.ContentHash)
	provenance := marketmodel.NewSourceProvenance(record, pkg, firstNonEmpty(fetched.LastRevision, record.LastRevision), install.ContentHash)
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
	return install, nil
}

func (s *Service) Installed(ctx context.Context) ([]marketmodel.InstallRecord, error) {
	records, err := s.installs.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].Spec < records[j].Spec })
	return records, nil
}

func (s *Service) Capabilities(ctx context.Context) ([]capmodel.CapabilityIndexRecord, error) {
	return s.ListCapabilities(ctx, capmodel.CapabilityQuery{})
}

func (s *Service) ListCapabilities(ctx context.Context, query capmodel.CapabilityQuery) ([]capmodel.CapabilityIndexRecord, error) {
	return s.capRegistry.ListCapabilities(ctx, query)
}

func (s *Service) ListPackages(ctx context.Context) ([]marketmodel.PackageView, error) {
	installs, err := s.Installed(ctx)
	if err != nil {
		return nil, err
	}
	capabilities, err := s.capabilities.List(ctx)
	if err != nil {
		return nil, err
	}
	bySpec := capabilitiesBySpec(capabilities)
	views := make([]marketmodel.PackageView, 0, len(installs))
	for _, install := range installs {
		views = append(views, marketmodel.PackageView{Install: install, Capabilities: bySpec[install.Spec]})
	}
	return views, nil
}

func (s *Service) Package(ctx context.Context, spec string) (marketmodel.PackageView, error) {
	record, ok, err := s.installs.Get(ctx, strings.TrimSpace(spec))
	if err != nil {
		return marketmodel.PackageView{}, err
	}
	if !ok {
		return marketmodel.PackageView{}, fmt.Errorf("package %q 未安装", spec)
	}
	capabilities, err := s.capabilities.List(ctx)
	if err != nil {
		return marketmodel.PackageView{}, err
	}
	return marketmodel.PackageView{Install: record, Capabilities: capabilitiesBySpec(capabilities)[record.Spec]}, nil
}
func (s *Service) ListPlugins(ctx context.Context) ([]marketmodel.PluginView, error) {
	installs, err := s.installedByType(ctx, marketmodel.PackageTypePlugin)
	if err != nil {
		return nil, err
	}
	capabilities, err := s.capabilities.List(ctx)
	if err != nil {
		return nil, err
	}
	bySpec := capabilitiesBySpec(capabilities)
	views := make([]marketmodel.PluginView, 0, len(installs))
	for _, install := range installs {
		views = append(views, marketmodel.PluginView{Install: install, Capabilities: bySpec[install.Spec]})
	}
	return views, nil
}

func (s *Service) Plugin(ctx context.Context, spec string) (marketmodel.PluginView, error) {
	record, ok, err := s.installs.Get(ctx, strings.TrimSpace(spec))
	if err != nil {
		return marketmodel.PluginView{}, err
	}
	if !ok {
		return marketmodel.PluginView{}, fmt.Errorf("plugin %q 未安装", spec)
	}
	if record.PackageType != marketmodel.PackageTypePlugin {
		return marketmodel.PluginView{}, fmt.Errorf("package %q 类型是%s，不是plugin", spec, record.PackageType)
	}
	capabilities, err := s.capabilities.List(ctx)
	if err != nil {
		return marketmodel.PluginView{}, err
	}
	return marketmodel.PluginView{Install: record, Capabilities: capabilitiesBySpec(capabilities)[record.Spec]}, nil
}

func (s *Service) SetCapabilityEnabled(ctx context.Context, id string, enabled bool) (capmodel.CapabilityIndexRecord, error) {
	record, err := s.capRegistry.SetCapabilityEnabled(ctx, id, enabled)
	if err != nil {
		return capmodel.CapabilityIndexRecord{}, err
	}
	install, ok, err := s.installs.Get(ctx, record.Spec)
	if err != nil {
		return capmodel.CapabilityIndexRecord{}, err
	}
	if ok {
		for i := range install.Capabilities {
			if install.Capabilities[i].ID == record.ID {
				install.Capabilities[i].Enabled = enabled
				install.Capabilities[i].UpdatedAt = s.now().UTC()
			}
		}
		install.UpdatedAt = s.now().UTC()
		if err := s.installs.Put(ctx, install); err != nil {
			return capmodel.CapabilityIndexRecord{}, err
		}
	}
	return record, nil
}
func (s *Service) SetEnabled(ctx context.Context, spec string, enabled bool) (marketmodel.InstallRecord, error) {
	record, ok, err := s.installs.Get(ctx, strings.TrimSpace(spec))
	if err != nil {
		return marketmodel.InstallRecord{}, err
	}
	if !ok {
		return marketmodel.InstallRecord{}, fmt.Errorf("package %q 未安装", spec)
	}
	record.Enabled = enabled
	record.UpdatedAt = s.now().UTC()
	for i := range record.Capabilities {
		record.Capabilities[i].Enabled = enabled
		record.Capabilities[i].UpdatedAt = record.UpdatedAt
	}
	if err := s.installs.Put(ctx, record); err != nil {
		return marketmodel.InstallRecord{}, err
	}
	if err := s.capabilities.SetPackageEnabled(ctx, record.Spec, enabled); err != nil {
		return marketmodel.InstallRecord{}, err
	}
	return record, nil
}

func (s *Service) Uninstall(ctx context.Context, spec string) error {
	trimmed := strings.TrimSpace(spec)
	record, ok, err := s.installs.Get(ctx, trimmed)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("package %q 未安装", spec)
	}
	if err := s.installer.Uninstall(ctx, record); err != nil {
		return err
	}
	if _, _, err := s.installs.Delete(ctx, trimmed); err != nil {
		return err
	}
	if err := s.capabilities.DeletePackage(ctx, record.Spec); err != nil {
		return err
	}
	return nil
}

func (s *Service) installedByType(ctx context.Context, typ marketmodel.PackageType) ([]marketmodel.InstallRecord, error) {
	records, err := s.Installed(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]marketmodel.InstallRecord, 0)
	for _, record := range records {
		if record.PackageType == typ {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *Service) normalizeCapabilityIndex(install marketmodel.InstallRecord) []capmodel.CapabilityIndexRecord {
	out := make([]capmodel.CapabilityIndexRecord, 0, len(install.Capabilities))
	for _, capability := range install.Capabilities {
		capability.Package = install.Package
		capability.PackageType = string(install.PackageType)
		capability.Marketplace = install.Marketplace
		capability.Spec = install.Spec
		capability.Scope = string(install.Scope)
		capability.Enabled = install.Enabled
		capability.InstallRoot = install.InstallRoot
		capability.SourceProvenance = capabilitySourceProvenance(install.SourceProvenance)
		capability.UpdatedAt = install.UpdatedAt
		if capability.ID == "" {
			capability.ID = capabilityID(capability)
		}
		out = append(out, capability)
	}
	return out
}

func capabilitySourceProvenance(source *marketmodel.SourceProvenance) *capmodel.SourceProvenance {
	if source == nil {
		return nil
	}
	return &capmodel.SourceProvenance{
		Type:                  string(source.Type),
		Address:               source.Address,
		Domain:                source.Domain,
		Repo:                  source.Repo,
		URL:                   source.URL,
		Path:                  source.Path,
		Ref:                   source.Ref,
		SubPath:               source.SubPath,
		PackageSource:         source.PackageSource,
		Marketplace:           source.Marketplace,
		MarketplaceSourcePath: source.MarketplaceSourcePath,
		ResolvedRevision:      source.ResolvedRevision,
		ContentHash:           source.ContentHash,
	}
}
func (s *Service) resolvePackageSpec(ctx context.Context, spec string) (string, string, error) {
	pkg, marketplace, err := marketmodel.SplitPackageSpec(spec)
	if err != nil {
		return "", "", err
	}
	if marketplace != "" {
		return pkg, marketplace, nil
	}
	records, err := s.registry.List(ctx)
	if err != nil {
		return "", "", err
	}
	var matches []string
	for _, record := range records {
		fetched, err := s.fetcher.Fetch(ctx, marketcontract.FetchRequest{Source: record.Source, Existing: &record})
		if err != nil {
			continue
		}
		if _, ok := findPackage(fetched.Manifest, pkg); ok {
			matches = append(matches, record.Name)
		}
	}
	if len(matches) == 0 {
		return "", "", fmt.Errorf("package %q 不存在", pkg)
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", "", fmt.Errorf("package %q 存在于多个marketplace，请使用%s@<marketplace>，候选: %s", pkg, pkg, strings.Join(matches, ", "))
	}
	return pkg, matches[0], nil
}

func findPackage(manifest marketmodel.Manifest, name string) (marketmodel.Package, bool) {
	for _, pkg := range manifest.Packages {
		if pkg.Name == name {
			return pkg, true
		}
	}
	return marketmodel.Package{}, false
}

func capabilitiesBySpec(records []capmodel.CapabilityIndexRecord) map[string][]capmodel.CapabilityIndexRecord {
	out := map[string][]capmodel.CapabilityIndexRecord{}
	for _, record := range records {
		out[record.Spec] = append(out[record.Spec], record)
	}
	for spec := range out {
		sort.SliceStable(out[spec], func(i, j int) bool { return out[spec][i].ID < out[spec][j].ID })
	}
	return out
}

func capabilityID(record capmodel.CapabilityIndexRecord) string {
	path := strings.TrimSpace(record.ResourcePath)
	if path == "" {
		path = record.Name
	}
	return record.Spec + ":" + string(record.Type) + ":" + path
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
