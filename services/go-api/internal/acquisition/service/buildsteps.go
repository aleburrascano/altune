package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

func (s *AcquireTrackAudioService) buildSteps(userId shared.UserId, trackId domain.TrackId) Pipeline {
	return CoreSteps(s.sources, s.audioTagger, s.audioStore, s.audioProber, s.identifier,
		[]func(*DownloadStep){
			WithStepDownloadLimiter(s.downloadLimiter),
			WithConfidenceFloor(s.confidenceFloor),
			WithStepVerifySkips(s.verifySkips),
		},
		WithStoreAudioRefGuard(s.trackRepo, trackId),
		WithStoreOrphanQueue(s.orphans, userId),
		WithStoreKeyPrefix(s.storeKeyPrefix)).
		withUpdateTrack(NewUpdateTrackStep(s.trackRepo, userId, trackId))
}

func CoreSteps(
	sources *SourceRegistry,
	tagger ports.AudioTagger,
	store ports.AudioWriter,
	prober ports.AudioProber,
	identifier ports.AudioIdentifier,
	downloadOpts []func(*DownloadStep),
	storeOpts ...func(*StoreStep),
) Pipeline {
	return Pipeline{
		search:     NewSearchStep(sources),
		selectBest: NewSelectStep(),
		download:   NewDownloadStep(sources, append([]func(*DownloadStep){WithDownloadProber(prober), WithDownloadIdentifier(identifier), WithVerifyWidth(2)}, downloadOpts...)...),
		tag:        NewTagStep(tagger),
		store:      NewStoreStep(store, append([]func(*StoreStep){WithStoreProber(prober)}, storeOpts...)...),
	}
}
