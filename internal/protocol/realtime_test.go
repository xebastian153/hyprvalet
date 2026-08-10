package protocol

import (
	"encoding/json"
	"testing"
)

func TestOpRealtimeExists(t *testing.T) {
	if OpRealtime != "realtime" {
		t.Fatalf("OpRealtime = %q, want %q", OpRealtime, "realtime")
	}
}

func TestRequestGenerationRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		gen  int
	}{
		{"zero generation", 0},
		{"positive generation", 3},
		{"large generation", 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Request{Op: OpRealtime, Generation: tt.gen, Text: "hello"}
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got Request
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Generation != tt.gen {
				t.Fatalf("Generation round-trip = %d, want %d", got.Generation, tt.gen)
			}
			if got.Op != OpRealtime {
				t.Fatalf("Op = %q, want realtime", got.Op)
			}
		})
	}
}

func TestRealtimeStatusesExist(t *testing.T) {
	// Streaming lifecycle statuses for realtime sidecar.
	if StatusStreaming != "streaming" {
		t.Fatalf("StatusStreaming = %q, want %q", StatusStreaming, "streaming")
	}
	if StatusCancelled != "cancelled" {
		t.Fatalf("StatusCancelled = %q, want %q", StatusCancelled, "cancelled")
	}
	if StatusQuarantined != "quarantined" {
		t.Fatalf("StatusQuarantined = %q, want %q", StatusQuarantined, "quarantined")
	}
}

func TestResponseGenerationRoundTrip(t *testing.T) {
	resp := Response{Status: StatusStreaming, Generation: 5, Text: "streaming"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Response
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Generation != 5 {
		t.Fatalf("Response Generation = %d, want 5", got.Generation)
	}
	if got.Status != StatusStreaming {
		t.Fatalf("Status = %q, want streaming", got.Status)
	}
}
