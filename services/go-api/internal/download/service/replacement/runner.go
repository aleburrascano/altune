package replacement

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
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
	MaxTries            int
	LowBitrateFloorKbps int
}

type Attempt struct {
	SourceKey string
	Grade     audioquality.Grade
	NoSource  bool
}

type Replacer interface {
	ReplaceGated(ctx context.Context, job ports.AcquisitionJob, track *ports.TrackSnapshot, gate ReplacementGate) (Attempt, error)
}

type Runner struct {
	ledger   ports.LibraryTracks
	replacer Replacer
	store    ports.UpgradeStore
	cfg      Config
	inPlace  InPlace
}

var ErrMissingRunnerPort = errors.New("replacement: a required runner port is missing")

func NewRunner(ledger ports.LibraryTracks, replacer Replacer, store ports.UpgradeStore, cfg Config, inPlace InPlace) (*Runner, error) {
	missing := []struct {
		name string
		gone bool
	}{
		{"ledger", ledger == nil},
		{"replacer", replacer == nil},
		{"store", store == nil},
	}
	for _, port := range missing {
		if port.gone {
			return nil, fmt.Errorf("%w: %s", ErrMissingRunnerPort, port.name)
		}
	}
	return &Runner{ledger: ledger, replacer: replacer, store: store, cfg: cfg, inPlace: inPlace}, nil
}

func MustNewRunner(ledger ports.LibraryTracks, replacer Replacer, store ports.UpgradeStore, cfg Config, inPlace InPlace) *Runner {
	r, err := NewRunner(ledger, replacer, store, cfg, inPlace)
	if err != nil {
		panic(err)
	}
	return r
}

type outcome string

const (
	upgraded       outcome = "upgraded"
	keptCurrent    outcome = "kept_current"
	noBetterSource outcome = "no_better_source"
	failed         outcome = "failed"
)

func outcomeOf(attempt Attempt, err error) outcome {
	switch {
	case err == nil:
		return upgraded
	case errors.Is(err, ErrNotAnUpgrade):
		return keptCurrent
	case attempt.NoSource:
		return noBetterSource
	default:
		return failed
	}
}

func (o outcome) countsAsTry() bool { return o == keptCurrent || o == noBetterSource }

func (r *Runner) ExecuteUpgrade(ctx context.Context, job ports.AcquisitionJob) error {
	track, err := r.ledger.Get(ctx, job.TrackID, job.UserID)
	if errors.Is(err, ports.ErrTrackNotFound) {
		slog.WarnContext(ctx, "acquisition.upgrade_track_not_found", logging.KeyTrackID, job.TrackID.String())
		return nil
	}
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	stored := inPlaceEvidenceOf(track.Evidence)
	current := stored.Grade
	if !track.Streamable || r.isSettled(track, current, stored.format()) {
		slog.InfoContext(ctx, "acquisition.upgrade_skipped", logging.KeyTrackID, job.TrackID.String())
		return nil
	}
	current, settled, err := r.recheckedGrade(ctx, track, stored, current)
	if err != nil || settled {
		return err
	}
	if isUserUpload(track) {
		slog.InfoContext(ctx, "acquisition.upgrade_skipped", logging.KeyTrackID, job.TrackID.String())
		return nil
	}
	gate := ReplacementGate{
		Current:        current,
		CatalogSeconds: secondsOf(track.CatalogDurationSeconds),
		StoredSeconds:  secondsOf(track.StoredDurationSeconds),
	}
	if stored.format().IsLowBitrate(r.cfg.LowBitrateFloorKbps) {
		gate.Stored = stored.format()
		gate.FloorKbps = r.cfg.LowBitrateFloorKbps
	}
	attempt, err := r.replacer.ReplaceGated(ctx, job, track, gate)
	return r.settle(ctx, job.TrackID, gate, attempt, err)
}

func (r *Runner) settle(ctx context.Context, trackId ids.TrackId, gate ReplacementGate, attempt Attempt, runErr error) error {
	result := outcomeOf(attempt, runErr)
	slog.InfoContext(ctx, "acquisition.upgrade_outcome",
		logging.KeyTrackID, trackId.String(),
		"outcome", string(result),
		"from", string(gate.Current),
		"to", string(attempt.Grade))
	if result == upgraded || ctx.Err() != nil {
		return runErr
	}
	settleCtx, cancel := background.Detach(ctx, settleBudget)
	defer cancel()
	r.record(settleCtx, trackId, gate, attempt, result)
	if result == failed {
		return runErr
	}
	return fmt.Errorf("%w: %s", ErrKept, result)
}

func (r *Runner) record(ctx context.Context, trackId ids.TrackId, gate ReplacementGate, attempt Attempt, result outcome) {
	note := ports.UpgradeAttempt{CountsAsTry: result.countsAsTry()}
	if result == keptCurrent {
		note.RejectedSourceKey = attempt.SourceKey
	}
	tries, err := r.store.NoteAttempt(ctx, trackId, note)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.upgrade_attempt_unrecorded",
			logging.KeyTrackID, trackId.String(), logging.KeyError, redact.LogError(err))
		return
	}
	if note.CountsAsTry && tries >= r.cfg.MaxTries {
		r.giveUp(ctx, trackId, gate.Current, tries)
	}
}

func (r *Runner) giveUp(ctx context.Context, trackId ids.TrackId, current audioquality.Grade, tries int) {
	reason := fmt.Sprintf("no source graded above %s after %d tries", current, tries)
	if err := r.store.GiveUp(ctx, trackId, audioquality.PipelineGeneration, reason); err != nil {
		slog.ErrorContext(ctx, "acquisition.upgrade_give_up_unrecorded",
			logging.KeyTrackID, trackId.String(), logging.KeyError, redact.LogError(err))
		return
	}
	slog.InfoContext(ctx, "acquisition.upgrade_gave_up",
		logging.KeyTrackID, trackId.String(), "grade", string(current), "tries", tries, "reason", reason)
}

func (r *Runner) isSettled(track *ports.TrackSnapshot, current audioquality.Grade, stored ports.SourceFormat) bool {
	switch {
	case current.IsBestAvailable(), current == audioquality.AUser:
		return true
	case current == audioquality.A:
		return !r.isUnchecked(track) && !stored.IsLowBitrate(r.cfg.LowBitrateFloorKbps)
	}
	return false
}

func secondsOf(seconds *float64) float64 {
	if seconds == nil {
		return 0
	}
	return *seconds
}
