package tasks

import (
	"regexp"
	"strings"
)

// This file parses the standardized card draft produced by SessionHub's
// "Gerador de Cards" prompt contract (docs/usage.md): a fixed nine-field
// Markdown block an AI assistant is instructed to return after being handed
// a raw feature/bug request, capped at a 9,000-token conservative estimate.
// The parser mirrors modules/task-board/app/js/card-importer.js field for
// field so a draft written against the legacy NodeStage Task Board prompt
// also imports cleanly here — the two tools share one prompt contract.

// importField is one of the nine fields the prompt contract requires.
type importField string

const (
	fieldTitle            importField = "title"
	fieldSummary          importField = "summary"
	fieldType             importField = "type"
	fieldPriority         importField = "priority"
	fieldAreas            importField = "areas"
	fieldDescription      importField = "description"
	fieldAIPrompt         importField = "ai_prompt"
	fieldExpectedFeatures importField = "expected_features"
	fieldAuditContract    importField = "audit_contract"
)

// importFieldByLabel maps a normalized (accent-folded, lowercased) field
// label to the field it fills. Keys are already normalized so lookups never
// re-run normalizeImportLabel on themselves.
var importFieldByLabel = map[string]importField{
	"titulo direto":              fieldTitle,
	"resumo de uma frase":        fieldSummary,
	"tipo":                       fieldType,
	"prioridade":                 fieldPriority,
	"areas envolvidas":           fieldAreas,
	"descricao detalhada":        fieldDescription,
	"prompt detalhado para a ia": fieldAIPrompt,
	"funcionalidades esperadas":  fieldExpectedFeatures,
	"contrato de auditoria":      fieldAuditContract,
}

// importRequiredFields lists the fields the prompt contract never allows to
// be empty, paired with the exact label shown back to the user in an error.
var importRequiredFields = []struct {
	field importField
	label string
}{
	{fieldTitle, "Título Direto"},
	{fieldSummary, "Resumo de Uma Frase"},
	{fieldDescription, "Descrição Detalhada"},
	{fieldAIPrompt, "Prompt Detalhado para a IA"},
	{fieldExpectedFeatures, "Funcionalidades Esperadas"},
	{fieldType, "Tipo"},
	{fieldPriority, "Prioridade"},
}

// importTypes is the closed vocabulary the "Tipo" field accepts, in the
// canonical casing written back to the card. Type stays a free-form string
// on Card (task.go), so these Portuguese labels are kept verbatim rather
// than translated — that is what a human reading the Kanban expects.
var importTypes = []Type{"Ideia", "Implementação", "Melhoria", "Ajuste", "Bug", "Correção"}

// importPriorities maps the "Prioridade" field's closed vocabulary to this
// package's Priority enum. "Crítica" is the one label that does not read as
// a cognate of its Priority constant.
var importPriorities = map[Priority]string{
	PriorityLow:    "Baixa",
	PriorityMedium: "Média",
	PriorityHigh:   "Alta",
	PriorityUrgent: "Crítica",
}

// importHeadingPattern recognizes one "Label: value" line, tolerating an
// optional leading bullet marker (-, *, +) and optional Markdown emphasis
// (*_`) wrapping the label, so both "* Título Direto: ..." and
// "**Título Direto:** ..." parse the same way.
var importHeadingPattern = regexp.MustCompile("^\\s*(?:[-*+]\\s+)?(?:[*_`]+)?(.+?)(?:[*_`]+)?\\s*:\\s*(.*)$")

var diacriticFolder = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c",
)

// normalizeImportLabel folds a label to the form importFieldByLabel and
// importTypes/importPriorities are keyed/compared against: lowercase,
// diacritic-free, stripped of a redundant leading bullet marker and of
// Markdown emphasis characters.
func normalizeImportLabel(value string) string {
	v := diacriticFolder.Replace(strings.ToLower(strings.TrimSpace(value)))
	for _, prefix := range []string{"- ", "* ", "+ "} {
		v = strings.TrimPrefix(v, prefix)
	}
	v = strings.NewReplacer("*", "", "_", "", "`", "").Replace(v)
	return strings.TrimSpace(v)
}

func parseImportHeading(line string) (importField, string, bool) {
	match := importHeadingPattern.FindStringSubmatch(line)
	if match == nil {
		return "", "", false
	}
	field, ok := importFieldByLabel[normalizeImportLabel(match[1])]
	if !ok {
		return "", "", false
	}
	return field, match[2], true
}

func canonicalImportType(value string) Type {
	normalized := normalizeImportLabel(value)
	for _, candidate := range importTypes {
		if normalizeImportLabel(string(candidate)) == normalized {
			return candidate
		}
	}
	return ""
}

func canonicalImportPriority(value string) Priority {
	normalized := normalizeImportLabel(value)
	for priority, label := range importPriorities {
		if normalizeImportLabel(label) == normalized {
			return priority
		}
	}
	return ""
}

// lexicalUnitPattern approximates a tokenizer well enough for a conservative
// budget check: runs of letters/digits/underscore count as one unit each,
// every other non-space character (punctuation, CJK, emoji) counts as its
// own unit.
var lexicalUnitPattern = regexp.MustCompile(`[\p{L}\p{N}_]+|[^\s\p{L}\p{N}_]`)

const (
	maxEstimatedTokens        = 9000
	conservativeBytesPerToken = 2.6
)

// estimateTokens is a deliberately conservative (over-)estimate: the higher
// of a bytes-per-token heuristic and a lexical-unit count, so a Card that
// passes this check is very unlikely to blow an actual model's budget.
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	byTokenBytes := int((float64(len(text)) / conservativeBytesPerToken) + 0.999999)
	byLexicalUnits := len(lexicalUnitPattern.FindAllString(text, -1))
	if byLexicalUnits > byTokenBytes {
		return byLexicalUnits
	}
	return byTokenBytes
}

func classifyImportSize(tokens int) string {
	switch {
	case tokens <= 4000:
		return "compact"
	case tokens <= 6000:
		return "adequate"
	case tokens <= maxEstimatedTokens:
		return "detailed"
	default:
		return "needs further optimization"
	}
}

// ImportMetrics reports the size of the parsed draft against the prompt
// contract's token budget, so a caller can show why an import was rejected
// or how close it is to the limit.
type ImportMetrics struct {
	Tokens         int    `json:"tokens"`
	Words          int    `json:"words"`
	Characters     int    `json:"characters"`
	Classification string `json:"classification"`
}

// ImportResult is the outcome of parsing one card draft: either Valid with
// every field ready to hand to Service.Import, or invalid with the exact
// reasons an operator (or the AI that wrote the draft) needs to fix.
type ImportResult struct {
	Valid   bool          `json:"valid"`
	Errors  []string      `json:"errors"`
	Metrics ImportMetrics `json:"metrics"`

	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	Description      string   `json:"description"`
	AIPrompt         string   `json:"ai_prompt"`
	ExpectedFeatures string   `json:"expected_features"`
	AuditContract    string   `json:"audit_contract"`
	ImpactedAreas    []string `json:"impacted_areas"`
	Type             Type     `json:"type"`
	Priority         Priority `json:"priority"`
}

// ParseImport parses raw draft text against the prompt contract's nine
// fields. It never returns an error: a malformed or incomplete draft comes
// back as ImportResult{Valid: false, Errors: [...]} so the caller can
// display exactly what to fix, the same way the reference implementation
// (card-importer.js) does.
func ParseImport(text string) ImportResult {
	source := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))

	values := map[importField]string{
		fieldType:     "Implementação",
		fieldPriority: "Média",
	}
	seen := map[importField]bool{}
	var active importField
	var activeSet bool

	for _, line := range strings.Split(source, "\n") {
		if field, value, ok := parseImportHeading(line); ok {
			active, activeSet = field, true
			seen[field] = true
			values[field] = strings.TrimSpace(value)
			continue
		}
		if activeSet {
			if values[active] != "" {
				values[active] += "\n"
			}
			values[active] += line
		}
	}
	for field, value := range values {
		values[field] = strings.TrimSpace(value)
	}

	var errors []string
	var missing []string
	for _, required := range importRequiredFields {
		requiresExplicitPresence := required.field == fieldType || required.field == fieldPriority
		if values[required.field] == "" || (requiresExplicitPresence && !seen[required.field]) {
			missing = append(missing, required.label)
		}
	}
	if len(missing) > 0 {
		errors = append(errors, "Campos obrigatórios ausentes: "+strings.Join(missing, ", ")+".")
	}

	taskType := canonicalImportType(values[fieldType])
	if seen[fieldType] && values[fieldType] != "" && taskType == "" {
		errors = append(errors, "Invalid Type. Use one of: "+joinTypes(importTypes)+".")
	}
	priority := canonicalImportPriority(values[fieldPriority])
	if seen[fieldPriority] && values[fieldPriority] != "" && priority == "" {
		errors = append(errors, "Invalid Priority. Use one of: Baixa, Média, Alta, Crítica.")
	}

	tokens := estimateTokens(source)
	metrics := ImportMetrics{
		Tokens:         tokens,
		Words:          len(strings.Fields(source)),
		Characters:     len([]rune(source)),
		Classification: classifyImportSize(tokens),
	}
	if tokens > maxEstimatedTokens {
		errors = append(errors, "Complete Card exceeds the 9,000-token conservative estimated budget. Regenerate it without removing unique requirements.")
	}

	var areas []string
	for _, area := range strings.Split(values[fieldAreas], ",") {
		if trimmed := strings.TrimSpace(area); trimmed != "" {
			areas = append(areas, trimmed)
		}
	}

	return ImportResult{
		Valid:            len(errors) == 0,
		Errors:           errors,
		Metrics:          metrics,
		Title:            values[fieldTitle],
		Summary:          values[fieldSummary],
		Description:      values[fieldDescription],
		AIPrompt:         values[fieldAIPrompt],
		ExpectedFeatures: values[fieldExpectedFeatures],
		AuditContract:    values[fieldAuditContract],
		ImpactedAreas:    areas,
		Type:             taskType,
		Priority:         priority,
	}
}

func joinTypes(types []Type) string {
	labels := make([]string, len(types))
	for i, t := range types {
		labels[i] = string(t)
	}
	return strings.Join(labels, ", ")
}
