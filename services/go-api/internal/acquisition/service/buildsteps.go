package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

func (s *AcquireTrackAudioService) buildSteps(userId shared.UserId, trackId domain.TrackId) Pipeline {
	return CoreSteps(s.sources, s.audioTagger, s.audioStore, s.audioProber, s.identifier,
		WithStoreAudioRefGuard(s.trackRepo, trackId),
		WithStoreOrphanQueue(s.orphans, userId)).
		withUpdateTrack(NewUpdateTrackStep(s.trackRepo, userId, trackId))
}

// CoreSteps assembles search→select→download→tag→store. The execution order is
// fixed by Pipeline's stage types, not by the field order here: a step placed in
// the wrong slot does not compile.
func CoreSteps(
	sources *SourceRegistry,
	tagger ports.AudioTagger,
	store ports.AudioWriter,
	prober ports.AudioProber,
	identifier ports.AudioIdentifier,
	storeOpts ...func(*StoreStep),
) Pipeline {
	return Pipeline{
		search:     NewSearchStep(sources),
		selectBest: NewSelectStep(),
		download:   NewDownloadStep(sources, WithDownloadProber(prober), WithDownloadIdentifier(identifier)),
		tag:        NewTagStep(tagger),
		store:      NewStoreStep(store, append([]func(*StoreStep){WithStoreProber(prober)}, storeOpts...)...),
	}
}
