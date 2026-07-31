package ui

import (
	"testing"
	"time"

	"github.com/jgcastro09/sessionhub/internal/registry"
	"github.com/jgcastro09/sessionhub/internal/tasks"
)

func TestFilteredTasksMatchesIDOrTitle(t *testing.T) {
	m := Model{taskCards: []tasks.Card{
		{ID: "TASK-0001", Title: "Add login"},
		{ID: "TASK-0002", Title: "Fix crash"},
	}}
	if got := m.filteredTasks(); len(got) != 2 {
		t.Fatalf("expected no filtering with empty query, got %d", len(got))
	}
	m.taskFilterQuery = "crash"
	got := m.filteredTasks()
	if len(got) != 1 || got[0].ID != "TASK-0002" {
		t.Fatalf("unexpected filtered tasks: %+v", got)
	}
	m.taskFilterQuery = "TASK-0001"
	got = m.filteredTasks()
	if len(got) != 1 || got[0].Title != "Add login" {
		t.Fatalf("unexpected filtered tasks by id: %+v", got)
	}
}

func TestFilteredRegistryEntriesMatchesPathOrModule(t *testing.T) {
	m := Model{registryEntries: []registry.Entry{
		{EntryID: "e1", Path: "internal/tasks/service.go", Module: "tasks"},
		{EntryID: "e2", Path: "internal/registry/service.go", Module: "registry"},
	}}
	m.registryFilterQuery = "registry"
	got := m.filteredRegistryEntries()
	if len(got) != 1 || got[0].EntryID != "e2" {
		t.Fatalf("unexpected filtered entries: %+v", got)
	}
}

func TestSectionLengthUsesFilteredLists(t *testing.T) {
	m := Model{
		section:   indexOfSection(t, "Tasks"),
		taskCards: []tasks.Card{{ID: "TASK-0001", Title: "A"}, {ID: "TASK-0002", Title: "B"}},
	}
	if got := m.sectionLength(); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
	m.taskFilterQuery = "0002"
	if got := m.sectionLength(); got != 1 {
		t.Fatalf("expected filtered length 1, got %d", got)
	}
}

func indexOfSection(t *testing.T, name string) int {
	t.Helper()
	for i, s := range sections {
		if s == name {
			return i
		}
	}
	t.Fatalf("section %q not found", name)
	return -1
}

func TestSubmitFormTaskSearchSetsFilterAndClosesForm(t *testing.T) {
	m := Model{form: makeForm(taskSearchForm, "Search tasks", []string{"Query"}, []string{""})}
	m.form.fields[0].SetValue("crash")
	updated, cmd := m.submitForm()
	next := updated.(Model)
	if next.taskFilterQuery != "crash" {
		t.Fatalf("expected filter query to be set, got %q", next.taskFilterQuery)
	}
	if next.form.kind != noForm {
		t.Fatalf("expected form to be closed, got kind %v", next.form.kind)
	}
	if cmd != nil {
		t.Fatalf("expected no async command for a local filter update")
	}
}

func TestSubmitFormTaskRequiresProject(t *testing.T) {
	m := Model{form: newTaskForm(), activeProject: -1}
	m.form.fields[0].SetValue("Title")
	m.form.fields[1].SetValue("feature")
	updated, _ := m.submitForm()
	next := updated.(Model)
	if next.form.err == "" {
		t.Fatalf("expected an error when no project is active")
	}
}

func TestTaskDetailsFormRendersSummary(t *testing.T) {
	card := tasks.NewCard("TASK-0001", "Add tests", "feature", tasks.PriorityHigh, time.Now())
	form := taskDetailsForm(card)
	if form.kind != automationDetailsView {
		t.Fatalf("expected automationDetailsView kind, got %v", form.kind)
	}
	if form.title != "TASK-0001 — Add tests" {
		t.Fatalf("unexpected title: %q", form.title)
	}
}
