package tasks

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/events"
	"github.com/jgcastro09/sessionhub/internal/project"
)

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	proj, err := project.Init(root, "Test Project")
	if err != nil {
		t.Fatalf("project.Init: %v", err)
	}
	catalog, err := project.OpenCatalog(t.TempDir())
	if err != nil {
		t.Fatalf("OpenCatalog: %v", err)
	}
	if _, err := catalog.Attach(root); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return New(catalog, events.NewBus(), t.TempDir()), proj.ID
}

func TestCreateAllocatesSequentialIDs(t *testing.T) {
	svc, projectID := newTestService(t)
	first, err := svc.Create(projectID, CreateInput{Title: "First", Type: "feature"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := svc.Create(projectID, CreateInput{Title: "Second", Type: "bug"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if first.ID != "TASK-0001" || second.ID != "TASK-0002" {
		t.Fatalf("unexpected ids: %s, %s", first.ID, second.ID)
	}
	if first.Status != StatusIdea {
		t.Fatalf("new task should start as idea, got %s", first.Status)
	}
}

func TestCreateRejectsMissingTitle(t *testing.T) {
	svc, projectID := newTestService(t)
	if _, err := svc.Create(projectID, CreateInput{Type: "feature"}); err == nil {
		t.Fatal("expected an error for a missing title")
	}
}

func TestSetStatusRejectsInvalidTransition(t *testing.T) {
	svc, projectID := newTestService(t)
	card, err := svc.Create(projectID, CreateInput{Title: "T", Type: "feature"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.SetStatus(projectID, card.ID, StatusDone); err == nil {
		t.Fatal("expected idea -> done to be rejected")
	}
	if _, err := svc.SetStatus(projectID, card.ID, StatusBacklog); err != nil {
		t.Fatalf("idea -> backlog should be allowed: %v", err)
	}
	if _, err := svc.SetStatus(projectID, card.ID, StatusReady); err != nil {
		t.Fatalf("backlog -> ready should be allowed: %v", err)
	}
}

func TestGetMissingTaskReturnsNotFound(t *testing.T) {
	svc, projectID := newTestService(t)
	if _, err := svc.Get(projectID, "TASK-9999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListFiltersByStatusAndQuery(t *testing.T) {
	svc, projectID := newTestService(t)
	a, _ := svc.Create(projectID, CreateInput{Title: "Add login", Type: "feature"})
	b, _ := svc.Create(projectID, CreateInput{Title: "Fix crash", Type: "bug"})
	if _, err := svc.SetStatus(projectID, a.ID, StatusBacklog); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	backlog, err := svc.List(projectID, Filter{Status: []Status{StatusBacklog}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(backlog) != 1 || backlog[0].ID != a.ID {
		t.Fatalf("expected only %s in backlog, got %+v", a.ID, backlog)
	}

	found, err := svc.Search(projectID, "crash")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found) != 1 || found[0].ID != b.ID {
		t.Fatalf("expected search to find %s, got %+v", b.ID, found)
	}
}

func TestConcurrentCreateNeverProducesDuplicateIDs(t *testing.T) {
	svc, projectID := newTestService(t)
	const n = 20
	var wg sync.WaitGroup
	ids := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			card, err := svc.Create(projectID, CreateInput{Title: "Task", Type: "feature"})
			if err != nil {
				errs <- err
				return
			}
			ids <- card.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("Create failed under concurrency: %v", err)
	}
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id produced: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique ids, got %d", n, len(seen))
	}
}

func TestAuditSourceCheckPassesAndCompletesTask(t *testing.T) {
	svc, projectID := newTestService(t)
	root, err := svc.root(projectID)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	markerPath := filepath.Join(root, "marker.txt")
	if err := os.WriteFile(markerPath, []byte("hello sessionhub"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	card, err := svc.Create(projectID, CreateInput{Title: "Audit me", Type: "chore"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.SetStatus(projectID, card.ID, StatusBacklog); err != nil {
		t.Fatalf("SetStatus backlog: %v", err)
	}
	if _, err := svc.SetStatus(projectID, card.ID, StatusReady); err != nil {
		t.Fatalf("SetStatus ready: %v", err)
	}
	if _, err := svc.SetStatus(projectID, card.ID, StatusInProgress); err != nil {
		t.Fatalf("SetStatus in_progress: %v", err)
	}

	contract := "- source: marker.txt contains hello\n- validation: always-pass"
	if _, err := svc.Update(projectID, card.ID, Patch{Sections: map[string]string{"Audit Contract": contract}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	svc.SetRecipeRunner(fakeRecipeRunner{})

	report, err := svc.Audit(projectID, card.ID)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !report.ReproduciblePass {
		t.Fatalf("expected reproducible pass, got %+v", report)
	}
	final, err := svc.Get(projectID, card.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != StatusDone {
		t.Fatalf("expected task to auto-complete, got status %s", final.Status)
	}
}

type fakeRecipeRunner struct{}

func (fakeRecipeRunner) Run(projectID, recipeName string) (bool, string, error) {
	return true, "ok", nil
}
