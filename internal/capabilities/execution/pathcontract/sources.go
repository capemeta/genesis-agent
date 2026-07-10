package pathcontract

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

type analyzedSource struct {
	Language string
	Location string
	Content  string
}

type commandSourceSpec struct {
	Language     string
	Executables  []string
	InlineFlags  []string
	FileFlags    []string
	Extensions   []string
	Subcommands  []string
	StopAtOption bool
}

func commandSources(cmd execmodel.Command, spec commandSourceSpec) []analyzedSource {
	fields := splitCommandFields(cmd.Command)
	var sources []analyzedSource
	for i, field := range fields {
		if !matchesExecutable(field, spec.Executables) {
			continue
		}
		start := i + 1
		if len(spec.Subcommands) > 0 {
			if start >= len(fields) || !containsFold(spec.Subcommands, fields[start]) {
				continue
			}
			start++
		}
		for j := start; j < len(fields); j++ {
			arg := fields[j]
			if flagIndex(spec.InlineFlags, arg) >= 0 && j+1 < len(fields) {
				sources = append(sources, analyzedSource{Language: spec.Language, Location: field + " " + arg, Content: fields[j+1]})
				j++
				continue
			}
			if flagIndex(spec.FileFlags, arg) >= 0 && j+1 < len(fields) {
				if source, ok := readSourceFile(cmd.Cwd, fields[j+1], spec.Language); ok {
					sources = append(sources, source)
				}
				j++
				continue
			}
			if strings.HasPrefix(arg, "-") {
				if spec.StopAtOption {
					break
				}
				continue
			}
			if matchesExtension(arg, spec.Extensions) {
				if source, ok := readSourceFile(cmd.Cwd, arg, spec.Language); ok {
					sources = append(sources, source)
				}
				if spec.StopAtOption {
					break
				}
			} else if spec.StopAtOption {
				break
			}
		}
	}
	return sources
}

func readSourceFile(cwd, arg, language string) (analyzedSource, bool) {
	path := resolveScriptPath(cwd, arg)
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxAnalyzedSourceBytes {
		return analyzedSource{}, false
	}
	return analyzedSource{Language: language, Location: path, Content: string(data)}, true
}

func readSourceDir(cwd, arg, language string, extensions []string) []analyzedSource {
	dir := resolveScriptPath(cwd, arg)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var sources []analyzedSource
	total := 0
	for _, entry := range entries {
		if entry.IsDir() || !matchesExtension(entry.Name(), extensions) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		total += len(data)
		if total > maxAnalyzedSourceBytes {
			break
		}
		sources = append(sources, analyzedSource{Language: language, Location: path, Content: string(data)})
	}
	return sources
}

func resolveScriptPath(cwd, arg string) string {
	if filepath.IsAbs(arg) || cwd == "" {
		return arg
	}
	return filepath.Join(cwd, arg)
}

func matchesExecutable(field string, names []string) bool {
	field = strings.Trim(field, `"'`)
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(field, `\`, string(filepath.Separator))))
	for _, name := range names {
		if base == strings.ToLower(name) {
			return true
		}
	}
	return false
}

func matchesExtension(path string, extensions []string) bool {
	lower := strings.ToLower(strings.Trim(path, `"'`))
	for _, ext := range extensions {
		if strings.HasSuffix(lower, strings.ToLower(ext)) {
			return true
		}
	}
	return false
}

func containsFold(values []string, value string) bool {
	for _, item := range values {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

func flagIndex(flags []string, arg string) int {
	for i, flag := range flags {
		if strings.EqualFold(arg, flag) {
			return i
		}
	}
	return -1
}

func splitCommandFields(command string) []string {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if unicode.IsSpace(r) {
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields
}
