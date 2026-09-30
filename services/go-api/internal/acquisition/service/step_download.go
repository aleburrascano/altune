package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

const (
	maxDownloadAttempts = ports.EnoughCandidates
	previewSeconds      = 130

	defaultCandidateTimeout = 3 * time.Minute
)

type candidateFetcher interface {
	Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error)
}

type previewSource interface {
	PreviewFetcherFor(candidate ports.AudioCandidate) (ports.PreviewFetcher, bool)
}

type DownloadStep struct {
	fetcher    candidateFetcher
	prober     ports.AudioProber
	identifier ports.AudioIdentifier
	limiter    *DownloadLimiter
	skips      ports.VerifySkipRecorder
	width      int
	floor      float64

	candidateTimeout time.Duration
}

func NewDownloadStep(fetcher candidateFetcher, opts ...func(*DownloadStep)) *DownloadStep {
	s := &DownloadStep{fetcher: fetcher, width: 1, candidateTimeout: defaultCandidateTimeout}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithDownloadProber(p ports.AudioProber) func(*DownloadStep) {
	return func(s *DownloadStep) { s.prober = p }
}

func WithDownloadIdentifier(i ports.AudioIdentifier) func(*DownloadStep) {
	return func(s *DownloadStep) { s.identifier = i }
}

func WithStepDownloadLimiter(l *DownloadLimiter) func(*DownloadStep) {
	return func(s *DownloadStep) { s.limiter = l }
}

func WithStepVerifySkips(r ports.VerifySkipRecorder) func(*DownloadStep) {
	return func(s *DownloadStep) { s.skips = r }
}

func (s *DownloadStep) recordSkip(gate string) {
	if s.skips != nil {
		s.skips.RecordVerifySkip(gate)
	}
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

func (s *DownloadStep) confidenceFloor() float64 {
	if s.floor <= 0 {
		return defaultConfidenceFloor
	}
	return s.floor
}

func (s *DownloadStep) Name() StepName { return stepNameDownload }

func (s *DownloadStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterSelect) (afterDownload, error) {
	if s.width > 1 {
		return s.executeWindowed(ctx, ac)
	}
	failures := downloadFailures{unavailable: ac.SearchUnavailable}
	var holds holdBook
	defer holds.discard()
	attempts := 0

	for i := range ac.Ranked {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return afterDownload{}, fmt.Errorf("download cancelled: %w", ctxErr)
		}
		if attempts >= maxDownloadAttempts {
			recordNotAttempted(ac, ac.Ranked[i:])
			break
		}
		if failures.sourceDead(ac.Ranked[i]) {
			recordSourceDead(ac, ac.Ranked[i])
			continue
		}
		if !ac.candidateDurationPlausible(ac.Ranked[i]) {
			recordImplausibleDuration(ctx, ac, ac.Ranked[i])
			continue
		}

		tmpDir, err := os.MkdirTemp("", tempDirPrefix+"*")
		if err != nil {
			return afterDownload{}, fmt.Errorf("create temp dir: %w", err)
		}

		ac.Attempted = append(ac.Attempted, AttemptedCandidate{URL: ac.Ranked[i].URL, Window: attempts})
		attempts++
		result := s.runAttempt(ctx, ac, ac.Ranked[i], tmpDir)
		result.applyTo(ctx, ac)
		if result.status == attemptAccepted {
			return afterDownload{}, nil
		}
		holds.offer(result)
		failures.noteAttempt(result)
	}

	return afterDownload{}, s.settle(ctx, ac, &holds, &failures)
}

func (s *DownloadStep) settle(ctx context.Context, ac *AcquisitionContext, holds *holdBook, failures *downloadFailures) error {
	held := holds.take()
	if held == nil {
		return failures.noneAccepted(ctx)
	}
	if held.confidence < s.confidenceFloor() {
		_ = os.RemoveAll(held.tmpDir)
		return fmt.Errorf("best candidate confidence %.2f is below the floor %.2f: %w",
			held.confidence, s.confidenceFloor(), ErrNoConfidentMatch)
	}
	held.status = attemptAccepted
	held.applyTo(ctx, ac)
	ac.BestEffort = true
	return nil
}

type holdBook struct {
	best *attempt
}

func (h *holdBook) offer(candidate attempt) {
	if candidate.status != attemptHeld {
		return
	}
	if h.best != nil && !candidate.beats(*h.best) {
		_ = os.RemoveAll(candidate.tmpDir)
		return
	}
	h.discard()
	h.best = &candidate
}

func (h *holdBook) take() *attempt {
	best := h.best
	h.best = nil
	return best
}

func (h *holdBook) discard() {
	if h.best != nil {
		_ = os.RemoveAll(h.best.tmpDir)
		h.best = nil
	}
}

func (a attempt) beats(other attempt) bool {
	if a.fallback != other.fallback {
		return !a.fallback
	}
	return a.confidence > other.confidence
}

type downloadFailures struct {
	last, unavailable error
	deadSources       map[ports.SourceName]bool
}

func (f *downloadFailures) noteAttempt(result attempt) {
	err := result.err()
	f.note(err)
	if !ports.IsSourceUnavailable(err) || result.candidate.Source == "" {
		return
	}
	if f.deadSources == nil {
		f.deadSources = map[ports.SourceName]bool{}
	}
	f.deadSources[result.candidate.Source] = true
}

func (f *downloadFailures) sourceDead(candidate ports.AudioCandidate) bool {
	return f.deadSources[candidate.Source]
}

func recordSourceDead(ac *AcquisitionContext, candidate ports.AudioCandidate) {
	ac.recordRejection(candidate.URL, candidate.Title, candidate.Source, RejectionNotAttempted,
		"source reported itself unavailable earlier in this job")
}

func (f *downloadFailures) note(err error) {
	if err == nil {
		return
	}
	f.last = err
	if ports.IsSourceUnavailable(err) {
		f.unavailable = err
	}
}

func (f *downloadFailures) noneAccepted(ctx context.Context) error {
	if f.last == nil && f.unavailable == nil {
		return fmt.Errorf("no candidate produced acceptable audio: %w", ErrNoConfidentMatch)
	}
	return f.result(ctx)
}

func (f *downloadFailures) result(ctx context.Context) error {
	if f.last == nil && f.unavailable != nil {
		return withCancellation(ctx, fmt.Errorf("no candidate produced acceptable audio: %w", f.unavailable))
	}
	if f.last == nil {
		return fmt.Errorf("no candidate produced acceptable audio")
	}
	last := f.last
	if f.unavailable != nil && !ports.IsSourceUnavailable(last) {
		last = fmt.Errorf("%w (last failure: %w)", f.unavailable, last)
	}
	return withCancellation(ctx, fmt.Errorf("no candidate produced acceptable audio: %w", last))
}

func recordNotAttempted(ac *AcquisitionContext, untried []ports.AudioCandidate) {
	for _, c := range untried {
		ac.recordRejection(c.URL, c.Title, c.Source, RejectionNotAttempted,
			fmt.Sprintf("skipped after %d download attempts", maxDownloadAttempts))
	}
}

func recordImplausibleDuration(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) {
	ac.recordRejection(candidate.URL, candidate.Title, candidate.Source, RejectionDuration,
		fmt.Sprintf("search duration %.0fs vs expected %.0fs", candidate.Duration, ac.Track.Duration))
	slog.InfoContext(ctx, "acquisition.candidate_skipped_duration",
		"track_id", ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"search_duration", candidate.Duration,
		"expected_duration", ac.Track.Duration,
	)
}

type attemptStatus int

const (
	attemptRejected attemptStatus = iota
	attemptHeld
	attemptAccepted
)

type attempt struct {
	candidate  ports.AudioCandidate
	filePath   string
	tmpDir     string
	verified   verificationResult
	rejection  *downloadRejection
	status     attemptStatus
	fallback   bool
	evidence   Evidence
	confidence float64
}

func (a attempt) err() error {
	if a.rejection == nil {
		return nil
	}
	return a.rejection.err
}

func (a attempt) applyTo(ctx context.Context, ac *AcquisitionContext) {
	if a.rejection != nil {
		ac.recordRejection(a.candidate.URL, a.candidate.Title, a.candidate.Source, a.rejection.stage, a.rejection.reason)
	}
	if a.status != attemptAccepted {
		return
	}
	sel := a.candidate
	ac.Selected = &sel
	ac.TempPath = a.filePath
	ac.TempDir = a.tmpDir
	ac.DurationVerified = a.verified.duration
	ac.IdentityVerified = a.verified.identity
	ac.ProbedDuration = a.verified.probed
	ac.Verdict = a.verified.verdict
	ac.adopt(ctx, a.evidence)
}

func (s *DownloadStep) runAttempt(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	tmpDir string,
) (result attempt) {
	defer func() {
		if result.status == attemptRejected {
			_ = os.RemoveAll(tmpDir)
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
	candidate ports.AudioCandidate,
	tmpDir string,
) attempt {
	prior, rejection := s.previewIdentification(ctx, ac, candidate)
	if rejection != nil {
		return attempt{rejection: rejection}
	}

	filePath, rejection := s.fetchFull(ctx, ac, candidate, tmpDir)
	if rejection != nil {
		return attempt{rejection: rejection}
	}

	verified, rejection := s.verify(ctx, ac, candidate, filePath, prior)
	if rejection != nil {
		return attempt{filePath: filePath, verified: verified, rejection: rejection}
	}
	return judgeAttempt(ac, candidate, filePath, verified)
}

func judgeAttempt(ac *AcquisitionContext, candidate ports.AudioCandidate, filePath string, verified verificationResult) attempt {
	evidence := ac.buildEvidence(candidate, verified)
	result := attempt{
		filePath:   filePath,
		verified:   verified,
		evidence:   evidence,
		confidence: ScoreConfidence(evidence, ac.Track.Duration),
		fallback:   len(evidence.Qualifiers) > 0,
	}
	if result.fallback && !fallbackLengthMatches(evidence.DurationDeltaSeconds, ac.Track.Duration) {
		result.rejection = &downloadRejection{
			stage:  RejectionDuration,
			reason: fmt.Sprintf("fallback version %.0fs off the expected length", evidence.DurationDeltaSeconds),
		}
		return result
	}
	result.status = attemptAccepted
	if result.fallback || verified.verdict.Kind == VerdictUnknown {
		result.status = attemptHeld
	}
	return result
}

func (s *DownloadStep) fetchFull(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	tmpDir string,
) (string, *downloadRejection) {
	if err := s.limiter.Acquire(ctx); err != nil {
		return "", &downloadRejection{stage: RejectionDownload, reason: "download failed", err: err}
	}
	defer s.limiter.Release()

	fetchCtx, cancel := context.WithTimeout(ctx, s.candidateTimeout)
	defer cancel()
	filePath, err := s.fetcher.Fetch(fetchCtx, candidate, tmpDir)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.candidate_download_failed",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"error", logSafeError(err))
		return "", &downloadRejection{stage: RejectionDownload, reason: "download failed", err: err}
	}
	return filePath, nil
}

type verificationResult struct {
	duration bool
	identity bool
	probed   float64
	verdict  AudioVerdict
}

type downloadRejection struct {
	stage  RejectionStage
	reason string
	err    error
}

func (s *DownloadStep) verify(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
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

	return result, nil
}

func (s *DownloadStep) verifyDuration(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
) (bool, float64, *downloadRejection) {
	if s.prober == nil {
		return false, 0, nil
	}
	if ac.Track.Duration <= 0 {
		s.recordSkip(ports.SkipNoDuration)
		return false, 0, nil
	}
	return s.probeDuration(ctx, ac, candidate, filePath)
}

func (s *DownloadStep) probeDuration(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
) (bool, float64, *downloadRejection) {
	actual, err := s.prober.ProbeDuration(ctx, filePath)
	switch {
	case err != nil:
		slog.WarnContext(ctx, "acquisition.probe_failed_accepting",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"error", logSafeError(err))
		s.recordSkip(ports.SkipProbeFailed)
		return false, 0, nil
	case !ac.durationAcceptable(actual):
		return false, 0, durationRejection(ctx, ac, candidate, actual)
	default:
		return true, actual, nil
	}
}

func durationRejection(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	actual float64,
) *downloadRejection {
	slog.InfoContext(ctx, "acquisition.candidate_rejected_duration",
		"track_id", ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"actual_duration", actual,
		"expected_duration", ac.Track.Duration,
		"authoritative", ac.Identity.Duration > 0,
	)
	return &downloadRejection{
		stage:  RejectionDuration,
		reason: fmt.Sprintf("duration %.0fs vs expected %.0fs", actual, ac.Track.Duration),
		err: fmt.Errorf("candidate %q duration %.0fs != expected %.0fs",
			candidate.URL, actual, ac.Track.Duration),
	}
}

func (s *DownloadStep) verifyDecodable(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
) *downloadRejection {
	if s.prober == nil {
		return nil
	}
	err := s.prober.ValidateDecodable(ctx, filePath)
	if err == nil {
		return nil
	}
	return decodeRejection(ctx, ac, candidate, err)
}

func decodeRejection(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	err error,
) *downloadRejection {
	if ctx.Err() != nil {
		return &downloadRejection{
			stage:  RejectionDownload,
			reason: "decode cancelled",
			err:    withCancellation(ctx, fmt.Errorf("candidate %q decode cancelled: %w", candidate.URL, err)),
		}
	}
	slog.WarnContext(ctx, "acquisition.candidate_rejected_undecodable",
		"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		"error", logSafeError(err))
	return &downloadRejection{
		stage:  RejectionUndecodable,
		reason: "audio failed to decode",
		err:    fmt.Errorf("candidate %q undecodable: %w", candidate.URL, err),
	}
}

func (s *DownloadStep) Rollback(_ context.Context, ac *AcquisitionContext) error {
	if ac.TempDir != "" {
		_ = os.RemoveAll(ac.TempDir)
	} else if ac.TempPath != "" {
		_ = os.RemoveAll(ac.TempPath)
	}
	return nil
}
