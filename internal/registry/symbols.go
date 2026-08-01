package registry

import (
	"regexp"
	"strings"
)

// maxSymbolsPerCategory/maxSymbolsPerFile cap how many identifiers one entry
// records, so a generated or vendored-looking file with thousands of
// matches never bloats a record.
const (
	maxSymbolsPerCategory = 128
	maxSymbolsPerFile     = 256
)

type symbolPattern struct {
	category string
	re       *regexp.Regexp
}

// symbolPatternsByLanguage is deliberately light, regex-based structural
// analysis, not a real parser — enough to make search, the Inspector panel,
// and the relationship graph's "implements" pairing useful. Session Hub
// cross-compiles CGO-free (see .goreleaser.yml), which rules out a
// tree-sitter-based parser; this stays pure Go.
var symbolPatternsByLanguage = map[string][]symbolPattern{
	"go": {
		{"functions", regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s+)?([A-Za-z_]\w*)\s*\(`)},
		{"types", regexp.MustCompile(`(?m)^type\s+([A-Za-z_]\w*)\s+(?:struct|interface)\b`)},
	},
	"typescript": {
		{"functions", regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?(?:async\s+)?function\s+([A-Za-z_]\w*)`)},
		{"classes", regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?class\s+([A-Za-z_]\w*)`)},
		{"functions", regexp.MustCompile(`(?m)^\s*export\s+(?:const|function)\s+([A-Za-z_]\w*)`)},
		{"interfaces", regexp.MustCompile(`(?m)^\s*(?:export\s+)?interface\s+([A-Za-z_]\w*)`)},
	},
	"python": {
		{"functions", regexp.MustCompile(`(?m)^def\s+([A-Za-z_]\w*)\s*\(`)},
		{"classes", regexp.MustCompile(`(?m)^class\s+([A-Za-z_]\w*)\s*[:\(]`)},
	},
	"shell": {
		{"functions", regexp.MustCompile(`(?m)^\s*(?:function\s+)?([A-Za-z_]\w*)\s*\(\)\s*\{`)},
	},
	"rust": {
		{"functions", regexp.MustCompile(`(?m)^\s*(?:pub\s+)?fn\s+([A-Za-z_]\w*)`)},
		{"types", regexp.MustCompile(`(?m)^\s*(?:pub\s+)?(?:struct|enum|trait)\s+([A-Za-z_]\w*)`)},
	},
	"c": {
		{"functions", regexp.MustCompile(`(?m)^[A-Za-z_][\w\s\*]*?\b([A-Za-z_]\w*)\s*\([^;{]*\)\s*\{`)},
		{"types", regexp.MustCompile(`(?m)^\s*(?:typedef\s+)?struct\s+([A-Za-z_]\w*)`)},
	},
}

func init() {
	symbolPatternsByLanguage["javascript"] = symbolPatternsByLanguage["typescript"]
	symbolPatternsByLanguage["cpp"] = symbolPatternsByLanguage["c"]
}

// detectSymbols runs the regex patterns for language against content,
// returning identifiers grouped by category with their 1-based source line.
func detectSymbols(content, language string) map[string][]SymbolRef {
	patterns, ok := symbolPatternsByLanguage[language]
	if !ok {
		return nil
	}
	out := map[string][]SymbolRef{}
	seen := map[string]bool{}
	total := 0
	for _, p := range patterns {
		if total >= maxSymbolsPerFile {
			break
		}
		for _, m := range p.re.FindAllStringSubmatchIndex(content, -1) {
			if total >= maxSymbolsPerFile || len(m) < 4 {
				break
			}
			name := strings.TrimSpace(content[m[2]:m[3]])
			if name == "" {
				continue
			}
			key := p.category + "\x00" + name
			if seen[key] || len(out[p.category]) >= maxSymbolsPerCategory {
				continue
			}
			line := 1 + strings.Count(content[:m[0]], "\n")
			out[p.category] = append(out[p.category], SymbolRef{Name: name, Line: line})
			seen[key] = true
			total++
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
