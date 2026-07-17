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

// Compile-time proof the client satisfies the reasoning port.
var _ core.LLMPort = (*Client)(nil)

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

func TestBuildSystemPromptListsCapabilities(t *testing.T) {
	reg := core.NewRegistry()
	// A tiny fake capability just to check the prompt lists ids and params.
	prompt := buildSystemPrompt([]core.Capability{promptCap{}})
	_ = reg
	if !strings.Contains(prompt, "demo.thing") {
		t.Error("prompt should list the capability id")
	}
	if !strings.Contains(prompt, "widget") {
		t.Error("prompt should list the capability params")
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
