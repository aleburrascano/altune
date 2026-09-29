package service

import (
	acqports "altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"bytes"
	"context"
	"errors"
	"log/slog"
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
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

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
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

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

func TestBackgroundScheduler_ShutdownMidSearch_ReleasesTheJobForAnotherWorker(t *testing.T) {
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
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithDrainBudget(10*time.Millisecond))
	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	<-source.searching

	scheduler.Shutdown(context.Background())

	settled := repo.tracks[key]
	if settled.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("status = %v, want %v (released for another worker)", settled.AcquisitionStatus, domain.AcquisitionPending)
	}
	if settled.FailureReason != nil {
		t.Errorf("persisted failure_reason = %q, want nil", deref(settled.FailureReason))
	}
}

func TestBackgroundScheduler_ShutdownMidSearch_ReleasesTheReplaceJobForAnotherWorker(t *testing.T) {
	logs := captureJSONLog(t)
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
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithDrainBudget(10*time.Millisecond))
	if err := scheduler.ScheduleReplace(context.Background(), userId, track.ID); err != nil {
		t.Fatalf("schedule replace: %v", err)
	}
	<-source.searching

	scheduler.Shutdown(context.Background())

	settled := repo.tracks[key]
	if settled.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("status = %v, want %v (released for another worker)", settled.AcquisitionStatus, domain.AcquisitionPending)
	}
	if settled.FailureReason != nil {
		t.Errorf("persisted failure_reason = %q, want nil", deref(settled.FailureReason))
	}
	if findLogRecord(t, logs, "track_acquisition_cancelled") == nil {
		t.Error("want a track_acquisition_cancelled log for the scheduler-cancelled replace, got none")
	}
}

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
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

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
	racingSchedulers     = 8
	shutdownRaceAttempts = 300
)

func TestBackgroundScheduler_ScheduleRacingShutdown_NeverLosesAnAcceptedJob(t *testing.T) {
	for attempt := 0; attempt < shutdownRaceAttempts; attempt++ {
		accepted, settled, remaining := scheduleWhileShuttingDown()

		for _, id := range accepted {
			_, isSettled := settled[id]
			_, isRemaining := remaining[id]
			if isSettled == isRemaining {
				t.Fatalf("attempt %d: track %s settled=%v, still queued=%v, want exactly one true (never both, never neither)",
					attempt, id, isSettled, isRemaining)
			}
		}
	}
}

func scheduleWhileShuttingDown() (accepted []string, settled map[string]acqports.JobRecord, remaining map[string]struct{}) {
	svc := NewAcquireTrackAudioService(&countingRepo{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var jobs sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &jobs, make(chan struct{}, 4))
	userId := shared.NewUserId(uuid.New())

	var mu sync.Mutex
	start := make(chan struct{})
	var callers sync.WaitGroup
	for i := 0; i < racingSchedulers; i++ {
		trackId := domain.NewTrackId()
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err == nil {
				mu.Lock()
				accepted = append(accepted, trackId.String())
				mu.Unlock()
			}
		}()
	}
	drained := make(chan struct{})
	var recent []acqports.JobRecord
	go func() {
		defer close(drained)
		<-start
		scheduler.Shutdown(context.Background())
		_, recent = scheduler.log.snapshot()
	}()

	close(start)
	<-drained
	callers.Wait()
	jobs.Wait()

	settled = make(map[string]acqports.JobRecord, len(recent))
	for _, job := range recent {
		settled[job.TrackID] = job
	}
	remaining = make(map[string]struct{})
	if mq, ok := scheduler.queue.(*memJobQueue); ok {
		mq.mu.Lock()
		for id := range mq.jobs {
			remaining[id.String()] = struct{}{}
		}
		mq.mu.Unlock()
	}
	return accepted, settled, remaining
}

const jobSettleTimeout = 2 * time.Second

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

func TestBackgroundScheduler_QueuedJobWaitsForAWorkerSlotAndRuns(t *testing.T) {
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	userId := shared.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-acq.started
	queued := domain.NewTrackId()

	if err := scheduler.Schedule(context.Background(), userId, queued, ""); err != nil {
		t.Fatalf("second Schedule = %v, want nil (enqueued, waiting for a worker)", err)
	}

	if _, settled := settledJob(scheduler, queued.String()); settled {
		t.Fatal("queued job settled before the running job released its worker, want it still waiting")
	}

	close(acq.release)
	settled := awaitSettledJob(t, scheduler, queued.String())
	if settled.State != JobSucceeded {
		t.Errorf("queued job state = %q, want %q (it must run once a worker frees up)", settled.State, JobSucceeded)
	}
	if got := acq.calls.Load(); got != 2 {
		t.Errorf("acquirer executions = %d, want 2 (both jobs eventually run)", got)
	}
	wg.Wait()
}

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

func trackHeldByJob(t *testing.T, hold func(*BackgroundAcquisitionScheduler) error) (*BackgroundAcquisitionScheduler, func()) {
	t.Helper()
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 2))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
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
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	if scheduler == nil {
		t.Fatal("NewBackgroundAcquisitionScheduler returned nil")
	}
}

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
			t.Cleanup(func() {
				scheduler.Shutdown(context.Background())
			})

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

func TestBackgroundScheduler_BoundsQueueDepthUnderBurst(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	userId := shared.NewUserId(uuid.New())
	const burst = 60
	for i := 0; i < burst; i++ {
		scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), "")
	}

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

func TestBackgroundScheduler_StatusQueueDrainsAndCountsShutdownRejections(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	close(repo.release)
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	userId := shared.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	wg.Wait()

	status := scheduler.Status()
	if status.QueueDepth != 0 {
		t.Errorf("queue depth after drain = %d, want 0", status.QueueDepth)
	}
	if status.QueueCapacity != cap(sem) {
		t.Errorf("queue capacity = %d, want %d (worker count)", status.QueueCapacity, cap(sem))
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

func TestBackgroundScheduler_ThreadsCorrelationIDIntoJobContext(t *testing.T) {
	repo := &corrCapturingRepo{fakeTrackRepository: newFakeTrackRepository()}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

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
			t.Cleanup(func() {
				scheduler.Shutdown(context.Background())
			})
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
	started     chan struct{}
	startedOnce sync.Once
	release     chan struct{}
	calls       atomic.Int32
}

func (r *blockingRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	r.calls.Add(1)
	r.startedOnce.Do(func() { close(r.started) })
	<-r.release
	return nil, nil
}
func (r *blockingRepo) Update(_ context.Context, _ *domain.Track, _ int) error { return nil }

func (r *blockingRepo) AudioRefInUse(_ context.Context, _ string, _ domain.TrackId) (bool, error) {
	return false, nil
}

func TestBackgroundScheduler_Schedule_DedupsConcurrentRun_ButReplaysAfterSettle(t *testing.T) {
	repo := &blockingRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	scheduler.Schedule(context.Background(), userId, trackId, "")
	<-repo.started
	scheduler.Schedule(context.Background(), userId, trackId, "")

	if got := repo.calls.Load(); got != 1 {
		t.Fatalf("GetByID calls while the first run is still in flight = %d, want 1 (no concurrent second run)", got)
	}

	close(repo.release)

	deadline := time.Now().Add(jobSettleTimeout)
	for repo.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	wg.Wait()

	if got := repo.calls.Load(); got != 2 {
		t.Errorf("GetByID calls = %d, want 2 (the deduped request replays once the first run settles)", got)
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
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), "")
	wg.Wait()
}

type panickingAcquirer struct {
	runs    atomic.Int32
	refused atomic.Int32
}

func (a *panickingAcquirer) Execute(context.Context, shared.UserId, domain.TrackId) error {
	a.runs.Add(1)
	panic("boom")
}

func (a *panickingAcquirer) ExecuteReplace(ctx context.Context, u shared.UserId, t domain.TrackId) error {
	return a.Execute(ctx, u, t)
}

func (a *panickingAcquirer) RefuseQueued(context.Context, shared.UserId, domain.TrackId) {
	a.refused.Add(1)
}

type settleKeepsPendingQueue struct{ *memJobQueue }

func (q settleKeepsPendingQueue) Settle(ctx context.Context, trackID domain.TrackId, fence int) error {
	return q.Release(ctx, trackID, fence, time.Now())
}

func TestBackgroundScheduler_Schedule_PanickingJobIsNotReclaimedWithinAPoll(t *testing.T) {
	acq := &panickingAcquirer{}
	var wg sync.WaitGroup
	wake := make(chan struct{}, 1)
	queue := settleKeepsPendingQueue{newMemJobQueue(wake)}
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1),
		WithJobQueue(queue), WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	if got := acq.runs.Load(); got != 1 {
		t.Errorf("runs = %d, want 1 (a panicked job must back off, not be re-claimed on the next poll)", got)
	}
}

func TestBackgroundScheduler_Schedule_PanickingJobAtTheAttemptCapFailsTheTrack(t *testing.T) {
	acq := &panickingAcquirer{}
	var wg sync.WaitGroup
	queue := settleKeepsPendingQueue{newMemJobQueue(make(chan struct{}, 1))}
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1), WithJobQueue(queue))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	job := acqports.Job{TrackID: domain.NewTrackId(), UserID: shared.NewUserId(uuid.New()), Attempts: maxAcquisitionAttempts}

	scheduler.releasePanickedJob(job)

	if got := acq.refused.Load(); got != 1 {
		t.Errorf("track failed %d times, want 1", got)
	}
}

func TestBackgroundScheduler_OnePrincipalCannotStarveAnother(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	userA := shared.NewUserId(uuid.New())
	userB := shared.NewUserId(uuid.New())

	const flood = 10
	for i := 0; i < flood; i++ {
		if err := scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), ""); err != nil {
			t.Fatalf("user A schedule #%d = %v, want nil (the queue is unbounded)", i+1, err)
		}
	}

	<-repo.started

	if err := scheduler.Schedule(context.Background(), userB, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("user B schedule while A floods = %v, want nil (A must not starve B)", err)
	}

	close(repo.release)
	wg.Wait()
}

func TestBackgroundScheduler_RuntimeKillSwitchTogglesAdmission(t *testing.T) {
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1), WithPollInterval(10*time.Millisecond))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	user := shared.NewUserId(uuid.New())

	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule while enabled = %v, want nil", err)
	}
	<-acq.started
	if scheduler.Status().Paused {
		t.Fatal("Status().Paused = true before any Pause, want false")
	}
	close(acq.release)
	wg.Wait()

	scheduler.Pause()
	if !scheduler.Status().Paused {
		t.Fatal("Status().Paused = false after Pause, want true")
	}
	paused := domain.NewTrackId()
	if err := scheduler.Schedule(context.Background(), user, paused, ""); err != nil {
		t.Fatalf("schedule while paused = %v, want nil (paused stops claiming, not enqueuing)", err)
	}
	if err := scheduler.ScheduleReplace(context.Background(), user, domain.NewTrackId()); err != nil {
		t.Fatalf("replace while paused = %v, want nil (paused stops claiming, not enqueuing)", err)
	}

	time.Sleep(30 * time.Millisecond)
	if _, settled := settledJob(scheduler, paused.String()); settled {
		t.Fatal("paused job already settled, want it left unclaimed until Resume")
	}
	if got := acq.calls.Load(); got != 1 {
		t.Fatalf("acquirer executions while paused = %d, want 1 (only the earlier, already-finished job)", got)
	}

	scheduler.Resume()
	if scheduler.Status().Paused {
		t.Fatal("Status().Paused = true after Resume, want false")
	}
	settled := awaitSettledJob(t, scheduler, paused.String())
	if settled.State != JobSucceeded {
		t.Errorf("resumed job state = %q, want %q (kill switch must be reversible)", settled.State, JobSucceeded)
	}
}

func TestBackgroundScheduler_ResumeClaimsWithoutWaitingAPollInterval(t *testing.T) {
	acq := &startedAcquirer{started: make(chan struct{}, 1), release: make(chan struct{})}
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1), WithPollInterval(10*time.Second))
	t.Cleanup(func() {
		close(acq.release)
		scheduler.Shutdown(context.Background())
	})

	scheduler.Pause()
	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule while paused = %v, want nil", err)
	}
	time.Sleep(50 * time.Millisecond)

	scheduler.Resume()
	select {
	case <-acq.started:
	case <-time.After(time.Second):
		t.Fatal("acquirer not started within 1s of Resume, want a wake well under the 10s poll interval")
	}
}

func TestBackgroundScheduler_PauseLeavesInflightRunning(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem, WithPollInterval(10*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	user := shared.NewUserId(uuid.New())
	running := domain.NewTrackId()
	if err := scheduler.Schedule(context.Background(), user, running, ""); err != nil {
		t.Fatalf("schedule = %v, want nil", err)
	}
	<-repo.started

	scheduler.Pause()
	queued := domain.NewTrackId()
	if err := scheduler.Schedule(context.Background(), user, queued, ""); err != nil {
		t.Fatalf("schedule while paused = %v, want nil (enqueued, not claimed)", err)
	}

	close(repo.release)
	_ = awaitSettledJob(t, scheduler, running.String())
	time.Sleep(30 * time.Millisecond)
	if _, settled := settledJob(scheduler, queued.String()); settled {
		t.Fatal("job scheduled while paused already settled, want it left unclaimed until Resume")
	}

	scheduler.Resume()
	settled := awaitSettledJob(t, scheduler, queued.String())
	if settled.State != JobSucceeded {
		t.Errorf("resumed job state = %q, want %q", settled.State, JobSucceeded)
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
			t.Cleanup(func() {
				s.Shutdown(context.Background())
			})
			s.log.register("t1", "")
			r := schedulerJobReporter{log: s.log, events: s.events, trackID: "t1", userId: shared.NewUserId(uuid.New())}

			r.stage("search")

			if s.log.jobs["t1"].Stage != "search" {
				t.Fatalf("stage not recorded on job record")
			}
		})
	}
}

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

func TestBackgroundScheduler_PrincipalDefault_AdmitsOneUserUpToGlobalDepth(t *testing.T) {
	const concurrency = 2
	acq := &blockingAcquirer{release: make(chan struct{})}
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, sem)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	t.Cleanup(func() {
		close(acq.release)
		wg.Wait()
	})

	userId := shared.NewUserId(uuid.New())
	const burst = concurrency*4 + 5
	for i := 0; i < burst; i++ {
		if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d of %d for one user = %v, want nil (the queue is unbounded)", i+1, burst, err)
		}
	}
}

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

func TestBackgroundScheduler_ShutdownCancellation_LeavesTheQueuedJobUnclaimed(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	base := newFakeTrackRepository()
	running := newPendingTrack(t, userId, base)
	queued := newPendingTrack(t, userId, base)

	repo := &holdOneTrackRepo{fakeTrackRepository: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	pub := newRecordingPublisher()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithDrainBudget(10*time.Millisecond))
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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shutdownCancel()
	scheduler.Shutdown(shutdownCtx)

	if _, settled := settledJob(scheduler, queued.ID.String()); settled {
		t.Fatal("queued job was claimed by shutdown, want left untouched in the queue")
	}

	mq, ok := scheduler.queue.(*memJobQueue)
	if !ok {
		t.Fatal("scheduler queue is not a *memJobQueue")
	}
	if _, ok := mq.jobs[queued.ID]; !ok {
		t.Error("queued job missing from the queue after shutdown, want still enqueued for the next worker")
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

func TestBackgroundScheduler_ScheduleDuringShutdown_Refuses(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	base := newFakeTrackRepository()
	running := newPendingTrack(t, userId, base)
	queued := newPendingTrack(t, userId, base)

	repo := &holdOneTrackRepo{fakeTrackRepository: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	pub := newRecordingPublisher()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithDrainBudget(10*time.Millisecond))
	t.Cleanup(func() {
		close(repo.release)
		wg.Wait()
	})

	if err := scheduler.Schedule(context.Background(), userId, running.ID, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-repo.holding
	scheduler.closed.Store(true)

	if err := scheduler.Schedule(context.Background(), userId, queued.ID, ""); !errors.Is(err, ErrSchedulerShutdown) {
		t.Errorf("Schedule once admission is closed = %v, want ErrSchedulerShutdown", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shutdownCancel()
	scheduler.Shutdown(shutdownCtx)

	stored, ok := base.tracks[queued.ID.String()+":"+userId.String()]
	if !ok {
		t.Fatal("track missing from the repo")
	}
	if stored.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("refused track status = %q, want %q (untouched)", stored.AcquisitionStatus, domain.AcquisitionPending)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0", got)
	}
}

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

func scheduleQueued() error { return nil }

func TestBackgroundScheduler_InflightDedupIsNotARefusal(t *testing.T) {
	repo := &blockingRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
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

func newStatusTestScheduler(t *testing.T) *BackgroundAcquisitionScheduler {
	t.Helper()
	var wg sync.WaitGroup
	s := NewBackgroundAcquisitionScheduler(nil, &wg, make(chan struct{}, 1))
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	return s
}

func TestAcquisitionStatus_Counts(t *testing.T) {
	s := newStatusTestScheduler(t)

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
	s := newStatusTestScheduler(t)
	for i := 0; i < recentJobCap+10; i++ {
		s.log.complete("t", "failed", "boom")
	}
	if got := len(s.Status().Recent); got != recentJobCap {
		t.Errorf("recent = %d, want capped at %d", got, recentJobCap)
	}
}

func TestAcquisitionStatus_SnapshotIsACopy(t *testing.T) {
	s := newStatusTestScheduler(t)
	s.log.complete("t", "failed", "boom")
	snap := s.Status()
	snap.Recent[0].Reason = "mutated"
	if s.Status().Recent[0].Reason != "boom" {
		t.Error("Status() returned a shared slice; callers can corrupt internal state")
	}
}

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

func TestAcquisitionVerification_FullyArmed_RequiresStreamrip(t *testing.T) {
	missingStreamrip := acqports.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}
	if missingStreamrip.FullyArmed() {
		t.Error("FullyArmed() = true with streamrip unavailable, want degraded")
	}
}

type fakeOutcomeRecorder struct {
	calls     chan acqports.AcquisitionOutcome
	deadlines chan time.Duration
	err       error
	delay     time.Duration
}

func newFakeOutcomeRecorder() *fakeOutcomeRecorder {
	return &fakeOutcomeRecorder{
		calls:     make(chan acqports.AcquisitionOutcome, 8),
		deadlines: make(chan time.Duration, 8),
	}
}

func (r *fakeOutcomeRecorder) Record(ctx context.Context, o acqports.AcquisitionOutcome) error {
	if deadline, ok := ctx.Deadline(); ok {
		r.deadlines <- time.Until(deadline)
	}
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			r.calls <- o
			return ctx.Err()
		}
	}
	r.calls <- o
	return r.err
}

func awaitOutcome(t *testing.T, calls chan acqports.AcquisitionOutcome) acqports.AcquisitionOutcome {
	t.Helper()
	select {
	case o := <-calls:
		return o
	case <-time.After(jobSettleTimeout):
		t.Fatal("outcome recorder never received a Record call")
		return acqports.AcquisitionOutcome{}
	}
}

func assertExactlyOneOutcome(t *testing.T, calls chan acqports.AcquisitionOutcome) acqports.AcquisitionOutcome {
	t.Helper()
	first := awaitOutcome(t, calls)
	select {
	case extra := <-calls:
		t.Fatalf("recorder received a second Record call %+v, want exactly one", extra)
	case <-time.After(50 * time.Millisecond):
	}
	return first
}

func TestBackgroundScheduler_RecordsOutcome_OnJobCompletion(t *testing.T) {
	t.Run("succeeded", func(t *testing.T) {
		recorder := newFakeOutcomeRecorder()
		svc := NewAcquireTrackAudioService(newFakeTrackRepository(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		var wg sync.WaitGroup
		scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		trackId := domain.NewTrackId()

		if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), trackId, ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}

		outcome := assertExactlyOneOutcome(t, recorder.calls)
		wg.Wait()
		if outcome.TrackID != trackId.String() {
			t.Errorf("track id = %q, want %q", outcome.TrackID, trackId.String())
		}
		if outcome.Outcome != JobSucceeded {
			t.Errorf("outcome = %q, want %q", outcome.Outcome, JobSucceeded)
		}
		if outcome.ElapsedMs < 0 {
			t.Errorf("elapsed ms = %d, want >= 0", outcome.ElapsedMs)
		}
	})

	t.Run("failed", func(t *testing.T) {
		recorder := newFakeOutcomeRecorder()
		userId := shared.NewUserId(uuid.New())
		track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
		if err != nil {
			t.Fatalf("new track: %v", err)
		}
		repo := newFakeTrackRepository()
		repo.tracks[track.ID.String()+":"+userId.String()] = track
		leaking := &fakeAudioSearcher{searchErr: errors.New("boom")}
		svc := NewAcquireTrackAudioService(repo, fakeRegistry(leaking), newFakeAudioStore())
		var wg sync.WaitGroup
		scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})

		if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}

		outcome := assertExactlyOneOutcome(t, recorder.calls)
		wg.Wait()
		if outcome.TrackID != track.ID.String() {
			t.Errorf("track id = %q, want %q", outcome.TrackID, track.ID.String())
		}
		if outcome.Outcome != JobFailed {
			t.Errorf("outcome = %q, want %q", outcome.Outcome, JobFailed)
		}
		if outcome.Reason == "" {
			t.Error("reason is empty, want the acquisition failure reason")
		}
		if outcome.ElapsedMs < 0 {
			t.Errorf("elapsed ms = %d, want >= 0", outcome.ElapsedMs)
		}
	})

	t.Run("panicked", func(t *testing.T) {
		recorder := newFakeOutcomeRecorder()
		svc := NewAcquireTrackAudioService(&panicRepo{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		var wg sync.WaitGroup
		scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		trackId := domain.NewTrackId()

		if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), trackId, ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}

		outcome := assertExactlyOneOutcome(t, recorder.calls)
		wg.Wait()
		if outcome.Outcome != JobFailed {
			t.Errorf("outcome = %q, want %q", outcome.Outcome, JobFailed)
		}
		if outcome.Reason != "panic" {
			t.Errorf("reason = %q, want %q", outcome.Reason, "panic")
		}
		if outcome.ElapsedMs < 0 {
			t.Errorf("elapsed ms = %d, want >= 0", outcome.ElapsedMs)
		}
	})

	t.Run("queued job runs and records its own outcome once a worker frees up", func(t *testing.T) {
		recorder := newFakeOutcomeRecorder()
		acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
		var wg sync.WaitGroup
		scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		userId := shared.NewUserId(uuid.New())
		running := domain.NewTrackId()
		if err := scheduler.Schedule(context.Background(), userId, running, ""); err != nil {
			t.Fatalf("first schedule: %v", err)
		}
		<-acq.started
		queued := domain.NewTrackId()

		if err := scheduler.Schedule(context.Background(), userId, queued, ""); err != nil {
			t.Fatalf("second schedule: %v", err)
		}

		if got := len(recorder.calls); got != 0 {
			t.Fatalf("recorded outcomes while the queued job still waits for a slot = %d, want 0", got)
		}
		close(acq.release)

		byTrackID := make(map[string]acqports.AcquisitionOutcome, 2)
		for i := 0; i < 2; i++ {
			o := awaitOutcome(t, recorder.calls)
			byTrackID[o.TrackID] = o
		}
		wg.Wait()

		runningOutcome, ok := byTrackID[running.String()]
		if !ok {
			t.Fatalf("no outcome recorded for the running track %q, got %v", running.String(), byTrackID)
		}
		if runningOutcome.Outcome != JobSucceeded {
			t.Errorf("running track outcome = %q, want %q", runningOutcome.Outcome, JobSucceeded)
		}

		outcome, ok := byTrackID[queued.String()]
		if !ok {
			t.Fatalf("no outcome recorded for the queued track %q, got %v", queued.String(), byTrackID)
		}
		if outcome.Outcome != JobSucceeded {
			t.Errorf("outcome = %q, want %q", outcome.Outcome, JobSucceeded)
		}
		if outcome.ElapsedMs < 0 {
			t.Errorf("elapsed ms = %d, want >= 0", outcome.ElapsedMs)
		}
	})
}

func TestBackgroundScheduler_OutcomeRecorderFailure_DoesNotAffectTheJob(t *testing.T) {
	t.Run("erroring recorder leaves the job succeeded", func(t *testing.T) {
		recorder := newFakeOutcomeRecorder()
		recorder.err = errors.New("outcome store unavailable")
		svc := NewAcquireTrackAudioService(newFakeTrackRepository(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		var wg sync.WaitGroup
		scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		trackId := domain.NewTrackId()

		if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), trackId, ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		awaitOutcome(t, recorder.calls)
		wg.Wait()

		_, recent := scheduler.log.snapshot()
		if len(recent) != 1 || recent[0].State != JobSucceeded {
			t.Fatalf("recent jobs = %+v, want exactly one %s job (a Record error must not change the job's result)", recent, JobSucceeded)
		}
	})

	t.Run("hanging recorder does not delay the next job", func(t *testing.T) {
		recorder := newFakeOutcomeRecorder()
		recorder.delay = outcomeRecordTimeout * 3
		acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
		var wg sync.WaitGroup
		scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		userId := shared.NewUserId(uuid.New())

		if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
			t.Fatalf("first schedule: %v", err)
		}
		<-acq.started
		close(acq.release)
		wg.Wait()

		start := time.Now()
		if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
			t.Fatalf("second schedule: %v", err)
		}
		wg.Wait()
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Errorf("second job took %s to run, want well under the recorder's %s hang (outcome recording must not block the next job)", elapsed, recorder.delay)
		}
		if got := acq.calls.Load(); got != 2 {
			t.Errorf("acquirer executions = %d, want 2", got)
		}
		for i := 0; i < 2; i++ {
			select {
			case <-recorder.calls:
			case <-time.After(recorder.delay + jobSettleTimeout):
				t.Fatalf("outcome recorder call %d never settled", i+1)
			}
		}
	})
}

func TestBackgroundScheduler_RecordOutcome_GivesTheRecorderAWorkingMargin(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	svc := NewAcquireTrackAudioService(newFakeTrackRepository(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	assertExactlyOneOutcome(t, recorder.calls)
	wg.Wait()

	select {
	case margin := <-recorder.deadlines:
		if margin < time.Second {
			t.Errorf("recorder's context deadline was %s away, want at least 1s of working margin", margin)
		}
	case <-time.After(jobSettleTimeout):
		t.Fatal("recorder never observed a context deadline")
	}
}

type syncLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureJSONLogSync(t *testing.T) *syncLogBuffer {
	t.Helper()
	buf := &syncLogBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestBackgroundScheduler_RecordOutcome_LogsWarnOnlyWhenRecordFails(t *testing.T) {
	t.Run("failure logs the warning", func(t *testing.T) {
		logs := captureJSONLogSync(t)
		recorder := newFakeOutcomeRecorder()
		recorder.err = errors.New("outcome store unavailable")
		svc := NewAcquireTrackAudioService(newFakeTrackRepository(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		var wg sync.WaitGroup
		scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})

		if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		assertExactlyOneOutcome(t, recorder.calls)
		wg.Wait()

		deadline := time.Now().Add(jobSettleTimeout)
		for !strings.Contains(logs.String(), "acquisition.outcome_record_failed") {
			if time.Now().After(deadline) {
				t.Fatalf("scheduler log missing acquisition.outcome_record_failed after a Record error:\n%s", logs.String())
			}
			time.Sleep(time.Millisecond)
		}
	})

	t.Run("success logs no warning", func(t *testing.T) {
		logs := captureJSONLogSync(t)
		recorder := newFakeOutcomeRecorder()
		svc := NewAcquireTrackAudioService(newFakeTrackRepository(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		var wg sync.WaitGroup
		scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})

		if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		assertExactlyOneOutcome(t, recorder.calls)
		wg.Wait()
		time.Sleep(20 * time.Millisecond)

		if strings.Contains(logs.String(), "acquisition.outcome_record_failed") {
			t.Errorf("scheduler logged acquisition.outcome_record_failed after a successful Record:\n%s", logs.String())
		}
	})
}

type ctxAtRecordRecorder struct {
	calls   chan acqports.AcquisitionOutcome
	ctxErrs chan error
}

func newCtxAtRecordRecorder() *ctxAtRecordRecorder {
	return &ctxAtRecordRecorder{
		calls:   make(chan acqports.AcquisitionOutcome, 8),
		ctxErrs: make(chan error, 8),
	}
}

func (r *ctxAtRecordRecorder) Record(ctx context.Context, o acqports.AcquisitionOutcome) error {
	r.ctxErrs <- ctx.Err()
	r.calls <- o
	return nil
}

func TestBackgroundScheduler_ShutdownMidRun_RecordsTheOutcomeOnALiveContext(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	source := newBlockingSource()
	recorder := newCtxAtRecordRecorder()
	svc := NewAcquireTrackAudioService(liveCtxTrackRepository{repo}, NewSourceRegistry(source), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder), WithDrainBudget(10*time.Millisecond))
	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	<-source.searching

	scheduler.Shutdown(context.Background())

	outcome := assertExactlyOneOutcome(t, recorder.calls)
	wg.Wait()
	if outcome.TrackID != track.ID.String() {
		t.Errorf("track id = %q, want %q", outcome.TrackID, track.ID.String())
	}
	if outcome.Outcome != JobCancelled {
		t.Errorf("outcome = %q, want %q (a job cut mid-run by shutdown is released for retry, not failed)", outcome.Outcome, JobCancelled)
	}
	if outcome.Reason != "shutdown" {
		t.Errorf("reason = %q, want %q", outcome.Reason, "shutdown")
	}
	if ctxErr := <-recorder.ctxErrs; ctxErr != nil {
		t.Errorf("Record's context was already done (%v), want a live context detached from the cancelled job", ctxErr)
	}
}

func TestBackgroundScheduler_ShutdownBeforeStart_LeavesTheQueuedJobWithNoOutcome(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	base := newFakeTrackRepository()
	running := newPendingTrack(t, userId, base)
	queued := newPendingTrack(t, userId, base)
	repo := &holdOneTrackRepo{fakeTrackRepository: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	recorder := newCtxAtRecordRecorder()
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder), WithDrainBudget(10*time.Millisecond))
	var released sync.Once
	release := func() {
		released.Do(func() { close(repo.release) })
		wg.Wait()
	}
	t.Cleanup(release)

	if err := scheduler.Schedule(context.Background(), userId, running.ID, ""); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	<-repo.holding
	if err := scheduler.Schedule(context.Background(), userId, queued.ID, ""); err != nil {
		t.Fatalf("second schedule: %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	scheduler.Shutdown(shutdownCtx)
	release()

	only := awaitOutcome(t, recorder.calls)
	if only.TrackID != running.ID.String() {
		t.Fatalf("recorded track = %q, want the running track %q (the queued job was never claimed)", only.TrackID, running.ID.String())
	}
	if only.Outcome != JobCancelled {
		t.Errorf("outcome = %q, want %q", only.Outcome, JobCancelled)
	}
	if only.Reason != "shutdown" {
		t.Errorf("reason = %q, want %q", only.Reason, "shutdown")
	}
	if ctxErr := <-recorder.ctxErrs; ctxErr != nil {
		t.Errorf("Record's context was already done (%v), want a live context", ctxErr)
	}

	select {
	case extra := <-recorder.calls:
		t.Fatalf("recorder received a call for the never-claimed queued job %+v, want none", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBackgroundScheduler_Shutdown_WaitsForTheInFlightOutcomeWrite(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	recorder.delay = 300 * time.Millisecond
	svc := NewAcquireTrackAudioService(newFakeTrackRepository(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	wg.Wait()

	start := time.Now()
	scheduler.Shutdown(context.Background())
	elapsed := time.Since(start)

	if elapsed < recorder.delay {
		t.Errorf("Shutdown returned after %s, want at least the %s the in-flight outcome write takes", elapsed, recorder.delay)
	}
	assertExactlyOneOutcome(t, recorder.calls)
}

func TestBackgroundScheduler_RecordedOutcome_MatchesWhatTheJobLogShows(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	trackId := domain.NewTrackId()
	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), trackId, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	<-acq.started
	time.Sleep(30 * time.Millisecond)
	close(acq.release)

	outcome := assertExactlyOneOutcome(t, recorder.calls)
	wg.Wait()
	recent := scheduler.Status().Recent
	if len(recent) != 1 {
		t.Fatalf("Status().Recent = %+v, want exactly one job", recent)
	}
	shown := recent[0]
	if outcome.ElapsedMs != shown.ElapsedMs {
		t.Errorf("recorded elapsed ms = %d, job log shows %d, want the same value", outcome.ElapsedMs, shown.ElapsedMs)
	}
	if outcome.ElapsedMs < 30 {
		t.Errorf("recorded elapsed ms = %d, want >= 30 (the job ran at least 30ms)", outcome.ElapsedMs)
	}
	if outcome.Outcome != shown.State || outcome.Reason != shown.Reason {
		t.Errorf("recorded (%q, %q), job log shows (%q, %q), want the same", outcome.Outcome, outcome.Reason, shown.State, shown.Reason)
	}
}

func TestBackgroundScheduler_RecordedFailureReason_KeepsTheCookiePathOut(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	leaking := &fakeAudioSearcher{searchErr: errors.New("yt-dlp exited 1: --cookies " + cookieJarPath + ": permission denied")}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(leaking), newFakeAudioStore())
	recorder := newFakeOutcomeRecorder()
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	outcome := assertExactlyOneOutcome(t, recorder.calls)
	wg.Wait()

	if outcome.Outcome != JobFailed {
		t.Fatalf("outcome = %q, want %q", outcome.Outcome, JobFailed)
	}
	if strings.Contains(outcome.Reason, cookieJarPath) {
		t.Errorf("recorded reason = %q, still names the cookie file %q", outcome.Reason, cookieJarPath)
	}
}

func TestBackgroundScheduler_FailedOutcomeWrite_LogsOneWarnWithoutTheCookiePath(t *testing.T) {
	logs := captureJSONLogSync(t)
	recorder := newFakeOutcomeRecorder()
	recorder.err = errors.New("pg write: --cookies " + cookieJarPath + ": permission denied")
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(&stubAcquirer{}, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	assertExactlyOneOutcome(t, recorder.calls)
	wg.Wait()

	var warnLines []string
	deadline := time.Now().Add(jobSettleTimeout)
	for {
		warnLines = warnLines[:0]
		for _, line := range strings.Split(logs.String(), "\n") {
			if strings.Contains(line, "acquisition.outcome_record_failed") {
				warnLines = append(warnLines, line)
			}
		}
		if len(warnLines) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(warnLines) != 1 {
		t.Fatalf("acquisition.outcome_record_failed lines = %d, want exactly 1:\n%s", len(warnLines), logs.String())
	}
	if !strings.Contains(warnLines[0], `"level":"WARN"`) {
		t.Errorf("outcome write failure logged as %s, want level WARN", warnLines[0])
	}
	if strings.Contains(logs.String(), cookieJarPath) {
		t.Errorf("log names the cookie file %q from the Record error:\n%s", cookieJarPath, logs.String())
	}
}

func TestBackgroundScheduler_RecordOutcome_BoundsTheWriteToTwoSeconds(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(&stubAcquirer{}, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	assertExactlyOneOutcome(t, recorder.calls)
	wg.Wait()

	select {
	case margin := <-recorder.deadlines:
		if margin > 2*time.Second {
			t.Errorf("recorder's context deadline was %s away, want at most 2s", margin)
		}
	case <-time.After(jobSettleTimeout):
		t.Fatal("recorder never observed a context deadline, want the write bounded at 2s")
	}
}

func TestBackgroundScheduler_ReplaceJob_RecordsItsOutcome(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(&stubAcquirer{}, &wg, make(chan struct{}, 1), WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	trackId := domain.NewTrackId()

	if err := scheduler.ScheduleReplace(context.Background(), shared.NewUserId(uuid.New()), trackId); err != nil {
		t.Fatalf("schedule replace: %v", err)
	}
	outcome := assertExactlyOneOutcome(t, recorder.calls)
	wg.Wait()
	if outcome.TrackID != trackId.String() || outcome.Outcome != JobSucceeded {
		t.Errorf("recorded (%q, %q), want (%q, %q)", outcome.TrackID, outcome.Outcome, trackId.String(), JobSucceeded)
	}
}

func TestBackgroundScheduler_DedupedSchedule_ReplaysAndRecordsBothOutcomes(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 2), WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()
	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	<-acq.started

	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err != nil {
		t.Fatalf("duplicate schedule = %v, want nil (already in flight)", err)
	}
	close(acq.release)

	first := awaitOutcome(t, recorder.calls)
	second := awaitOutcome(t, recorder.calls)
	wg.Wait()
	if first.TrackID != trackId.String() || second.TrackID != trackId.String() {
		t.Errorf("track ids = %q, %q, want both %q", first.TrackID, second.TrackID, trackId.String())
	}
	if got := acq.calls.Load(); got != 2 {
		t.Errorf("acquirer calls = %d, want 2 (the deduped request replays once the first run settles)", got)
	}
}

func TestBackgroundScheduler_NilOutcomeRecorder_LeavesTheJobUntouched(t *testing.T) {
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(&stubAcquirer{}, &wg, make(chan struct{}, 1), WithOutcomeRecorder(nil))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	wg.Wait()

	status := scheduler.Status()
	if status.Succeeded != 1 || len(status.Recent) != 1 || status.Recent[0].State != JobSucceeded {
		t.Errorf("status = succeeded %d, recent %+v, want one %s job", status.Succeeded, status.Recent, JobSucceeded)
	}
}

type ctxCapturingAcquirer struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	ctx     atomic.Value
}

func (a *ctxCapturingAcquirer) Execute(ctx context.Context, _ shared.UserId, _ domain.TrackId) error {
	a.ctx.Store(ctx)
	a.once.Do(func() { close(a.started) })
	select {
	case <-a.release:
	case <-ctx.Done():
	}
	return ctx.Err()
}

func (a *ctxCapturingAcquirer) ExecuteReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	return a.Execute(ctx, userId, trackId)
}

func (a *ctxCapturingAcquirer) RefuseQueued(context.Context, shared.UserId, domain.TrackId) {}

type flakyHeartbeatQueue struct {
	*memJobQueue
	heartbeatErr error
}

func (q *flakyHeartbeatQueue) Heartbeat(context.Context, domain.TrackId, int, time.Duration) error {
	return q.heartbeatErr
}

func TestBackgroundScheduler_HeartbeatFailuresOutlastingTheLease_CancelsTheJob(t *testing.T) {
	acq := &ctxCapturingAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	wake := make(chan struct{}, 1)
	queue := &flakyHeartbeatQueue{memJobQueue: newMemJobQueue(wake), heartbeatErr: errors.New("db blip")}
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1),
		WithJobQueue(queue),
		WithHeartbeatInterval(5*time.Millisecond),
		WithLeaseDuration(20*time.Millisecond),
	)
	t.Cleanup(func() {
		close(acq.release)
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	<-acq.started

	jobCtx, ok := acq.ctx.Load().(context.Context)
	if !ok {
		t.Fatal("acquirer never observed a job context")
	}

	select {
	case <-jobCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("job context was never cancelled once heartbeat failures outlasted the lease duration")
	}
}

func TestAcquisitionStatus_ReportsVerifySkipsAndFingerprintVerification(t *testing.T) {
	skips := &VerifySkipCounter{}
	fingerprinted := false
	s := newStatusTestScheduler(t)
	WithVerifySkipCount(skips.Count)(s)
	WithFingerprintVerified(func() bool { return fingerprinted })(s)

	before := s.Status()
	skips.RecordVerifySkip("decode_timeout")
	skips.RecordVerifySkip("identify_failed")
	fingerprinted = true
	after := s.Status()

	if before.VerifySkipped != 0 || before.Verification.FingerprintVerified {
		t.Errorf("before = %d skips, verified %v, want 0 and false", before.VerifySkipped, before.Verification.FingerprintVerified)
	}
	if after.VerifySkipped != 2 || !after.Verification.FingerprintVerified {
		t.Errorf("after = %d skips, verified %v, want 2 and true", after.VerifySkipped, after.Verification.FingerprintVerified)
	}
}

type releaseRecord struct {
	fence       int
	availableAt time.Time
}

type releaseRecordingQueue struct {
	*memJobQueue
	releases chan releaseRecord
}

func (q *releaseRecordingQueue) Release(ctx context.Context, trackID domain.TrackId, fence int, availableAt time.Time) error {
	q.releases <- releaseRecord{fence: fence, availableAt: availableAt}
	return q.memJobQueue.Release(ctx, trackID, fence, availableAt)
}

func TestBackgroundScheduler_TransientFailure_ReleasesTheJobWithBackoffAndKeepsTheTrackPending(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := newPendingTrack(t, userId, repo)
	searcher := &fakeAudioSearcher{searchErr: &acqports.SourceUnavailableError{Source: "fake", Err: errors.New("http error 503")}}
	pub := newRecordingPublisher()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), newFakeAudioStore(), WithAcquireEvents(pub))
	wake := make(chan struct{}, 1)
	queue := &releaseRecordingQueue{memJobQueue: newMemJobQueue(wake), releases: make(chan releaseRecord, 1)}
	queue.rememberUser(track.ID, userId)
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1),
		WithJobQueue(queue), WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	before := time.Now()
	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	var released releaseRecord
	select {
	case released = <-queue.releases:
	case <-time.After(5 * time.Second):
		t.Fatal("job was never released for retry")
	}
	if released.fence != 1 {
		t.Errorf("released fence = %d, want 1", released.fence)
	}
	if wait := released.availableAt.Sub(before); wait < retryBackoff(1) || wait > retryBackoff(1)+5*time.Second {
		t.Errorf("released availableAt is %v after scheduling, want about %v", wait, retryBackoff(1))
	}
	stored := repo.tracks[track.ID.String()+":"+userId.String()]
	if stored.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("status = %v, want %v", stored.AcquisitionStatus, domain.AcquisitionPending)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0", got)
	}
}
