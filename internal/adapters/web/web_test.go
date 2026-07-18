package web

import (
	"strings"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

var (
	_ core.Capability = openURL{}
	_ core.Capability = searchWeb{}
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name, in, want string
		wantErr        bool
	}{
		{"bare domain gets https", "youtube.com", "https://youtube.com", false},
		{"path preserved", "youtube.com/abc", "https://youtube.com/abc", false},
		{"https kept", "https://example.com", "https://example.com", false},
		{"missing", "", "", true},
		{"a phrase is not a url", "gatos graciosos", "", true},
		{"no dot is not a host", "localhostthing", "", true},
		{"non-http scheme rejected", "ftp://files.example.com", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeURL(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeURL(%q) = %q, want error", tt.in, got)
				}
				if !core.IsValidation(err) {
					t.Fatalf("rejection must be a ValidationError for the retry loop, got: %v", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("normalizeURL(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestSearchTemplateEscapesInEnvAndDefault(t *testing.T) {
	// The default template must carry the %s the query is escaped into.
	if !strings.Contains(searchTemplateFromEnv(), "%s") {
		t.Fatal("search template must contain the query placeholder")
	}
	// A malformed override (no %s) falls back to the safe default.
	t.Setenv("HYPRVALET_SEARCH_URL", "https://bad.example/nofmt")
	if searchTemplateFromEnv() != searchTemplate {
		t.Fatal("an override without a placeholder must fall back to the default")
	}
}
