package command

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"genesis-agent/internal/platform/config"
)

func newConfigureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "交互式配置开发/运行密钥",
		Long:  `通过命令行提问方式配置大模型 API Key 以及各类网络搜索服务商密钥，并保存至 ~/.genesis-agent/cli/config.yaml 中。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("无法获取用户主目录: %w", err)
			}

			userConfigDir := filepath.Join(home, ".genesis-agent", "cli")
			userConfigPath := filepath.Join(userConfigDir, "config.yaml")

			// Check and load existing config
			data := make(map[string]interface{})

			if fileBytes, err := os.ReadFile(userConfigPath); err == nil {
				_ = yaml.Unmarshal(fileBytes, &data)
			}

			ensureMap := func(m map[string]interface{}, key string) map[string]interface{} {
				if _, ok := m[key]; !ok {
					m[key] = make(map[string]interface{})
				}
				nm, ok := m[key].(map[string]interface{})
				if !ok {
					// In case it unmarshaled as map[interface{}]interface{}
					if rawMap, ok := m[key].(map[interface{}]interface{}); ok {
						converted := make(map[string]interface{})
						for k, v := range rawMap {
							converted[fmt.Sprintf("%v", k)] = v
						}
						m[key] = converted
						return converted
					}
					nm = make(map[string]interface{})
					m[key] = nm
				}
				return nm
			}

			webMap := ensureMap(data, "web")
			skillsMap := ensureMap(data, "skills")
			llmMap := ensureMap(data, "llm")
			providersMap := ensureMap(llmMap, "providers")
			qwenMap := ensureMap(providersMap, "qwen")
			qwenAuthMap := ensureMap(qwenMap, "auth")

			getString := func(m map[string]interface{}, key string) string {
				if val, ok := m[key]; ok {
					if str, ok := val.(string); ok {
						return str
					}
				}
				return ""
			}

			reader := bufio.NewReader(os.Stdin)

			ask := func(prompt string, currentVal string) string {
				displayVal := currentVal
				if strings.HasPrefix(displayVal, "dpapi:") {
					displayVal = "[已加密隐藏]"
				}
				if displayVal != "" {
					fmt.Printf("%s [%s]: ", prompt, displayVal)
				} else {
					fmt.Printf("%s: ", prompt)
				}

				input, _ := reader.ReadString('\n')
				input = strings.TrimSpace(input)
				if input == "" {
					return currentVal
				}
				return input
			}

			fmt.Println("\n=== Genesis Agent 密钥配置向导 ===")
			fmt.Println("按回车键保留默认/现有值。")

			// 1. Ask for DPAPI encryption option
			fmt.Print("是否启用 OS 本地安全加密 (DPAPI) 保护密钥存储？ (y/N): ")
			dpapiInput, _ := reader.ReadString('\n')
			dpapiInput = strings.TrimSpace(strings.ToLower(dpapiInput))
			useDPAPI := (dpapiInput == "y" || dpapiInput == "yes")

			encryptKey := func(key string) string {
				if key == "" || strings.HasPrefix(key, "dpapi:") {
					return key
				}
				if useDPAPI {
					enc, err := config.Encrypt([]byte(key))
					if err == nil {
						return "dpapi:" + enc
					}
					fmt.Printf("⚠️  DPAPI 加密失败，使用明文保存: %v\n", err)
				}
				return key
			}

			// 2. Ask for LLM API Key (Qwen default)
			qwenKey := ask("1. 请输入通义千问 (Qwen) API Key", getString(qwenAuthMap, "api_key"))
			qwenAuthMap["api_key"] = encryptKey(qwenKey)

			// 3. Ask for Tavily API Key
			tavilyKey := ask("2. 请输入 Tavily Search API Key", getString(webMap, "tavily_api_key"))
			webMap["tavily_api_key"] = encryptKey(tavilyKey)

			// 4. Ask for Exa API Key
			exaKey := ask("3. 请输入 Exa (Metaphor) API Key", getString(webMap, "exa_api_key"))
			webMap["exa_api_key"] = encryptKey(exaKey)

			// 5. Ask for SerpAPI Key
			serpKey := ask("4. 请输入 SerpAPI Key", getString(webMap, "serpapi_api_key"))
			webMap["serpapi_api_key"] = encryptKey(serpKey)

			// 6. Ask for Brave Key
			braveKey := ask("5. 请输入 Brave Search API Key", getString(webMap, "brave_api_key"))
			webMap["brave_api_key"] = encryptKey(braveKey)

			// 7. Ask for SearXNG URL
			searxngURL := ask("6. 请输入 SearXNG Base URL", getString(webMap, "searxng_base_url"))
			webMap["searxng_base_url"] = searxngURL

			// 8. Ask for optional Skill root
			skillRoot := ask("7. 请输入额外 Skill 根目录（可选）", firstSkillSourcePath(skillsMap))
			if strings.TrimSpace(skillRoot) != "" {
				skillsMap["sources"] = []map[string]interface{}{{"kind": "host", "id": "cli-user", "scope": "user", "path": skillRoot}}
			}

			// Save to file
			if err := os.MkdirAll(userConfigDir, 0755); err != nil {
				return fmt.Errorf("创建配置文件夹失败: %w", err)
			}

			outBytes, err := yaml.Marshal(data)
			if err != nil {
				return fmt.Errorf("编码配置文件失败: %w", err)
			}

			if err := os.WriteFile(userConfigPath, outBytes, 0644); err != nil {
				return fmt.Errorf("保存配置文件失败: %w", err)
			}

			fmt.Printf("\n🎉 配置成功保存至: %s\n\n", userConfigPath)
			return nil
		},
	}

	return cmd
}

func firstSkillSourcePath(skillsMap map[string]interface{}) string {
	raw, ok := skillsMap["sources"]
	if !ok {
		return ""
	}
	sources, ok := raw.([]interface{})
	if !ok || len(sources) == 0 {
		return ""
	}
	first, ok := sources[0].(map[string]interface{})
	if !ok {
		return ""
	}
	path, _ := first["path"].(string)
	return path
}
