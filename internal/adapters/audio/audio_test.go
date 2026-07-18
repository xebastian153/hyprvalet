package audio

import (
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// The adapter must satisfy the core port; compile-time proof.
var (
	_ core.Capability = setVolume{}
	_ core.Capability = toggleMute{}
)

func TestPercentArg(t *testing.T) {
	tests := []struct {
		name    string
		args    core.Args
		want    int
		wantErr bool
	}{
		{"valid", core.Args{"percent": "50"}, 50, false},
		{"zero is valid", core.Args{"percent": "0"}, 0, false},
		{"hundred is valid", core.Args{"percent": "100"}, 100, false},
		{"trims spaces", core.Args{"percent": " 30 "}, 30, false},
		{"missing", core.Args{}, 0, true},
		{"empty", core.Args{"percent": " "}, 0, true},
		{"not a number", core.Args{"percent": "loud"}, 0, true},
		{"negative", core.Args{"percent": "-5"}, 0, true},
		{"over unity gain", core.Args{"percent": "150"}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := percentArg(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("percentArg(%v) = %d, want error", tt.args, got)
				}
				if !core.IsValidation(err) {
					t.Fatalf("rejection must be a ValidationError for the corrective loop, got: %v", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("percentArg(%v) = %d, %v; want %d", tt.args, got, err, tt.want)
			}
		})
	}
}
