package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

func (s *AcquireTrackAudioService) buildSteps(userId shared.UserId, trackId domain.TrackId) []Step {
	return append(
		CoreSteps(s.sources, s.audioTagger, s.audioStore, s.audioProber, s.identifier),
		NewUpdateTrackStep(s.trackRepo, userId, trackId),
	)
}

func CoreSteps(
	sources *SourceRegistry,
	tagger ports.AudioTagger,
	store ports.AudioWriter,
	prober ports.AudioProber,
	identifier ports.AudioIdentifier,
) []Step {
	return []Step{
		NewSearchStep(sources),
		NewSelectStep(),
		NewDownloadStep(sources, WithDownloadProber(prober), WithDownloadIdentifier(identifier)),
		NewTagStep(tagger),
		NewStoreStep(store, WithStoreProber(prober)),
	}
}
