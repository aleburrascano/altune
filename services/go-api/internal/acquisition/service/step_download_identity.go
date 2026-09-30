package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

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
		s.noteIdentifyFailure(ctx, ac, candidate, err, "acquisition.preview_fallback", ports.SkipPreviewFallback)
		return nil, nil
	}

	return judgePreview(ctx, ac, candidate, match, time.Since(started))
}

func judgePreview(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	match ports.RecordingMatch,
	elapsed time.Duration,
) (*identification, *downloadRejection) {
	verdict := classifyMatch(ac, match, candidate.Duration)
	logPreviewFingerprint(ctx, ac, candidate, verdict, elapsed)
	judged := verificationResult{verdict: verdict}
	if rejection := judgeVerdict(ctx, ac, candidate, match, &judged); rejection != nil {
		return nil, rejection
	}
	return &identification{verdict: verdict, identity: judged.identity}, nil
}

func (s *DownloadStep) noteIdentifyFailure(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	err error,
	fallbackEvent string,
	gate string,
) {
	slog.WarnContext(ctx, identifyFailureEvent(err, fallbackEvent),
		"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		"error", logSafeError(err))
	s.recordSkip(gate)
}

func identifyFailureEvent(err error, fallback string) string {
	if errors.Is(err, ports.ErrIdentifyThrottled) {
		return "acquisition.identify_throttled"
	}
	return fallback
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
	defer func() { _ = os.RemoveAll(previewDir) }()

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
	fetchCtx, cancel := context.WithTimeout(ctx, s.candidateTimeout)
	defer cancel()
	return previewer.FetchPreview(fetchCtx, candidate, previewDir, previewSeconds)
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
		s.noteIdentifyFailure(ctx, ac, candidate, err, "acquisition.identify_failed", ports.SkipIdentifyFailed)
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
