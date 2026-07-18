package terminal

import (
	"strings"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

var (
	_ core.Capability = readTerminal{}
	_ core.Capability = sendTerminal{}
)

func TestSendIsConfirmTier(t *testing.T) {
	// Relaying words into Claude's terminal must never be Safe — it always
	// asks first.
	if (sendTerminal{}).Risk() != core.RiskConfirm {
		t.Fatal("terminal.send must be Confirm-tier — it types on the user's behalf")
	}
}

func TestSendRejectsEmpty(t *testing.T) {
	if _, err := (sendTerminal{}).Run(nil, core.Args{}); !core.IsValidation(err) {
		t.Fatalf("empty send must be a ValidationError, got %v", err)
	}
}

func TestCleanCapture(t *testing.T) {
	raw := strings.Join([]string{
		"╭──────────────────────────────╮",
		"│ Accessing workspace:         │",
		"                              ",
		"│   /home/sebas/proyectos/x    │",
		"╰──────────────────────────────╯",
		"───────────────────────────────",
		"❯ 1. Yes, I trust this folder",
		"  2. No, exit",
		"",
	}, "\n")
	got := cleanCapture(raw, 14)
	// Decoration-only lines drop out; text lines survive (trimmed).
	for _, want := range []string{"Accessing workspace:", "/home/sebas/proyectos/x", "1. Yes, I trust this folder", "2. No, exit"} {
		if !strings.Contains(got, want) {
			t.Fatalf("clean output missing %q:\n%s", want, got)
		}
	}
	// A pure border line must not survive.
	if strings.Contains(got, "╰") || strings.Contains(got, "───") {
		t.Fatalf("decoration leaked into output:\n%s", got)
	}
}

func TestCleanCaptureKeepsTail(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "line "+string(rune('a'+i%26))+"content")
	}
	got := cleanCapture(strings.Join(lines, "\n"), 5)
	if n := len(strings.Split(got, "\n")); n != 5 {
		t.Fatalf("kept %d lines, want the last 5", n)
	}
}

func TestHasMeaning(t *testing.T) {
	if hasMeaning("╭────╮ │ ╰──╯ ───") {
		t.Fatal("a decoration-only line has no meaning")
	}
	if !hasMeaning("❯ 1. Yes") {
		t.Fatal("a line with letters/digits has meaning")
	}
}
