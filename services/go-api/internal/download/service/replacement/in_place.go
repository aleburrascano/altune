package replacement

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/ids"
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
	Fingerprint audioquality.FingerprintResult
	Basis       string
	Score       float64
	Lowers      bool

	BitErrorRate float64
}

func (f Finding) Answered() bool { return f.Fingerprint != audioquality.Silent }

type InPlaceChecker interface {
	CheckInPlace(ctx context.Context, track *ports.TrackSnapshot) (Finding, error)
}

type inPlaceEvidence struct {
	Grade             audioquality.Grade      `json:"grade"`
	SourceKind        audioquality.SourceKind `json:"source_kind"`
	InPlaceGeneration int                     `json:"in_place_generation"`
	SourceFormatID    string                  `json:"source_format_id"`
	SourceKbps        int                     `json:"source_kbps"`
}

func (e inPlaceEvidence) format() ports.SourceFormat {
	return ports.SourceFormat{ID: e.SourceFormatID, Kbps: e.SourceKbps}
}

func inPlaceEvidenceOf(raw json.RawMessage) inPlaceEvidence {
	var stored inPlaceEvidence
	if err := json.Unmarshal(raw, &stored); err != nil || (!stored.Grade.IsKnown() && !stored.Grade.IsBestAvailable()) {
		stored.Grade = audioquality.Ungraded
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

func (c *inPlaceCheck) waitForSlot(ctx context.Context) error { return c.pacer.wait(ctx) }

func (c *inPlaceCheck) check(ctx context.Context, track *ports.TrackSnapshot) (Finding, error) {
	return c.checker.CheckInPlace(ctx, track)
}

func (c *inPlaceCheck) noteFailure(ctx context.Context, trackID ids.TrackId) (int, error) {
	return c.recorder.NoteInPlaceFailure(ctx, trackID)
}

func (c *inPlaceCheck) record(ctx context.Context, trackID ids.TrackId, record ports.InPlaceRecord) (bool, error) {
	return c.recorder.RecordInPlace(ctx, trackID, record)
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
	return r.inPlace != nil && inPlaceEvidenceOf(track.Evidence).InPlaceGeneration < audioquality.PipelineGeneration
}

type inPlaceOutcome struct {
	grade   audioquality.Grade
	settled bool
}

func (r *Runner) checkInPlace(ctx context.Context, track *ports.TrackSnapshot, stored inPlaceEvidence) (inPlaceOutcome, error) {
	if err := r.inPlace.waitForSlot(ctx); err != nil {
		return inPlaceOutcome{}, err
	}
	finding, err := r.inPlace.check(ctx, track)
	if err != nil {
		return r.failedCheck(ctx, track, stored, err)
	}
	return r.settleFinding(ctx, track, stored, finding)
}

const maxInPlaceFailures = 3

func (r *Runner) failedCheck(ctx context.Context, track *ports.TrackSnapshot, stored inPlaceEvidence, checkErr error) (inPlaceOutcome, error) {
	failures, err := r.inPlace.noteFailure(ctx, track.ID)
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
		"user_upload", isUserUpload(track), "user_upload_mismatch", record.UserUploadMismatch)
	if err := r.recordInPlace(ctx, track, record); err != nil {
		return inPlaceOutcome{}, err
	}
	after := gradeAfter(stored.Grade, record)
	return inPlaceOutcome{grade: after, settled: isUserUpload(track) || after.IsTop()}, nil
}

func gradeAfter(stored audioquality.Grade, record ports.InPlaceRecord) audioquality.Grade {
	recorded := audioquality.Grade(record.Grade)
	if recorded == "" || (!record.MayLower && !recorded.Beats(stored)) {
		return stored
	}
	return recorded
}

func verdictOf(track *ports.TrackSnapshot, stored inPlaceEvidence, finding Finding) ports.InPlaceRecord {
	record := ports.InPlaceRecord{
		AudioRef: *track.AudioRef, Generation: audioquality.PipelineGeneration,
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
		kind = audioquality.FanUpload
	}
	record.Grade = string(audioquality.Of(finding.Fingerprint, downloadprovenance.ProvenanceBestEffort, kind))
	record.MayLower = finding.Lowers || finding.Fingerprint == audioquality.Contradicts || finding.Fingerprint == audioquality.Unsupported
	return record
}

func userUploadVerdict(record ports.InPlaceRecord, finding Finding) ports.InPlaceRecord {
	switch finding.Fingerprint {
	case audioquality.Matches:
		record.Grade = string(audioquality.AUser)
	case audioquality.Contradicts, audioquality.Unsupported:
		record.UserUploadMismatch = true
	case audioquality.Silent, audioquality.Approximates:
	}
	return record
}

func (r *Runner) recordInPlace(ctx context.Context, track *ports.TrackSnapshot, record ports.InPlaceRecord) error {
	recorded, err := r.inPlace.record(ctx, track.ID, record)
	if err != nil {
		return fmt.Errorf("record in-place verdict of track %s: %w", track.ID, err)
	}
	if !recorded {
		slog.InfoContext(ctx, "acquisition.upgrade_in_place_file_changed", "track_id", track.ID.String())
	}
	return nil
}
