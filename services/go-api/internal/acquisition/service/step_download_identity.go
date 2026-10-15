package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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

	return s.judgePreview(ctx, ac, candidate, match, time.Since(started))
}

func (s *DownloadStep) judgePreview(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	match ports.RecordingMatch,
	elapsed time.Duration,
) (*identification, *downloadRejection) {
	if ac.Replace.isCurrentFile(match) {
		return nil, currentFileRejection(ctx, ac, candidate)
	}
	verdict := s.classifyMatch(ctx, ac, match, candidate.Duration)
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
		"error", redact.LogError(err))
	s.recordSkip(gate)
}

func identifyFailureEvent(err error, fallback string) string {
	if errors.Is(err, acqdomain.ErrIdentifyThrottled) {
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
	previewDir, err := s.workspace.Create("preview-*")
	if err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("create preview dir: %w", err)
	}
	defer func() { _ = s.workspace.Remove(previewDir) }()

	previewPath, err := s.fetchPreview(ctx, previewer, candidate, previewDir)
	if err != nil {
		return ports.RecordingMatch{}, err
	}
	defer timeStage(ctx, "download.identify", time.Now())
	return s.identifier.Identify(ctx, previewPath, candidate.Duration)
}

func (s *DownloadStep) fetchPreview(
	ctx context.Context,
	previewer ports.PreviewFetcher,
	candidate ports.AudioCandidate,
	previewDir string,
) (string, error) {
	queued := time.Now()
	if err := s.limiter.Acquire(ctx); err != nil {
		return "", err
	}
	defer s.limiter.Release()
	timeStage(ctx, "download.queue", queued)
	fetchCtx, cancel := context.WithTimeout(ctx, s.candidateTimeout)
	defer cancel()
	defer timeStage(ctx, "download.preview", time.Now())
	path, err := previewer.FetchPreview(fetchCtx, candidate, previewDir, previewSeconds)
	if err != nil {
		return "", candidateTimedOut(ctx, fetchCtx, candidate, err)
	}
	return path, nil
}

func (s *DownloadStep) identify(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
	result *verificationResult,
) *downloadRejection {
	if s.identifier == nil || (ac.Identity.MBID == "" && ac.Replace.StoredAcoustID == "") {
		return nil
	}

	identifyStart := time.Now()
	match, err := s.identifier.Identify(ctx, filePath, 0)
	timeStage(ctx, "download.identify", identifyStart)
	if err != nil {
		s.noteIdentifyFailure(ctx, ac, candidate, err, "acquisition.identify_failed", ports.SkipIdentifyFailed)
		return nil
	}

	if ac.Replace.isCurrentFile(match) {
		return currentFileRejection(ctx, ac, candidate)
	}
	if ac.Identity.MBID == "" {
		return nil
	}
	result.verdict = s.classifyMatch(ctx, ac, match, result.probed)
	logAudioVerdict(ctx, ac, candidate, result.verdict)
	return judgeVerdict(ctx, ac, candidate, match, result)
}

func (s *DownloadStep) classifyMatch(ctx context.Context, ac *AcquisitionContext, match ports.RecordingMatch, probed float64) AudioVerdict {
	ref := referenceFor(ac)
	facts := s.sameVersionFacts(ctx, ref, SoftCandidateMBIDs(ref, probed, match.Results))
	return ClassifyAudio(ref, probed, match.Results, facts)
}

func referenceFor(ac *AcquisitionContext) AudioReference {
	mbids := ac.Identity.MBIDs
	if len(mbids) == 0 {
		mbids = []string{ac.Identity.MBID}
	}
	return AudioReference{Title: ac.Track.Title, Artist: ac.Track.Artist, Duration: ac.ExpectedLength(), MBIDs: mbids}
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
	if !match.Known() {
		slog.InfoContext(ctx, "acquisition.identify_unknown",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source)
		return nil
	}
	slog.InfoContext(ctx, "acquisition.identify_uncorroborated",
		"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		"want_mbid", ac.Identity.MBID, "got_mbids", match.MBIDs)
	return nil
}

func fingerprintRejection(candidate ports.AudioCandidate, reason string) *downloadRejection {
	return &downloadRejection{
		stage:  RejectionFingerprint,
		reason: reason,
		err:    fmt.Errorf("candidate %q rejected by fingerprint: %s", candidate.URL, reason),
	}
}

func currentFileRejection(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) *downloadRejection {
	slog.InfoContext(ctx, "acquisition.candidate_is_current_file",
		"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		"stored_acoustid", ac.Replace.StoredAcoustID)
	return &downloadRejection{
		stage:  RejectionCurrentFile,
		reason: "current file",
		err:    fmt.Errorf("candidate %q is the file the track already has", candidate.URL),
	}
}

const maxVersionLookupsPerCandidate = 3

type recordingFactsCache struct {
	mu      sync.Mutex
	entries map[string]ports.RecordingFacts
	missing map[string]bool
}

func (c *recordingFactsCache) get(mbid string) (facts ports.RecordingFacts, found, known bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.missing[mbid] {
		return ports.RecordingFacts{}, false, true
	}
	facts, found = c.entries[mbid]
	return facts, found, found
}

func (c *recordingFactsCache) put(mbid string, facts ports.RecordingFacts, found bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !found {
		if c.missing == nil {
			c.missing = make(map[string]bool)
		}
		c.missing[mbid] = true
		return
	}
	if c.entries == nil {
		c.entries = make(map[string]ports.RecordingFacts)
	}
	c.entries[mbid] = facts
}

func WithRecordingLookup(l ports.RecordingLookup) func(*DownloadStep) {
	return func(s *DownloadStep) { s.recordingLookup = l }
}

func (s *DownloadStep) sameVersionFacts(ctx context.Context, ref AudioReference, mbids []string) LookedUpRecordings {
	if s.recordingLookup == nil {
		return nil
	}
	facts := make(LookedUpRecordings)
	for _, mbid := range mbids[:min(len(mbids), maxVersionLookupsPerCandidate)] {
		recording, found := s.recordingFacts(ctx, mbid)
		if !found {
			continue
		}
		facts[mbid] = recording
		if sharesVersionLabels(ref.Title, recording) {
			break
		}
	}
	return facts
}

func (s *DownloadStep) recordingFacts(ctx context.Context, mbid string) (ports.RecordingFacts, bool) {
	if facts, found, known := s.factsCache.get(mbid); known {
		return facts, found
	}
	facts, found, err := s.recordingLookup.RecordingByMBID(ctx, mbid)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.version_lookup_failed", "mbid", mbid, "error", redact.LogError(err))
		return ports.RecordingFacts{}, false
	}
	s.factsCache.put(mbid, facts, found)
	return facts, found
}
