package omarchy

import (
	"strings"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Every capability must satisfy the core port; compile-time proof.
var (
	_ core.Capability = runCommand{}
	_ core.Capability = openBrowser{}
	_ core.Capability = openMusic{}
	_ core.Capability = takeScreenshot{}
	_ core.Capability = themeNext{}
	_ core.Capability = themeSet{}
	_ core.Capability = nightlightToggle{}
	_ core.Capability = lockScreen{}
)

func TestMatchTheme(t *testing.T) {
	installed := []string{"Catppuccin", "Matte Black", "Tokyo_Night", ""}

	tests := []struct {
		name, want, resolved string
		wantErr              bool
	}{
		{"exact", "Catppuccin", "Catppuccin", false},
		{"case-insensitive", "catppuccin", "Catppuccin", false},
		{"spaces vs none", "matteblack", "Matte Black", false},
		{"hyphen vs underscore", "tokyo-night", "Tokyo_Night", false},
		{"unknown theme", "dracula", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := matchTheme(tt.want, installed)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("matchTheme(%q) = %q, want error", tt.want, got)
				}
				if !core.IsValidation(err) {
					t.Fatalf("unknown theme must be a ValidationError, got: %v", err)
				}
				// The corrective error must carry the real options so the
				// retry loop can feed the model a menu, not just a "no".
				if !strings.Contains(err.Error(), "Matte Black") {
					t.Fatalf("error should list installed themes: %v", err)
				}
				return
			}
			if err != nil || got != tt.resolved {
				t.Fatalf("matchTheme(%q) = %q, %v; want %q", tt.want, got, err, tt.resolved)
			}
		})
	}
}
