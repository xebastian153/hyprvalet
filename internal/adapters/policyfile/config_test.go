package policyfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	rules, err := Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if got := rules.ByRisk[core.RiskSafe].Decision; got != core.DecisionAllow {
		t.Errorf("default safe tier = %v, want allow", got)
	}
	if got := rules.ByRisk[core.RiskForbidden].Decision; got != core.DecisionDeny {
		t.Errorf("default forbidden tier = %v, want deny", got)
	}
	if got := rules.Default.Decision; got != core.DecisionAsk {
		t.Errorf("default fallback = %v, want ask", got)
	}
}

func TestLoadOverlaysOntoDefaults(t *testing.T) {
	rules, err := Load(writeTemp(t, `
default = "deny"
default_arm_minutes = 10

[risk]
safe = "ask"

[access]
command = "deny"

[capability."app.open"]
decision = "allow"
requires_arming = true
arm_minutes = 3
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if rules.Default.Decision != core.DecisionDeny {
		t.Errorf("default = %v, want deny", rules.Default.Decision)
	}
	if rules.DefaultArmFor != 10*time.Minute {
		t.Errorf("DefaultArmFor = %v, want 10m", rules.DefaultArmFor)
	}
	// Overridden level.
	if rules.ByRisk[core.RiskSafe].Decision != core.DecisionAsk {
		t.Errorf("safe = %v, want ask (overridden)", rules.ByRisk[core.RiskSafe].Decision)
	}
	// Unspecified level keeps the shipped baseline (overlay, not replace).
	if rules.ByRisk[core.RiskConfirm].Decision != core.DecisionAsk {
		t.Errorf("confirm = %v, want ask (baseline kept)", rules.ByRisk[core.RiskConfirm].Decision)
	}
	if rules.ByAccess[core.AccessCommand].Decision != core.DecisionDeny {
		t.Errorf("access.command = %v, want deny", rules.ByAccess[core.AccessCommand].Decision)
	}
	r := rules.ByCapID["app.open"]
	if r.Decision != core.DecisionAllow || !r.RequiresArming || r.ArmFor != 3*time.Minute {
		t.Errorf("app.open rule = %+v, want {allow, arming, 3m}", r)
	}
}

func TestLoadCapabilityArmingOnlyDefaultsToAsk(t *testing.T) {
	rules, err := Load(writeTemp(t, `
[capability."x.y"]
requires_arming = true
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := rules.ByCapID["x.y"]
	if r.Decision != core.DecisionAsk {
		t.Errorf("decision = %v, want ask (empty decision defaults to ask)", r.Decision)
	}
	if !r.RequiresArming {
		t.Error("requires_arming not carried through")
	}
}

func TestLoadFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"malformed toml", "this is not valid toml ][["},
		{"invalid decision word", `default = "maybe"`},
		{"invalid risk tier", "[risk]\ncritical = \"deny\""},
		{"invalid access kind", "[access]\nfilesystem = \"deny\""},
		{"invalid capability decision", "[capability.\"a.b\"]\ndecision = \"perhaps\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, tt.content)); err == nil {
				t.Fatal("a broken policy must error, never fall back to permissive")
			}
		})
	}
}
