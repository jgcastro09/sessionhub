package automation

import (
	"context"
	"testing"
	"time"
)

func TestNextSimpleOccurrenceNeverBackfills(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("test", -3*60*60)
	defer func() { time.Local = old }()
	after := time.Date(2026, time.July, 30, 15, 5, 0, 0, time.Local)
	next, err := nextSimpleOccurrence(SimpleSchedule{Type: ScheduleDaily, Time: "14:00"}, after)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.July, 31, 14, 0, 0, 0, time.Local)
	if next == nil || !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}

	next, err = nextSimpleOccurrence(SimpleSchedule{Type: ScheduleOnce, Date: "2026-07-30", Time: "14:00"}, after)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Fatalf("past one-time occurrence = %v, want nil", next)
	}
}

func TestSchedulerStartupMarksPastOnceAsMissed(t *testing.T) {
	root := t.TempDir()
	s, err := NewScheduler(context.Background(), root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().In(time.Local).Add(-time.Minute)
	s.items["a"] = SimpleAutomation{
		ID: "a", Name: "past", SessionID: "session", Enabled: true,
		Schedule: SimpleSchedule{Type: ScheduleOnce, Date: past.Format("2006-01-02"), Time: past.Format("15:04")},
		Steps:    []SimpleStep{{ID: "step", ExecutorID: "executor", Prompt: "hello"}},
	}
	if err := s.normalize(time.Now()); err != nil {
		t.Fatal(err)
	}
	item, ok := s.Get("a")
	if !ok {
		t.Fatal("automation missing")
	}
	if item.Enabled || item.Status != StatusMissed || item.LastRun.Status != StatusMissed {
		t.Fatalf("got enabled=%v status=%q last=%q", item.Enabled, item.Status, item.LastRun.Status)
	}
	s.Close()
}
