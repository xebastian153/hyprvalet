package prompt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BuildReport is the system prompt for the completion report: once Claude Code
// has finished building the project, the assistant tells the user how it went.
// It is grounded in the final terminal so it reports what actually happened
// rather than what the plan hoped for.
func BuildReport(plan, projectDir string) string {
	var b strings.Builder
	b.WriteString(persona)
	b.WriteString(nowLine())
	b.WriteString("A project you planned with the user has just been built by Claude Code. This was the plan:\n")
	b.WriteString(strings.TrimSpace(plan))
	if strings.TrimSpace(projectDir) != "" {
		fmt.Fprintf(&b, "\nThe project lives at: %s\n", strings.TrimSpace(projectDir))
	}
	b.WriteString("\nYou are given the final terminal content. Report to the user how it went, in two or three short sentences, spoken in the user's language: what was built, anything notable that came up, and where to find it.\n")
	b.WriteString("Be concrete and grounded in the terminal — never invent a result it does not show; if something is unclear or unfinished, say so plainly.\n")
	b.WriteString("Respond with only a JSON object shaped {\"report\": \"\"}.\n")
	return b.String()
}

// ParseReport pulls the spoken report out of the model's reply. If it is not the
// expected JSON, the trimmed raw text is used — a report is plain prose, so raw
// output is still usable rather than a failure.
func ParseReport(content string) string {
	content = strings.TrimSpace(content)
	var parsed struct {
		Report string `json:"report"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err == nil {
		if r := strings.TrimSpace(parsed.Report); r != "" {
			return r
		}
	}
	return content
}
