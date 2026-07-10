package pathcontract

import "strings"

type literalStyle int

const (
	literalSingle literalStyle = 1 << iota
	literalDouble
	literalBacktick
	literalTripleDouble
	literalHereSingle
	literalHereDouble
)

func sourceLiteralViolations(analyzer string, sources []analyzedSource, styles literalStyle) []Violation {
	var out []Violation
	for _, source := range sources {
		for _, literal := range stringLiterals(source.Content, styles) {
			out = append(out, violationsFromText(analyzer, source.Location, literal)...)
		}
	}
	return out
}

func wholeSourceViolations(analyzer string, sources []analyzedSource, commentPrefix string) []Violation {
	var out []Violation
	for _, source := range sources {
		content := source.Content
		if commentPrefix != "" {
			content = stripLineComments(content, commentPrefix)
		}
		out = append(out, violationsFromText(analyzer, source.Location, content)...)
	}
	return out
}

func stripLineComments(source, prefix string) string {
	var out []string
	for _, line := range strings.Split(source, "\n") {
		idx := strings.Index(line, prefix)
		if idx >= 0 {
			line = line[:idx]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func stringLiterals(source string, styles literalStyle) []string {
	var literals []string
	for i := 0; i < len(source); {
		if styles&literalHereSingle != 0 && strings.HasPrefix(source[i:], "@'") {
			literal, next, ok := readUntil(source, i+2, "'@")
			if ok {
				literals = append(literals, literal)
				i = next
				continue
			}
		}
		if styles&literalHereDouble != 0 && strings.HasPrefix(source[i:], `@"`) {
			literal, next, ok := readUntil(source, i+2, `"@`)
			if ok {
				literals = append(literals, literal)
				i = next
				continue
			}
		}
		if styles&literalTripleDouble != 0 && strings.HasPrefix(source[i:], `"""`) {
			literal, next, ok := readUntil(source, i+3, `"""`)
			if ok {
				literals = append(literals, literal)
				i = next
				continue
			}
		}
		quote := source[i]
		switch {
		case quote == '\'' && styles&literalSingle != 0:
			literal, next, ok := readQuoted(source, i, quote)
			if ok {
				literals = append(literals, literal)
				i = next
				continue
			}
		case quote == '"' && styles&literalDouble != 0:
			literal, next, ok := readQuoted(source, i, quote)
			if ok {
				literals = append(literals, literal)
				i = next
				continue
			}
		case quote == '`' && styles&literalBacktick != 0:
			literal, next, ok := readQuoted(source, i, quote)
			if ok {
				literals = append(literals, literal)
				i = next
				continue
			}
		}
		i++
	}
	return literals
}

func readQuoted(source string, start int, quote byte) (string, int, bool) {
	var b strings.Builder
	escaped := false
	for i := start + 1; i < len(source); i++ {
		if escaped {
			b.WriteByte(source[i])
			escaped = false
			continue
		}
		if source[i] == '\\' {
			b.WriteByte(source[i])
			escaped = true
			continue
		}
		if source[i] == quote {
			return b.String(), i + 1, true
		}
		b.WriteByte(source[i])
	}
	return "", start + 1, false
}

func readUntil(source string, start int, terminator string) (string, int, bool) {
	idx := strings.Index(source[start:], terminator)
	if idx < 0 {
		return "", start, false
	}
	end := start + idx
	return source[start:end], end + len(terminator), true
}
