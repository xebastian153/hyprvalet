package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Compile-time proof the client satisfies both reasoning ports.
var (
	_ core.LLMPort     = (*Client)(nil)
	_ core.PlannerPort = (*Client)(nil)
)

// mockOpenAI returns a server replying with content as an OpenAI-style chat
// completion, capturing the request body and auth header for assertions.
func mockOpenAI(t *testing.T, content string, gotBody *map[string]any, gotAuth *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		if gotBody != nil {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, gotBody)
		}
		resp := map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestInterpretSendsAuthAndModelAndParses(t *testing.T) {
	var body map[string]any
	var auth string
	ts := mockOpenAI(t, `{"capability":"workspace.switch","args":{"workspace":"3"},"reply":""}`, &body, &auth)
	defer ts.Close()

	intent, err := New(ts.URL, "gpt-4o-mini", "sk-test").Interpret(context.Background(), "go to 3", nil, nil)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if intent.Capability != "workspace.switch" || intent.Args["workspace"] != "3" {
		t.Fatalf("intent = %+v", intent)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("auth header = %q", auth)
	}
	if body["model"] != "gpt-4o-mini" {
		t.Fatalf("model = %v", body["model"])
	}
	if rf, ok := body["response_format"].(map[string]any); !ok || rf["type"] != "json_object" {
		t.Fatalf("response_format = %v (want json_object mode)", body["response_format"])
	}
}

func TestChatReturnsRawContent(t *testing.T) {
	ts := mockOpenAI(t, `{"question":"what for?"}`, nil, nil)
	defer ts.Close()
	out, err := New(ts.URL, "m", "k").Chat(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out != `{"question":"what for?"}` {
		t.Fatalf("chat content = %q", out)
	}
}

func TestMissingKeyFailsWithHint(t *testing.T) {
	if _, err := New("http://unused", "m", "").Interpret(context.Background(), "x", nil, nil); err == nil {
		t.Fatal("missing key must error with a hint, before any network call")
	}
}
