package automation

import (
	"testing"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

func TestNextDailyOccurrence(t *testing.T) {
	after := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	got, err := NextOccurrence(domain.Schedule{
		Kind: domain.ScheduleDaily, Spec: "13:30", Timezone: "UTC",
	}, after)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 30, 13, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestIntervalRejectsZero(t *testing.T) {
	_, err := NextOccurrence(domain.Schedule{
		Kind: domain.ScheduleInterval, Spec: "0s", Timezone: "UTC",
	}, time.Now())
	if err == nil {
		t.Fatal("expected invalid interval")
	}
}
