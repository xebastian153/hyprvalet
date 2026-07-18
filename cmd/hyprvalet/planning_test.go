package main

import "testing"

func TestPlanningIntent(t *testing.T) {
	cases := []struct {
		text     string
		wantOK   bool
		wantIdea string
	}{
		{"armemos un proyecto para una tienda online", true, "para una tienda online"},
		{"jarvis, planeemos: un bot de discord", true, "un bot de discord"},
		{"quiero crear un proyecto", true, ""},
		{"let's plan a budget tracker", true, "a budget tracker"},
		{"brainstorm a note-taking app", true, "a note-taking app"},
		{"abre el navegador", false, ""},
		{"qué hora es", false, ""},
	}
	for _, c := range cases {
		idea, ok := planningIntent(c.text)
		if ok != c.wantOK {
			t.Errorf("planningIntent(%q) ok = %v, want %v", c.text, ok, c.wantOK)
			continue
		}
		if ok && idea != c.wantIdea {
			t.Errorf("planningIntent(%q) idea = %q, want %q", c.text, idea, c.wantIdea)
		}
	}
}
