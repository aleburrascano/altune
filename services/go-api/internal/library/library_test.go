package library_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type noLinks struct{}

func (noLinks) SignsLinks() bool { return false }

func (noLinks) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("no links")
}

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
		AudioLinks:     noLinks{},
		AudioMetrics:   &librarytest.Metrics{},
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
		"set track number": module.SetTrackNumber != nil,
		"orphaned audio":   module.OrphanedAudio != nil,
	}
	for name, ok := range built {
		if !ok {
			t.Errorf("%s was not built", name)
		}
	}
}

func TestRoutesMountEveryLibraryRoute(t *testing.T) {
	t.Parallel()

	module, err := library.New(completeDeps(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	bounded, longLived := chi.NewRouter(), chi.NewRouter()
	module.Routes().Bounded(bounded)
	module.Routes().LongLived(longLived)

	mounted := func(r chi.Router, method, path string) bool {
		return r.Match(chi.NewRouteContext(), method, path)
	}
	for _, want := range []struct{ method, path string }{
		{http.MethodGet, "/tracks/"},
		{http.MethodPost, "/tracks/"},
		{http.MethodGet, "/tracks/abc/status"},
		{http.MethodPost, "/tracks/abc/audio/recover"},
		{http.MethodPost, "/audio-urls"},
		{http.MethodGet, "/library/albums"},
		{http.MethodGet, "/library/artists"},
		{http.MethodGet, "/playlists/"},
		{http.MethodPost, "/playlists/abc/tracks/batch"},
	} {
		if !mounted(bounded, want.method, want.path) {
			t.Errorf("%s %s is not mounted in the bounded group", want.method, want.path)
		}
	}
	if !mounted(longLived, http.MethodGet, "/tracks/abc/audio") {
		t.Error("GET /tracks/{trackId}/audio is not mounted in the long-lived group")
	}
	if mounted(bounded, http.MethodGet, "/tracks/abc/audio") {
		t.Error("the audio stream is mounted in the bounded group, where the request timeout would cut it off")
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
