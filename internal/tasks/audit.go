package tasks

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jgcastro09/sessionhub/internal/project"
)

// CheckKind is one of the three structured evidence types an Audit Contract
// may declare. Free-form commands are never accepted (see plan section 4.4
// and section 10 "Fora de escopo": execução de comandos livres a partir de
// cards is explicitly out of scope).
type CheckKind string

const (
	CheckRegistry   CheckKind = "registry"
	CheckSource     CheckKind = "source"
	CheckValidation CheckKind = "validation"
)

// Check is one parsed "- kind: value" line from the "Audit Contract"
// section.
type Check struct {
	Kind CheckKind `json:"kind"`
	Raw  string    `json:"raw"`
}

// CheckResult is one evaluated Check.
type CheckResult struct {
	Check  Check `json:"check"`
	Passed bool  `json:"passed"`
	// Resolved is false when the dependency needed to evaluate this check
	// (a Registry entry lookup, a declared validation recipe) is not wired
	// up yet. An unresolved check never counts as passed, and never counts
	// as a hard failure either — it simply blocks automatic completion.
	Resolved bool   `json:"resolved"`
	Detail   string `json:"detail"`
}

// AuditReport is the outcome of running one card's Audit Contract.
type AuditReport struct {
	RanAt            time.Time     `json:"ran_at"`
	Results          []CheckResult `json:"results"`
	ReproduciblePass bool          `json:"reproducible_pass"`
}

// RegistryChecker is the narrow surface internal/registry exposes back to
// internal/tasks so a card can reference "registry: entry:<id>" without
// internal/tasks importing the whole Registry service. Wired in once
// internal/registry exists (Phase C); nil until then.
type RegistryChecker interface {
	EntryExists(projectID, entryID string) (bool, error)
}

// RecipeRunner is the narrow surface a declared validation recipe (from
// .shproject/automation/validation-recipes.json) is executed through. Wired
// in from internal/automation once recipes exist (Phase D); nil until then.
// It must reject any recipe name it does not recognize rather than running
// an arbitrary command.
type RecipeRunner interface {
	Run(projectID, recipeName string) (passed bool, detail string, err error)
}

// ParseAuditContract extracts the declared checks from the raw "Audit
// Contract" section body. Lines that are not "- kind: value" bullets are
// ignored, so a card author can add free-form notes above or below the list.
func ParseAuditContract(body string) []Check {
	var checks []Check
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")
		kind, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		kind = strings.TrimSpace(kind)
		value = strings.TrimSpace(value)
		switch CheckKind(kind) {
		case CheckRegistry, CheckSource, CheckValidation:
			if value != "" {
				checks = append(checks, Check{Kind: CheckKind(kind), Raw: value})
			}
		}
	}
	return checks
}

// RunAudit evaluates every declared check against the current project state
// and returns the report. It never mutates the card; callers decide whether
// to persist the report and/or transition status (see Service.Audit).
func (s *Service) RunAudit(projectID, root string, checks []Check) AuditReport {
	report := AuditReport{RanAt: time.Now().UTC()}
	hasEvidence := false
	hasValidation := false
	allResolvedAndPassed := len(checks) > 0
	for _, check := range checks {
		result := s.evaluateCheck(projectID, root, check)
		report.Results = append(report.Results, result)
		switch check.Kind {
		case CheckRegistry, CheckSource:
			hasEvidence = true
		case CheckValidation:
			hasValidation = true
		}
		if !result.Resolved || !result.Passed {
			allResolvedAndPassed = false
		}
	}
	report.ReproduciblePass = hasEvidence && hasValidation && allResolvedAndPassed
	return report
}

func (s *Service) evaluateCheck(projectID, root string, check Check) CheckResult {
	switch check.Kind {
	case CheckSource:
		return s.evaluateSourceCheck(root, check)
	case CheckRegistry:
		return s.evaluateRegistryCheck(projectID, check)
	case CheckValidation:
		return s.evaluateValidationCheck(projectID, check)
	default:
		return CheckResult{Check: check, Resolved: true, Passed: false, Detail: "unknown check kind"}
	}
}

// evaluateSourceCheck implements "source: <path> contains <substring>".
func (s *Service) evaluateSourceCheck(root string, check Check) CheckResult {
	path, substring, ok := strings.Cut(check.Raw, " contains ")
	path = strings.TrimSpace(path)
	substring = strings.TrimSpace(substring)
	if !ok || path == "" || substring == "" {
		return CheckResult{Check: check, Resolved: true, Passed: false,
			Detail: `expected "<path> contains <substring>"`}
	}
	resolved, err := project.ResolvePath(root, path)
	if err != nil {
		return CheckResult{Check: check, Resolved: true, Passed: false, Detail: err.Error()}
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return CheckResult{Check: check, Resolved: true, Passed: false, Detail: err.Error()}
	}
	if strings.Contains(string(data), substring) {
		return CheckResult{Check: check, Resolved: true, Passed: true, Detail: "match found"}
	}
	return CheckResult{Check: check, Resolved: true, Passed: false, Detail: "substring not found"}
}

// evaluateRegistryCheck implements "registry: entry:<entry_id>".
func (s *Service) evaluateRegistryCheck(projectID string, check Check) CheckResult {
	if s.registry == nil {
		return CheckResult{Check: check, Resolved: false, Detail: "registry is not available"}
	}
	entryID := strings.TrimPrefix(check.Raw, "entry:")
	exists, err := s.registry.EntryExists(projectID, entryID)
	if err != nil {
		return CheckResult{Check: check, Resolved: true, Passed: false, Detail: err.Error()}
	}
	if !exists {
		return CheckResult{Check: check, Resolved: true, Passed: false, Detail: "registry entry not found"}
	}
	return CheckResult{Check: check, Resolved: true, Passed: true, Detail: "registry entry found"}
}

// evaluateValidationCheck implements "validation: <recipe-name>", executed
// through the declared recipes in
// .shproject/automation/validation-recipes.json — never a free-form command.
func (s *Service) evaluateValidationCheck(projectID string, check Check) CheckResult {
	if s.recipes == nil {
		return CheckResult{Check: check, Resolved: false, Detail: "no validation recipe runner is configured"}
	}
	passed, detail, err := s.recipes.Run(projectID, check.Raw)
	if err != nil {
		return CheckResult{Check: check, Resolved: true, Passed: false, Detail: err.Error()}
	}
	return CheckResult{Check: check, Resolved: true, Passed: passed, Detail: detail}
}

// RenderAuditReport formats a report as the Markdown body of the "Audit
// Report" card section.
func RenderAuditReport(report AuditReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Executado em %s.\n\n", report.RanAt.Format(time.RFC3339))
	for _, result := range report.Results {
		status := "FALHOU"
		switch {
		case !result.Resolved:
			status = "PENDENTE"
		case result.Passed:
			status = "PASSOU"
		}
		fmt.Fprintf(&b, "- [%s] %s: %s — %s\n", status, result.Check.Kind, result.Check.Raw, result.Detail)
	}
	if report.ReproduciblePass {
		b.WriteString("\nResultado: evidência reproduzível completa.\n")
	} else {
		b.WriteString("\nResultado: evidência incompleta ou pendente.\n")
	}
	return b.String()
}
