package service

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"genesis-agent/internal/capabilities/skill/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

// preflightRuntime 对 Skill 声明的 runtime 做轻量探测；失败返回 missing 列表。
// 对齐设计 §6.9；借鉴 Kode ensureRipgrepReady 的「探测 + 可读 Fix」，不做静默安装。
func (s *Service) preflightRuntime(ctx context.Context, meta model.Metadata, scriptRel, workspaceRoot string) []scriptcontract.MissingDep {
	rt := meta.Dependencies.Runtime
	if len(rt.Python) == 0 && len(rt.Node) == 0 && len(rt.System) == 0 {
		return nil
	}
	ext := strings.ToLower(filepathExt(scriptRel))
	var missing []scriptcontract.MissingDep

	// 仅对当前脚本相关生态做探测，避免每次跑 py 都 require node 包。
	needNode := ext == ".js" || ext == ".mjs" || ext == ".cjs" || ext == ".ts"
	needPython := ext == ".py"

	if needNode {
		for _, pkg := range rt.Node {
			name := strings.TrimSpace(pkg.Name)
			if name == "" {
				continue
			}
			mod := strings.TrimSpace(pkg.Require)
			if mod == "" {
				mod = name
			}
			if !s.nodeRequireOK(ctx, mod, workspaceRoot) {
				missing = append(missing, scriptcontract.MissingDep{Manager: "npm", Name: name, Require: mod})
			}
		}
	}
	if needPython {
		for _, pkg := range rt.Python {
			name := strings.TrimSpace(pkg.Name)
			if name == "" {
				continue
			}
			imp := strings.TrimSpace(pkg.Import)
			if imp == "" {
				imp = name
			}
			if !s.pythonImportOK(ctx, imp) {
				missing = append(missing, scriptcontract.MissingDep{Manager: "pip", Name: name, Require: imp})
			}
		}
	}
	// system：LookPath 探测（设计 §6.9）；缺省属镜像/本机预装。
	// 不在此函数内决定是否硬失败——调用方对 npm/pip 硬失败，对仅 system 缺失写 warning 后继续
	//（Skill 级 system 声明不代表每个脚本都需要 soffice，避免误伤 create_pptx.js）。
	for _, pkg := range rt.System {
		name := strings.TrimSpace(pkg.Name)
		if name == "" {
			continue
		}
		cmdName := strings.TrimSpace(pkg.Command)
		if cmdName == "" {
			cmdName = name
		}
		if !isSafePackageNameLite(cmdName) {
			missing = append(missing, scriptcontract.MissingDep{Manager: "system", Name: name, Require: cmdName})
			continue
		}
		if _, err := exec.LookPath(cmdName); err != nil {
			missing = append(missing, scriptcontract.MissingDep{Manager: "system", Name: name, Require: cmdName})
		}
	}
	return missing
}

func filepathExt(p string) string {
	i := strings.LastIndex(p, ".")
	if i < 0 {
		return ""
	}
	return p[i:]
}

func (s *Service) pythonImportOK(ctx context.Context, importName string) bool {
	bin := s.pythonBin
	if strings.TrimSpace(bin) == "" {
		bin = "python"
	}
	// 仅允许简单标识符 / 点分模块，防注入。
	if !isSafeImportIdent(importName) {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "-c", fmt.Sprintf("import %s", importName))
	return cmd.Run() == nil
}

func (s *Service) nodeRequireOK(ctx context.Context, moduleName, workspaceRoot string) bool {
	bin := s.nodeBin
	if strings.TrimSpace(bin) == "" {
		bin = "node"
	}
	if !isSafePackageNameLite(moduleName) {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "-e", fmt.Sprintf("require(%q)", moduleName))
	cmd.Env = append(cmd.Environ(), "NODE_PATH="+nodeModuleSearchPath(workspaceRoot))
	return cmd.Run() == nil
}

func isSafeImportIdent(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
			continue
		}
		if r >= '0' && r <= '9' && i > 0 {
			continue
		}
		if r == '.' && i > 0 {
			continue
		}
		return false
	}
	return true
}

func isSafePackageNameLite(s string) bool {
	if s == "" || len(s) > 128 || strings.Contains(s, "..") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '@', r == '/', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func installableMissing(missing []scriptcontract.MissingDep) []scriptcontract.MissingDep {
	out := make([]scriptcontract.MissingDep, 0, len(missing))
	for _, m := range missing {
		if m.Manager == "npm" || m.Manager == "pip" {
			out = append(out, m)
		}
	}
	return out
}

// tryAutoRetryInstall 在 opt-in 且已注入 Installer 时尝试安装可对话期安装的包。
// 成功则清除失败态并返回 true（调用方应再执行一次）；system 缺失或已重试过则返回 false。
func (s *Service) tryAutoRetryInstall(ctx context.Context, skill string, out *scriptcontract.RunResult) bool {
	if out == nil || !s.autoRetryAfterInstall || s.installer == nil {
		return false
	}
	if out.Metadata != nil && out.Metadata["auto_retried"] == "true" {
		return false
	}
	if out.FailureKind != "dependency_missing" {
		return false
	}
	// system 不可对话期安装；仅对 npm/pip 缺失尝试安装。若同时缺 system，仍可先装包再重试。
	miss := installableMissing(out.Missing)
	if len(miss) == 0 {
		return false
	}
	if installErr := s.installer.InstallRuntime(ctx, skill, miss); installErr != nil {
		out.Warnings = append(out.Warnings, "auto_retry_after_install failed: "+installErr.Error())
		return false
	}
	if out.Metadata == nil {
		out.Metadata = map[string]string{}
	}
	out.Metadata["auto_retried"] = "true"
	out.Warnings = append(out.Warnings, "auto_retry_after_install: installed then re-running")
	out.OK = true
	out.FailureKind = ""
	out.Missing = nil
	out.Error = ""
	out.SuggestedAction = ""
	out.SuggestedInstall = nil
	out.Retryable = false
	out.ExitCode = 0
	out.Stdout = ""
	out.Stderr = ""
	return true
}
