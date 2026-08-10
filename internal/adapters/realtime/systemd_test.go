package realtime

import (
	"os"
	"strings"
	"testing"
)

func TestSystemdServiceExistsAndValid(t *testing.T) {
	path := "configs/systemd/hyprvalet-realtime.service"
	// Try multiple relative locations (test runs from package dir)
	candidates := []string{
		"../../../configs/systemd/hyprvalet-realtime.service",
		"../../configs/systemd/hyprvalet-realtime.service",
		"configs/systemd/hyprvalet-realtime.service",
		path,
	}
	var data []byte
	var err error
	var found string
	for _, p := range candidates {
		data, err = os.ReadFile(p)
		if err == nil {
			found = p
			break
		}
	}
	if err != nil {
		t.Fatalf("service file not found (tried %v): %v", candidates, err)
	}
	content := string(data)
	_ = found

	tests := []struct {
		name   string
		substr string
	}{
		{"ExecStart contains serve", "serve"},
		{"host 127.0.0.1", "127.0.0.1"},
		{"port 8765", "8765"},
		{"llm_backend responses_api", "responses_api"},
		{"model google/gemma-4-31B", "google/gemma-4-31B"},
		{"responses_api_base_url https://api.cerebras.ai/v1", "https://api.cerebras.ai/v1"},
		{"EnvironmentFile hyprvalet/env", "hyprvalet/env"},
		{"Restart on-failure", "Restart=on-failure"},
		{"no CUDA reference", "CUDA"}, // we will check NOT contain CUDA via separate logic
		{"CEREBRAS_API_KEY note or env", "CEREBRAS_API_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "no CUDA reference" {
				if strings.Contains(strings.ToLower(content), "cuda") {
					t.Fatalf("service file should not reference CUDA (cloud Cerebras, no local GPU needed), found cuda in %q", content)
				}
				return
			}
			if !strings.Contains(content, tt.substr) {
				t.Fatalf("service file %q missing %q (%q)", found, tt.substr, content)
			}
		})
	}
	// Check that CEREBRAS_API_KEY permission note mentions 0600 or EnvironmentFile is 0600-guarded
	if !strings.Contains(content, "0600") && !strings.Contains(content, "600") {
		t.Logf("warning: service file does not explicitly mention 0600 permissions for CEREBRAS_API_KEY, but EnvironmentFile present: %v", strings.Contains(content, "EnvironmentFile"))
		// not fatal, but we check Env file exists note
		if !strings.Contains(content, "EnvironmentFile") {
			t.Fatal("service file should reference EnvironmentFile for CEREBRAS_API_KEY 0600 handling")
		}
	}
	if !strings.Contains(content, "[Unit]") || !strings.Contains(content, "[Service]") || !strings.Contains(content, "[Install]") {
		t.Fatal("service file missing systemd unit sections")
	}
}
