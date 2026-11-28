package playback

import (
	"altune/go-api/internal/playback/internal/adapters/catalog"
	"altune/go-api/internal/playback/internal/adapters/handler"
	"altune/go-api/internal/playback/internal/adapters/persistence"
	playbacktelemetry "altune/go-api/internal/playback/internal/adapters/telemetry"
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/playback/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"errors"
	"fmt"
	"time"
)

type (
	TrackSource     = ports.TrackSource
	NowPlayingTrack = ports.NowPlayingTrack
	QueueSaveLimit  = handler.QueueSaveLimit
	Routes          = handler.Routes
)

var (
	ErrTrackSourceUnavailable  = domain.ErrTrackSourceUnavailable
	ErrNowPlayingTrackNotFound = domain.ErrNowPlayingTrackNotFound
)

var errDepsIncomplete = errors.New("playback: New needs a database, a metrics registry, a track source and positive lookup and erasure timeouts")

type database interface {
	persistence.QueueStateDB
	persistence.DeletedUserDataDB
}

type Deps struct {
	DB                      database
	Metrics                 *telemetry.Registry
	Tracks                  TrackSource
	NowPlayingLookupTimeout time.Duration
	ErasureScanTimeout      time.Duration
	QueueSaveLimit          QueueSaveLimit
}

func (d Deps) validate() error {
	if d.DB == nil || d.Metrics == nil || d.Tracks == nil || d.NowPlayingLookupTimeout <= 0 || d.ErasureScanTimeout <= 0 {
		return errDepsIncomplete
	}
	return nil
}

type Module struct {
	routes  handler.Routes
	sweeper *service.ErasureSweeper
}

func New(d Deps) (*Module, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	metrics := playbacktelemetry.NewPlaybackMetrics(d.Metrics)
	queueStates := persistence.NewPostgresQueueStateRepository(d.DB, metrics, clock.Now)

	guardedTracks, err := catalog.NewGuardedSource(d.Tracks, metrics, clock.Now, d.NowPlayingLookupTimeout)
	if err != nil {
		return nil, fmt.Errorf("guard the track source: %w", err)
	}
	nowPlaying, err := service.NewNowPlayingReader(guardedTracks)
	if err != nil {
		return nil, fmt.Errorf("build the now-playing reader: %w", err)
	}
	queues, err := service.NewQueueService(queueStates, nowPlaying, clock.Now)
	if err != nil {
		return nil, fmt.Errorf("build the queue service: %w", err)
	}
	queueHandler, err := handler.NewQueueHandler(queues, d.QueueSaveLimit, clock.Now, metrics)
	if err != nil {
		return nil, fmt.Errorf("build the queue handler: %w", err)
	}
	return &Module{
		routes: handler.NewRoutes(queueHandler),
		sweeper: service.NewErasureSweeper(
			persistence.NewPostgresDeletedIdentityRepository(d.DB), queues, metrics, queueStates, d.ErasureScanTimeout),
	}, nil
}

func (m *Module) Routes() Routes { return m.routes }

func (m *Module) EraseDeletedUserData(ctx context.Context) (int, error) {
	forgotten, err := m.sweeper.Execute(ctx)
	if err != nil {
		return forgotten, fmt.Errorf("erase deleted user data: %w", err)
	}
	return forgotten, nil
}

func ReadSnapshot(reg *telemetry.Registry) playbacktelemetry.Snapshot {
	return playbacktelemetry.ReadSnapshot(reg)
}
