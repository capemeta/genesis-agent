package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	skillcard "genesis-agent/internal/capabilities/skill/card"
	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/parser"
)

func newSkillCardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "card",
		Short: "生成和校验 Skill 发布卡片",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSkillCardGenerateCmd(), newSkillCardValidateCmd())
	return cmd
}

func newSkillCardGenerateCmd() *cobra.Command {
	var owner string
	var license string
	var version string
	var force bool
	cmd := &cobra.Command{
		Use:   "generate <skill-dir>",
		Short: "生成 skill-card.md",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillPath, result, err := loadSkillValidation(args[0])
			if err != nil {
				return err
			}
			if result.HasErrors() {
				printValidationResult(result)
				return fmt.Errorf("Skill校验失败，停止生成skill-card.md")
			}
			target := filepath.Join(skillPath, skillcard.SkillCardPath)
			if _, err := os.Stat(target); err == nil && !force {
				return fmt.Errorf("skill-card.md已存在，使用--force覆盖: %s", target)
			} else if err != nil && !os.IsNotExist(err) {
				return err
			}
			content, err := skillcard.Render(skillcard.TemplateDataFromMetadata(result.Metadata, owner, license, version))
			if err != nil {
				return err
			}
			if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
				return err
			}
			fmt.Printf("已生成 Skill Card: %s\n", target)
			return nil
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "维护人或团队")
	cmd.Flags().StringVar(&license, "license", "", "许可证或使用条款")
	cmd.Flags().StringVar(&version, "version", "", "Skill Card中的版本，默认读取Skill version或0.1.0")
	cmd.Flags().BoolVar(&force, "force", false, "覆盖已存在skill-card.md")
	return cmd
}

func newSkillCardValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <skill-dir>",
		Short: "校验 skill-card.md",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillPath, skillResult, err := loadSkillValidation(args[0])
			if err != nil {
				return err
			}
			if skillResult.HasErrors() {
				printValidationResult(skillResult)
				return fmt.Errorf("Skill校验失败，停止校验skill-card.md")
			}
			result := skillcard.NewValidator().ValidateFS(os.DirFS(skillPath))
			printSkillCardValidationResult(result)
			if result.HasErrors() {
				return fmt.Errorf("Skill Card校验失败")
			}
			if len(result.Findings) == 0 {
				fmt.Println("skill-card校验通过：未发现问题")
				return nil
			}
			fmt.Println("skill-card校验通过：存在warning/info，请按需处理")
			return nil
		},
	}
}

func loadSkillValidation(path string) (string, parser.ValidationResult, error) {
	skillPath, err := filepath.Abs(path)
	if err != nil {
		return "", parser.ValidationResult{}, err
	}
	info, err := os.Stat(skillPath)
	if err != nil {
		return "", parser.ValidationResult{}, fmt.Errorf("读取Skill目录失败: %w", err)
	}
	if !info.IsDir() {
		return "", parser.ValidationResult{}, fmt.Errorf("Skill路径必须是目录: %s", skillPath)
	}
	source := contract.ParseSource{
		DirectoryName: filepath.Base(skillPath),
		DisplayPath:   skillPath,
		BaseDirectory: skillPath,
	}
	return skillPath, parser.NewValidator().ValidateSkillFS(os.DirFS(skillPath), source), nil
}

func printSkillCardValidationResult(result skillcard.ValidationResult) {
	for _, finding := range result.Findings {
		if finding.Path == "" {
			fmt.Printf("%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Message)
			continue
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Path, finding.Message)
	}
}
