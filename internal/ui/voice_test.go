package ui

import "testing"

func TestVoiceTranscriptDelta(t *testing.T) {
	tests := []struct {
		name     string
		previous string
		current  string
		want     string
	}{
		{name: "first live result", current: "hello world", want: "hello world"},
		{name: "appends only new words", previous: "hello world", current: "hello world from Session Hub", want: " from Session Hub"},
		{name: "does not repeat a revised prefix", previous: "hello word", current: "hello world from Session Hub", want: " from Session Hub"},
		{name: "does not duplicate a shorter result", previous: "hello world", current: "hello", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := voiceTranscriptDelta(test.previous, test.current); got != test.want {
				t.Fatalf("voiceTranscriptDelta(%q, %q) = %q, want %q", test.previous, test.current, got, test.want)
			}
		})
	}
}
