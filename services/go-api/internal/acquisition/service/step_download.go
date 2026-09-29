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
}

func NewDownloadStep(fetcher candidateFetcher, opts ...func(*DownloadStep)) *DownloadStep {
	s := &DownloadStep{fetcher: fetcher, width: 1}
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

func (s *DownloadStep) Name() string { return stepNameDownload }

func (s *DownloadStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterSelect) (afterDownload, error) {
	if s.width > 1 {
		return s.executeWindowed(ctx, ac)
	}
	var failures downloadFailures
	attempts := 0

	for i := range ac.Ranked {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return afterDownload{}, fmt.Errorf("download cancelled: %w", ctxErr)
		}
		if attempts >= maxDownloadAttempts {
			recordNotAttempted(ac, ac.Ranked[i:])
			break
		}
		if !ac.candidateDurationPlausible(ac.Ranked[i]) {
			recordImplausibleDuration(ctx, ac, ac.Ranked[i])
			continue
		}

		tmpDir, err := os.MkdirTemp("", tempDirPrefix+"*")
		if err != nil {
			return afterDownload{}, fmt.Errorf("create temp dir: %w", err)
		}

		attempts++
		selected, err := s.tryCandidate(ctx, ac, ac.Ranked[i], tmpDir)
		if selected {
			return afterDownload{}, nil
		}
		failures.note(err)
	}

	return afterDownload{}, failures.result(ctx)
}

type downloadFailures struct {
	last, unavailable error
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

func (f *downloadFailures) result(ctx context.Context) error {
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

func (s *DownloadStep) tryCandidate(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	tmpDir string,
) (selected bool, err error) {
	result := s.runAttempt(ctx, ac, candidate, tmpDir)
	result.applyTo(ac)
	return result.accepted, result.err()
}

type attempt struct {
	candidate ports.AudioCandidate
	filePath  string
	tmpDir    string
	verified  verificationResult
	rejection *downloadRejection
	accepted  bool
}

func (a attempt) err() error {
	if a.rejection == nil {
		return nil
	}
	return a.rejection.err
}

func (a attempt) applyTo(ac *AcquisitionContext) {
	if a.rejection != nil {
		ac.recordRejection(a.candidate.URL, a.candidate.Title, a.candidate.Source, a.rejection.stage, a.rejection.reason)
	}
	if !a.accepted {
		return
	}
	sel := a.candidate
	ac.Selected = &sel
	ac.TempPath = a.filePath
	ac.DurationVerified = a.verified.duration
	ac.IdentityVerified = a.verified.identity
	ac.ProbedDuration = a.verified.probed
	ac.Verdict = a.verified.verdict
}

func (s *DownloadStep) runAttempt(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	tmpDir string,
) (result attempt) {
	defer func() {
		if !result.accepted {
			os.RemoveAll(tmpDir)
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
	return attempt{filePath: filePath, verified: verified, rejection: rejection, accepted: rejection == nil}
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

	filePath, err := s.fetcher.Fetch(ctx, candidate, tmpDir)
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

	if s.prober != nil && ac.Track.Duration > 0 {
		actual, err := s.prober.ProbeDuration(ctx, filePath)
		switch {
		case err != nil:
			slog.WarnContext(ctx, "acquisition.probe_failed_accepting",
				"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
				"error", logSafeError(err))
		case !ac.durationAcceptable(actual):
			slog.InfoContext(ctx, "acquisition.candidate_rejected_duration",
				"track_id", ac.Track.ID,
				"url", candidate.URL,
				"source", candidate.Source,
				"actual_duration", actual,
				"expected_duration", ac.Track.Duration,
				"authoritative", ac.Identity.Duration > 0,
			)
			return result, &downloadRejection{
				stage:  RejectionDuration,
				reason: fmt.Sprintf("duration %.0fs vs expected %.0fs", actual, ac.Track.Duration),
				err: fmt.Errorf("candidate %q duration %.0fs != expected %.0fs",
					candidate.URL, actual, ac.Track.Duration),
			}
		default:
			result.duration = true
			result.probed = actual
		}
	}

	if s.prober != nil {
		if err := s.prober.ValidateDecodable(ctx, filePath); err != nil {
			slog.WarnContext(ctx, "acquisition.candidate_rejected_undecodable",
				"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
				"error", logSafeError(err))
			return result, &downloadRejection{
				stage:  RejectionUndecodable,
				reason: "audio failed to decode",
				err:    fmt.Errorf("candidate %q undecodable: %w", candidate.URL, err),
			}
		}
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

type identification struct {
	verdict  AudioVerdict
	identity bool
}

func (s *DownloadStep) previewer(ac *AcquisitionContext, candidate ports.AudioCandidate) (ports.PreviewFetcher, bool) {
	source, ok := s.fetcher.(previewSource)
	if !ok || s.identifier == nil || ac.Identity.MBID == "" {
		return nil, false
	}
	return source.PreviewFetcherFor(candidate)
}

func (s *DownloadStep) previewIdentification(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
) (*identification, *downloadRejection) {
	previewer, ok := s.previewer(ac, candidate)
	if !ok {
		return nil, nil
	}

	started := time.Now()
	match, err := s.identifyPreview(ctx, ac, candidate, previewer)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.preview_fallback",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"error", logSafeError(err))
		s.recordSkip(ports.SkipPreviewFallback)
		return nil, nil
	}

	verdict := classifyMatch(ac, match, candidate.Duration)
	logPreviewFingerprint(ctx, ac, candidate, verdict, time.Since(started))
	judged := verificationResult{verdict: verdict}
	if rejection := judgeVerdict(ctx, ac, candidate, match, &judged); rejection != nil {
		return nil, rejection
	}
	return &identification{verdict: verdict, identity: judged.identity}, nil
}

func logPreviewFingerprint(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	verdict AudioVerdict,
	elapsed time.Duration,
) {
	slog.InfoContext(ctx, "acquisition.preview_fingerprint",
		"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		"fetch_ms", elapsed.Milliseconds(), "verdict", string(verdict.Kind))
}

func (s *DownloadStep) identifyPreview(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	previewer ports.PreviewFetcher,
) (ports.RecordingMatch, error) {
	previewDir, err := os.MkdirTemp("", tempDirPrefix+"preview-*")
	if err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("create preview dir: %w", err)
	}
	defer os.RemoveAll(previewDir)

	previewPath, err := s.fetchPreview(ctx, previewer, candidate, previewDir)
	if err != nil {
		return ports.RecordingMatch{}, err
	}
	return s.identifier.Identify(ctx, previewPath, candidate.Duration)
}

func (s *DownloadStep) fetchPreview(
	ctx context.Context,
	previewer ports.PreviewFetcher,
	candidate ports.AudioCandidate,
	previewDir string,
) (string, error) {
	if err := s.limiter.Acquire(ctx); err != nil {
		return "", err
	}
	defer s.limiter.Release()
	return previewer.FetchPreview(ctx, candidate, previewDir, previewSeconds)
}

func (s *DownloadStep) identify(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
	result *verificationResult,
) *downloadRejection {
	if s.identifier == nil || ac.Identity.MBID == "" {
		return nil
	}

	match, err := s.identifier.Identify(ctx, filePath, 0)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.identify_failed",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"error", logSafeError(err))
		s.recordSkip(ports.SkipIdentifyFailed)
		return nil
	}

	result.verdict = classifyMatch(ac, match, result.probed)
	logAudioVerdict(ctx, ac, candidate, result.verdict)
	return judgeVerdict(ctx, ac, candidate, match, result)
}

func classifyMatch(ac *AcquisitionContext, match ports.RecordingMatch, probed float64) AudioVerdict {
	verdict := ClassifyAudio(referenceFor(ac), probed, match.Results)
	if verdict.Kind == VerdictUnknown && match.Known() && identityAgrees(ac, match) {
		return AudioVerdict{Kind: VerdictHard, Score: match.Score}
	}
	return verdict
}

func identityAgrees(ac *AcquisitionContext, match ports.RecordingMatch) bool {
	return match.Matches(ac.Identity.MBID) || match.InCluster(ac.Identity.AcoustIDs)
}

func referenceFor(ac *AcquisitionContext) AudioReference {
	mbids := ac.Identity.MBIDs
	if len(mbids) == 0 {
		mbids = []string{ac.Identity.MBID}
	}
	duration := ac.Identity.Duration
	if duration == 0 {
		duration = ac.Track.Duration
	}
	return AudioReference{Title: ac.Track.Title, Artist: ac.Track.Artist, Duration: duration, MBIDs: mbids}
}

const maxLoggedSurviving = 5

func logAudioVerdict(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate, verdict AudioVerdict) {
	slog.InfoContext(ctx, "acquisition.audio_verdict",
		"track_id", ac.Track.ID,
		"candidate_url", candidate.URL,
		"verdict", string(verdict.Kind),
		"score", verdict.Score,
		"surviving", loggedSurviving(verdict.Surviving),
		"reference_doubted", ac.Identity.ReferenceDoubted,
	)
}

func loggedSurviving(surviving []ports.LinkedRecording) []map[string]string {
	logged := make([]map[string]string, 0, min(len(surviving), maxLoggedSurviving))
	for _, recording := range surviving[:min(len(surviving), maxLoggedSurviving)] {
		logged = append(logged, map[string]string{"mbid": recording.MBID, "title": recording.Title})
	}
	return logged
}

func judgeVerdict(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	match ports.RecordingMatch,
	result *verificationResult,
) *downloadRejection {
	switch result.verdict.Kind {
	case VerdictHard, VerdictSoft:
		result.identity = true
		return nil
	case VerdictOtherVersion:
		return fingerprintRejection(candidate, "other version")
	case VerdictDifferentSong:
		return fingerprintRejection(candidate, "different song")
	default:
		return judgeUnknown(ctx, ac, candidate, match)
	}
}

func judgeUnknown(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	match ports.RecordingMatch,
) *downloadRejection {
	switch {
	case !match.Known():
		slog.InfoContext(ctx, "acquisition.identify_unknown",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source)
		return nil
	case len(ac.Identity.AcoustIDs) == 0:
		slog.InfoContext(ctx, "acquisition.identify_uncorroborated",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"want_mbid", ac.Identity.MBID, "got_mbids", match.MBIDs)
		return nil
	default:
		logFingerprintRejected(ctx, ac, candidate, match)
		return fingerprintRejection(candidate, "different recording")
	}
}

func logFingerprintRejected(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	match ports.RecordingMatch,
) {
	slog.InfoContext(ctx, "acquisition.candidate_rejected_fingerprint",
		"track_id", ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"want_mbid", ac.Identity.MBID,
		"want_acoustids", ac.Identity.AcoustIDs,
		"got_acoustid", match.AcoustID,
		"got_mbids", match.MBIDs,
	)
}

func fingerprintRejection(candidate ports.AudioCandidate, reason string) *downloadRejection {
	return &downloadRejection{
		stage:  RejectionFingerprint,
		reason: reason,
		err:    fmt.Errorf("candidate %q rejected by fingerprint: %s", candidate.URL, reason),
	}
}

func (s *DownloadStep) Rollback(_ context.Context, ac *AcquisitionContext) error {
	if ac.TempPath != "" {
		os.RemoveAll(ac.TempPath)
	}
	return nil
}
