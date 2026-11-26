package service

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/inmemoryqueue"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	acqdomain "altune/go-api/internal/download/internal/core/domain"
	acqports "altune/go-api/internal/download/internal/core/ports"

	"github.com/google/uuid"
)

type memJobQueue = inmemoryqueue.Queue

func newMemJobQueue(wake chan<- struct{}) *memJobQueue { return inmemoryqueue.New(wake, time.Now) }

type ledgerTrack = acquisitiontest.LedgerTrack

func newLedgerTrack(userId ids.UserId, title, artist, album string) ledgerTrack {
	title = strings.TrimSpace(title)
	if strings.TrimSpace(album) == "" {
		album = title
	}
	return ledgerTrack{TrackSnapshot: acqports.TrackSnapshot{
		ID:     ids.NewTrackId(),
		UserId: userId,
		Title:  title,
		Artist: artist,
		Album:  album,
		Status: acqports.TrackPending,
	}}
}

func makeReady(track *ledgerTrack, audioRef string) {
	track.Status, track.AudioRef, track.FailureReason = acqports.TrackReady, &audioRef, ""
}

func makeFailed(track *ledgerTrack, reason string) {
	track.Status, track.AudioRef, track.FailureReason = acqports.TrackFailed, nil, reason
}

func useSource(track *ledgerTrack, url string) { track.AudioSourceURL = &url }

func withCatalogLength(t *testing.T, ledger *acquisitiontest.Ledger, track ledgerTrack, seconds float64) ledgerTrack {
	t.Helper()
	track.CatalogDurationSeconds = &seconds
	ledger.Seed(track)
	return track
}

func ptrTo[T any](v T) *T { return &v }

func readyWith(ref string) func(*ledgerTrack) {
	return func(track *ledgerTrack) { makeReady(track, ref) }
}

func committedTrack(t *testing.T, ledger *acquisitiontest.Ledger, id ids.TrackId) ledgerTrack {
	t.Helper()
	track, ok := ledger.Track(id)
	if !ok {
		t.Fatalf("track %s is missing from the ledger", id)
	}
	return track
}

type emptyLedger struct{}

func (emptyLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*acqports.TrackSnapshot, error) {
	return acquisitiontest.NewLedger().Get(ctx, id, userId)
}

func (emptyLedger) AudioRefInUse(context.Context, string, ids.TrackId) (bool, error) {
	return false, nil
}

func (emptyLedger) Complete(context.Context, ids.TrackId, ids.UserId, acqports.ClaimNumber, acqports.Completion) error {
	return acqports.ErrTrackNotFound
}

func (emptyLedger) Fail(context.Context, ids.TrackId, ids.UserId, acqports.ClaimNumber, string) error {
	return acqports.ErrTrackNotFound
}

func (emptyLedger) RevertAt(context.Context, acqports.TrackSnapshot) error {
	return acqports.ErrTrackNotFound
}

type liveCtxLedger struct{ *acquisitiontest.Ledger }

func (l liveCtxLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*acqports.TrackSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Ledger.Get(ctx, id, userId)
}

func (l liveCtxLedger) Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, c acqports.Completion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.Ledger.Complete(ctx, id, userId, fence, c)
}

func (l liveCtxLedger) RevertAt(ctx context.Context, track acqports.TrackSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.Ledger.RevertAt(ctx, track)
}

func (l liveCtxLedger) Fail(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.Ledger.Fail(ctx, id, userId, fence, reason)
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
	requests      []acqports.FindRequest
	mu            sync.Mutex
}

func (s *fakeAudioSearcher) Search(_ context.Context, _ string) ([]acqports.AudioCandidate, error) {
	s.mu.Lock()
	s.searchCalled = true
	s.mu.Unlock()
	return s.searchResults, s.searchErr
}

func (s *fakeAudioSearcher) Download(_ context.Context, url string, _ string) (string, error) {
	s.mu.Lock()
	s.downloadURLs = append(s.downloadURLs, url)
	s.mu.Unlock()
	return s.downloadPath, s.downloadErr
}

func (s *fakeAudioSearcher) Name() acqdomain.SourceName { return "fake" }

func (s *fakeAudioSearcher) Find(ctx context.Context, req acqports.FindRequest) ([]acqports.AudioCandidate, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	return s.Search(ctx, "")
}

func (s *fakeAudioSearcher) Fetch(ctx context.Context, c acqports.AudioCandidate, outDir string) (acqports.FetchedAudio, error) {
	return fetchedAt(s.Download(ctx, c.URL, outDir))
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

func (s *fakeAudioStore) Delete(_ context.Context, audioRef string) error {
	delete(s.stored, audioRef)
	return nil
}

func TestBackgroundScheduler_Schedule(t *testing.T) {
	ledger := acquisitiontest.NewLedger()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()

	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)
	scheduler := NewBackgroundAcquisitionScheduler(svc, 2)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	userId := ids.NewUserId(uuid.New())
	trackId := ids.NewTrackId()

	scheduler.Schedule(context.Background(), userId, trackId, "")

	awaitIdle(t, scheduler)
	if inFlight := scheduler.Status().InFlight; inFlight != 0 {
		t.Errorf("in flight = %d after the job completed, want 0", inFlight)
	}
}

func TestBackgroundScheduler_ScheduleMultiple_RespectsSemaphore(t *testing.T) {
	ledger := acquisitiontest.NewLedger()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()

	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	userId := ids.NewUserId(uuid.New())

	var completedCount atomic.Int32
	const numSchedules = 3

	for i := 0; i < numSchedules; i++ {
		trackId := ids.NewTrackId()
		scheduler.Schedule(context.Background(), userId, trackId, "")
	}

	awaitIdle(t, scheduler)
	_ = completedCount.Load()

	if inFlight := scheduler.Status().InFlight; inFlight != 0 {
		t.Errorf("in flight = %d after every job completed, want 0", inFlight)
	}
}

func TestBackgroundScheduler_ShutdownMidSearch_ReleasesTheJobForAnotherWorker(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	source := newBlockingSource()
	svc := newTestAcquireService(liveCtxLedger{ledger}, NewSourceRegistry(source), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithDrainBudget(10*time.Millisecond))
	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	<-source.searching

	scheduler.Shutdown(context.Background())

	settled := committedTrack(t, ledger, track.ID)
	if settled.Status != acqports.TrackPending {
		t.Errorf("status = %v, want %v (released for another worker)", settled.Status, acqports.TrackPending)
	}
	if settled.FailureReason != "" {
		t.Errorf("persisted failure_reason = %q, want nil", settled.FailureReason)
	}
}

func TestBackgroundScheduler_ShutdownMidSearch_ReleasesTheReplaceJobForAnotherWorker(t *testing.T) {
	logs := captureJSONLog(t)
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	useSource(&track, "https://www.youtube.com/watch?v=currentAAAA")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	source := newBlockingSource()
	svc := newTestAcquireService(liveCtxLedger{ledger}, NewSourceRegistry(source), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithDrainBudget(10*time.Millisecond))
	if err := scheduler.ScheduleReplace(context.Background(), userId, track.ID); err != nil {
		t.Fatalf("schedule replace: %v", err)
	}
	<-source.searching

	scheduler.Shutdown(context.Background())

	settled := committedTrack(t, ledger, track.ID)
	if settled.Status != acqports.TrackPending {
		t.Errorf("status = %v, want %v (released for another worker)", settled.Status, acqports.TrackPending)
	}
	if settled.FailureReason != "" {
		t.Errorf("persisted failure_reason = %q, want nil", settled.FailureReason)
	}
	if findLogRecord(t, logs, "track_acquisition_cancelled") == nil {
		t.Error("want a track_acquisition_cancelled log for the scheduler-cancelled replace, got none")
	}
}

const cookieJarPath = "/home/x/cookies.txt"

func TestBackgroundScheduler_FailedJob_KeepsTheCookiePathOutOfTheReasonAndTheLog(t *testing.T) {
	logs := captureJSONLog(t)
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	leaking := &fakeAudioSearcher{searchErr: errors.New("yt-dlp exited 1: --cookies " + cookieJarPath + ": permission denied")}
	svc := newTestAcquireService(ledger, fakeRegistry(leaking), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	awaitIdle(t, scheduler)

	_, recent := scheduler.log.snapshot()
	if len(recent) != 1 || recent[0].State != acqports.JobFailed {
		t.Fatalf("recent jobs = %+v, want exactly one %s job", recent, acqports.JobFailed)
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
		accepted, settled, remaining := scheduleWhileShuttingDown(t)

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

func scheduleWhileShuttingDown(t *testing.T) (accepted []string, settled map[string]acqports.JobRecord, remaining map[string]struct{}) {
	t.Helper()
	svc := newTestAcquireService(&countingLedger{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var jobs sync.WaitGroup
	var queueAhead atomic.Int64
	scheduler := NewBackgroundAcquisitionScheduler(svc, 4, func(s *BackgroundAcquisitionScheduler) {
		s.queue = inmemoryqueue.New(s.wake, func() time.Time { return time.Now().Add(time.Duration(queueAhead.Load())) })
	})
	queue, ok := scheduler.queue.(*memJobQueue)
	if !ok || queue == nil {
		t.Fatalf("scheduler queue = %T, want the in-memory queue the option built", scheduler.queue)
		return nil, nil, nil
	}
	userId := ids.NewUserId(uuid.New())

	var mu sync.Mutex
	start := make(chan struct{})
	var callers sync.WaitGroup
	for i := 0; i < racingSchedulers; i++ {
		trackId := ids.NewTrackId()
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
	queueAhead.Store(int64(24 * time.Hour))
	remaining = claimableTracks(queue)
	return accepted, settled, remaining
}

const jobSettleTimeout = 2 * time.Second

type startedAcquirer struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (a *startedAcquirer) Execute(context.Context, ids.UserId, ids.TrackId) error {
	a.calls.Add(1)
	a.once.Do(func() { close(a.started) })
	<-a.release
	return nil
}

func (a *startedAcquirer) ExecuteReplace(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
	return a.Execute(ctx, userId, trackId)
}

func (a *startedAcquirer) RefuseQueued(context.Context, ids.UserId, ids.TrackId) {}

func TestBackgroundScheduler_QueuedJobWaitsForAWorkerSlotAndRuns(t *testing.T) {
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	userId := ids.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), userId, ids.NewTrackId(), ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-acq.started
	queued := ids.NewTrackId()

	if err := scheduler.Schedule(context.Background(), userId, queued, ""); err != nil {
		t.Fatalf("second Schedule = %v, want nil (enqueued, waiting for a worker)", err)
	}

	if _, settled := settledJob(scheduler, queued.String()); settled {
		t.Fatal("queued job settled before the running job released its worker, want it still waiting")
	}

	close(acq.release)
	settled := awaitSettledJob(t, scheduler, queued.String())
	if settled.State != acqports.JobSucceeded {
		t.Errorf("queued job state = %q, want %q (it must run once a worker frees up)", settled.State, acqports.JobSucceeded)
	}
	if got := acq.calls.Load(); got != 2 {
		t.Errorf("acquirer executions = %d, want 2 (both jobs eventually run)", got)
	}
	awaitIdle(t, scheduler)
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
	ledger := &burstLedger{started: make(chan struct{}), release: make(chan struct{})}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 2)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	var released sync.Once
	drain := func() {
		released.Do(func() { close(ledger.release) })
		awaitIdle(t, scheduler)
	}
	t.Cleanup(drain)
	if err := hold(scheduler); err != nil {
		t.Fatalf("holding schedule = %v, want nil", err)
	}
	<-ledger.started
	return scheduler, drain
}

func TestBackgroundScheduler_AReplaceWaitsOutAPlainJobOnTheSameTrack(t *testing.T) {
	userId, trackId := ids.NewUserId(uuid.New()), ids.NewTrackId()
	scheduler, drain := trackHeldByJob(t, func(s *BackgroundAcquisitionScheduler) error {
		return s.Schedule(context.Background(), userId, trackId, "")
	})

	if err := scheduler.ScheduleReplace(context.Background(), userId, trackId); !errors.Is(err, acqdomain.ErrTrackJobInFlight) {
		t.Fatalf("replace while a plain job runs = %v, want ErrTrackJobInFlight", err)
	}
	drain()

	if err := scheduler.ScheduleReplace(context.Background(), userId, trackId); err != nil {
		t.Errorf("replace after the plain job settled = %v, want nil", err)
	}
}

func TestBackgroundScheduler_ARetryWaitsOutAReplaceOnTheSameTrack(t *testing.T) {
	userId, trackId := ids.NewUserId(uuid.New()), ids.NewTrackId()
	scheduler, drain := trackHeldByJob(t, func(s *BackgroundAcquisitionScheduler) error {
		return s.ScheduleReplace(context.Background(), userId, trackId)
	})

	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); !errors.Is(err, acqdomain.ErrTrackJobInFlight) {
		t.Fatalf("retry while a replace runs = %v, want ErrTrackJobInFlight", err)
	}
	drain()

	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err != nil {
		t.Errorf("retry after the replace settled = %v, want nil", err)
	}
}

func TestNewBackgroundAcquisitionScheduler_ReturnsNonNil(t *testing.T) {
	ledger := acquisitiontest.NewLedger()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
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

func (s *stubAcquirer) Execute(context.Context, ids.UserId, ids.TrackId) error {
	s.record("Execute")
	return nil
}

func (s *stubAcquirer) ExecuteReplace(context.Context, ids.UserId, ids.TrackId) error {
	s.record("ExecuteReplace")
	return nil
}

func (s *stubAcquirer) RefuseQueued(context.Context, ids.UserId, ids.TrackId) {}

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
		schedule func(*BackgroundAcquisitionScheduler, ids.UserId, ids.TrackId) error
		want     string
	}{
		"Schedule runs Execute": {
			schedule: func(s *BackgroundAcquisitionScheduler, userId ids.UserId, trackId ids.TrackId) error {
				return s.Schedule(context.Background(), userId, trackId, "")
			},
			want: "Execute",
		},
		"ScheduleReplace runs ExecuteReplace": {
			schedule: func(s *BackgroundAcquisitionScheduler, userId ids.UserId, trackId ids.TrackId) error {
				return s.ScheduleReplace(context.Background(), userId, trackId)
			},
			want: "ExecuteReplace",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			acq := &stubAcquirer{}
			scheduler := NewBackgroundAcquisitionScheduler(acq, 1)
			t.Cleanup(func() {
				scheduler.Shutdown(context.Background())
			})

			if err := tc.schedule(scheduler, ids.NewUserId(uuid.New()), ids.NewTrackId()); err != nil {
				t.Fatalf("schedule: %v", err)
			}
			awaitIdle(t, scheduler)

			got := acq.entryPointsRun()
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("entry points run = %v, want [%s]", got, tc.want)
			}
		})
	}
}

type burstLedger struct {
	emptyLedger
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (r *burstLedger) enter() {
	r.calls.Add(1)
	r.once.Do(func() { close(r.started) })
	<-r.release
}

func (r *burstLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*acqports.TrackSnapshot, error) {
	r.enter()
	return r.emptyLedger.Get(ctx, id, userId)
}

func (r *burstLedger) Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, c acqports.Completion) error {
	r.enter()
	return r.emptyLedger.Complete(ctx, id, userId, fence, c)
}

func (r *burstLedger) Fail(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, reason string) error {
	r.enter()
	return r.emptyLedger.Fail(ctx, id, userId, fence, reason)
}

func (r *burstLedger) RevertAt(ctx context.Context, track acqports.TrackSnapshot) error {
	r.enter()
	return r.emptyLedger.RevertAt(ctx, track)
}

func TestBackgroundScheduler_BoundsQueueDepthUnderBurst(t *testing.T) {
	ledger := &burstLedger{started: make(chan struct{}), release: make(chan struct{})}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	const workers = 1
	scheduler := NewBackgroundAcquisitionScheduler(svc, workers)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	userId := ids.NewUserId(uuid.New())
	const burst = 60
	for i := 0; i < burst; i++ {
		scheduler.Schedule(context.Background(), userId, ids.NewTrackId(), "")
	}

	<-ledger.started

	active := len(scheduler.Status().ActiveJobs)
	maxAllowed := workers * 8
	if active > maxAllowed {
		t.Errorf("active job-log entries = %d under burst of %d; want bounded <= %d (arrivals must not spawn unbounded work)",
			active, burst, maxAllowed)
	}

	close(ledger.release)
	awaitIdle(t, scheduler)
}

func TestBackgroundScheduler_StatusQueueDrainsAndCountsShutdownRejections(t *testing.T) {
	ledger := &burstLedger{started: make(chan struct{}), release: make(chan struct{})}
	close(ledger.release)
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	const workers = 1
	scheduler := NewBackgroundAcquisitionScheduler(svc, workers)

	userId := ids.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), userId, ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	awaitIdle(t, scheduler)

	status := scheduler.Status()
	if status.QueueDepth != 0 {
		t.Errorf("queue depth after drain = %d, want 0", status.QueueDepth)
	}
	if status.Workers != workers {
		t.Errorf("queue capacity = %d, want %d (worker count)", status.Workers, workers)
	}

	scheduler.Shutdown(context.Background())
	err := scheduler.ScheduleReplace(context.Background(), userId, ids.NewTrackId())
	if !errors.Is(err, acqdomain.ErrSchedulerShutdown) {
		t.Fatalf("schedule after shutdown err = %v, want ErrSchedulerShutdown", err)
	}
	if got := scheduler.Status().Rejected; got != 1 {
		t.Errorf("rejected after shutdown refusal = %d, want 1", got)
	}
}

type corrCapturingLedger struct {
	*acquisitiontest.Ledger
	mu     sync.Mutex
	corrID string
	seen   bool
}

func (r *corrCapturingLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*acqports.TrackSnapshot, error) {
	r.mu.Lock()
	r.corrID = logging.CorrelationIDFromContext(ctx)
	r.seen = true
	r.mu.Unlock()
	return r.Ledger.Get(ctx, id, userId)
}

func TestBackgroundScheduler_ThreadsCorrelationIDIntoJobContext(t *testing.T) {
	ledger := &corrCapturingLedger{Ledger: acquisitiontest.NewLedger()}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	const wantCorrID = "corr-xyz-345"
	ctx := logging.WithCorrelationID(context.Background(), wantCorrID)
	scheduler.Schedule(ctx, ids.NewUserId(uuid.New()), ids.NewTrackId(), "")

	awaitIdle(t, scheduler)

	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if !ledger.seen {
		t.Fatal("acquisition service was never invoked by the scheduled job")
	}
	if ledger.corrID != wantCorrID {
		t.Fatalf("job context lost the request correlation id: got %q, want %q", ledger.corrID, wantCorrID)
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
			userId := ids.NewUserId(uuid.New())
			ledger := acquisitiontest.NewLedger()
			track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=old")
			store := newFakeAudioStore()
			store.stored["u/a/b/c.mp3"] = true
			searcher := &fakeAudioSearcher{}
			svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)
			scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
			t.Cleanup(func() {
				scheduler.Shutdown(context.Background())
			})
			if tc.wantSearch {
				scheduler.ScheduleReplace(context.Background(), userId, track.ID)
			} else {
				scheduler.Schedule(context.Background(), userId, track.ID, "")
			}
			awaitIdle(t, scheduler)

			if searcher.searchCalled != tc.wantSearch {
				t.Errorf("search called = %v, want %v", searcher.searchCalled, tc.wantSearch)
			}
		})
	}
}

type blockingLedger struct {
	emptyLedger
	started     chan struct{}
	startedOnce sync.Once
	release     chan struct{}
	calls       atomic.Int32
}

func (r *blockingLedger) enter() {
	r.calls.Add(1)
	r.startedOnce.Do(func() { close(r.started) })
	<-r.release
}

func (r *blockingLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*acqports.TrackSnapshot, error) {
	r.enter()
	return r.emptyLedger.Get(ctx, id, userId)
}

func (r *blockingLedger) Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, c acqports.Completion) error {
	r.enter()
	return r.emptyLedger.Complete(ctx, id, userId, fence, c)
}

func (r *blockingLedger) Fail(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, reason string) error {
	r.enter()
	return r.emptyLedger.Fail(ctx, id, userId, fence, reason)
}

func (r *blockingLedger) RevertAt(ctx context.Context, track acqports.TrackSnapshot) error {
	r.enter()
	return r.emptyLedger.RevertAt(ctx, track)
}

func TestBackgroundScheduler_Schedule_DedupsConcurrentRun_ButReplaysAfterSettle(t *testing.T) {
	ledger := &blockingLedger{started: make(chan struct{}), release: make(chan struct{})}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 4)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	userId := ids.NewUserId(uuid.New())
	trackId := ids.NewTrackId()

	scheduler.Schedule(context.Background(), userId, trackId, "")
	<-ledger.started
	scheduler.Schedule(context.Background(), userId, trackId, "")

	if got := ledger.calls.Load(); got != 1 {
		t.Fatalf("GetByID calls while the first run is still in flight = %d, want 1 (no concurrent second run)", got)
	}

	close(ledger.release)

	deadline := time.Now().Add(jobSettleTimeout)
	for ledger.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	awaitIdle(t, scheduler)

	if got := ledger.calls.Load(); got != 2 {
		t.Errorf("GetByID calls = %d, want 2 (the deduped request replays once the first run settles)", got)
	}
}

type countingLedger struct {
	emptyLedger
	calls atomic.Int32
}

func (r *countingLedger) enter() {
	r.calls.Add(1)
}

func (r *countingLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*acqports.TrackSnapshot, error) {
	r.enter()
	return r.emptyLedger.Get(ctx, id, userId)
}

func (r *countingLedger) Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, c acqports.Completion) error {
	r.enter()
	return r.emptyLedger.Complete(ctx, id, userId, fence, c)
}

func (r *countingLedger) Fail(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, reason string) error {
	r.enter()
	return r.emptyLedger.Fail(ctx, id, userId, fence, reason)
}

func (r *countingLedger) RevertAt(ctx context.Context, track acqports.TrackSnapshot) error {
	r.enter()
	return r.emptyLedger.RevertAt(ctx, track)
}

func TestBackgroundScheduler_Schedule_AfterShutdown_NoOp(t *testing.T) {
	ledger := &countingLedger{}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler.Shutdown(ctx)

	scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), "")
	awaitIdle(t, scheduler)

	if got := ledger.calls.Load(); got != 0 {
		t.Errorf("GetByID calls = %d, want 0 (schedule after shutdown must be a no-op)", got)
	}
}

type panicLedger struct{ emptyLedger }

func (r *panicLedger) enter() {
	panic("boom")
}

func (r *panicLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*acqports.TrackSnapshot, error) {
	r.enter()
	return r.emptyLedger.Get(ctx, id, userId)
}

func (r *panicLedger) Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, c acqports.Completion) error {
	r.enter()
	return r.emptyLedger.Complete(ctx, id, userId, fence, c)
}

func (r *panicLedger) Fail(ctx context.Context, id ids.TrackId, userId ids.UserId, fence acqports.ClaimNumber, reason string) error {
	r.enter()
	return r.emptyLedger.Fail(ctx, id, userId, fence, reason)
}

func (r *panicLedger) RevertAt(ctx context.Context, track acqports.TrackSnapshot) error {
	r.enter()
	return r.emptyLedger.RevertAt(ctx, track)
}

func TestBackgroundScheduler_Schedule_RecoversFromPanic(t *testing.T) {
	svc := newTestAcquireService(&panicLedger{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), "")
	awaitIdle(t, scheduler)
}

type panickingAcquirer struct {
	runs    atomic.Int32
	refused atomic.Int32
}

func (a *panickingAcquirer) Execute(context.Context, ids.UserId, ids.TrackId) error {
	a.runs.Add(1)
	panic("boom")
}

func (a *panickingAcquirer) ExecuteReplace(ctx context.Context, u ids.UserId, t ids.TrackId) error {
	return a.Execute(ctx, u, t)
}

func (a *panickingAcquirer) RefuseQueued(context.Context, ids.UserId, ids.TrackId) {
	a.refused.Add(1)
}

type settleKeepsPendingQueue struct{ *memJobQueue }

func (q settleKeepsPendingQueue) Settle(ctx context.Context, trackID ids.TrackId, fence acqports.ClaimNumber) error {
	return q.Release(ctx, trackID, fence, 0)
}

func TestBackgroundScheduler_Schedule_PanickingJobIsNotReclaimedWithinAPoll(t *testing.T) {
	acq := &panickingAcquirer{}
	wake := make(chan struct{}, 1)
	queue := settleKeepsPendingQueue{newMemJobQueue(wake)}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1,
		WithJobQueue(queue), WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	if got := acq.runs.Load(); got != 1 {
		t.Errorf("runs = %d, want 1 (a panicked job must back off, not be re-claimed on the next poll)", got)
	}
}

func TestBackgroundScheduler_Schedule_PanickingJobAtTheAttemptCapFailsTheTrack(t *testing.T) {
	acq := &panickingAcquirer{}
	queue := settleKeepsPendingQueue{newMemJobQueue(make(chan struct{}, 1))}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithJobQueue(queue))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	job := acqports.Job{TrackID: ids.NewTrackId(), UserID: ids.NewUserId(uuid.New()), Run: maxAcquisitionAttempts}

	scheduler.releasePanickedJob(job)

	if got := acq.refused.Load(); got != 1 {
		t.Errorf("track failed %d times, want 1", got)
	}
}

func TestBackgroundScheduler_Schedule_PanickingJobWithOldClaimsButFreshRunIsRetried(t *testing.T) {
	acq := &panickingAcquirer{}
	queue := settleKeepsPendingQueue{newMemJobQueue(make(chan struct{}, 1))}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithJobQueue(queue))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	job := acqports.Job{TrackID: ids.NewTrackId(), UserID: ids.NewUserId(uuid.New()), ClaimNumber: acqports.ClaimNumber(maxAcquisitionAttempts + 4), Run: 1}

	scheduler.releasePanickedJob(job)

	if got := acq.refused.Load(); got != 0 {
		t.Errorf("track failed %d times, want 0 (first panic of a fresh run keeps its budget)", got)
	}
}

func TestBackgroundScheduler_Schedule_PanickingJobAtTheRunCapFailsTheTrack(t *testing.T) {
	acq := &panickingAcquirer{}
	queue := settleKeepsPendingQueue{newMemJobQueue(make(chan struct{}, 1))}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithJobQueue(queue))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	job := acqports.Job{TrackID: ids.NewTrackId(), UserID: ids.NewUserId(uuid.New()), ClaimNumber: acqports.ClaimNumber(maxAcquisitionAttempts + 4), Run: maxAcquisitionAttempts}

	scheduler.releasePanickedJob(job)

	if got := acq.refused.Load(); got != 1 {
		t.Errorf("track failed %d times, want 1", got)
	}
}

func TestBackgroundScheduler_OnePrincipalCannotStarveAnother(t *testing.T) {
	ledger := &burstLedger{started: make(chan struct{}), release: make(chan struct{})}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	userA := ids.NewUserId(uuid.New())
	userB := ids.NewUserId(uuid.New())

	const flood = 10
	for i := 0; i < flood; i++ {
		if err := scheduler.Schedule(context.Background(), userA, ids.NewTrackId(), ""); err != nil {
			t.Fatalf("user A schedule #%d = %v, want nil (the queue is unbounded)", i+1, err)
		}
	}

	<-ledger.started

	if err := scheduler.Schedule(context.Background(), userB, ids.NewTrackId(), ""); err != nil {
		t.Fatalf("user B schedule while A floods = %v, want nil (A must not starve B)", err)
	}

	close(ledger.release)
	awaitIdle(t, scheduler)
}

func TestBackgroundScheduler_RuntimeKillSwitchTogglesAdmission(t *testing.T) {
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithPollInterval(10*time.Millisecond))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	user := ids.NewUserId(uuid.New())

	if err := scheduler.Schedule(context.Background(), user, ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule while enabled = %v, want nil", err)
	}
	<-acq.started
	if scheduler.Status().Paused {
		t.Fatal("Status().Paused = true before any Pause, want false")
	}
	close(acq.release)
	awaitIdle(t, scheduler)

	scheduler.Pause()
	if !scheduler.Status().Paused {
		t.Fatal("Status().Paused = false after Pause, want true")
	}
	paused := ids.NewTrackId()
	if err := scheduler.Schedule(context.Background(), user, paused, ""); !errors.Is(err, acqdomain.ErrAcquisitionPaused) {
		t.Fatalf("schedule while paused = %v, want ErrAcquisitionPaused", err)
	}
	if err := scheduler.ScheduleReplace(context.Background(), user, ids.NewTrackId()); !errors.Is(err, acqdomain.ErrAcquisitionPaused) {
		t.Fatalf("replace while paused = %v, want ErrAcquisitionPaused", err)
	}

	time.Sleep(30 * time.Millisecond)
	if _, settled := settledJob(scheduler, paused.String()); settled {
		t.Fatal("refused job settled while paused, want it never admitted")
	}
	if got := acq.calls.Load(); got != 1 {
		t.Fatalf("acquirer executions while paused = %d, want 1 (only the earlier, already-finished job)", got)
	}

	scheduler.Resume()
	if scheduler.Status().Paused {
		t.Fatal("Status().Paused = true after Resume, want false")
	}
	if err := scheduler.Schedule(context.Background(), user, paused, ""); err != nil {
		t.Fatalf("schedule after Resume = %v, want nil", err)
	}
	settled := awaitSettledJob(t, scheduler, paused.String())
	if settled.State != acqports.JobSucceeded {
		t.Errorf("resumed job state = %q, want %q (kill switch must be reversible)", settled.State, acqports.JobSucceeded)
	}
}

func TestBackgroundScheduler_ResumeClaimsWithoutWaitingAPollInterval(t *testing.T) {
	acq := &startedAcquirer{started: make(chan struct{}, 1), release: make(chan struct{})}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithPollInterval(10*time.Second))
	t.Cleanup(func() {
		close(acq.release)
		scheduler.Shutdown(context.Background())
	})

	scheduler.Pause()
	user := ids.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), user, ids.NewTrackId(), ""); !errors.Is(err, acqdomain.ErrAcquisitionPaused) {
		t.Fatalf("schedule while paused = %v, want ErrAcquisitionPaused", err)
	}
	time.Sleep(50 * time.Millisecond)
	select {
	case <-acq.started:
		t.Fatal("acquirer started while paused, want no execution")
	default:
	}

	scheduler.Resume()
	if err := scheduler.Schedule(context.Background(), user, ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule after Resume = %v, want nil", err)
	}
	select {
	case <-acq.started:
	case <-time.After(time.Second):
		t.Fatal("acquirer not started within 1s of Resume, want a wake well under the 10s poll interval")
	}
}

func TestBackgroundScheduler_PauseLeavesInflightRunning(t *testing.T) {
	ledger := &burstLedger{started: make(chan struct{}), release: make(chan struct{})}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithPollInterval(10*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	user := ids.NewUserId(uuid.New())
	running := ids.NewTrackId()
	if err := scheduler.Schedule(context.Background(), user, running, ""); err != nil {
		t.Fatalf("schedule = %v, want nil", err)
	}
	<-ledger.started

	scheduler.Pause()
	queued := ids.NewTrackId()
	if err := scheduler.Schedule(context.Background(), user, queued, ""); !errors.Is(err, acqdomain.ErrAcquisitionPaused) {
		t.Fatalf("schedule while paused = %v, want ErrAcquisitionPaused", err)
	}

	close(ledger.release)
	_ = awaitSettledJob(t, scheduler, running.String())
	time.Sleep(30 * time.Millisecond)
	if _, settled := settledJob(scheduler, queued.String()); settled {
		t.Fatal("job refused while paused settled, want it never admitted")
	}

	scheduler.Resume()
	if err := scheduler.Schedule(context.Background(), user, queued, ""); err != nil {
		t.Fatalf("schedule after Resume = %v, want nil", err)
	}
	settled := awaitSettledJob(t, scheduler, queued.String())
	if settled.State != acqports.JobSucceeded {
		t.Errorf("resumed job state = %q, want %q", settled.State, acqports.JobSucceeded)
	}
	awaitIdle(t, scheduler)
}

type recordingProgressPublisher struct {
	mu     sync.Mutex
	events []recordedProgress
}

type recordedProgress struct {
	typ     string
	payload map[string]any
}

func (p *recordingProgressPublisher) Publish(_ context.Context, _ ids.UserId, eventType events.EventType, payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, recordedProgress{typ: string(eventType), payload: payload})
}

func TestSchedulerJobReporter_PublishesProgressOnStage(t *testing.T) {
	pub := &recordingProgressPublisher{}
	log := &jobLog{jobs: map[string]*acqports.JobRecord{"t1": {TrackID: "t1"}}}
	r := schedulerJobReporter{log: log, events: pub, trackID: "t1", userId: ids.NewUserId(uuid.New())}

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
			s := NewBackgroundAcquisitionScheduler(nil, 1, opts...)
			t.Cleanup(func() {
				s.Shutdown(context.Background())
			})
			s.log.register("t1", "")
			r := schedulerJobReporter{log: s.log, events: s.events, trackID: "t1", userId: ids.NewUserId(uuid.New())}

			r.stage("search")

			if s.log.jobs["t1"].Stage != "search" {
				t.Fatalf("stage not recorded on job record")
			}
		})
	}
}

func replaceThroughTheScheduler(t *testing.T, kind acqports.JobKind, pub *recordingPublisher) {
	t.Helper()
	ledger, store, userId, trackId, _ := seedReadyTrack(t)
	svc := newTestAcquireService(ledger, NewSourceRegistry(newAudioSource{}), store, WithAcquireEvents(pub))
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1,
		WithPollInterval(5*time.Millisecond), WithSchedulerEvents(pub))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })
	scheduler.queue.(userRememberer).RememberUser(trackId, userId)

	if err := scheduler.queue.Enqueue(context.Background(), trackId, kind, 0); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, recent := scheduler.log.snapshot(); len(recent) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the replace job never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBackgroundScheduler_AUserReplaceStartsWithTheTrackTitle(t *testing.T) {
	pub := newRecordingPublisher()

	replaceThroughTheScheduler(t, acqports.JobKindReplace, pub)

	started := pub.last[events.TypeTrackAcquisitionStarted]
	if started["title"] != "Blinding Lights" || started["artist"] != "The Weeknd" {
		t.Errorf("started payload = %v, want the track's title and artist", started)
	}
	progress := pub.last[events.TypeTrackAcquisitionProgress]
	if progress["title"] != "Blinding Lights" {
		t.Errorf("progress payload = %v, want the track's title", progress)
	}
}

type blockingAcquirer struct {
	release chan struct{}
	calls   atomic.Int32
}

func (a *blockingAcquirer) Execute(context.Context, ids.UserId, ids.TrackId) error {
	a.calls.Add(1)
	<-a.release
	return nil
}

func (a *blockingAcquirer) ExecuteReplace(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
	return a.Execute(ctx, userId, trackId)
}

func (a *blockingAcquirer) RefuseQueued(context.Context, ids.UserId, ids.TrackId) {}

func TestBackgroundScheduler_PrincipalDefault_AdmitsOneUserUpToGlobalDepth(t *testing.T) {
	const concurrency = 2
	acq := &blockingAcquirer{release: make(chan struct{})}
	scheduler := NewBackgroundAcquisitionScheduler(acq, concurrency)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	t.Cleanup(func() {
		close(acq.release)
		awaitIdle(t, scheduler)
	})

	userId := ids.NewUserId(uuid.New())
	const burst = concurrency*4 + 5
	for i := 0; i < burst; i++ {
		if err := scheduler.Schedule(context.Background(), userId, ids.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d of %d for one user = %v, want nil (the queue is unbounded)", i+1, burst, err)
		}
	}
}

type recordingPublisher struct {
	mu     sync.Mutex
	byType map[events.EventType]int
	last   map[events.EventType]map[string]any
}

func newRecordingPublisher() *recordingPublisher {
	return &recordingPublisher{byType: make(map[events.EventType]int), last: make(map[events.EventType]map[string]any)}
}

func (p *recordingPublisher) Publish(_ context.Context, _ ids.UserId, eventType events.EventType, payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byType[eventType]++
	p.last[eventType] = payload
}

func (p *recordingPublisher) count(eventType events.EventType) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byType[eventType]
}

type holdOneLibraryTracks struct {
	*acquisitiontest.Ledger
	hold     ids.TrackId
	holding  chan struct{}
	release  chan struct{}
	holdOnce sync.Once
}

func (r *holdOneLibraryTracks) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*acqports.TrackSnapshot, error) {
	if id == r.hold {
		r.holdOnce.Do(func() { close(r.holding) })
		<-r.release
	}
	return r.Ledger.Get(ctx, id, userId)
}

func newPendingTrack(t *testing.T, userId ids.UserId, ledger *acquisitiontest.Ledger) ledgerTrack {
	t.Helper()
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger.Seed(track)
	return track
}

func claimableTracks(q *memJobQueue) map[string]struct{} {
	tracks := make(map[string]struct{})
	for {
		job, err := q.Claim(context.Background(), time.Minute)
		if err != nil {
			return tracks
		}
		tracks[job.TrackID.String()] = struct{}{}
	}
}

func TestBackgroundScheduler_ShutdownCancellation_LeavesTheQueuedJobUnclaimed(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	base := acquisitiontest.NewLedger()
	running := newPendingTrack(t, userId, base)
	queued := newPendingTrack(t, userId, base)

	ledger := &holdOneLibraryTracks{Ledger: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	pub := newRecordingPublisher()
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithDrainBudget(10*time.Millisecond))
	t.Cleanup(func() {
		close(ledger.release)
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), userId, running.ID, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-ledger.holding

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
	if _, ok := claimableTracks(mq)[queued.ID.String()]; !ok {
		t.Error("queued job missing from the queue after shutdown, want still enqueued for the next worker")
	}

	stored := committedTrack(t, base, queued.ID)
	if stored.Status != acqports.TrackPending {
		t.Errorf("queued track status = %v, want %v (untouched by shutdown)", stored.Status, acqports.TrackPending)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0", got)
	}
}

func TestBackgroundScheduler_ScheduleDuringShutdown_Refuses(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	base := acquisitiontest.NewLedger()
	running := newPendingTrack(t, userId, base)
	queued := newPendingTrack(t, userId, base)

	ledger := &holdOneLibraryTracks{Ledger: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	pub := newRecordingPublisher()
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithDrainBudget(10*time.Millisecond))
	t.Cleanup(func() {
		close(ledger.release)
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), userId, running.ID, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-ledger.holding
	scheduler.closed.Store(true)

	if err := scheduler.Schedule(context.Background(), userId, queued.ID, ""); !errors.Is(err, acqdomain.ErrSchedulerShutdown) {
		t.Errorf("Schedule once admission is closed = %v, want ErrSchedulerShutdown", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shutdownCancel()
	scheduler.Shutdown(shutdownCtx)

	stored := committedTrack(t, base, queued.ID)
	if stored.Status != acqports.TrackPending {
		t.Errorf("refused track status = %v, want %v (untouched)", stored.Status, acqports.TrackPending)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0", got)
	}
}

func TestAcquireTrackAudioService_RefuseQueued_AlreadySettledTrack_IsQuietNoOp(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	makeReady(&track, "audio/ref.mp3")
	ledger.Seed(track)

	pub := newRecordingPublisher()
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))

	svc.RefuseQueued(context.Background(), userId, track.ID)

	stored := committedTrack(t, ledger, track.ID)
	if stored.Status != acqports.TrackReady {
		t.Errorf("already-ready track status = %v, want %v (untouched)", stored.Status, acqports.TrackReady)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0 (quiet no-op)", got)
	}
}

func TestBackgroundScheduler_InflightDedupIsNotARefusal(t *testing.T) {
	ledger := &blockingLedger{started: make(chan struct{}), release: make(chan struct{})}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	userId, trackId := ids.NewUserId(uuid.New()), ids.NewTrackId()

	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-ledger.started
	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err != nil {
		t.Errorf("duplicate Schedule while in flight = %v, want nil (a job is already running)", err)
	}
	close(ledger.release)
	awaitIdle(t, scheduler)
}

func TestBackgroundScheduler_AfterShutdownRefuses(t *testing.T) {
	svc := newTestAcquireService(&countingLedger{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
	scheduler.Shutdown(context.Background())

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); !errors.Is(err, acqdomain.ErrSchedulerShutdown) {
		t.Errorf("Schedule after shutdown = %v, want ErrSchedulerShutdown", err)
	}
}

func newStatusTestScheduler(t *testing.T) *BackgroundAcquisitionScheduler {
	t.Helper()
	s := NewBackgroundAcquisitionScheduler(nil, 1)
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

func TestAcquisitionVerification_ToolsPresent_RequiresYtDlp(t *testing.T) {
	armed := acqports.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}
	if !armed.ToolsPresent() {
		t.Error("ToolsPresent() = false with every tool present, want true")
	}

	missingYtDlp := acqports.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true}
	if missingYtDlp.ToolsPresent() {
		t.Error("ToolsPresent() = true with yt-dlp unavailable; the startup warning never fires (defect)")
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
		svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		trackId := ids.NewTrackId()

		if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), trackId, ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}

		outcome := assertExactlyOneOutcome(t, recorder.calls)
		awaitIdle(t, scheduler)
		if outcome.TrackID != trackId.String() {
			t.Errorf("track id = %q, want %q", outcome.TrackID, trackId.String())
		}
		if outcome.Outcome != acqports.JobSucceeded {
			t.Errorf("outcome = %q, want %q", outcome.Outcome, acqports.JobSucceeded)
		}
		if outcome.ElapsedMs < 0 {
			t.Errorf("elapsed ms = %d, want >= 0", outcome.ElapsedMs)
		}
	})

	t.Run("failed", func(t *testing.T) {
		recorder := newFakeOutcomeRecorder()
		userId := ids.NewUserId(uuid.New())
		track := newLedgerTrack(userId, "Song", "Artist", "Album")
		ledger := acquisitiontest.NewLedger()
		ledger.Seed(track)
		leaking := &fakeAudioSearcher{searchErr: errors.New("boom")}
		svc := newTestAcquireService(ledger, fakeRegistry(leaking), newFakeAudioStore())
		scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})

		if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}

		outcome := assertExactlyOneOutcome(t, recorder.calls)
		awaitIdle(t, scheduler)
		if outcome.TrackID != track.ID.String() {
			t.Errorf("track id = %q, want %q", outcome.TrackID, track.ID.String())
		}
		if outcome.Outcome != acqports.JobFailed {
			t.Errorf("outcome = %q, want %q", outcome.Outcome, acqports.JobFailed)
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
		svc := newTestAcquireService(&panicLedger{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		trackId := ids.NewTrackId()

		if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), trackId, ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}

		outcome := assertExactlyOneOutcome(t, recorder.calls)
		awaitIdle(t, scheduler)
		if outcome.Outcome != acqports.JobFailed {
			t.Errorf("outcome = %q, want %q", outcome.Outcome, acqports.JobFailed)
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
		scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		userId := ids.NewUserId(uuid.New())
		running := ids.NewTrackId()
		if err := scheduler.Schedule(context.Background(), userId, running, ""); err != nil {
			t.Fatalf("first schedule: %v", err)
		}
		<-acq.started
		queued := ids.NewTrackId()

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
		awaitIdle(t, scheduler)

		runningOutcome, ok := byTrackID[running.String()]
		if !ok {
			t.Fatalf("no outcome recorded for the running track %q, got %v", running.String(), byTrackID)
		}
		if runningOutcome.Outcome != acqports.JobSucceeded {
			t.Errorf("running track outcome = %q, want %q", runningOutcome.Outcome, acqports.JobSucceeded)
		}

		outcome, ok := byTrackID[queued.String()]
		if !ok {
			t.Fatalf("no outcome recorded for the queued track %q, got %v", queued.String(), byTrackID)
		}
		if outcome.Outcome != acqports.JobSucceeded {
			t.Errorf("outcome = %q, want %q", outcome.Outcome, acqports.JobSucceeded)
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
		svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		trackId := ids.NewTrackId()

		if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), trackId, ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		awaitOutcome(t, recorder.calls)
		awaitIdle(t, scheduler)

		_, recent := scheduler.log.snapshot()
		if len(recent) != 1 || recent[0].State != acqports.JobSucceeded {
			t.Fatalf("recent jobs = %+v, want exactly one %s job (a Record error must not change the job's result)", recent, acqports.JobSucceeded)
		}
	})

	t.Run("hanging recorder does not delay the next job", func(t *testing.T) {
		recorder := newFakeOutcomeRecorder()
		recorder.delay = outcomeRecordTimeout * 3
		acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
		scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})
		userId := ids.NewUserId(uuid.New())

		if err := scheduler.Schedule(context.Background(), userId, ids.NewTrackId(), ""); err != nil {
			t.Fatalf("first schedule: %v", err)
		}
		<-acq.started
		close(acq.release)
		awaitIdle(t, scheduler)

		start := time.Now()
		if err := scheduler.Schedule(context.Background(), userId, ids.NewTrackId(), ""); err != nil {
			t.Fatalf("second schedule: %v", err)
		}
		awaitIdle(t, scheduler)
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
	svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	assertExactlyOneOutcome(t, recorder.calls)
	awaitIdle(t, scheduler)

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

func TestWithVerificationStatus_LogsDegradedOrArmed(t *testing.T) {
	cases := []struct {
		name         string
		verification acqports.Verification
		wantLevel    string
		wantMsg      string
	}{
		{"not fully armed warns", acqports.Verification{Ffprobe: true}, "WARN", "acquisition.verification_degraded"},
		{"fully armed informs", acqports.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}, "INFO", "acquisition.verification_armed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureJSONLogSync(t)

			WithVerificationStatus(tc.verification)(&BackgroundAcquisitionScheduler{})

			var record struct {
				Level string `json:"level"`
				Msg   string `json:"msg"`
			}
			if err := json.Unmarshal([]byte(logs.String()), &record); err != nil {
				t.Fatalf("expected exactly one JSON log line, got %q: %v", logs.String(), err)
			}
			if record.Level != tc.wantLevel || record.Msg != tc.wantMsg {
				t.Errorf("log = %s %s, want %s %s", record.Level, record.Msg, tc.wantLevel, tc.wantMsg)
			}
		})
	}
}

func TestWithVerificationStatus_NamesFingerprintLookupsUnverified(t *testing.T) {
	logs := captureJSONLogSync(t)

	WithVerificationStatus(acqports.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true})(&BackgroundAcquisitionScheduler{})

	var record struct {
		FingerprintLookup string `json:"fingerprint_lookup"`
	}
	if err := json.Unmarshal([]byte(logs.String()), &record); err != nil {
		t.Fatalf("expected exactly one JSON log line, got %q: %v", logs.String(), err)
	}
	if !strings.Contains(record.FingerprintLookup, "unverified") {
		t.Errorf("fingerprint_lookup = %q, want it to say unverified", record.FingerprintLookup)
	}
}

func TestBackgroundScheduler_RecordOutcome_LogsWarnOnlyWhenRecordFails(t *testing.T) {
	t.Run("failure logs the warning", func(t *testing.T) {
		logs := captureJSONLogSync(t)
		recorder := newFakeOutcomeRecorder()
		recorder.err = errors.New("outcome store unavailable")
		svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})

		if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		assertExactlyOneOutcome(t, recorder.calls)
		awaitIdle(t, scheduler)

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
		svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
		scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
		t.Cleanup(func() {
			scheduler.Shutdown(context.Background())
		})

		if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		assertExactlyOneOutcome(t, recorder.calls)
		awaitIdle(t, scheduler)
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
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	source := newBlockingSource()
	recorder := newCtxAtRecordRecorder()
	svc := newTestAcquireService(liveCtxLedger{ledger}, NewSourceRegistry(source), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder), WithDrainBudget(10*time.Millisecond))
	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	<-source.searching

	scheduler.Shutdown(context.Background())

	outcome := assertExactlyOneOutcome(t, recorder.calls)
	awaitIdle(t, scheduler)
	if outcome.TrackID != track.ID.String() {
		t.Errorf("track id = %q, want %q", outcome.TrackID, track.ID.String())
	}
	if outcome.Outcome != acqports.JobCancelled {
		t.Errorf("outcome = %q, want %q (a job cut mid-run by shutdown is released for retry, not failed)", outcome.Outcome, acqports.JobCancelled)
	}
	if outcome.Reason != "shutdown" {
		t.Errorf("reason = %q, want %q", outcome.Reason, "shutdown")
	}
	if ctxErr := <-recorder.ctxErrs; ctxErr != nil {
		t.Errorf("Record's context was already done (%v), want a live context detached from the cancelled job", ctxErr)
	}
}

func TestBackgroundScheduler_ShutdownBeforeStart_LeavesTheQueuedJobWithNoOutcome(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	base := acquisitiontest.NewLedger()
	running := newPendingTrack(t, userId, base)
	queued := newPendingTrack(t, userId, base)
	ledger := &holdOneLibraryTracks{Ledger: base, hold: running.ID, holding: make(chan struct{}), release: make(chan struct{})}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	recorder := newCtxAtRecordRecorder()
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder), WithDrainBudget(10*time.Millisecond))
	var released sync.Once
	release := func() {
		released.Do(func() { close(ledger.release) })
		awaitIdle(t, scheduler)
	}
	t.Cleanup(release)

	if err := scheduler.Schedule(context.Background(), userId, running.ID, ""); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	<-ledger.holding
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
	if only.Outcome != acqports.JobCancelled {
		t.Errorf("outcome = %q, want %q", only.Outcome, acqports.JobCancelled)
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
	svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))

	start := time.Now()
	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	awaitIdle(t, scheduler)

	scheduler.Shutdown(context.Background())
	elapsed := time.Since(start)

	if elapsed < recorder.delay {
		t.Errorf("Shutdown returned %s after Schedule, want at least the %s the in-flight outcome write takes", elapsed, recorder.delay)
	}
	assertExactlyOneOutcome(t, recorder.calls)
}

func TestBackgroundScheduler_RecordedOutcome_MatchesWhatTheJobLogShows(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	trackId := ids.NewTrackId()
	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), trackId, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	<-acq.started
	time.Sleep(30 * time.Millisecond)
	close(acq.release)

	outcome := assertExactlyOneOutcome(t, recorder.calls)
	awaitIdle(t, scheduler)
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
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	leaking := &fakeAudioSearcher{searchErr: errors.New("yt-dlp exited 1: --cookies " + cookieJarPath + ": permission denied")}
	svc := newTestAcquireService(ledger, fakeRegistry(leaking), newFakeAudioStore())
	recorder := newFakeOutcomeRecorder()
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	outcome := assertExactlyOneOutcome(t, recorder.calls)
	awaitIdle(t, scheduler)

	if outcome.Outcome != acqports.JobFailed {
		t.Fatalf("outcome = %q, want %q", outcome.Outcome, acqports.JobFailed)
	}
	if strings.Contains(outcome.Reason, cookieJarPath) {
		t.Errorf("recorded reason = %q, still names the cookie file %q", outcome.Reason, cookieJarPath)
	}
}

func TestBackgroundScheduler_FailedOutcomeWrite_LogsOneWarnWithoutTheCookiePath(t *testing.T) {
	logs := captureJSONLogSync(t)
	recorder := newFakeOutcomeRecorder()
	recorder.err = errors.New("pg write: --cookies " + cookieJarPath + ": permission denied")
	scheduler := NewBackgroundAcquisitionScheduler(&stubAcquirer{}, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	assertExactlyOneOutcome(t, recorder.calls)
	awaitIdle(t, scheduler)

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
	scheduler := NewBackgroundAcquisitionScheduler(&stubAcquirer{}, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	assertExactlyOneOutcome(t, recorder.calls)
	awaitIdle(t, scheduler)

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
	scheduler := NewBackgroundAcquisitionScheduler(&stubAcquirer{}, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	trackId := ids.NewTrackId()

	if err := scheduler.ScheduleReplace(context.Background(), ids.NewUserId(uuid.New()), trackId); err != nil {
		t.Fatalf("schedule replace: %v", err)
	}
	outcome := assertExactlyOneOutcome(t, recorder.calls)
	awaitIdle(t, scheduler)
	if outcome.TrackID != trackId.String() || outcome.Outcome != acqports.JobSucceeded {
		t.Errorf("recorded (%q, %q), want (%q, %q)", outcome.TrackID, outcome.Outcome, trackId.String(), acqports.JobSucceeded)
	}
}

func TestBackgroundScheduler_DedupedSchedule_ReplaysAndRecordsBothOutcomes(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 2, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	userId := ids.NewUserId(uuid.New())
	trackId := ids.NewTrackId()
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
	awaitIdle(t, scheduler)
	if first.TrackID != trackId.String() || second.TrackID != trackId.String() {
		t.Errorf("track ids = %q, %q, want both %q", first.TrackID, second.TrackID, trackId.String())
	}
	if got := acq.calls.Load(); got != 2 {
		t.Errorf("acquirer calls = %d, want 2 (the deduped request replays once the first run settles)", got)
	}
}

func TestBackgroundScheduler_NilOutcomeRecorder_LeavesTheJobUntouched(t *testing.T) {
	scheduler := NewBackgroundAcquisitionScheduler(&stubAcquirer{}, 1, WithOutcomeRecorder(nil))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	awaitIdle(t, scheduler)

	status := scheduler.Status()
	if status.Succeeded != 1 || len(status.Recent) != 1 || status.Recent[0].State != acqports.JobSucceeded {
		t.Errorf("status = succeeded %d, recent %+v, want one %s job", status.Succeeded, status.Recent, acqports.JobSucceeded)
	}
}

type ctxCapturingAcquirer struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	ctx     atomic.Value
}

func (a *ctxCapturingAcquirer) Execute(ctx context.Context, _ ids.UserId, _ ids.TrackId) error {
	a.ctx.Store(ctx)
	a.once.Do(func() { close(a.started) })
	select {
	case <-a.release:
	case <-ctx.Done():
	}
	return ctx.Err()
}

func (a *ctxCapturingAcquirer) ExecuteReplace(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
	return a.Execute(ctx, userId, trackId)
}

func (a *ctxCapturingAcquirer) RefuseQueued(context.Context, ids.UserId, ids.TrackId) {}

type flakyHeartbeatQueue struct {
	*memJobQueue
	heartbeatErr error
}

func (q *flakyHeartbeatQueue) Heartbeat(context.Context, ids.TrackId, acqports.ClaimNumber, time.Duration) error {
	return q.heartbeatErr
}

func TestBackgroundScheduler_HeartbeatFailuresOutlastingTheLease_CancelsTheJob(t *testing.T) {
	acq := &ctxCapturingAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	wake := make(chan struct{}, 1)
	queue := &flakyHeartbeatQueue{memJobQueue: newMemJobQueue(wake), heartbeatErr: errors.New("db blip")}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1,
		WithJobQueue(queue),
		WithHeartbeatInterval(5*time.Millisecond),
		WithLeaseDuration(20*time.Millisecond),
	)
	t.Cleanup(func() {
		close(acq.release)
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
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
	fence acqports.ClaimNumber
	delay time.Duration
}

type releaseRecordingQueue struct {
	*memJobQueue
	releases chan releaseRecord
}

func (q *releaseRecordingQueue) Release(ctx context.Context, trackID ids.TrackId, fence acqports.ClaimNumber, delay time.Duration) error {
	q.releases <- releaseRecord{fence: fence, delay: delay}
	return q.memJobQueue.Release(ctx, trackID, fence, delay)
}

func TestBackgroundScheduler_TransientFailure_ReleasesTheJobWithBackoffAndKeepsTheTrackPending(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := newPendingTrack(t, userId, ledger)
	searcher := &fakeAudioSearcher{searchErr: &acqdomain.SourceUnavailableError{Source: "fake", Err: errors.New("http error 503")}}
	pub := newRecordingPublisher()
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore(), WithAcquireEvents(pub))
	wake := make(chan struct{}, 1)
	queue := &releaseRecordingQueue{memJobQueue: newMemJobQueue(wake), releases: make(chan releaseRecord, 1)}
	queue.RememberUser(track.ID, userId)
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1,
		WithJobQueue(queue), WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

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
	if released.delay != retryBackoff(1) {
		t.Errorf("released delay = %v, want %v", released.delay, retryBackoff(1))
	}
	stored := storedTrack(t, ledger, track)
	if stored.Status != acqports.TrackPending {
		t.Errorf("status = %v, want %v", stored.Status, acqports.TrackPending)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0", got)
	}
}

func TestBackgroundScheduler_PauseRefusesAdmissionUntilResume(t *testing.T) {
	acq := &startedAcquirer{started: make(chan struct{}, 2), release: make(chan struct{})}
	close(acq.release)
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithPollInterval(10*time.Millisecond))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	user := ids.NewUserId(uuid.New())
	scheduler.Pause()

	refused := ids.NewTrackId()
	if err := scheduler.Schedule(context.Background(), user, refused, ""); !errors.Is(err, acqdomain.ErrAcquisitionPaused) {
		t.Fatalf("schedule while paused = %v, want ErrAcquisitionPaused", err)
	}
	if err := scheduler.ScheduleReplace(context.Background(), user, ids.NewTrackId()); !errors.Is(err, acqdomain.ErrAcquisitionPaused) {
		t.Fatalf("replace while paused = %v, want ErrAcquisitionPaused", err)
	}
	if _, queued := settledJob(scheduler, refused.String()); queued {
		t.Fatal("refused job settled, want it never enqueued")
	}

	scheduler.Resume()
	accepted := ids.NewTrackId()
	if err := scheduler.Schedule(context.Background(), user, accepted, ""); err != nil {
		t.Fatalf("schedule after Resume = %v, want nil", err)
	}
	if settled := awaitSettledJob(t, scheduler, accepted.String()); settled.State != acqports.JobSucceeded {
		t.Errorf("post-resume job state = %q, want %q", settled.State, acqports.JobSucceeded)
	}
}

type backlogQueue struct {
	*memJobQueue
	pending   int
	oldestAge time.Duration
}

func (q backlogQueue) PendingDepth(context.Context) (int, time.Duration, error) {
	return q.pending, q.oldestAge, nil
}

func TestSchedulerStatus_QueueDepthIsThePendingBacklogNotInFlight(t *testing.T) {
	queue := backlogQueue{memJobQueue: newMemJobQueue(nil), pending: 5, oldestAge: 90 * time.Second}
	s := NewBackgroundAcquisitionScheduler(nil, 2, WithJobQueue(queue))
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	s.inflightCount.Add(2)

	got := s.Status()
	if got.QueueDepth != 5 {
		t.Errorf("QueueDepth = %d, want 5 pending jobs", got.QueueDepth)
	}
	if got.InFlight != 2 {
		t.Errorf("InFlight = %d, want 2", got.InFlight)
	}
	if got.OldestPendingAge != 90*time.Second {
		t.Errorf("OldestPendingAge = %v, want 90s", got.OldestPendingAge)
	}
}

type failingDepthQueue struct{ *memJobQueue }

func (failingDepthQueue) PendingDepth(context.Context) (int, time.Duration, error) {
	return 0, 0, errors.New("db down")
}

func TestSchedulerStatus_QueueDepthKnownFalseWhenDepthReadFails(t *testing.T) {
	s := NewBackgroundAcquisitionScheduler(nil, 2, WithJobQueue(failingDepthQueue{newMemJobQueue(nil)}))
	t.Cleanup(func() { s.Shutdown(context.Background()) })

	if got := s.Status(); got.QueueDepthKnown {
		t.Errorf("QueueDepthKnown = true after a failed depth read, want false")
	}
}

func TestSchedulerStatus_QueueDepthKnownTrueWhenDepthReadSucceeds(t *testing.T) {
	queue := backlogQueue{memJobQueue: newMemJobQueue(nil)}
	s := NewBackgroundAcquisitionScheduler(nil, 2, WithJobQueue(queue))
	t.Cleanup(func() { s.Shutdown(context.Background()) })

	if got := s.Status(); !got.QueueDepthKnown {
		t.Errorf("QueueDepthKnown = false after a successful depth read, want true")
	}
}

type conflictingQueue struct {
	*memJobQueue
	enqueues  atomic.Int32
	rejectAll bool
	gate      chan struct{}
}

func (q *conflictingQueue) Enqueue(ctx context.Context, trackID ids.TrackId, kind acqports.JobKind, delay time.Duration) error {
	if n := q.enqueues.Add(1); q.rejectAll || n > 1 {
		return acqdomain.ErrJobKindConflict
	}
	return q.memJobQueue.Enqueue(ctx, trackID, kind, delay)
}

func (q *conflictingQueue) Claim(ctx context.Context, lease time.Duration) (acqports.Job, error) {
	select {
	case <-q.gate:
		return q.memJobQueue.Claim(ctx, lease)
	default:
		return acqports.Job{}, acqdomain.ErrNoJobAvailable
	}
}

func countCorrIDs(s *BackgroundAcquisitionScheduler) int {
	n := 0
	s.corrIDs.Range(func(_, _ any) bool { n++; return true })
	return n
}

func TestBackgroundScheduler_Schedule_RejectedEnqueueLeavesNoCorrelationID(t *testing.T) {
	queue := &conflictingQueue{memJobQueue: newMemJobQueue(make(chan struct{}, 1)), rejectAll: true, gate: make(chan struct{})}
	scheduler := NewBackgroundAcquisitionScheduler(&stubAcquirer{}, 1, WithJobQueue(queue))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	ctx := logging.WithCorrelationID(context.Background(), "corr-rejected")
	err := scheduler.Schedule(ctx, ids.NewUserId(uuid.New()), ids.NewTrackId(), "")

	if !errors.Is(err, acqdomain.ErrTrackJobInFlight) {
		t.Fatalf("schedule = %v, want ErrTrackJobInFlight", err)
	}
	if got := countCorrIDs(scheduler); got != 0 {
		t.Errorf("correlation ids retained after a rejected enqueue = %d, want 0", got)
	}
}

func TestBackgroundScheduler_Schedule_RejectedSecondRequestKeepsTheQueuedJobsCorrelationID(t *testing.T) {
	ledger := &corrCapturingLedger{Ledger: acquisitiontest.NewLedger()}
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	queue := &conflictingQueue{memJobQueue: newMemJobQueue(make(chan struct{}, 1)), gate: make(chan struct{})}
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1,
		WithJobQueue(queue), WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	userId := ids.NewUserId(uuid.New())
	trackId := ids.NewTrackId()
	first := logging.WithCorrelationID(context.Background(), "corr-first")
	second := logging.WithCorrelationID(context.Background(), "corr-second")
	if err := scheduler.Schedule(first, userId, trackId, ""); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	if err := scheduler.Schedule(second, userId, trackId, ""); !errors.Is(err, acqdomain.ErrTrackJobInFlight) {
		t.Fatalf("second schedule = %v, want ErrTrackJobInFlight", err)
	}
	close(queue.gate)
	awaitIdle(t, scheduler)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ledger.mu.Lock()
		seen := ledger.seen
		ledger.mu.Unlock()
		if seen {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.corrID != "corr-first" {
		t.Errorf("claimed job ran under correlation id %q, want %q", ledger.corrID, "corr-first")
	}
}

func TestBackgroundScheduler_Schedule_RunsTheJobWhenTheSchedulerClockIsAnHourAhead(t *testing.T) {
	acq := &startedAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	close(acq.release)
	skewed := func(s *BackgroundAcquisitionScheduler) {
		s.now = func() time.Time { return time.Now().Add(time.Hour) }
		s.queue = newMemJobQueue(s.wake)
	}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, skewed)
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	trackID := ids.NewTrackId()

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), trackID, ""); err != nil {
		t.Fatalf("Schedule = %v, want nil", err)
	}

	settled := awaitSettledJob(t, scheduler, trackID.String())
	if settled.State != acqports.JobSucceeded {
		t.Errorf("job state = %q, want %q (claimability follows the queue clock, not the scheduler's)", settled.State, acqports.JobSucceeded)
	}
}

type claimSignalQueue struct {
	acqports.DownloadQueue
	claims  atomic.Int64
	claimed chan struct{}
}

func (q *claimSignalQueue) Claim(ctx context.Context, lease time.Duration) (acqports.Job, error) {
	job, err := q.DownloadQueue.Claim(ctx, lease)
	if err == nil {
		q.claims.Add(1)
		close(q.claimed)
	}
	return job, err
}

func seededPendingQueue(t *testing.T) *claimSignalQueue {
	t.Helper()
	mem := newMemJobQueue(nil)
	trackID := ids.NewTrackId()
	mem.RememberUser(trackID, ids.NewUserId(uuid.New()))
	if err := mem.Enqueue(context.Background(), trackID, acqports.JobKindAcquire, 0); err != nil {
		t.Fatalf("seed pending job: %v", err)
	}
	return &claimSignalQueue{DownloadQueue: mem, claimed: make(chan struct{})}
}

func driveWorkerLoops(s *BackgroundAcquisitionScheduler, loops int) {
	for range loops + 1 {
		s.lanes.acquire <- struct{}{}
	}
}

func TestBackgroundScheduler_StartedPausedNeverClaimsASeededJobUntilResume(t *testing.T) {
	queue := seededPendingQueue(t)
	acq := &stubAcquirer{}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1,
		WithJobQueue(queue),
		WithPollInterval(time.Hour),
		WithSchedulerPaused(true))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	if !scheduler.Status().Paused {
		t.Fatal("Status().Paused = false right after construction, want true")
	}
	driveWorkerLoops(scheduler, 3)
	if n := queue.claims.Load(); n != 0 {
		t.Fatalf("paused scheduler claimed %d job(s) before Resume, want 0", n)
	}

	scheduler.Resume()
	select {
	case <-queue.claimed:
	case <-time.After(5 * time.Second):
		t.Fatal("seeded job was not claimed after Resume")
	}
}

func TestBackgroundScheduler_AClaimedJobRecordsItsQueueWait(t *testing.T) {
	svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1)
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	job := acqports.Job{TrackID: ids.NewTrackId(), UserID: ids.NewUserId(uuid.New()), Waited: 3 * time.Second}
	scheduler.startClaimedJob(context.Background(), job.TrackID.String(), job)
	record := scheduler.log.complete(job.TrackID.String(), acqports.JobSucceeded, "")

	if got := record.StageMs["queue"]; got != 3000 {
		t.Errorf("queue stage = %dms, want 3000", got)
	}
}

func TestBackgroundScheduler_OutcomeCarriesStageTimesSourceHostAndFileFacts(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	scheduler.recordOutcome(context.Background(), acqports.JobRecord{
		TrackID:        "t1",
		State:          acqports.JobSucceeded,
		ResolvedSource: "https://music.youtube.com/watch?v=XDC3uH1FSgw",
		StageMs:        map[string]int64{"search": 500},
		Source:         acqports.SourceFormat{ID: "251", Codec: "opus", Kbps: 130},
		Stored:         acqports.StoredFile{Bytes: 4096, Codec: "mp3"},
	})

	got := assertExactlyOneOutcome(t, recorder.calls)
	if got.SourceHost != "music.youtube.com" {
		t.Errorf("source host = %q, want music.youtube.com", got.SourceHost)
	}
	if got.StageMs["search"] != 500 || got.Source.ID != "251" || got.Stored.Bytes != 4096 {
		t.Errorf("outcome = %+v, want the job's stage times and file facts", got)
	}
}

func TestBackgroundScheduler_AFailedOutcomeNamesNoSourceHost(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	scheduler.recordOutcome(context.Background(), acqports.JobRecord{
		TrackID:        "t1",
		State:          acqports.JobFailed,
		ResolvedSource: "https://music.youtube.com/watch?v=XDC3uH1FSgw",
	})

	got := assertExactlyOneOutcome(t, recorder.calls)
	if got.SourceHost != "" {
		t.Errorf("source host = %q, want empty: a failed job stored nothing", got.SourceHost)
	}
}

type orderedAcquirer struct {
	stubAcquirer
	releaseFirst chan struct{}
	firstStarted chan struct{}
	firstOnce    sync.Once
}

func (a *orderedAcquirer) Execute(context.Context, ids.UserId, ids.TrackId) error {
	a.firstOnce.Do(func() {
		close(a.firstStarted)
		<-a.releaseFirst
	})
	a.record("Execute")
	return nil
}

func (a *orderedAcquirer) ExecuteUpgrade(context.Context, ids.UserId, ids.TrackId) error {
	a.record("ExecuteUpgrade")
	return nil
}

func TestBackgroundScheduler_UpgradeJobsRunOnTheUpgradeLaneAfterEveryWaitingUserAdd(t *testing.T) {
	acq := &orderedAcquirer{releaseFirst: make(chan struct{}), firstStarted: make(chan struct{})}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1,
		WithPollInterval(5*time.Millisecond),
		WithUpgradeLane(acq, 1))
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(acq.releaseFirst) }) }
	t.Cleanup(func() {
		release()
		scheduler.Shutdown(context.Background())
	})
	ctx := context.Background()
	user := ids.NewUserId(uuid.New())

	if err := scheduler.Schedule(ctx, user, ids.NewTrackId(), ""); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	<-acq.firstStarted
	if err := scheduler.Schedule(ctx, user, ids.NewTrackId(), ""); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := scheduler.ScheduleUpgrade(ctx, user, ids.NewTrackId()); err != nil {
		t.Fatalf("ScheduleUpgrade: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if ran := acq.entryPointsRun(); len(ran) != 0 {
		t.Fatalf("ran %v while the user's add was still waiting, want the upgrade held back", ran)
	}
	release()
	awaitIdle(t, scheduler)

	got := acq.entryPointsRun()
	want := []string{"Execute", "Execute", "ExecuteUpgrade"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("entry points run = %v, want %v: user adds first, the upgrade last", got, want)
	}
}

type keptUpgrader struct{ stubAcquirer }

func (*keptUpgrader) ExecuteUpgrade(context.Context, ids.UserId, ids.TrackId) error {
	return replacement.ErrKept
}

func TestBackgroundScheduler_AKeptUpgradeIsRecordedAsKeptNotAsSuccessOrFailure(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	acq := &keptUpgrader{}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1,
		WithPollInterval(5*time.Millisecond),
		WithOutcomeRecorder(recorder),
		WithUpgradeLane(acq, 1))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	if err := scheduler.ScheduleUpgrade(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId()); err != nil {
		t.Fatalf("ScheduleUpgrade: %v", err)
	}
	awaitIdle(t, scheduler)

	got := assertExactlyOneOutcome(t, recorder.calls)
	if got.Outcome != acqports.JobKept {
		t.Errorf("outcome = %q, want %q", got.Outcome, acqports.JobKept)
	}
	if status := scheduler.Status(); status.Succeeded != 0 || status.Failed != 0 {
		t.Errorf("succeeded = %d, failed = %d, want a kept file to move neither", status.Succeeded, status.Failed)
	}
}

type committingUpgrader struct {
	stubAcquirer
	step       *UpdateTrackStep
	started    chan struct{}
	proceed    chan struct{}
	commitErr  chan error
	startedOne sync.Once
}

func (u *committingUpgrader) ExecuteUpgrade(ctx context.Context, _ ids.UserId, _ ids.TrackId) error {
	u.startedOne.Do(func() { close(u.started) })
	<-u.proceed
	_, err := u.step.Execute(ctx, &AcquisitionContext{AudioRef: "upgraded.mp3"}, afterStore{kept: acqports.AudioCandidate{URL: "https://www.youtube.com/watch?v=upgradedAAA"}})
	u.commitErr <- err
	return err
}

func TestBackgroundScheduler_AnUpgradeTakenOverByAReplaceDuringItsDownloadNeverCommits(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	acq := &committingUpgrader{
		step:      NewUpdateTrackStep(ledger, userId, track.ID),
		started:   make(chan struct{}),
		proceed:   make(chan struct{}),
		commitErr: make(chan error, 1),
	}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 2,
		WithPollInterval(5*time.Millisecond),
		WithUpgradeLane(acq, 1))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })
	ctx := context.Background()
	if err := scheduler.ScheduleUpgrade(ctx, userId, track.ID); err != nil {
		t.Fatalf("ScheduleUpgrade: %v", err)
	}
	<-acq.started

	if err := scheduler.ScheduleReplace(ctx, userId, track.ID); err != nil {
		t.Fatalf("ScheduleReplace over a running upgrade: %v", err)
	}
	close(acq.proceed)

	if commitErr := <-acq.commitErr; !errors.Is(commitErr, acqdomain.ErrLeaseLost) {
		t.Errorf("upgrade commit = %v, want ErrLeaseLost", commitErr)
	}
	if status := storedTrack(t, ledger, track).Status; status != acqports.TrackPending {
		t.Errorf("track status = %v, want the upgrade to have written nothing", status)
	}
}

type queueCallRecorder struct {
	*memJobQueue
	released atomic.Int32
	settled  atomic.Int32
}

func (q *queueCallRecorder) Release(ctx context.Context, trackID ids.TrackId, fence acqports.ClaimNumber, delay time.Duration) error {
	q.released.Add(1)
	return q.memJobQueue.Release(ctx, trackID, fence, delay)
}

func (q *queueCallRecorder) Settle(ctx context.Context, trackID ids.TrackId, fence acqports.ClaimNumber) error {
	q.settled.Add(1)
	return q.memJobQueue.Settle(ctx, trackID, fence)
}

func TestBackgroundScheduler_APanickingReplaceAtTheRunCapIsSettled(t *testing.T) {
	acq := &panickingAcquirer{}
	queue := &queueCallRecorder{memJobQueue: newMemJobQueue(make(chan struct{}, 1))}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithJobQueue(queue))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	job := acqports.Job{TrackID: ids.NewTrackId(), UserID: ids.NewUserId(uuid.New()), Kind: acqports.JobKindReplace, Run: maxAcquisitionAttempts}

	scheduler.releasePanickedJob(job)

	if got := queue.released.Load(); got != 0 {
		t.Errorf("released %d times, want 0: a released replace stays claimable on a ready track", got)
	}
	if got := queue.settled.Load(); got != 1 {
		t.Errorf("settled %d times, want 1", got)
	}
}

type panicQueueRecorder struct {
	*memJobQueue
	mu      sync.Mutex
	delays  []time.Duration
	settled atomic.Int32
}

func (q *panicQueueRecorder) Release(ctx context.Context, trackID ids.TrackId, fence acqports.ClaimNumber, delay time.Duration) error {
	q.mu.Lock()
	q.delays = append(q.delays, delay)
	q.mu.Unlock()
	return q.memJobQueue.Release(ctx, trackID, fence, delay)
}

func (q *panicQueueRecorder) Settle(ctx context.Context, trackID ids.TrackId, fence acqports.ClaimNumber) error {
	q.settled.Add(1)
	return q.memJobQueue.Settle(ctx, trackID, fence)
}

func (q *panicQueueRecorder) releaseDelays() []time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]time.Duration(nil), q.delays...)
}

func panickedJobOutcome(t *testing.T, job acqports.Job) (*panickingAcquirer, *panicQueueRecorder) {
	t.Helper()
	acq := &panickingAcquirer{}
	queue := &panicQueueRecorder{memJobQueue: newMemJobQueue(make(chan struct{}, 1))}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1, WithJobQueue(queue))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	job.TrackID = ids.NewTrackId()
	job.UserID = ids.NewUserId(uuid.New())

	scheduler.releasePanickedJob(job)

	return acq, queue
}

func TestBackgroundScheduler_APanickedJobAtOrPastTheRunCapLeavesTheQueueWhateverItsKind(t *testing.T) {
	kinds := []acqports.JobKind{acqports.JobKindAcquire, acqports.JobKindReplace, acqports.JobKindUpgrade}
	runs := []struct {
		name string
		run  int
	}{{"at the cap", 3}, {"one past the cap", 4}, {"past the shift overflow", 17}, {"far past the cap", 1000}}
	for _, kind := range kinds {
		for _, r := range runs {
			run := r.run
			t.Run(string(kind)+"/"+r.name, func(t *testing.T) {
				acq, queue := panickedJobOutcome(t, acqports.Job{Kind: kind, Run: run})

				if got := queue.releaseDelays(); len(got) != 0 {
					t.Errorf("released with %v, want no Release: a released job at the cap stays claimable", got)
				}
				if got := queue.settled.Load(); got != 1 {
					t.Errorf("settled %d times, want 1", got)
				}
				if got := acq.refused.Load(); got != 1 {
					t.Errorf("refused %d times, want 1", got)
				}
			})
		}
	}
}

func TestBackgroundScheduler_APanickedJobThatLostItsLeaseDoesNotFailTheTrack(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	track.ClaimNumber = 1000
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	queue := settleKeepsPendingQueue{newMemJobQueue(make(chan struct{}, 1))}
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithJobQueue(queue))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	job := acqports.Job{TrackID: track.ID, UserID: userId, ClaimNumber: 1, Run: maxAcquisitionAttempts}

	scheduler.releasePanickedJob(job)

	if got := committedTrack(t, ledger, track.ID).Status; got != acqports.TrackPending {
		t.Errorf("status = %v, want pending: the panicked job no longer holds the lease", got)
	}
	if got := committedTrack(t, ledger, track.ID).FailureReason; got != "" {
		t.Errorf("failure_reason = %q, want none", got)
	}
}

func TestBackgroundScheduler_APanickedJobBelowTheRunCapIsRetriedAfterItsBackoff(t *testing.T) {
	cases := []struct {
		name string
		kind acqports.JobKind
		run  int
		want time.Duration
	}{
		{"acquire/first run", acqports.JobKindAcquire, 1, 30 * time.Second},
		{"acquire/second run", acqports.JobKindAcquire, 2, 2 * time.Minute},
		{"replace/first run", acqports.JobKindReplace, 1, 30 * time.Second},
		{"replace/second run", acqports.JobKindReplace, 2, 2 * time.Minute},
		{"upgrade/first run", acqports.JobKindUpgrade, 1, 30 * time.Second},
		{"upgrade/second run", acqports.JobKindUpgrade, 2, 2 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acq, queue := panickedJobOutcome(t, acqports.Job{Kind: tc.kind, Run: tc.run})

			if got := queue.releaseDelays(); len(got) != 1 || got[0] != tc.want {
				t.Errorf("release delays = %v, want exactly [%v]", got, tc.want)
			}
			if got := queue.settled.Load(); got != 0 {
				t.Errorf("settled %d times, want 0: below the cap the job keeps its retry", got)
			}
			if got := acq.refused.Load(); got != 0 {
				t.Errorf("refused %d times, want 0", got)
			}
		})
	}
}

type retryableAcquirer struct{}

func (retryableAcquirer) Execute(context.Context, ids.UserId, ids.TrackId) error {
	return ErrAcquisitionRetryable
}

func (retryableAcquirer) ExecuteReplace(context.Context, ids.UserId, ids.TrackId) error {
	return ErrAcquisitionRetryable
}

func (retryableAcquirer) RefuseQueued(context.Context, ids.UserId, ids.TrackId) {}

func TestBackgroundScheduler_ARetryableJobIsRetryingNotFailed(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	scheduler := NewBackgroundAcquisitionScheduler(retryableAcquirer{}, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	deadline := time.Now().Add(jobSettleTimeout)
	for len(scheduler.Status().Recent) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	status := scheduler.Status()
	if len(status.Recent) == 0 || status.Recent[0].State != acqports.JobRetrying {
		t.Fatalf("recent = %+v, want one %s job", status.Recent, acqports.JobRetrying)
	}
	if status.Failed != 0 {
		t.Errorf("failed = %d, want 0", status.Failed)
	}
	select {
	case o := <-recorder.calls:
		t.Errorf("recorder received %+v, want no outcome row for a retrying job", o)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBackgroundScheduler_ALeaseLostJobRecordsCancelled(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	acq := &ctxCapturingAcquirer{started: make(chan struct{}), release: make(chan struct{})}
	queue := &flakyHeartbeatQueue{memJobQueue: newMemJobQueue(make(chan struct{}, 1)), heartbeatErr: acqdomain.ErrLeaseLost}
	scheduler := NewBackgroundAcquisitionScheduler(acq, 1,
		WithJobQueue(queue),
		WithHeartbeatInterval(5*time.Millisecond),
		WithPollInterval(5*time.Millisecond),
		WithOutcomeRecorder(recorder),
	)
	t.Cleanup(func() {
		close(acq.release)
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	outcome := awaitOutcome(t, recorder.calls)
	if outcome.Outcome != acqports.JobCancelled || outcome.Reason != "lease_lost" {
		t.Errorf("outcome = %q reason %q, want %q reason lease_lost", outcome.Outcome, outcome.Reason, acqports.JobCancelled)
	}
	if failed := scheduler.Status().Failed; failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
}

func TestScheduler_OutcomeCarriesJobKindRunAndProvenance(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	svc := newTestAcquireService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

	job := acqports.Job{TrackID: ids.NewTrackId(), UserID: ids.NewUserId(uuid.New()), Kind: acqports.JobKindReplace, Run: 2}
	key := job.TrackID.String()
	reportedCtx := scheduler.startClaimedJob(context.Background(), key, job)
	jobReporterFrom(reportedCtx).provenance("corroborated")
	scheduler.recordOutcome(reportedCtx, scheduler.log.complete(key, acqports.JobSucceeded, ""))

	got := assertExactlyOneOutcome(t, recorder.calls)
	if got.Kind != acqports.JobKindReplace || got.Run != 2 || got.Provenance != "corroborated" {
		t.Errorf("outcome kind/run/provenance = %q/%d/%q, want replace/2/corroborated", got.Kind, got.Run, got.Provenance)
	}
}

func TestBackgroundScheduler_AJobWhoseLeaseWasReclaimedLeavesTheTrackPending(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	track.ClaimNumber = 1000
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{searchErr: errors.New("boom")}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	assertExactlyOneOutcome(t, recorder.calls)
	awaitIdle(t, scheduler)
	if got := committedTrack(t, ledger, track.ID).Status; got != acqports.TrackPending {
		t.Errorf("status = %v, want pending: the job ran on a fence the track no longer carries", got)
	}
	if got := committedTrack(t, ledger, track.ID).FailureReason; got != "" {
		t.Errorf("failure_reason = %q, want none", got)
	}
}

func TestBackgroundScheduler_ALedgerRefusalForAStaleFenceRecordsCancelledNotFailed(t *testing.T) {
	recorder := newFakeOutcomeRecorder()
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	track.ClaimNumber = 1000
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{searchErr: errors.New("boom")}), newFakeAudioStore())
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	outcome := assertExactlyOneOutcome(t, recorder.calls)
	awaitIdle(t, scheduler)
	if outcome.Outcome != acqports.JobCancelled || outcome.Reason != leaseLostReason {
		t.Errorf("outcome = %q/%q, want %q/%q", outcome.Outcome, outcome.Reason, acqports.JobCancelled, leaseLostReason)
	}
}

func TestExecute_ALedgerRefusalForAStaleFenceSurfacesErrLeaseLostWithTheCause(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	track.ClaimNumber = 2
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	cause := errors.New("boom")
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{searchErr: cause}), newFakeAudioStore())

	err := svc.Execute(withJobFence(context.Background(), 1), userId, track.ID)

	if !errors.Is(err, acqdomain.ErrLeaseLost) || !errors.Is(err, cause) {
		t.Errorf("err = %v, want both ErrLeaseLost and the pipeline's cause", err)
	}
}

func TestScheduler_OutcomeCarriesTheRunsConfidenceSourceAndLengths(t *testing.T) {
	const target = 215.0
	stored := namedCandidate("https://youtube.com/watch?v=abcdefghijk")
	stored.Duration = 214
	audio := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o600); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	cases := []struct {
		name         string
		searcher     *fakeAudioSearcher
		prober       *queueProber
		wantSource   string
		wantMeasured float64
		wantScored   bool
	}{
		{"stored file", &fakeAudioSearcher{searchResults: []acqports.AudioCandidate{stored}, downloadPath: audio}, &queueProber{durations: []float64{213.4, 213.4}}, sourceKey(stored.URL), 213.4, true},
		{"no match: target, no measurement", &fakeAudioSearcher{}, &queueProber{}, "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newFakeOutcomeRecorder()
			userId := ids.NewUserId(uuid.New())
			ledger := acquisitiontest.NewLedger()
			track := pendingTrack(t, ledger, userId)
			track = withCatalogLength(t, ledger, track, target)
			svc := newTestAcquireService(ledger, fakeRegistry(tc.searcher), newFakeAudioStore(), WithAudioProber(tc.prober))
			scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithOutcomeRecorder(recorder))
			t.Cleanup(func() { scheduler.Shutdown(context.Background()) })

			if err := scheduler.Schedule(context.Background(), userId, track.ID, ""); err != nil {
				t.Fatalf("schedule: %v", err)
			}

			got := assertExactlyOneOutcome(t, recorder.calls)
			awaitIdle(t, scheduler)
			if (got.Confidence > 0) != tc.wantScored || got.SourceKey != tc.wantSource || got.TargetSeconds != target || got.MeasuredSeconds != tc.wantMeasured {
				t.Errorf("outcome = %v/%q/%v/%v, want scored=%v/%q/%v/%v", got.Confidence, got.SourceKey, got.TargetSeconds, got.MeasuredSeconds, tc.wantScored, tc.wantSource, float64(target), tc.wantMeasured)
			}
		})
	}
}

func fencedPendingTrack(t *testing.T, fence int) (*acquisitiontest.Ledger, ledgerTrack) {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	track.ClaimNumber = acqports.ClaimNumber(fence)
	ledger.Seed(track)
	return ledger, track
}

func panicThroughTheLedger(t *testing.T, store acqports.LibraryTracks, job acqports.Job) (*queueCallRecorder, *recordingPublisher) {
	t.Helper()
	pub := newRecordingPublisher()
	svc := newTestAcquireService(store, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), WithAcquireEvents(pub))
	queue := &queueCallRecorder{memJobQueue: newMemJobQueue(make(chan struct{}, 1))}
	scheduler := NewBackgroundAcquisitionScheduler(svc, 1, WithJobQueue(queue))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})
	scheduler.releasePanickedJob(job)
	return queue, pub
}

func TestBackgroundScheduler_APanickedJobAtTheRunCapThatStillHoldsItsLeaseFailsTheTrack(t *testing.T) {
	ledger, track := fencedPendingTrack(t, 7)
	job := acqports.Job{TrackID: track.ID, UserID: track.UserId, ClaimNumber: 7, Run: maxAcquisitionAttempts}

	_, pub := panicThroughTheLedger(t, ledger, job)

	if got := storedTrack(t, ledger, track).Status; got != acqports.TrackFailed {
		t.Errorf("status = %v, want failed: the job still holds the lease", got)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 1 {
		t.Errorf("track_acquisition_failed publishes = %d, want 1", got)
	}
}

func TestBackgroundScheduler_APanickedJobOneFenceBehindDoesNotFailTheTrackOrAnnounceIt(t *testing.T) {
	ledger, track := fencedPendingTrack(t, 7)
	job := acqports.Job{TrackID: track.ID, UserID: track.UserId, ClaimNumber: 6, Run: maxAcquisitionAttempts}

	_, pub := panicThroughTheLedger(t, ledger, job)

	stored := storedTrack(t, ledger, track)
	if stored.Status != acqports.TrackPending {
		t.Errorf("status = %v, want pending: a fence one behind has lost the lease", stored.Status)
	}
	if stored.FailureReason != "" {
		t.Errorf("failure_reason = %q, want none", stored.FailureReason)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("track_acquisition_failed publishes = %d, want 0: nothing failed", got)
	}
}

func TestBackgroundScheduler_APanickedJobBelowTheRunCapLeavesTheTrackPendingForItsRetry(t *testing.T) {
	ledger, track := fencedPendingTrack(t, 7)
	job := acqports.Job{TrackID: track.ID, UserID: track.UserId, ClaimNumber: 7, Run: maxAcquisitionAttempts - 1}

	queue, _ := panicThroughTheLedger(t, ledger, job)

	if got := storedTrack(t, ledger, track).Status; got != acqports.TrackPending {
		t.Errorf("status = %v, want pending: below the cap the job is retried", got)
	}
	if got := queue.released.Load(); got != 1 {
		t.Errorf("released %d times, want 1", got)
	}
	if got := queue.settled.Load(); got != 0 {
		t.Errorf("settled %d times, want 0", got)
	}
}

func TestBackgroundScheduler_AnUnfencedPanickedJobAtTheRunCapStillFailsTheTrack(t *testing.T) {
	ledger, track := fencedPendingTrack(t, 1000)
	job := acqports.Job{TrackID: track.ID, UserID: track.UserId, ClaimNumber: 0, Run: maxAcquisitionAttempts}

	panicThroughTheLedger(t, ledger, job)

	if got := storedTrack(t, ledger, track).Status; got != acqports.TrackFailed {
		t.Errorf("status = %v, want failed: an unfenced job is not checked against the lease", got)
	}
}

func TestBackgroundScheduler_APanickedJobAtTheRunCapSurvivesARefusalThatPanicsAndLeavesTheQueue(t *testing.T) {
	job := acqports.Job{TrackID: ids.NewTrackId(), UserID: ids.NewUserId(uuid.New()), ClaimNumber: 7, Run: maxAcquisitionAttempts}

	var queue *queueCallRecorder
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic escaped the scheduler: %v", r)
			}
		}()
		queue, _ = panicThroughTheLedger(t, &panicLedger{}, job)
	}()

	if queue == nil {
		t.Fatal("panicThroughTheLedger returned no queue recorder")
	}
	if got := queue.settled.Load(); got != 1 {
		t.Errorf("settled %d times, want 1: the job at the cap still leaves the queue", got)
	}
}

func TestBackgroundScheduler_APanickedReplaceOrUpgradeThatLostItsLeaseLeavesTheTrackPending(t *testing.T) {
	for _, kind := range []acqports.JobKind{acqports.JobKindReplace, acqports.JobKindUpgrade} {
		t.Run(string(kind), func(t *testing.T) {
			ledger, track := fencedPendingTrack(t, 1000)
			job := acqports.Job{TrackID: track.ID, UserID: track.UserId, Kind: kind, ClaimNumber: 1, Run: maxAcquisitionAttempts}

			panicThroughTheLedger(t, ledger, job)

			if got := storedTrack(t, ledger, track).Status; got != acqports.TrackPending {
				t.Errorf("status = %v, want pending: the panicked %s job no longer holds the lease", got, kind)
			}
		})
	}
}

func TestBackgroundScheduler_APanickedUpgradeAtTheRunCapThatHoldsItsLeaseLeavesAReadyTrackReady(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	makeReady(&track, "audio/ref.mp3")
	track.ClaimNumber = 7
	ledger.Seed(track)
	job := acqports.Job{TrackID: track.ID, UserID: userId, Kind: acqports.JobKindUpgrade, ClaimNumber: 7, Run: maxAcquisitionAttempts}

	panicThroughTheLedger(t, ledger, job)

	if got := storedTrack(t, ledger, track).Status; got != acqports.TrackReady {
		t.Errorf("status = %v, want ready: a failed upgrade never un-readies the playing file", got)
	}
}

func (p *pendingTracker) size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.set)
}

func awaitIdle(t *testing.T, s *BackgroundAcquisitionScheduler) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for s.pending.size() > 0 {
		if s.closed.Load() {
			s.jobs.Wait()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduler still has %d pending jobs after 30s", s.pending.size())
		}
		time.Sleep(time.Millisecond)
	}
}
