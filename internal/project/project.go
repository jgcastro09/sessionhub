// Package project owns the portable .shproject contract and the local
// catalogue of projects known to this Session Hub installation.
package project

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jgcastro09/sessionhub/internal/atomicfile"
	"github.com/jgcastro09/sessionhub/internal/domain"
)

const (
	Directory     = ".shproject"
	ManifestFile  = "manifest.json"
	ExecutorsFile = "executors.json"
	SchemaVersion = 1
)

var ErrNotProject = errors.New(".shproject manifest was not found")

// Manifest is deliberately small and portable. It never includes local
// executors, credentials, output, or any other machine-specific state.
type Manifest struct {
	SchemaVersion int             `json:"schema_version"`
	ProjectID     string          `json:"project_id"`
	Name          string          `json:"name"`
	Root          string          `json:"root"`
	CreatedAt     time.Time       `json:"created_at"`
	Features      map[string]bool `json:"features,omitempty"`
}

type ExecutorSlot struct {
	Name         string   `json:"name"`
	Role         string   `json:"role,omitempty"`
	ContextRules []string `json:"context_rules,omitempty"`
}

type ExecutorSlots struct {
	Slots []ExecutorSlot `json:"slots"`
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported .shproject schema_version %d", m.SchemaVersion)
	}
	if !validUUID(m.ProjectID) {
		return fmt.Errorf("project_id must be a UUID")
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("project name is required")
	}
	if m.Root != "." {
		return fmt.Errorf("manifest root must be .")
	}
	if m.CreatedAt.IsZero() {
		return fmt.Errorf("project created_at is required")
	}
	return nil
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	v := hex.EncodeToString(b[:])
	return v[:8] + "-" + v[8:12] + "-" + v[12:16] + "-" + v[16:20] + "-" + v[20:], nil
}

// Init creates .shproject without touching user source files. Existing
// manifests are loaded rather than overwritten.
func Init(root, name string) (domain.Project, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return domain.Project{}, err
	}
	if found, err := Load(root); err == nil {
		return found, nil
	} else if !errors.Is(err, ErrNotProject) {
		return domain.Project{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(root)
	}
	projectID, err := newUUID()
	if err != nil {
		return domain.Project{}, fmt.Errorf("create project id: %w", err)
	}
	manifest := Manifest{SchemaVersion: SchemaVersion, ProjectID: projectID, Name: strings.TrimSpace(name), Root: ".", CreatedAt: time.Now().UTC(), Features: map[string]bool{}}
	if err := manifest.Validate(); err != nil {
		return domain.Project{}, err
	}
	base := filepath.Join(root, Directory)
	for _, dir := range []string{base, filepath.Join(base, "context", "decisions"), filepath.Join(base, "tasks", "cards"), filepath.Join(base, "registry", "records"), filepath.Join(base, "automation")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return domain.Project{}, fmt.Errorf("create project directory: %w", err)
		}
	}
	if err := atomicfile.WriteJSON(filepath.Join(base, ManifestFile), manifest); err != nil {
		return domain.Project{}, err
	}
	if err := atomicfile.WriteJSON(filepath.Join(base, ExecutorsFile), ExecutorSlots{Slots: []ExecutorSlot{}}); err != nil {
		return domain.Project{}, err
	}
	return domain.Project{ID: manifest.ProjectID, Name: manifest.Name, Root: root, CreatedAt: manifest.CreatedAt, UpdatedAt: manifest.CreatedAt}, nil
}

// Discover walks upward from start until it finds a project root.
func Discover(start string) (domain.Project, error) {
	path, err := cleanRoot(start)
	if err != nil {
		return domain.Project{}, err
	}
	for {
		project, err := Load(path)
		if err == nil {
			return project, nil
		}
		if !errors.Is(err, ErrNotProject) {
			return domain.Project{}, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return domain.Project{}, ErrNotProject
		}
		path = parent
	}
}

func Load(root string) (domain.Project, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return domain.Project{}, err
	}
	data, err := os.ReadFile(filepath.Join(root, Directory, ManifestFile))
	if errors.Is(err, os.ErrNotExist) {
		return domain.Project{}, ErrNotProject
	}
	if err != nil {
		return domain.Project{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return domain.Project{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return domain.Project{}, err
	}
	return domain.Project{ID: manifest.ProjectID, Name: manifest.Name, Root: root, CreatedAt: manifest.CreatedAt, UpdatedAt: manifest.CreatedAt}, nil
}

// ResolvePath only accepts a relative path that remains inside root after
// symlink evaluation. It is safe for project-owned config and context files.
func ResolvePath(root, relative string) (string, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("absolute project path is not allowed")
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project root")
	}
	target := filepath.Join(root, clean)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		// A new file has no symlink to resolve. Its parent must still be safe.
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(target))
		if parentErr != nil {
			return "", fmt.Errorf("resolve project path: %w", err)
		}
		resolvedTarget = filepath.Join(parent, filepath.Base(target))
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project root")
	}
	return target, nil
}

func cleanRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("project root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat project root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project root is not a directory")
	}
	return filepath.Clean(abs), nil
}
