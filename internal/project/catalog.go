package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

// Catalog is deliberately local: detaching a project only removes this
// index entry and can never delete files in the project root.
type Catalog struct {
	path string
	mu   sync.Mutex
}

func OpenCatalog(runtimeRoot string) (*Catalog, error) {
	if runtimeRoot == "" {
		return nil, fmt.Errorf("runtime root is required")
	}
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		return nil, err
	}
	return &Catalog{path: filepath.Join(runtimeRoot, "project-index.json")}, nil
}

func (c *Catalog) List() ([]domain.Project, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.load()
}

func (c *Catalog) Attach(root string) (domain.Project, error) {
	project, err := Load(root)
	if err != nil {
		return domain.Project{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	items, err := c.load()
	if err != nil {
		return domain.Project{}, err
	}
	for i := range items {
		if items[i].ID == project.ID {
			items[i] = project
			return project, c.save(items)
		}
	}
	items = append(items, project)
	return project, c.save(items)
}

func (c *Catalog) Detach(projectID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	items, err := c.load()
	if err != nil {
		return err
	}
	kept := items[:0]
	found := false
	for _, item := range items {
		if item.ID == projectID {
			found = true
			continue
		}
		kept = append(kept, item)
	}
	if !found {
		return fmt.Errorf("project %q is not attached", projectID)
	}
	return c.save(kept)
}

func (c *Catalog) load() ([]domain.Project, error) {
	data, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return []domain.Project{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []domain.Project
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("decode project catalog: %w", err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (c *Catalog) save(items []domain.Project) error { return writeJSON(c.path, items) }
