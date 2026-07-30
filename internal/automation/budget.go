package automation

import (
	"fmt"
	"time"

	"github.com/nodestage/sessionhub/internal/domain"
)

type Consumption struct {
	InputTokens   int64
	OutputTokens  int64
	CostMicrosUSD int64
	Duration      time.Duration
	Attempts      int
	Cycles        int
	Concurrency   int
}

func CheckBudget(budget domain.Budget, used Consumption) error {
	checks := []struct {
		name  string
		limit int64
		value int64
	}{
		{"input tokens", budget.InputTokens, used.InputTokens},
		{"output tokens", budget.OutputTokens, used.OutputTokens},
		{"total tokens", budget.TotalTokens, used.InputTokens + used.OutputTokens},
		{"estimated API-equivalent cost", budget.CostMicrosUSD, used.CostMicrosUSD},
		{"attempts", int64(budget.Attempts), int64(used.Attempts)},
		{"cycles", int64(budget.Cycles), int64(used.Cycles)},
		{"concurrency", int64(budget.Concurrency), int64(used.Concurrency)},
	}
	for _, check := range checks {
		if check.limit > 0 && check.value >= check.limit {
			return fmt.Errorf("%s budget reached: %d/%d", check.name, check.value, check.limit)
		}
	}
	if budget.Duration > 0 && used.Duration >= budget.Duration {
		return fmt.Errorf("duration budget reached: %s/%s", used.Duration, budget.Duration)
	}
	return nil
}
