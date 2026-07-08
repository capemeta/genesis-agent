package model

const (
	ProductCLI        = "cli"
	ProductDesktop    = "desktop"
	ProductEnterprise = "enterprise"

	PersistenceDriverJSON   = "json"
	PersistenceDriverSQLite = "sqlite"
	PersistenceDriverSQL    = "sql"
)

// CLIProtocol 返回 CLI 当前使用的本地 JSON 持久化协议。
func CLIProtocol() ProductCapabilityProtocol {
	return ProductCapabilityProtocol{
		Product:      ProductCLI,
		Scopes:       []InstallScope{InstallScopeUser, InstallScopeProject},
		DefaultScope: InstallScopeUser,
		Persistence: ProductPersistenceProfile{
			Product:          ProductCLI,
			Driver:           PersistenceDriverJSON,
			SchemaVersion:    "capability-package-v1",
			SupportsProjects: true,
		},
	}
}

// DesktopProtocol 返回 Desktop 计划使用的 SQLite 持久化协议。
func DesktopProtocol() ProductCapabilityProtocol {
	return ProductCapabilityProtocol{
		Product:      ProductDesktop,
		Scopes:       []InstallScope{InstallScopeUser, InstallScopeProject},
		DefaultScope: InstallScopeUser,
		Persistence: ProductPersistenceProfile{
			Product:          ProductDesktop,
			Driver:           PersistenceDriverSQLite,
			SchemaVersion:    "capability-package-v1",
			SupportsProjects: true,
		},
	}
}

// EnterpriseProtocol 返回 Enterprise 计划使用的多租户持久化协议。
func EnterpriseProtocol() ProductCapabilityProtocol {
	return ProductCapabilityProtocol{
		Product:      ProductEnterprise,
		Scopes:       []InstallScope{InstallScopeTenant, InstallScopeOrg, InstallScopeProject, InstallScopeUser, InstallScopeRole},
		DefaultScope: InstallScopeTenant,
		Persistence: ProductPersistenceProfile{
			Product:          ProductEnterprise,
			Driver:           PersistenceDriverSQL,
			SchemaVersion:    "capability-package-v1",
			SupportsTenant:   true,
			SupportsProjects: true,
		},
	}
}
