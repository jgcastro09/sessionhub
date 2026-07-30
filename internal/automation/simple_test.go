package automation

import (
	"context"
	"strings"
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

func TestSchedulerMarksTransientFailureForRetry(t *testing.T) {
	s, err := NewScheduler(context.Background(), t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.items["a"] = SimpleAutomation{ID: "a", Status: StatusRunning}
	s.setRetrying("a", 1, 3, context.DeadlineExceeded)
	item, ok := s.Get("a")
	if !ok {
		t.Fatal("automation missing")
	}
	if item.Status != StatusRunning || item.CurrentStep != 1 || item.LastRun.Status != StatusRunning || item.LastRun.RetryCount != 3 {
		t.Fatalf("unexpected retry state: %#v", item)
	}
	if !strings.Contains(item.LastRun.Error, "Retrying step 1") {
		t.Fatalf("retry cause not shown: %q", item.LastRun.Error)
	}
	s.Close()
}

func TestSchedulerKeepsBoundedLiveOutput(t *testing.T) {
	s, err := NewScheduler(context.Background(), t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.items["a"] = SimpleAutomation{ID: "a", Status: StatusRunning}
	s.setLiveOutput("a", strings.Repeat("x", 1000))
	item, ok := s.Get("a")
	if !ok {
		t.Fatal("automation missing")
	}
	if item.Activity == "" || len(item.LiveOutput) != 900 {
		t.Fatalf("unexpected live state: activity=%q output=%d", item.Activity, len(item.LiveOutput))
	}
	s.Close()
}

func TestSchedulerSanitizesTerminalColorsBeforeSavingHistory(t *testing.T) {
	s, err := NewScheduler(context.Background(), t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.items["a"] = SimpleAutomation{ID: "a", Status: StatusRunning}
	s.setLiveOutput("a", "\x1b[38;2;255;255;255mwhite text\x1b[48;5;0mblack background\x1b[0m\r\nanswer")
	item, ok := s.Get("a")
	if !ok {
		t.Fatal("automation missing")
	}
	if strings.Contains(item.LiveOutput, "\x1b") || item.LiveOutput != "white textblack background\nanswer" {
		t.Fatalf("terminal output was not converted to safe plain text: %q", item.LiveOutput)
	}
	s.Close()
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
