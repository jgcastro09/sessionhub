package domain

import "testing"

func TestStateTransitions(t *testing.T) {
	tests := []struct {
		from, to State
		ok       bool
	}{
		{StatePending, StateRunning, true},
		{StateRunning, StateSucceeded, true},
		{StateSucceeded, StateRunning, false},
		{StateInterrupted, StatePending, true},
		{StateCanceled, StateRunning, false},
	}
	for _, tt := range tests {
		err := ValidateTransition(tt.from, tt.to)
		if (err == nil) != tt.ok {
			t.Fatalf("%s -> %s: got error %v, want ok=%v", tt.from, tt.to, err, tt.ok)
		}
	}
}

func TestExecutorRedaction(t *testing.T) {
	cfg := ExecutorConfig{Environment: []SecretEnv{
		{Name: "VISIBLE", Value: "one"},
		{Name: "TOKEN", Value: "secret", Secret: true},
	}}
	got := cfg.Redacted()
	if got.Environment[0].Value != "one" || got.Environment[1].Value != "***" {
		t.Fatalf("unexpected redaction: %#v", got.Environment)
	}
	if cfg.Environment[1].Value != "secret" {
		t.Fatal("redaction mutated original")
	}
}
