package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"sync"
	"time"
)

var (
	ErrBackfillInProgress  = &domain.CodedError{Msg: "featured artist backfill already in progress", Status: 409, Code: "catalog.backfill_in_progress"}
	ErrBackfillCoolingDown = &domain.CodedError{Msg: "featured artist backfill ran recently; try again later", Status: 429, Code: "catalog.backfill_cooling_down"}
)

type backfillAdmission struct {
	cooldown time.Duration
	now      func() time.Time

	mu           sync.Mutex
	running      map[shared.UserId]struct{}
	lastFinished map[shared.UserId]time.Time
}

func newBackfillAdmission(cooldown time.Duration, now func() time.Time) *backfillAdmission {
	return &backfillAdmission{
		cooldown:     cooldown,
		now:          now,
		running:      make(map[shared.UserId]struct{}),
		lastFinished: make(map[shared.UserId]time.Time),
	}
}

func (a *backfillAdmission) admit(userId shared.UserId) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneExpired()
	if _, busy := a.running[userId]; busy {
		return ErrBackfillInProgress
	}
	if _, recent := a.lastFinished[userId]; recent {
		return ErrBackfillCoolingDown
	}
	a.running[userId] = struct{}{}
	return nil
}

func (a *backfillAdmission) release(userId shared.UserId) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.running, userId)
	a.lastFinished[userId] = a.now()
}

func (a *backfillAdmission) pruneExpired() {
	now := a.now()
	for id, finished := range a.lastFinished {
		if now.Sub(finished) >= a.cooldown {
			delete(a.lastFinished, id)
		}
	}
}
