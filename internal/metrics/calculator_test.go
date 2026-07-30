package metrics

import (
	"testing"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

func TestPrecisionOrder(t *testing.T) {
	c := NewCalculator()
	exactInput, exactOutput := int64(10), int64(20)
	exact := c.Measure("ignored", "ignored", "", Usage{
		InputTokens: &exactInput, OutputTokens: &exactOutput,
	})
	if exact.Precision != domain.PrecisionExact || exact.TotalTokens() != 30 {
		t.Fatalf("exact: %#v", exact)
	}
	estimated := c.Measure("hello world", "ok", "unicode_words", Usage{})
	if estimated.Precision != domain.PrecisionEstimated {
		t.Fatalf("estimated: %#v", estimated)
	}
	approximate := c.Measure("hello", "world", "", Usage{})
	if approximate.Precision != domain.PrecisionApproximate {
		t.Fatalf("approximate: %#v", approximate)
	}
}
