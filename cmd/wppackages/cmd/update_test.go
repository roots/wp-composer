package cmd

import (
	"testing"
	"time"
)

func TestShouldAdvanceSyncedAt(t *testing.T) {
	now := time.Now().UTC()

	t.Run("versions changed", func(t *testing.T) {
		committed := now.Add(-5 * time.Minute)
		got := shouldAdvanceSyncedAt(`{"1.0":"url"}`, `{}`, &committed, now)
		if got != syncAdvance {
			t.Errorf("got %d, want syncAdvance", got)
		}
	})

	t.Run("versions unchanged within window", func(t *testing.T) {
		committed := now.Add(-10 * time.Minute)
		got := shouldAdvanceSyncedAt(`{"1.0":"url"}`, `{"1.0":"url"}`, &committed, now)
		if got != syncRetry {
			t.Errorf("got %d, want syncRetry", got)
		}
	})

	t.Run("versions unchanged after window", func(t *testing.T) {
		committed := now.Add(-25 * time.Hour)
		got := shouldAdvanceSyncedAt(`{"1.0":"url"}`, `{"1.0":"url"}`, &committed, now)
		if got != syncExpire {
			t.Errorf("got %d, want syncExpire", got)
		}
	})

	t.Run("versions unchanged nil last_committed", func(t *testing.T) {
		got := shouldAdvanceSyncedAt(`{"1.0":"url"}`, `{"1.0":"url"}`, nil, now)
		if got != syncExpire {
			t.Errorf("got %d, want syncExpire", got)
		}
	})

	t.Run("versions unchanged at window boundary", func(t *testing.T) {
		committed := now.Add(-staleRetryWindow)
		got := shouldAdvanceSyncedAt(`{"1.0":"url"}`, `{"1.0":"url"}`, &committed, now)
		if got != syncRetry {
			t.Errorf("got %d, want syncRetry (at boundary)", got)
		}
	})

	t.Run("versions unchanged just past window", func(t *testing.T) {
		committed := now.Add(-staleRetryWindow - 1*time.Second)
		got := shouldAdvanceSyncedAt(`{"1.0":"url"}`, `{"1.0":"url"}`, &committed, now)
		if got != syncExpire {
			t.Errorf("got %d, want syncExpire (just past window)", got)
		}
	})
}
