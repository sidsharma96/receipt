package fingerprint

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// normalizeScratchFlags replaces only a numeric suffix after an exact known
// prefix, followed by the original destination and delimiter. It preserves the
// old regexp's ASCII delimiters, including adjacent and quoted flags.
func normalizeScratchFlags(flags, pathPrefix string) string {
	prefixes := []string{"-ffile-prefix-map=" + pathPrefix, "-fdebug-prefix-map=" + pathPrefix}
	var out strings.Builder
	copied := 0
	for at := 0; at < len(flags); {
		relative := strings.Index(flags[at:], "-f")
		if relative < 0 {
			break
		}
		start := at + relative
		at = start + 2
		if start > 0 && !scratchDelimiter(flags[start-1]) {
			continue
		}
		first, last := scratchDigits(flags[start:], prefixes)
		if first == last {
			continue
		}
		out.WriteString(flags[copied : start+first])
		out.WriteString("<scratch>")
		copied = start + last
		at = copied
	}
	if copied == 0 {
		return flags
	}
	out.WriteString(flags[copied:])
	return out.String()
}

func scratchDigits(flag string, prefixes []string) (int, int) {
	const destination = "=/tmp/go-build"
	for _, prefix := range prefixes {
		if !strings.HasPrefix(flag, prefix) {
			continue
		}
		first := len(prefix)
		last := first
		for last < len(flag) && flag[last] >= '0' && flag[last] <= '9' {
			last++
		}
		if last == first || !strings.HasPrefix(flag[last:], destination) {
			continue
		}
		end := last + len(destination)
		if end == len(flag) || scratchDelimiter(flag[end]) {
			return first, last
		}
	}
	return 0, 0
}

func scratchDelimiter(c byte) bool {
	return c == ' ' || c == '\t' || c == '"' || c == '\''
}

// scratchFlags keeps the old compiler's refusal behavior for unusual paths.
// Ordinary filesystem paths avoid compiling a per-call regular expression.
func scratchFlags(flags, prefix string) (string, error) {
	if len(prefix) <= 4096 && utf8.ValidString(prefix) {
		return normalizeScratchFlags(flags, prefix), nil
	}
	pattern, err := regexp.Compile(`(^|[ \t"'])(-f(?:file|debug)-prefix-map=` + regexp.QuoteMeta(prefix) + `)[0-9]+(=/tmp/go-build)([ \t"']|$)`)
	if err != nil {
		return "", fmt.Errorf("compile Go scratch-path pattern: %w", err)
	}
	for {
		next := pattern.ReplaceAllString(flags, `${1}${2}<scratch>${3}${4}`)
		if next == flags {
			return flags, nil
		}
		flags = next
	}
}
