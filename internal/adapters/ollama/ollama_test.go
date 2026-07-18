package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Compile-time proof the client satisfies both reasoning ports.
var (
	_ core.LLMPort     = (*Client)(nil)
	_ core.PlannerPort = (*Client)(nil)
)

// mockOllama returns a server that replies with the given message content as an
// Ollama /api/chat response, and captures the request body for assertions.
func mockOllama(t *testing.T, content string, captured *chatRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, captured)
		}
		resp := chatResponse{Message: chatMessage{Role: "assistant", Content: content}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestInterpretParsesIntent(t *testing.T) {
	var got chatRequest
	ts := mockOllama(t, `{"capability":"workspace.switch","args":{"workspace":"3"},"reasoning":"user asked to switch"}`, &got)
	defer ts.Close()

	intent, err := New(ts.URL, "test-model").Interpret(
		context.Background(), "go to workspace 3", nil)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if intent.Capability != "workspace.switch" {
		t.Errorf("capability = %q, want workspace.switch", intent.Capability)
	}
	if intent.Args["workspace"] != "3" {
		t.Errorf("args[workspace] = %q, want 3", intent.Args["workspace"])
	}

	// The request must carry the model and a structured-output format.
	if got.Model != "test-model" {
		t.Errorf("request model = %q, want test-model", got.Model)
	}
	if len(got.Format) == 0 {
		t.Error("request did not include a structured-output format")
	}
}

func TestInterpretNoMatchIsEmptyNotError(t *testing.T) {
	ts := mockOllama(t, `{"capability":"","reasoning":"nothing fits"}`, nil)
	defer ts.Close()

	intent, err := New(ts.URL, "m").Interpret(context.Background(), "make me a sandwich", nil)
	if err != nil {
		t.Fatalf("no-match must not error: %v", err)
	}
	if intent.Capability != "" {
		t.Fatalf("capability = %q, want empty", intent.Capability)
	}
}

func TestInterpretHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer ts.Close()

	if _, err := New(ts.URL, "missing").Interpret(context.Background(), "x", nil); err == nil {
		t.Fatal("a non-200 response must be an error")
	}
}

func TestInterpretBadContent(t *testing.T) {
	ts := mockOllama(t, `this is not intent json`, nil)
	defer ts.Close()

	if _, err := New(ts.URL, "m").Interpret(context.Background(), "x", nil); err == nil {
		t.Fatal("unparseable model content must be an error")
	}
}

func TestPromptsListCapabilities(t *testing.T) {
	caps := []core.Capability{promptCap{}}
	for _, tt := range []struct {
		name   string
		prompt string
	}{
		{"intent prompt", buildIntentPrompt(caps)},
		{"plan prompt", buildPlanPrompt(caps)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.prompt, "demo.thing") {
				t.Error("prompt should list the capability id")
			}
			if !strings.Contains(tt.prompt, "widget") {
				t.Error("prompt should list the capability params")
			}
		})
	}
}

func TestPlanParsesSteps(t *testing.T) {
	var got chatRequest
	content := `{"summary":"set up work","steps":[` +
		`{"capability":"workspace.switch","args":{"workspace":"2"}},` +
		`{"capability":"app.open","args":{"cmd":"code"}}]}`
	ts := mockOllama(t, content, &got)
	defer ts.Close()

	plan, err := New(ts.URL, "m").Plan(context.Background(), "set up my work env", nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Summary != "set up work" {
		t.Errorf("summary = %q", plan.Summary)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(plan.Steps))
	}
	if plan.Steps[0].Capability != "workspace.switch" || plan.Steps[0].Args["workspace"] != "2" {
		t.Errorf("step 0 wrong: %+v", plan.Steps[0])
	}
	if plan.Steps[1].Capability != "app.open" || plan.Steps[1].Args["cmd"] != "code" {
		t.Errorf("step 1 wrong: %+v", plan.Steps[1])
	}
	if plan.Request != "set up my work env" {
		t.Errorf("request not preserved: %q", plan.Request)
	}
	if len(got.Format) == 0 {
		t.Error("plan request must include a structured-output format")
	}
}

func TestPlanEmptyStepsIsNotError(t *testing.T) {
	ts := mockOllama(t, `{"summary":"cannot do that","steps":[]}`, nil)
	defer ts.Close()

	plan, err := New(ts.URL, "m").Plan(context.Background(), "brew coffee", nil)
	if err != nil {
		t.Fatalf("empty plan must not error: %v", err)
	}
	if len(plan.Steps) != 0 {
		t.Fatalf("expected 0 steps, got %d", len(plan.Steps))
	}
}

// promptCap is a minimal capability for prompt-building assertions.
type promptCap struct{}

func (promptCap) ID() string                                     { return "demo.thing" }
func (promptCap) Description() string                            { return "Do a demo thing" }
func (promptCap) Access() core.AccessKind                        { return core.AccessCommand }
func (promptCap) Risk() core.Risk                                { return core.RiskSafe }
func (promptCap) Params() []string                               { return []string{"widget"} }
func (promptCap) Run(context.Context, core.Args) (string, error) { return "", nil }
