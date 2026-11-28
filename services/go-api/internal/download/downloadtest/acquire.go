package downloadtest

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
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
	RecordingProvider = ports.RecordingProvider
	RecordingLookup   = ports.RecordingLookup
	RerecordingFinder = ports.RerecordingFinder
	RecordingResolver = ports.RecordingResolver
)

const (
	ProviderYouTube    = ports.ProviderYouTube
	ProviderDeezer     = ports.ProviderDeezer
	ProviderSoundCloud = ports.ProviderSoundCloud
)

type RecordingPorts struct {
	Lookup       ports.RecordingLookup
	Rerecordings ports.RerecordingFinder
	Resolver     ports.RecordingResolver
}

func AcquireWithNoSource(ctx context.Context, ledger *memory.Ledger, recording RecordingPorts, userID ids.UserId, trackID ids.TrackId) error {
	svc := service.MustNewAcquireTrackAudioService(ledger, service.NewSourceRegistry(), memory.NewAudioStore(), scratchfiles.New(),
		service.VerificationPorts{
			Identifier:      ports.NoopAudioIdentifier(),
			RecordingLookup: recording.Lookup,
			Rerecordings:    recording.Rerecordings,
			Previews:        ports.NoopPreviewMatcher(),
			Recordings:      recording.Resolver,
		}, service.StagePorts{
			Prober:        SilentAudioProber(),
			Tagger:        ports.NoopAudioTagger(),
			Rejections:    ports.NoopRejectionStore(),
			StoredAudio:   ports.NoopStoredAudioReader(),
			Orphans:       service.MustDirectDeleteOrphans(memory.NewAudioStore()),
			Skips:         ports.NoopVerifySkipRecorder(),
			Events:        events.NoopPublisher(),
			DownloadLimit: service.Unlimited(),
			Observer:      service.NoObserver(),
			Sleep:         clock.Sleep,
			Now:           time.Now,
			Unit:          random.Real,
		})
	if err := svc.Execute(ctx, ports.AcquisitionJob{UserID: userID, TrackID: trackID}); err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}
