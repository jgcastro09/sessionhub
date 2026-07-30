package automation

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

func NextOccurrence(schedule domain.Schedule, after time.Time) (time.Time, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load schedule timezone %q: %w", schedule.Timezone, err)
	}
	localAfter := after.In(location)
	switch schedule.Kind {
	case domain.ScheduleOnce:
		value, err := time.Parse(time.RFC3339, schedule.Spec)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse one-time schedule: %w", err)
		}
		if !value.After(after) {
			return time.Time{}, nil
		}
		return value, nil
	case domain.ScheduleInterval:
		interval, err := time.ParseDuration(schedule.Spec)
		if err != nil || interval <= 0 {
			return time.Time{}, fmt.Errorf("parse positive schedule interval %q", schedule.Spec)
		}
		base := schedule.CreatedAt
		if schedule.LastRun != nil {
			base = *schedule.LastRun
		}
		next := base.Add(interval)
		for !next.After(after) {
			next = next.Add(interval)
		}
		return next, nil
	case domain.ScheduleDaily:
		hour, minute, err := parseClock(schedule.Spec)
		if err != nil {
			return time.Time{}, err
		}
		next := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(),
			hour, minute, 0, 0, location)
		if !next.After(localAfter) {
			next = next.AddDate(0, 0, 1)
		}
		return next, nil
	case domain.ScheduleWeekdays:
		daysPart, clockPart, ok := strings.Cut(schedule.Spec, "@")
		if !ok {
			return time.Time{}, fmt.Errorf("weekday schedule must be Days@HH:MM")
		}
		hour, minute, err := parseClock(clockPart)
		if err != nil {
			return time.Time{}, err
		}
		days, err := parseWeekdays(daysPart)
		if err != nil {
			return time.Time{}, err
		}
		for offset := 0; offset <= 7; offset++ {
			day := localAfter.AddDate(0, 0, offset)
			if !days[day.Weekday()] {
				continue
			}
			candidate := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, location)
			if candidate.After(localAfter) {
				return candidate, nil
			}
		}
		return time.Time{}, fmt.Errorf("no weekday occurrence found")
	default:
		return time.Time{}, fmt.Errorf("unknown schedule kind %q", schedule.Kind)
	}
}

func parseClock(value string) (int, int, error) {
	hourText, minuteText, ok := strings.Cut(value, ":")
	if !ok {
		return 0, 0, fmt.Errorf("clock must use HH:MM")
	}
	hour, errH := strconv.Atoi(hourText)
	minute, errM := strconv.Atoi(minuteText)
	if errH != nil || errM != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid clock %q", value)
	}
	return hour, minute, nil
}

func parseWeekdays(value string) (map[time.Weekday]bool, error) {
	names := map[string]time.Weekday{
		"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday,
		"wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday,
		"sat": time.Saturday,
	}
	out := make(map[time.Weekday]bool)
	for _, part := range strings.Split(value, ",") {
		day, ok := names[strings.ToLower(strings.TrimSpace(part))]
		if !ok {
			return nil, fmt.Errorf("invalid weekday %q", part)
		}
		out[day] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one weekday is required")
	}
	return out, nil
}
