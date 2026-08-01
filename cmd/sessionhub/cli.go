package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jgcastro09/sessionhub/internal/automation"
	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/embedding"
	"github.com/jgcastro09/sessionhub/internal/events"
	"github.com/jgcastro09/sessionhub/internal/project"
	"github.com/jgcastro09/sessionhub/internal/registry"
	"github.com/jgcastro09/sessionhub/internal/tasks"
)

// runCLI implements the scriptable surface described in plan section 6.1:
// "sessionhub tasks ..." and "sessionhub registry ...". It runs entirely
// in-process and exits — no UI, no additional listener — resolving the
// current Project the same way git resolves a repo: by walking up from the
// working directory to find .shproject.
func runCLI(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected a subcommand (tasks, registry)")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	proj, err := project.Discover(cwd)
	if err != nil {
		return fmt.Errorf("no .shproject found above %s: %w", cwd, err)
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	catalog, err := project.OpenCatalog(paths.Projects)
	if err != nil {
		return err
	}
	if _, err := catalog.Attach(proj.Root); err != nil {
		return err
	}

	bus := events.NewBus()
	taskSvc := tasks.New(catalog, bus, paths.Projects)
	registrySvc := registry.New(catalog, bus)
	taskSvc.SetRegistryChecker(registrySvc)
	taskSvc.SetRecipeRunner(automation.NewRecipeRunner(catalog))
	embeddingManager := embedding.NewManager(paths.Tools)
	defer embeddingManager.Close()
	registrySvc.SetEmbedder(cliEmbedder{embeddingManager})

	switch args[0] {
	case "tasks":
		return runTasksCLI(taskSvc, proj.ID, args[1:])
	case "registry":
		return runRegistryCLI(registrySvc, proj.ID, args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q (expected tasks or registry)", args[0])
	}
}

// cliEmbedder adapts *embedding.Manager to registry.Embedder for the
// one-shot CLI process — see app.embeddingEmbedder for the long-lived TUI
// equivalent. Ensure is cheap once the engine is already running.
type cliEmbedder struct{ manager *embedding.Manager }

func (e cliEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := e.manager.Ensure(ctx); err != nil {
		return nil, err
	}
	return e.manager.Embed(ctx, text)
}

func printJSON(value any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

// ---- tasks ----

func runTasksCLI(svc *tasks.Service, projectID string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected a tasks subcommand (list, create, show, status, search, audit)")
	}
	switch args[0] {
	case "list":
		return tasksList(svc, projectID, args[1:])
	case "create":
		return tasksCreate(svc, projectID, args[1:])
	case "show":
		return tasksShow(svc, projectID, args[1:])
	case "status":
		return tasksStatus(svc, projectID, args[1:])
	case "search":
		return tasksSearch(svc, projectID, args[1:])
	case "audit":
		return tasksAudit(svc, projectID, args[1:])
	default:
		return fmt.Errorf("unknown tasks subcommand %q", args[0])
	}
}

func tasksList(svc *tasks.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("tasks list", flag.ContinueOnError)
	status := fs.String("status", "", "filter by status (comma-separated)")
	priority := fs.String("priority", "", "filter by priority (comma-separated)")
	area := fs.String("area", "", "filter by impacted area")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	filter := tasks.Filter{ImpactedArea: *area}
	for _, s := range splitCSV(*status) {
		filter.Status = append(filter.Status, tasks.Status(s))
	}
	for _, p := range splitCSV(*priority) {
		filter.Priority = append(filter.Priority, tasks.Priority(p))
	}
	cards, err := svc.List(projectID, filter)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(cards)
	}
	for _, c := range cards {
		fmt.Printf("%s\t%-18s\t%-8s\t%s\n", c.ID, c.Status, c.Priority, c.Title)
	}
	return nil
}

func tasksCreate(svc *tasks.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("tasks create", flag.ContinueOnError)
	title := fs.String("title", "", "task title (required)")
	taskType := fs.String("type", "", "task type (required, e.g. feature, bug, chore)")
	priority := fs.String("priority", "medium", "task priority")
	areas := fs.String("areas", "", "comma-separated impacted areas")
	refs := fs.String("registry-refs", "", "comma-separated registry references")
	deps := fs.String("dependencies", "", "comma-separated task id dependencies")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" || strings.TrimSpace(*taskType) == "" {
		return fmt.Errorf("--title and --type are required")
	}
	card, err := svc.Create(projectID, tasks.CreateInput{
		Title: *title, Type: tasks.Type(*taskType), Priority: tasks.Priority(*priority),
		ImpactedAreas: splitCSV(*areas), RegistryRefs: splitCSV(*refs), Dependencies: splitCSV(*deps),
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(card)
	}
	fmt.Printf("created %s: %s\n", card.ID, card.Title)
	return nil
}

func tasksShow(svc *tasks.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("tasks show", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sessionhub tasks show <TASK-ID>")
	}
	card, err := svc.Get(projectID, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(card)
	}
	os.Stdout.Write(card.Marshal())
	return nil
}

func tasksStatus(svc *tasks.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("tasks status", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: sessionhub tasks status <TASK-ID> <new-status>")
	}
	card, err := svc.SetStatus(projectID, fs.Arg(0), tasks.Status(fs.Arg(1)))
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(card)
	}
	fmt.Printf("%s -> %s\n", card.ID, card.Status)
	return nil
}

func tasksSearch(svc *tasks.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("tasks search", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sessionhub tasks search <query>")
	}
	cards, err := svc.Search(projectID, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(cards)
	}
	for _, c := range cards {
		fmt.Printf("%s\t%-18s\t%s\n", c.ID, c.Status, c.Title)
	}
	return nil
}

func tasksAudit(svc *tasks.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("tasks audit", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sessionhub tasks audit <TASK-ID>")
	}
	report, err := svc.Audit(projectID, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(report)
	}
	fmt.Print(tasks.RenderAuditReport(report))
	return nil
}

// ---- registry ----

func runRegistryCLI(svc *registry.Service, projectID string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected a registry subcommand (scan, health, search, graph, review)")
	}
	switch args[0] {
	case "scan", "build":
		return registryScan(svc, projectID, args[1:])
	case "health", "validate":
		return registryHealth(svc, projectID, args[1:])
	case "search":
		return registrySearch(svc, projectID, args[1:])
	case "graph":
		return registryGraph(svc, projectID, args[1:])
	case "review":
		return registryReview(svc, projectID, args[1:])
	case "git":
		return registryGit(svc, projectID, args[1:])
	default:
		return fmt.Errorf("unknown registry subcommand %q", args[0])
	}
}

func registryGit(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry git", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	correlation, err := svc.GitStatus(context.Background(), projectID)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(correlation)
	}
	if correlation.Git.Repository == "" {
		fmt.Println("not a Git repository")
		return nil
	}
	fmt.Printf("branch: %s\nupstream: %s\nahead: %d  behind: %d\nclean: %v  conflicted: %v\n",
		correlation.Git.Branch, correlation.Git.Upstream, correlation.Git.Ahead, correlation.Git.Behind,
		correlation.Git.Clean, correlation.Git.Conflicted)
	for _, id := range correlation.ChangedEntries {
		fmt.Printf("changed: %s\n", id)
	}
	return nil
}

func registryScan(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry scan", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	full := fs.Bool("full", false, "bypass the fingerprint cache and re-read/re-hash every eligible file (audit)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var entries []registry.Entry
	var err error
	if *full {
		entries, err = svc.ScanFull(projectID)
	} else {
		entries, err = svc.Scan(projectID)
	}
	if err != nil {
		return err
	}
	metrics, metricsErr := svc.ScanMetrics(projectID)
	if *jsonOut {
		if metricsErr != nil {
			return printJSON(struct {
				Entries []registry.Entry `json:"entries"`
			}{Entries: entries})
		}
		return printJSON(struct {
			Entries []registry.Entry     `json:"entries"`
			Metrics registry.ScanMetrics `json:"metrics"`
		}{Entries: entries, Metrics: metrics})
	}
	active := 0
	for _, e := range entries {
		if e.Status == registry.StatusActive {
			active++
		}
	}
	fmt.Printf("scanned: %d entries (%d active, %d missing)\n", len(entries), active, len(entries)-active)
	if metricsErr == nil {
		fmt.Printf("  %d seen, %d reused, %d reanalyzed, %d bytes read, %dms%s\n",
			metrics.FilesSeen, metrics.FilesReused, metrics.FilesReanalyzed, metrics.BytesRead, metrics.DurationMS,
			map[bool]string{true: " (full)"}[metrics.Full])
	}
	return nil
}

func registryHealth(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry health", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report, err := svc.Health(projectID)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(report)
	}
	if report.Healthy {
		fmt.Println("registry healthy")
		return nil
	}
	for _, p := range report.MissingPaths {
		fmt.Printf("missing entry: %s\n", p)
	}
	for _, id := range report.StaleReviews {
		fmt.Printf("stale (needs review): %s\n", id)
	}
	for _, id := range report.ClassificationIssues {
		fmt.Printf("unclassified: %s\n", id)
	}
	for _, id := range report.DanglingRelationships {
		fmt.Printf("dangling relationship: %s\n", id)
	}
	for _, issue := range report.DependencyIssues {
		fmt.Printf("%s dependency %q: %s\n", issue.Path, issue.RawImport, issue.Reason)
	}
	if report.PendingClassificationCount > 0 {
		fmt.Printf("pending classification: %d files\n", report.PendingClassificationCount)
	}
	return fmt.Errorf("registry not healthy: %d missing, %d stale, %d unclassified, %d dangling, %d dependency issues, %d pending",
		len(report.MissingPaths), len(report.StaleReviews), len(report.ClassificationIssues),
		len(report.DanglingRelationships), len(report.DependencyIssues), report.PendingClassificationCount)
}

func registrySearch(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry search", flag.ContinueOnError)
	limit := fs.Int("limit", 20, "max results")
	semantic := fs.Bool("semantic", false, "include semantic ranking alongside full-text")
	category := fs.String("category", "", "filter by category")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sessionhub registry search <query>")
	}
	q := registry.SearchQuery{Text: fs.Arg(0), Limit: *limit, Semantic: *semantic}
	if *category != "" {
		q.Categories = []string{*category}
	}
	results, _, err := svc.Search(projectID, q)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(results)
	}
	for _, r := range results {
		fmt.Printf("%d\t%s\t%s\n", r.Score, r.Entry.EntryID, r.Entry.Path)
	}
	return nil
}

func registryGraph(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry graph", flag.ContinueOnError)
	depth := fs.Int("depth", 2, "traversal depth")
	limit := fs.Int("limit", 150, "max nodes")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sessionhub registry graph <entry-id>")
	}
	entry, err := svc.Get(projectID, fs.Arg(0))
	if err != nil {
		return err
	}
	g, err := svc.EntryGraph(projectID, fs.Arg(0), *depth, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(g)
	}
	fmt.Printf("%s (%s)\n%s\n\nrelated (%d nodes, %d edges):\n", entry.EntryID, entry.Path, entry.Description, len(g.Nodes), len(g.Edges))
	for _, e := range g.Edges {
		if e.From == entry.EntryID || e.To == entry.EntryID {
			fmt.Printf("  %s %s -> %s (%s)\n", e.Kind, e.From, e.To, e.Confidence)
		}
	}
	return nil
}

func registryReview(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry review", flag.ContinueOnError)
	description := fs.String("description", "", "human description")
	criticality := fs.String("criticality", "", "standard, important, or critical")
	responsibilities := fs.String("responsibilities", "", "comma-separated responsibilities")
	relatedFiles := fs.String("related-files", "", "comma-separated related entry_ids")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sessionhub registry review <entry-id> [flags]")
	}
	entry, err := svc.Review(projectID, fs.Arg(0), registry.ReviewInput{
		Description:      *description,
		Criticality:      registry.Criticality(*criticality),
		Responsibilities: splitCSV(*responsibilities),
		RelatedFiles:     splitCSV(*relatedFiles),
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(entry)
	}
	fmt.Printf("reviewed %s\n", entry.EntryID)
	return nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
