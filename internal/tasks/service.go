package tasks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jgcastro09/sessionhub/internal/atomicfile"
	"github.com/jgcastro09/sessionhub/internal/events"
	"github.com/jgcastro09/sessionhub/internal/project"
)

// ErrNotFound is returned when a task ID does not exist under a project.
var ErrNotFound = errors.New("task not found")

// CardsDir returns the .shproject-relative directory cards live under.
func cardsDir(root string) string {
	return filepath.Join(root, project.Directory, "tasks", "cards")
}

// Service is the internal/tasks entry point: CRUD, workflow validation, and
// the Audit Contract runner, all reading and writing Markdown cards under
// <project>/.shproject/tasks/cards. It holds no project-specific state
// beyond an in-process write lock per project ID — every other read
// resolves the project root fresh from the catalog on each call, so it
// tolerates the catalog changing (attach/detach) between calls.
type Service struct {
	catalog     *project.Catalog
	bus         *events.Bus
	runtimeRoot string

	registry RegistryChecker
	recipes  RecipeRunner

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New builds a Task Manager service. bus may be nil in tests that do not
// care about event delivery. runtimeRoot is the local runtime root
// (config.Paths.Projects) that per-Executor task claims are written under —
// never under .shproject (plan 4.3).
func New(catalog *project.Catalog, bus *events.Bus, runtimeRoot string) *Service {
	return &Service{catalog: catalog, bus: bus, runtimeRoot: runtimeRoot, locks: map[string]*sync.Mutex{}}
}

// SetRegistryChecker wires in the Code Registry lookup used by "registry:"
// Audit Contract checks. Safe to call once at startup, before any request
// concurrency begins.
func (s *Service) SetRegistryChecker(r RegistryChecker) { s.registry = r }

// SetRecipeRunner wires in the declared validation-recipe executor used by
// "validation:" Audit Contract checks.
func (s *Service) SetRecipeRunner(r RecipeRunner) { s.recipes = r }

func (s *Service) lockFor(projectID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[projectID]
	if !ok {
		l = &sync.Mutex{}
		s.locks[projectID] = l
	}
	return l
}

func (s *Service) root(projectID string) (string, error) {
	p, err := s.catalog.Get(projectID)
	if err != nil {
		return "", err
	}
	return p.Root, nil
}

func (s *Service) publish(projectID, kind string, payload any) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(projectID, kind, payload)
}

// Filter narrows List/Search results. Zero-value Filter matches everything.
type Filter struct {
	Status       []Status
	Priority     []Priority
	ImpactedArea string
	RegistryRef  string
	Query        string
}

func (f Filter) matches(c Card) bool {
	if len(f.Status) > 0 && !containsStatus(f.Status, c.Status) {
		return false
	}
	if len(f.Priority) > 0 && !containsPriority(f.Priority, c.Priority) {
		return false
	}
	if f.ImpactedArea != "" && !containsFold(c.ImpactedAreas, f.ImpactedArea) {
		return false
	}
	if f.RegistryRef != "" && !containsFold(c.RegistryRefs, f.RegistryRef) {
		return false
	}
	if f.Query != "" && !cardMatchesQuery(c, f.Query) {
		return false
	}
	return true
}

func containsStatus(list []Status, s Status) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func containsPriority(list []Priority, p Priority) bool {
	for _, v := range list {
		if v == p {
			return true
		}
	}
	return false
}

func containsFold(list []string, needle string) bool {
	for _, v := range list {
		if strings.EqualFold(v, needle) {
			return true
		}
	}
	return false
}

func cardMatchesQuery(c Card, query string) bool {
	query = strings.ToLower(query)
	if strings.Contains(strings.ToLower(c.ID), query) || strings.Contains(strings.ToLower(c.Title), query) {
		return true
	}
	for _, s := range c.Sections {
		if strings.Contains(strings.ToLower(s.Body), query) {
			return true
		}
	}
	return false
}

// List returns every card in a project matching filter, sorted by ID.
func (s *Service) List(projectID string, filter Filter) ([]Card, error) {
	root, err := s.root(projectID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(cardsDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return []Card{}, nil
		}
		return nil, err
	}
	cards := make([]Card, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cardsDir(root), entry.Name()))
		if err != nil {
			return nil, err
		}
		card, err := ParseCard(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		if filter.matches(card) {
			cards = append(cards, card)
		}
	}
	SortByID(cards)
	return cards, nil
}

// Search is List with only a free-text query — kept as a distinct method so
// the CLI/TUI/API surfaces described in section 6 can expose a plain "search
// this text" affordance without constructing a Filter.
func (s *Service) Search(projectID, query string) ([]Card, error) {
	return s.List(projectID, Filter{Query: query})
}

func (s *Service) cardPath(root, taskID string) string {
	return filepath.Join(cardsDir(root), taskID+".md")
}

// Get reads a single card by ID.
func (s *Service) Get(projectID, taskID string) (Card, error) {
	root, err := s.root(projectID)
	if err != nil {
		return Card{}, err
	}
	return s.readCard(root, taskID)
}

func (s *Service) readCard(root, taskID string) (Card, error) {
	path, err := project.ResolvePath(root, filepath.Join(project.Directory, "tasks", "cards", taskID+".md"))
	if err != nil {
		return Card{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Card{}, ErrNotFound
	}
	if err != nil {
		return Card{}, err
	}
	return ParseCard(data)
}

func (s *Service) writeCard(root string, card Card) error {
	path, err := project.ResolvePath(root, filepath.Join(project.Directory, "tasks", "cards", card.ID+".md"))
	if err != nil {
		return err
	}
	return atomicfile.Write(path, card.Marshal(), 0o644)
}

// CreateInput is the user-supplied portion of a new card.
type CreateInput struct {
	Title         string   `json:"title"`
	Type          Type     `json:"type"`
	Priority      Priority `json:"priority"`
	ImpactedAreas []string `json:"impacted_areas"`
	RegistryRefs  []string `json:"registry_refs"`
	Dependencies  []string `json:"dependencies"`
}

var idPattern = "TASK-%04d"

func nextTaskID(root string) (string, error) {
	entries, err := os.ReadDir(cardsDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf(idPattern, 1), nil
		}
		return "", err
	}
	max := 0
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".md")
		var n int
		if _, err := fmt.Sscanf(name, "TASK-%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf(idPattern, max+1), nil
}

// Create allocates the next sequential ID for the project and writes the new
// card. It refuses to overwrite an existing file, retrying with the next ID
// if a collision is detected (guards against a second sessionhub process
// racing the same project).
func (s *Service) Create(projectID string, input CreateInput) (Card, error) {
	if strings.TrimSpace(input.Title) == "" {
		return Card{}, fmt.Errorf("task title is required")
	}
	if strings.TrimSpace(string(input.Type)) == "" {
		return Card{}, fmt.Errorf("task type is required")
	}
	if input.Priority == "" {
		input.Priority = PriorityMedium
	}
	if !input.Priority.valid() {
		return Card{}, fmt.Errorf("unknown task priority %q", input.Priority)
	}

	root, err := s.root(projectID)
	if err != nil {
		return Card{}, err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()

	if err := ensureWorkflowFile(root); err != nil {
		return Card{}, err
	}

	for attempt := 0; attempt < 1000; attempt++ {
		id, err := nextTaskID(root)
		if err != nil {
			return Card{}, err
		}
		now := time.Now().UTC()
		card := NewCard(id, input.Title, input.Type, input.Priority, now)
		card.ImpactedAreas = normalizeStringList(input.ImpactedAreas)
		card.RegistryRefs = normalizeStringList(input.RegistryRefs)
		card.Dependencies = normalizeStringList(input.Dependencies)
		if err := card.Validate(); err != nil {
			return Card{}, err
		}
		path, err := project.ResolvePath(root, filepath.Join(project.Directory, "tasks", "cards", card.ID+".md"))
		if err != nil {
			return Card{}, err
		}
		if err := atomicfile.CreateExclusive(path, card.Marshal(), 0o644); err != nil {
			if errors.Is(err, atomicfile.ErrExists) {
				continue
			}
			return Card{}, err
		}
		s.publish(projectID, events.KindTaskCreated, map[string]string{"task_id": card.ID})
		return card, nil
	}
	return Card{}, fmt.Errorf("could not allocate a unique task id")
}

// Patch is a partial update: nil/empty fields leave the current value
// unchanged, except the list fields, which always replace (a caller wanting
// to append must read-modify-write).
type Patch struct {
	Title         *string           `json:"title,omitempty"`
	Type          *Type             `json:"type,omitempty"`
	Priority      *Priority         `json:"priority,omitempty"`
	ImpactedAreas []string          `json:"impacted_areas,omitempty"`
	RegistryRefs  []string          `json:"registry_refs,omitempty"`
	Dependencies  []string          `json:"dependencies,omitempty"`
	Sections      map[string]string `json:"sections,omitempty"` // heading -> new body
}

// IsEmpty reports whether a Patch would change nothing, so callers (like the
// Web API's combined "patch + optional status change" handler) can skip a
// redundant write.
func (p Patch) IsEmpty() bool {
	return p.Title == nil && p.Type == nil && p.Priority == nil &&
		p.ImpactedAreas == nil && p.RegistryRefs == nil && p.Dependencies == nil && len(p.Sections) == 0
}

// Update applies a patch to an existing card.
func (s *Service) Update(projectID, taskID string, patch Patch) (Card, error) {
	root, err := s.root(projectID)
	if err != nil {
		return Card{}, err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()

	card, err := s.readCard(root, taskID)
	if err != nil {
		return Card{}, err
	}
	if patch.Title != nil {
		card.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.Type != nil {
		card.Type = *patch.Type
	}
	if patch.Priority != nil {
		if !patch.Priority.valid() {
			return Card{}, fmt.Errorf("unknown task priority %q", *patch.Priority)
		}
		card.Priority = *patch.Priority
	}
	if patch.ImpactedAreas != nil {
		card.ImpactedAreas = normalizeStringList(patch.ImpactedAreas)
	}
	if patch.RegistryRefs != nil {
		card.RegistryRefs = normalizeStringList(patch.RegistryRefs)
	}
	if patch.Dependencies != nil {
		card.Dependencies = normalizeStringList(patch.Dependencies)
	}
	for heading, body := range patch.Sections {
		card.setSection(heading, body)
	}
	card.UpdatedAt = time.Now().UTC()
	if err := card.Validate(); err != nil {
		return Card{}, err
	}
	if err := s.writeCard(root, card); err != nil {
		return Card{}, err
	}
	s.publish(projectID, events.KindTaskUpdated, map[string]string{"task_id": card.ID})
	return card, nil
}

// SetStatus validates and applies a workflow transition.
func (s *Service) SetStatus(projectID, taskID string, status Status) (Card, error) {
	root, err := s.root(projectID)
	if err != nil {
		return Card{}, err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()

	card, err := s.readCard(root, taskID)
	if err != nil {
		return Card{}, err
	}
	if err := ValidateTransition(card.Status, status); err != nil {
		return Card{}, err
	}
	card.Status = status
	card.UpdatedAt = time.Now().UTC()
	if err := s.writeCard(root, card); err != nil {
		return Card{}, err
	}
	s.publish(projectID, events.KindTaskStatus, map[string]string{"task_id": card.ID, "status": string(status)})
	return card, nil
}

// Audit runs the card's declared Audit Contract, persists the "Audit
// Report" section, and applies the two deterministic transitions the plan
// allows outside manual action: an in-progress/changes_requested task with
// full reproducible evidence moves to done, and a done task that loses its
// evidence moves back to changes_requested. Any other combination leaves
// status untouched — completion stays manual by default.
func (s *Service) Audit(projectID, taskID string) (AuditReport, error) {
	root, err := s.root(projectID)
	if err != nil {
		return AuditReport{}, err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()

	card, err := s.readCard(root, taskID)
	if err != nil {
		return AuditReport{}, err
	}
	checks := ParseAuditContract(card.section("Audit Contract"))
	report := s.RunAudit(projectID, root, checks)
	card.setSection("Audit Report", RenderAuditReport(report))

	switch {
	case report.ReproduciblePass && (card.Status == StatusInProgress || card.Status == StatusChangesRequested):
		if ValidateTransition(card.Status, StatusDone) == nil {
			card.Status = StatusDone
		}
	case !report.ReproduciblePass && card.Status == StatusDone:
		if ValidateTransition(card.Status, StatusChangesRequested) == nil {
			card.Status = StatusChangesRequested
		}
	}
	card.UpdatedAt = time.Now().UTC()
	if err := s.writeCard(root, card); err != nil {
		return AuditReport{}, err
	}
	s.publish(projectID, events.KindTaskAudited, map[string]any{
		"task_id": card.ID, "status": string(card.Status), "passed": report.ReproduciblePass,
	})
	return report, nil
}

// Numeric returns the integer suffix of a TASK-XXXX id, or 0 if it does not
// match the standard pattern. Exposed for CLI/TUI sort helpers.
func Numeric(taskID string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(taskID, "TASK-"))
	if err != nil {
		return 0
	}
	return n
}
