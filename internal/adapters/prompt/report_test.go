package prompt

import (
	"strings"
	"testing"
)

func TestParseReportFromJSON(t *testing.T) {
	got := ParseReport(`{"report":"Listo, señor. Construí el CLI de gastos con add y summary."}`)
	if got != "Listo, señor. Construí el CLI de gastos con add y summary." {
		t.Fatalf("report parse wrong: %q", got)
	}
}

func TestParseReportFallsBackToRaw(t *testing.T) {
	// Not the expected JSON — the prose is still usable.
	got := ParseReport("Se construyó el proyecto sin problemas.")
	if got != "Se construyó el proyecto sin problemas." {
		t.Fatalf("raw fallback wrong: %q", got)
	}
}

func TestParseReportEmptyJSONFallsBack(t *testing.T) {
	// An empty report field falls back to the raw content, not an empty string.
	raw := `{"report":""}`
	if got := ParseReport(raw); got != raw {
		t.Fatalf("empty report should fall back to raw, got %q", got)
	}
}

func TestBuildReportGroundsInTerminalAndLocation(t *testing.T) {
	p := BuildReport("Project plan — Shop", "/home/sebas/proyectos/shop")
	for _, want := range []string{"Shop", "/home/sebas/proyectos/shop", "grounded in the terminal", "report"} {
		if !strings.Contains(p, want) {
			t.Fatalf("report prompt missing %q", want)
		}
	}
}
