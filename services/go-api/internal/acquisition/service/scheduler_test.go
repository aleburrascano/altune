package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	acqports "altune/go-api/internal/acquisition/ports"

	"github.com/google/uuid"
)

type fakeTrackRepository struct {
	tracks map[string]*domain.Track
	err    error
}

func newFakeTrackRepository() *fakeTrackRepository {
	return &fakeTrackRepository{tracks: make(map[string]*domain.Track)}
}

func (r *fakeTrackRepository) Add(_ context.Context, track *domain.Track) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	key := track.ID.String() + ":" + track.UserId.String()
	if _, exists := r.tracks[key]; exists {
		return false, nil
	}
	r.tracks[key] = track
	return true, nil
}

func (r *fakeTrackRepository) GetByID(_ context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	if r.err != nil {
		return nil, r.err
	}
	key := id.String() + ":" + userId.String()
	return r.tracks[key], nil
}

func (r *fakeTrackRepository) AudioRefInUse(_ context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	for _, t := range r.tracks {
		if t.ID != excludeTrackID && t.AudioRef != nil && *t.AudioRef == audioRef {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeTrackRepository) ListForUser(_ context.Context, _ shared.UserId, _, _ int) ([]*domain.Track, int, error) {
	return nil, 0, nil
}

func (r *fakeTrackRepository) Update(_ context.Context, track *domain.Track, _ int) error {
	if r.err != nil {
		return r.err
	}
	key := track.ID.String() + ":" + track.UserId.String()
	r.tracks[key] = track
	return nil
}

func (r *fakeTrackRepository) Delete(_ context.Context, _ domain.TrackId, _ shared.UserId) (bool, error) {
	return false, nil
}

func (r *fakeTrackRepository) GetByDedupKey(_ context.Context, _ shared.UserId, _ string) (*domain.Track, error) {
	return nil, nil
}

// liveCtxTrackRepository refuses reads and writes on a context that has already
// ended, the way a real connection pool does. The plain fake ignores its
// context, so only this one can tell a settle on a live context from one on the
// job's dead context (#1975).
type liveCtxTrackRepository struct{ *fakeTrackRepository }

func (r liveCtxTrackRepository) GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.fakeTrackRepository.GetByID(ctx, id, userId)
}

func (r liveCtxTrackRepository) Update(ctx context.Context, track *domain.Track, expectedVersion int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.fakeTrackRepository.Update(ctx, track, expectedVersion)
}

// blockingSource holds the search open until the job context ends, the shape of
// a source fan-out still running when the deadline fires or the scheduler shuts
// down. searching closes on the first Find, so a test can end the job at the
// moment one is genuinely in flight.
type blockingSource struct {
	*fakeAudioSearcher
	searching chan struct{}
	announce  sync.Once
}

func newBlockingSource() *blockingSource {
	return &blockingSource{fakeAudioSearcher: &fakeAudioSearcher{}, searching: make(chan struct{})}
}

func (s *blockingSource) Find(ctx context.Context, _ acqports.FindRequest) ([]acqports.AudioCandidate, error) {
	s.announce.Do(func() { close(s.searching) })
	<-ctx.Done()
	return nil, errors.New("upstream closed")
}

type fakeAudioSearcher struct {
	searchResults []acqports.AudioCandidate
	searchErr     error
	downloadPath  string
	downloadErr   error
	searchCalled  bool
	downloadURLs  []string
}

func (s *fakeAudioSearcher) Search(_ context.Context, _ string) ([]acqports.AudioCandidate, error) {
	s.searchCalled = true
	return s.searchResults, s.searchErr
}

func (s *fakeAudioSearcher) Download(_ context.Context, url string, _ string) (string, error) {
	s.downloadURLs = append(s.downloadURLs, url)
	return s.downloadPath, s.downloadErr
}

func (s *fakeAudioSearcher) Name() string { return "fake" }

func (s *fakeAudioSearcher) Find(ctx context.Context, _ acqports.FindRequest) ([]acqports.AudioCandidate, error) {
	return s.Search(ctx, "")
}

func (s *fakeAudioSearcher) Fetch(ctx context.Context, c acqports.AudioCandidate, outDir string) (string, error) {
	return s.Download(ctx, c.URL, outDir)
}

func fakeRegistry(s *fakeAudioSearcher) *SourceRegistry { return NewSourceRegistry(s) }

type fakeAudioStore struct {
	stored map[string]bool
	err    error
}

func newFakeAudioStore() *fakeAudioStore {
	return &fakeAudioStore{stored: make(map[string]bool)}
}

func (s *fakeAudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.stored[audioRef], nil
}

func (s *fakeAudioStore) Store(_ context.Context, _ string, audioRef string) error {
	if s.err != nil {
		return s.err
	}
	s.stored[audioRef] = true
	return nil
}

func (s *fakeAudioStore) Stream(_ context.Context, _ string) (ports.AudioStream, int64, error) {
	return nil, 0, nil
}

func (s *fakeAudioStore) Delete(_ context.Context, audioRef string) error {
	delete(s.stored, audioRef)
	return nil
}

func TestBackgroundScheduler_Schedule(t *testing.T) {
	repo := newFakeTrackRepository()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()

	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	scheduler.Schedule(context.Background(), userId, trackId, "")

	wg.Wait()
	if len(sem) != 0 {
		t.Errorf("semaphore should be empty after goroutine completes, got %d tokens held", len(sem))
	}
}

func TestBackgroundScheduler_ScheduleMultiple_RespectsSemaphore(t *testing.T) {
	repo := newFakeTrackRepository()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()

	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	userId := shared.NewUserId(uuid.New())

	var completedCount atomic.Int32
	const numSchedules = 3

	for i := 0; i < numSchedules; i++ {
		trackId := domain.NewTrackId()
		scheduler.Schedule(context.Background(), userId, trackId, "")
	}

	wg.Wait()
	_ = completedCount.Load()

	if len(sem) != 0 {
		t.Errorf("semaphore should be empty after all goroutines complete, got %d tokens held", len(sem))
	}
}

// Shutdown cancels the scheduler's base context with no drain window, so a job
// blocked mid-search settles on a context that has already ended. The failure
// still has to reach the store, or the track spins as pending until the stale
// sweep ten minutes later (#1975).
func TestBackgroundScheduler_ShutdownMidSearch_PersistsCancellationFailure(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	key := track.ID.String() + ":" + userId.String()
	repo.tracks[key] = track
	source := newBlockingSource()
	svc := NewAcquireTrackAudioService(liveCtxTrackRepository{repo}, NewSourceRegistry(source), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))
	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	<-source.searching

	scheduler.Shutdown(context.Background())

	settled := repo.tracks[key]
	if settled.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want %v (failed)", settled.AcquisitionStatus, domain.AcquisitionFailed)
	}
	if got := deref(settled.FailureReason); got != string(domain.FailureAcquisitionCancelled) {
		t.Errorf("persisted failure_reason = %q, want %q", got, domain.FailureAcquisitionCancelled)
	}
}

const (
	// racingSchedulers is how many Schedule calls fire at the instant Shutdown
	// does. Each uses its own track id, so none is deduped, and the burst stays
	// under both the admission queue (cap(sem)*defaultQueueDepthFactor) and
	// recentJobCap: a nil error here always means exactly one job was spawned,
	// and every settled job is still in the log when the drain ends.
	racingSchedulers = 8
	// shutdownRaceAttempts replays the race often enough to catch an interleaving
	// that only sometimes lands inside the window between the shutdown check and
	// the WaitGroup Add.
	shutdownRaceAttempts = 300
)

// A Schedule that has already passed the shutdown check must finish registering
// its job before the drain declares itself done: sync.WaitGroup requires the Add
// that lifts the counter off zero to happen before Wait, and a job added after
// Wait returned runs past the drain on an already-cancelled context (#1979).
func TestBackgroundScheduler_ScheduleRacingShutdown_DrainsEveryQueuedJob(t *testing.T) {
	for attempt := 0; attempt < shutdownRaceAttempts; attempt++ {
		active, settled, queued := scheduleWhileShuttingDown()

		if len(active) != 0 {
			t.Fatalf("attempt %d: %d job(s) still unsettled when Shutdown returned, want 0", attempt, len(active))
		}
		if len(settled) != queued {
			t.Fatalf("attempt %d: %d job(s) settled when Shutdown returned, want %d (one per queued Schedule)",
				attempt, len(settled), queued)
		}
	}
}

// scheduleWhileShuttingDown races racingSchedulers Schedule calls against
// Shutdown and reports the job log as of the instant Shutdown returned, next to
// the number of calls that reported a job queued.
func scheduleWhileShuttingDown() (active, settled []acqports.JobRecord, queued int) {
	svc := NewAcquireTrackAudioService(&countingRepo{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var jobs sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &jobs, make(chan struct{}, 4))
	userId := shared.NewUserId(uuid.New())

	var accepted atomic.Int64
	start := make(chan struct{})
	var callers sync.WaitGroup
	for i := 0; i < racingSchedulers; i++ {
		trackId := domain.NewTrackId()
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err == nil {
				accepted.Add(1)
			}
		}()
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-start
		scheduler.Shutdown(context.Background())
		active, settled = scheduler.log.snapshot()
	}()

	close(start)
	<-drained
	callers.Wait()
	jobs.Wait()
	return active, settled, int(accepted.Load())
}

const (
	// testQueueWait is the queue-wait deadline the regression below runs under:
	// long enough not to fire while the second job is still being admitted,
	// short enough to keep the test sub-second.
	testQueueWait = 50 * time.Millisecond
	// jobSettleTimeout is how long a test waits for a job to leave the active
	// log. Only a job that never settles — the defect — reaches it, so it is
	// generous enough to survive a loaded CI runner.
	jobSettleTimeout = 2 * time.Second
)

// An admitted job must not wait for a worker slot indefinitely: with the only
// worker held, the queued job settles as cancelled once its wait expires
// instead of showing pending behind several ten-minute acquisitions (#1981).
func TestBackgroundScheduler_QueueWaitExpires_CancelsTheJobWithoutRunningIt(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithQueueWaitTimeout(testQueueWait))
	userId := shared.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-repo.started
	queued := domain.NewTrackId()

	if err := scheduler.Schedule(context.Background(), userId, queued, ""); err != nil {
		t.Fatalf("second Schedule = %v, want nil (admitted, waiting for a slot)", err)
	}

	settled := awaitSettledJob(t, scheduler, queued.String())
	if settled.State != JobCancelled {
		t.Errorf("expired job state = %q, want %q", settled.State, JobCancelled)
	}
	if settled.Reason != "queue_wait_timeout" {
		t.Errorf("expired job reason = %q, want %q", settled.Reason, "queue_wait_timeout")
	}
	if got := repo.calls.Load(); got != 1 {
		t.Errorf("track loads = %d, want 1 (only the running job; the expired one must never run)", got)
	}
	close(repo.release)
	wg.Wait()
}

// awaitSettledJob returns trackID's record once the job has left the active
// log. Polling rather than blocking keeps a job that never settles a readable
// failure instead of a hung test.
func awaitSettledJob(t *testing.T, s *BackgroundAcquisitionScheduler, trackID string) acqports.JobRecord {
	t.Helper()
	deadline := time.Now().Add(jobSettleTimeout)
	for time.Now().Before(deadline) {
		if job, settled := settledJob(s, trackID); settled {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s still waiting for a worker slot after %s, want settled", trackID, jobSettleTimeout)
	return acqports.JobRecord{}
}

func settledJob(s *BackgroundAcquisitionScheduler, trackID string) (acqports.JobRecord, bool) {
	_, recent := s.log.snapshot()
	for _, job := range recent {
		if job.TrackID == trackID {
			return job, true
		}
	}
	return acqports.JobRecord{}, false
}

func TestNewBackgroundAcquisitionScheduler_ReturnsNonNil(t *testing.T) {
	repo := newFakeTrackRepository()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)

	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)
	if scheduler == nil {
		t.Fatal("NewBackgroundAcquisitionScheduler returned nil")
	}
}
