package upgrade

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/redact"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Finding struct {
	Verdict     string
	Fingerprint grade.Fingerprint
	Basis       string
	Score       float64

	BitErrorRate float64
}

func (f Finding) Answered() bool { return f.Fingerprint != grade.Silent }

type InPlaceChecker interface {
	CheckInPlace(ctx context.Context, track *ports.TrackSnapshot) (Finding, error)
}

type inPlaceEvidence struct {
	Grade             grade.Grade      `json:"grade"`
	SourceKind        grade.SourceKind `json:"source_kind"`
	InPlaceGeneration int              `json:"in_place_generation"`
}

func inPlaceEvidenceOf(raw json.RawMessage) inPlaceEvidence {
	var stored inPlaceEvidence
	if err := json.Unmarshal(raw, &stored); err != nil || !stored.Grade.IsKnown() {
		stored.Grade = grade.Ungraded
	}
	return stored
}

type pacer struct {
	mu    sync.Mutex
	next  time.Time
	every time.Duration
}

func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	turn := time.Now()
	if p.next.After(turn) {
		turn = p.next
	}
	p.next = turn.Add(p.every)
	p.mu.Unlock()
	timer := time.NewTimer(time.Until(turn))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for an in-place check slot: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

type inPlaceCheck struct {
	checker  InPlaceChecker
	recorder ports.InPlaceStore
	pacer    *pacer
}

func (r *Runner) WithInPlaceCheck(checker InPlaceChecker, recorder ports.InPlaceStore, perHour int) *Runner {
	every := time.Hour / time.Duration(max(perHour, 1))
	r.inPlace = &inPlaceCheck{checker: checker, recorder: recorder, pacer: &pacer{every: every}}
	return r
}

func isUserUpload(track *ports.TrackSnapshot) bool {
	return track.AudioSourceURL == nil || *track.AudioSourceURL == ""
}

func (r *Runner) isUnchecked(track *ports.TrackSnapshot) bool {
	return r.inPlace != nil && inPlaceEvidenceOf(track.Evidence).InPlaceGeneration < grade.PipelineGeneration
}

type inPlaceOutcome struct {
	grade   grade.Grade
	settled bool
}

func (r *Runner) checkInPlace(ctx context.Context, track *ports.TrackSnapshot, stored inPlaceEvidence) (inPlaceOutcome, error) {
	if err := r.inPlace.pacer.wait(ctx); err != nil {
		return inPlaceOutcome{}, err
	}
	finding, err := r.inPlace.checker.CheckInPlace(ctx, track)
	if err != nil {
		return r.failedCheck(ctx, track, stored, err)
	}
	return r.settleFinding(ctx, track, stored, finding)
}

const maxInPlaceFailures = 3

func (r *Runner) failedCheck(ctx context.Context, track *ports.TrackSnapshot, stored inPlaceEvidence, checkErr error) (inPlaceOutcome, error) {
	if r.cfg.DryRun {
		r.noteUnchecked(ctx, track.ID)
		return inPlaceOutcome{}, fmt.Errorf("check track %s in place: %w", track.ID, checkErr)
	}
	failures, err := r.inPlace.recorder.NoteInPlaceFailure(ctx, track.ID)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.upgrade_in_place_failure_unrecorded",
			"track_id", track.ID.String(), "error", redact.LogError(err))
	}
	if err == nil && failures >= maxInPlaceFailures {
		slog.WarnContext(ctx, "acquisition.upgrade_in_place_given_up",
			"track_id", track.ID.String(), "failures", failures, "error", redact.LogError(checkErr))
		return r.settleFinding(ctx, track, stored, Finding{})
	}
	return inPlaceOutcome{}, fmt.Errorf("check track %s in place: %w", track.ID, checkErr)
}

func (r *Runner) settleFinding(ctx context.Context, track *ports.TrackSnapshot, stored inPlaceEvidence, finding Finding) (inPlaceOutcome, error) {
	record := verdictOf(track, stored, finding)
	slog.InfoContext(ctx, "acquisition.upgrade_in_place",
		"track_id", track.ID.String(), "from", string(stored.Grade), "to", record.Grade,
		"verdict", finding.Verdict, "basis", finding.Basis,
		"user_upload", isUserUpload(track), "user_upload_mismatch", record.UserUploadMismatch,
		"dry_run", r.cfg.DryRun)
	if r.cfg.DryRun {
		r.noteUnchecked(ctx, track.ID)
	} else if err := r.recordInPlace(ctx, track, record); err != nil {
		return inPlaceOutcome{}, err
	}
	return inPlaceOutcome{grade: gradeAfter(stored.Grade, record), settled: isUserUpload(track) || grade.Grade(record.Grade).IsTop()}, nil
}

func gradeAfter(stored grade.Grade, record ports.InPlaceRecord) grade.Grade {
	if record.Grade == "" {
		return stored
	}
	return grade.Grade(record.Grade)
}

func verdictOf(track *ports.TrackSnapshot, stored inPlaceEvidence, finding Finding) ports.InPlaceRecord {
	record := ports.InPlaceRecord{
		AudioRef: *track.AudioRef, Generation: grade.PipelineGeneration,
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
		kind = grade.FanUpload
	}
	record.Grade = string(grade.Of(finding.Fingerprint, shared.ProvenanceBestEffort, kind))
	record.Contradicted = finding.Fingerprint == grade.Contradicts || finding.Fingerprint == grade.Unsupported
	return record
}

func userUploadVerdict(record ports.InPlaceRecord, finding Finding) ports.InPlaceRecord {
	switch finding.Fingerprint {
	case grade.Matches:
		record.Grade = string(grade.AUser)
	case grade.Contradicts, grade.Unsupported:
		record.UserUploadMismatch = true
	}
	return record
}

func (r *Runner) recordInPlace(ctx context.Context, track *ports.TrackSnapshot, record ports.InPlaceRecord) error {
	recorded, err := r.inPlace.recorder.RecordInPlace(ctx, track.ID, record)
	if err != nil {
		return fmt.Errorf("record in-place verdict of track %s: %w", track.ID, err)
	}
	if !recorded {
		slog.InfoContext(ctx, "acquisition.upgrade_in_place_file_changed", "track_id", track.ID.String())
	}
	return nil
}

func (r *Runner) noteUnchecked(ctx context.Context, trackID shared.TrackId) {
	if _, err := r.store.NoteAttempt(ctx, trackID, ports.UpgradeAttempt{}); err != nil {
		slog.ErrorContext(ctx, "acquisition.upgrade_attempt_unrecorded",
			"track_id", trackID.String(), "error", redact.LogError(err))
	}
}
