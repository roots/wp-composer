package packages

import (
	"fmt"
	"time"
)

// TimeNow is the clock function, replaceable in tests.
var TimeNow = time.Now

// ComputeProviderGroup assigns a provider group based on the last_committed date.
//
// Rules:
//   - nil               → "unknown"
//   - This week (≥ Monday 00:00 UTC) → "this-week"
//   - Current year Q1 (Jan-Mar)      → "{year}-03"
//   - Current year Q2 (Apr-Jun)      → "{year}-06"
//   - Current year Q3 (Jul-Sep)      → "{year}-09"
//   - Current year Q4 (Oct-Dec)      → "{year}-12"
//   - Previous years ≥ 2011          → "{year}"
//   - Before 2011                    → "old"
func ComputeProviderGroup(lastCommitted *time.Time) string {
	if lastCommitted == nil {
		return "unknown"
	}

	now := TimeNow().UTC()
	lc := lastCommitted.UTC()

	// This week: between Monday 00:00 UTC and now
	monday := startOfWeek(now)
	if !lc.Before(monday) && !lc.After(now) {
		return "this-week"
	}

	year := lc.Year()
	currentYear := now.Year()

	if year == currentYear {
		return fmt.Sprintf("%d-%02d", year, quarterEnd(lc.Month()))
	}

	if year >= 2011 {
		return fmt.Sprintf("%d", year)
	}

	return "old"
}

func startOfWeek(t time.Time) time.Time {
	weekday := t.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	offset := int(weekday) - int(time.Monday)
	return time.Date(t.Year(), t.Month(), t.Day()-offset, 0, 0, 0, 0, time.UTC)
}

func quarterEnd(m time.Month) int {
	switch {
	case m <= time.March:
		return 3
	case m <= time.June:
		return 6
	case m <= time.September:
		return 9
	default:
		return 12
	}
}
