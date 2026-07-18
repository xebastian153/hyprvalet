package remind

import (
	"context"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

var _ core.Capability = setReminder{}

func TestMinutesArg(t *testing.T) {
	tests := []struct {
		name    string
		args    core.Args
		want    int
		wantErr bool
	}{
		{"valid", core.Args{"minutes": "15"}, 15, false},
		{"one is valid", core.Args{"minutes": "1"}, 1, false},
		{"a day is valid", core.Args{"minutes": "1440"}, 1440, false},
		{"missing", core.Args{}, 0, true},
		{"not a number", core.Args{"minutes": "quince"}, 0, true},
		{"zero", core.Args{"minutes": "0"}, 0, true},
		{"beyond a day", core.Args{"minutes": "2000"}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := minutesArg(tt.args)
			if tt.wantErr {
				if err == nil || !core.IsValidation(err) {
					t.Fatalf("minutesArg(%v) = %d, %v; want a ValidationError", tt.args, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("minutesArg(%v) = %d, %v; want %d", tt.args, got, err, tt.want)
			}
		})
	}
}

func TestMessageValidation(t *testing.T) {
	// Missing and oversized messages must be corrective rejections BEFORE any
	// scheduling happens.
	if _, err := (setReminder{}).Run(context.Background(), core.Args{"minutes": "5"}); !core.IsValidation(err) {
		t.Fatalf("missing message = %v, want ValidationError", err)
	}
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := (setReminder{}).Run(context.Background(), core.Args{"minutes": "5", "message": string(long)}); !core.IsValidation(err) {
		t.Fatalf("oversized message = %v, want ValidationError", err)
	}
}
