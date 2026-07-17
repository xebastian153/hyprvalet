package recipefile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/adapters/hypr"
	"github.com/xebastian153/hyprvalet/internal/adapters/omarchy"
	"github.com/xebastian153/hyprvalet/internal/core"
)

// testRegistry builds a registry from the real capabilities so recipes are
// validated against actual capability IDs.
func testRegistry(t *testing.T) *core.Registry {
	t.Helper()
	reg := core.NewRegistry()
	for _, c := range append(hypr.Capabilities(), omarchy.Capabilities()...) {
		if err := reg.Register(c); err != nil {
			t.Fatalf("registering capability: %v", err)
		}
	}
	return reg
}

// writeRecipe writes one recipe file into dir and returns dir.
func writeRecipe(t *testing.T, dir, fname, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, fname), []byte(content), 0o600); err != nil {
		t.Fatalf("writing recipe: %v", err)
	}
}

func TestLoadMissingDirIsEmpty(t *testing.T) {
	book, err := Load(filepath.Join(t.TempDir(), "no-recipes"), testRegistry(t))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(book.List()) != 0 {
		t.Fatalf("expected empty book, got %d recipes", len(book.List()))
	}
}

func TestLoadValidRecipe(t *testing.T) {
	dir := t.TempDir()
	writeRecipe(t, dir, "work.toml", `
name = "work"
description = "Set up my work environment"

[[step]]
capability = "workspace.switch"
args = { workspace = "2" }

[[step]]
capability = "omarchy.run"
args = { args = "restart waybar" }
`)
	book, err := Load(dir, testRegistry(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, ok := book.Get("work")
	if !ok {
		t.Fatal("recipe 'work' not loaded")
	}
	if len(r.Steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(r.Steps))
	}
	if r.Steps[0].Capability != "workspace.switch" || r.Steps[0].Args["workspace"] != "2" {
		t.Fatalf("step 0 wrong: %+v", r.Steps[0])
	}
}

func TestLoadFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			"malformed toml",
			"name = \"x\"\n[[step]] this is broken ][",
		},
		{
			"unknown capability",
			"name = \"x\"\n[[step]]\ncapability = \"does.not.exist\"",
		},
		{
			"lifecycle guard violation",
			"name = \"x\"\n[[step]]\ncapability = \"omarchy.run\"\nargs = { args = \"systemctl reboot\" }",
		},
		{
			"no steps",
			"name = \"x\"\ndescription = \"empty\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRecipe(t, dir, "bad.toml", tt.content)
			if _, err := Load(dir, testRegistry(t)); err == nil {
				t.Fatal("a broken or unsafe recipe must error, not load silently")
			}
		})
	}
}

func TestLoadRejectsDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	one := `
name = "dup"
[[step]]
capability = "workspace.switch"
args = { workspace = "1" }
`
	writeRecipe(t, dir, "a.toml", one)
	writeRecipe(t, dir, "b.toml", one)
	if _, err := Load(dir, testRegistry(t)); err == nil {
		t.Fatal("two files with the same recipe name must error")
	}
}
