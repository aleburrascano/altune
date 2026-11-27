package service

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/audioverdict"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/tracklength"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/redact"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type AcquireTrackAudioService struct {
	ledger          ports.LibraryTracks
	sources         *SourceRegistry
	audioStore      ports.AudioWriter
	audioProber     ports.AudioProber
	audioTagger     ports.AudioTagger
	identifier      ports.AudioIdentifier
	recordings      ports.RecordingResolver
	rerecordings    ports.RerecordingFinder
	events          events.Publisher
	orphans         ports.OrphanedAudioRecorder
	storeKeyPrefix  string
	rejections      ports.RejectionStore
	downloadLimiter *ConcurrencyLimit
	recordingLookup ports.RecordingLookup
	verifySkips     ports.VerifySkipRecorder
	confidenceFloor float64
	workspace       ports.ScratchFolder
	storedAudio     ports.StoredAudioReader
	contextObserver func(*AcquisitionContext)
	sleep           clock.Sleeper
	now             clock.Func
	unit            random.Unit
	previews        ports.PreviewMatcher

	cuts            audioverdict.Cuts
	acquireTimeout  time.Duration
	supersededGrace time.Duration
}

func NewAcquireTrackAudioService(
	ledger ports.LibraryTracks,
	sources *SourceRegistry,
	audioStore ports.AudioWriter,
	workspace ports.ScratchFolder,
	wiring VerificationPorts,
	stages StagePorts,
	opts ...func(*AcquireTrackAudioService),
) (*AcquireTrackAudioService, error) {
	const owner = "NewAcquireTrackAudioService"
	if err := errors.Join(
		missingPort(owner, "ledger", ledger),
		missingPort(owner, "source registry", sources),
		missingPort(owner, "audio store", audioStore),
		wiring.validate(owner), stages.validate(owner), requireWorkspace(owner, workspace),
	); err != nil {
		return nil, err
	}
	s := &AcquireTrackAudioService{
		ledger:          ledger,
		sources:         sources,
		audioStore:      audioStore,
		workspace:       workspace,
		identifier:      wiring.Identifier,
		recordingLookup: wiring.RecordingLookup,
		rerecordings:    wiring.Rerecordings,
		previews:        wiring.Previews,
		audioProber:     stages.Prober,
		audioTagger:     stages.Tagger,
		rejections:      stages.Rejections,
		storedAudio:     stages.StoredAudio,
		orphans:         stages.Orphans,
		verifySkips:     stages.Skips,
		recordings:      wiring.Recordings,
		events:          stages.Events,
		downloadLimiter: stages.DownloadLimit,
		contextObserver: stages.Observer,
		sleep:           stages.Sleep,
		now:             stages.Now,
		unit:            stages.Unit,
		cuts:            audioverdict.DefaultCuts(),

		confidenceFloor: defaultConfidenceFloor,
		acquireTimeout:  AcquireTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

func MustNewAcquireTrackAudioService(
	ledger ports.LibraryTracks,
	sources *SourceRegistry,
	audioStore ports.AudioWriter,
	workspace ports.ScratchFolder,
	wiring VerificationPorts,
	stages StagePorts,
	opts ...func(*AcquireTrackAudioService),
) *AcquireTrackAudioService {
	s, err := NewAcquireTrackAudioService(ledger, sources, audioStore, workspace, wiring, stages, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

func WithAcquireConfidenceFloor(floor float64) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.confidenceFloor = floor }
}

func WithAcquireTimeout(d time.Duration) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.acquireTimeout = d }
}

func WithSupersededAudioGrace(d time.Duration) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.supersededGrace = d }
}

func WithAcquireStoreKeyPrefix(prefix string) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.storeKeyPrefix = prefix }
}

const AcquireTimeout = 10 * time.Minute

func (s *AcquireTrackAudioService) outcomes() outcomeReporter {
	return outcomeReporter{ledger: s.ledger, events: s.events}
}

func (s *AcquireTrackAudioService) supersededAudio() supersededAudio {
	return supersededAudio{ledger: s.ledger, orphans: s.orphans, grace: s.supersededGrace, now: s.now}
}

func (s *AcquireTrackAudioService) storedFiles() storedFileFingerprinter {
	return storedFileFingerprinter{workspace: s.workspace, reader: s.storedAudio, identifier: s.identifier}
}

func (s *AcquireTrackAudioService) Execute(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
	ctx = logging.ContextWithSubjectUser(ctx, userId.String())
	jobCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, s.acquireTimeout)
	defer cancel()
	track, found, err := s.loadTrack(ctx, userId, trackId)
	if err != nil || !found {
		return err
	}
	proceed, err := s.reconcileForReacquire(ctx, track)
	if err != nil || !proceed {
		return err
	}
	ac := s.startAcquisition(ctx, userId, trackId, track)
	defer s.observe(ac)
	defer CleanupTemp(ctx, s.workspace, ac)
	s.loadPriorRejections(ctx, trackId, ac)
	if err := s.runAcquisition(ctx, userId, trackId, ac); err != nil {
		return s.outcomes().settleExecute(ctx, jobCtx, userId, trackId, err, ac)
	}
	return nil
}

func (s *AcquireTrackAudioService) observe(ac *AcquisitionContext) {
	s.contextObserver(ac)
}

func (s *AcquireTrackAudioService) ExecuteReplace(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
	ctx = logging.ContextWithSubjectUser(ctx, userId.String())
	jobCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, s.acquireTimeout)
	defer cancel()
	track, found, err := s.loadTrack(ctx, userId, trackId)
	if err != nil || !found {
		return err
	}
	_, err = s.replaceLoaded(ctx, jobCtx, userId, track, replacement.ReplacementGate{}, WillRetry)
	return err
}

func (s *AcquireTrackAudioService) ReplaceGated(ctx context.Context, userId ids.UserId, track *ports.TrackSnapshot, gate replacement.ReplacementGate) (replacement.Attempt, error) {
	ctx = logging.ContextWithSubjectUser(ctx, userId.String())
	jobCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, s.acquireTimeout)
	defer cancel()
	ac, err := s.forUpgradeSweep().replaceLoaded(ctx, jobCtx, userId, track, gate, neverRetry)
	return attemptOf(ac, err), err
}

func (s *AcquireTrackAudioService) forUpgradeSweep() *AcquireTrackAudioService {
	sweep := *s
	sweep.events = events.NoopPublisher()
	sweep.rerecordings = ports.NoopRerecordingFinder()
	return &sweep
}

func attemptOf(ac *AcquisitionContext, err error) replacement.Attempt {
	attempt := replacement.Attempt{NoSource: err != nil && foundNoSource(err)}
	if ac != nil && ac.Selected != nil {
		attempt.SourceKey = sourceKey(ac.Selected.URL)
		attempt.Grade = ac.CollectEvidence().Grade
	}
	return attempt
}

func (s *AcquireTrackAudioService) replaceLoaded(ctx, jobCtx context.Context, userId ids.UserId, track *ports.TrackSnapshot, gate replacement.ReplacementGate, retry retryPolicy) (*AcquisitionContext, error) {
	trackId := track.ID
	ac := s.startAcquisition(ctx, userId, trackId, track)
	ac.Replace.Gate = gate
	ac.Replace.StoredKnownWrong = storedFileKnownWrong(track)
	defer s.observe(ac)
	defer CleanupTemp(ctx, s.workspace, ac)
	if err := configureReplaceExclusion(ctx, ac, track, trackId); err != nil {
		return ac, s.outcomes().settleReplace(ctx, jobCtx, userId, trackId, err, retry, ac)
	}
	s.storedFiles().fingerprint(ctx, ac, track)
	s.loadPriorRejections(ctx, trackId, ac)
	if err := s.runAcquisition(ctx, userId, trackId, ac); err != nil {
		return ac, s.outcomes().settleReplace(ctx, jobCtx, userId, trackId, err, retry, ac)
	}
	s.supersededAudio().release(ctx, userId, trackId, ac)
	return ac, nil
}

func (s *AcquireTrackAudioService) loadTrack(ctx context.Context, userId ids.UserId, trackId ids.TrackId) (*ports.TrackSnapshot, bool, error) {
	track, err := s.ledger.Get(ctx, trackId, userId)
	if errors.Is(err, ports.ErrTrackNotFound) {
		slog.WarnContext(ctx, "acquire_track_not_found", logging.KeyTrackID, trackId.String())
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get track: %w", err)
	}
	return track, true, nil
}

func (s *AcquireTrackAudioService) startAcquisition(ctx context.Context, userId ids.UserId, trackId ids.TrackId, track *ports.TrackSnapshot) *AcquisitionContext {
	jobReporterFrom(ctx).meta(track.Title, track.Artist, track.Album)

	slog.InfoContext(ctx, "track_acquisition_started",
		logging.KeyTrackID, trackId.String(),
		"has_isrc", track.ISRC != nil,
	)
	s.events.Publish(ctx, userId, events.TrackAcquisitionStarted{
		TrackID: trackId.String(),
		Title:   track.Title,
		Artist:  track.Artist,
	})
	return &AcquisitionContext{
		Track:   buildTrackRef(track),
		Replace: ReplaceState{ExcludeKeys: mergeSourceKeys(track.RejectedSourceKeys)},
	}
}

func (s *AcquireTrackAudioService) runAcquisition(ctx context.Context, userId ids.UserId, trackId ids.TrackId, ac *AcquisitionContext) error {
	defer s.recordRejections(ctx, ac)
	defer reportDecision(ctx, ac)
	identityCtx, identityDone := startStage(ctx, s.now, "identity")
	s.resolveIdentity(identityCtx, ac)
	identityDone(nil)
	steps := s.buildSteps(userId, trackId)
	failure := RunPipeline(ctx, steps, ac)
	if failure != nil {
		failure = s.takeRerecording(ctx, steps, ac, failure)
	}
	if failure != nil {
		return failure
	}

	jobReporterFrom(ctx).fetched(ac.SourceFormat)
	jobReporterFrom(ctx).provenance(string(ac.Provenance()))
	s.outcomes().completed(ctx, userId, trackId, ac.AudioRef)
	return nil
}

func (s *AcquireTrackAudioService) takeRerecording(ctx context.Context, steps Pipeline, ac *AcquisitionContext, originalErr error) error {
	originalMBID := cmp.Or(ac.Identity.MBID, ac.Track.CatalogMBID)
	if !s.mayTakeRerecording(ctx, ac, originalErr) || originalMBID == "" {
		return originalErr
	}
	label, found := s.findRerecording(ctx, ac, originalMBID)
	if !found {
		return originalErr
	}
	err := s.runRerecordingPass(ctx, steps, ac, label)
	if err == nil {
		logRerecordingTier(ctx, ac, originalMBID, label.MBID, "taken", nil)
		return nil
	}
	logRerecordingTier(ctx, ac, originalMBID, label.MBID, "pass_failed", err)
	if ctx.Err() != nil {
		return err
	}
	return originalErr
}

func (s *AcquireTrackAudioService) runRerecordingPass(ctx context.Context, steps Pipeline, ac *AcquisitionContext, label ports.TrackLabel) error {
	pass := ac.Snapshot()
	pass.retargetTo(label)
	s.resolveIdentity(ctx, &pass)
	if err := RunPipeline(ctx, steps, &pass); err != nil {
		return err
	}
	s.recordRejections(ctx, ac)
	pass.Rejections = pass.Rejections[len(ac.Rejections):]
	*ac = pass
	return nil
}

func (s *AcquireTrackAudioService) mayTakeRerecording(ctx context.Context, ac *AcquisitionContext, err error) bool {
	return noSourceHasIt(err) && ctx.Err() == nil && ac.Replace.allowsRerecording()
}

func (s *AcquireTrackAudioService) findRerecording(ctx context.Context, ac *AcquisitionContext, originalMBID string) (ports.TrackLabel, bool) {
	label, found, err := s.rerecordings.FindRerecording(ctx, ports.RerecordingQuery{OriginalMBID: originalMBID})
	switch {
	case err != nil:
		logRerecordingTier(ctx, ac, originalMBID, "", "finder_failed", err)
	case !found:
		logRerecordingTier(ctx, ac, originalMBID, "", "none", nil)
	case label.MBID == ac.Track.CatalogMBID:
		logRerecordingTier(ctx, ac, originalMBID, label.MBID, "already_the_rerecording", nil)
	default:
		return label, true
	}
	return ports.TrackLabel{}, false
}

func logRerecordingTier(ctx context.Context, ac *AcquisitionContext, originalMBID, rerecordingMBID, outcome string, err error) {
	attrs := []any{logging.KeyTrackID, ac.Track.ID, "original_mbid", originalMBID, "rerecording_mbid", rerecordingMBID, "outcome", outcome}
	if err != nil {
		slog.WarnContext(ctx, "acquisition.rerecording_tier", append(attrs, logging.KeyError, redact.LogError(err))...)
		return
	}
	slog.InfoContext(ctx, "acquisition.rerecording_tier", attrs...)
}

func storedFileKnownWrong(track *ports.TrackSnapshot) bool {
	catalog, stored := deref(track.CatalogDurationSeconds), deref(track.StoredDurationSeconds)
	if catalog > 0 && stored > 0 && !tracklength.SameRecording(catalog, stored) {
		return true
	}
	var evidence Evidence
	return json.Unmarshal(track.Evidence, &evidence) == nil && evidence.Grade == audioquality.F
}

func reportDecision(ctx context.Context, ac *AcquisitionContext) {
	var key string
	if ac.Selected != nil {
		key = sourceKey(ac.Selected.URL)
	}
	jobReporterFrom(ctx).decided(ac.Confidence, key, ac.ExpectedLength(), ac.StoredLength().Seconds())
}

func configureReplaceExclusion(ctx context.Context, ac *AcquisitionContext, track *ports.TrackSnapshot, trackId ids.TrackId) error {
	ac.Replace.PreservedRef = deref(track.AudioRef)
	ac.Track.AudioSourceURL = ""
	if !ac.refetchesCurrentSource() {
		ac.Replace.ExcludeKeys = mergeSourceKeys(
			ac.Replace.ExcludeKeys,
			sourceKey(deref(track.AudioSourceURL)),
		)
	}
	if track.AudioSourceURL == nil {
		slog.InfoContext(ctx, "acquisition.replacing_unknown_source", logging.KeyTrackID, trackId.String())
	}
	if len(ac.Replace.ExcludeKeys) > 0 {
		slog.InfoContext(ctx, "acquisition.replacing_source",
			logging.KeyTrackID, trackId.String(), "excluded_keys", ac.Replace.ExcludeKeys)
	}
	return requireCurrentFileFact(track)
}

func requireCurrentFileFact(track *ports.TrackSnapshot) error {
	if sourceKey(deref(track.AudioSourceURL)) != "" || deref(track.StoredDurationSeconds) > 0 {
		return nil
	}
	return ErrReplaceFactMissing
}

func (s *AcquireTrackAudioService) reconcileForReacquire(ctx context.Context, track *ports.TrackSnapshot) (proceed bool, err error) {
	return reacquirePolicy{ledger: s.ledger, audioStore: s.audioStore}.reconcile(ctx, track)
}

func (s *AcquireTrackAudioService) RefuseQueued(ctx context.Context, userId ids.UserId, trackId ids.TrackId) {
	s.outcomes().refuseQueued(logging.ContextWithSubjectUser(ctx, userId.String()), userId, trackId)
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

func buildTrackRef(track *ports.TrackSnapshot) TrackRef {
	return TrackRef{
		ID:          track.ID.String(),
		UserID:      track.UserId.String(),
		Title:       track.Title,
		Artist:      track.Artist,
		Album:       track.Album,
		ISRC:        deref(track.ISRC),
		Year:        deref(track.Year),
		TrackNumber: deref(track.TrackNumber),
		AlbumArtist: deref(track.AlbumArtist),
		Genre:       deref(track.Genre),

		CatalogDuration: deref(track.CatalogDurationSeconds),
		CatalogExplicit: track.CatalogExplicit,
		CatalogMBID:     deref(track.CatalogMBID),
		CatalogSources:  track.CatalogSources,

		AudioSourceURL: deref(track.AudioSourceURL),
	}
}

func mergeSourceKeys(existing []string, add ...string) []string {
	merged := make([]string, 0, len(existing)+len(add))
	seen := make(map[string]bool, len(existing)+len(add))
	for _, key := range append(append([]string{}, existing...), add...) {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, key)
	}
	return merged
}
