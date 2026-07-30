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

func TestVoiceButtonBoundsAndState(t *testing.T) {
	m := Model{width: 100}
	start, end, ok := m.voiceButtonBounds()
	if !ok || start >= end || !m.voiceButtonAt(start, 0) || m.voiceButtonAt(end, 0) {
		t.Fatalf("invalid microphone button bounds: start=%d end=%d ok=%v", start, end, ok)
	}
	if got, want := m.voiceButtonLabel(), " 🎙 MICROFONE "; got != want {
		t.Fatalf("idle microphone label = %q, want %q", got, want)
	}
	m.recording = true
	if got, want := m.voiceButtonLabel(), " ■ PARAR "; got != want {
		t.Fatalf("recording microphone label = %q, want %q", got, want)
	}
	m.width = 10
	if _, _, ok := m.voiceButtonBounds(); ok {
		t.Fatal("microphone button should be hidden in a narrow terminal")
	}
}
