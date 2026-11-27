package app

import (
	"altune/go-api/internal/app/bridges/playbackfromlibrary"
	"altune/go-api/internal/library"
	"altune/go-api/internal/playback"
	"fmt"
)

func (a *application) wirePlayback(trackRepo *library.PostgresTrackRepository) (*playback.Module, error) {
	module, err := playback.New(playback.Deps{
		DB:                      a.pool,
		Metrics:                 a.metrics,
		Tracks:                  playbackfromlibrary.NewNowPlayingTracks(trackRepo),
		NowPlayingLookupTimeout: a.cfg.PlaybackNowPlayingLookupTimeout,
		ErasureScanTimeout:      a.cfg.PlaybackErasureScanTimeout,
		QueueSaveLimit:          playback.QueueSaveLimit{Every: a.cfg.PlaybackQueueRateEvery, Burst: a.cfg.PlaybackQueueRateBurst},
	})
	if err != nil {
		return nil, fmt.Errorf("build the playback module: %w", err)
	}
	return module, nil
}
