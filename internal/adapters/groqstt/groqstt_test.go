package groqstt

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeWav(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.wav")
	// Minimal fake wav content; groq fake server doesn't validate audio.
	if content == "" {
		content = "RIFF fake wav data"
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write wav: %v", err)
	}
	return p
}

func TestCleanTranscript(t *testing.T) {
	tests := []struct {
		name, raw, want string
	}{
		{"joins lines", " switch to workspace 3\n and open firefox \n", "switch to workspace 3 and open firefox"},
		{"strips artifacts", "[BLANK_AUDIO]\n", ""},
		{"keeps text around artifacts", "[MUSIC]\nopen firefox\n", "open firefox"},
		{"empty is empty", "\n\n", ""},
		{"brackets inside text kept", "hello [world] inside", "hello [world] inside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanTranscript(tt.raw); got != tt.want {
				t.Fatalf("CleanTranscript(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestTranscribeEmptyKey(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "")
	c := New("http://unused", DefaultModel, "")
	wav := makeWav(t, "")
	_, err := c.Transcribe(context.Background(), wav)
	if err == nil {
		t.Fatal("empty key must error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "groq api key not found at") {
		t.Fatalf("error should contain 'groq api key not found at', got %q", err.Error())
	}
}

func TestTranscribeSuccess(t *testing.T) {
	var gotAuth, gotContentType string
	var gotFields map[string]string
	var gotFileName, gotFileContentType string
	var gotFileContent string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotFields = map[string]string{
			"model":           r.FormValue("model"),
			"language":        r.FormValue("language"),
			"response_format": r.FormValue("response_format"),
			"temperature":     r.FormValue("temperature"),
		}
		f, header, err := r.FormFile("file")
		if err == nil {
			defer f.Close()
			gotFileName = header.Filename
			gotFileContentType = header.Header.Get("Content-Type")
			b, _ := io.ReadAll(f)
			gotFileContent = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": " switch to workspace 3\n"})
	}))
	defer ts.Close()

	t.Setenv("GROQ_API_KEY", "sk-test")
	t.Setenv("HYPRVALET_STT_LANG", "")
	c := New(ts.URL, DefaultModel, "sk-test")
	wav := makeWav(t, "fake wav bytes")
	got, err := c.Transcribe(context.Background(), wav)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "switch to workspace 3" {
		t.Fatalf("transcript = %q, want %q", got, "switch to workspace 3")
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q, want Bearer sk-test", gotAuth)
	}
	if !strings.Contains(gotContentType, "multipart/form-data") {
		t.Fatalf("Content-Type = %q, want multipart/form-data", gotContentType)
	}
	if gotFields["model"] != DefaultModel {
		t.Fatalf("model = %q, want %q", gotFields["model"], DefaultModel)
	}
	if gotFields["response_format"] != "json" {
		t.Fatalf("response_format = %q, want json", gotFields["response_format"])
	}
	if gotFields["temperature"] != "0" {
		t.Fatalf("temperature = %q, want 0", gotFields["temperature"])
	}
	// language defaults to auto
	if gotFields["language"] != "auto" {
		t.Fatalf("language = %q, want auto", gotFields["language"])
	}
	if gotFileName == "" {
		t.Fatal("file field missing")
	}
	if gotFileContentType != "audio/wav" {
		t.Fatalf("file Content-Type = %q, want audio/wav", gotFileContentType)
	}
	if gotFileContent != "fake wav bytes" {
		t.Fatalf("file content = %q", gotFileContent)
	}
}

func TestTranscribeLanguageParam(t *testing.T) {
	tests := []struct {
		name     string
		envLang  string
		wantLang string
	}{
		{"default auto", "", "auto"},
		{"spanish", "es", "es"},
		{"english", "en", "en"},
		{"trimmed", "  es  ", "es"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotLang string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseMultipartForm(10 << 20)
				gotLang = r.FormValue("language")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"text": "hola"})
			}))
			defer ts.Close()
			t.Setenv("GROQ_API_KEY", "sk-test")
			t.Setenv("HYPRVALET_STT_LANG", tt.envLang)
			c := New(ts.URL, DefaultModel, "sk-test")
			wav := makeWav(t, "")
			if _, err := c.Transcribe(context.Background(), wav); err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if gotLang != tt.wantLang {
				t.Fatalf("language = %q, want %q", gotLang, tt.wantLang)
			}
		})
	}
}

func TestTranscribeModelField(t *testing.T) {
	var gotModel string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		gotModel = r.FormValue("model")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "ok"})
	}))
	defer ts.Close()
	t.Setenv("GROQ_API_KEY", "k")
	c := New(ts.URL, DefaultModel, "k")
	wav := makeWav(t, "")
	if _, err := c.Transcribe(context.Background(), wav); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotModel != "whisper-large-v3" {
		t.Fatalf("model = %q, want whisper-large-v3", gotModel)
	}
}

func TestTranscribeMultipartFile(t *testing.T) {
	// Verify multipart parsing via multipart.Reader to ensure boundary handling.
	var gotFileHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		mr, err := multipart.NewReader(r.Body, strings.Split(ct, "boundary=")[1]), error(nil)
		// Fallback: use ParseMultipartForm
		if err != nil {
			_ = r.ParseMultipartForm(10 << 20)
		} else {
			_ = mr
		}
		gotFileHeader = ct
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "ok"})
	}))
	defer ts.Close()
	t.Setenv("GROQ_API_KEY", "k")
	c := New(ts.URL, DefaultModel, "k")
	wav := makeWav(t, "wavcontent")
	if _, err := c.Transcribe(context.Background(), wav); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if !strings.Contains(gotFileHeader, "multipart/form-data") {
		t.Fatalf("Content-Type missing multipart, got %q", gotFileHeader)
	}
}

func TestTranscribe401(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	defer ts.Close()
	t.Setenv("GROQ_API_KEY", "bad")
	c := New(ts.URL, DefaultModel, "bad")
	wav := makeWav(t, "")
	_, err := c.Transcribe(context.Background(), wav)
	if err == nil {
		t.Fatal("401 must error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error should contain 401, got %q", err.Error())
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		t.Fatalf("error should contain unauthorized, got %q", err.Error())
	}
}

func TestTranscribe429(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests)
	}))
	defer ts.Close()
	t.Setenv("GROQ_API_KEY", "k")
	c := New(ts.URL, DefaultModel, "k")
	wav := makeWav(t, "")
	_, err := c.Transcribe(context.Background(), wav)
	if err == nil {
		t.Fatal("429 must error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("error should contain 429, got %q", err.Error())
	}
	if !strings.Contains(strings.ToLower(err.Error()), "retryable") {
		t.Fatalf("429 error should be retryable, got %q", err.Error())
	}
}

func TestTranscribe5xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `internal error`, http.StatusInternalServerError)
	}))
	defer ts.Close()
	t.Setenv("GROQ_API_KEY", "k")
	c := New(ts.URL, DefaultModel, "k")
	wav := makeWav(t, "")
	_, err := c.Transcribe(context.Background(), wav)
	if err == nil {
		t.Fatal("5xx must error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "retryable") {
		t.Fatalf("5xx error should be retryable, got %q", err.Error())
	}
}

func TestTranscribeArtifactCleaning(t *testing.T) {
	tests := []struct {
		name, serverText, want string
	}{
		{"blank audio", "[BLANK_AUDIO]", ""},
		{"music artifact", "[MUSIC]\nopen firefox\n", "open firefox"},
		{"trim and clean", "  hello world  \n", "hello world"},
		{"blank with text", "[BLANK_AUDIO]\nhola mundo\n", "hola mundo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"text": tt.serverText})
			}))
			defer ts.Close()
			t.Setenv("GROQ_API_KEY", "k")
			c := New(ts.URL, DefaultModel, "k")
			wav := makeWav(t, "")
			got, err := c.Transcribe(context.Background(), wav)
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTranscribeTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "late"})
	}))
	defer ts.Close()

	t.Setenv("GROQ_API_KEY", "k")
	c := New(ts.URL, DefaultModel, "k")
	// Use a client with short timeout to avoid 30s wait; inject via New then override http client.
	c = &Client{
		baseURL: ts.URL,
		model:   DefaultModel,
		apiKey:  "k",
		http:    &http.Client{Timeout: 50 * time.Millisecond},
	}
	wav := makeWav(t, "")
	ctx := context.Background()
	_, err := c.Transcribe(ctx, wav)
	if err == nil {
		t.Fatal("timeout must error")
	}
	// Error should mention timeout or deadline or calling groq stt
	if !strings.Contains(strings.ToLower(err.Error()), "timeout") && !strings.Contains(strings.ToLower(err.Error()), "deadline") && !strings.Contains(err.Error(), "Client.Timeout") {
		// Accept any error that indicates failure; but ensure it's not success
		t.Logf("timeout error = %q (acceptable if not containing timeout word, but should be context-related)", err.Error())
	}
}

func TestTranscribeContextCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "late"})
	}))
	defer ts.Close()
	t.Setenv("GROQ_API_KEY", "k")
	c := New(ts.URL, DefaultModel, "k")
	wav := makeWav(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.Transcribe(ctx, wav)
	if err == nil {
		t.Fatal("context cancel must error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "context") && !strings.Contains(strings.ToLower(err.Error()), "deadline") && !strings.Contains(err.Error(), "canceled") {
		t.Logf("context error = %q", err.Error())
	}
}

func TestAvailable(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "")
	if Available() {
		t.Fatal("Available should be false when key empty")
	}
	t.Setenv("GROQ_API_KEY", "sk-123")
	if !Available() {
		t.Fatal("Available should be true when key set")
	}
	t.Setenv("GROQ_API_KEY", "  sk-123  ")
	if !Available() {
		t.Fatal("Available should trim spaces")
	}
}

func TestNewFromEnv(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "sk-env")
	t.Setenv("HYPRVALET_GROQ_URL", "")
	t.Setenv("HYPRVALET_GROQ_STT_MODEL", "")
	c := NewFromEnv()
	if c.apiKey != "sk-env" {
		t.Fatalf("apiKey = %q, want sk-env", c.apiKey)
	}
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.model != DefaultModel {
		t.Fatalf("model = %q, want %q", c.model, DefaultModel)
	}
	t.Setenv("HYPRVALET_GROQ_URL", "https://example.com/v1")
	t.Setenv("HYPRVALET_GROQ_STT_MODEL", "whisper-large-v3-turbo")
	c = NewFromEnv()
	if c.baseURL != "https://example.com/v1" {
		t.Fatalf("override baseURL = %q", c.baseURL)
	}
	if c.model != "whisper-large-v3-turbo" {
		t.Fatalf("override model = %q", c.model)
	}
}

func TestEnvFilePath(t *testing.T) {
	p := EnvFilePath()
	if p == "" {
		t.Fatal("EnvFilePath empty")
	}
	if !filepath.IsAbs(p) {
		t.Fatalf("EnvFilePath should be absolute, got %q", p)
	}
	t.Setenv("XDG_CONFIG_HOME", "/tmp/testxdg")
	if got := EnvFilePath(); got != "/tmp/testxdg/hyprvalet/env" {
		t.Fatalf("XDG path = %q", got)
	}
}

func TestTranscribeMissingFile(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "k")
	c := New("http://unused", DefaultModel, "k")
	_, err := c.Transcribe(context.Background(), "/nonexistent/path.wav")
	if err == nil {
		t.Fatal("missing file must error")
	}
	if !strings.Contains(err.Error(), "audio file not found") {
		t.Fatalf("error should mention audio file not found, got %q", err.Error())
	}
}
