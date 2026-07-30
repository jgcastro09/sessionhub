package automation

import (
	"testing"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

func TestBudgetStopsAtLimit(t *testing.T) {
	err := CheckBudget(domain.Budget{TotalTokens: 100}, Consumption{
		InputTokens: 60, OutputTokens: 40,
	})
	if err == nil {
		t.Fatal("expected total token budget violation at exact limit")
	}
	if err := CheckBudget(domain.Budget{TotalTokens: 101}, Consumption{
		InputTokens: 60, OutputTokens: 40,
	}); err != nil {
		t.Fatal(err)
	}
}
