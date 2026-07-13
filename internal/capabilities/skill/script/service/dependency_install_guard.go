package service

import (
	"path/filepath"
	"strings"

	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

type dependencyInstallCommand struct {
	Manager  string
	Packages []string
}

func detectDependencyInstallCommand(command string) (dependencyInstallCommand, bool) {
	fields := commandFields(command)
	fields = unwrapShellCommand(fields)
	if len(fields) < 2 {
		return dependencyInstallCommand{}, false
	}
	bin := commandBase(fields[0])
	switch bin {
	case "npm":
		if isNPMInstallVerb(fields[1]) {
			return dependencyInstallCommand{Manager: "npm", Packages: packageArgs(fields[2:])}, true
		}
	case "pnpm", "yarn":
		if isJSInstallVerb(fields[1]) {
			return dependencyInstallCommand{Manager: "npm", Packages: packageArgs(fields[2:])}, true
		}
	case "pip", "pip3":
		if strings.EqualFold(fields[1], "install") {
			return dependencyInstallCommand{Manager: "pip", Packages: packageArgs(fields[2:])}, true
		}
	case "python", "python3", "py":
		if len(fields) >= 4 && fields[1] == "-m" && strings.EqualFold(fields[2], "pip") && strings.EqualFold(fields[3], "install") {
			return dependencyInstallCommand{Manager: "pip", Packages: packageArgs(fields[4:])}, true
		}
	case "uv":
		if len(fields) >= 3 && strings.EqualFold(fields[1], "pip") && strings.EqualFold(fields[2], "install") {
			return dependencyInstallCommand{Manager: "pip", Packages: packageArgs(fields[3:])}, true
		}
	}
	return dependencyInstallCommand{}, false
}

func installCommandMissingDeps(cmd dependencyInstallCommand) []scriptcontract.MissingDep {
	out := make([]scriptcontract.MissingDep, 0, len(cmd.Packages))
	for _, pkg := range cmd.Packages {
		name := normalizeInstallPackageName(cmd.Manager, pkg)
		if name == "" || !isSafePackageNameLite(name) {
			continue
		}
		out = append(out, scriptcontract.MissingDep{Manager: cmd.Manager, Name: name, Require: name})
	}
	return out
}

func commandFields(command string) []string {
	raw := strings.Fields(strings.TrimSpace(command))
	fields := make([]string, 0, len(raw))
	for _, field := range raw {
		field = strings.Trim(strings.TrimSpace(field), `"'`)
		if field != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

func unwrapShellCommand(fields []string) []string {
	if len(fields) < 3 {
		return fields
	}
	bin := commandBase(fields[0])
	flag := strings.ToLower(fields[1])
	if (bin == "sh" || bin == "bash" || bin == "zsh") && (flag == "-c" || flag == "-lc") {
		return commandFields(strings.Join(fields[2:], " "))
	}
	if (bin == "cmd" || bin == "cmd.exe") && flag == "/c" {
		return commandFields(strings.Join(fields[2:], " "))
	}
	return fields
}

func commandBase(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(filepath.Base(strings.ReplaceAll(value, "\\", "/")), ".exe")
	return value
}

func isNPMInstallVerb(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "install", "i", "add", "ci":
		return true
	default:
		return false
	}
}

func isJSInstallVerb(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "install", "add":
		return true
	default:
		return false
	}
}

func packageArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := strings.Trim(strings.TrimSpace(args[i]), `"'`)
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if optionConsumesValue(arg) && i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.Contains(arg, "=") && strings.HasPrefix(arg, "--") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func optionConsumesValue(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "-r", "--requirement", "-c", "--constraint", "--index-url", "--extra-index-url", "--target", "--prefix", "--cache-dir":
		return true
	default:
		return false
	}
}

func normalizeInstallPackageName(manager, pkg string) string {
	pkg = strings.Trim(strings.TrimSpace(pkg), `"'`)
	if pkg == "" {
		return ""
	}
	if manager == "pip" {
		for _, sep := range []string{"==", ">=", "<=", "~=", "!=", ">", "<"} {
			if idx := strings.Index(pkg, sep); idx > 0 {
				pkg = pkg[:idx]
				break
			}
		}
		if idx := strings.Index(pkg, "["); idx > 0 {
			pkg = pkg[:idx]
		}
		return strings.TrimSpace(pkg)
	}
	if strings.HasPrefix(pkg, "@") {
		parts := strings.Split(pkg, "@")
		if len(parts) >= 3 {
			return "@" + parts[1]
		}
		return pkg
	}
	if idx := strings.LastIndex(pkg, "@"); idx > 0 {
		return pkg[:idx]
	}
	return pkg
}
