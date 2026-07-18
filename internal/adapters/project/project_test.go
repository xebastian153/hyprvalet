package project

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

var (
	_ core.Capability = newProject{}
	_ core.Capability = openProject{}
)

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"tienda", "tienda"},
		{"Mi Tienda Online", "mi-tienda-online"},
		{"  spaced  ", "spaced"},
		{"already-dashed", "already-dashed"},
		{"under_score", "under-score"},
		{"ac-cents-áé", "ac-cents"},
		{"weird/../path", "weird-path"},
		{"...", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := slugify(tt.in); got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A slug can never escape the base directory — no traversal, no absolute path.
func TestResolveStaysInBase(t *testing.T) {
	t.Setenv("HYPRVALET_PROJECTS_DIR", "/home/user/proyectos")
	for _, name := range []string{"../../etc/passwd", "/etc/shadow", "a/b/c", "..", "normal name"} {
		slug, dir, err := resolve(name)
		if err != nil {
			continue // empty/unusable slugs are rejected outright — also safe
		}
		if strings.Contains(slug, "/") || strings.Contains(slug, "..") {
			t.Fatalf("slug %q from %q is unsafe", slug, name)
		}
		if filepath.Dir(dir) != "/home/user/proyectos" {
			t.Fatalf("dir %q from %q escaped the base", dir, name)
		}
	}
}

func TestResolveRejectsEmpty(t *testing.T) {
	if _, _, err := resolve("..."); err == nil || !core.IsValidation(err) {
		t.Fatalf("unusable name must be a ValidationError, got %v", err)
	}
}
