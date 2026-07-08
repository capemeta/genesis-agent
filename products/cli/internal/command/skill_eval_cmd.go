package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"genesis-agent/internal/capabilities/skill/contract"
	skilleval "genesis-agent/internal/capabilities/skill/eval"
	"genesis-agent/internal/capabilities/skill/parser"
)

func newSkillEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "管理 Skill evals/evals.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSkillEvalValidateCmd(), newSkillEvalValidateRunCmd())
	return cmd
}

func newSkillEvalValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <skill-dir>",
		Short: "校验 Skill evals/evals.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillPath, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			info, err := os.Stat(skillPath)
			if err != nil {
				return fmt.Errorf("读取Skill目录失败: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("Skill路径必须是目录: %s", skillPath)
			}
			source := contract.ParseSource{DirectoryName: filepath.Base(skillPath), DisplayPath: skillPath, BaseDirectory: skillPath}
			skillResult := parser.NewValidator().ValidateSkillFS(os.DirFS(skillPath), source)
			if skillResult.Metadata.Name == "" {
				return fmt.Errorf("无法读取Skill metadata")
			}
			evalResult := skilleval.NewValidator().ValidateFS(os.DirFS(skillPath), skillResult.Metadata.Name)
			if !evalResult.Found {
				printEvalValidationResult(evalResult)
				return fmt.Errorf("未找到%s", skilleval.EvalsPath)
			}
			fmt.Printf("Skill: %s\nEvals: %d\nFiles: %d\nExpectations: %d\n", evalResult.Suite.SkillName, evalResult.Summary.EvalCount, evalResult.Summary.FileCount, evalResult.Summary.ExpectationCount)
			printEvalValidationResult(evalResult)
			if evalResult.HasErrors() {
				return fmt.Errorf("Skill eval校验失败")
			}
			if len(evalResult.Findings) == 0 {
				fmt.Println("eval校验通过：未发现问题")
				return nil
			}
			fmt.Println("eval校验通过：存在warning/info，请按需处理")
			return nil
		},
	}
}

func newSkillEvalValidateRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate-run <run-dir>",
		Short: "校验 Skill eval run 的 grading/metrics/timing 产物",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runPath, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			info, err := os.Stat(runPath)
			if err != nil {
				return fmt.Errorf("读取eval run目录失败: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("eval run路径必须是目录: %s", runPath)
			}
			result := skilleval.NewValidator().ValidateRunFS(os.DirFS(runPath))
			fmt.Printf("Expectations: %d\nClaims: %d\n", result.Summary.Expectations, result.Summary.Claims)
			printRunValidationResult(result)
			if result.HasErrors() {
				return fmt.Errorf("Skill eval run校验失败")
			}
			if len(result.Findings) == 0 {
				fmt.Println("eval run校验通过：未发现问题")
				return nil
			}
			fmt.Println("eval run校验通过：存在warning/info，请按需处理")
			return nil
		},
	}
}
func printEvalValidationResult(result skilleval.ValidationResult) {
	for _, finding := range result.Findings {
		if finding.Path == "" {
			fmt.Printf("%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Message)
			continue
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Path, finding.Message)
	}
}

func printRunValidationResult(result skilleval.RunValidationResult) {
	for _, finding := range result.Findings {
		if finding.Path == "" {
			fmt.Printf("%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Message)
			continue
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Path, finding.Message)
	}
}
