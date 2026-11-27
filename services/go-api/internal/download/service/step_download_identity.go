package service

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/audioverdict"
	"altune/go-api/internal/download/internal/core/candidateranking"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/rejectedcandidates"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/expiringcache"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

type identification struct {
	verdict  AudioVerdict
	identity bool
}

func (s *DownloadStep) previewer(ac *AcquisitionContext, candidate ports.AudioCandidate) (ports.PreviewFetcher, bool) {
	source, ok := s.fetcher.(previewSource)
	if !ok || ac.Identity.MBID == "" {
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

	started := s.now()
	match, err := s.identifyPreview(ctx, ac, candidate, previewer)
	if err != nil {
		s.noteIdentifyFailure(ctx, ac, candidate, err, "acquisition.preview_fallback", ports.SkipPreviewFallback)
		return nil, nil
	}

	return s.judgePreview(ctx, ac, candidate, match, s.now().Sub(started))
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
	verdict := s.classifyMatch(ctx, ac, match, candidate.Duration, sourceOf(ac, candidate))
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
	defaultEvent string,
	gate string,
) {
	if ctx.Err() != nil || errors.Is(err, ports.ErrNoAudioIdentifier) {
		return
	}
	slog.WarnContext(ctx, identifyFailureEvent(err, defaultEvent),
		logging.KeyTrackID, ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		logging.KeyError, redact.LogError(err))
	s.recordSkip(gate)
}

func identifyFailureEvent(err error, defaultEvent string) string {
	if errors.Is(err, acqdomain.ErrIdentifyThrottled) {
		return "acquisition.identify_throttled"
	}
	return defaultEvent
}

func logPreviewFingerprint(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	verdict AudioVerdict,
	elapsed time.Duration,
) {
	slog.InfoContext(ctx, "acquisition.preview_fingerprint",
		logging.KeyTrackID, ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
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
	defer removeScratch(ctx, s.workspace, previewDir)

	previewPath, err := s.fetchPreview(ctx, ac, previewer, candidate, previewDir)
	if err != nil {
		return ports.RecordingMatch{}, err
	}
	identifyCtx, identifyDone := startStage(ctx, ac, s.now, "download.identify")
	match, err := s.identifier.Identify(identifyCtx, previewPath, candidate.Duration)
	identifyDone(err)
	return match, err
}

func (s *DownloadStep) fetchPreview(
	ctx context.Context,
	ac *AcquisitionContext,
	previewer ports.PreviewFetcher,
	candidate ports.AudioCandidate,
	previewDir string,
) (string, error) {
	_, queued := startStage(ctx, ac, s.now, "download.queue")
	if err := s.limiter.Acquire(ctx); err != nil {
		queued(err)
		return "", err
	}
	defer s.limiter.Release()
	queued(nil)
	fetchCtx, cancel := context.WithTimeout(ctx, s.candidateTimeout)
	defer cancel()
	fetchCtx, previewDone := startStage(fetchCtx, ac, s.now, "download.preview")
	path, err := previewer.FetchPreview(fetchCtx, candidate, previewDir, previewSeconds)
	previewDone(err)
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
	if ac.Identity.MBID == "" && ac.Replace.StoredAcoustID == "" {
		return nil
	}

	identifyCtx, identifyDone := startStage(ctx, ac, s.now, "download.identify")
	match, err := s.identifier.Identify(identifyCtx, filePath, 0)
	identifyDone(err)
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
	result.verdict = s.classifyMatch(ctx, ac, match, result.probed.Seconds(), sourceOf(ac, candidate))
	logAudioVerdict(ctx, ac, candidate, result.verdict, match.Results)
	return judgeVerdict(ctx, ac, candidate, match, result)
}

type judgedSource struct {
	kind  audioquality.SourceKind
	title string
}

func sourceOf(ac *AcquisitionContext, candidate ports.AudioCandidate) judgedSource {
	return judgedSource{kind: SourceKindOf(candidate, candidateranking.UploaderOf(ac.Track.Artist, candidate)), title: candidate.Title}
}

func (s *DownloadStep) classifyMatch(ctx context.Context, ac *AcquisitionContext, match ports.RecordingMatch, probed float64, source judgedSource) AudioVerdict {
	input := judgeInput(ac, match.Results, probed, source)
	input.Audio.RecordingFacts = versionFactsOf(ctx, s.factsCache, s.recordingLookup, input)
	return AudioVerdictOf(audioverdict.Judge(input, s.cuts), match.Results)
}

func (s *DownloadStep) judgeUnfingerprinted(ac *AcquisitionContext, probed float64, source judgedSource) audioverdict.Verdict {
	return audioverdict.Judge(judgeInput(ac, nil, probed, source), s.cuts)
}

func judgeInput(ac *AcquisitionContext, results []ports.AcoustIDResult, length float64, source judgedSource) audioverdict.Input {
	return audioverdict.Input{
		Identity:       judgeIdentity(ac),
		Audio:          audioverdict.Audio{AcoustID: results, Length: length, RequireVersionFacts: true},
		Source:         audioverdict.Source{Kind: source.kind},
		CandidateTitle: source.title,
	}
}

func judgeIdentity(ac *AcquisitionContext) audioverdict.Identity {
	return audioverdict.Identity{
		Version:        identityVersionOf(ac),
		ISRC:           cmp.Or(ac.Track.ISRC, ac.Identity.ISRC),
		MBID:           ac.Identity.MBID,
		MergedMBIDs:    ac.Identity.MBIDs,
		Doubted:        ac.Identity.ReferenceDoubted,
		CatalogLengths: map[string]float64{"catalog": ac.Track.CatalogDuration, "recording": ac.lengths().Resolved()},
		Title:          ac.Track.Title,
		Artist:         ac.Track.Artist,
	}
}

func identityVersionOf(ac *AcquisitionContext) audioverdict.IdentityVersion {
	return audioverdict.IdentityVersion(fmt.Sprintf("mbid=%s;isrc=%s;doubted=%t", ac.Identity.MBID, cmp.Or(ac.Track.ISRC, ac.Identity.ISRC), ac.Identity.ReferenceDoubted))
}

const (
	maxLoggedSurviving     = 5
	maxLoggedResults       = 5
	maxLoggedRecordings    = 5
	maxLoggedReleaseGroups = 5
)

func logAudioVerdict(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate, verdict AudioVerdict, results []ports.AcoustIDResult) {
	slog.InfoContext(ctx, "acquisition.audio_verdict",
		logging.KeyTrackID, ac.Track.ID,
		"candidate_url", candidate.URL,
		"verdict", string(verdict.Kind),
		"judged", string(verdict.Judged.Kind),
		"grade", string(verdict.Judged.Grade),
		"reasons", verdict.Judged.Reasons,
		"score", verdict.Score,
		"surviving", loggedSurviving(verdict.Surviving),
		"results", loggedResults(results),
		"reference_doubted", ac.Identity.ReferenceDoubted,
	)
}

func loggedSurviving(surviving []ports.LinkedRecording) []map[string]any {
	logged := make([]map[string]any, 0, min(len(surviving), maxLoggedSurviving))
	for _, recording := range surviving[:min(len(surviving), maxLoggedSurviving)] {
		logged = append(logged, map[string]any{
			"mbid":                 recording.MBID,
			"title":                recording.Title,
			"release_groups":       loggedReleaseGroups(recording.ReleaseGroups),
			"release_groups_total": len(recording.ReleaseGroups),
		})
	}
	return logged
}

func loggedResults(results []ports.AcoustIDResult) []map[string]any {
	logged := make([]map[string]any, 0, min(len(results), maxLoggedResults))
	for _, result := range results[:min(len(results), maxLoggedResults)] {
		recordings := make([]string, 0, min(len(result.Recordings), maxLoggedRecordings))
		for _, recording := range result.Recordings[:min(len(result.Recordings), maxLoggedRecordings)] {
			recordings = append(recordings, recording.MBID)
		}
		logged = append(logged, map[string]any{"score": result.Score, "recordings": recordings})
	}
	return logged
}

func loggedReleaseGroups(groups []ports.ReleaseGroup) []map[string]any {
	logged := make([]map[string]any, 0, min(len(groups), maxLoggedReleaseGroups))
	for _, group := range groups[:min(len(groups), maxLoggedReleaseGroups)] {
		logged = append(logged, map[string]any{"type": group.Type, "secondary_types": group.SecondaryTypes})
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
	case VerdictHard:
		result.identity = true
		return nil
	case VerdictSoft:
		return nil
	case VerdictContradicted:
		return fingerprintRejection(candidate, "contradicted: "+strings.Join(result.verdict.Judged.Reasons, "; "))
	case VerdictOtherVersion, VerdictDifferentSong, VerdictUnknown:
		return judgeUnknown(ctx, ac, candidate, match)
	}
	return judgeUnknown(ctx, ac, candidate, match)
}

func judgeUnknown(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	match ports.RecordingMatch,
) *downloadRejection {
	if !match.Known() {
		slog.InfoContext(ctx, "acquisition.identify_unknown",
			logging.KeyTrackID, ac.Track.ID, "url", candidate.URL, "source", candidate.Source)
		return nil
	}
	slog.InfoContext(ctx, "acquisition.identify_uncorroborated",
		logging.KeyTrackID, ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		"want_mbid", ac.Identity.MBID, "got_mbids", match.MBIDs)
	return nil
}

func fingerprintRejection(candidate ports.AudioCandidate, reason string) *downloadRejection {
	return &downloadRejection{
		stage:  rejectedcandidates.Fingerprint,
		reason: reason,
		err:    fmt.Errorf("candidate %q rejected by fingerprint: %s", candidate.URL, reason),
	}
}

func currentFileRejection(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) *downloadRejection {
	slog.InfoContext(ctx, "acquisition.candidate_is_current_file",
		logging.KeyTrackID, ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
		"stored_acoustid", ac.Replace.StoredAcoustID)
	return &downloadRejection{
		stage:  rejectedcandidates.CurrentFile,
		reason: "current file",
		err:    fmt.Errorf("candidate %q is the file the track already has", candidate.URL),
	}
}

const maxVersionLookupsPerCandidate = 3

const (
	recordingFactsTTL = 10 * time.Minute
	recordingFactsCap = 256

	recordingLookupBudget = 30 * time.Second
)

type recordedFacts struct {
	facts ports.RecordingFacts
	found bool
}

type recordingFactsCache struct {
	memo   *expiringcache.Memo[string, recordedFacts]
	flight singleflight.Group
}

func newRecordingFactsCache(now clock.Func) *recordingFactsCache {
	return &recordingFactsCache{memo: expiringcache.New[string, recordedFacts](recordingFactsTTL, recordingFactsCap, now)}
}

type recordingFetch func(ctx context.Context, mbid string) (ports.RecordingFacts, bool, error)

func (c *recordingFactsCache) lookup(ctx context.Context, mbid string, fetch recordingFetch) (ports.RecordingFacts, bool, error) {
	if known, ok := c.memo.Get(mbid); ok {
		return known.facts, known.found, nil
	}
	pending := c.flight.DoChan(mbid, func() (any, error) {
		if known, ok := c.memo.Peek(mbid); ok {
			return known, nil
		}
		flightCtx, cancel := background.Detach(ctx, recordingLookupBudget)
		defer cancel()
		known, err := fetchRecorded(flightCtx, mbid, fetch)
		if err != nil {
			return nil, err
		}
		c.memo.Put(mbid, known)
		return known, nil
	})
	select {
	case <-ctx.Done():
		return ports.RecordingFacts{}, false, ctx.Err()
	case result := <-pending:
		if result.Err != nil {
			return ports.RecordingFacts{}, false, result.Err
		}
		known := result.Val.(recordedFacts)
		return known.facts, known.found, nil
	}
}

func fetchRecorded(ctx context.Context, mbid string, fetch recordingFetch) (known recordedFacts, err error) {
	panicked := background.SafeRun(ctx, "acquisition.recording_lookup_panicked", func() {
		var facts ports.RecordingFacts
		var found bool
		facts, found, err = fetch(ctx, mbid)
		known = recordedFacts{facts: facts, found: found}
	}, "mbid", mbid)
	if panicked != nil {
		return recordedFacts{}, fmt.Errorf("run background task: %w", panicked)
	}
	return known, err
}

func versionFactsOf(ctx context.Context, cache *recordingFactsCache, lookup ports.RecordingLookup, input audioverdict.Input) map[string]ports.RecordingFacts {
	facts := make(map[string]ports.RecordingFacts)
	candidates := audioverdict.VersionLookupCandidates(input)
	for _, mbid := range candidates[:min(len(candidates), maxVersionLookupsPerCandidate)] {
		if recording, found := cachedRecordingFacts(ctx, cache, lookup, mbid); found {
			facts[mbid] = recording
		}
	}
	if len(candidates) > 0 {
		hits, misses := cache.memo.Stats()
		slog.InfoContext(ctx, "acquisition.recording_facts_memo", "hits", hits, "misses", misses)
	}
	return facts
}

func cachedRecordingFacts(ctx context.Context, cache *recordingFactsCache, lookup ports.RecordingLookup, mbid string) (ports.RecordingFacts, bool) {
	facts, found, err := cache.lookup(ctx, mbid, lookup.RecordingByMBID)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.version_lookup_failed", "mbid", mbid, logging.KeyError, redact.LogError(err))
		return ports.RecordingFacts{}, false
	}
	return facts, found
}
