package webserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/app"
	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/project"
	"github.com/jgcastro09/sessionhub/internal/registry"
	"github.com/jgcastro09/sessionhub/internal/tasks"
	"github.com/jgcastro09/sessionhub/internal/webserver"
)

func newTestApp(t *testing.T) (*app.App, string) {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("SESSIONHUB_DATA_DIR", dataDir)
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	application, err := app.New(context.Background(), paths, "test")
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, "internal", "app"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "internal", "app", "app.go"),
		[]byte("package app\n\nfunc NewProject() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	proj, err := project.Init(projectRoot, "Test Project")
	if err != nil {
		t.Fatalf("project.Init: %v", err)
	}
	if _, err := application.Projects.Attach(projectRoot); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return application, proj.ID
}

func newTestServer(t *testing.T, application *app.App) string {
	t.Helper()
	server, err := webserver.Listen(context.Background(), "127.0.0.1:0", application, config.WebBindLocal)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return "http://" + server.Address()
}

func TestTasksAPIEndToEnd(t *testing.T) {
	application, projectID := newTestApp(t)
	base := newTestServer(t, application)

	createBody, _ := json.Marshal(tasks.CreateInput{Title: "Add tests", Type: "feature", Priority: tasks.PriorityHigh})
	resp, err := http.Post(base+"/api/v2/projects/"+projectID+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("POST tasks: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST tasks status: %d", resp.StatusCode)
	}
	var created tasks.Card
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created card: %v", err)
	}
	if created.ID != "TASK-0001" {
		t.Fatalf("unexpected id: %s", created.ID)
	}

	listResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/tasks")
	if err != nil {
		t.Fatalf("GET tasks: %v", err)
	}
	defer listResp.Body.Close()
	var cards []tasks.Card
	if err := json.NewDecoder(listResp.Body).Decode(&cards); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}

	patchBody, _ := json.Marshal(map[string]string{"status": "backlog"})
	req, _ := http.NewRequest(http.MethodPatch, base+"/api/v2/projects/"+projectID+"/tasks/TASK-0001", bytes.NewReader(patchBody))
	patchResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH tasks: %v", err)
	}
	defer patchResp.Body.Close()
	var patched tasks.Card
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patched card: %v", err)
	}
	if patched.Status != tasks.StatusBacklog {
		t.Fatalf("expected backlog, got %s", patched.Status)
	}

	auditResp, err := http.Post(base+"/api/v2/projects/"+projectID+"/tasks/TASK-0001/audit", "application/json", nil)
	if err != nil {
		t.Fatalf("POST audit: %v", err)
	}
	defer auditResp.Body.Close()
	if auditResp.StatusCode != http.StatusOK {
		t.Fatalf("audit status: %d", auditResp.StatusCode)
	}

	missingResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/tasks/TASK-9999")
	if err != nil {
		t.Fatalf("GET missing: %v", err)
	}
	defer missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing task, got %d", missingResp.StatusCode)
	}
}

func TestRegistryAPIEndToEnd(t *testing.T) {
	application, projectID := newTestApp(t)
	base := newTestServer(t, application)

	scanResp, err := http.Post(base+"/api/v2/projects/"+projectID+"/registry/scan", "application/json", nil)
	if err != nil {
		t.Fatalf("POST scan: %v", err)
	}
	defer scanResp.Body.Close()
	var entries []registry.Entry
	if err := json.NewDecoder(scanResp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected at least one entry from scan")
	}

	healthResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/registry/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	defer healthResp.Body.Close()
	var report registry.CoverageReport
	if err := json.NewDecoder(healthResp.Body).Decode(&report); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if !report.OK() {
		t.Fatalf("expected clean coverage right after scan, got %+v", report)
	}

	searchResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/registry/search?query=NewProject")
	if err != nil {
		t.Fatalf("GET search: %v", err)
	}
	defer searchResp.Body.Close()
	var results []registry.SearchResult
	if err := json.NewDecoder(searchResp.Body).Decode(&results); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(results) != 1 || results[0].Entry.Path != "internal/app/app.go" {
		t.Fatalf("unexpected search results: %+v", results)
	}

	contextResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/registry/context?entry_id=" + results[0].Entry.EntryID)
	if err != nil {
		t.Fatalf("GET context: %v", err)
	}
	defer contextResp.Body.Close()
	if contextResp.StatusCode != http.StatusOK {
		t.Fatalf("context status: %d", contextResp.StatusCode)
	}
}
