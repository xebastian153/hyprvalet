// Package web adds browsing capabilities: open a URL, and search the web. Both
// hand a single URL to the desktop's default handler as an argument vector —
// never through a shell — so a page address or a search phrase can carry no
// command with it.
package web

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/xebastian153/hyprvalet/internal/core"
)

func envSearch() string { return os.Getenv("HYPRVALET_SEARCH_URL") }

// Capabilities returns every web capability.
func Capabilities() []core.Capability {
	return []core.Capability{openURL{}, searchWeb{}}
}

// searchTemplate is the search URL; %s is the URL-encoded query. Override with
// HYPRVALET_SEARCH_URL (e.g. a DuckDuckGo or private-instance endpoint).
const searchTemplate = "https://www.google.com/search?q=%s"

// openDetached launches the default handler for a URL and returns at once. Like
// an app launcher, the browser outlives the request, so it is fire-and-forget:
// detached from the request context and reaped in the background so it leaves
// no zombie. The URL is a single argument — no shell parses it.
func openDetached(rawURL string) error {
	cmd := exec.Command("xdg-open", rawURL)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("xdg-open: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// openURL opens a web address in the default browser.
type openURL struct{}

func (openURL) ID() string              { return "web.open" }
func (openURL) Description() string     { return "Open a URL in the default web browser" }
func (openURL) Access() core.AccessKind { return core.AccessApp }
func (openURL) Risk() core.Risk         { return core.RiskSafe }
func (openURL) Params() []string        { return []string{"url"} }
func (openURL) Run(_ context.Context, args core.Args) (string, error) {
	u, err := normalizeURL(args["url"])
	if err != nil {
		return "", err
	}
	if err := openDetached(u); err != nil {
		return "", err
	}
	return fmt.Sprintf("opened %s", u), nil
}

// normalizeURL validates a spoken web address and returns a well-formed URL. A
// missing scheme becomes https. A value with spaces is not a URL — it is a
// search phrase, and the corrective error says so, so the model can retry with
// web.search instead.
func normalizeURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", core.Validationf("missing required arg %q (a web address like youtube.com)", "url")
	}
	if strings.ContainsAny(s, " \t") {
		return "", core.Validationf("arg %q looks like a search phrase, not an address — use web.search for %q", "url", s)
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	parsed, err := url.Parse(s)
	if err != nil || parsed.Host == "" || !strings.Contains(parsed.Host, ".") {
		return "", core.Validationf("arg %q is not a valid web address, got %q", "url", raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", core.Validationf("arg %q must be an http or https address, got scheme %q", "url", parsed.Scheme)
	}
	return parsed.String(), nil
}

// searchWeb opens a web search for a phrase.
type searchWeb struct{}

func (searchWeb) ID() string              { return "web.search" }
func (searchWeb) Description() string     { return "Search the web for a phrase in the default browser" }
func (searchWeb) Access() core.AccessKind { return core.AccessApp }
func (searchWeb) Risk() core.Risk         { return core.RiskSafe }
func (searchWeb) Params() []string        { return []string{"query"} }
func (searchWeb) Run(_ context.Context, args core.Args) (string, error) {
	q := strings.TrimSpace(args["query"])
	if q == "" {
		return "", core.Validationf("missing required arg %q (what to search for)", "query")
	}
	target := fmt.Sprintf(searchTemplateFromEnv(), url.QueryEscape(q))
	if err := openDetached(target); err != nil {
		return "", err
	}
	return fmt.Sprintf("searched the web for %q", q), nil
}

func searchTemplateFromEnv() string {
	if t := strings.TrimSpace(envSearch()); t != "" && strings.Contains(t, "%s") {
		return t
	}
	return searchTemplate
}
