package playback

import (
	ports "altune/go-api/internal/playback/internal"
	"altune/go-api/internal/playback/internal/adapters/handler"
	"altune/go-api/internal/playback/internal/adapters/persistence"
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type (
	TrackSource     = ports.TrackSource
	NowPlayingTrack = ports.NowPlayingTrack
	QueueSaveLimit  = handler.QueueSaveLimit
	MetricsSnapshot = handler.Snapshot
)

var (
	ErrTrackSourceUnavailable   = domain.ErrTrackSourceUnavailable
	ErrNowPlayingTrackNotFound  = domain.ErrNowPlayingTrackNotFound
	ErrIdentityStoreUnavailable = domain.ErrIdentityStoreUnavailable
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
	routes  http.Handler
	sweeper *service.ErasureSweeper
}

func New(d Deps) (*Module, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	metrics := handler.NewPlaybackMetrics(d.Metrics)
	queueStates := persistence.NewPostgresQueueStateRepository(d.DB, metrics, clock.Now)

	nowPlaying, err := service.NewNowPlayingReader(d.Tracks, metrics, clock.Now, d.NowPlayingLookupTimeout)
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
		routes: queueHandler.Routes(),
		sweeper: service.NewErasureSweeper(
			persistence.NewPostgresDeletedIdentityRepository(d.DB), queues, metrics, queueStates, d.ErasureScanTimeout),
	}, nil
}

func (m *Module) Routes() http.Handler { return m.routes }

func (m *Module) EraseDeletedUserData(ctx context.Context) (int, error) {
	forgotten, err := m.sweeper.Execute(ctx)
	if err != nil {
		return forgotten, fmt.Errorf("erase deleted user data: %w", err)
	}
	return forgotten, nil
}

func DefaultQueueSaveLimit() QueueSaveLimit { return handler.DefaultQueueSaveLimit() }

func ReadSnapshot(reg *telemetry.Registry) MetricsSnapshot { return handler.ReadSnapshot(reg) }
