package downloadtest

import (
	"altune/go-api/internal/download/internal/adapters/evidence"
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/random"
	"context"
	"fmt"
	"time"
)

type (
	RecordingProvider = domain.RecordingProvider
	RecordingLookup   = ports.RecordingLookup
	RerecordingFinder = ports.RerecordingFinder
	RecordingResolver = ports.RecordingResolver
)

const (
	ProviderYouTube    = domain.ProviderYouTube
	ProviderDeezer     = domain.ProviderDeezer
	ProviderSoundCloud = domain.ProviderSoundCloud
)

type RecordingPorts struct {
	Lookup       ports.RecordingLookup
	Rerecordings ports.RerecordingFinder
	Resolver     ports.RecordingResolver
}

func AcquireWithNoSource(ctx context.Context, ledger *memory.Ledger, recording RecordingPorts, userID ids.UserId, trackID ids.TrackId) error {
	svc := service.MustNewAcquireTrackAudioService(ledger, service.NewSourceRegistry(), memory.NewAudioStore(), scratchfiles.New(),
		service.VerificationPorts{
			Identifier:      memory.NoopAudioIdentifier(),
			RecordingLookup: recording.Lookup,
			Rerecordings:    recording.Rerecordings,
			Previews:        memory.NoopPreviewMatcher(),
			Recordings:      recording.Resolver,
		}, service.StagePorts{
			Prober:        SilentAudioProber(),
			Tagger:        memory.NoopAudioTagger(),
			Rejections:    memory.NoopRejectionStore(),
			StoredAudio:   memory.NoopStoredAudioReader(),
			Orphans:       service.MustDirectDeleteOrphans(memory.NewAudioStore()),
			Skips:         memory.NoopVerifySkipRecorder(),
			Evidence:      evidence.NewCodec(),
			Events:        events.NoopPublisher(),
			DownloadLimit: service.Unlimited(),
			Observer:      service.NoObserver(),
			Sleep:         clock.Sleep,
			Now:           time.Now,
			Unit:          random.Real,
		})
	if err := svc.Execute(ctx, domain.AcquisitionJob{UserID: userID, TrackID: trackID}); err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}
