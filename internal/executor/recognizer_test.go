package executor

import (
	"testing"
	"time"

	"github.com/nodestage/sessionhub/internal/domain"
)

func TestRecognitionAmbiguityPauses(t *testing.T) {
	rules := []domain.RecognitionRule{
		{ID: "ok", Name: "ok", Kind: domain.RecognizeLiteral, Value: "DONE", Outcome: domain.StateSucceeded, Priority: 10},
		{ID: "bad", Name: "bad", Kind: domain.RecognizePattern, Value: "DONE", Outcome: domain.StateFailed, Priority: 10},
	}
	got := RecognizeOutput(rules, "DONE")
	if !got.Matched || !got.Ambiguous || got.Outcome != domain.StateWaiting {
		t.Fatalf("unexpected recognition: %#v", got)
	}
}

func TestStableRecognition(t *testing.T) {
	rules := []domain.RecognitionRule{{
		ID: "stable", Name: "stable", Kind: domain.RecognizeStable,
		Value: "2s", Outcome: domain.StateSucceeded,
	}}
	if got := RecognizeStable(rules, time.Second); got.Matched {
		t.Fatal("matched before stability period")
	}
	if got := RecognizeStable(rules, 3*time.Second); !got.Matched {
		t.Fatal("did not match after stability period")
	}
}
