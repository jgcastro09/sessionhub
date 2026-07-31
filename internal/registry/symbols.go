package registry

import (
	"regexp"
	"strings"
)

// maxSymbols caps how many top-level identifiers one entry records, so a
// generated or vendored-looking file with thousands of matches never bloats
// a record.
const maxSymbols = 64

var symbolPatterns = map[string][]*regexp.Regexp{
	"go": {
		regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s+)?([A-Za-z_]\w*)\s*\(`),
		regexp.MustCompile(`(?m)^type\s+([A-Za-z_]\w*)\s+(?:struct|interface)\b`),
	},
	"typescript": {
		regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?(?:async\s+)?function\s+([A-Za-z_]\w*)`),
		regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?class\s+([A-Za-z_]\w*)`),
		regexp.MustCompile(`(?m)^\s*export\s+(?:const|function)\s+([A-Za-z_]\w*)`),
		regexp.MustCompile(`(?m)^\s*(?:export\s+)?interface\s+([A-Za-z_]\w*)`),
	},
	"python": {
		regexp.MustCompile(`(?m)^def\s+([A-Za-z_]\w*)\s*\(`),
		regexp.MustCompile(`(?m)^class\s+([A-Za-z_]\w*)\s*[:\(]`),
	},
	"shell": {
		regexp.MustCompile(`(?m)^\s*(?:function\s+)?([A-Za-z_]\w*)\s*\(\)\s*\{`),
	},
}

func init() {
	symbolPatterns["javascript"] = symbolPatterns["typescript"]
}

// detectSymbols runs the light, regex-based structural analysis the plan
// calls for (5.3: "analisar estrutura leve de Go, JS/TS, Python, ... shell e
// manifestos") — deliberately not a real parser, just enough to make search
// and context packs useful.
func detectSymbols(content, language string) []string {
	patterns, ok := symbolPatterns[language]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, pattern := range patterns {
		for _, match := range pattern.FindAllStringSubmatch(content, -1) {
			name := strings.TrimSpace(match[1])
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
			if len(out) >= maxSymbols {
				return out
			}
		}
	}
	return out
}
