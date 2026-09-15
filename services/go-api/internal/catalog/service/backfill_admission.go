package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"sync"
	"time"
)

var (
	// ErrBackfillInProgress rejects a featured-artist backfill while the same
	// user already has one in flight: each run can issue thousands of external
	// lookups, so parallel runs for one principal only multiply that load.
	ErrBackfillInProgress = &domain.CodedError{Msg: "featured artist backfill already in progress", Status: 409, Code: "catalog.backfill_in_progress"}
	// ErrBackfillCoolingDown rejects a run started within the cooldown window
	// after the user's previous run finished, so back-to-back calls cannot
	// sustain a continuous stream of external lookups.
	ErrBackfillCoolingDown = &domain.CodedError{Msg: "featured artist backfill ran recently; try again later", Status: 429, Code: "catalog.backfill_cooling_down"}
)

// backfillAdmission enforces, per user, at most one in-flight backfill run and
// a cooldown between the end of one run and the start of the next. The state
// is process-local, matching the single-process modular monolith.
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

// admit reserves the user's single run slot, or returns ErrBackfillInProgress
// while a run is in flight and ErrBackfillCoolingDown within the cooldown. On
// success the caller must call release when the run ends.
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

// release frees the user's slot and starts the cooldown. The cooldown applies
// however the run ended (success, error, or cancellation) so aborting a request
// early cannot be used to skip it.
func (a *backfillAdmission) release(userId shared.UserId) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.running, userId)
	a.lastFinished[userId] = a.now()
}

// pruneExpired drops finished cooldowns so the map stays bounded by the users
// who ran within the last window rather than every user ever seen. Callers
// must hold mu.
func (a *backfillAdmission) pruneExpired() {
	now := a.now()
	for id, finished := range a.lastFinished {
		if now.Sub(finished) >= a.cooldown {
			delete(a.lastFinished, id)
		}
	}
}
