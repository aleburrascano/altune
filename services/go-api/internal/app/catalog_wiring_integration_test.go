package app_test

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/monitoring/monitoringtest"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/polltest"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func fixtureTiers() downloadtest.SourceOrder {
	return downloadtest.SourceOrder{
		First:    []downloadtest.AudioSource{downloadtest.NewArtTrackSource(downloadtest.Clip)},
		Fallback: []downloadtest.AudioSource{downloadtest.NewUploadSource(downloadtest.Clip)},
	}
}

func acquisitionEventTap(t *testing.T) *monitoring.EventTap {
	t.Helper()
	return monitoringtest.MustNewEventTap(events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor")), time.Now)
}

func newOutboundClient(timeout time.Duration, transport http.RoundTripper) *http.Client {
	return outbound.MustNewClient(timeout, transport)
}

func downloadOnPostgres(t *testing.T, cfg *config.Config, pool *pgxpool.Pool, repo *library.PostgresTrackRepository, sources downloadtest.SourceOrder) *download.Module {
	t.Helper()
	cfg.AcquisitionDeezerPreviewTimeout = time.Second
	metrics := librarytest.DiscardDBCallMetrics()
	store := librarytest.NewFilesystemAudioStore(cfg.MusicDir, librarytest.DiscardFSCallMetrics())
	recording := downloadfromsearch.NoRecording()
	module, err := download.New(t.Context(), download.Deps{
		Config:            cfg,
		Transport:         http.DefaultTransport,
		Database:          download.NewDatabase(pool),
		Events:            acquisitionEventTap(t),
		NewClient:         newOutboundClient,
		Library:           downloadfromlibrary.NewLibraryTracks(repo, time.Now, ids.Real),
		InPlaceVerdicts:   downloadfromlibrary.NewInPlaceVerdicts(repo),
		AudioStore:        store,
		StoredAudio:       downloadfromlibrary.NewStoredAudioReader(store),
		Orphans:           downloadfromlibrary.NewOrphanedAudioRecorder(library.NewPostgresOrphanedAudioRepository(pool, metrics)),
		RecordingResolver: recording.Resolver,
		RecordingLookup:   recording.Lookup,
		Rerecordings:      recording.Rerecordings,
		Sources:           sources,
	})
	if err != nil {
		t.Fatalf("download.New: %v", err)
	}
	if !module.On() {
		t.Fatal("download.New with sources and an audio store left acquisition off")
	}
	return module
}

func insertPendingTrack(t *testing.T, pool *pgxpool.Pool, repo *library.PostgresTrackRepository, title string, duration float64) (ids.UserId, *library.Track) {
	t.Helper()
	userID := ids.NewUserId(uuid.New())
	track, err := librarytest.NewTrack(librarytest.NewTrackInput{UserId: userID, Title: title, Artist: "Fixture Artist", Album: ""}, time.Now, ids.Real)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if duration > 0 {
		if err := track.SetDuration(duration); err != nil {
			t.Fatalf("set duration: %v", err)
		}
	}
	if _, _, err := repo.Add(context.Background(), track); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID().UUID()) })
	return userID, track
}

func acquireThroughPostgres(t *testing.T) (library.Track, string, download.Verification) {
	t.Helper()
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	musicDir := t.TempDir()
	repo := library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	userID, track := insertPendingTrack(t, pool, repo, "Fixture Song", 212)
	cfg := &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1, AcquisitionUpgradeConcurrency: 1, MusicDir: musicDir}
	scheduler := downloadOnPostgres(t, cfg, pool, repo, fixtureTiers()).Scheduler()
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	if err := scheduler.Schedule(context.Background(), userID, track.ID(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	return awaitReady(t, repo, track, userID), musicDir, scheduler.Status(t.Context()).Verification
}

func awaitReady(t *testing.T, repo *library.PostgresTrackRepository, track *library.Track, userID ids.UserId) library.Track {
	t.Helper()
	var got *library.Track
	polltest.Within(10*time.Second, func() bool {
		var err error
		got, err = repo.GetByID(context.Background(), track.ID(), userID)
		if err != nil {
			t.Fatalf("read track: %v", err)
		}
		return got.DownloadStatus() == library.AcquisitionReady
	})
	return *got
}

func assertReadyWithAudio(t *testing.T, track library.Track, musicDir string) {
	t.Helper()
	if track.DownloadStatus() != library.AcquisitionReady || track.AudioRef() == nil {
		t.Fatalf("status = %v audioRef = %v, want ready with a ref", track.DownloadStatus(), track.AudioRef())
	}
	stored, err := os.Stat(filepath.Join(musicDir, *track.AudioRef()))
	if err != nil || stored.Size() == 0 {
		t.Fatalf("stored audio %q: size %v err %v", *track.AudioRef(), stored, err)
	}
}

func TestCatalogAcquisition_ZeroUpgradeWorkersBuildsNoUpgradeLaneInsteadOfPanicking(t *testing.T) {
	pool := unreachablePool(t)
	musicDir := t.TempDir()

	module := downloadOnPostgres(t, &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1, MusicDir: musicDir},
		pool, library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics()), fixtureTiers())
	t.Cleanup(func() { module.Scheduler().Shutdown(context.Background()) })

	if module.Scheduler() == nil {
		t.Fatal("download.New returned no scheduler")
	}
}

func TestCatalogAcquisition_TheFirstTierArtTrackIsTheStoredSource(t *testing.T) {
	track, musicDir, _ := acquireThroughPostgres(t)

	assertReadyWithAudio(t, track, musicDir)
	if track.AudioSourceURL() == nil || *track.AudioSourceURL() != downloadtest.ArtTrackURL {
		t.Fatalf("stored source = %v, want the resolved art track %q", track.AudioSourceURL(), downloadtest.ArtTrackURL)
	}
}

func TestCatalogAcquisition_FetchedAudioPassesTheProbeAndDecodeGates(t *testing.T) {
	if ffprobe, ffmpeg := downloadtest.FfprobeAvailable(os.Getenv("FFMPEG_LOCATION")); !ffprobe || !ffmpeg {
		t.Skip("ffprobe/ffmpeg not installed")
	}

	track, musicDir, verification := acquireThroughPostgres(t)

	if !verification.Ffprobe || !verification.Ffmpeg {
		t.Errorf("verification = %+v, want the composed prober to report ffprobe and ffmpeg", verification)
	}
	assertReadyWithAudio(t, track, musicDir)
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

func TestCatalogAcquisition_RecordsTheOutcomeThroughThePostgresStore(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	repo := library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	userID, track := insertPendingTrack(t, pool, repo, "Song", 0)
	cfg := &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1, AcquisitionUpgradeConcurrency: 1, MusicDir: t.TempDir()}
	scheduler := downloadOnPostgres(t, cfg, pool, repo, fixtureTiers()).Scheduler()
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	if err := scheduler.Schedule(context.Background(), userID, track.ID(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	polltest.Until(t, 5*time.Second, fmt.Sprintf("an acquisition_outcomes row for track %s (download.New must record outcomes in the Postgres store)", track.ID()), func() bool {
		return countAcquisitionOutcomeRows(t, pool, track.ID().String()) == 1
	})
}

type sourceWithNothing struct{}

func (sourceWithNothing) Name() downloadtest.SourceName { return "nothing" }

func (sourceWithNothing) Find(context.Context, downloadtest.FindRequest) ([]downloadtest.AudioCandidate, error) {
	return nil, nil
}

func (sourceWithNothing) Fetch(context.Context, downloadtest.AudioCandidate, string) (downloadtest.FetchedAudio, error) {
	return downloadtest.FetchedAudio{}, errors.New("nothing to fetch")
}

func insertReadyTrackGradedC(t *testing.T, pool *pgxpool.Pool, repo *library.PostgresTrackRepository) (ids.UserId, ids.TrackId) {
	t.Helper()
	userID, track := insertPendingTrack(t, pool, repo, "Song", 0)
	if _, err := pool.Exec(context.Background(), `
UPDATE tracks SET acquisition_status = 'ready', audio_ref = 'stored.mp3',
	audio_source_url = 'https://www.youtube.com/watch?v=storedvid01',
	acquisition_evidence = '{"grade": "C"}'
WHERE id = $1`, track.ID().UUID()); err != nil {
		t.Fatalf("mark ready graded C: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE acquisition_jobs SET available_at = NULL WHERE track_id = $1`, track.ID().UUID()); err != nil {
		t.Fatalf("unschedule the finished job: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO acquisition_upgrade_state (track_id, evidence) VALUES ($1, '{"in_place_generation": 2}')
ON CONFLICT (track_id) DO UPDATE SET evidence = EXCLUDED.evidence`, track.ID().UUID()); err != nil {
		t.Fatalf("seed the upgrade state: %v", err)
	}
	return userID, track.ID()
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
SELECT j.job_kind, j.available_at IS NOT NULL, j.lease_until IS NOT NULL,
	j.run_attempts, COALESCE((s.evidence->>'upgrade_tries')::int, 0)
FROM acquisition_jobs j LEFT JOIN acquisition_upgrade_state s ON s.track_id = j.track_id
WHERE j.track_id = $1`, trackID.UUID()).Scan(&s.Kind, &s.Queued, &s.Leased, &s.Claims, &s.UpgradeTries)
	if err != nil {
		t.Fatalf("read upgrade job state: %v", err)
	}
	return s
}

func TestCatalogAcquisition_AnUpgradeWorkerClaimsAndSettlesAnEnqueuedUpgrade(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	repo := library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	userID, trackID := insertReadyTrackGradedC(t, pool, repo)
	cfg := &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1, MusicDir: t.TempDir(), AcquisitionUpgradeConcurrency: 1, AcquisitionUpgradeMaxTries: 3}
	scheduler := downloadOnPostgres(t, cfg, pool, repo, downloadtest.SourceOrder{First: []downloadtest.AudioSource{sourceWithNothing{}}}).Scheduler()
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	if err := scheduler.ScheduleUpgrade(context.Background(), userID, trackID); err != nil {
		t.Fatalf("schedule upgrade: %v", err)
	}

	want := upgradeJobState{Kind: "acquire", Queued: false, Leased: false, Claims: 1, UpgradeTries: 1}
	const settleWithin = 15 * time.Second
	var got upgradeJobState
	if !polltest.Within(settleWithin, func() bool {
		got = readUpgradeJobState(t, pool, trackID)
		return got == want
	}) {
		t.Fatalf("upgrade job of track %s after %v = %+v, want %+v (claimed once through the upgrade lane, an attempt noted, the job settled off the queue)", trackID, settleWithin, got, want)
	}
}

type sourceThatHangs struct{ started chan struct{} }

func (sourceThatHangs) Name() downloadtest.SourceName { return "hangs" }

func (s sourceThatHangs) Find(ctx context.Context, _ downloadtest.FindRequest) ([]downloadtest.AudioCandidate, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (sourceThatHangs) Fetch(context.Context, downloadtest.AudioCandidate, string) (downloadtest.FetchedAudio, error) {
	return downloadtest.FetchedAudio{}, errors.New("nothing to fetch")
}

func TestCatalogAcquisition_ShutdownCancelsAJobThatOutlivesTheConfiguredDrainBudget(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	repo := library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	userID, track := insertPendingTrack(t, pool, repo, "Hanging Song", 0)
	hang := sourceThatHangs{started: make(chan struct{}, 1)}
	cfg := &config.Config{Env: "test", AcquisitionDownloadConcurrency: 1, AcquisitionUpgradeConcurrency: 1, MusicDir: t.TempDir(), AcquisitionDrainBudgetSeconds: 1}
	scheduler := downloadOnPostgres(t, cfg, pool, repo, downloadtest.SourceOrder{First: []downloadtest.AudioSource{hang}}).Scheduler()
	var shutdown sync.Once
	t.Cleanup(func() { shutdown.Do(func() { scheduler.Shutdown(context.Background()) }) })
	if err := scheduler.Schedule(context.Background(), userID, track.ID(), ""); err != nil {
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
