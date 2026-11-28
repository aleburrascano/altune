package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Finding struct {
	Verdict     string
	Fingerprint domain.FingerprintResult
	Basis       string
	Score       float64
	Lowers      bool

	BitErrorRate float64
}

func (f Finding) Answered() bool { return f.Fingerprint != domain.FingerprintSilent }

type InPlaceProbe interface {
	CheckInPlace(ctx context.Context, track *domain.TrackSnapshot) (Finding, error)
}

func (r *Runner) inPlaceEvidenceOf(raw json.RawMessage) domain.InPlaceEvidence {
	stored, err := r.evidence.DecodeInPlace(raw)
	if err != nil || (!stored.Grade.IsKnown() && !stored.Grade.IsBestAvailable()) {
		stored.Grade = domain.Ungraded
	}
	return stored
}

type pacer struct {
	mu    sync.Mutex
	next  time.Time
	every time.Duration
	now   clock.Func
	sleep clock.Sleeper
}

func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := p.now()
	turn := now
	if p.next.After(turn) {
		turn = p.next
	}
	p.next = turn.Add(p.every)
	p.mu.Unlock()
	if err := p.sleep(ctx, turn.Sub(now)); err != nil {
		return fmt.Errorf("wait for an in-place check slot: %w", err)
	}
	return nil
}

type inPlaceCheck struct {
	checker  InPlaceProbe
	recorder ports.InPlaceStore
	pacer    *pacer
}

func (c *inPlaceCheck) waitForSlot(ctx context.Context) error { return c.pacer.wait(ctx) }

func (c *inPlaceCheck) check(ctx context.Context, track *domain.TrackSnapshot) (Finding, error) {
	r0, err := c.checker.CheckInPlace(ctx, track)
	if err != nil {
		return r0, fmt.Errorf("check in place: %w", err)
	}
	return r0, nil
}

func (c *inPlaceCheck) noteFailure(ctx context.Context, trackID ids.TrackId) (int, error) {
	return c.recorder.NoteInPlaceFailure(ctx, trackID)
}

func (c *inPlaceCheck) record(ctx context.Context, trackID ids.TrackId, record domain.InPlaceRecord) (bool, error) {
	return c.recorder.RecordInPlace(ctx, trackID, record)
}

var ErrMissingInPlacePort = errors.New("replacement: an in-place check needs a checker, a recorder, a clock and a sleeper")

type InPlace struct {
	check   *inPlaceCheck
	enabled bool
}

func NoInPlaceCheck() InPlace { return InPlace{} }

func InPlaceCheckOf(checker InPlaceProbe, recorder ports.InPlaceStore, perHour int, now clock.Func, sleep clock.Sleeper) (InPlace, error) {
	if checker == nil || recorder == nil || now == nil || sleep == nil {
		return InPlace{}, ErrMissingInPlacePort
	}
	every := time.Hour / time.Duration(max(perHour, 1))
	return InPlace{check: &inPlaceCheck{checker: checker, recorder: recorder, pacer: &pacer{every: every, now: now, sleep: sleep}}, enabled: true}, nil
}

func isUserUpload(track *domain.TrackSnapshot) bool {
	return track.AudioSourceURL == nil || *track.AudioSourceURL == ""
}

func (r *Runner) isUnchecked(track *domain.TrackSnapshot) bool {
	return r.inPlace.enabled && r.inPlaceEvidenceOf(track.Evidence).InPlaceGeneration < domain.PipelineGeneration
}

type inPlaceOutcome struct {
	grade   domain.Grade
	settled bool
}

func (r *Runner) recheckedGrade(ctx context.Context, track *domain.TrackSnapshot, stored domain.InPlaceEvidence, current domain.Grade) (domain.Grade, bool, error) {
	if !r.isUnchecked(track) {
		return current, false, nil
	}
	checked, err := r.checkInPlace(ctx, track, stored)
	if err != nil || checked.settled {
		return current, true, err
	}
	return checked.grade, false, nil
}

func (r *Runner) checkInPlace(ctx context.Context, track *domain.TrackSnapshot, stored domain.InPlaceEvidence) (inPlaceOutcome, error) {
	if err := r.inPlace.check.waitForSlot(ctx); err != nil {
		return inPlaceOutcome{}, err
	}
	finding, err := r.inPlace.check.check(ctx, track)
	if err != nil {
		return r.failedCheck(ctx, track, stored, err)
	}
	return r.settleFinding(ctx, track, stored, finding)
}

const maxInPlaceFailures = 3

func (r *Runner) failedCheck(ctx context.Context, track *domain.TrackSnapshot, stored domain.InPlaceEvidence, checkErr error) (inPlaceOutcome, error) {
	failures, err := r.inPlace.check.noteFailure(ctx, track.ID)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.upgrade_in_place_failure_unrecorded",
			logging.KeyTrackID, track.ID.String(), logging.KeyError, redact.LogError(err))
	}
	if err == nil && failures >= maxInPlaceFailures {
		slog.WarnContext(ctx, "acquisition.upgrade_in_place_given_up",
			logging.KeyTrackID, track.ID.String(), "failures", failures, logging.KeyError, redact.LogError(checkErr))
		return r.settleFinding(ctx, track, stored, Finding{})
	}
	return inPlaceOutcome{}, fmt.Errorf("check track %s in place: %w", track.ID, checkErr)
}

func (r *Runner) settleFinding(ctx context.Context, track *domain.TrackSnapshot, stored domain.InPlaceEvidence, finding Finding) (inPlaceOutcome, error) {
	record := inPlaceVerdictOf(track, stored, finding)
	slog.InfoContext(ctx, "acquisition.upgrade_in_place",
		logging.KeyTrackID, track.ID.String(), "from", string(stored.Grade), "to", record.Grade,
		"verdict", finding.Verdict, "basis", finding.Basis,
		"user_upload", isUserUpload(track), "user_upload_mismatch", record.UserUploadMismatch)
	if err := r.recordInPlace(ctx, track, record); err != nil {
		return inPlaceOutcome{}, err
	}
	after := gradeAfter(stored.Grade, record)
	return inPlaceOutcome{grade: after, settled: isUserUpload(track) || after.IsTop()}, nil
}

func gradeAfter(stored domain.Grade, record domain.InPlaceRecord) domain.Grade {
	recorded := domain.Grade(record.Grade)
	if recorded == "" || (!record.MayLower && !recorded.Beats(stored)) {
		return stored
	}
	return recorded
}

func inPlaceVerdictOf(track *domain.TrackSnapshot, stored domain.InPlaceEvidence, finding Finding) domain.InPlaceRecord {
	record := domain.InPlaceRecord{
		AudioRef: *track.AudioRef, Generation: domain.PipelineGeneration,
		Verdict: finding.Verdict, Basis: finding.Basis, Score: finding.Score, BitErrorRate: finding.BitErrorRate,
	}
	switch {
	case !finding.Answered():
		return record
	case isUserUpload(track):
		return userUploadVerdict(record, finding)
	}
	kind := stored.SourceKind
	if kind == "" {
		kind = domain.FanUpload
	}
	record.Grade = string(domain.GradeOf(finding.Fingerprint, downloadprovenance.ProvenanceBestEffort, kind))
	record.MayLower = finding.Lowers || finding.Fingerprint == domain.FingerprintContradicts || finding.Fingerprint == domain.FingerprintUnsupported
	return record
}

func userUploadVerdict(record domain.InPlaceRecord, finding Finding) domain.InPlaceRecord {
	switch finding.Fingerprint {
	case domain.FingerprintMatches:
		record.Grade = string(domain.GradeAUser)
	case domain.FingerprintContradicts, domain.FingerprintUnsupported:
		record.UserUploadMismatch = true
	case domain.FingerprintSilent, domain.FingerprintApproximates:
	}
	return record
}

func (r *Runner) recordInPlace(ctx context.Context, track *domain.TrackSnapshot, record domain.InPlaceRecord) error {
	recorded, err := r.inPlace.check.record(ctx, track.ID, record)
	if err != nil {
		return fmt.Errorf("record in-place verdict of track %s: %w", track.ID, err)
	}
	if !recorded {
		slog.InfoContext(ctx, "acquisition.upgrade_in_place_file_changed", logging.KeyTrackID, track.ID.String())
	}
	return nil
}
