// Package recipefile loads recipes from TOML files into a core.RecipeBook. It is
// an adapter at the edge of the hexagon: it maps a file format into domain types
// and never lets the core depend on TOML or the filesystem. Each recipe is
// validated as it loads (registered capabilities + lifecycle guard), so a
// malformed or unsafe recipe fails closed at load time rather than mid-run.
package recipefile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/xebastian153/hyprvalet/internal/core"
)

// RecipesDir returns the recipe directory: $XDG_CONFIG_HOME/hyprvalet/recipes,
// falling back to ~/.config/hyprvalet/recipes. Each *.toml file is one recipe.
func RecipesDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hyprvalet", "recipes")
}

// fileRecipe is the on-disk TOML shape. Steps are an array-of-tables ([[step]]),
// which preserves order. Keeping this separate from core.Recipe is deliberate:
// the file format is an adapter concern, not a domain type.
type fileRecipe struct {
	Name        string     `toml:"name"`
	Description string     `toml:"description"`
	Step        []fileStep `toml:"step"`
}

type fileStep struct {
	Capability string            `toml:"capability"`
	Args       map[string]string `toml:"args"`
}

func (fr fileRecipe) toRecipe() core.Recipe {
	steps := make([]core.Step, len(fr.Step))
	for i, s := range fr.Step {
		steps[i] = core.Step{Capability: s.Capability, Args: core.Args(s.Args)}
	}
	return core.Recipe{Name: fr.Name, Description: fr.Description, Steps: steps}
}

// Load reads every *.toml recipe in dir, validates each against the registry,
// and files it in a RecipeBook. A missing directory yields an empty book (not an
// error) so the tool works before any recipe is written. A malformed file, an
// unknown capability, a lifecycle-guard violation, or a duplicate name returns
// an error — recipes fail closed. Files load in name order so errors are stable.
func Load(dir string, reg *core.Registry) (*core.RecipeBook, error) {
	book := core.NewRecipeBook()

	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return book, nil
	}
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, fname := range names {
		path := filepath.Join(dir, fname)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var fr fileRecipe
		if err := toml.Unmarshal(data, &fr); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if err := book.Add(fr.toRecipe(), reg); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return book, nil
}
