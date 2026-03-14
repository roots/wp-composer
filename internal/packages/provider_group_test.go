package packages

import (
	"testing"
	"time"
)

func TestComputeProviderGroup(t *testing.T) {
	// Fix "now" to 2026-03-13 (Friday) for deterministic tests.
	original := TimeNow
	TimeNow = func() time.Time { return time.Date(2026, 3, 13, 12, 0, 0, 0, time.UTC) }
	defer func() { TimeNow = original }()

	tests := []struct {
		name string
		date *time.Time
		want string
	}{
		{"nil date", nil, "unknown"},
		{"this week monday", tp(2026, 3, 9, 1, 0, 0), "this-week"},
		{"this week today", tp(2026, 3, 13, 10, 0, 0), "this-week"},
		{"last week sunday", tp(2026, 3, 8, 23, 59, 59), "2026-03"},
		{"current year Q1 jan", tp(2026, 1, 15, 0, 0, 0), "2026-03"},
		{"current year Q1 mar", tp(2026, 3, 1, 0, 0, 0), "2026-03"},
		{"current year Q2", tp(2026, 4, 15, 0, 0, 0), "2026-06"},
		{"current year Q3", tp(2026, 7, 1, 0, 0, 0), "2026-09"},
		{"current year Q4", tp(2026, 10, 1, 0, 0, 0), "2026-12"},
		{"previous year 2025", tp(2025, 6, 15, 0, 0, 0), "2025"},
		{"year 2011", tp(2011, 1, 1, 0, 0, 0), "2011"},
		{"year 2010", tp(2010, 12, 31, 0, 0, 0), "old"},
		{"year 2005", tp(2005, 5, 1, 0, 0, 0), "old"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeProviderGroup(tt.date)
			if got != tt.want {
				t.Errorf("ComputeProviderGroup(%v) = %q, want %q", tt.date, got, tt.want)
			}
		})
	}
}

func tp(year int, month time.Month, day, hour, min, sec int) *time.Time {
	t := time.Date(year, month, day, hour, min, sec, 0, time.UTC)
	return &t
}
