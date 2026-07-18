package prompt

import (
	"encoding/json"
	"strings"
)

// Watch states classify what a watched Claude Code terminal is doing right now.
const (
	WatchWorking    = "working"    // busy — thinking, typing, running; nothing to do
	WatchDesign     = "design"     // asking an open-ended design question we can answer
	WatchPermission = "permission" // asking to DO something with consequences — the user's call
	WatchDone       = "done"       // finished, or idle with nothing pending
)

// WatchTurn is the assistant's read of the terminal: the state, the question
// Claude is asking (if any), and — for a design question only — the answer to
// relay, grounded in the plan.
type WatchTurn struct {
	State    string
	Question string
	Answer   string
}

// BuildWatch is the system prompt for watching Claude Code work on the project.
// Its single most important job is SAFETY: it must never classify a permission
// request as a design question, so the assistant never approves a consequential
// action on the user's behalf. The default when unsure is "permission".
func BuildWatch(plan string) string {
	var b strings.Builder
	b.WriteString(persona)
	b.WriteString(nowLine())
	b.WriteString("You are watching a Claude Code terminal build a project you planned with the user. Here is the plan you agreed on:\n")
	b.WriteString(strings.TrimSpace(plan))
	b.WriteString("\n\nYou are given the terminal's current content. Classify what Claude is doing right now and respond with only a JSON object shaped {\"state\": \"\", \"question\": \"\", \"answer\": \"\"}.\n")
	b.WriteString("state is exactly one of:\n")
	b.WriteString("- \"working\": Claude is thinking, typing, or running something — it is NOT waiting for input. Leave question and answer empty.\n")
	b.WriteString("- \"design\": Claude is asking an OPEN-ENDED design or clarification question about HOW to build (naming, structure, which approach, a preference) that the plan lets you answer. It is NOT asking to perform an action. Put its question in \"question\" and, in \"answer\", the reply to send — grounded in the plan, in the user's language, one or two sentences.\n")
	b.WriteString("- \"permission\": Claude is asking to DO something with consequences — run a shell command, create, edit, or delete files, install packages, trust a folder, or ANY yes/no or numbered approval prompt. Put a short description in \"question\"; leave \"answer\" empty. You must NEVER answer these — the human decides.\n")
	b.WriteString("- \"done\": Claude finished the task, or is idle at an empty prompt with nothing pending.\n")
	b.WriteString("Decisive rule: if you are not certain a question is a harmless open-ended design question, classify it as \"permission\". When in doubt, it is permission.\n")
	return b.String()
}

// ParseWatch turns the classifier's reply into a WatchTurn. It is defensive and
// fails SAFE: anything it cannot read cleanly, or any unrecognized state, is
// treated as "working" so the assistant does nothing rather than acting on a
// misread. A "design" turn with no answer is downgraded to "working" too — there
// is nothing safe to relay.
func ParseWatch(content string) WatchTurn {
	content = strings.TrimSpace(content)
	var parsed struct {
		State    string `json:"state"`
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return WatchTurn{State: WatchWorking}
	}
	state := strings.ToLower(strings.TrimSpace(parsed.State))
	turn := WatchTurn{
		State:    state,
		Question: strings.TrimSpace(parsed.Question),
		Answer:   strings.TrimSpace(parsed.Answer),
	}
	switch state {
	case WatchDesign:
		if turn.Answer == "" {
			return WatchTurn{State: WatchWorking} // nothing safe to say
		}
		return turn
	case WatchPermission, WatchDone:
		return turn
	default:
		return WatchTurn{State: WatchWorking}
	}
}
