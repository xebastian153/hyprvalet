package groq

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

// mockGroq returns a server replying with content as an OpenAI-style chat
// completion, capturing the request body and auth header for assertions.
func mockGroq(t *testing.T, content string, gotBody *map[string]any, gotAuth *string) *httptest.Server {
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
	ts := mockGroq(t, `{"capability":"workspace.switch","args":{"workspace":"3"},"reply":""}`, &body, &auth)
	defer ts.Close()

	intent, err := New(ts.URL, "test-70b", "sk-test").Interpret(context.Background(), "go to 3", nil, nil)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if intent.Capability != "workspace.switch" || intent.Args["workspace"] != "3" {
		t.Fatalf("intent = %+v", intent)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("auth header = %q", auth)
	}
	if body["model"] != "test-70b" {
		t.Fatalf("model = %v", body["model"])
	}
	if rf, ok := body["response_format"].(map[string]any); !ok || rf["type"] != "json_object" {
		t.Fatalf("response_format = %v (want json_object mode)", body["response_format"])
	}
}

func TestPlanParses(t *testing.T) {
	ts := mockGroq(t, `{"summary":"two hops","steps":[{"capability":"workspace.switch","args":{"workspace":"3"}},{"capability":"workspace.switch","args":{"workspace":"2"}}]}`, nil, nil)
	defer ts.Close()

	plan, err := New(ts.URL, "m", "k").Plan(context.Background(), "3 then 2", nil, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Steps) != 2 || plan.Summary != "two hops" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestMissingKeyFailsWithHint(t *testing.T) {
	if _, err := New("http://unused", "m", "").Interpret(context.Background(), "x", nil, nil); err == nil {
		t.Fatal("missing key must error with a hint, before any network call")
	}
}

func TestHTTPErrorSurfaces(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	defer ts.Close()

	if _, err := New(ts.URL, "m", "bad").Interpret(context.Background(), "x", nil, nil); err == nil {
		t.Fatal("a non-200 response must be an error")
	}
}
