package service_test

import (
	"altune/go-api/internal/download/internal/adapters/inmemoryledger"
	"altune/go-api/internal/download/internal/adapters/inmemoryqueue"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/service/jobrun"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

type ranTracks struct {
	mu  sync.Mutex
	ran []ids.TrackId
}

func (r *ranTracks) record(id ids.TrackId) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, id)
}

func (r *ranTracks) has(id ids.TrackId) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.ran, id)
}

type recordingAcquirer struct{ ranTracks }

func (a *recordingAcquirer) Execute(_ context.Context, job jobrun.Job) error {
	a.record(job.TrackID)
	return nil
}

func (a *recordingAcquirer) ExecuteReplace(context.Context, jobrun.Job) error {
	return nil
}

func (a *recordingAcquirer) RefuseQueued(context.Context, jobrun.Job) {}

func (r *ranTracks) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ran)
}

type recordingUpgrader struct{ ranTracks }

func (u *recordingUpgrader) ExecuteUpgrade(_ context.Context, job jobrun.Job) error {
	u.record(job.TrackID)
	return nil
}

type holdingAcquirer struct {
	ranTracks
	release chan struct{}
}

func (a *holdingAcquirer) Execute(_ context.Context, job jobrun.Job) error {
	a.record(job.TrackID)
	<-a.release
	return nil
}

func (a *holdingAcquirer) ExecuteReplace(context.Context, jobrun.Job) error {
	return nil
}

func (a *holdingAcquirer) RefuseQueued(context.Context, jobrun.Job) {}

func TestScheduler_ABurstOfAddsStartsOnEveryIdleAcquireWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := newTestScheduler(acquirer, 4, time.Now)
		defer scheduler.Shutdown(context.Background())
		defer close(acquirer.release)
		user := ids.NewUserId(uuid.New())

		synctest.Wait()
		for range 4 {
			if err := scheduler.Schedule(context.Background(), user, ids.NewTrackId(), ""); err != nil {
				t.Fatalf("Schedule = %v, want nil", err)
			}
		}
		synctest.Wait()

		if got := acquirer.count(); got != 4 {
			t.Fatalf("%d of 4 adds started before the poll interval, want every idle acquire worker to take one", got)
		}
	})
}

func TestScheduler_ResumeRunsAJobThatWaitedWhilePaused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := newTestScheduler(acquirer, 1, time.Now)
		defer scheduler.Shutdown(context.Background())
		user := ids.NewUserId(uuid.New())
		running, waiting := ids.NewTrackId(), ids.NewTrackId()

		synctest.Wait()
		for _, track := range []ids.TrackId{running, waiting} {
			if err := scheduler.Schedule(context.Background(), user, track, ""); err != nil {
				t.Fatalf("Schedule(%s) = %v, want nil", track, err)
			}
			synctest.Wait()
		}
		scheduler.Pause()
		close(acquirer.release)
		synctest.Wait()
		if acquirer.has(waiting) {
			t.Fatalf("the waiting add of %s ran while the scheduler was paused", waiting)
		}

		scheduler.Resume()
		synctest.Wait()

		if !acquirer.has(waiting) {
			t.Fatalf("the waiting add of %s did not run before the poll interval after Resume", waiting)
		}
	})
}

func TestScheduler_ResumeStartsTheWholeBacklogOnEveryIdleWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := newTestScheduler(acquirer, 4, time.Now)
		defer scheduler.Shutdown(context.Background())
		defer close(acquirer.release)
		user := ids.NewUserId(uuid.New())

		synctest.Wait()
		for range 8 {
			if err := scheduler.Schedule(context.Background(), user, ids.NewTrackId(), ""); err != nil {
				t.Fatalf("Schedule = %v, want nil", err)
			}
			synctest.Wait()
		}
		scheduler.Pause()
		for range 4 {
			acquirer.release <- struct{}{}
		}
		synctest.Wait()
		if got := acquirer.count(); got != 4 {
			t.Fatalf("%d of 8 adds started before Resume, want the 4 that held the workers and none while paused", got)
		}

		scheduler.Resume()
		synctest.Wait()

		if got := acquirer.count(); got != 8 {
			t.Fatalf("%d of 8 adds started after Resume, want the 4 backlogged adds on the 4 idle workers before the poll interval", got)
		}
	})
}

func TestScheduler_AnUpgradeRunsAtOnceWhileAcquireWorkersAreIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		upgrader := &recordingUpgrader{}
		scheduler := newTestScheduler(&recordingAcquirer{}, 4, time.Now,
			withTestUpgrades(upgrader, 1))
		defer scheduler.Shutdown(context.Background())
		user := ids.NewUserId(uuid.New())

		for round := range 3 {
			synctest.Wait()
			track := ids.NewTrackId()
			if err := scheduler.ScheduleUpgrade(context.Background(), user, track); err != nil {
				t.Fatalf("round %d: ScheduleUpgrade = %v, want nil", round, err)
			}
			synctest.Wait()
			if !upgrader.has(track) {
				t.Fatalf("round %d: the upgrade of %s did not run before the poll interval; an idle acquire worker took its wake-up", round, track)
			}
		}
	})
}

func TestScheduler_AnAddRunsAtOnceWhileUpgradeWorkersAreIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acquirer := &recordingAcquirer{}
		scheduler := newTestScheduler(acquirer, 1, time.Now,
			withTestUpgrades(&recordingUpgrader{}, 4))
		defer scheduler.Shutdown(context.Background())
		user := ids.NewUserId(uuid.New())

		for round := range 3 {
			synctest.Wait()
			track := ids.NewTrackId()
			if err := scheduler.Schedule(context.Background(), user, track, ""); err != nil {
				t.Fatalf("round %d: Schedule = %v, want nil", round, err)
			}
			synctest.Wait()
			if !acquirer.has(track) {
				t.Fatalf("round %d: the add of %s did not run before the poll interval; an idle upgrade worker took its wake-up", round, track)
			}
		}
	})
}

func TestScheduler_ASelectOutageOnTheFinalRunIsNotRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		track := inmemoryledger.LedgerTrack{TrackSnapshot: pendingBlindingLights()}
		ledger := inmemoryledger.NewLedger(track)
		svc := acquireService(ledger, service.NewSourceRegistry(throttled(), healthyButRejected()), &objectStore{objects: map[string]string{}})

		outcome := runOneJobAs(t, svc, track, 3)

		if outcome.released {
			t.Errorf("outcome = %+v, want the final run settled, not retried", outcome)
		}
		if code := persistedCode(t, ledger, track.ID); code != downloadfailure.FailureSourceUnavailable {
			t.Errorf("persisted failure code = %q, want source_unavailable", code)
		}
	})
}

type failingAcquirer struct{}

func (failingAcquirer) Execute(context.Context, jobrun.Job) error {
	return errors.New("every source refused the track")
}

func (failingAcquirer) ExecuteReplace(context.Context, jobrun.Job) error {
	return errors.New("every source refused the track")
}

func (failingAcquirer) RefuseQueued(context.Context, jobrun.Job) {}

type panicOnMessage struct {
	slog.Handler
	message string
}

func (h panicOnMessage) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == h.message {
		panic("log handler exploded on " + r.Message)
	}
	return h.Handler.Handle(ctx, r)
}

func TestScheduler_AJobWhoseCompletionPanicsAfterItIsRecordedIsRecordedOnce(t *testing.T) {
	logging.UseHandler(t, panicOnMessage{Handler: slog.NewTextHandler(io.Discard, nil), message: "background acquisition failed"})
	queue := &firstOutcomeQueue{Queue: inmemoryqueue.New(make(chan struct{}, 1), time.Now), outcome: make(chan jobOutcome, 1)}
	scheduler := newTestScheduler(failingAcquirer{}, 1, time.Now, withTestQueue(queue))
	defer scheduler.Shutdown(context.Background())
	trackID := ids.NewTrackId()

	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), trackID, ""); err != nil {
		t.Fatalf("Schedule = %v, want nil", err)
	}
	<-queue.outcome

	var records []ports.JobRecord
	for _, r := range scheduler.Status().Recent {
		if r.TrackID == trackID.String() {
			records = append(records, r)
		}
	}
	if len(records) != 1 {
		t.Fatalf("recent records for the job = %+v, want exactly one", records)
	}
	if records[0].State != ports.JobFailed || records[0].Reason == "panic" {
		t.Errorf("record = %s %q, want the job's own failure, not the log handler's panic", records[0].State, records[0].Reason)
	}
}

type succeedingAcquirer struct{ failingAcquirer }

func (succeedingAcquirer) Execute(context.Context, jobrun.Job) error { return nil }

type capturedLogs struct {
	mu      *sync.Mutex
	records *[]slog.Record
}

func (h capturedLogs) Enabled(context.Context, slog.Level) bool { return true }

func (h capturedLogs) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, r)
	return nil
}

func (h capturedLogs) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h capturedLogs) WithGroup(string) slog.Handler { return h }

func logsOfOneJob(t *testing.T, acquirer interface {
	Execute(context.Context, jobrun.Job) error
	ExecuteReplace(context.Context, jobrun.Job) error
	RefuseQueued(context.Context, jobrun.Job)
},
) []slog.Record {
	t.Helper()
	var records []slog.Record
	logging.UseHandler(t, capturedLogs{mu: &sync.Mutex{}, records: &records})
	queue := &firstOutcomeQueue{Queue: inmemoryqueue.New(make(chan struct{}, 1), time.Now), outcome: make(chan jobOutcome, 1)}
	scheduler := newTestScheduler(acquirer, 1, time.Now, withTestQueue(queue))
	if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("Schedule = %v, want nil", err)
	}
	<-queue.outcome
	scheduler.Shutdown(context.Background())
	return records
}

func TestScheduler_AFailedJobIsLoggedAsAnErrorWithItsReason(t *testing.T) {
	var failed []slog.Record
	for _, r := range logsOfOneJob(t, failingAcquirer{}) {
		if r.Message == "background acquisition failed" {
			failed = append(failed, r)
		}
	}

	if len(failed) != 1 || failed[0].Level != slog.LevelError {
		t.Fatalf("failure records = %+v, want exactly one at error level", failed)
	}
	reason := ""
	failed[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "error" {
			reason = a.Value.String()
		}
		return true
	})
	if reason != "every source refused the track" {
		t.Errorf("logged error = %q, want the job's failure reason", reason)
	}
}

func TestScheduler_ASucceededJobLogsNoCompletionRecord(t *testing.T) {
	for _, r := range logsOfOneJob(t, succeedingAcquirer{}) {
		if r.Message == "" || r.Message == "background acquisition failed" || r.Message == "background acquisition will retry" {
			t.Errorf("record %q at %v, want no completion record for a job that succeeded", r.Message, r.Level)
		}
	}
}

func TestScheduler_ShutdownWaitsForAnInFlightJobToFinishAndDoesNotCancelIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := newTestScheduler(acquirer, 1, time.Now)
		if err := scheduler.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}
		synctest.Wait()

		returned := make(chan struct{})
		go func() {
			scheduler.Shutdown(context.Background())
			close(returned)
		}()
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("Shutdown returned while a job was still in flight, want it to drain first")
		default:
		}

		close(acquirer.release)
		synctest.Wait()
		select {
		case <-returned:
		default:
			t.Fatal("Shutdown did not return after the in-flight job finished")
		}
		if status := scheduler.Status(); status.Succeeded != 1 || status.Failed != 0 {
			t.Errorf("succeeded = %d, failed = %d, want the job to finish normally (1, 0), not be cancelled", status.Succeeded, status.Failed)
		}
	})
}

type queueCall struct {
	corrID      string
	hasDeadline bool
	live        bool
}

func callOf(ctx context.Context) queueCall {
	_, hasDeadline := ctx.Deadline()
	return queueCall{corrID: logging.CorrelationIDFromContext(ctx), hasDeadline: hasDeadline, live: ctx.Err() == nil}
}

type callRecordingQueue struct {
	*inmemoryqueue.Queue
	mu         sync.Mutex
	settles    []queueCall
	heartbeats []queueCall
	releases   []queueCall
	releasedCh chan queueCall
	settledCh  chan queueCall
	beatCh     chan queueCall
}

func signal(ch chan queueCall, call queueCall) {
	select {
	case ch <- call:
	default:
	}
}

func (q *callRecordingQueue) Release(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber, delay time.Duration) error {
	call := callOf(ctx)
	q.mu.Lock()
	q.releases = append(q.releases, call)
	q.mu.Unlock()
	signal(q.releasedCh, call)
	return q.Queue.Release(ctx, trackID, fence, delay)
}

func (q *callRecordingQueue) released() []queueCall {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]queueCall(nil), q.releases...)
}

type alwaysPanickingAcquirer struct {
	runs          atomic.Int32
	refused       atomic.Int32
	refusedCorrID atomic.Value
}

func (a *alwaysPanickingAcquirer) Execute(context.Context, jobrun.Job) error {
	a.runs.Add(1)
	panic("boom")
}

func (a *alwaysPanickingAcquirer) ExecuteReplace(ctx context.Context, job jobrun.Job) error {
	return a.Execute(ctx, jobrun.Job{UserID: job.UserID, TrackID: job.TrackID})
}

func (a *alwaysPanickingAcquirer) RefuseQueued(ctx context.Context, job jobrun.Job) {
	a.refusedCorrID.Store(logging.CorrelationIDFromContext(ctx))
	a.refused.Add(1)
}

func TestScheduler_ATrackThatKeepsPanickingIsRefusedAndSettledWithinABudgetAtTheAttemptCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := &callRecordingQueue{Queue: inmemoryqueue.New(make(chan struct{}, 1), time.Now), settledCh: make(chan queueCall, 1)}
		acquirer := &alwaysPanickingAcquirer{}
		scheduler := newTestScheduler(acquirer, 1, time.Now, withTestQueue(queue))
		defer scheduler.Shutdown(context.Background())

		ctx := logging.ContextWithCorrelationID(context.Background(), "corr-panic-cap")
		if err := scheduler.Schedule(ctx, ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}
		<-queue.settledCh

		settles, _ := queue.calls()
		want := queueCall{corrID: "corr-panic-cap", hasDeadline: true, live: true}
		if len(settles) != 1 || settles[0] != want {
			t.Fatalf("settle calls = %+v after %d runs, want exactly one %+v", settles, acquirer.runs.Load(), want)
		}
		if got := acquirer.refused.Load(); got != 1 {
			t.Errorf("track refused %d times, want once when the attempt cap is reached", got)
		}
		if got := acquirer.refusedCorrID.Load(); got != "corr-panic-cap" {
			t.Errorf("refusal ran under correlation id %v, want the job's corr-panic-cap", got)
		}
		if got, retries := acquirer.runs.Load(), len(queue.released()); int(got) != retries+1 {
			t.Errorf("%d runs and %d releases, want every run but the last released for another try", got, retries)
		}
	})
}

func TestScheduler_TheQueueReleaseOfARetryableJobCarriesTheCorrelationIDAndABudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := &callRecordingQueue{Queue: inmemoryqueue.New(make(chan struct{}, 1), time.Now), releasedCh: make(chan queueCall, 1)}
		scheduler := newTestScheduler(retryingAcquirer{}, 1, time.Now, withTestQueue(queue))
		defer scheduler.Shutdown(context.Background())
		ctx := logging.ContextWithCorrelationID(context.Background(), "corr-release")

		if err := scheduler.Schedule(ctx, ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}

		got := <-queue.releasedCh
		want := queueCall{corrID: "corr-release", hasDeadline: true, live: true}
		if got != want {
			t.Fatalf("first release call = %+v, want %+v", got, want)
		}
	})
}

func (q *callRecordingQueue) Settle(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber) error {
	call := callOf(ctx)
	q.mu.Lock()
	q.settles = append(q.settles, call)
	q.mu.Unlock()
	signal(q.settledCh, call)
	return q.Queue.Settle(ctx, trackID, fence)
}

func (q *callRecordingQueue) Heartbeat(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber, lease time.Duration) error {
	call := callOf(ctx)
	q.mu.Lock()
	q.heartbeats = append(q.heartbeats, call)
	q.mu.Unlock()
	signal(q.beatCh, call)
	return q.Queue.Heartbeat(ctx, trackID, fence, lease)
}

func (q *callRecordingQueue) calls() (settles, heartbeats []queueCall) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]queueCall(nil), q.settles...), append([]queueCall(nil), q.heartbeats...)
}

func TestScheduler_TheQueueSettleCarriesTheCorrelationIDAndABudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := &callRecordingQueue{Queue: inmemoryqueue.New(make(chan struct{}, 1), time.Now), settledCh: make(chan queueCall, 1)}
		scheduler := newTestScheduler(&recordingAcquirer{}, 1, time.Now, withTestQueue(queue))
		defer scheduler.Shutdown(context.Background())
		ctx := logging.ContextWithCorrelationID(context.Background(), "corr-settle")

		if err := scheduler.Schedule(ctx, ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}
		<-queue.settledCh

		settles, _ := queue.calls()
		want := queueCall{corrID: "corr-settle", hasDeadline: true, live: true}
		if len(settles) != 1 || settles[0] != want {
			t.Fatalf("settle calls = %+v, want one %+v: the call keeps the request's correlation id and has its own budget", settles, want)
		}
	})
}

func TestScheduler_TheHeartbeatCarriesTheCorrelationIDAndABudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := &callRecordingQueue{Queue: inmemoryqueue.New(make(chan struct{}, 1), time.Now), beatCh: make(chan queueCall, 8)}
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := newTestScheduler(acquirer, 1, time.Now,
			withTestQueue(queue), service.WithHeartbeatInterval(time.Second))
		defer scheduler.Shutdown(context.Background())
		defer close(acquirer.release)
		ctx := logging.ContextWithCorrelationID(context.Background(), "corr-beat")

		if err := scheduler.Schedule(ctx, ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}
		for range 3 {
			<-queue.beatCh
		}

		_, beats := queue.calls()
		want := queueCall{corrID: "corr-beat", hasDeadline: true, live: true}
		if len(beats) == 0 {
			t.Fatal("no heartbeat reached the queue")
		}
		for _, beat := range beats {
			if beat != want {
				t.Fatalf("heartbeat call = %+v, want %+v", beat, want)
			}
		}
	})
}

type correlationNotingAcquirer struct {
	mu       sync.Mutex
	corrIDs  []string
	retries  int
	finished chan struct{}
}

func (a *correlationNotingAcquirer) Execute(ctx context.Context, job jobrun.Job) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.corrIDs = append(a.corrIDs, logging.CorrelationIDFromContext(ctx))
	if len(a.corrIDs) <= a.retries {
		return service.ErrAcquisitionRetryable
	}
	close(a.finished)
	return nil
}

func (a *correlationNotingAcquirer) ExecuteReplace(ctx context.Context, job jobrun.Job) error {
	return a.Execute(ctx, jobrun.Job{UserID: job.UserID, TrackID: job.TrackID})
}

func (a *correlationNotingAcquirer) RefuseQueued(context.Context, jobrun.Job) {}

func (a *correlationNotingAcquirer) seen() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.corrIDs)
}

func TestScheduler_EveryRetryOfAJobRunsUnderTheRequestsCorrelationID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acquirer := &correlationNotingAcquirer{retries: 2, finished: make(chan struct{})}
		scheduler := newTestScheduler(acquirer, 1, time.Now)
		defer scheduler.Shutdown(context.Background())
		ctx := logging.ContextWithCorrelationID(context.Background(), "corr-retries")

		if err := scheduler.Schedule(ctx, ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}
		<-acquirer.finished

		want := []string{"corr-retries", "corr-retries", "corr-retries"}
		if got := acquirer.seen(); !slices.Equal(got, want) {
			t.Fatalf("correlation ids seen by the runs = %q, want %q: a retry must stay tied to its request", got, want)
		}
	})
}
