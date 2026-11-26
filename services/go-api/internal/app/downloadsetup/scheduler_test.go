package downloadsetup_test

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/app/downloadsetup"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/testdb"
	"context"
	_ "embed"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/clip.mp3
var fixtureClip []byte

func fixtureTiers() downloadsetup.SourceOrder {
	return downloadsetup.SourceOrder{
		First:    []download.AudioSource{download.NewArtTrackSource(fixtureClip)},
		Fallback: []download.AudioSource{download.NewUploadSource(fixtureClip)},
	}
}

type fixtureTrackRepo struct {
	mu    sync.Mutex
	track *library.Track
}

func (r *fixtureTrackRepo) GetByID(_ context.Context, _ library.TrackId, _ ids.UserId) (*library.Track, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.track == nil {
		return nil, library.ErrTrackNotFound
	}
	return r.track, nil
}

func (r *fixtureTrackRepo) Update(_ context.Context, track *library.Track, _ int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.track = track
	return nil
}

func (r *fixtureTrackRepo) AudioRefInUse(_ context.Context, _ string, _ library.TrackId) (bool, error) {
	return false, nil
}

func eventTap(t *testing.T) *monitoring.EventTap {
	t.Helper()
	return monitoring.MustNewEventTap(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")), time.Now)
}

func newOfflineScheduler(t *testing.T, cfg *config.Config, sources downloadsetup.SourceOrder) *download.BackgroundAcquisitionScheduler {
	t.Helper()
	scheduler := downloadsetup.NewScheduler(t.Context(), downloadsetup.Deps{
		Transport:  http.DefaultTransport,
		Config:     cfg,
		Events:     eventTap(t),
		Ledger:     downloadfromlibrary.NewLibraryTracks(&fixtureTrackRepo{}),
		AudioStore: library.NewFilesystemAudioStore(cfg.MusicDir),
		Recording:  downloadfromsearch.NoRecording(),
		Sources:    sources,
	})
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })
	return scheduler
}

func acquireThroughPostgres(t *testing.T) (library.Track, string, download.Verification) {
	t.Helper()
	pool := integrationPool(t)
	musicDir := t.TempDir()
	userID := ids.NewUserId(uuid.New())
	track, err := library.NewTrack(userID, "Fixture Song", "Fixture Artist", "")
	if err != nil || track.SetDuration(212) != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := library.NewPostgresTrackRepository(pool)
	if _, _, err := repo.Add(context.Background(), track); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID()) })
	scheduler := downloadsetup.NewScheduler(t.Context(), downloadsetup.Deps{
		Transport:  http.DefaultTransport,
		Config:     &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1, MusicDir: musicDir},
		Pool:       pool,
		Events:     eventTap(t),
		Ledger:     downloadfromlibrary.NewLibraryTracks(repo),
		AudioStore: library.NewFilesystemAudioStore(musicDir),
		Recording:  downloadfromsearch.NoRecording(),
		Sources:    fixtureTiers(),
	})
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	if err := scheduler.Schedule(context.Background(), userID, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	return awaitReady(t, repo, track, userID), musicDir, scheduler.Status().Verification
}

func awaitReady(t *testing.T, repo *library.PostgresTrackRepository, track *library.Track, userID ids.UserId) library.Track {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := repo.GetByID(context.Background(), track.ID, userID)
		if err != nil {
			t.Fatalf("read track: %v", err)
		}
		if got.DownloadStatus == library.AcquisitionReady || time.Now().After(deadline) {
			return *got
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertReadyWithAudio(t *testing.T, track library.Track, musicDir string) {
	t.Helper()
	if track.DownloadStatus != library.AcquisitionReady || track.AudioRef == nil {
		t.Fatalf("status = %v audioRef = %v, want ready with a ref", track.DownloadStatus, track.AudioRef)
	}
	stored, err := os.Stat(filepath.Join(musicDir, *track.AudioRef))
	if err != nil || stored.Size() == 0 {
		t.Fatalf("stored audio %q: size %v err %v", *track.AudioRef, stored, err)
	}
}

func TestNewScheduler_RefusesAMissingTransport(t *testing.T) {
	defer func() {
		got, _ := recover().(error)
		if !errors.Is(got, downloadsetup.ErrTransportRequired) {
			t.Fatalf("panic = %v, want ErrTransportRequired", got)
		}
	}()

	downloadsetup.NewScheduler(t.Context(), downloadsetup.Deps{Config: &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1}})

	t.Fatal("NewScheduler built a scheduler without a transport")
}

func TestNewScheduler_TheFirstTierArtTrackIsTheStoredSource(t *testing.T) {
	track, musicDir, _ := acquireThroughPostgres(t)

	assertReadyWithAudio(t, track, musicDir)
	if track.AudioSourceURL == nil || *track.AudioSourceURL != download.ArtTrackURL {
		t.Fatalf("stored source = %v, want the resolved art track %q", track.AudioSourceURL, download.ArtTrackURL)
	}
}

func TestNewScheduler_FetchedAudioPassesTheProbeAndDecodeGates(t *testing.T) {
	if ffprobe, ffmpeg := download.NewFfprobeProber(os.Getenv("FFMPEG_LOCATION")).Available(); !ffprobe || !ffmpeg {
		t.Skip("ffprobe/ffmpeg not installed")
	}

	track, musicDir, verification := acquireThroughPostgres(t)

	if !verification.Ffprobe || !verification.Ffmpeg {
		t.Errorf("verification = %+v, want the composed prober to report ffprobe and ffmpeg", verification)
	}
	assertReadyWithAudio(t, track, musicDir)
}

func TestNewScheduler_OneUserMayQueuePastTheOldGlobalDepth(t *testing.T) {
	const concurrency = 2
	s := newOfflineScheduler(t, &config.Config{AcquisitionDownloadConcurrency: 1, MusicDir: t.TempDir(), AcquisitionConcurrency: concurrency}, fixtureTiers())

	userA := ids.NewUserId(uuid.New())
	const pastOldGlobalDepth = concurrency*4 + 5
	for i := 0; i < pastOldGlobalDepth; i++ {
		if err := s.Schedule(context.Background(), userA, library.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d of %d for one user: err = %v, want nil (the queue has no depth limit)", i+1, pastOldGlobalDepth, err)
		}
	}
}

func TestNewScheduler_DoesNotCapAPrincipalsQueue(t *testing.T) {
	const concurrency = 2
	const principalCap = concurrency
	s := newOfflineScheduler(t, &config.Config{AcquisitionDownloadConcurrency: 1, MusicDir: t.TempDir(), AcquisitionConcurrency: concurrency}, fixtureTiers())

	userA := ids.NewUserId(uuid.New())
	for i := 0; i < principalCap+2; i++ {
		if err := s.Schedule(context.Background(), userA, library.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d past userA's old share: err = %v, want nil (the cap no longer refuses)", i+1, err)
		}
	}
	if err := s.Schedule(context.Background(), ids.NewUserId(uuid.New()), library.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule for second principal: err = %v, want nil", err)
	}
}

func TestNewScheduler_AcquisitionPausedStartsPaused(t *testing.T) {
	s := newOfflineScheduler(t, &config.Config{AcquisitionDownloadConcurrency: 1, MusicDir: t.TempDir(), AcquisitionPaused: true}, fixtureTiers())

	if !s.Status().Paused {
		t.Fatal("Status().Paused = false straight out of NewScheduler, want true so no worker claims before the startup switches apply")
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testdb.Pool(t)
	testdb.ExclusiveAcquisitionQueue(t, pool)
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

func TestNewScheduler_RecordsTheOutcomeThroughThePostgresStore(t *testing.T) {
	pool := integrationPool(t)

	userID := ids.NewUserId(uuid.New())
	track, err := library.NewTrack(userID, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if _, _, err := library.NewPostgresTrackRepository(pool).Add(context.Background(), track); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	trackRepo := library.NewPostgresTrackRepository(pool)
	scheduler := downloadsetup.NewScheduler(t.Context(), downloadsetup.Deps{
		Transport:  http.DefaultTransport,
		Config:     &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1, MusicDir: t.TempDir()},
		Pool:       pool,
		Events:     eventTap(t),
		Ledger:     downloadfromlibrary.NewLibraryTracks(trackRepo),
		AudioStore: library.NewFilesystemAudioStore(t.TempDir()),
		Recording:  downloadfromsearch.NoRecording(),
		Sources:    fixtureTiers(),
	})
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	if err := scheduler.Schedule(context.Background(), userID, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if countAcquisitionOutcomeRows(t, pool, track.ID.String()) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no acquisition_outcomes row written for track %s within 5s (wiring must set SchedulerPorts.Outcomes to the Postgres store)", track.ID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type sourceWithNothing struct{}

func (sourceWithNothing) Name() download.SourceName { return "nothing" }

func (sourceWithNothing) Find(context.Context, download.FindRequest) ([]download.AudioCandidate, error) {
	return nil, nil
}

func (sourceWithNothing) Fetch(context.Context, download.AudioCandidate, string) (download.FetchedAudio, error) {
	return download.FetchedAudio{}, errors.New("nothing to fetch")
}

func insertReadyTrackGradedC(t *testing.T, pool *pgxpool.Pool) (ids.UserId, ids.TrackId) {
	t.Helper()
	userID := ids.NewUserId(uuid.New())
	track, err := library.NewTrack(userID, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if _, _, err := library.NewPostgresTrackRepository(pool).Add(context.Background(), track); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	})
	if _, err := pool.Exec(context.Background(), `
UPDATE tracks SET acquisition_status = 'ready', audio_ref = 'stored.mp3',
	audio_source_url = 'https://www.youtube.com/watch?v=storedvid01', acquisition_available_at = NULL,
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

func readUpgradeJobState(t *testing.T, pool *pgxpool.Pool, trackID ids.TrackId) upgradeJobState {
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

func TestNewScheduler_AnUpgradeWorkerClaimsAndSettlesAnEnqueuedUpgrade(t *testing.T) {
	pool := integrationPool(t)
	userID, trackID := insertReadyTrackGradedC(t, pool)

	cfg := &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1, MusicDir: t.TempDir(), AcquisitionUpgradeConcurrency: 1, AcquisitionUpgradeMaxTries: 3}
	scheduler := downloadsetup.NewScheduler(t.Context(), downloadsetup.Deps{
		Transport:  http.DefaultTransport,
		Config:     cfg,
		Pool:       pool,
		Events:     eventTap(t),
		Ledger:     downloadfromlibrary.NewLibraryTracks(library.NewPostgresTrackRepository(pool)),
		AudioStore: library.NewFilesystemAudioStore(cfg.MusicDir),
		Recording:  downloadfromsearch.NoRecording(),
		Sources:    downloadsetup.SourceOrder{First: []download.AudioSource{sourceWithNothing{}}},
	})
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

func TestNewScheduler_ReportsFingerprintingReadyOnlyWithAnAcoustIDKey(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "fpcalc"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fpcalc stub: %v", err)
	}
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "no key", key: "", want: false},
		{name: "a key", key: "acoustid-test-key", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newOfflineScheduler(t, &config.Config{AcquisitionDownloadConcurrency: 1, MusicDir: t.TempDir(), FFmpegLocation: binDir, AcoustIDAPIKey: tt.key}, fixtureTiers())

			if got := s.Status().Verification.Fpcalc; got != tt.want {
				t.Errorf("Status().Verification.Fpcalc = %v with fpcalc installed and AcoustID key %q, want %v", got, tt.key, tt.want)
			}
		})
	}
}

type sourceThatHangs struct{ started chan struct{} }

func (sourceThatHangs) Name() download.SourceName { return "hangs" }

func (s sourceThatHangs) Find(ctx context.Context, _ download.FindRequest) ([]download.AudioCandidate, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (sourceThatHangs) Fetch(context.Context, download.AudioCandidate, string) (download.FetchedAudio, error) {
	return download.FetchedAudio{}, errors.New("nothing to fetch")
}

func TestNewScheduler_ShutdownCancelsAJobThatOutlivesTheConfiguredDrainBudget(t *testing.T) {
	pool := integrationPool(t)
	userID := ids.NewUserId(uuid.New())
	track, err := library.NewTrack(userID, "Hanging Song", "Artist", "")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := library.NewPostgresTrackRepository(pool)
	if _, _, err := repo.Add(context.Background(), track); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID()) })
	hang := sourceThatHangs{started: make(chan struct{}, 1)}
	scheduler := downloadsetup.NewScheduler(t.Context(), downloadsetup.Deps{
		Transport:  http.DefaultTransport,
		Config:     &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1, MusicDir: t.TempDir(), AcquisitionDrainBudgetSeconds: 1},
		Pool:       pool,
		Events:     eventTap(t),
		Ledger:     downloadfromlibrary.NewLibraryTracks(repo),
		AudioStore: library.NewFilesystemAudioStore(t.TempDir()),
		Recording:  downloadfromsearch.NoRecording(),
		Sources:    downloadsetup.SourceOrder{First: []download.AudioSource{hang}},
	})
	var shutdown sync.Once
	t.Cleanup(func() { shutdown.Do(func() { scheduler.Shutdown(context.Background()) }) })
	if err := scheduler.Schedule(context.Background(), userID, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	select {
	case <-hang.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the scheduled job never reached the source within 10s")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	shutdown.Do(func() { scheduler.Shutdown(ctx) })
	elapsed := time.Since(start)

	if elapsed < time.Second {
		t.Errorf("Shutdown returned after %s, want it to give the in-flight job its 1s drain budget first", elapsed)
	}
	if elapsed > 10*time.Second {
		t.Errorf("Shutdown returned after %s, want the job cancelled once the configured 1s drain budget ran out", elapsed)
	}
}
