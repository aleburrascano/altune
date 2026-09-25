package app

import (
	"altune/go-api/internal/acquisition/adapters/fixture"
	"altune/go-api/internal/acquisition/adapters/id3"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"
	adminHandler "altune/go-api/internal/admin/handler"
	"altune/go-api/internal/auth"
	catalogMetrics "altune/go-api/internal/catalog/adapters/metrics"
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/catalog/domain"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"context"
	"encoding/json"
	"errors"
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

// TestWireCatalogSchedulerGating pins wireCatalog's observable object graph so
// splitting it into named wiring steps stays behavior-preserving: with no
// acquisition source the scheduler and the retry/reacquire handlers must stay
// nil, and with a source they must all be built and the scheduler recorded on
// the App for shutdown.
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

			got, err := a.wireCatalog(nil, nil, nil)
			if err != nil {
				t.Fatalf("wireCatalog: %v", err)
			}

			assertCatalogHandlersBuilt(t, got)
			if (a.scheduler != nil) != tt.wantScheduler {
				t.Errorf("a.scheduler set = %v, want %v", a.scheduler != nil, tt.wantScheduler)
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

	if _, err := a.wireCatalog(nil, nil, nil); err == nil {
		t.Fatal("wireCatalog with no audio store config: want error, got nil")
	}
	if a.scheduler != nil {
		t.Error("scheduler must not be wired when the audio store fails")
	}
}

// TestWireCatalogPrincipalDefault_AdmitsOneUserUpToGlobalDepth proves #2789:
// the production wiring's default (ACQUISITION_PRINCIPAL_QUEUE_DEPTH unset,
// so AcquisitionPrincipalQueueDepth is the zero value) leaves the per-principal
// cap disabled, so one self-hosted user's saves past worker concurrency are
// admitted up to the shared global queue depth instead of being refused with
// ErrPrincipalQueueFull. It builds the scheduler through the real wireCatalog
// path with a saturated worker semaphore, so every admitted job parks on the
// sem instead of running the real acquire pipeline.
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
	// Saturate the workers so every admitted job blocks on the semaphore and
	// keeps holding its slot for the duration of the test.
	for i := 0; i < concurrency; i++ {
		a.sem <- struct{}{}
	}

	if _, err := a.wireCatalog(nil, nil, nil); err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if a.scheduler == nil {
		t.Fatal("scheduler must be wired with a source configured")
	}
	t.Cleanup(func() { a.scheduler.Shutdown(context.Background()) })

	userA := shared.NewUserId(uuid.New())
	const globalDepth = concurrency * 4 // acqService.defaultQueueDepthFactor
	for i := 0; i < globalDepth; i++ {
		if err := a.scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d of %d for one user: err = %v, want nil (past concurrency %d, within global depth)", i+1, globalDepth, err, concurrency)
		}
	}

	if err := a.scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), ""); !errors.Is(err, acqService.ErrAcquisitionQueueFull) {
		t.Fatalf("schedule past global depth: err = %v, want ErrAcquisitionQueueFull", err)
	}
}

// TestWireCatalogEnforcesPrincipalQueueCap proves #1418: setting
// ACQUISITION_PRINCIPAL_QUEUE_DEPTH (a non-zero AcquisitionPrincipalQueueDepth)
// turns the per-principal fair-share gate on, so one user past its explicit
// share is rejected while global admission slots remain for other users. It
// builds the scheduler through the real wireCatalog path with a saturated
// worker semaphore, so every admitted job parks on the sem (holding its
// principal slot) instead of running the real acquire pipeline.
func TestWireCatalogEnforcesPrincipalQueueCap(t *testing.T) {
	const concurrency = 2
	const principalCap = concurrency // explicit opt-in via ACQUISITION_PRINCIPAL_QUEUE_DEPTH
	a := &App{
		cfg: &config.Config{
			MusicDir:                       t.TempDir(),
			YtMusicEnabled:                 true,
			AcquisitionConcurrency:         concurrency,
			AcquisitionPrincipalQueueDepth: principalCap,
		},
		sem: make(chan struct{}, concurrency),
	}
	// Saturate the workers so every admitted job blocks on the semaphore and
	// keeps holding its per-principal slot for the duration of the test.
	for i := 0; i < concurrency; i++ {
		a.sem <- struct{}{}
	}

	if _, err := a.wireCatalog(nil, nil, nil); err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if a.scheduler == nil {
		t.Fatal("scheduler must be wired with a source configured")
	}
	t.Cleanup(func() { a.scheduler.Shutdown(context.Background()) })

	userA := shared.NewUserId(uuid.New())
	// Fill userA's explicit share, then push two arrivals past it.
	for i := 0; i < principalCap; i++ {
		if err := a.scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule within userA share: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		err := a.scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), "")
		if !errors.Is(err, acqService.ErrPrincipalQueueFull) {
			t.Fatalf("schedule past userA share: err = %v, want ErrPrincipalQueueFull", err)
		}
	}

	// Global slots remain: a different principal is still admitted.
	if err := a.scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule for second principal while global slots remain: err = %v, want nil", err)
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
	track *domain.Track
}

func (r *fixtureTrackRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	return r.track, nil
}

func (r *fixtureTrackRepo) Update(_ context.Context, track *domain.Track, _ int) error {
	r.track = track
	return nil
}

func (r *fixtureTrackRepo) AudioRefInUse(_ context.Context, _ string, _ domain.TrackId) (bool, error) {
	return false, nil
}

func TestAcquisitionSources_FixtureNeverWiredOutsideOptedInNonProd(t *testing.T) {
	cases := []config.Config{
		{Env: "production", AcquisitionFixtureOptIn: true, YtDLPEnabled: true},
		{Env: "staging", AcquisitionFixtureOptIn: true, YtDLPEnabled: true},
		{Env: "", AcquisitionFixtureOptIn: true, YtDLPEnabled: true},
		{Env: "test", AcquisitionFixtureOptIn: false, YtDLPEnabled: true},
	}
	for _, c := range cases {
		sources, _ := (&App{cfg: &c}).acquisitionSources()
		names := sourceNames(sources)
		if containsSource(names, fixture.SourceName) || !containsSource(names, ytdlp.SourceName) {
			t.Fatalf("cfg env=%q optIn=%v: sources %v, want live sources and no fixture", c.Env, c.AcquisitionFixtureOptIn, names)
		}
	}
}

func TestAcquisitionSources_OptedInTestEnvUsesOnlyFixture(t *testing.T) {
	cfg := &config.Config{Env: "test", AcquisitionFixtureOptIn: true, YtMusicEnabled: true, YtDLPEnabled: true, StreamripServices: []string{"qobuz"}}
	sources, _ := (&App{cfg: cfg}).acquisitionSources()
	if names := sourceNames(sources); len(names) != 1 || names[0] != fixture.SourceName {
		t.Fatalf("sources = %v, want only %q", names, fixture.SourceName)
	}
}

func TestAcquisitionFixture_SavedTrackReachesReadyWithFileInMusicDir(t *testing.T) {
	musicDir := t.TempDir()
	track := acquireWithFixture(t, musicDir)
	assertReadyWithAudio(t, track, musicDir)
}

func TestAcquisitionFixture_FetchedAudioPassesProbeAndDecodeGates(t *testing.T) {
	prober := ytdlp.NewFfprobeProber(os.Getenv("FFMPEG_LOCATION"))
	if ffprobe, ffmpeg := prober.Available(); !ffprobe || !ffmpeg {
		t.Skip("ffprobe/ffmpeg not installed")
	}
	musicDir := t.TempDir()
	track := acquireWithFixture(t, musicDir, acqService.WithAudioProber(prober))
	assertReadyWithAudio(t, track, musicDir)
}

func acquireWithFixture(t *testing.T, musicDir string, opts ...func(*acqService.AcquireTrackAudioService)) *domain.Track {
	t.Helper()
	sources, _ := (&App{cfg: &config.Config{Env: "test", AcquisitionFixtureOptIn: true}}).acquisitionSources()
	userID := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userID, "Fixture Song", "Fixture Artist", "")
	if err != nil || track.SetDuration(212) != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := &fixtureTrackRepo{track: track}
	opts = append(opts, acqService.WithAudioTagger(id3.NewTagger()))
	svc := acqService.NewAcquireTrackAudioService(repo, acqService.NewSourceRegistry(sources...), storage.NewFilesystemAudioStore(musicDir), opts...)
	if err := svc.Execute(context.Background(), userID, track.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return repo.track
}

func assertReadyWithAudio(t *testing.T, track *domain.Track, musicDir string) {
	t.Helper()
	if track.AcquisitionStatus != domain.AcquisitionReady || track.AudioRef == nil {
		t.Fatalf("status = %v audioRef = %v, want ready with a ref", track.AcquisitionStatus, track.AudioRef)
	}
	stored, err := os.Stat(filepath.Join(musicDir, *track.AudioRef))
	if err != nil || stored.Size() == 0 {
		t.Fatalf("stored audio %q: size %v err %v", *track.AudioRef, stored, err)
	}
}

// TestAudioSourcesToggle reproduces the defect where the yt-dlp and ytmusic
// sources were wired unconditionally: before this change no config flag could
// skip either one, so disabling a single misbehaving provider meant a code
// change or taking down the whole audio store. Each source must now be gated
// independently by YTMUSIC_ENABLED / YTDLP_ENABLED, mirroring streamrip.
func TestAudioSourcesToggle(t *testing.T) {
	searcher := ytdlp.NewYtDlpAudioSearcher("", "", "")

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

			sources, _ := a.audioSourcesFor(searcher)
			names := sourceNames(sources)

			if got := containsSource(names, ytmusic.SourceName); got != tt.wantYtMusic {
				t.Errorf("ytmusic present = %v, want %v (sources=%v)", got, tt.wantYtMusic, names)
			}
			if got := containsSource(names, ytdlp.SourceName); got != tt.wantYtDlp {
				t.Errorf("ytdlp present = %v, want %v (sources=%v)", got, tt.wantYtDlp, names)
			}
		})
	}
}

func sourceNames(sources []acqPorts.AudioSource) []string {
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name())
	}
	return names
}

func containsSource(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestBuildAudioStoreFailsFastWhenMisconfigured reproduces the defect where a
// misconfigured audio store booted healthy and panicked on the first stream or
// delete with a nil store. Startup must instead fail fast, naming the missing
// configuration, and never hand a nil store to the catalog services.
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
				// OCI_S3_BUCKET deliberately missing.
			},
			wantContains: []string{"OCI_S3_BUCKET", "incomplete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{cfg: tt.cfg}

			store, err := a.buildAudioStore()
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

// TestBuildAudioStoreFilesystemSucceeds guards that the supported filesystem
// backend still resolves to a live store without error.
func TestBuildAudioStoreFilesystemSucceeds(t *testing.T) {
	a := &App{cfg: &config.Config{MusicDir: t.TempDir()}}

	store, err := a.buildAudioStore()
	if err != nil {
		t.Fatalf("filesystem audio store should resolve, got error: %v", err)
	}
	if store == nil {
		t.Fatal("expected a non-nil filesystem audio store")
	}
}

// TestWireCatalogProbesStreamripBinary reproduces the defect where the
// streamrip rip binary was stored without any startup probe: a missing binary
// never showed up in AcquisitionVerification and only failed deep inside a
// background Fetch. The wired scheduler must now report the probe result.
func TestWireCatalogProbesStreamripBinary(t *testing.T) {
	present := filepath.Join(t.TempDir(), "rip")
	if err := os.WriteFile(present, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake rip: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "absent", "rip")

	tests := []struct {
		name     string
		services []string
		bin      string
		want     bool
	}{
		{name: "configured binary missing is degraded", services: []string{"tidal"}, bin: missing, want: false},
		{name: "configured binary present is armed", services: []string{"tidal"}, bin: present, want: true},
		{name: "no streamrip service is not degraded", services: nil, bin: missing, want: true},
		{name: "only unsupported services is not degraded", services: []string{"napster"}, bin: missing, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{
				cfg: &config.Config{
					MusicDir:          t.TempDir(),
					YtMusicEnabled:    true,
					StreamripServices: tt.services,
					StreamripBin:      tt.bin,
				},
				sem: make(chan struct{}, 1),
			}

			if _, err := a.wireCatalog(nil, nil, nil); err != nil {
				t.Fatalf("wireCatalog: %v", err)
			}
			if a.scheduler == nil {
				t.Fatal("scheduler not wired")
			}
			if got := a.scheduler.Status().Verification.Streamrip; got != tt.want {
				t.Errorf("Verification.Streamrip = %v, want %v", got, tt.want)
			}
		})
	}
}

// blackHoleDatabase accepts TCP connections and never answers, standing in for
// a wedged Postgres: the pgx handshake blocks until the caller's deadline.
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

// A catalog request against a wedged database is cut off by the persistence
// per-call deadline, and the operator reads that timeout back from the catalog
// section of GET /admin/metrics/live on the production router.
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
	cat, err := a.wireCatalog(nil, nil, nil)
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
		playbackHandler.NewQueueHandler(nil),
		discoveryHandler.NewDiscoveryHandler(discoveryHandler.DiscoveryServices{}), nil)
	mountAdmin(r, verifier, adminPrincipals{operator: operator.String()}, adminHandler.New(nil, nil).WithLiveMetrics(liveMetricsSnapshot))

	before := catalogMetrics.ReadSnapshot().DBCallTimeouts
	start := time.Now()
	code, body := callAdmin(t, r, http.MethodGet, "/v1/library/albums", operatorToken)
	if code < http.StatusInternalServerError {
		t.Fatalf("library read against a wedged DB: status %d, want 5xx; body %s", code, body)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("library read took %v, the DB-call deadline did not bound it", elapsed)
	}

	code, body = callAdmin(t, r, http.MethodGet, "/admin/metrics/live", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("operator metrics read: status %d, want 200; body %s", code, body)
	}
	var got struct {
		Catalog catalogMetrics.Snapshot `json:"catalog"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Catalog.DBCallTimeouts != before+1 {
		t.Errorf("catalog.db_call_timeouts_total %d, want %d; body %s", got.Catalog.DBCallTimeouts, before+1, body)
	}
}
