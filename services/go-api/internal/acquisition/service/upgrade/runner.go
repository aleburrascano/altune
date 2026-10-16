package upgrade

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var ErrKept = errors.New("upgrade kept the current file")

const settleBudget = 10 * time.Second

type Config struct {
	DryRun   bool
	MaxTries int
}

type Attempt struct {
	SourceKey string
	Grade     grade.Grade
	NoSource  bool
}

type Replacer interface {
	ReplaceGated(ctx context.Context, userId shared.UserId, track *ports.TrackSnapshot, gate Gate) (Attempt, error)
}

type Runner struct {
	ledger   ports.TrackLedger
	replacer Replacer
	store    ports.UpgradeStore
	cfg      Config
	inPlace  *inPlaceCheck
}

func NewRunner(ledger ports.TrackLedger, replacer Replacer, store ports.UpgradeStore, cfg Config) *Runner {
	return &Runner{ledger: ledger, replacer: replacer, store: store, cfg: cfg}
}

type outcome string

const (
	upgraded       outcome = "upgraded"
	wouldUpgrade   outcome = "would_upgrade"
	keptCurrent    outcome = "kept_current"
	noBetterSource outcome = "no_better_source"
	failed         outcome = "failed"
)

func outcomeOf(attempt Attempt, err error) outcome {
	switch {
	case err == nil:
		return upgraded
	case errors.Is(err, ErrWouldUpgrade):
		return wouldUpgrade
	case errors.Is(err, ErrNotAnUpgrade):
		return keptCurrent
	case attempt.NoSource:
		return noBetterSource
	default:
		return failed
	}
}

func (o outcome) countsAsTry() bool { return o == keptCurrent || o == noBetterSource }

func (r *Runner) ExecuteUpgrade(ctx context.Context, userId shared.UserId, trackId shared.TrackId) error {
	track, err := r.ledger.Get(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if track == nil {
		slog.WarnContext(ctx, "acquisition.upgrade_track_not_found", "track_id", trackId.String())
		return nil
	}
	stored := inPlaceEvidenceOf(track.Evidence)
	current := stored.Grade
	if !isPlayableFile(track) || r.isSettled(track, current) {
		slog.InfoContext(ctx, "acquisition.upgrade_skipped", "track_id", trackId.String())
		return nil
	}
	if r.isUnchecked(track) {
		checked, err := r.checkInPlace(ctx, track, stored)
		if err != nil || checked.settled {
			return err
		}
		current = checked.grade
	}
	if isUserUpload(track) {
		slog.InfoContext(ctx, "acquisition.upgrade_skipped", "track_id", trackId.String())
		return nil
	}
	gate := Gate{Current: current, DryRun: r.cfg.DryRun}
	attempt, err := r.replacer.ReplaceGated(ctx, userId, track, gate)
	return r.settle(ctx, trackId, gate, attempt, err)
}

func (r *Runner) settle(ctx context.Context, trackId shared.TrackId, gate Gate, attempt Attempt, runErr error) error {
	result := outcomeOf(attempt, runErr)
	slog.InfoContext(ctx, "acquisition.upgrade_outcome",
		"track_id", trackId.String(),
		"outcome", string(result),
		"from", string(gate.Current),
		"to", string(attempt.Grade),
		"dry_run", gate.DryRun)
	if result == upgraded || ctx.Err() != nil {
		return runErr
	}
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleBudget)
	defer cancel()
	r.record(settleCtx, trackId, gate, attempt, result)
	if result == failed {
		return runErr
	}
	return fmt.Errorf("%w: %s", ErrKept, result)
}

func (r *Runner) record(ctx context.Context, trackId shared.TrackId, gate Gate, attempt Attempt, result outcome) {
	note := ports.UpgradeAttempt{CountsAsTry: result.countsAsTry() && !gate.DryRun}
	if result == keptCurrent && !gate.DryRun {
		note.RejectedSourceKey = attempt.SourceKey
	}
	tries, err := r.store.NoteAttempt(ctx, trackId, note)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.upgrade_attempt_unrecorded",
			"track_id", trackId.String(), "error", redact.LogError(err))
		return
	}
	if note.CountsAsTry && tries >= r.cfg.MaxTries {
		r.giveUp(ctx, trackId, gate.Current, tries)
	}
}

func (r *Runner) giveUp(ctx context.Context, trackId shared.TrackId, current grade.Grade, tries int) {
	reason := fmt.Sprintf("no source graded above %s after %d tries", current, tries)
	if err := r.store.GiveUp(ctx, trackId, grade.PipelineGeneration, reason); err != nil {
		slog.ErrorContext(ctx, "acquisition.upgrade_give_up_unrecorded",
			"track_id", trackId.String(), "error", redact.LogError(err))
		return
	}
	slog.InfoContext(ctx, "acquisition.upgrade_gave_up",
		"track_id", trackId.String(), "grade", string(current), "tries", tries, "reason", reason)
}

func (r *Runner) isSettled(track *ports.TrackSnapshot, current grade.Grade) bool {
	switch {
	case current.IsBestAvailable(), current == grade.AUser:
		return true
	case current == grade.A:
		return !r.isUnchecked(track)
	}
	return false
}

func isPlayableFile(track *ports.TrackSnapshot) bool {
	return track.Status == ports.TrackReady && track.AudioRef != nil
}
