package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWireCatalogSchedulerGating(t *testing.T) {
	tests := []struct {
		name          string
		ytMusic       bool
		wantScheduler bool
	}{
		{name: "no sources leaves acquisition unwired", ytMusic: false, wantScheduler: false},
		{name: "a source wires scheduler and retry handlers", ytMusic: true, wantScheduler: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{
				cfg: &config.Config{MusicDir: t.TempDir(), YtMusicEnabled: tt.ytMusic},
				sem: make(chan struct{}, 1),
			}

			got, err := a.wireCatalog(t.Context(), nil, &discovery.Discovery{})
			if err != nil {
				t.Fatalf("wireCatalog: %v", err)
			}

			assertCatalogHandlersBuilt(t, got)
			if (got.scheduler != nil) != tt.wantScheduler {
				t.Errorf("got.scheduler set = %v, want %v", got.scheduler != nil, tt.wantScheduler)
			}
			if (got.retryH != nil) != tt.wantScheduler {
				t.Errorf("retryH set = %v, want %v", got.retryH != nil, tt.wantScheduler)
			}
			if (got.reacquireH != nil) != tt.wantScheduler {
				t.Errorf("reacquireH set = %v, want %v", got.reacquireH != nil, tt.wantScheduler)
			}
		})
	}
}

func TestWireCatalogFailsWithoutAudioStore(t *testing.T) {
	a := &App{cfg: &config.Config{YtMusicEnabled: true}}

	wired, err := a.wireCatalog(t.Context(), nil, &discovery.Discovery{})
	if err == nil {
		t.Fatal("wireCatalog with no audio store config: want error, got nil")
	}
	if wired.scheduler != nil {
		t.Error("scheduler must not be wired when the audio store fails")
	}
}

func TestWireCatalogPrincipalDefault_AdmitsOneUserUpToGlobalDepth(t *testing.T) {
	const concurrency = 2
	a := &App{
		cfg: &config.Config{
			MusicDir:               t.TempDir(),
			YtMusicEnabled:         true,
			AcquisitionConcurrency: concurrency,
		},
		sem: make(chan struct{}, concurrency),
	}
	for i := 0; i < concurrency; i++ {
		a.sem <- struct{}{}
	}

	wired, err := a.wireCatalog(t.Context(), nil, &discovery.Discovery{})
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if wired.scheduler == nil {
		t.Fatal("scheduler must be wired with a source configured")
	}
	t.Cleanup(func() { wired.scheduler.Shutdown(context.Background()) })

	userA := shared.NewUserId(uuid.New())
	const pastOldGlobalDepth = concurrency*4 + 5
	for i := 0; i < pastOldGlobalDepth; i++ {
		if err := wired.scheduler.Schedule(context.Background(), userA, catalog.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d of %d for one user: err = %v, want nil (the queue has no depth limit)", i+1, pastOldGlobalDepth, err)
		}
	}
}

func TestWireCatalogDoesNotCapPerPrincipalQueue(t *testing.T) {
	const concurrency = 2
	const principalCap = concurrency
	a := &App{
		cfg: &config.Config{
			MusicDir:               t.TempDir(),
			YtMusicEnabled:         true,
			AcquisitionConcurrency: concurrency,
		},
		sem: make(chan struct{}, concurrency),
	}
	for i := 0; i < concurrency; i++ {
		a.sem <- struct{}{}
	}

	wired, err := a.wireCatalog(t.Context(), nil, &discovery.Discovery{})
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if wired.scheduler == nil {
		t.Fatal("scheduler must be wired with a source configured")
	}
	t.Cleanup(func() { wired.scheduler.Shutdown(context.Background()) })

	userA := shared.NewUserId(uuid.New())
	for i := 0; i < principalCap+2; i++ {
		if err := wired.scheduler.Schedule(context.Background(), userA, catalog.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d past userA's old share: err = %v, want nil (the cap no longer refuses)", i+1, err)
		}
	}

	if err := wired.scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), catalog.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule for second principal: err = %v, want nil", err)
	}
}

func assertCatalogHandlersBuilt(t *testing.T, w catalogWiring) {
	t.Helper()
	if w.trackRepo == nil || w.setTrackNumberSvc == nil {
		t.Error("trackRepo and setTrackNumberSvc must be wired")
	}
	if w.trackHandler == nil || w.libraryHandler == nil || w.playlistHandler == nil {
		t.Error("track, library and playlist handlers must be wired")
	}
	if w.streamHandler == nil || w.audioURLHandler == nil {
		t.Error("stream and audio URL handlers must be wired")
	}
}

type fixtureTrackRepo struct {
	track *catalog.Track
}

func (r *fixtureTrackRepo) GetByID(_ context.Context, _ catalog.TrackId, _ shared.UserId) (*catalog.Track, error) {
	return r.track, nil
}

func (r *fixtureTrackRepo) Update(_ context.Context, track *catalog.Track, _ int) error {
	r.track = track
	return nil
}

func (r *fixtureTrackRepo) AudioRefInUse(_ context.Context, _ string, _ catalog.TrackId) (bool, error) {
	return false, nil
}

//go:embed testdata/clip.mp3
var fixtureClip []byte

func fixtureTiers() *sourceTiers {
	return &sourceTiers{
		first:    []acquisition.AudioSource{acquisition.NewArtTrackSource(fixtureClip)},
		fallback: []acquisition.AudioSource{acquisition.NewUploadSource(fixtureClip)},
	}
}

func TestAcquisitionSources_InjectedFixturesReplaceEveryLiveSourceAndCanary(t *testing.T) {
	fixtures := fixtureTiers()
	a := &App{cfg: &config.Config{Env: "test", YtMusicEnabled: true, YtDLPEnabled: true}, fixtureSources: fixtures}

	sources, tools := a.acquisitionSources(t.Context())

	first, fallback := sourceNames(sources.first), sourceNames(sources.fallback)
	wantFirst, wantFallback := sourceNames(fixtures.first), sourceNames(fixtures.fallback)
	if len(first) != 1 || first[0] != wantFirst[0] || len(fallback) != 1 || fallback[0] != wantFallback[0] {
		t.Fatalf("tiers first=%v fallback=%v, want %v and %v", first, fallback, wantFirst, wantFallback)
	}
	if tools.YtDlp {
		t.Error("tools report yt-dlp available, want no live tool behind injected fixtures")
	}
	if a.sourceCanarySearcher() != nil || a.sourceCanaryYTMClient() != nil {
		t.Error("a source canary is wired, want none behind injected fixtures")
	}
}

func TestAcquisitionSources_WithoutInjectedFixturesWiresLiveSourcesAndCanaries(t *testing.T) {
	a := &App{cfg: &config.Config{Env: "test", YtDLPEnabled: true}}

	sources, _ := a.acquisitionSources(t.Context())

	if names := sourceNames(sources.all()); !containsSource(names, acquisition.YtDlpSourceName) {
		t.Fatalf("sources %v, want the live yt-dlp source", names)
	}
	if a.sourceCanarySearcher() == nil || a.sourceCanaryYTMClient() == nil {
		t.Error("a source canary is missing, want both wired for live sources")
	}
}

func TestAcquisitionFixture_FirstTierArtTrackIsTheStoredSource(t *testing.T) {
	musicDir := t.TempDir()
	track := acquireWithFixture(t, musicDir)
	assertReadyWithAudio(t, track, musicDir)
	if track.AudioSourceURL == nil || *track.AudioSourceURL != acquisition.ArtTrackURL {
		t.Fatalf("stored source = %v, want the resolved art track %q", track.AudioSourceURL, acquisition.ArtTrackURL)
	}
}

func TestAcquisitionFixture_SavedTrackReachesReadyWithFileInMusicDir(t *testing.T) {
	musicDir := t.TempDir()
	track := acquireWithFixture(t, musicDir)
	assertReadyWithAudio(t, track, musicDir)
}

func TestAcquisitionFixture_FetchedAudioPassesProbeAndDecodeGates(t *testing.T) {
	prober := acquisition.NewFfprobeProber(os.Getenv("FFMPEG_LOCATION"))
	if ffprobe, ffmpeg := prober.Available(); !ffprobe || !ffmpeg {
		t.Skip("ffprobe/ffmpeg not installed")
	}
	musicDir := t.TempDir()
	track := acquireWithFixture(t, musicDir, acquisition.WithAudioProber(prober))
	assertReadyWithAudio(t, track, musicDir)
}

func acquireWithFixture(t *testing.T, musicDir string, opts ...func(*acquisition.AcquireTrackAudioService)) *catalog.Track {
	t.Helper()
	sources, _ := (&App{cfg: &config.Config{}, fixtureSources: fixtureTiers()}).acquisitionSources(t.Context())
	userID := shared.NewUserId(uuid.New())
	track, err := catalog.NewTrack(userID, "Fixture Song", "Fixture Artist", "")
	if err != nil || track.SetDuration(212) != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := &fixtureTrackRepo{track: track}
	opts = append(opts, acquisition.WithAudioTagger(acquisition.NewId3Tagger()), acquisition.WithAcquireTempWorkspace(acquisition.NewTempWorkspace()))
	svc := acquisition.NewAcquireTrackAudioService(acquisition.NewTrackLedger(repo), acquisition.NewSourceRegistry(sources.first...).WithFallback(sources.fallback...), catalog.NewFilesystemAudioStore(musicDir), opts...)
	if err := svc.Execute(context.Background(), userID, track.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return repo.track
}

func assertReadyWithAudio(t *testing.T, track *catalog.Track, musicDir string) {
	t.Helper()
	if track.AcquisitionStatus != catalog.AcquisitionReady || track.AudioRef == nil {
		t.Fatalf("status = %v audioRef = %v, want ready with a ref", track.AcquisitionStatus, track.AudioRef)
	}
	stored, err := os.Stat(filepath.Join(musicDir, *track.AudioRef))
	if err != nil || stored.Size() == 0 {
		t.Fatalf("stored audio %q: size %v err %v", *track.AudioRef, stored, err)
	}
}

func TestAudioSourcesToggle(t *testing.T) {
	searcher := acquisition.NewYtDlpAudioSearcher("", "", "")

	tests := []struct {
		name        string
		ytMusic     bool
		ytDlp       bool
		wantYtMusic bool
		wantYtDlp   bool
	}{
		{name: "both enabled (preserves current behavior)", ytMusic: true, ytDlp: true, wantYtMusic: true, wantYtDlp: true},
		{name: "ytmusic disabled", ytMusic: false, ytDlp: true, wantYtMusic: false, wantYtDlp: true},
		{name: "ytdlp disabled", ytMusic: true, ytDlp: false, wantYtMusic: true, wantYtDlp: false},
		{name: "both disabled", ytMusic: false, ytDlp: false, wantYtMusic: false, wantYtDlp: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{cfg: &config.Config{YtMusicEnabled: tt.ytMusic, YtDLPEnabled: tt.ytDlp}}

			sources := a.audioSourcesFor(t.Context(), searcher)
			names := sourceNames(sources.all())

			if got := containsSource(names, acquisition.YtMusicSourceName); got != tt.wantYtMusic {
				t.Errorf("ytmusic present = %v, want %v (sources=%v)", got, tt.wantYtMusic, names)
			}
			if got := containsSource(names, acquisition.YtDlpSourceName); got != tt.wantYtDlp {
				t.Errorf("ytdlp present = %v, want %v (sources=%v)", got, tt.wantYtDlp, names)
			}
		})
	}
}

func TestAudioSources_YouTubeMusicSearchRunsFirstAndYtDlpIsTheFallback(t *testing.T) {
	a := &App{cfg: &config.Config{YtMusicEnabled: true, YtDLPEnabled: true}}
	tiers := a.audioSourcesFor(t.Context(), acquisition.NewYtDlpAudioSearcher("", "", ""))

	first, fallback := sourceNames(tiers.first), sourceNames(tiers.fallback)
	if !containsSource(first, acquisition.YtMusicSourceName) || !containsSource(first, acquisition.YtmSearchSourceName) {
		t.Errorf("first tier = %v, want the catalog id and the songs search", first)
	}
	if len(fallback) != 1 || fallback[0] != acquisition.YtDlpSourceName {
		t.Errorf("fallback = %v, want only ytdlp", fallback)
	}
}

func TestAudioSources_TheSongsSearchOutranksTheCatalogLinkForTheSameVideo(t *testing.T) {
	a := &App{cfg: &config.Config{YtMusicEnabled: true, YtDLPEnabled: true}}
	first := sourceNames(a.audioSourcesFor(t.Context(), acquisition.NewYtDlpAudioSearcher("", "", "")).first)

	if len(first) != 3 || first[0] != acquisition.CatalogPinSourceName || first[1] != acquisition.YtmSearchSourceName || first[2] != acquisition.YtMusicSourceName {
		t.Errorf("first tier = %v, want [%s %s %s]: duplicates keep the earlier source, and only the songs search carries the video's own length",
			first, acquisition.CatalogPinSourceName, acquisition.YtmSearchSourceName, acquisition.YtMusicSourceName)
	}
}

func sourceNames(sources []acquisition.AudioSource) []acquisition.SourceName {
	names := make([]acquisition.SourceName, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name())
	}
	return names
}

func containsSource(list []acquisition.SourceName, want acquisition.SourceName) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestBuildAudioStoreFailsFastWhenMisconfigured(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *config.Config
		wantContains []string
	}{
		{
			name:         "no backend configured",
			cfg:          &config.Config{},
			wantContains: []string{"MUSIC_DIR", "OCI_S3_ENDPOINT"},
		},
		{
			name: "incomplete OCI S3 names the missing variable",
			cfg: &config.Config{
				OCIS3Endpoint:  "https://objectstorage.example.com",
				OCIS3AccessKey: "key",
				OCIS3SecretKey: "secret",
			},
			wantContains: []string{"OCI_S3_BUCKET", "incomplete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := catalog.FromConfig(tt.cfg, slog.Default())
			if err == nil {
				t.Fatalf("expected startup error for misconfigured audio store, got store=%v", store)
			}
			if store != nil {
				t.Fatalf("expected nil store alongside error, got %v", store)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err.Error(), want)
				}
			}
		})
	}
}

func TestBuildAudioStoreFilesystemSucceeds(t *testing.T) {
	store, err := catalog.FromConfig(&config.Config{MusicDir: t.TempDir()}, slog.Default())
	if err != nil {
		t.Fatalf("filesystem audio store should resolve, got error: %v", err)
	}
	if store == nil {
		t.Fatal("expected a non-nil filesystem audio store")
	}
}

func TestBuildAudioStoreScopesWhenKeyPrefixSet(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.FromConfig(&config.Config{MusicDir: dir, AudioKeyPrefix: "staging/"}, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outsideRef := "prod-user/a/b/c.mp3"
	outsidePath := filepath.Join(dir, outsideRef)
	if err := os.MkdirAll(filepath.Dir(outsidePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(outsidePath, []byte("audio"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := store.Delete(context.Background(), outsideRef); err != nil {
		t.Fatalf("expected delete outside the prefix to no-op, got error: %v", err)
	}
	if _, err := os.Stat(outsidePath); err != nil {
		t.Fatalf("expected the file outside the prefix to survive, got: %v", err)
	}
}

func TestBuildAudioStoreUnscopedWhenKeyPrefixEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.FromConfig(&config.Config{MusicDir: dir}, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ref := "u/a/b/c.mp3"
	path := filepath.Join(dir, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := store.Delete(context.Background(), ref); err != nil {
		t.Fatalf("expected delete to succeed with no prefix configured, got error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected the file to be deleted when no prefix is configured, got: %v", err)
	}
}

func blackHoleDatabase(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var held []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held = append(held, c)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		for _, c := range held {
			_ = c.Close()
		}
	})
	return "postgres://altune:altune@" + ln.Addr().String() + "/altune?sslmode=disable"
}

func TestCatalogDBTimeout_ReachesOperatorLiveMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the 5s production DB-call deadline")
	}
	pool, err := pgxpool.New(context.Background(), blackHoleDatabase(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	a := &App{
		cfg:  &config.Config{MusicDir: t.TempDir()},
		sem:  make(chan struct{}, 1),
		pool: pool,
	}
	cat, err := a.wireCatalog(t.Context(), nil, &discovery.Discovery{})
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	operator := shared.NewUserId(uuid.New())
	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		if token == operatorToken {
			return auth.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return auth.VerifiedToken{}, errors.New("bad token")
	})
	r := a.mountRoutes(verifier, cat,
		playback.NewQueueHandler(nil),
		discovery.NewDiscoveryHandler(discovery.DiscoveryServices{}))
	mountObserveLiveMetrics(r, verifier, operator, liveMetricsSnapshot)

	before := catalog.ReadSnapshot().DBCallTimeouts
	start := time.Now()
	code, body := callObserveAs(t, r, http.MethodGet, "/v1/library/albums", operatorToken)
	if code < http.StatusInternalServerError {
		t.Fatalf("library read against a wedged DB: status %d, want 5xx; body %s", code, body)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("library read took %v, the DB-call deadline did not bound it", elapsed)
	}

	code, body = callObserveAs(t, r, http.MethodGet, "/observe/metrics/live", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("operator metrics read: status %d, want 200; body %s", code, body)
	}
	var got struct {
		Catalog catalog.MetricsSnapshot `json:"catalog"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Catalog.DBCallTimeouts != before+1 {
		t.Errorf("catalog.db_call_timeouts_total %d, want %d; body %s", got.Catalog.DBCallTimeouts, before+1, body)
	}
}

func newAppTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func countAcquisitionOutcomeRows(t *testing.T, pool *pgxpool.Pool, trackID string) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM acquisition_outcomes WHERE track_id = $1`, trackID).Scan(&count)
	if err != nil {
		t.Fatalf("count acquisition_outcomes: %v", err)
	}
	return count
}

func TestWireCatalogScheduler_RecordsOutcomeThroughThePostgresStore(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newAppTestPool(t)

	userID := shared.NewUserId(uuid.New())
	track, err := catalog.NewTrack(userID, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if _, _, err := catalog.NewPgxTrackRepository(pool).Add(context.Background(), track); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	a := &App{
		cfg:            &config.Config{Env: "test", MusicDir: t.TempDir()},
		sem:            make(chan struct{}, 1),
		pool:           pool,
		fixtureSources: fixtureTiers(),
	}
	tap := observe.NewEventTap(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")))
	wired, err := a.wireCatalog(t.Context(), tap, &discovery.Discovery{})
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if wired.scheduler == nil {
		t.Fatal("scheduler not wired")
	}
	t.Cleanup(func() { wired.scheduler.Shutdown(context.Background()) })

	if err := wired.scheduler.Schedule(context.Background(), userID, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if countAcquisitionOutcomeRows(t, pool, track.ID.String()) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no acquisition_outcomes row written for track %s within 5s (wiring must pass WithOutcomeRecorder with the Postgres store)", track.ID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type sourceWithNothing struct{}

func (sourceWithNothing) Name() acquisition.SourceName { return "nothing" }

func (sourceWithNothing) Find(context.Context, acquisition.FindRequest) ([]acquisition.AudioCandidate, error) {
	return nil, nil
}

func (sourceWithNothing) Fetch(context.Context, acquisition.AudioCandidate, string) (acquisition.FetchedAudio, error) {
	return acquisition.FetchedAudio{}, errors.New("nothing to fetch")
}

func insertReadyTrackGradedC(t *testing.T, pool *pgxpool.Pool) (shared.UserId, shared.TrackId) {
	t.Helper()
	userID := shared.NewUserId(uuid.New())
	track, err := catalog.NewTrack(userID, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if _, _, err := catalog.NewPgxTrackRepository(pool).Add(context.Background(), track); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	})
	if _, err := pool.Exec(context.Background(), `
UPDATE tracks SET acquisition_status = 'ready', audio_ref = 'stored.mp3',
	audio_source_url = 'https://www.youtube.com/watch?v=stored', acquisition_available_at = NULL,
	acquisition_evidence = '{"grade": "C", "in_place_generation": 2}'
WHERE id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("mark ready graded C: %v", err)
	}
	return userID, track.ID
}

type upgradeJobState struct {
	Kind         string
	Queued       bool
	Leased       bool
	Claims       int
	UpgradeTries int
}

func readUpgradeJobState(t *testing.T, pool *pgxpool.Pool, trackID shared.TrackId) upgradeJobState {
	t.Helper()
	var s upgradeJobState
	err := pool.QueryRow(context.Background(), `
SELECT acquisition_job_kind, acquisition_available_at IS NOT NULL, acquisition_lease_until IS NOT NULL,
	acquisition_run_attempts, COALESCE((acquisition_evidence->>'upgrade_tries')::int, 0)
FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&s.Kind, &s.Queued, &s.Leased, &s.Claims, &s.UpgradeTries)
	if err != nil {
		t.Fatalf("read upgrade job state: %v", err)
	}
	return s
}

func TestBuildAcquisitionScheduler_AnUpgradeWorkerClaimsAndSettlesAnEnqueuedUpgrade(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newAppTestPool(t)
	userID, trackID := insertReadyTrackGradedC(t, pool)

	a := &App{
		cfg:  &config.Config{Env: "test", MusicDir: t.TempDir(), AcquisitionUpgradeConcurrency: 1, AcquisitionUpgradeMaxTries: 3},
		sem:  make(chan struct{}, 1),
		pool: pool,
	}
	audioStore, err := catalog.FromConfig(a.cfg, slog.Default())
	if err != nil {
		t.Fatalf("audio store: %v", err)
	}
	ledger := acquisition.NewTrackLedger(catalog.NewPgxTrackRepository(pool))
	sources := sourceTiers{first: []acquisition.AudioSource{sourceWithNothing{}}}
	scheduler := a.buildAcquisitionScheduler(t.Context(), observe.NewEventTap(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))),
		&discovery.Discovery{}, ledger, audioStore, sources, acquisition.AcquisitionVerification{})
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	if err := scheduler.ScheduleUpgrade(context.Background(), userID, trackID); err != nil {
		t.Fatalf("schedule upgrade: %v", err)
	}

	want := upgradeJobState{Kind: "acquire", Queued: false, Leased: false, Claims: 1, UpgradeTries: 1}
	const settleWithin = 15 * time.Second
	deadline := time.Now().Add(settleWithin)
	for {
		got := readUpgradeJobState(t, pool, trackID)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("upgrade job of track %s after %v = %+v, want %+v (claimed once through the upgrade lane, an attempt noted, the job settled off the queue)", trackID, settleWithin, got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type scriptedIdentifier struct {
	acquisition.AudioIdentifier
	err error
}

func (s scriptedIdentifier) Identify(context.Context, string, float64) (acquisition.RecordingMatch, error) {
	return acquisition.RecordingMatch{}, s.err
}

func TestWireCatalogFingerprintVerifiedStaysFalseUntilALookupSucceeds(t *testing.T) {
	failing := &lookupTracker{AudioIdentifier: scriptedIdentifier{err: errors.New("invalid api key")}}
	working := &lookupTracker{AudioIdentifier: scriptedIdentifier{}}

	_, _ = failing.Identify(context.Background(), "a.opus", 0)
	_, _ = working.Identify(context.Background(), "a.opus", 0)

	if failing.Verified() {
		t.Error("Verified() = true after only failed lookups, want false")
	}
	if !working.Verified() {
		t.Error("Verified() = false after a successful lookup, want true")
	}
}

type sequencedIdentifier struct {
	acquisition.AudioIdentifier
	errs []error
	next int
}

func (s *sequencedIdentifier) Identify(context.Context, string, float64) (acquisition.RecordingMatch, error) {
	err := s.errs[s.next]
	s.next++
	return acquisition.RecordingMatch{}, err
}

func TestWireCatalogFingerprintVerifiedFollowsTheLatestLookup(t *testing.T) {
	throttled := fmt.Errorf("acoustid lookup: %w", acquisition.ErrIdentifyThrottled)
	tracker := &lookupTracker{AudioIdentifier: &sequencedIdentifier{errs: []error{
		nil, throttled, errors.New("invalid api key"), throttled, nil,
	}}}

	steps := []struct {
		name string
		want bool
	}{
		{"success", true},
		{"throttled after success", true},
		{"failure after success", false},
		{"throttled after failure", false},
		{"success after failure", true},
	}
	for _, step := range steps {
		_, _ = tracker.Identify(context.Background(), "a.opus", 0)
		if got := tracker.Verified(); got != step.want {
			t.Errorf("Verified() after %s = %v, want %v", step.name, got, step.want)
		}
	}
}

func TestWireCatalogRejectsPartialOCIS3EvenWithMusicDir(t *testing.T) {
	a := &App{cfg: &config.Config{
		YtMusicEnabled: true,
		MusicDir:       t.TempDir(),
		OCIS3Endpoint:  "https://objectstorage.example.com",
		OCIS3Bucket:    "audio",
	}}

	wired, err := a.wireCatalog(t.Context(), nil, &discovery.Discovery{})
	if err == nil {
		t.Fatal("wireCatalog with 2 of 4 OCI keys and MUSIC_DIR: want error, got nil")
	}
	for _, want := range []string{"OCI_S3_ACCESS_KEY", "OCI_S3_SECRET_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err.Error(), want)
		}
	}
	if wired.scheduler != nil {
		t.Error("scheduler must not be wired when the audio store fails")
	}
}

func TestBuildAcquisitionScheduler_AcquisitionPausedStartsPausedBeforeStartupSwitches(t *testing.T) {
	a := &App{
		cfg: &config.Config{MusicDir: t.TempDir(), YtMusicEnabled: true, AcquisitionPaused: true},
		sem: make(chan struct{}, 1),
	}

	got, err := a.wireCatalog(t.Context(), nil, &discovery.Discovery{})
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if got.scheduler == nil {
		t.Fatal("scheduler not wired")
	}
	t.Cleanup(func() { got.scheduler.Shutdown(context.Background()) })

	if !got.scheduler.Status().Paused {
		t.Fatal("Status().Paused = false straight out of wiring, want true so no worker claims before applyStartupSwitches")
	}
}

func TestCatalogWiring_StoredAudioReaderStreamsTheStoredFile(t *testing.T) {
	store := catalog.NewFilesystemAudioStore(t.TempDir())
	source := filepath.Join(t.TempDir(), "in.m4a")
	if err := os.WriteFile(source, []byte("stored audio bytes"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := store.Store(t.Context(), source, "audio/a.m4a"); err != nil {
		t.Fatalf("store: %v", err)
	}
	dir := t.TempDir()

	path, err := storedAudioReader{store: store}.CopyStored(t.Context(), "audio/a.m4a", dir)
	if err != nil {
		t.Fatalf("CopyStored: %v", err)
	}

	copied, err := os.Open(path)
	if err != nil {
		t.Fatalf("open copy: %v", err)
	}
	defer func() { _ = copied.Close() }()
	got, err := io.ReadAll(copied)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if string(got) != "stored audio bytes" || filepath.Dir(path) != dir || filepath.Ext(path) != ".m4a" {
		t.Errorf("copy = %q at %s, want the stored bytes inside %s keeping the .m4a extension", got, path, dir)
	}
}

func TestCatalogWiring_StoredAudioReaderReportsAMissingStoredFile(t *testing.T) {
	store := catalog.NewFilesystemAudioStore(t.TempDir())

	if _, err := (storedAudioReader{store: store}).CopyStored(t.Context(), "audio/missing.m4a", t.TempDir()); err == nil {
		t.Fatal("CopyStored of a ref that is not stored: want error, got nil")
	}
}

func TestYtDlpSearcherRecordsAJarsFirstBotCheckUnderTheConfiguredEnvironment(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte("#!/bin/sh\necho \"ERROR: [youtube] x: Sign in to confirm you're not a bot.\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: &config.Config{YtDLPCookieFile: jar, Env: "staging"}}

	_ = a.newYtDlpSearcher().Canary(context.Background(), acquisition.YouTubeCanary)

	raw, err := os.ReadFile(jar + ".session.json")
	if err != nil {
		t.Fatalf("session file next to the configured jar: %v", err)
	}
	var session struct {
		FirstSignInFailure *struct {
			Environment string `json:"environment"`
			Run         string `json:"run"`
		} `json:"first_signin_failure"`
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatalf("session file %q: %v", raw, err)
	}
	if f := session.FirstSignInFailure; f == nil || f.Environment != "staging" || f.Run != "canary" {
		t.Errorf("first sign-in failure = %+v, want the canary's, recorded under environment staging", f)
	}
}
