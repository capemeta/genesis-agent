package httpclient

// AuthType 表示认证方式。
type AuthType string

const (
	AuthTypeNone         AuthType = "none"
	AuthTypeAPIKeyHeader AuthType = "api_key_header"
	AuthTypeAPIKeyQuery  AuthType = "api_key_query"
	AuthTypeBearerToken  AuthType = "bearer_token"
	AuthTypeBasicAuth    AuthType = "basic_auth"
	AuthTypeCustomHeader AuthType = "custom_header"
)

// AuthConfig 描述一次请求的认证配置。
type AuthConfig struct {
	Type       AuthType
	HeaderName string
	QueryName  string
	Username   string
	Password   string
	Token      string
	Value      string
}
