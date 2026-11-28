package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"time"
)

type AcquireTrackAudioService struct {
	*pipeline
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
	s := &AcquireTrackAudioService{pipeline: &pipeline{
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
		evidence:        stages.Evidence,
		recordings:      wiring.Recordings,
		events:          stages.Events,
		downloadLimiter: stages.DownloadLimit,
		contextObserver: stages.Observer,
		sleep:           stages.Sleep,
		now:             stages.Now,
		unit:            stages.Unit,
		cuts:            DefaultCuts(),

		confidenceFloor: defaultConfidenceFloor,
		acquireTimeout:  AcquireTimeout,
	}}
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

func (s *AcquireTrackAudioService) Execute(ctx context.Context, job domain.AcquisitionJob) error {
	ctx, cancel := s.acquireBudget(ctx, job)
	defer cancel()
	track, found, err := s.loadTrack(ctx, job.UserID, job.TrackID)
	if err != nil || !found {
		return err
	}
	proceed, err := s.reconcileForReacquire(ctx, track)
	if err != nil || !proceed {
		return err
	}
	ac := s.startAcquisition(ctx, job, track)
	defer s.observe(ac)
	defer CleanupTemp(ctx, s.workspace, ac)
	s.loadPriorRejections(ctx, job.TrackID, ac)
	if err := s.runAcquisition(ctx, ac); err != nil {
		return s.outcomes().settleExecute(ctx, err, ac)
	}
	return nil
}

func (s *AcquireTrackAudioService) reconcileForReacquire(ctx context.Context, track *domain.TrackSnapshot) (proceed bool, err error) {
	return reacquirePolicy{ledger: s.ledger, audioStore: s.audioStore}.reconcile(ctx, track)
}

func (s *AcquireTrackAudioService) RefuseQueued(ctx context.Context, job domain.AcquisitionJob) {
	s.outcomes().refuseQueued(logging.ContextWithSubjectUser(ctx, job.UserID.String()), job)
}
