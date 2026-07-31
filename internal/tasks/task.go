// Package tasks implements the Task Manager service described in
// plans/project-task-manager-code-registry.md: a Markdown card per task
// under <project>/.shproject/tasks/cards, with .shproject as the only
// canonical store. There is no server, no Python runtime, and no import
// path from the legacy modules/task-board reference implementation.
package tasks

import (
	"fmt"
	"strings"
)

// Status is one of the stable, non-localized internal statuses. The Web
// Panel is responsible for any Portuguese label; business logic never sees
// localized text.
type Status string

const (
	StatusIdea             Status = "idea"
	StatusBacklog          Status = "backlog"
	StatusReady            Status = "ready"
	StatusInProgress       Status = "in_progress"
	StatusChangesRequested Status = "changes_requested"
	StatusDone             Status = "done"
	StatusArchived         Status = "archived"
)

// AllStatuses lists every valid status in board order.
var AllStatuses = []Status{
	StatusIdea, StatusBacklog, StatusReady, StatusInProgress,
	StatusChangesRequested, StatusDone, StatusArchived,
}

func (s Status) valid() bool {
	for _, candidate := range AllStatuses {
		if candidate == s {
			return true
		}
	}
	return false
}

// transitions defines every status change the workflow allows. Any edge not
// listed here is rejected by ValidateTransition, including transitioning a
// status to itself (callers that only touch other fields should not call
// SetStatus at all).
var transitions = map[Status]map[Status]bool{
	StatusIdea:             {StatusBacklog: true, StatusArchived: true},
	StatusBacklog:          {StatusReady: true, StatusIdea: true, StatusArchived: true},
	StatusReady:            {StatusInProgress: true, StatusBacklog: true, StatusArchived: true},
	StatusInProgress:       {StatusChangesRequested: true, StatusDone: true, StatusReady: true, StatusArchived: true},
	StatusChangesRequested: {StatusInProgress: true, StatusArchived: true},
	// A done task can be reopened automatically by the auditor (section 7.5
	// of the plan: losing its evidence moves it back to changes_requested)
	// or manually by a person.
	StatusDone:     {StatusChangesRequested: true, StatusArchived: true},
	StatusArchived: {StatusBacklog: true},
}

// ValidateTransition rejects any status change the workflow does not allow.
func ValidateTransition(from, to Status) error {
	if !from.valid() {
		return fmt.Errorf("unknown current status %q", from)
	}
	if !to.valid() {
		return fmt.Errorf("unknown target status %q", to)
	}
	if from == to {
		return fmt.Errorf("status %q did not change", from)
	}
	if !transitions[from][to] {
		return fmt.Errorf("invalid status transition %q -> %q", from, to)
	}
	return nil
}

// Type is a free-form but validated task category (feature, bug, chore,
// spike, ...). Kept as a string rather than an enum because Project setups
// vary; the card format only requires it to be non-empty.
type Type string

// Priority mirrors the same "small closed vocabulary, stable identifiers"
// approach as Status.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

func (p Priority) valid() bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent:
		return true
	default:
		return false
	}
}

// sectionOrder is the fixed set of Markdown body sections every card
// carries, in the order they are written back out. Card content preserves
// unknown/legacy headings verbatim (see card.go) but always emits these in
// this order so diffs stay stable.
var sectionOrder = []string{
	"Resumo",
	"Descrição detalhada",
	"Prompt sugerido",
	"Critérios de aceite",
	"Referências técnicas",
	"Audit Contract",
	"Audit Report",
	"Evidências",
	"Resumo de conclusão",
	"Notas e histórico",
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
