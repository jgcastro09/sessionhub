package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jgcastro09/sessionhub/internal/project"
)

// RecipeSet is the on-disk shape of
// .shproject/automation/validation-recipes.json (plan 4.4): a named,
// declared set of deterministic commands an Audit Contract's "validation:"
// line may reference. There is deliberately no default set written for a
// new project — unlike workflow.json/registry config.json, a recipe like
// "go-test" encodes a language assumption the plan's "no NodeStage-specific
// assumptions" principle forbids baking in generically. A card can only ever
// run what a human explicitly declared here.
type RecipeSet struct {
	Recipes map[string]CommandConfig `json:"recipes"`
}

func recipesPath(root string) string {
	return filepath.Join(root, project.Directory, "automation", "validation-recipes.json")
}

// LoadRecipes reads the declared recipe set, or an empty one if the file
// does not exist yet.
func LoadRecipes(root string) (RecipeSet, error) {
	data, err := os.ReadFile(recipesPath(root))
	if os.IsNotExist(err) {
		return RecipeSet{Recipes: map[string]CommandConfig{}}, nil
	}
	if err != nil {
		return RecipeSet{}, err
	}
	var set RecipeSet
	if err := json.Unmarshal(data, &set); err != nil {
		return RecipeSet{}, fmt.Errorf("decode validation recipes: %w", err)
	}
	if set.Recipes == nil {
		set.Recipes = map[string]CommandConfig{}
	}
	return set, nil
}

// defaultRecipeTimeout bounds a recipe that doesn't declare its own —
// generous enough for a real test suite, short enough that a hung command
// never blocks a card's audit indefinitely.
const defaultRecipeTimeout = 5 * time.Minute

// maxRecipeOutput caps how much stdout/stderr an Audit Report quotes, so a
// noisy build never bloats a task card.
const maxRecipeOutput = 2000

// RecipeRunner implements tasks.RecipeRunner: it executes only recipes
// declared in a project's validation-recipes.json, through the existing
// deterministic-command primitive (RunCommand) — never a free-form command
// from a card (plan 10: "Execução de comandos livres a partir de cards" is
// explicitly out of scope).
type RecipeRunner struct {
	catalog *project.Catalog
}

// NewRecipeRunner builds a RecipeRunner backed by the local project
// catalogue (the same one internal/tasks and internal/registry resolve
// project roots through).
func NewRecipeRunner(catalog *project.Catalog) *RecipeRunner {
	return &RecipeRunner{catalog: catalog}
}

// Run executes recipeName for projectID and reports whether it passed
// (exit code 0, not timed out) plus a truncated summary for the card's
// Audit Report section.
func (r *RecipeRunner) Run(projectID, recipeName string) (bool, string, error) {
	proj, err := r.catalog.Get(projectID)
	if err != nil {
		return false, "", err
	}
	set, err := LoadRecipes(proj.Root)
	if err != nil {
		return false, "", err
	}
	config, ok := set.Recipes[recipeName]
	if !ok {
		return false, "", fmt.Errorf("recipe %q is not declared in .shproject/automation/validation-recipes.json", recipeName)
	}
	if config.WorkingDir == "" {
		config.WorkingDir = proj.Root
	} else {
		resolved, err := project.ResolvePath(proj.Root, config.WorkingDir)
		if err != nil {
			return false, "", fmt.Errorf("recipe %q working_dir: %w", recipeName, err)
		}
		config.WorkingDir = resolved
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultRecipeTimeout
	}

	// RunCommand returns a fully-populated CommandResult even when it also
	// returns a non-nil error for a non-zero exit code or a timeout — that
	// is exactly the "recipe failed" outcome an Audit Contract needs to see,
	// not an execution error. Only an empty result (command not found,
	// couldn't start at all) is a real error.
	result, runErr := RunCommand(context.Background(), config)
	if runErr != nil && result.Command == "" {
		return false, "", fmt.Errorf("recipe %q: %w", recipeName, runErr)
	}
	passed := result.ExitCode == 0 && !result.TimedOut
	detail := fmt.Sprintf("exit_code=%d duration=%s", result.ExitCode, result.Duration.Round(time.Millisecond))
	if result.TimedOut {
		detail += " (timed out)"
	}
	if output := strings.TrimSpace(result.Stdout + result.Stderr); output != "" {
		detail += "\n" + truncateOutput(output, maxRecipeOutput)
	}
	return passed, detail, nil
}

func truncateOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "... (truncated)"
}
