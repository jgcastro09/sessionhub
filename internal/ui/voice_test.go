package ui

import (
	"testing"

	"github.com/jgcastro09/sessionhub/internal/voice"
)

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

func TestFormatVoiceProgress(t *testing.T) {
	if got, want := formatVoiceProgress(voice.Progress{Stage: "Whisper model", Current: 50 << 20, Total: 100 << 20}), "Whisper model: 50% (50.0 / 100.0 MB)"; got != want {
		t.Fatalf("formatVoiceProgress() = %q, want %q", got, want)
	}
	if got, want := formatVoiceProgress(voice.Progress{Stage: "Starting local Whisper server"}), "Starting local Whisper server"; got != want {
		t.Fatalf("formatVoiceProgress() = %q, want %q", got, want)
	}
}
