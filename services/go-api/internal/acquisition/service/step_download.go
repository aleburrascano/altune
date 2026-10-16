package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/audiojudge"
	"altune/go-api/internal/shared/redact"
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	maxDownloadAttempts = 8
	previewSeconds      = 130

	DefaultCandidateTimeout = 3 * time.Minute
)

type candidateFetcher interface {
	Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error)
}

type previewSource interface {
	PreviewFetcherFor(candidate ports.AudioCandidate) (ports.PreviewFetcher, bool)
}

type DownloadStep struct {
	fetcher         candidateFetcher
	workspace       ports.TempWorkspace
	prober          ports.AudioProber
	identifier      ports.AudioIdentifier
	limiter         *DownloadLimiter
	recordingLookup ports.RecordingLookup
	factsCache      recordingFactsCache
	skips           ports.VerifySkipRecorder
	width           int
	floor           float64
	cuts            audiojudge.Cuts

	candidateTimeout time.Duration
}

const prodVerifyWidth = 2

func NewDownloadStep(fetcher candidateFetcher, opts ...func(*DownloadStep)) *DownloadStep {
	s := &DownloadStep{fetcher: fetcher, width: prodVerifyWidth, candidateTimeout: DefaultCandidateTimeout, floor: defaultConfidenceFloor, cuts: audiojudge.DefaultCuts()}
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

func WithStepTempWorkspace(w ports.TempWorkspace) func(*DownloadStep) {
	return func(s *DownloadStep) { s.workspace = w }
}

func WithStepVerifySkips(r ports.VerifySkipRecorder) func(*DownloadStep) {
	return func(s *DownloadStep) { s.skips = r }
}

func (s *DownloadStep) recordSkip(gate string) {
	if s.skips != nil {
		s.skips.RecordVerifySkip(gate)
	}
}

func WithJudgeCuts(cuts audiojudge.Cuts) func(*DownloadStep) {
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

func (s *DownloadStep) Name() StepName { return stepNameDownload }

func (s *DownloadStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterSelect) (afterDownload, error) {
	if s.workspace == nil {
		return afterDownload{}, ErrNoTempWorkspace
	}
	if s.width > 1 {
		return s.executeWindowed(ctx, ac)
	}
	failures := downloadFailures{unavailable: ac.SearchUnavailable}
	holds := holdBook{ws: s.workspace}
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

		tmpDir, err := s.workspace.Create("*")
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
	if held.confidence < s.floor {
		_ = holds.ws.Remove(held.tmpDir)
		return fmt.Errorf("best candidate confidence %.2f is below the floor %.2f: %w",
			held.confidence, s.floor, ErrNoConfidentMatch)
	}
	held.status = attemptAccepted
	held.applyTo(ctx, ac)
	ac.BestEffort = true
	return nil
}

type holdBook struct {
	ws   ports.TempWorkspace
	best *attempt
}

func (h *holdBook) offer(candidate attempt) {
	if candidate.status != attemptHeld {
		return
	}
	if h.best != nil && !candidate.beats(*h.best) {
		_ = h.ws.Remove(candidate.tmpDir)
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
		_ = h.ws.Remove(h.best.tmpDir)
		h.best = nil
	}
}

func (a attempt) beats(other attempt) bool {
	if a.otherVersion != other.otherVersion {
		return !a.otherVersion
	}
	return a.confidence > other.confidence
}

type downloadFailures struct {
	last, unavailable error
	deadSources       map[acqdomain.SourceName]bool
}

func (f *downloadFailures) noteAttempt(result attempt) {
	err := result.err()
	f.note(err)
	if !acqdomain.IsSourceUnavailable(err) || result.candidate.Source == "" {
		return
	}
	if f.deadSources == nil {
		f.deadSources = map[acqdomain.SourceName]bool{}
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
	if acqdomain.IsSourceUnavailable(err) {
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
	if f.unavailable != nil && !acqdomain.IsSourceUnavailable(last) {
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
	ac.recordUnmeasuredRejection(candidate.URL, candidate.Title, candidate.Source, RejectionDuration,
		fmt.Sprintf("search duration %.0fs vs expected %.0fs", candidate.Duration, ac.ExpectedLength()))
	slog.InfoContext(ctx, "acquisition.candidate_skipped_duration",
		"track_id", ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"search_duration", candidate.Duration,
		"expected_duration", ac.ExpectedLength(),
	)
}

type attemptStatus int

const (
	attemptRejected attemptStatus = iota
	attemptHeld
	attemptAccepted
)

type attempt struct {
	candidate    ports.AudioCandidate
	filePath     string
	tmpDir       string
	verified     verificationResult
	rejection    *downloadRejection
	status       attemptStatus
	otherVersion bool
	evidence     Evidence
	confidence   float64
	format       ports.SourceFormat
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
	ac.SourceFormat = a.format
	ac.TempDir = a.tmpDir
	ac.DurationVerified = a.verified.duration
	ac.IdentityVerified = a.verified.identity
	ac.ProbedDuration = a.verified.probed
	ac.Verdict = a.verified.verdict
	ac.adopt(ctx, a.evidence, a.confidence)
}

func (s *DownloadStep) runAttempt(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	tmpDir string,
) (result attempt) {
	defer func() {
		if result.status == attemptRejected {
			_ = s.workspace.Remove(tmpDir)
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
	rejectOtherVersionLengthUnknown = "fallback version, length unknown"
	rejectOtherVersionOffLengthFmt  = "fallback version %.0fs off the expected length"
)

func judgeAttempt(ac *AcquisitionContext, candidate ports.AudioCandidate, filePath string, verified verificationResult) attempt {
	evidence := ac.buildEvidence(candidate, verified)
	result := attempt{
		filePath:     filePath,
		verified:     verified,
		evidence:     evidence,
		confidence:   ScoreConfidence(evidence, ac.ExpectedLength()),
		otherVersion: len(evidence.Qualifiers) > 0,
	}
	if result.otherVersion && !otherVersionLengthMatches(evidence.DurationDeltaSeconds, ac.ExpectedLength()) {
		reason := rejectOtherVersionLengthUnknown
		if evidence.DurationDeltaSeconds != nil {
			reason = fmt.Sprintf(rejectOtherVersionOffLengthFmt, *evidence.DurationDeltaSeconds)
		}
		result.rejection = &downloadRejection{stage: RejectionDuration, reason: reason}
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
	candidate ports.AudioCandidate,
	tmpDir string,
) (ports.FetchedAudio, *downloadRejection) {
	queued := time.Now()
	if err := s.limiter.Acquire(ctx); err != nil {
		return ports.FetchedAudio{}, &downloadRejection{stage: RejectionDownload, reason: "download failed", err: err}
	}
	defer s.limiter.Release()
	timeStage(ctx, "download.queue", queued)

	fetchCtx, cancel := context.WithTimeout(ctx, s.candidateTimeout)
	defer cancel()
	fetchStart := time.Now()
	fetched, err := s.fetcher.Fetch(fetchCtx, candidate, tmpDir)
	timeStage(ctx, "download.full", fetchStart)
	if err != nil {
		err = candidateTimedOut(ctx, fetchCtx, candidate, err)
		slog.WarnContext(ctx, "acquisition.candidate_download_failed",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"error", redact.LogError(err))
		return ports.FetchedAudio{}, &downloadRejection{stage: RejectionDownload, reason: "download failed", err: err}
	}
	return fetched, nil
}

func candidateTimedOut(jobCtx, fetchCtx context.Context, candidate ports.AudioCandidate, err error) error {
	if !ports.RunTimedOut(jobCtx, fetchCtx) {
		return err
	}
	return &acqdomain.SourceUnavailableError{
		Source: candidate.Source,
		Err:    fmt.Errorf("candidate timed out: %s", redact.LogError(err)),
	}
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
	if result.verdict.Judged.Kind == "" {
		result.verdict.Judged = s.judgeUnfingerprinted(ac, result.probed, sourceOf(ac, candidate))
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
	if expectedLength(ac, candidate) <= 0 {
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
	case err != nil && ctx.Err() != nil:
		return false, 0, nil
	case err != nil:
		slog.WarnContext(ctx, "acquisition.probe_failed_accepting",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"error", redact.LogError(err))
		s.recordSkip(ports.SkipProbeFailed)
		return false, 0, nil
	case !ac.fileLengthAcceptable(candidate, actual):
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
	held := lengthHeldAgainst(ac, candidate, actual)
	slog.InfoContext(ctx, "acquisition.candidate_rejected_duration",
		"track_id", ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"actual_duration", actual,
		"expected_duration", held,
		"authoritative", ac.resolvedLength() > 0,
	)
	return &downloadRejection{
		stage:  RejectionDuration,
		reason: fmt.Sprintf("duration %.0fs vs expected %.0fs", actual, held),
		err: fmt.Errorf("candidate %q duration %.0fs != expected %.0fs",
			candidate.URL, actual, held),
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
		"error", redact.LogError(err))
	return &downloadRejection{
		stage:  RejectionUndecodable,
		reason: "audio failed to decode",
		err:    fmt.Errorf("candidate %q undecodable: %w", candidate.URL, err),
	}
}

func (s *DownloadStep) Rollback(_ context.Context, ac *AcquisitionContext) error {
	if ac.TempDir != "" {
		_ = s.workspace.Remove(ac.TempDir)
	} else if ac.TempPath != "" {
		_ = s.workspace.Remove(ac.TempPath)
	}
	return nil
}
