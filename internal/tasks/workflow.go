package tasks

import (
	"os"
	"path/filepath"

	"github.com/jgcastro09/sessionhub/internal/atomicfile"
	"github.com/jgcastro09/sessionhub/internal/project"
)

// WorkflowDocument is the portable, inspectable form of the status/
// transition graph hard-coded in task.go. It exists so tooling outside this
// binary (an agent bridge, a docs generator) can discover the workflow
// without parsing Go source; internal/tasks itself always validates
// transitions against the Go table, never against this file, so a stray
// edit to workflow.json can never change runtime behavior.
type WorkflowDocument struct {
	SchemaVersion int                 `json:"schema_version"`
	Statuses      []Status            `json:"statuses"`
	Transitions   map[Status][]Status `json:"transitions"`
}

func workflowPath(root string) string {
	return filepath.Join(root, project.Directory, "tasks", "workflow.json")
}

func buildWorkflowDocument() WorkflowDocument {
	doc := WorkflowDocument{SchemaVersion: 1, Statuses: AllStatuses, Transitions: map[Status][]Status{}}
	for from, tos := range transitions {
		var list []Status
		for to := range tos {
			list = append(list, to)
		}
		SortStatuses(list)
		doc.Transitions[from] = list
	}
	return doc
}

// SortStatuses orders statuses by their position in AllStatuses, falling
// back to appending unknown values at the end in encounter order.
func SortStatuses(statuses []Status) {
	rank := make(map[Status]int, len(AllStatuses))
	for i, s := range AllStatuses {
		rank[s] = i
	}
	for i := 1; i < len(statuses); i++ {
		for j := i; j > 0 && rank[statuses[j-1]] > rank[statuses[j]]; j-- {
			statuses[j-1], statuses[j] = statuses[j], statuses[j-1]
		}
	}
}

// ensureWorkflowFile writes .shproject/tasks/workflow.json if it does not
// exist yet. Safe to call on every request; the existence check keeps the
// common case a single stat call.
func ensureWorkflowFile(root string) error {
	path := workflowPath(root)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicfile.WriteJSON(path, buildWorkflowDocument())
}
