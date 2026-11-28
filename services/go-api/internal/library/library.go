package library

import (
	"altune/go-api/internal/library/internal/adapters/handler"
	"altune/go-api/internal/library/internal/adapters/persistence"
	"altune/go-api/internal/library/internal/adapters/storage"
	"altune/go-api/internal/library/internal/adapters/telemetry"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"errors"
	"fmt"
)

type (
	Track                           = domain.Track
	TrackId                         = domain.TrackId
	DownloadStatus                  = domain.DownloadStatus
	AcquisitionEvent                = domain.AcquisitionEvent
	CatalogSource                   = domain.CatalogSource
	OwnedTrackRef                   = domain.OwnedTrackRef
	ReadyAudioTrack                 = ports.ReadyAudioTrack
	AudioStore                      = ports.AudioStore
	AudioLinkSigner                 = ports.AudioLinkSigner
	AudioAgeLister                  = ports.AudioAgeLister
	AudioRefPrefixLister            = ports.AudioRefPrefixLister
	ObjectAge                       = ports.ObjectAge
	TrackReadWriter                 = ports.TrackReadWriter
	InPlaceVerdict                  = ports.InPlaceVerdict
	AcquisitionScheduler            = ports.AcquisitionScheduler
	DBCallMetrics                   = ports.DBCallMetrics
	OrphanedAudio                   = ports.OrphanedAudio
	OrphanedAudioQueue              = ports.OrphanedAudioQueue
	OrphanedAudioRecorder           = ports.OrphanedAudioRecorder
	FeaturedArtistCredit            = ports.FeaturedArtistCredit
	FeaturedArtistCreditSource      = ports.FeaturedArtistCreditSource
	FeaturedArtistResolver          = ports.FeaturedArtistResolver
	SetTrackNumberService           = service.SetTrackNumberService
	PostgresTrackRepository         = persistence.PostgresTrackRepository
	PostgresOrphanedAudioRepository = persistence.PostgresOrphanedAudioRepository
	Routes                          = handler.Routes
)

const (
	AcquisitionPending   = domain.AcquisitionPending
	AcquisitionReady     = domain.AcquisitionReady
	AcquisitionFailed    = domain.AcquisitionFailed
	EventFailPending     = domain.EventFailPending
	EventMarkFailed      = domain.EventMarkFailed
	EventMarkReady       = domain.EventMarkReady
	EventReplaceAudio    = domain.EventReplaceAudio
	EventRevertToPending = domain.EventRevertToPending
)

var (
	ErrTrackNotFound                   = domain.ErrTrackNotFound
	ErrIllegalAcquisitionTransition    = domain.ErrIllegalAcquisitionTransition
	ErrDBTransient                     = domain.ErrDBTransient
	ErrTrackVersionConflict            = domain.ErrTrackVersionConflict
	ErrTrackFenceSuperseded            = domain.ErrTrackFenceSuperseded
	AcquisitionStatuses                = domain.AcquisitionStatuses
	ClaimableStatuses                  = domain.ClaimableStatuses
	RetryableStatuses                  = domain.RetryableStatuses
	UpgradableStatuses                 = domain.UpgradableStatuses
	Transition                         = domain.Transition
	PlausibleYear                      = domain.PlausibleYear
	NoopAcquisitionScheduler           = ports.NoopAcquisitionScheduler
	NewFeaturedArtistResolver          = service.NewFeaturedArtistResolver
	NewReconcileOrphanedAudioService   = service.NewReconcileOrphanedAudioService
	NewPostgresTrackRepository         = persistence.NewPostgresTrackRepository
	NewPostgresOrphanedAudioRepository = persistence.NewPostgresOrphanedAudioRepository
	NewAudioStoreFromConfig            = storage.NewAudioStoreFromConfig
	NewAudioStoreMetrics               = telemetry.NewAudioStoreMetrics
	NewDBCallMetrics                   = telemetry.NewDBCallMetrics
	NewFSCallMetrics                   = telemetry.NewFSCallMetrics
	ReadSnapshot                       = telemetry.ReadSnapshot
)

var errDepsIncomplete = errors.New("library: New needs a database, database call metrics, an events publisher, an acquisition scheduler, audio links (a signer that reports SignsLinks false when the store cannot sign), audio metrics and a stream recovery switch")

type Deps struct {
	DB              persistence.Database
	DBMetrics       ports.DBCallMetrics
	Events          events.Publisher
	Scheduler       ports.AcquisitionScheduler
	AudioStore      ports.AudioStore
	AudioLinks      ports.AudioLinkSigner
	AudioMetrics    ports.AudioStoreMetrics
	Featured        ports.FeaturedArtistResolver
	StreamRecovery  func() bool
	PrefetchEnabled bool
}

func (d Deps) validate() error {
	if d.DB == nil || d.DBMetrics == nil || d.Events == nil || d.Scheduler == nil || d.AudioLinks == nil || d.AudioMetrics == nil || d.StreamRecovery == nil {
		return errDepsIncomplete
	}
	return nil
}

type Module struct {
	SetTrackNumber *service.SetTrackNumberService
	OrphanedAudio  *persistence.PostgresOrphanedAudioRepository
	routes         handler.Routes
}

func (m *Module) Routes() Routes { return m.routes }

func New(d Deps) (*Module, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	catalogTracks := persistence.NewPostgresCatalogTrackRepository(d.DB, d.DBMetrics)
	playlists := persistence.NewPostgresPlaylistRepository(d.DB, d.DBMetrics)
	orphanedAudio := persistence.NewPostgresOrphanedAudioRepository(d.DB, d.DBMetrics)

	addTrack, err := service.NewAddTrackService(catalogTracks, handler.NewWireEvents(d.Events), d.Scheduler, clock.Now, ids.Real)
	if err != nil {
		return nil, fmt.Errorf("build the add-track service: %w", err)
	}
	playlistLifecycle, err := service.NewPlaylistLifecycleService(playlists, d.Events, clock.Now, ids.Real)
	if err != nil {
		return nil, fmt.Errorf("build the playlist lifecycle service: %w", err)
	}
	playlistMembership, err := service.NewPlaylistMembershipService(playlists, catalogTracks, d.Events, clock.Now)
	if err != nil {
		return nil, fmt.Errorf("build the playlist membership service: %w", err)
	}

	setTrackNumber := service.NewSetTrackNumberService(catalogTracks)
	listTracks := service.NewListTracksService(catalogTracks)
	trackStatus := service.NewGetTrackStatusService(catalogTracks)
	deleteTrack := service.NewDeleteTrackService(catalogTracks, d.AudioStore, d.Events, d.AudioMetrics, orphanedAudio)
	backfillFeatured := service.MustNewBackfillFeaturedService(catalogTracks, catalogTracks, d.Featured, clock.Now)
	listFeaturing := service.NewListFeaturingService(catalogTracks)
	streamTrack := service.NewStreamTrackService(catalogTracks, d.AudioStore, d.Scheduler, d.AudioMetrics, d.StreamRecovery, clock.Now)
	audioLink := service.MustNewAudioURLService(catalogTracks, d.AudioLinks, clock.Now, d.AudioMetrics)

	featuredHandler := handler.NewFeaturedArtistHandler(backfillFeatured, listFeaturing)
	return &Module{
		SetTrackNumber: setTrackNumber,
		OrphanedAudio:  orphanedAudio,
		routes: handler.NewRoutes(
			handler.MustNewTrackHandler(addTrack, listTracks, trackStatus, deleteTrack, setTrackNumber, featuredHandler, clock.Now),
			handler.NewLibraryHandler(service.NewLibraryLensService(catalogTracks)),
			handler.MustNewPlaylistHandler(playlistLifecycle, playlistMembership, clock.Now),
			handler.MustNewStreamHandler(streamTrack, clock.Now),
			handler.MustNewAudioURLHandler(audioLink, clock.Now, handler.WithPrefetchEnabled(d.PrefetchEnabled)),
		),
	}, nil
}
