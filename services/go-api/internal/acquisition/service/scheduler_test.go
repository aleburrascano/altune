package service

import (
	acqports "altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// cookieJarPath stands for the host path yt-dlp names in its stderr when the
// --cookies file will not open. The acquisition error chain carries subprocess
// stderr verbatim, and the scheduler's own failure wrapper is the last place
// that text can be stopped before it reaches the job log — which the admin
// status endpoint serves as `reason` (#1972).
const cookieJarPath = "/home/x/cookies.txt"

func TestBackgroundScheduler_FailedJob_KeepsTheCookiePathOutOfTheReasonAndTheLog(t *testing.T) {
	logs := captureJSONLog(t)
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	leaking := &fakeAudioSearcher{searchErr: errors.New("yt-dlp exited 1: --cookies " + cookieJarPath + ": permission denied")}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(leaking), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))

	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	wg.Wait()

	_, recent := scheduler.log.snapshot()
	if len(recent) != 1 || recent[0].State != JobFailed {
		t.Fatalf("recent jobs = %+v, want exactly one %s job", recent, JobFailed)
	}
	if strings.Contains(recent[0].Reason, cookieJarPath) {
		t.Errorf("job reason = %q, still names the cookie file %q", recent[0].Reason, cookieJarPath)
	}
	if strings.Contains(logs.String(), cookieJarPath) {
		t.Errorf("scheduler log names the cookie file %q:\n%s", cookieJarPath, logs.String())
	}
	if _, failed := scheduler.log.counts(); failed != 1 {
		t.Errorf("failed count = %d, want 1 (redaction must not change failure counting)", failed)
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

// startedAcquirer blocks Execute/ExecuteReplace until release closes and
// signals started on the first call, so a test can pin a worker slot and then
// assert exactly which jobs actually ran their acquisition — as opposed to
// which jobs merely had their track loaded during settle, which RefuseQueued
// now also does for an abandoned job (#2789).
type startedAcquirer struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (a *startedAcquirer) Execute(context.Context, shared.UserId, domain.TrackId) error {
	a.calls.Add(1)
	a.once.Do(func() { close(a.started) })
	<-a.release
	return nil
}

func (a *startedAcquirer) ExecuteReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	return a.Execute(ctx, userId, trackId)
}

func (a *startedAcquirer) RefuseQueued(context.Context, shared.UserId, domain.TrackId) {}

// An admitted job must not wait for a worker slot indefinitely: with the only
// worker held, the queued job settles as cancelled once its wait expires
// instead of showing pending behind several ten-minute acquisitions (#1981).
func TestBackgroundScheduler_QueueWaitExpires_CancelsTheJobWithoutRunningIt(t *testing.T) {
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1), WithQueueWaitTimeout(testQueueWait))
	userId := shared.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-acq.started
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
	if got := acq.calls.Load(); got != 1 {
		t.Errorf("acquirer executions = %d, want 1 (only the running job; the expired one must never run its acquisition, even though settling now loads its track)", got)
	}
	close(acq.release)
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

// trackHeldByJob starts hold's job and returns the scheduler once that job is
// running, so the track's in-flight slot is genuinely taken, plus a drain that
// lets the job finish and waits for it: past drain the slot is free. Cleanup
// drains again for whatever the test scheduled afterwards.
func trackHeldByJob(t *testing.T, hold func(*BackgroundAcquisitionScheduler) error) (*BackgroundAcquisitionScheduler, func()) {
	t.Helper()
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 2))
	var released sync.Once
	drain := func() {
		released.Do(func() { close(repo.release) })
		wg.Wait()
	}
	t.Cleanup(drain)
	if err := hold(scheduler); err != nil {
		t.Fatalf("holding schedule = %v, want nil", err)
	}
	<-repo.started
	return scheduler, drain
}

// The in-flight registry is keyed by track alone, so a replace asked for while
// a plain acquisition runs used to be reported as queued: the replace never
// ran, and the gate kept the reacquire cooldown for a job that did not exist,
// locking the user out for the whole window (#1980).
func TestReacquireAdmission_PlainJobInFlight_RefusesAndRefundsCooldown(t *testing.T) {
	track := readyTrack(t)
	scheduler, drain := trackHeldByJob(t, func(s *BackgroundAcquisitionScheduler) error {
		return s.Schedule(context.Background(), track.UserId, track.ID, "")
	})
	admission := NewReacquireAdmission(newFakeCooldownStore())
	replace := func() error { return scheduler.ScheduleReplace(context.Background(), track.UserId, track.ID) }

	if err := admission.Admit(context.Background(), track, replace); !errors.Is(err, ErrTrackJobInFlight) {
		t.Fatalf("reacquire while a plain job runs = %v, want ErrTrackJobInFlight", err)
	}
	drain()

	if err := admission.Admit(context.Background(), track, replace); err != nil {
		t.Errorf("reacquire after the plain job settled = %v, want nil (cooldown must not be burned by the dropped replace)", err)
	}
}

// The mirror of the case above, the same defect from the other side: a retry
// must not be reported as queued because a replace happens to hold the track.
func TestRetryAdmission_ReplaceInFlight_RefusesAndRefundsCooldown(t *testing.T) {
	track := failedTrack(t)
	scheduler, drain := trackHeldByJob(t, func(s *BackgroundAcquisitionScheduler) error {
		return s.ScheduleReplace(context.Background(), track.UserId, track.ID)
	})
	admission := NewRetryAdmission(newFakeCooldownStore())
	retry := func() error { return scheduler.Schedule(context.Background(), track.UserId, track.ID, "") }

	if err := admission.Admit(context.Background(), track, retry); !errors.Is(err, ErrTrackJobInFlight) {
		t.Fatalf("retry while a replace runs = %v, want ErrTrackJobInFlight", err)
	}
	drain()

	if err := admission.Admit(context.Background(), track, retry); err != nil {
		t.Errorf("retry after the replace settled = %v, want nil (cooldown must not be burned by the dropped retry)", err)
	}
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

// stubAcquirer is the seam's payoff: a scheduler job can be driven without a
// repository, a store, or a provider registry behind it.
type stubAcquirer struct {
	mu  sync.Mutex
	ran []string
}

func (s *stubAcquirer) Execute(context.Context, shared.UserId, domain.TrackId) error {
	s.record("Execute")
	return nil
}

func (s *stubAcquirer) ExecuteReplace(context.Context, shared.UserId, domain.TrackId) error {
	s.record("ExecuteReplace")
	return nil
}

func (s *stubAcquirer) RefuseQueued(context.Context, shared.UserId, domain.TrackId) {}

func (s *stubAcquirer) record(entryPoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ran = append(s.ran, entryPoint)
}

func (s *stubAcquirer) entryPointsRun() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ran...)
}

func TestBackgroundScheduler_RunsTheStubbedAcquirerEntryPoint(t *testing.T) {
	cases := map[string]struct {
		schedule func(*BackgroundAcquisitionScheduler, shared.UserId, domain.TrackId) error
		want     string
	}{
		"Schedule runs Execute": {
			schedule: func(s *BackgroundAcquisitionScheduler, userId shared.UserId, trackId domain.TrackId) error {
				return s.Schedule(context.Background(), userId, trackId, "")
			},
			want: "Execute",
		},
		"ScheduleReplace runs ExecuteReplace": {
			schedule: func(s *BackgroundAcquisitionScheduler, userId shared.UserId, trackId domain.TrackId) error {
				return s.ScheduleReplace(context.Background(), userId, trackId)
			},
			want: "ExecuteReplace",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			acq := &stubAcquirer{}
			var wg sync.WaitGroup
			scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1))

			if err := tc.schedule(scheduler, shared.NewUserId(uuid.New()), domain.NewTrackId()); err != nil {
				t.Fatalf("schedule: %v", err)
			}
			wg.Wait()

			got := acq.entryPointsRun()
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("entry points run = %v, want [%s]", got, tc.want)
			}
		})
	}
}

// burstRepo blocks the first in-flight acquisition until release is closed,
// letting a test saturate the worker semaphore and observe how many jobs pile
// up behind it. sync.Once guards the started signal so repeated GetByID calls
// (one per admitted job once drained) do not double-close the channel.
type burstRepo struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (r *burstRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	r.calls.Add(1)
	r.once.Do(func() { close(r.started) })
	<-r.release
	return nil, nil
}

func (r *burstRepo) Update(_ context.Context, _ *domain.Track, _ int) error { return nil }

func (r *burstRepo) AudioRefInUse(_ context.Context, _ string, _ domain.TrackId) (bool, error) {
	return false, nil
}

// TestBackgroundScheduler_BoundsQueueDepthUnderBurst reproduces the
// backpressure defect: a burst of Schedule calls far beyond the configured
// worker concurrency must not register a job-log entry (and spawn a goroutine)
// per arrival. Total outstanding work has to stay bounded relative to
// concurrency, not grow with arrivals.
func TestBackgroundScheduler_BoundsQueueDepthUnderBurst(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	userId := shared.NewUserId(uuid.New())
	const burst = 60
	for i := 0; i < burst; i++ {
		scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), "")
	}

	// Ensure a worker is actually in-flight so the semaphore is saturated.
	<-repo.started

	active := len(scheduler.Status().ActiveJobs)
	maxAllowed := cap(sem) * 8
	if active > maxAllowed {
		t.Errorf("active job-log entries = %d under burst of %d; want bounded <= %d (arrivals must not spawn unbounded work)",
			active, burst, maxAllowed)
	}

	close(repo.release)
	wg.Wait()
}

// TestBackgroundScheduler_ReportsQueueFull pins the reporting-full contract: at
// a fixed queue depth, a burst admits exactly depth jobs and reports the rest
// as rejected, without registering job-log entries for them.
func TestBackgroundScheduler_ReportsQueueFull(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	const queueDepth = 4
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem, WithQueueDepth(queueDepth))

	userId := shared.NewUserId(uuid.New())
	const burst = 50
	for i := 0; i < burst; i++ {
		scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), "")
	}

	<-repo.started

	status := scheduler.Status()
	if got := len(status.ActiveJobs); got != queueDepth {
		t.Errorf("active job-log entries = %d, want exactly %d (queue depth)", got, queueDepth)
	}
	if want := uint64(burst - queueDepth); status.Rejected != want {
		t.Errorf("rejected = %d, want %d (arrivals past the queue depth)", status.Rejected, want)
	}
	if status.QueueDepth != queueDepth {
		t.Errorf("queue depth = %d, want %d (admission queue saturated)", status.QueueDepth, queueDepth)
	}
	if status.QueueCapacity != queueDepth {
		t.Errorf("queue capacity = %d, want %d (configured depth)", status.QueueCapacity, queueDepth)
	}

	close(repo.release)
	wg.Wait()
}

// TestBackgroundScheduler_StatusQueueDrainsAndCountsShutdownRejections pins
// that the queue-depth gauge returns to zero once jobs drain, that capacity
// stays at the configured depth, and that jobs refused during shutdown count
// toward Rejected alongside queue-full sheds.
func TestBackgroundScheduler_StatusQueueDrainsAndCountsShutdownRejections(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	close(repo.release)
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	const queueDepth = 3
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem, WithQueueDepth(queueDepth))

	userId := shared.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	wg.Wait()

	status := scheduler.Status()
	if status.QueueDepth != 0 {
		t.Errorf("queue depth after drain = %d, want 0", status.QueueDepth)
	}
	if status.QueueCapacity != queueDepth {
		t.Errorf("queue capacity = %d, want %d", status.QueueCapacity, queueDepth)
	}

	scheduler.Shutdown(context.Background())
	err := scheduler.ScheduleReplace(context.Background(), userId, domain.NewTrackId())
	if !errors.Is(err, ErrSchedulerShutdown) {
		t.Fatalf("schedule after shutdown err = %v, want ErrSchedulerShutdown", err)
	}
	if got := scheduler.Status().Rejected; got != 1 {
		t.Errorf("rejected after shutdown refusal = %d, want 1", got)
	}
}

// corrCapturingRepo records the correlation ID carried by the context the
// background job hands to the acquisition service. The job context is built in
// scheduler.go from s.baseCtx, so before the fix it carried no link to the
// originating request and this observed empty.
type corrCapturingRepo struct {
	*fakeTrackRepository
	mu     sync.Mutex
	corrID string
	seen   bool
}

func (r *corrCapturingRepo) GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	r.mu.Lock()
	r.corrID = logging.CorrelationIDFromContext(ctx)
	r.seen = true
	r.mu.Unlock()
	return r.fakeTrackRepository.GetByID(ctx, id, userId)
}

// TestBackgroundScheduler_ThreadsCorrelationIDIntoJobContext reproduces the
// defect: a scheduled job's context must carry the request's correlation ID so
// every slog.*Context call made deep in the acquisition pipeline traces back to
// the originating request. Red before the job context was derived with the
// request's corr_id; green once Schedule threads r.Context() through.
func TestBackgroundScheduler_ThreadsCorrelationIDIntoJobContext(t *testing.T) {
	repo := &corrCapturingRepo{fakeTrackRepository: newFakeTrackRepository()}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	const wantCorrID = "corr-xyz-345"
	ctx := logging.WithCorrelationID(context.Background(), wantCorrID)
	scheduler.Schedule(ctx, shared.NewUserId(uuid.New()), domain.NewTrackId(), "")

	wg.Wait()

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if !repo.seen {
		t.Fatal("acquisition service was never invoked by the scheduled job")
	}
	if repo.corrID != wantCorrID {
		t.Fatalf("job context lost the request correlation id: got %q, want %q", repo.corrID, wantCorrID)
	}
}

// A ready track whose audio still exists is the one input where the two
// service entry points diverge observably: Execute reconciles and skips it,
// ExecuteReplace searches for a new source. So it pins which one each
// scheduler method dispatches to.
func TestBackgroundScheduler_DispatchesToTheMatchingServiceEntryPoint(t *testing.T) {
	cases := []struct {
		name       string
		wantSearch bool
	}{
		{
			name:       "Schedule runs Execute",
			wantSearch: false,
		},
		{
			name:       "ScheduleReplace runs ExecuteReplace",
			wantSearch: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			repo := newFakeTrackRepository()
			track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=old")
			store := newFakeAudioStore()
			store.stored["u/a/b/c.mp3"] = true
			searcher := &fakeAudioSearcher{}
			svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

			var wg sync.WaitGroup
			scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))
			if tc.wantSearch {
				scheduler.ScheduleReplace(context.Background(), userId, track.ID)
			} else {
				scheduler.Schedule(context.Background(), userId, track.ID, "")
			}
			wg.Wait()

			if searcher.searchCalled != tc.wantSearch {
				t.Errorf("search called = %v, want %v", searcher.searchCalled, tc.wantSearch)
			}
		})
	}
}

type blockingRepo struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (r *blockingRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	r.calls.Add(1)
	close(r.started)
	<-r.release
	return nil, nil
}
func (r *blockingRepo) Update(_ context.Context, _ *domain.Track, _ int) error { return nil }

func (r *blockingRepo) AudioRefInUse(_ context.Context, _ string, _ domain.TrackId) (bool, error) {
	return false, nil
}

func TestBackgroundScheduler_Schedule_DedupsInflight(t *testing.T) {
	repo := &blockingRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	scheduler.Schedule(context.Background(), userId, trackId, "")
	<-repo.started
	scheduler.Schedule(context.Background(), userId, trackId, "")
	close(repo.release)
	wg.Wait()

	if got := repo.calls.Load(); got != 1 {
		t.Errorf("GetByID calls = %d, want 1 (second schedule must be deduped)", got)
	}
}

type countingRepo struct{ calls atomic.Int32 }

func (r *countingRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	r.calls.Add(1)
	return nil, nil
}
func (r *countingRepo) Update(_ context.Context, _ *domain.Track, _ int) error { return nil }

func (r *countingRepo) AudioRefInUse(_ context.Context, _ string, _ domain.TrackId) (bool, error) {
	return false, nil
}

func TestBackgroundScheduler_Schedule_AfterShutdown_NoOp(t *testing.T) {
	repo := &countingRepo{}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler.Shutdown(ctx)

	scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), "")
	wg.Wait()

	if got := repo.calls.Load(); got != 0 {
		t.Errorf("GetByID calls = %d, want 0 (schedule after shutdown must be a no-op)", got)
	}
}

type panicRepo struct{}

func (r *panicRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	panic("boom")
}
func (r *panicRepo) Update(_ context.Context, _ *domain.Track, _ int) error { return nil }

func (r *panicRepo) AudioRefInUse(_ context.Context, _ string, _ domain.TrackId) (bool, error) {
	return false, nil
}

func TestBackgroundScheduler_Schedule_RecoversFromPanic(t *testing.T) {
	svc := NewAcquireTrackAudioService(&panicRepo{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), "")
	wg.Wait()
}

// TestBackgroundScheduler_OnePrincipalCannotStarveAnother reproduces the
// starvation defect: with only a single shared admission queue, one authenticated
// user can fill every slot and every other user's job is rejected. A per-principal
// share must cap how much of the queue one user holds so slots remain for others.
//
// Setup: global queue depth 4, per-principal cap 2, worker concurrency 1 (so the
// first admitted job stays in flight and nothing drains). User A bursts 4 jobs;
// with fairness only 2 are admitted, leaving 2 global slots for user B. Without
// the fix, A's 4 jobs fill the whole queue and B is refused — the regression.
func TestBackgroundScheduler_OnePrincipalCannotStarveAnother(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem,
		WithQueueDepth(4), WithPrincipalQueueDepth(2))

	userA := shared.NewUserId(uuid.New())
	userB := shared.NewUserId(uuid.New())

	// User A floods: first 2 admitted (its full share), the rest rejected as its
	// per-principal queue is full — not as a global queue-full shed.
	results := make([]error, 4)
	for i := range results {
		results[i] = scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), "")
	}
	for i := 0; i < 2; i++ {
		if results[i] != nil {
			t.Fatalf("user A schedule #%d = %v, want nil (within per-principal share)", i+1, results[i])
		}
	}
	for i := 2; i < 4; i++ {
		if !errors.Is(results[i], ErrPrincipalQueueFull) {
			t.Fatalf("user A schedule #%d = %v, want ErrPrincipalQueueFull (share exhausted)", i+1, results[i])
		}
	}

	// A worker for user A is now in flight and holding the sole semaphore slot.
	<-repo.started

	// User B must still get in: A holds only its 2-slot share, so 2 global slots
	// remain. Before the fix, A owned all 4 slots and this was ErrAcquisitionQueueFull.
	if err := scheduler.Schedule(context.Background(), userB, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("user B schedule while A floods = %v, want nil (A must not starve B)", err)
	}

	close(repo.release)
	wg.Wait()
}

// TestBackgroundScheduler_PrincipalShareReleasesOnCompletion pins that a
// principal's share is refunded when its jobs finish: a user capped at one slot
// can schedule again once the prior job drains. A leaked reservation would keep
// the user permanently rejected.
func TestBackgroundScheduler_PrincipalShareReleasesOnCompletion(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	close(repo.release) // jobs complete immediately
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1),
		WithQueueDepth(4), WithPrincipalQueueDepth(1))

	user := shared.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("first schedule = %v, want nil", err)
	}
	wg.Wait() // let the job drain and refund the principal slot

	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("second schedule after drain = %v, want nil (share must be refunded)", err)
	}
	wg.Wait()
}

// TestBackgroundScheduler_RuntimeKillSwitchTogglesAdmission reproduces the
// missing-kill-switch defect: a misbehaving downloader can only be stopped by
// taking the process down, because admission is gated solely on the
// process-lifetime shutdown flag. A runtime pause must refuse new jobs without
// a restart, and a resume must re-admit them — all on the same live scheduler
// instance.
//
// Setup: jobs complete immediately (release closed), so nothing stays in
// flight to confound the admission result. Enabled -> admit, Pause -> refuse
// with ErrAcquisitionPaused, Resume -> admit again. Before the fix there is no
// Pause/Resume control at all and the toggle cannot even be expressed.
func TestBackgroundScheduler_RuntimeKillSwitchTogglesAdmission(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	close(repo.release) // jobs drain immediately; admission is the only variable
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))

	user := shared.NewUserId(uuid.New())

	// Enabled by default: a job admits.
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule while enabled = %v, want nil", err)
	}
	if scheduler.Status().Paused {
		t.Fatal("Status().Paused = true before any Pause, want false")
	}
	wg.Wait()

	// Kill switch flipped off at runtime — no process restart. New jobs refused.
	scheduler.Pause()
	if !scheduler.Status().Paused {
		t.Fatal("Status().Paused = false after Pause, want true")
	}
	err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), "")
	if !errors.Is(err, ErrAcquisitionPaused) {
		t.Fatalf("schedule while paused = %v, want ErrAcquisitionPaused", err)
	}
	if err := scheduler.ScheduleReplace(context.Background(), user, domain.NewTrackId()); !errors.Is(err, ErrAcquisitionPaused) {
		t.Fatalf("replace while paused = %v, want ErrAcquisitionPaused", err)
	}

	// Resumed at runtime: admission returns without a restart.
	scheduler.Resume()
	if scheduler.Status().Paused {
		t.Fatal("Status().Paused = true after Resume, want false")
	}
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule after resume = %v, want nil (kill switch must be reversible)", err)
	}
	wg.Wait()
}

// TestBackgroundScheduler_PauseLeavesInflightRunning pins that pausing is a
// kill switch on *admission* only: a job already in flight when the pause lands
// runs to completion, and no admission or principal reservation is leaked (a
// later resume admits the configured depth again). A pause that stranded the
// in-flight slot would deadlock the queue.
func TestBackgroundScheduler_PauseLeavesInflightRunning(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem, WithQueueDepth(1))

	user := shared.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule = %v, want nil", err)
	}
	<-repo.started // one job in flight, holding the sole admission + worker slot

	// Pause after the job started: it must keep running (below, closing release
	// lets it finish). New arrivals are refused.
	scheduler.Pause()
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); !errors.Is(err, ErrAcquisitionPaused) {
		t.Fatalf("schedule while paused = %v, want ErrAcquisitionPaused", err)
	}

	// Let the in-flight job drain: it must complete despite the pause, refunding
	// its slot rather than leaking it.
	close(repo.release)
	wg.Wait()

	// Resume: the refunded slot is available again, proving nothing leaked.
	scheduler.Resume()
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule after drain+resume = %v, want nil (slot must be refunded)", err)
	}
	wg.Wait()
}

type recordingProgressPublisher struct {
	mu     sync.Mutex
	events []recordedProgress
}

type recordedProgress struct {
	typ     string
	payload map[string]any
}

func (p *recordingProgressPublisher) Publish(_ context.Context, _ shared.UserId, eventType string, payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, recordedProgress{typ: eventType, payload: payload})
}

func TestSchedulerJobReporter_PublishesProgressOnStage(t *testing.T) {
	pub := &recordingProgressPublisher{}
	log := &jobLog{jobs: map[string]*acqports.JobRecord{"t1": {TrackID: "t1"}}}
	r := schedulerJobReporter{log: log, events: pub, trackID: "t1", userId: shared.NewUserId(uuid.New())}

	r.stage("download")

	if len(pub.events) != 1 {
		t.Fatalf("events = %d, want 1", len(pub.events))
	}
	got := pub.events[0]
	if got.typ != "track_acquisition_progress" {
		t.Fatalf("type = %q, want track_acquisition_progress", got.typ)
	}
	if got.payload["track_id"] != "t1" || got.payload["stage"] != "download" {
		t.Fatalf("payload = %v, want track_id=t1 stage=download", got.payload)
	}
}

func TestSchedulerJobReporter_StageWithoutConfiguredEventsDoesNotPanic(t *testing.T) {
	for name, opts := range map[string][]func(*BackgroundAcquisitionScheduler){
		"no events option":  nil,
		"nil events option": {WithSchedulerEvents(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewBackgroundAcquisitionScheduler(nil, &sync.WaitGroup{}, make(chan struct{}, 1), opts...)
			s.log.register("t1", "")
			r := schedulerJobReporter{log: s.log, events: s.events, trackID: "t1", userId: shared.NewUserId(uuid.New())}

			r.stage("search")

			if s.log.jobs["t1"].Stage != "search" {
				t.Fatalf("stage not recorded on job record")
			}
		})
	}
}

// blockingAcquirer holds every Execute/ExecuteReplace call open until release
// closes, so a test can saturate the worker semaphore and observe admission
// past it without a real acquire pipeline behind it.
type blockingAcquirer struct {
	release chan struct{}
	calls   atomic.Int32
}

func (a *blockingAcquirer) Execute(context.Context, shared.UserId, domain.TrackId) error {
	a.calls.Add(1)
	<-a.release
	return nil
}

func (a *blockingAcquirer) ExecuteReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	return a.Execute(ctx, userId, trackId)
}

func (a *blockingAcquirer) RefuseQueued(context.Context, shared.UserId, domain.TrackId) {}

// One user's 6th quick save used to be refused with ErrPrincipalQueueFull
// because the wired default equalled the worker concurrency (#1418). The
// scheduler's own default (no WithPrincipalQueueDepth) must let one principal
// occupy the whole shared admission queue, bounded only by the global depth.
func TestBackgroundScheduler_PrincipalDefault_AdmitsOneUserUpToGlobalDepth(t *testing.T) {
	const concurrency = 2
	acq := &blockingAcquirer{release: make(chan struct{})}
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, sem)
	t.Cleanup(func() {
		close(acq.release)
		wg.Wait()
	})

	userId := shared.NewUserId(uuid.New())
	const globalDepth = concurrency * defaultQueueDepthFactor
	for i := 0; i < globalDepth; i++ {
		if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d of %d for one user = %v, want nil (past concurrency %d, within global depth)", i+1, globalDepth, err, concurrency)
		}
	}

	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); !errors.Is(err, ErrAcquisitionQueueFull) {
		t.Fatalf("schedule past global depth = %v, want ErrAcquisitionQueueFull", err)
	}
}

// recordingPublisher counts every event published, by type, so a test can
// assert an exact number of terminal events rather than "at least one".
type recordingPublisher struct {
	mu     sync.Mutex
	byType map[string]int
	last   map[string]map[string]any
}

func newRecordingPublisher() *recordingPublisher {
	return &recordingPublisher{byType: make(map[string]int), last: make(map[string]map[string]any)}
}

func (p *recordingPublisher) Publish(_ context.Context, _ shared.UserId, eventType string, payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byType[eventType]++
	p.last[eventType] = payload
}

func (p *recordingPublisher) count(eventType string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byType[eventType]
}

func (p *recordingPublisher) payload(eventType string) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last[eventType]
}

// holdOneTrackRepo blocks GetByID for exactly one track id until release
// closes; every other id (and every Update) passes straight through to the
// embedded fake, so a settle on a different track observes a real row.
type holdOneTrackRepo struct {
	*fakeTrackRepository
	hold     domain.TrackId
	holding  chan struct{}
	release  chan struct{}
	holdOnce sync.Once
}

func (r *holdOneTrackRepo) GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	if id == r.hold {
		r.holdOnce.Do(func() { close(r.holding) })
		<-r.release
	}
	return r.fakeTrackRepository.GetByID(ctx, id, userId)
}

func newPendingTrack(t *testing.T, userId shared.UserId, repo *fakeTrackRepository) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if _, err := repo.Add(context.Background(), track); err != nil {
		t.Fatalf("add track: %v", err)
	}
	return track
}

// A job abandoned at the queue-wait deadline must leave the track terminally
// failed, not pending, and tell the client exactly once: otherwise it shows
// "downloading" until the stale-pending sweep reclaims it 15+ minutes later.
func TestBackgroundScheduler_QueueWaitTimeout_SettlesTrackFailedAndPublishesOnce(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	base := newFakeTrackRepository()
	running := newPendingTrack(t, userId, base)
	queued := newPendingTrack(t, userId, base)

	repo := &holdOneTrackRepo{fakeTrackRepository: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	pub := newRecordingPublisher()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithQueueWaitTimeout(testQueueWait))
	t.Cleanup(func() {
		close(repo.release)
		wg.Wait()
	})

	if err := scheduler.Schedule(context.Background(), userId, running.ID, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-repo.holding

	if err := scheduler.Schedule(context.Background(), userId, queued.ID, ""); err != nil {
		t.Fatalf("second Schedule = %v, want nil (admitted, waiting for a slot)", err)
	}

	settled := awaitSettledJob(t, scheduler, queued.ID.String())
	if settled.State != JobCancelled {
		t.Fatalf("queued job state = %q, want %q", settled.State, JobCancelled)
	}

	deadline := time.Now().Add(jobSettleTimeout)
	for time.Now().Before(deadline) && pub.count(events.TypeTrackAcquisitionFailed) == 0 {
		time.Sleep(time.Millisecond)
	}

	stored, ok := base.tracks[queued.ID.String()+":"+userId.String()]
	if !ok {
		t.Fatal("track missing from the repo")
	}
	if stored.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("queued track status = %q, want %q", stored.AcquisitionStatus, domain.AcquisitionFailed)
	}
	if stored.FailureReason == nil || *stored.FailureReason != string(domain.FailureAcquisitionRefused) {
		t.Errorf("queued track failure reason = %v, want %q", stored.FailureReason, domain.FailureAcquisitionRefused)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 1 {
		t.Errorf("track_acquisition_failed publishes = %d, want 1", got)
	}
	if payload := pub.payload(events.TypeTrackAcquisitionFailed); payload["track_id"] != queued.ID.String() {
		t.Errorf("failed event track_id = %v, want %q", payload["track_id"], queued.ID.String())
	}
}

// Shutdown cancellation must never settle the track: the sweep reclaims a
// genuinely orphaned pending row after restart, and a settle write racing the
// process going down is exactly what would corrupt that handoff.
func TestBackgroundScheduler_ShutdownCancellation_DoesNotSettleTheTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	base := newFakeTrackRepository()
	running := newPendingTrack(t, userId, base)
	queued := newPendingTrack(t, userId, base)

	repo := &holdOneTrackRepo{fakeTrackRepository: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	pub := newRecordingPublisher()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))
	t.Cleanup(func() {
		close(repo.release)
		wg.Wait()
	})

	if err := scheduler.Schedule(context.Background(), userId, running.ID, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-repo.holding

	if err := scheduler.Schedule(context.Background(), userId, queued.ID, ""); err != nil {
		t.Fatalf("second Schedule = %v, want nil (admitted, waiting for a slot)", err)
	}

	// Shutdown cancels baseCtx immediately (before it drains); the running job
	// stays blocked on repo.holding, so bound the drain wait short rather than
	// let it block the test until t.Cleanup releases it.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shutdownCancel()
	scheduler.Shutdown(shutdownCtx)

	settled := awaitSettledJob(t, scheduler, queued.ID.String())
	if settled.State != JobCancelled {
		t.Fatalf("queued job state = %q, want %q", settled.State, JobCancelled)
	}
	if settled.Reason != "" {
		t.Errorf("queued job reason = %q, want empty (shutdown, not queue_wait_timeout)", settled.Reason)
	}

	stored, ok := base.tracks[queued.ID.String()+":"+userId.String()]
	if !ok {
		t.Fatal("track missing from the repo")
	}
	if stored.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("queued track status = %q, want %q (untouched by shutdown)", stored.AcquisitionStatus, domain.AcquisitionPending)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0", got)
	}
}

// A queue-wait deadline that fires once Shutdown has begun must not settle the
// track either: Shutdown marks the scheduler closed before cancelling baseCtx,
// so the timer can win the select against a cancellation already under way.
func TestBackgroundScheduler_QueueWaitTimeoutDuringShutdown_DoesNotSettleTheTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	base := newFakeTrackRepository()
	running := newPendingTrack(t, userId, base)
	queued := newPendingTrack(t, userId, base)

	repo := &holdOneTrackRepo{fakeTrackRepository: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	pub := newRecordingPublisher()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithQueueWaitTimeout(testQueueWait))
	t.Cleanup(func() {
		close(repo.release)
		wg.Wait()
	})

	if err := scheduler.Schedule(context.Background(), userId, running.ID, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-repo.holding

	if err := scheduler.Schedule(context.Background(), userId, queued.ID, ""); err != nil {
		t.Fatalf("second Schedule = %v, want nil (admitted, waiting for a slot)", err)
	}
	scheduler.closed.Store(true)

	settled := awaitSettledJob(t, scheduler, queued.ID.String())
	if settled.State != JobCancelled {
		t.Fatalf("queued job state = %q, want %q", settled.State, JobCancelled)
	}

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) && pub.count(events.TypeTrackAcquisitionFailed) == 0 {
		time.Sleep(time.Millisecond)
	}

	stored, ok := base.tracks[queued.ID.String()+":"+userId.String()]
	if !ok {
		t.Fatal("track missing from the repo")
	}
	if stored.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("queued track status = %q, want %q (untouched once shutdown began)", stored.AcquisitionStatus, domain.AcquisitionPending)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0", got)
	}
}

// A queue-wait timeout landing after another path already settled the track
// (a race with the stale-pending sweep, or a concurrent success) must not
// clobber the winner: RefuseQueued's CAS conflict is a quiet no-op.
func TestAcquireTrackAudioService_RefuseQueued_AlreadySettledTrack_IsQuietNoOp(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkReady("audio/ref.mp3"); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	if _, err := repo.Add(context.Background(), track); err != nil {
		t.Fatalf("add track: %v", err)
	}

	pub := newRecordingPublisher()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))

	svc.RefuseQueued(context.Background(), userId, track.ID)

	stored, ok := repo.tracks[track.ID.String()+":"+userId.String()]
	if !ok {
		t.Fatal("track missing from the repo")
	}
	if stored.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("already-ready track status = %q, want %q (untouched)", stored.AcquisitionStatus, domain.AcquisitionReady)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0 (quiet no-op)", got)
	}
}

// A replace targets a track that already has working ready audio; the
// queue-wait timeout must never fail it, since the audio and status a replace
// leaves behind belong to the acquisition that put them there, not to this
// job that never ran.
func TestBackgroundScheduler_QueueWaitTimeout_ReplaceNeverFailsTheReadyTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	base := newFakeTrackRepository()
	running := newPendingTrack(t, userId, base)
	ready, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := ready.MarkReady("audio/ref.mp3"); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	if _, err := base.Add(context.Background(), ready); err != nil {
		t.Fatalf("add track: %v", err)
	}

	repo := &holdOneTrackRepo{fakeTrackRepository: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	pub := newRecordingPublisher()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithQueueWaitTimeout(testQueueWait), WithSchedulerEvents(pub))
	t.Cleanup(func() {
		close(repo.release)
		wg.Wait()
	})

	if err := scheduler.Schedule(context.Background(), userId, running.ID, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-repo.holding

	if err := scheduler.ScheduleReplace(context.Background(), userId, ready.ID); err != nil {
		t.Fatalf("ScheduleReplace = %v, want nil (admitted, waiting for a slot)", err)
	}

	settled := awaitSettledJob(t, scheduler, ready.ID.String())
	if settled.State != JobCancelled {
		t.Fatalf("queued replace job state = %q, want %q", settled.State, JobCancelled)
	}

	deadline := time.Now().Add(jobSettleTimeout)
	for time.Now().Before(deadline) && pub.count(events.TypeTrackReplaceFailed) == 0 {
		time.Sleep(time.Millisecond)
	}
	if got := pub.count(events.TypeTrackReplaceFailed); got != 1 {
		t.Errorf("track_replace_failed publishes = %d, want 1", got)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0 (a replace never fails the ready track)", got)
	}

	stored, ok := base.tracks[ready.ID.String()+":"+userId.String()]
	if !ok {
		t.Fatal("track missing from the repo")
	}
	if stored.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("ready track status = %q, want %q (untouched by the abandoned replace)", stored.AcquisitionStatus, domain.AcquisitionReady)
	}
}

// defaultQueueWaitTimeout must stay comfortably under the stale-pending grace
// (internal/catalog/service.DefaultStalePendingGrace, 15m) so a queued job
// always settles itself well before the sweep would otherwise reclaim it.
func TestDefaultQueueWaitTimeout_FitsInsideStalePendingGrace(t *testing.T) {
	const catalogStalePendingGrace = 15 * time.Minute
	if defaultQueueWaitTimeout >= catalogStalePendingGrace {
		t.Fatalf("defaultQueueWaitTimeout = %s, want less than the stale-pending grace %s", defaultQueueWaitTimeout, catalogStalePendingGrace)
	}
}

// scheduleQueued stands in for a scheduler that accepted the job.
func scheduleQueued() error { return nil }

// saturatedScheduler returns a scheduler (queue depth 1) whose only slot is held
// by a blocked job, plus a drain func that unblocks it and waits for it to exit.
func saturatedScheduler(t *testing.T) (*BackgroundAcquisitionScheduler, func()) {
	t.Helper()
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithQueueDepth(1))

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("filling Schedule = %v, want nil", err)
	}
	<-repo.started
	return scheduler, func() {
		close(repo.release)
		wg.Wait()
	}
}

func TestBackgroundScheduler_QueueFullIsObservable(t *testing.T) {
	scheduler, drain := saturatedScheduler(t)
	userId := shared.NewUserId(uuid.New())

	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); !errors.Is(err, ErrAcquisitionQueueFull) {
		t.Errorf("Schedule on full queue = %v, want ErrAcquisitionQueueFull", err)
	}
	if err := scheduler.ScheduleReplace(context.Background(), userId, domain.NewTrackId()); !errors.Is(err, ErrAcquisitionQueueFull) {
		t.Errorf("ScheduleReplace on full queue = %v, want ErrAcquisitionQueueFull", err)
	}
	drain()

	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
		t.Errorf("Schedule after drain = %v, want nil (admitted)", err)
	}
}

func TestBackgroundScheduler_InflightDedupIsNotARefusal(t *testing.T) {
	repo := &blockingRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))
	userId, trackId := shared.NewUserId(uuid.New()), domain.NewTrackId()

	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-repo.started
	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err != nil {
		t.Errorf("duplicate Schedule while in flight = %v, want nil (a job is already running)", err)
	}
	close(repo.release)
	wg.Wait()
}

func TestBackgroundScheduler_AfterShutdownRefuses(t *testing.T) {
	svc := NewAcquireTrackAudioService(&countingRepo{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))
	scheduler.Shutdown(context.Background())

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); !errors.Is(err, ErrSchedulerShutdown) {
		t.Errorf("Schedule after shutdown = %v, want ErrSchedulerShutdown", err)
	}
}

// TestRetryAdmission_QueueFullDoesNotBurnCooldown drives the real admission
// channel full: the rejection reaches the caller, the retry cooldown stays
// unburned, and the still-failed track is retryable as soon as the queue drains.
func TestRetryAdmission_QueueFullDoesNotBurnCooldown(t *testing.T) {
	scheduler, drain := saturatedScheduler(t)
	admission := NewRetryAdmission(newFakeCooldownStore())
	track := failedTrack(t)
	schedule := func() error { return scheduler.Schedule(context.Background(), track.UserId, track.ID, "") }

	if err := admission.Admit(context.Background(), track, schedule); !errors.Is(err, ErrAcquisitionQueueFull) {
		t.Fatalf("Admit on full queue = %v, want ErrAcquisitionQueueFull", err)
	}
	if track.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("status = %v, want still failed (retryable)", track.AcquisitionStatus)
	}
	drain()

	if err := admission.Admit(context.Background(), track, schedule); err != nil {
		t.Errorf("Admit after drain = %v, want nil (cooldown must not be burned by the shed job)", err)
	}
}

func TestReacquireAdmission_QueueFullDoesNotBurnCooldown(t *testing.T) {
	scheduler, drain := saturatedScheduler(t)
	admission := NewReacquireAdmission(newFakeCooldownStore())
	track := readyTrack(t)
	schedule := func() error { return scheduler.ScheduleReplace(context.Background(), track.UserId, track.ID) }

	if err := admission.Admit(context.Background(), track, schedule); !errors.Is(err, ErrAcquisitionQueueFull) {
		t.Fatalf("Admit on full queue = %v, want ErrAcquisitionQueueFull", err)
	}
	drain()

	if err := admission.Admit(context.Background(), track, schedule); err != nil {
		t.Errorf("Admit after drain = %v, want nil (cooldown must not be burned by the shed job)", err)
	}
}

func TestAdmission_ScheduleSkippedWhenNotAdmitted(t *testing.T) {
	called := false
	schedule := func() error { called = true; return nil }

	if err := NewRetryAdmission(newFakeCooldownStore()).Admit(context.Background(), readyTrack(t), schedule); !errors.Is(err, ErrRetryNotFailed) {
		t.Fatalf("Admit = %v, want ErrRetryNotFailed", err)
	}
	a := NewRetryAdmission(newFakeCooldownStore())
	track := failedTrack(t)
	if err := a.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	if err := a.Admit(context.Background(), track, schedule); !errors.Is(err, ErrCooldownActive) {
		t.Fatalf("second Admit = %v, want ErrCooldownActive", err)
	}
	if called {
		t.Error("schedule ran for a request admission refused")
	}
}

func newStatusTestScheduler() *BackgroundAcquisitionScheduler {
	var wg sync.WaitGroup
	return NewBackgroundAcquisitionScheduler(nil, &wg, make(chan struct{}, 1))
}

func TestAcquisitionStatus_Counts(t *testing.T) {
	s := newStatusTestScheduler()

	s.inflightCount.Add(2)
	for i := 0; i < 5; i++ {
		s.log.complete("succeeded-track", "succeeded", "")
	}
	s.log.complete("track-1", "failed", "yt-dlp exited 1")

	got := s.Status()
	if got.InFlight != 2 {
		t.Errorf("in_flight = %d, want 2", got.InFlight)
	}
	if got.Succeeded != 5 {
		t.Errorf("succeeded = %d, want 5", got.Succeeded)
	}
	if got.Failed != 1 {
		t.Errorf("failed = %d, want 1", got.Failed)
	}
	if len(got.Recent) != 6 || got.Recent[0].TrackID != "track-1" ||
		got.Recent[0].State != "failed" || got.Recent[0].Reason != "yt-dlp exited 1" {
		t.Errorf("recent = %+v, want 6 entries, newest a failed entry for track-1", got.Recent)
	}
}

func TestAcquisitionStatus_RecentBounded(t *testing.T) {
	s := newStatusTestScheduler()
	for i := 0; i < recentJobCap+10; i++ {
		s.log.complete("t", "failed", "boom")
	}
	if got := len(s.Status().Recent); got != recentJobCap {
		t.Errorf("recent = %d, want capped at %d", got, recentJobCap)
	}
}

func TestAcquisitionStatus_SnapshotIsACopy(t *testing.T) {
	s := newStatusTestScheduler()
	s.log.complete("t", "failed", "boom")
	snap := s.Status()
	snap.Recent[0].Reason = "mutated"
	if s.Status().Recent[0].Reason != "boom" {
		t.Error("Status() returned a shared slice; callers can corrupt internal state")
	}
}

// TestAcquisitionVerification_FullyArmed_RequiresYtDlp reproduces the defect:
// when yt-dlp is unavailable but ffprobe/ffmpeg/fpcalc are present, the
// verification must report as degraded so WithVerificationStatus logs the
// startup warning. Before yt-dlp joined the verification, FullyArmed reported
// armed and no warning ever fired.
func TestAcquisitionVerification_FullyArmed_RequiresYtDlp(t *testing.T) {
	armed := acqports.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true, Streamrip: true}
	if !armed.FullyArmed() {
		t.Error("FullyArmed() = false with every tool present, want true")
	}

	missingYtDlp := acqports.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, Streamrip: true}
	if missingYtDlp.FullyArmed() {
		t.Error("FullyArmed() = true with yt-dlp unavailable; the startup warning never fires (defect)")
	}
}

// TestAcquisitionVerification_FullyArmed_RequiresStreamrip: a configured but
// unrunnable streamrip binary must report degraded, like every other tool.
func TestAcquisitionVerification_FullyArmed_RequiresStreamrip(t *testing.T) {
	missingStreamrip := acqports.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}
	if missingStreamrip.FullyArmed() {
		t.Error("FullyArmed() = true with streamrip unavailable, want degraded")
	}
}
