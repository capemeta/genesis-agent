package command

import (
	"encoding/json"
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	skillcard "genesis-agent/internal/capabilities/skill/card"
	"genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/capabilities/skill/parser"
)

func newSkillCreateCmd() *cobra.Command {
	var basePath string
	var description string
	var resources string
	var evals bool
	var force bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "创建 Genesis Skill 脚手架",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := normalizeSkillName(args[0])
			if err := skillmodel.ValidateName(name); err != nil {
				return err
			}
			if strings.TrimSpace(description) == "" {
				description = fmt.Sprintf("Use this skill when the user needs %s.", strings.ReplaceAll(name, "-", " "))
			}
			if basePath == "" {
				basePath = filepath.Join(".genesis", "skills")
			}
			root, err := filepath.Abs(basePath)
			if err != nil {
				return err
			}
			skillDir := filepath.Join(root, name)
			if err := ensureCreatableDir(skillDir, force); err != nil {
				return err
			}
			if err := os.MkdirAll(skillDir, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillTemplate(name, description)), 0o644); err != nil {
				return err
			}
			for _, dir := range parseResourceDirs(resources) {
				if err := os.MkdirAll(filepath.Join(skillDir, dir), 0o755); err != nil {
					return err
				}
			}
			if evals {
				if err := os.MkdirAll(filepath.Join(skillDir, "evals"), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(skillDir, "evals", "evals.json"), []byte(evalsTemplate(name)), 0o644); err != nil {
					return err
				}
			}
			fmt.Printf("已创建 Skill: %s\n", skillDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&basePath, "path", "", "创建到指定根目录，默认 .genesis/skills")
	cmd.Flags().StringVar(&description, "description", "", "Skill description")
	cmd.Flags().StringVar(&resources, "resources", "", "创建资源目录，逗号分隔：references,scripts,assets")
	cmd.Flags().BoolVar(&evals, "evals", false, "创建 evals/evals.json 初稿")
	cmd.Flags().BoolVar(&force, "force", false, "覆盖已存在目录")
	return cmd
}

func newSkillPackageCmd() *cobra.Command {
	var outDir string
	var marketplace string
	var packageName string
	var version string
	var force bool
	cmd := &cobra.Command{
		Use:   "package <skill-dir>",
		Short: "生成可安装的 Genesis Skill marketplace 包",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillDir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			result := parser.NewValidator().ValidateSkillFS(os.DirFS(skillDir), contract.ParseSource{
				DirectoryName: filepath.Base(skillDir),
				DisplayPath:   skillDir,
				BaseDirectory: skillDir,
			})
			if result.HasErrors() {
				printValidationResult(result)
				return fmt.Errorf("Skill校验失败，停止打包")
			}
			if result.Metadata.Name == "" {
				return fmt.Errorf("无法读取Skill metadata")
			}
			name := result.Metadata.Name
			if packageName == "" {
				packageName = name
			}
			if marketplace == "" {
				marketplace = packageName + "-marketplace"
			}
			if outDir == "" {
				outDir = filepath.Join("dist", packageName)
			}
			outDir, err = filepath.Abs(outDir)
			if err != nil {
				return err
			}
			if err := ensureCreatableDir(outDir, force); err != nil {
				return err
			}
			skillDest := filepath.Join(outDir, "skills", name)
			if err := copySkillPackage(skillDir, skillDest); err != nil {
				return err
			}
			manifest := marketmodel.Manifest{
				Schema:      "https://genesis-agent.local/schemas/package-marketplace/v1",
				Name:        marketplace,
				Description: "Genesis Package marketplace",
				Packages: []marketmodel.Package{{
					Name:        packageName,
					Type:        marketmodel.PackageTypeSkillPackage,
					Description: result.Metadata.Description,
					Version:     version,
					Source:      "./",
					Capabilities: []capmodel.CapabilityManifest{{
						Type:        capmodel.CapabilityTypeSkill,
						Name:        name,
						Path:        "./skills/" + name,
						Description: result.Metadata.Description,
					}},
				}},
			}
			if err := writeMarketplaceManifest(outDir, manifest); err != nil {
				return err
			}
			fmt.Printf("已生成 Skill marketplace 包: %s\n", outDir)
			if len(result.Findings) > 0 {
				printValidationResult(result)
			}
			cardResult := skillcard.NewValidator().ValidateFS(os.DirFS(skillDir))
			if len(cardResult.Findings) > 0 {
				printSkillCardValidationResult(cardResult)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&outDir, "out", "", "输出目录，默认 dist/<package>")
	cmd.Flags().StringVar(&marketplace, "marketplace", "", "marketplace 名称")
	cmd.Flags().StringVar(&packageName, "package", "", "package 名称，默认 Skill 名称")
	cmd.Flags().StringVar(&version, "version", "0.1.0", "package version")
	cmd.Flags().BoolVar(&force, "force", false, "覆盖输出目录")
	return cmd
}

func normalizeSkillName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	out := strings.Builder{}
	lastHyphen := false
	for _, r := range value {
		isAllowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAllowed {
			out.WriteRune(r)
			lastHyphen = false
			continue
		}
		if r == '-' && !lastHyphen {
			out.WriteRune('-')
			lastHyphen = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func ensureCreatableDir(path string, force bool) error {
	if _, err := os.Stat(path); err == nil {
		if !force {
			return fmt.Errorf("目录已存在，使用--force覆盖: %s", path)
		}
		if isDangerousOverwriteTarget(path) {
			return fmt.Errorf("拒绝覆盖高风险目录: %s", path)
		}
		return os.RemoveAll(path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isDangerousOverwriteTarget(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	abs = filepath.Clean(abs)
	if parent := filepath.Dir(abs); parent == abs {
		return true
	}
	if wd, err := os.Getwd(); err == nil && filepath.Clean(wd) == abs {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == abs {
		return true
	}
	return false
}

func skillTemplate(name, description string) string {
	return fmt.Sprintf(`---
name: %s
description: %s
---

# %s

## Workflow

1. Understand the input and confirm missing constraints only when they affect the result.
2. Use referenced resources or scripts only when the task requires them.
3. Produce the output contract below.
4. Validate the result before responding.

## Output Contract

- Describe the expected output here.

## Resource Map

- Add references or scripts only when they are actually needed.
`, name, description, skillDisplayName(name))
}
func skillDisplayName(name string) string {
	words := strings.Split(name, "-")
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}
func evalsTemplate(name string) string {
	return fmt.Sprintf(`{
  "skill_name": "%s",
  "evals": [
    {
      "id": 1,
      "prompt": "Replace with a realistic user request.",
      "expected_output": "Describe what a successful result should contain.",
      "files": [],
      "expectations": [
        "The final answer or produced artifact matches the expected_output description."
      ]
    }
  ]
}
`, name)
}

func parseResourceDirs(resources string) []string {
	allowed := map[string]struct{}{"references": {}, "scripts": {}, "assets": {}}
	dirs := make([]string, 0)
	seen := map[string]struct{}{}
	for _, item := range strings.Split(resources, ",") {
		item = strings.TrimSpace(item)
		if _, ok := allowed[item]; !ok {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		dirs = append(dirs, item)
	}
	return dirs
}

func copySkillPackage(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if shouldExcludeSkillPackagePath(rel, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func shouldExcludeSkillPackagePath(rel string, entry os.DirEntry) bool {
	base := filepath.Base(rel)
	if base == ".DS_Store" || strings.HasSuffix(base, ".pyc") {
		return true
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "__pycache__" || part == "node_modules" {
			return true
		}
	}
	return len(parts) == 1 && entry.IsDir() && parts[0] == "evals"
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func writeMarketplaceManifest(root string, manifest marketmodel.Manifest) error {
	dir := filepath.Join(root, ".genesis")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "marketplace.json"), append(data, '\n'), 0o644)
}

func printValidationResult(result parser.ValidationResult) {
	for _, finding := range result.Findings {
		if finding.Path == "" {
			fmt.Printf("%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Message)
			continue
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Path, finding.Message)
	}
}
