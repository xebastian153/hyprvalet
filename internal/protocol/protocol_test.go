package protocol

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

func TestRequestResponseRoundTrip(t *testing.T) {
	req := Request{Op: OpRun, Cap: "workspace.switch", Args: map[string]string{"workspace": "3"}}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if got.Op != OpRun || got.Cap != "workspace.switch" || got.Args["workspace"] != "3" {
		t.Fatalf("request round-trip lost data: %+v", got)
	}

	resp := Response{Status: StatusRan, Text: "switched to workspace 3"}
	data, err = json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var gotResp Response
	if err := json.Unmarshal(data, &gotResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if gotResp.Status != StatusRan || gotResp.Text != "switched to workspace 3" {
		t.Fatalf("response round-trip lost data: %+v", gotResp)
	}
}

// fakeCap is a minimal core.Capability for CapInfoOf.
type fakeCap struct{}

func (fakeCap) ID() string                                     { return "demo.thing" }
func (fakeCap) Description() string                            { return "Do a thing" }
func (fakeCap) Access() core.AccessKind                        { return core.AccessCommand }
func (fakeCap) Risk() core.Risk                                { return core.RiskConfirm }
func (fakeCap) Params() []string                               { return []string{"a", "b"} }
func (fakeCap) Run(context.Context, core.Args) (string, error) { return "", nil }

func TestCapInfoOf(t *testing.T) {
	info := CapInfoOf(fakeCap{})
	if info.ID != "demo.thing" || info.Access != "command" || info.Risk != "confirm" {
		t.Fatalf("CapInfoOf flattened wrong: %+v", info)
	}
	if len(info.Params) != 2 {
		t.Fatalf("params lost: %+v", info.Params)
	}
}
