package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/music"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	MaxDownloadAttempts = 8
	previewSeconds      = 130

	DefaultCandidateTimeout = 3 * time.Minute
)

type candidateFetcher interface {
	Fetch(ctx context.Context, candidate domain.AudioCandidate, outDir string) (domain.FetchedAudio, error)
	PreviewFetcherFor(candidate domain.AudioCandidate) (ports.PreviewFetcher, bool)
}

type DownloadStep struct {
	fetcher         candidateFetcher
	workspace       ports.ScratchFolder
	prober          ports.AudioProber
	identifier      ports.AudioIdentifier
	limiter         *ConcurrencyLimit
	recordingLookup ports.RecordingLookup
	factsCache      *recordingFactsCache
	now             clock.Func
	skips           ports.VerifySkipRecorder
	width           int
	floor           float64
	cuts            Cuts

	candidateTimeout time.Duration
}

const ProdVerifyWidth = 2

func NewDownloadStep(fetcher candidateFetcher, workspace ports.ScratchFolder, identifier ports.AudioIdentifier, lookup ports.RecordingLookup, prober ports.AudioProber, skips ports.VerifySkipRecorder, limit *ConcurrencyLimit, now clock.Func, opts ...func(*DownloadStep)) (*DownloadStep, error) {
	const owner = "NewDownloadStep"
	if err := errors.Join(
		missingPort(owner, "fetcher", fetcher),
		missingPort(owner, "identifier", identifier),
		missingPort(owner, "recording lookup", lookup),
		missingPort(owner, "prober", prober),
		missingPort(owner, "skip recorder", skips),
		missingPort(owner, "download limit", limit),
		missingPort(owner, "clock", now),
		requireWorkspace(owner, workspace),
	); err != nil {
		return nil, err
	}
	return buildDownloadStep(fetcher, workspace, identifier, lookup, prober, skips, limit, now, opts...), nil
}

func buildDownloadStep(fetcher candidateFetcher, workspace ports.ScratchFolder, identifier ports.AudioIdentifier, lookup ports.RecordingLookup, prober ports.AudioProber, skips ports.VerifySkipRecorder, limit *ConcurrencyLimit, now clock.Func, opts ...func(*DownloadStep)) *DownloadStep {
	s := &DownloadStep{fetcher: fetcher, workspace: workspace, identifier: identifier, recordingLookup: lookup, now: now, factsCache: newRecordingFactsCache(now), limiter: limit, prober: prober, skips: skips, width: ProdVerifyWidth, candidateTimeout: DefaultCandidateTimeout, floor: defaultConfidenceFloor, cuts: DefaultCuts()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *DownloadStep) recordSkip(gate string) {
	s.skips.RecordVerifySkip(gate)
}

func WithJudgeCuts(cuts Cuts) func(*DownloadStep) {
	return func(s *DownloadStep) { s.cuts = cuts }
}

func WithVerifyWidth(n int) func(*DownloadStep) {
	return func(s *DownloadStep) { s.width = max(n, 1) }
}

func WithCandidateTimeout(d time.Duration) func(*DownloadStep) {
	return func(s *DownloadStep) { s.candidateTimeout = d }
}

func WithConfidenceFloor(floor float64) func(*DownloadStep) {
	return func(s *DownloadStep) { s.floor = floor }
}

func (s *DownloadStep) Name() StepName { return StepNameDownload }

func (s *DownloadStep) Execute(ctx context.Context, ac *AcquisitionContext, _ AfterSelect) (afterDownload, error) {
	return s.executeWindowed(ctx, ac)
}

func (s *DownloadStep) settle(ctx context.Context, ac *AcquisitionContext, holds *holdBook, failures *downloadFailures) (afterDownload, error) {
	if muxed, ok := holds.takeMuxed(); ok {
		return muxed.keep(ctx, ac), nil
	}
	held, ok := holds.take()
	if !ok {
		return afterDownload{}, failures.noneAccepted(ctx)
	}
	if held.confidence < s.floor {
		removeScratch(ctx, holds.ws, held.tmpDir)
		return afterDownload{}, fmt.Errorf("best candidate confidence %.2f is below the floor %.2f: %w",
			held.confidence, s.floor, ErrNoConfidentMatch)
	}
	held.status = attemptAccepted
	downloaded := held.keep(ctx, ac)
	ac.BestEffort = true
	return downloaded, nil
}

type attemptStatus int

const (
	attemptRejected attemptStatus = iota
	attemptHeld
	attemptAccepted
)

type attempt struct {
	candidate    domain.AudioCandidate
	filePath     string
	tmpDir       string
	verified     verificationResult
	rejection    *downloadRejection
	status       attemptStatus
	otherVersion bool
	evidence     domain.AcquisitionEvidence
	confidence   float64
	format       domain.SourceFormat
}

func (a attempt) muxed() bool { return a.format.ID == domain.MuxedFallbackFormatID }

type heldAttempt struct {
	attempt attempt
	held    bool
}

func holding(a attempt) heldAttempt { return heldAttempt{attempt: a, held: true} }

type holdBook struct {
	ws         ports.ScratchFolder
	best       heldAttempt
	muxed      heldAttempt
	retryGiven bool
}

func (h *holdBook) muxedRetrySpent() bool {
	if !h.muxed.held {
		return false
	}
	spent := h.retryGiven
	h.retryGiven = true
	return spent
}

func (h *holdBook) takeMuxed() (attempt, bool) {
	muxed := h.muxed
	h.muxed = heldAttempt{}
	return muxed.attempt, muxed.held
}

func (h *holdBook) resolveAccepted(ctx context.Context, ac *AcquisitionContext, result attempt) (afterDownload, bool) {
	switch {
	case !result.muxed() && !h.muxed.held:
		return result.keep(ctx, ac), true
	case !result.muxed():
		return h.chooseAgainstMuxed(ctx, ac, result), true
	case !h.muxed.held:
		h.muxed = holding(result)
	default:
		removeScratch(ctx, h.ws, result.tmpDir)
	}
	return afterDownload{}, false
}

func (h *holdBook) chooseAgainstMuxed(ctx context.Context, ac *AcquisitionContext, audioOnly attempt) afterDownload {
	muxed, _ := h.takeMuxed()
	winner, loser := muxed, audioOnly
	if !gradeOf(ctx, ac, muxed).Beats(gradeOf(ctx, ac, audioOnly)) {
		winner, loser = audioOnly, muxed
	}
	removeScratch(ctx, h.ws, loser.tmpDir)
	return winner.keep(ctx, ac)
}

func gradeOf(ctx context.Context, ac *AcquisitionContext, a attempt) domain.Grade {
	scratch := ac.Snapshot()
	a.applyTo(ctx, &scratch)
	return scratch.CollectEvidence().Grade
}

func (h *holdBook) offer(ctx context.Context, candidate attempt) {
	if candidate.status != attemptHeld {
		return
	}
	if h.best.held && !candidate.beats(h.best.attempt) {
		removeScratch(ctx, h.ws, candidate.tmpDir)
		return
	}
	h.discard(ctx)
	h.best = holding(candidate)
}

func (h *holdBook) take() (attempt, bool) {
	best := h.best
	h.best = heldAttempt{}
	return best.attempt, best.held
}

func (h *holdBook) discard(ctx context.Context) {
	if h.muxed.held {
		removeScratch(ctx, h.ws, h.muxed.attempt.tmpDir)
		h.muxed = heldAttempt{}
	}
	if h.best.held {
		removeScratch(ctx, h.ws, h.best.attempt.tmpDir)
		h.best = heldAttempt{}
	}
}

func (a attempt) beats(other attempt) bool {
	if a.otherVersion != other.otherVersion {
		return !a.otherVersion
	}
	return a.confidence > other.confidence
}

type downloadFailures struct {
	searchUnavailable error
	noted             []error
	deadSources       map[domain.SourceName]bool
}

func newDownloadFailures(searchUnavailable error) downloadFailures {
	return downloadFailures{searchUnavailable: searchUnavailable, deadSources: map[domain.SourceName]bool{}}
}

func (f *downloadFailures) unavailable() error {
	for i := len(f.noted) - 1; i >= 0; i-- {
		if domain.IsSourceUnavailable(f.noted[i]) {
			return f.noted[i]
		}
	}
	return f.searchUnavailable
}

func (f *downloadFailures) noteAttempt(result attempt) {
	err := result.err()
	f.note(err)
	if !domain.IsSourceUnavailable(err) || result.candidate.Source == "" {
		return
	}
	f.deadSources[result.candidate.Source] = true
}

func (f *downloadFailures) sourceDead(candidate domain.AudioCandidate) bool {
	return f.deadSources[candidate.Source]
}

func recordSourceDead(ac *AcquisitionContext, candidate domain.AudioCandidate) {
	ac.recordRejection(candidate.URL, candidate.Title, candidate.Source, RejectNotAttempted,
		"source reported itself unavailable earlier in this job")
}

func (f *downloadFailures) note(err error) {
	if err == nil {
		return
	}
	f.noted = append(f.noted, err)
}

func (f *downloadFailures) noneAccepted(ctx context.Context) error {
	if len(f.noted) == 0 && f.unavailable() == nil {
		return fmt.Errorf("no candidate produced acceptable audio: %w", ErrNoConfidentMatch)
	}
	return f.result(ctx)
}

func (f *downloadFailures) result(ctx context.Context) error {
	unavailable := f.unavailable()
	if len(f.noted) == 0 && unavailable != nil {
		return withCancellation(ctx, fmt.Errorf("no candidate produced acceptable audio: %w", unavailable))
	}
	if len(f.noted) == 0 {
		return fmt.Errorf("no candidate produced acceptable audio")
	}
	last := f.noted[len(f.noted)-1]
	if unavailable != nil && !domain.IsSourceUnavailable(last) {
		last = fmt.Errorf("%w (last failure: %w)", unavailable, last)
	}
	return withCancellation(ctx, fmt.Errorf("no candidate produced acceptable audio: %w", last))
}

func recordNotAttempted(ac *AcquisitionContext, untried []domain.AudioCandidate) {
	for _, c := range untried {
		ac.recordRejection(c.URL, c.Title, c.Source, RejectNotAttempted,
			fmt.Sprintf("skipped after %d download attempts", MaxDownloadAttempts))
	}
}

func recordImplausibleDuration(ctx context.Context, ac *AcquisitionContext, candidate domain.AudioCandidate) {
	ac.recordUnmeasuredRejection(candidate.URL, candidate.Title, candidate.Source, RejectDuration,
		fmt.Sprintf("search duration %.0fs vs expected %.0fs", candidate.Duration, ac.ExpectedLength()))
	slog.InfoContext(ctx, "acquisition.candidate_skipped_duration",
		logging.KeyTrackID, ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"search_duration", candidate.Duration,
		"expected_duration", ac.ExpectedLength(),
	)
}

func (a attempt) err() error { return a.rejection.failure() }

func (a attempt) applyTo(ctx context.Context, ac *AcquisitionContext) {
	a.rejection.record(ac, a.candidate)
	if a.status != attemptAccepted {
		return
	}
	sel := a.candidate
	ac.Selected = &sel
	ac.TempPath = a.filePath
	ac.SourceFormat = a.format
	ac.TempDir = a.tmpDir
	ac.DurationVerified = a.verified.duration
	ac.IdentityVerified = a.verified.identity
	ac.ProbedDuration = a.verified.probed
	ac.Verdict = a.verified.verdict
	ac.adopt(ctx, a.evidence, a.confidence)
}

func (a attempt) keep(ctx context.Context, ac *AcquisitionContext) afterDownload {
	a.applyTo(ctx, ac)
	return afterDownload{Kept: a.candidate}
}

func (s *DownloadStep) runAttempt(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate domain.AudioCandidate,
	tmpDir string,
) (result attempt) {
	defer func() {
		if result.status == attemptRejected {
			removeScratch(ctx, s.workspace, tmpDir)
		}
	}()
	result = s.attemptAudio(ctx, ac, candidate, tmpDir)
	result.candidate = candidate
	result.tmpDir = tmpDir
	return result
}

func (s *DownloadStep) attemptAudio(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate domain.AudioCandidate,
	tmpDir string,
) attempt {
	prior, rejection := s.previewIdentification(ctx, ac, candidate)
	if rejection != nil {
		return attempt{rejection: rejection}
	}

	fetched, rejection := s.fetchFull(ctx, ac, candidate, tmpDir)
	if rejection != nil {
		return attempt{rejection: rejection}
	}

	verified, rejection := s.verify(ctx, ac, candidate, fetched.Path, prior)
	if rejection != nil {
		return attempt{filePath: fetched.Path, verified: verified, rejection: rejection}
	}
	result := judgeAttempt(ac, candidate, fetched.Path, verified)
	result.format = fetched.Format
	return result
}

const (
	RejectOtherVersionLengthUnknown = "other version, length unknown"
	RejectOtherVersionOffLengthFmt  = "other version %.0fs off the expected length"
)

func otherVersionLengthReason(evidence domain.AcquisitionEvidence) string {
	if evidence.DurationDeltaSeconds == nil {
		return RejectOtherVersionLengthUnknown
	}
	return fmt.Sprintf(RejectOtherVersionOffLengthFmt, *evidence.DurationDeltaSeconds)
}

func judgeAttempt(ac *AcquisitionContext, candidate domain.AudioCandidate, filePath string, verified verificationResult) attempt {
	evidence := ac.buildEvidence(candidate, verified)
	result := attempt{
		filePath:     filePath,
		verified:     verified,
		evidence:     evidence,
		confidence:   ScoreConfidence(evidence, ac.ExpectedLength()),
		otherVersion: len(evidence.Qualifiers) > 0,
	}
	if result.otherVersion && !SameRecording(ac.ExpectedLength(), verified.probed.Seconds()) {
		result.rejection = &downloadRejection{stage: RejectDuration, reason: otherVersionLengthReason(evidence)}
		return result
	}
	result.status = attemptAccepted
	if result.otherVersion || verified.verdict.Kind == VerdictUnknown {
		result.status = attemptHeld
	}
	return result
}

func (s *DownloadStep) fetchFull(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate domain.AudioCandidate,
	tmpDir string,
) (domain.FetchedAudio, *downloadRejection) {
	_, queued := startStage(ctx, ac, s.now, "download.queue")
	if err := s.limiter.Acquire(ctx); err != nil {
		queued(err)
		return domain.FetchedAudio{}, &downloadRejection{stage: RejectDownload, reason: "download failed", err: err}
	}
	defer s.limiter.Release()
	queued(nil)

	fetchCtx, cancel := context.WithTimeout(ctx, s.candidateTimeout)
	defer cancel()
	fetchCtx, fetchDone := startStage(fetchCtx, ac, s.now, "download.full")
	fetched, err := s.fetcher.Fetch(fetchCtx, candidate, tmpDir)
	fetchDone(err)
	if err != nil {
		err = candidateTimedOut(ctx, fetchCtx, candidate, err)
		slog.WarnContext(ctx, "acquisition.candidate_download_failed",
			logging.KeyTrackID, ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			logging.KeyError, redact.LogError(err))
		return domain.FetchedAudio{}, &downloadRejection{stage: RejectDownload, reason: "download failed", err: err}
	}
	if fetched.Path == "" {
		slog.WarnContext(ctx, "acquisition.candidate_download_no_file",
			logging.KeyTrackID, ac.Track.ID, "url", candidate.URL, "source", candidate.Source)
		return domain.FetchedAudio{}, &downloadRejection{stage: RejectDownload, reason: "download failed", err: errFetchedNoFile}
	}
	return fetched, nil
}

var errFetchedNoFile = errors.New("fetcher returned no file")

func candidateTimedOut(jobCtx, fetchCtx context.Context, candidate domain.AudioCandidate, err error) error {
	if !domain.RunTimedOut(jobCtx, fetchCtx) {
		return err
	}
	return &domain.SourceUnavailableError{
		Source: candidate.Source,
		Err:    fmt.Errorf("candidate timed out: %s", redact.LogError(err)),
	}
}

type verificationResult struct {
	duration bool
	identity bool
	probed   music.TrackLength
	verdict  AudioVerdict
}

type downloadRejection struct {
	stage  RejectionStage
	reason string
	err    error
}

func (r *downloadRejection) failure() error {
	if r == nil {
		return nil
	}
	return r.err
}

func (r *downloadRejection) record(ac *AcquisitionContext, candidate domain.AudioCandidate) {
	if r == nil {
		return
	}
	ac.recordRejection(candidate.URL, candidate.Title, candidate.Source, r.stage, r.reason)
}

func (s *DownloadStep) verify(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate domain.AudioCandidate,
	filePath string,
	prior *identification,
) (verificationResult, *downloadRejection) {
	var result verificationResult

	duration, probed, rejection := s.verifyDuration(ctx, ac, candidate, filePath)
	if rejection != nil {
		return result, rejection
	}
	result.duration = duration
	result.probed = probed

	if rejection := s.verifyDecodable(ctx, ac, candidate, filePath); rejection != nil {
		return result, rejection
	}

	if prior != nil {
		result.verdict = prior.verdict
		result.identity = prior.identity
		return result, nil
	}
	if rejection := s.identify(ctx, ac, candidate, filePath, &result); rejection != nil {
		return result, rejection
	}
	if result.verdict.Judged.Kind == "" {
		result.verdict.Judged = s.judgeUnfingerprinted(ac, result.probed.Seconds(), sourceOf(ac, candidate))
	}

	return result, nil
}

func (s *DownloadStep) verifyDuration(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate domain.AudioCandidate,
	filePath string,
) (bool, music.TrackLength, *downloadRejection) {
	judge, known := ac.lengths().FileJudge(candidate)
	if !known {
		s.recordSkip(domain.SkipNoDuration)
		return false, music.TrackLength{}, nil
	}
	return s.probeDuration(ctx, ac, candidate, filePath, judge)
}

func (s *DownloadStep) probeDuration(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate domain.AudioCandidate,
	filePath string,
	judge FileJudge,
) (bool, music.TrackLength, *downloadRejection) {
	actual, err := s.prober.ProbeDuration(ctx, filePath)
	probed, lengthErr := music.TrackLengthOf(actual)
	if err == nil {
		err = lengthErr
	}
	switch {
	case err != nil && (ctx.Err() != nil || errors.Is(err, domain.ErrNoAudioProber)):
		return false, music.TrackLength{}, nil
	case err != nil:
		slog.WarnContext(ctx, "acquisition.probe_failed_accepting",
			logging.KeyTrackID, ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"probed_seconds", actual, logging.KeyError, redact.LogError(err))
		s.recordSkip(domain.SkipProbeFailed)
		return false, music.TrackLength{}, nil
	}
	if acceptable, held := judge.Judge(actual); !acceptable {
		return false, music.TrackLength{}, durationRejection(ctx, ac, candidate, actual, held)
	}
	return true, probed, nil
}

func durationRejection(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate domain.AudioCandidate,
	actual float64,
	held float64,
) *downloadRejection {
	slog.InfoContext(ctx, "acquisition.candidate_rejected_duration",
		logging.KeyTrackID, ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"actual_duration", actual,
		"expected_duration", held,
		"authoritative", ac.lengths().Resolved() > 0,
	)
	return &downloadRejection{
		stage:  RejectDuration,
		reason: fmt.Sprintf("duration %.0fs vs expected %.0fs", actual, held),
		err: fmt.Errorf("candidate %q duration %.0fs != expected %.0fs",
			candidate.URL, actual, held),
	}
}

func (s *DownloadStep) verifyDecodable(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate domain.AudioCandidate,
	filePath string,
) *downloadRejection {
	err := s.prober.ValidateDecodable(ctx, filePath)
	if err == nil {
		return nil
	}
	return decodeRejection(ctx, ac, candidate, err)
}

func decodeRejection(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate domain.AudioCandidate,
	err error,
) *downloadRejection {
	if ctx.Err() != nil {
		return &downloadRejection{
			stage:  RejectDownload,
			reason: "decode cancelled",
			err:    withCancellation(ctx, fmt.Errorf("candidate %q decode cancelled: %w", candidate.URL, err)),
		}
	}
	slog.WarnContext(ctx, "acquisition.candidate_rejected_undecodable",
		logging.KeyTrackID, ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		logging.KeyError, redact.LogError(err))
	return &downloadRejection{
		stage:  RejectUndecodable,
		reason: "audio failed to decode",
		err:    fmt.Errorf("candidate %q undecodable: %w", candidate.URL, err),
	}
}

func (s *DownloadStep) Rollback(ctx context.Context, ac *AcquisitionContext) error {
	switch {
	case ac.TempDir != "":
		removeScratch(ctx, s.workspace, ac.TempDir)
	case ac.TempPath != "":
		removeScratch(ctx, s.workspace, ac.TempPath)
	}
	return nil
}
