package library_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func completeDeps(t *testing.T) library.Deps {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://library@127.0.0.1:1/library?connect_timeout=1")
	if err != nil {
		t.Fatalf("configure an unreachable pool: %v", err)
	}
	t.Cleanup(pool.Close)
	metrics := library.NewDBCallMetrics(telemetry.NewRegistry())
	return library.Deps{
		CatalogTracks:  library.NewPostgresCatalogTrackRepository(pool, metrics),
		Playlists:      library.NewPostgresPlaylistRepository(pool, metrics),
		OrphanedAudio:  library.NewPostgresOrphanedAudioRepository(pool, metrics),
		Events:         events.NoopPublisher(),
		Scheduler:      library.NoopAcquisitionScheduler(),
		AudioLinks:     library.NoAudioLinks{},
		AudioMetrics:   &library.InMemoryAudioStoreMetrics{},
		StreamRecovery: func() bool { return false },
	}
}

func TestNewBuildsEveryHandlerFromCompleteDeps(t *testing.T) {
	t.Parallel()

	module, err := library.New(completeDeps(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	built := map[string]bool{
		"track handler":     module.TrackHandler != nil,
		"library handler":   module.LibraryHandler != nil,
		"playlist handler":  module.PlaylistHandler != nil,
		"stream handler":    module.StreamHandler != nil,
		"audio url handler": module.AudioURLHandler != nil,
		"set track number":  module.SetTrackNumber != nil,
		"orphaned audio":    module.OrphanedAudio != nil,
	}
	for name, ok := range built {
		if !ok {
			t.Errorf("%s was not built", name)
		}
	}
}

func TestNewRefusesDepsMissingARequiredPart(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*library.Deps){
		"catalog tracks":  func(d *library.Deps) { d.CatalogTracks = nil },
		"playlists":       func(d *library.Deps) { d.Playlists = nil },
		"orphaned audio":  func(d *library.Deps) { d.OrphanedAudio = nil },
		"events":          func(d *library.Deps) { d.Events = nil },
		"scheduler":       func(d *library.Deps) { d.Scheduler = nil },
		"audio links":     func(d *library.Deps) { d.AudioLinks = nil },
		"audio metrics":   func(d *library.Deps) { d.AudioMetrics = nil },
		"stream recovery": func(d *library.Deps) { d.StreamRecovery = nil },
	}
	for name, strip := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			deps := completeDeps(t)
			strip(&deps)

			module, err := library.New(deps)

			if err == nil || module != nil {
				t.Fatalf("New accepted deps without %s: module=%v err=%v", name, module, err)
			}
		})
	}
}
