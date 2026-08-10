package realtime

import (
	"encoding/json"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/adapters/audio"
	"github.com/xebastian153/hyprvalet/internal/adapters/hypr"
	"github.com/xebastian153/hyprvalet/internal/adapters/media"
	"github.com/xebastian153/hyprvalet/internal/adapters/memory"
	"github.com/xebastian153/hyprvalet/internal/adapters/omarchy"
	"github.com/xebastian153/hyprvalet/internal/adapters/project"
	"github.com/xebastian153/hyprvalet/internal/adapters/remind"
	"github.com/xebastian153/hyprvalet/internal/adapters/terminal"
	"github.com/xebastian153/hyprvalet/internal/adapters/web"
	"github.com/xebastian153/hyprvalet/internal/core"
)

func testRegistry28(t *testing.T) *core.Registry {
	t.Helper()
	reg := core.NewRegistry()
	all := append(hypr.Capabilities(), omarchy.Capabilities()...)
	all = append(all, media.Capabilities()...)
	all = append(all, audio.Capabilities()...)
	all = append(all, remind.Capabilities()...)
	all = append(all, web.Capabilities()...)
	all = append(all, project.Capabilities()...)
	all = append(all, terminal.Capabilities()...)
	all = append(all, memory.Capabilities()...)
	for _, c := range all {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register %q: %v", c.ID(), err)
		}
	}
	if got := len(reg.List()); got != 28 {
		t.Fatalf("registry has %d caps, want 28", got)
	}
	return reg
}

func TestToolsFromRegistry_CountAndIDs(t *testing.T) {
	reg := testRegistry28(t)
	tools := ToolsFromRegistry(reg)
	if len(tools) != 28 {
		t.Fatalf("ToolsFromRegistry len = %d, want 28", len(tools))
	}
	// All tool names should match registry IDs.
	seen := make(map[string]bool)
	for _, tl := range tools {
		if tl.Type != "function" {
			t.Fatalf("tool %q type = %q, want %q", tl.Name, tl.Type, "function")
		}
		if tl.Name == "" {
			t.Fatal("tool with empty name")
		}
		if tl.Description == "" {
			t.Fatalf("tool %q has empty description", tl.Name)
		}
		seen[tl.Name] = true
		// parameters must be valid JSON object with type object
		var schema map[string]any
		if err := json.Unmarshal(tl.Parameters, &schema); err != nil {
			t.Fatalf("tool %q parameters not valid JSON: %v", tl.Name, err)
		}
		if schema["type"] != "object" {
			t.Fatalf("tool %q parameters.type = %v, want object", tl.Name, schema["type"])
		}
		if _, ok := schema["properties"]; !ok {
			t.Fatalf("tool %q schema missing properties", tl.Name)
		}
	}
	for _, cap := range reg.List() {
		if !seen[cap.ID()] {
			t.Fatalf("cap %q missing from tools", cap.ID())
		}
	}
}

func TestToolsFromRegistry_ParamSchemas(t *testing.T) {
	reg := testRegistry28(t)
	tools := ToolsFromRegistry(reg)
	byName := make(map[string]RealtimeTool)
	for _, tl := range tools {
		byName[tl.Name] = tl
	}

	tests := []struct {
		name          string
		capID         string
		wantParams    []string
		shouldBeEmpty bool
	}{
		{"hypr workspace.switch has workspace", "workspace.switch", []string{"workspace"}, false},
		{"hypr app.open has cmd", "app.open", []string{"cmd"}, false},
		{"hypr window.close no params", "window.close", nil, true},
		{"omarchy theme.set has name", "theme.set", []string{"name"}, false},
		{"omarchy omarchy.run has args", "omarchy.run", []string{"args"}, false},
		{"omarchy browser.open no params", "browser.open", nil, true},
		{"media play_pause no params", "media.play_pause", nil, true},
		{"media next no params", "media.next", nil, true},
		{"audio volume.set has percent", "volume.set", []string{"percent"}, false},
		{"audio volume.mute no params", "volume.mute", nil, true},
		{"web web.open has url", "web.open", []string{"url"}, false},
		{"web web.search has query", "web.search", []string{"query"}, false},
		{"project new has name", "project.new", []string{"name"}, false},
		{"terminal read no params", "terminal.read", nil, true},
		{"terminal send has text", "terminal.send", []string{"text"}, false},
		{"memory remember has text", "memory.remember", []string{"text"}, false},
		{"memory recall has query", "memory.recall", []string{"query"}, false},
		{"memory forget has query", "memory.forget", []string{"query"}, false},
		{"remind set has minutes+message", "reminder.set", []string{"minutes", "message"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tl, ok := byName[tt.capID]
			if !ok {
				t.Fatalf("tool %q not found", tt.capID)
			}
			var schema struct {
				Type                 string         `json:"type"`
				Properties           map[string]any `json:"properties"`
				Required             []string       `json:"required"`
				AdditionalProperties bool           `json:"additionalProperties"`
			}
			if err := json.Unmarshal(tl.Parameters, &schema); err != nil {
				t.Fatalf("unmarshal schema: %v", err)
			}
			if tt.shouldBeEmpty {
				if len(schema.Properties) != 0 {
					t.Fatalf("tool %q expected no properties, got %v", tt.capID, schema.Properties)
				}
				if len(schema.Required) != 0 {
					t.Fatalf("tool %q expected no required, got %v", tt.capID, schema.Required)
				}
			} else {
				for _, p := range tt.wantParams {
					if _, ok := schema.Properties[p]; !ok {
						t.Fatalf("tool %q missing property %q, got %v", tt.capID, p, schema.Properties)
					}
					found := false
					for _, r := range schema.Required {
						if r == p {
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("tool %q missing required %q, got %v", tt.capID, p, schema.Required)
					}
					// each property should be string type
					prop := schema.Properties[p].(map[string]any)
					if prop["type"] != "string" {
						t.Fatalf("tool %q param %q type = %v, want string", tt.capID, p, prop["type"])
					}
				}
			}
			// additionalProperties must be false for strictness
			if schema.AdditionalProperties != false {
				t.Fatalf("tool %q additionalProperties = %v, want false", tt.capID, schema.AdditionalProperties)
			}
		})
	}
}

func TestToolChoice_PromptSafe(t *testing.T) {
	if ToolChoiceAuto != "auto" {
		t.Fatalf("ToolChoiceAuto = %q, want %q (prompt-safe, not required/none)", ToolChoiceAuto, "auto")
	}
	// Ensure ToolsFromRegistry does not embed a required tool_choice that forces execution.
	reg := testRegistry28(t)
	tools := ToolsFromRegistry(reg)
	if len(tools) == 0 {
		t.Fatal("no tools")
	}
	// tool_choice is session-level, not per-tool, but we verify constant is safe
	if ToolChoiceAuto == "required" {
		t.Fatal("tool_choice required is unsafe — must be auto")
	}
}

func TestResolveToolCall_Valid(t *testing.T) {
	reg := testRegistry28(t)
	tests := []struct {
		name    string
		tool    string
		args    string
		wantCap string
	}{
		{"workspace.switch valid", "workspace.switch", `{"workspace":"3"}`, "workspace.switch"},
		{"app.open valid", "app.open", `{"cmd":"firefox"}`, "app.open"},
		{"volume.set valid", "volume.set", `{"percent":"42"}`, "volume.set"},
		{"memory.remember valid", "memory.remember", `{"text":"hello"}`, "memory.remember"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cap, args, err := ResolveToolCall(reg, tt.tool, json.RawMessage(tt.args))
			if err != nil {
				t.Fatalf("ResolveToolCall error: %v", err)
			}
			if cap.ID() != tt.wantCap {
				t.Fatalf("cap ID = %q, want %q", cap.ID(), tt.wantCap)
			}
			if args == nil {
				t.Fatal("args nil")
			}
		})
	}
}

func TestResolveToolCall_HallucinatedValidationError(t *testing.T) {
	reg := testRegistry28(t)
	hallucinations := []struct {
		name string
		tool string
		args string
	}{
		{"unknown cap", "window.nuke", `{"target":"all"}`},
		{"typo", "workspace.swithc", `{"workspace":"1"}`},
		{"empty", "", `{}`},
		{"hallucinated new cap", "ai.generate", `{"prompt":"hello"}`},
		{"case mismatch", "Workspace.Switch", `{"workspace":"1"}`},
	}
	for _, tt := range hallucinations {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ResolveToolCall(reg, tt.tool, json.RawMessage(tt.args))
			if err == nil {
				t.Fatal("expected error for hallucinated cap, got nil")
			}
			if !core.IsValidation(err) {
				t.Fatalf("expected Validationf error, got %T: %v", err, err)
			}
			// error message should mention hallucination or not registered
			if len(err.Error()) == 0 {
				t.Fatal("empty error message")
			}
		})
	}
}

func TestResolveToolCall_DoubleAllowlist(t *testing.T) {
	// Double allowlist: Registry + tool_choice path.
	// Even if tool was previously returned by ToolsFromRegistry, a hallucinated
	// name must still be rejected via Validationf (not executed).
	reg := testRegistry28(t)
	tools := ToolsFromRegistry(reg)
	// Simulate sidecar allowlist: tools list contains only registry caps.
	// Hallucinated cap should not be in that list either.
	toolNames := make(map[string]bool)
	for _, tl := range tools {
		toolNames[tl.Name] = true
	}
	hallucinated := "browser.evil"
	if toolNames[hallucinated] {
		t.Fatal("hallucinated tool should not be in allowlist")
	}
	_, _, err := ResolveToolCall(reg, hallucinated, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("hallucinated should be rejected even after checking tool list")
	}
	if !core.IsValidation(err) {
		t.Fatalf("want Validationf, got %v", err)
	}
}

func TestToolsFromRegistry_AllCategoriesCovered(t *testing.T) {
	reg := testRegistry28(t)
	tools := ToolsFromRegistry(reg)
	cats := map[string][]string{
		"hypr":     {"workspace.switch", "window.close", "window.fullscreen", "window.move_to_workspace", "app.open"},
		"omarchy":  {"browser.open", "screenshot.take", "theme.next", "theme.set", "nightlight.toggle", "system.lock", "music.open", "omarchy.run"},
		"media":    {"media.play_pause", "media.next", "media.previous"},
		"audio":    {"volume.set", "volume.mute"},
		"web":      {"web.open", "web.search"},
		"project":  {"project.new", "project.open"},
		"terminal": {"terminal.read", "terminal.send"},
		"memory":   {"memory.remember", "memory.recall", "memory.forget"},
		"remind":   {"reminder.set"},
	}
	byName := make(map[string]bool)
	for _, tl := range tools {
		byName[tl.Name] = true
	}
	for cat, ids := range cats {
		for _, id := range ids {
			if !byName[id] {
				t.Fatalf("category %q missing cap %q", cat, id)
			}
		}
	}
}

func TestToolsFromRegistry_Deterministic(t *testing.T) {
	reg := testRegistry28(t)
	a := ToolsFromRegistry(reg)
	b := ToolsFromRegistry(reg)
	if len(a) != len(b) {
		t.Fatalf("len mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			t.Fatalf("determinism mismatch at %d: %q vs %q", i, a[i].Name, b[i].Name)
		}
	}
	// ensure sorted order (registry is sorted, tools should preserve it)
	for i := 1; i < len(a); i++ {
		if a[i-1].Name > a[i].Name {
			t.Fatalf("tools not sorted: %q > %q at %d", a[i-1].Name, a[i].Name, i)
		}
	}
}
