package webserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/registry"
)

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
	entryID := entries[0].EntryID

	healthResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/registry/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	defer healthResp.Body.Close()
	var report registry.HealthReport
	if err := json.NewDecoder(healthResp.Body).Decode(&report); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if len(report.MissingPaths) != 0 {
		t.Fatalf("expected clean coverage right after scan, got %+v", report.MissingPaths)
	}
	// Nothing has been reviewed yet — the fixture's one entry should be
	// showing up in StaleReviews (never-reviewed) and in the review queue.
	if len(report.StaleReviews) != 1 {
		t.Fatalf("expected the unreviewed entry to be flagged stale: %+v", report)
	}

	searchResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/registry/search?query=NewProject")
	if err != nil {
		t.Fatalf("GET search: %v", err)
	}
	defer searchResp.Body.Close()
	var page struct {
		Results []registry.SearchResult `json:"results"`
		Total   int                     `json:"total"`
	}
	if err := json.NewDecoder(searchResp.Body).Decode(&page); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if page.Total != 1 || page.Results[0].Entry.Path != "internal/app/app.go" {
		t.Fatalf("unexpected search results: %+v", page)
	}

	reviewQueueResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/registry/review-queue")
	if err != nil {
		t.Fatalf("GET review-queue: %v", err)
	}
	defer reviewQueueResp.Body.Close()
	var queuePage struct {
		Results []registry.SearchResult `json:"results"`
		Total   int                     `json:"total"`
	}
	if err := json.NewDecoder(reviewQueueResp.Body).Decode(&queuePage); err != nil {
		t.Fatalf("decode review-queue: %v", err)
	}
	if queuePage.Total != 1 {
		t.Fatalf("expected 1 entry in the review queue, got %+v", queuePage)
	}

	graphResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/registry/entries/" + entryID + "/graph")
	if err != nil {
		t.Fatalf("GET entry graph: %v", err)
	}
	defer graphResp.Body.Close()
	if graphResp.StatusCode != http.StatusOK {
		t.Fatalf("entry graph status: %d", graphResp.StatusCode)
	}

	reviewBody, _ := json.Marshal(registry.ReviewInput{
		Description: "entrypoint", Criticality: registry.CriticalityStandard, Responsibilities: []string{"boots the app"},
	})
	reviewResp, err := http.Post(base+"/api/v2/projects/"+projectID+"/registry/entries/"+entryID+"/review", "application/json", bytes.NewReader(reviewBody))
	if err != nil {
		t.Fatalf("POST review: %v", err)
	}
	defer reviewResp.Body.Close()
	if reviewResp.StatusCode != http.StatusOK {
		t.Fatalf("review status: %d", reviewResp.StatusCode)
	}
	var reviewed registry.Entry
	if err := json.NewDecoder(reviewResp.Body).Decode(&reviewed); err != nil {
		t.Fatalf("decode reviewed entry: %v", err)
	}
	if reviewed.ReviewStatus != registry.ReviewReviewed {
		t.Fatalf("expected the entry to be marked reviewed, got %+v", reviewed)
	}
}

func TestRegistryConfigPutRejectsMissingJustification(t *testing.T) {
	application, projectID := newTestApp(t)
	base := newTestServer(t, application)

	getResp, err := http.Get(base + "/api/v2/projects/" + projectID + "/registry/config")
	if err != nil {
		t.Fatalf("GET config: %v", err)
	}
	defer getResp.Body.Close()
	var cfg registry.Config
	if err := json.NewDecoder(getResp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	cfg.Eligibility.ExtensionReasons[".xyz"] = "" // no justification

	body, _ := json.Marshal(cfg)
	req, _ := http.NewRequest(http.MethodPut, base+"/api/v2/projects/"+projectID+"/registry/config", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unjustified rule, got %d", resp.StatusCode)
	}
}

func TestRegistryEntriesListRespectsFilters(t *testing.T) {
	application, projectID := newTestApp(t)
	base := newTestServer(t, application)

	if _, err := http.Post(base+"/api/v2/projects/"+projectID+"/registry/scan", "application/json", nil); err != nil {
		t.Fatalf("POST scan: %v", err)
	}

	resp, err := http.Get(base + "/api/v2/projects/" + projectID + "/registry/entries?category=go&limit=1")
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	defer resp.Body.Close()
	var page struct {
		Results []registry.SearchResult `json:"results"`
		Total   int                     `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode entries: %v", err)
	}
	if page.Total != 1 || len(page.Results) != 1 || page.Results[0].Entry.Category != "go" {
		t.Fatalf("unexpected filtered entries: %+v", page)
	}
}
