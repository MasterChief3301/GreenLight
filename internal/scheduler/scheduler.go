// Package scheduler runs the background timeout/reminder engine.
package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/MasterChief3301/greenlight/internal/app"
	"github.com/MasterChief3301/greenlight/internal/store"
)

// purgeInterval throttles how often the history purge runs (the retention
// window is coarse, so there's no need to scan more often than this).
const purgeInterval = time.Hour

// Scheduler periodically scans for overdue requests (applying their default
// action), sends reminder pings for still-pending requests, and purges old
// decided history.
type Scheduler struct {
	app       *app.App
	interval  time.Duration
	lastPurge time.Time
}

// New builds a Scheduler.
func New(a *app.App, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &Scheduler{app: a, interval: interval}
}

// Run blocks, ticking until ctx is cancelled. It runs one scan immediately so
// that requests overdue at startup (e.g. after a restart) are handled promptly.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			s.app.Log.Info("scheduler stopping")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick runs one scan: expire overdue requests, then send due reminders.
func (s *Scheduler) tick(ctx context.Context) {
	now := time.Now().UTC()

	overdue, err := s.app.Store.ListOverdue(now)
	if err != nil {
		s.app.Log.Error("scheduler: list overdue", "err", err)
	}
	for _, r := range overdue {
		if _, err := s.app.Expire(r); err != nil {
			if errors.Is(err, store.ErrAlreadyResolved) {
				// A user decided in the same window; nothing to do.
				continue
			}
			s.app.Log.Error("scheduler: expire", "id", r.ID, "err", err)
		}
	}

	if s.app.Cfg.ReminderEnabled {
		s.sendReminders(now)
	}

	s.purgeHistory(now)
}

// purgeHistory deletes decided requests older than the configured retention
// window. It's throttled to run at most once per purgeInterval.
func (s *Scheduler) purgeHistory(now time.Time) {
	retention := s.app.Cfg.HistoryRetention
	if retention <= 0 {
		return // disabled → keep history forever
	}
	if now.Sub(s.lastPurge) < purgeInterval {
		return
	}
	s.lastPurge = now
	cutoff := now.Add(-retention)
	n, err := s.app.Store.DeleteDecidedBefore(cutoff)
	if err != nil {
		s.app.Log.Error("scheduler: purge history", "err", err)
		return
	}
	if n > 0 {
		s.app.Log.Info("purged old history", "deleted", n, "older_than", cutoff.Format(time.RFC3339))
	}
}

// sendReminders pings for pending requests that have passed the reminder point
// (a fraction of their timeout) and haven't yet been reminded.
func (s *Scheduler) sendReminders(now time.Time) {
	pending, err := s.app.Store.ListPending()
	if err != nil {
		s.app.Log.Error("scheduler: list pending", "err", err)
		return
	}
	frac := s.app.Cfg.ReminderFraction
	for _, r := range pending {
		if r.ReminderSentAt != nil {
			continue
		}
		elapsed := now.Sub(r.CreatedAt).Seconds()
		threshold := float64(r.TimeoutSeconds) * frac
		if elapsed >= threshold {
			if err := s.app.SendReminder(r); err != nil {
				s.app.Log.Warn("scheduler: reminder", "id", r.ID, "err", err)
			}
		}
	}
}
