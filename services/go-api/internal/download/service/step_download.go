package service

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/audioverdict"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/rejectedcandidates"
	"altune/go-api/internal/download/internal/core/tracklength"
	"altune/go-api/internal/shared/redact"
	sharedtracklength "altune/go-api/internal/shared/tracklength"
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
	Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error)
}

type previewSource interface {
	PreviewFetcherFor(candidate ports.AudioCandidate) (ports.PreviewFetcher, bool)
}

type DownloadStep struct {
	fetcher         candidateFetcher
	workspace       ports.ScratchFolder
	prober          ports.AudioProber
	identifier      ports.AudioIdentifier
	limiter         *ConcurrencyLimit
	recordingLookup ports.RecordingLookup
	factsCache      *recordingFactsCache
	skips           ports.VerifySkipRecorder
	width           int
	floor           float64
	cuts            audioverdict.Cuts

	candidateTimeout time.Duration
}

const ProdVerifyWidth = 2

func NewDownloadStep(fetcher candidateFetcher, workspace ports.ScratchFolder, identifier ports.AudioIdentifier, lookup ports.RecordingLookup, prober ports.AudioProber, skips ports.VerifySkipRecorder, opts ...func(*DownloadStep)) *DownloadStep {
	requirePort("NewDownloadStep", "identifier", identifier == nil)
	requirePort("NewDownloadStep", "recording lookup", lookup == nil)
	requirePort("NewDownloadStep", "prober", prober == nil)
	requirePort("NewDownloadStep", "skip recorder", skips == nil)
	s := &DownloadStep{fetcher: fetcher, workspace: requireWorkspace("NewDownloadStep", workspace), identifier: identifier, recordingLookup: lookup, factsCache: newRecordingFactsCache(time.Now), limiter: &ConcurrencyLimit{}, prober: prober, skips: skips, width: ProdVerifyWidth, candidateTimeout: DefaultCandidateTimeout, floor: defaultConfidenceFloor, cuts: audioverdict.DefaultCuts()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithStepDownloadLimiter(l *ConcurrencyLimit) func(*DownloadStep) {
	return func(s *DownloadStep) {
		requirePort("WithStepDownloadLimiter", "limiter", l == nil)
		s.limiter = l
	}
}

func (s *DownloadStep) recordSkip(gate string) {
	s.skips.RecordVerifySkip(gate)
}

func WithJudgeCuts(cuts audioverdict.Cuts) func(*DownloadStep) {
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
	if muxed := holds.takeMuxed(); muxed != nil {
		return muxed.keep(ctx, ac), nil
	}
	held := holds.take()
	if held == nil {
		return afterDownload{}, failures.noneAccepted(ctx)
	}
	if held.confidence < s.floor {
		_ = holds.ws.Remove(held.tmpDir)
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

func (a attempt) muxed() bool { return a.format.ID == ports.MuxedFallbackFormatID }

type holdBook struct {
	ws         ports.ScratchFolder
	best       *attempt
	muxed      *attempt
	retryGiven bool
}

func (h *holdBook) muxedRetrySpent() bool {
	if h.muxed == nil {
		return false
	}
	spent := h.retryGiven
	h.retryGiven = true
	return spent
}

func (h *holdBook) takeMuxed() *attempt {
	muxed := h.muxed
	h.muxed = nil
	return muxed
}

func (h *holdBook) resolveAccepted(ctx context.Context, ac *AcquisitionContext, result attempt) (afterDownload, bool) {
	switch {
	case !result.muxed() && h.muxed == nil:
		return result.keep(ctx, ac), true
	case !result.muxed():
		return h.chooseAgainstMuxed(ctx, ac, result), true
	case h.muxed == nil:
		h.muxed = &result
	default:
		_ = h.ws.Remove(result.tmpDir)
	}
	return afterDownload{}, false
}

func (h *holdBook) chooseAgainstMuxed(ctx context.Context, ac *AcquisitionContext, audioOnly attempt) afterDownload {
	muxed := h.takeMuxed()
	winner, loser := *muxed, audioOnly
	if !gradeOf(ctx, ac, *muxed).Beats(gradeOf(ctx, ac, audioOnly)) {
		winner, loser = audioOnly, *muxed
	}
	_ = h.ws.Remove(loser.tmpDir)
	return winner.keep(ctx, ac)
}

func gradeOf(ctx context.Context, ac *AcquisitionContext, a attempt) audioquality.Grade {
	scratch := ac.snapshot()
	a.applyTo(ctx, &scratch)
	return scratch.CollectEvidence().Grade
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
	if h.muxed != nil {
		_ = h.ws.Remove(h.muxed.tmpDir)
		h.muxed = nil
	}
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
	ac.recordRejection(candidate.URL, candidate.Title, candidate.Source, rejectedcandidates.NotAttempted,
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
		ac.recordRejection(c.URL, c.Title, c.Source, rejectedcandidates.NotAttempted,
			fmt.Sprintf("skipped after %d download attempts", MaxDownloadAttempts))
	}
}

func recordImplausibleDuration(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) {
	ac.recordUnmeasuredRejection(candidate.URL, candidate.Title, candidate.Source, rejectedcandidates.Duration,
		fmt.Sprintf("search duration %.0fs vs expected %.0fs", candidate.Duration, ac.ExpectedLength()))
	slog.InfoContext(ctx, "acquisition.candidate_skipped_duration",
		"track_id", ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"search_duration", candidate.Duration,
		"expected_duration", ac.ExpectedLength(),
	)
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

func (a attempt) keep(ctx context.Context, ac *AcquisitionContext) afterDownload {
	a.applyTo(ctx, ac)
	return afterDownload{Kept: a.candidate}
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
	RejectOtherVersionLengthUnknown = "other version, length unknown"
	RejectOtherVersionOffLengthFmt  = "other version %.0fs off the expected length"
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
	if result.otherVersion && !tracklength.SameRecording(ac.ExpectedLength(), verified.probed.Seconds()) {
		reason := RejectOtherVersionLengthUnknown
		if evidence.DurationDeltaSeconds != nil {
			reason = fmt.Sprintf(RejectOtherVersionOffLengthFmt, *evidence.DurationDeltaSeconds)
		}
		result.rejection = &downloadRejection{stage: rejectedcandidates.Duration, reason: reason}
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
		return ports.FetchedAudio{}, &downloadRejection{stage: rejectedcandidates.Download, reason: "download failed", err: err}
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
		return ports.FetchedAudio{}, &downloadRejection{stage: rejectedcandidates.Download, reason: "download failed", err: err}
	}
	if fetched.Path == "" {
		slog.WarnContext(ctx, "acquisition.candidate_download_no_file",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source)
		return ports.FetchedAudio{}, &downloadRejection{stage: rejectedcandidates.Download, reason: "download failed", err: errFetchedNoFile}
	}
	return fetched, nil
}

var errFetchedNoFile = errors.New("fetcher returned no file")

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
	probed   sharedtracklength.TrackLength
	verdict  AudioVerdict
}

type downloadRejection struct {
	stage  rejectedcandidates.Stage
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
		result.verdict.Judged = s.judgeUnfingerprinted(ac, result.probed.Seconds(), sourceOf(ac, candidate))
	}

	return result, nil
}

func (s *DownloadStep) verifyDuration(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
) (bool, sharedtracklength.TrackLength, *downloadRejection) {
	judge, known := ac.lengths().FileJudge(candidate)
	if !known {
		s.recordSkip(ports.SkipNoDuration)
		return false, sharedtracklength.TrackLength{}, nil
	}
	return s.probeDuration(ctx, ac, candidate, filePath, judge)
}

func (s *DownloadStep) probeDuration(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
	judge tracklength.FileJudge,
) (bool, sharedtracklength.TrackLength, *downloadRejection) {
	actual, err := s.prober.ProbeDuration(ctx, filePath)
	probed, lengthErr := sharedtracklength.TrackLengthOf(actual)
	if err == nil {
		err = lengthErr
	}
	switch {
	case err != nil && (ctx.Err() != nil || errors.Is(err, ports.ErrNoAudioProber)):
		return false, sharedtracklength.TrackLength{}, nil
	case err != nil:
		slog.WarnContext(ctx, "acquisition.probe_failed_accepting",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"probed_seconds", actual, "error", redact.LogError(err))
		s.recordSkip(ports.SkipProbeFailed)
		return false, sharedtracklength.TrackLength{}, nil
	}
	if acceptable, held := judge.Judge(actual); !acceptable {
		return false, sharedtracklength.TrackLength{}, durationRejection(ctx, ac, candidate, actual, held)
	}
	return true, probed, nil
}

func durationRejection(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	actual float64,
	held float64,
) *downloadRejection {
	slog.InfoContext(ctx, "acquisition.candidate_rejected_duration",
		"track_id", ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"actual_duration", actual,
		"expected_duration", held,
		"authoritative", ac.lengths().Resolved() > 0,
	)
	return &downloadRejection{
		stage:  rejectedcandidates.Duration,
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
			stage:  rejectedcandidates.Download,
			reason: "decode cancelled",
			err:    withCancellation(ctx, fmt.Errorf("candidate %q decode cancelled: %w", candidate.URL, err)),
		}
	}
	slog.WarnContext(ctx, "acquisition.candidate_rejected_undecodable",
		"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		"error", redact.LogError(err))
	return &downloadRejection{
		stage:  rejectedcandidates.Undecodable,
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
