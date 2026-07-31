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
		return fmt.Errorf("expected a registry subcommand (scan, build, validate, search, context, review)")
	}
	switch args[0] {
	case "scan", "build":
		return registryScan(svc, projectID, args[1:])
	case "validate":
		return registryValidate(svc, projectID, args[1:])
	case "search":
		return registrySearch(svc, projectID, args[1:])
	case "context":
		return registryContext(svc, projectID, args[1:])
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	entries, err := svc.Scan(projectID)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(registry.RedactedEntries(entries))
	}
	active := 0
	for _, e := range entries {
		if e.Status == registry.StatusActive {
			active++
		}
	}
	fmt.Printf("scanned: %d entries (%d active, %d missing)\n", len(entries), active, len(entries)-active)
	return nil
}

func registryValidate(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry validate", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report, err := svc.Validate(projectID)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(report)
	}
	if report.OK() {
		fmt.Println("coverage OK")
		return nil
	}
	for _, p := range report.MissingPaths {
		fmt.Printf("missing entry: %s\n", p)
	}
	for _, p := range report.StaleHashes {
		fmt.Printf("stale (needs review): %s\n", p)
	}
	return fmt.Errorf("coverage incomplete: %d missing, %d stale", len(report.MissingPaths), len(report.StaleHashes))
}

func registrySearch(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry search", flag.ContinueOnError)
	limit := fs.Int("limit", 20, "max results")
	semantic := fs.Bool("semantic", false, "use semantic search instead of lexical")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sessionhub registry search <query>")
	}
	var results []registry.SearchResult
	var err error
	if *semantic {
		results, err = svc.SemanticSearch(context.Background(), projectID, fs.Arg(0), *limit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "semantic search unavailable (%v), falling back to lexical\n", err)
			results, err = svc.Search(projectID, fs.Arg(0), *limit)
		}
	} else {
		results, err = svc.Search(projectID, fs.Arg(0), *limit)
	}
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(redactCLIResults(results))
	}
	for _, r := range results {
		fmt.Printf("%d\t%s\t%s\n", r.Score, r.Entry.EntryID, r.Entry.Path)
	}
	return nil
}

func redactCLIResults(results []registry.SearchResult) []registry.SearchResult {
	out := make([]registry.SearchResult, len(results))
	for i, r := range results {
		out[i] = registry.SearchResult{Entry: r.Entry.Redacted(), Score: r.Score}
	}
	return out
}

func registryContext(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry context", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sessionhub registry context <entry-id>")
	}
	entry, err := svc.Get(projectID, fs.Arg(0))
	if err != nil {
		return err
	}
	related, err := svc.Search(projectID, entry.Module+" "+strings.Join(entry.Symbols, " "), 10)
	if err != nil {
		return err
	}
	pack := registry.ContextPack{Entry: entry.Redacted(), Related: redactCLIResults(registry.SearchResults(related).Filter(entry.EntryID))}
	if *jsonOut {
		return printJSON(pack)
	}
	fmt.Printf("%s (%s)\n%s\n\nrelated:\n", entry.EntryID, entry.Path, entry.Description)
	for _, r := range registry.SearchResults(related).Filter(entry.EntryID) {
		fmt.Printf("  %s\t%s\n", r.Entry.EntryID, r.Entry.Path)
	}
	return nil
}

func registryReview(svc *registry.Service, projectID string, args []string) error {
	fs := flag.NewFlagSet("registry review", flag.ContinueOnError)
	module := fs.String("module", "", "module name")
	description := fs.String("description", "", "human description")
	criticality := fs.String("criticality", "", "low, medium, or high")
	responsibilities := fs.String("responsibilities", "", "comma-separated responsibilities")
	relationsConfirmed := fs.String("relations-confirmed", "", "comma-separated confirmed relations")
	relationsProbable := fs.String("relations-probable", "", "comma-separated probable relations")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sessionhub registry review <entry-id> [flags]")
	}
	entry, err := svc.Review(projectID, fs.Arg(0), registry.ReviewInput{
		Module: *module, Description: *description, Criticality: *criticality,
		Responsibilities:   splitCSV(*responsibilities),
		RelationsConfirmed: splitCSV(*relationsConfirmed),
		RelationsProbable:  splitCSV(*relationsProbable),
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(entry.Redacted())
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
