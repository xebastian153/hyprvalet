package main

import "testing"

func TestStripWake(t *testing.T) {
	wakes := map[string]bool{"jarvis": true}
	tests := []struct {
		name, text, rest string
		woken            bool
	}{
		{"name plus command", "Jarvis, abrí el navegador", "abrí el navegador", true},
		{"name alone", "¡Jarvis!", "", true},
		{"hey prefix", "Hey Jarvis abrí la música", "abrí la música", true},
		{"not addressed", "abrí el navegador", "", false},
		{"name buried later is not a wake", "me dijo que jarvis es una película", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rest, woken := stripWake(tt.text, wakes)
			if woken != tt.woken || rest != tt.rest {
				t.Fatalf("stripWake(%q) = (%q, %v), want (%q, %v)", tt.text, rest, woken, tt.rest, tt.woken)
			}
		})
	}
}

func TestWakeWordsAlternates(t *testing.T) {
	t.Setenv("HYPRVALET_WAKE_WORD", "jarvis, Yarvis ,charvis")
	set := wakeWords()
	for _, w := range []string{"jarvis", "yarvis", "charvis"} {
		if !set[w] {
			t.Fatalf("wake set %v missing %q", set, w)
		}
	}
}
