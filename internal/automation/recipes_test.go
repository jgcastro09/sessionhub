package automation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/project"
)

func newTestCatalog(t *testing.T) (*project.Catalog, string, string) {
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
	return catalog, proj.ID, root
}

func writeRecipes(t *testing.T, root string, set RecipeSet) {
	t.Helper()
	data, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal recipes: %v", err)
	}
	path := recipesPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write recipes: %v", err)
	}
}

func TestRecipeRunnerRunsDeclaredRecipe(t *testing.T) {
	catalog, projectID, root := newTestCatalog(t)
	writeRecipes(t, root, RecipeSet{Recipes: map[string]CommandConfig{
		"always-pass": {Command: "true"},
		"always-fail": {Command: "false"},
	}})
	runner := NewRecipeRunner(catalog)

	passed, detail, err := runner.Run(projectID, "always-pass")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !passed {
		t.Fatalf("expected always-pass to pass, detail=%s", detail)
	}

	passed, _, err = runner.Run(projectID, "always-fail")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if passed {
		t.Fatalf("expected always-fail to fail")
	}
}

func TestRecipeRunnerRejectsUndeclaredRecipe(t *testing.T) {
	catalog, projectID, root := newTestCatalog(t)
	writeRecipes(t, root, RecipeSet{Recipes: map[string]CommandConfig{}})
	runner := NewRecipeRunner(catalog)

	if _, _, err := runner.Run(projectID, "rm -rf /"); err == nil {
		t.Fatal("expected an error for an undeclared recipe name")
	}
}

func TestRecipeRunnerWithNoFileDeclaresNothing(t *testing.T) {
	catalog, projectID, _ := newTestCatalog(t)
	runner := NewRecipeRunner(catalog)
	if _, _, err := runner.Run(projectID, "go-test"); err == nil {
		t.Fatal("expected an error when no validation-recipes.json exists")
	}
}
