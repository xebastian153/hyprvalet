package realtime

import (
	"encoding/json"
	"strings"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// ToolChoiceAuto is the prompt-safe tool_choice for session.update.
// "auto" lets the model choose, but Go double-checks via ResolveToolCall.
// "required" would force a tool call even for greetings — unsafe.
const ToolChoiceAuto = "auto"

// RealtimeTool is the JSON shape for OpenAI Realtime `session.update` tools.
// Each maps 1:1 from a Registry capability via signature_from_schema.
type RealtimeTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolsFromRegistry maps every registered capability to a Realtime function tool.
// Param schemas are derived from cap.Params(): each param is a string property,
// required, with additionalProperties false. Sorted by registry order (already sorted).
func ToolsFromRegistry(reg *core.Registry) []RealtimeTool {
	if reg == nil {
		return nil
	}
	caps := reg.List()
	out := make([]RealtimeTool, 0, len(caps))
	for _, c := range caps {
		schema := parametersSchema(c.Params())
		out = append(out, RealtimeTool{
			Type:        "function",
			Name:        c.ID(),
			Description: c.Description(),
			Parameters:  schema,
		})
	}
	return out
}

type jsonSchema struct {
	Type                 string         `json:"type"`
	Properties           map[string]any `json:"properties"`
	Required             []string       `json:"required"`
	AdditionalProperties bool           `json:"additionalProperties"`
}

// parametersSchema builds a strict JSON schema for the given params.
// Each param is a string type property, required, no additional props.
func parametersSchema(params []string) json.RawMessage {
	props := make(map[string]any, len(params))
	req := make([]string, 0, len(params))
	for _, p := range params {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		props[p] = map[string]string{
			"type":        "string",
			"description": p,
		}
		req = append(req, p)
	}
	if req == nil {
		req = []string{}
	}
	if props == nil {
		props = map[string]any{}
	}
	s := jsonSchema{
		Type:                 "object",
		Properties:           props,
		Required:             req,
		AdditionalProperties: false,
	}
	b, _ := json.Marshal(s)
	return json.RawMessage(b)
}

// ResolveToolCall validates a realtime function_call against the allowlist.
// It is the second half of double allowlisting: sidecar tool_choice limits
// what the model CAN call, Registry.Get limits what Go WILL run.
// Hallucinated names return a Validationf error (retryable corrective text),
// never a plain error, so the caller can feed it back via item.create.
func ResolveToolCall(reg *core.Registry, name string, argsJSON json.RawMessage) (core.Capability, core.Args, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil, core.Validationf("model chose empty capability — no capability matched the request")
	}
	cap, ok := reg.Get(trimmed)
	if !ok {
		return nil, nil, core.Validationf("model chose %q, which is not a registered capability", trimmed)
	}
	// Parse args: must be JSON object with string values.
	// Empty or null JSON means no args.
	if len(argsJSON) == 0 || strings.TrimSpace(string(argsJSON)) == "" || strings.TrimSpace(string(argsJSON)) == "null" {
		return cap, core.Args{}, nil
	}
	// Allow both {"k":"v"} and {}.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(argsJSON, &raw); err != nil {
		return nil, nil, core.Validationf("tool %q args is not a JSON object: %v", trimmed, err)
	}
	args := make(core.Args, len(raw))
	for k, v := range raw {
		// Try string directly.
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			args[k] = s
			continue
		}
		// Fallback: marshal raw then trim quotes? But strict: Validationf if not string.
		// Attempt to unmarshal as stringified value via fmt.
		var anyVal any
		if err := json.Unmarshal(v, &anyVal); err == nil {
			// Coerce non-string to string for permissive validation; but note.
			// Safer to require string per schema, so Validationf if not string.
			return nil, nil, core.Validationf("tool %q arg %q must be a string, got %s", trimmed, k, string(v))
		}
		return nil, nil, core.Validationf("tool %q arg %q invalid: %s", trimmed, k, string(v))
	}
	return cap, args, nil
}
