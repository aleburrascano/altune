package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// defaultQueueDepthFactor bounds total outstanding acquisition jobs (in-flight
// + pending) at this multiple of the worker concurrency. A burst of Schedule
// calls past that bound is reported as rejected instead of spawning an
// unbounded number of goroutines and job-log entries.
const defaultQueueDepthFactor = 4

// defaultQueueWaitTimeout bounds how long an admitted job waits for a worker
// slot. The whole queue can be ahead of it and each acquisition gets up to
// acquireTimeout, so an unbounded wait leaves the last admitted job pending for
// several ten-minute generations (#1981). Five minutes is short enough that a
// user sees a settled job rather than a spinner, and long enough that a job
// queued behind one normal acquisition still runs.
const defaultQueueWaitTimeout = 5 * time.Minute

// queueWaitTimeoutReason is the completion reason on a job abandoned at the
// queue-wait deadline; it distinguishes "never got a worker" from the
// shutdown cancellation that shares JobCancelled.
const queueWaitTimeoutReason = "queue_wait_timeout"

// acquirer is the whole of the acquisition service the scheduler uses: the two
// entry points a scheduled job runs. Depending on it rather than on
// *AcquireTrackAudioService keeps the scheduler exercisable without the full
// acquisition graph behind it.
type acquirer interface {
	Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error
	ExecuteReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error
}

type BackgroundAcquisitionScheduler struct {
	svc      acquirer
	events   events.Publisher
	wg       *sync.WaitGroup
	sem      chan struct{}
	admit    chan struct{}
	cancel   context.CancelFunc
	baseCtx  context.Context
	closed   atomic.Bool
	paused   atomic.Bool
	inflight sync.Map

	// admitMu orders admission against the drain. Schedule holds it for reading
	// from the shutdown check through wg.Add; Shutdown takes it for writing to
	// set closed. Without that order a Schedule already past the check can Add
	// after Wait began, which sync.WaitGroup forbids and which leaves the job
	// running past the drain. It is always the outermost lock here: the drain
	// releases it before waiting, and no job holds it.
	admitMu sync.RWMutex

	queueDepth       int
	queueWaitTimeout time.Duration
	principalCap     int
	principals       *principalGate

	inflightCount atomic.Int64
	rejected      atomic.Uint64

	verification ports.AcquisitionVerification
	log          *jobLog
}

func NewBackgroundAcquisitionScheduler(
	svc acquirer,
	wg *sync.WaitGroup,
	sem chan struct{},
	opts ...func(*BackgroundAcquisitionScheduler),
) *BackgroundAcquisitionScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &BackgroundAcquisitionScheduler{
		svc:     svc,
		wg:      wg,
		sem:     sem,
		cancel:  cancel,
		baseCtx: ctx,
		log:     newJobLog(),
		events:  events.NoopPublisher(),
	}
	for _, opt := range opts {
		opt(s)
	}
	depth := s.queueDepth
	if depth <= 0 {
		depth = cap(sem) * defaultQueueDepthFactor
	}
	if depth < 1 {
		depth = 1
	}
	s.admit = make(chan struct{}, depth)
	if s.queueWaitTimeout <= 0 {
		s.queueWaitTimeout = defaultQueueWaitTimeout
	}
	s.principals = newPrincipalGate(s.principalCap)
	return s
}

func WithSchedulerEvents(pub events.Publisher) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if pub != nil {
			s.events = pub
		}
	}
}

// WithQueueDepth caps the total number of outstanding acquisition jobs
// (in-flight + pending). Schedule calls past the cap are rejected rather than
// admitted. A non-positive value falls back to defaultQueueDepthFactor times
// the worker concurrency.
func WithQueueDepth(depth int) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.queueDepth = depth }
}

// WithQueueWaitTimeout bounds how long an admitted job waits for a worker slot
// before it is abandoned as JobCancelled with reason queueWaitTimeoutReason.
// The job never runs, so nothing it would have done is half-done. A
// non-positive value falls back to defaultQueueWaitTimeout.
func WithQueueWaitTimeout(wait time.Duration) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.queueWaitTimeout = wait }
}

// WithPrincipalQueueDepth caps the number of outstanding acquisition jobs
// (in-flight + pending) a single principal (userId) may hold at once, so no one
// user can consume the whole shared admission queue and starve others. Arrivals
// past a principal's share are rejected with ErrPrincipalQueueFull while slots
// remain for other principals. A non-positive value disables the per-principal
// cap (a single principal may then fill the global queue).
func WithPrincipalQueueDepth(depth int) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.principalCap = depth }
}

// principalGate bounds how many admission slots a single principal holds at
// once. It fair-shares the global queue: check-and-reserve is atomic under the
// mutex so concurrent Schedule calls for one principal cannot exceed the cap.
// A non-positive cap disables the gate (every admit succeeds).
type principalGate struct {
	cap  int
	mu   sync.Mutex
	held map[string]int
}

func newPrincipalGate(capacity int) *principalGate {
	return &principalGate{cap: capacity, held: make(map[string]int)}
}

// admit reserves a slot for id, returning false when id already holds its full
// share. Callers that admit must release exactly once when the job finishes.
func (g *principalGate) admit(id string) bool {
	if g.cap <= 0 {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held[id] >= g.cap {
		return false
	}
	g.held[id]++
	return true
}

// release returns a slot reserved by admit. It is a no-op when the gate is
// disabled, so it pairs safely with every admitted job.
func (g *principalGate) release(id string) {
	if g.cap <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held[id] <= 1 {
		delete(g.held, id)
		return
	}
	g.held[id]--
}

func WithVerificationStatus(v ports.AcquisitionVerification) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		s.verification = v
		if !v.FullyArmed() {
			slog.Warn("acquisition.verification_degraded",
				"ffprobe", v.Ffprobe, "ffmpeg", v.Ffmpeg, "fpcalc", v.Fpcalc, "yt_dlp", v.YtDlp, "streamrip", v.Streamrip)
			return
		}
		slog.Info("acquisition.verification_armed")
	}
}

// acquisitionRun is the acquirer entry point a scheduled job executes: Execute
// or ExecuteReplace.
type acquisitionRun func(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error

// jobKind names which entry point a job runs. The in-flight registry is keyed
// by track alone, so the kind is what tells a duplicate request (already
// satisfied by the running job) from a different one (which that job would not
// perform).
type jobKind string

const (
	jobAcquire jobKind = "acquire"
	jobReplace jobKind = "replace"
)

// ErrAcquisitionQueueFull reports that the bounded admission queue shed the
// job: nothing was queued, so the caller must not treat the request as accepted.
var ErrAcquisitionQueueFull = &admissionError{
	msg:    "acquisition queue is full, try again later",
	status: 503,
	code:   "acquisition.queue_full",
}

// ErrPrincipalQueueFull reports that the caller (userId) already holds its
// per-principal share of the admission queue: nothing was queued for this
// request, but slots remain for other principals. Retryable once the
// principal's in-flight jobs drain.
var ErrPrincipalQueueFull = &admissionError{
	msg:    "too many concurrent acquisitions for this user, try again later",
	status: 429,
	code:   "acquisition.principal_queue_full",
}

// ErrTrackJobInFlight reports that a job of the other kind holds the track's
// in-flight slot, so the requested one was not queued: a replace cannot run
// while a plain acquisition does, and neither stands in for the other.
// Retryable once the running job settles.
var ErrTrackJobInFlight = &admissionError{
	msg:    "another acquisition for this track is already running, try again later",
	status: 409,
	code:   "acquisition.job_in_flight",
}

// ErrSchedulerShutdown reports that the scheduler is draining and refused the job.
var ErrSchedulerShutdown = &admissionError{
	msg:    "acquisition is shutting down, try again later",
	status: 503,
	code:   "acquisition.shutting_down",
}

// ErrAcquisitionPaused reports that acquisition has been paused at runtime (a
// kill switch, distinct from process shutdown): nothing was queued, but the
// job admits again once Resume/SetEnabled(true) re-enables the scheduler
// without a process restart. In-flight jobs are unaffected by the pause.
var ErrAcquisitionPaused = &admissionError{
	msg:    "acquisition is paused, try again later",
	status: 503,
	code:   "acquisition.paused",
}

// SetEnabled is the runtime kill switch for acquisition. Passing false pauses
// admission: subsequent Schedule/ScheduleReplace calls are refused with
// ErrAcquisitionPaused while already in-flight jobs keep running and the
// Shutdown path is untouched. Passing true resumes admission. The flag is
// atomic, so it is safe to toggle concurrently with scheduling. It does not
// survive a process restart — it is a live control, not persisted config.
func (s *BackgroundAcquisitionScheduler) SetEnabled(enabled bool) {
	s.paused.Store(!enabled)
}

// Pause is the kill switch shorthand for SetEnabled(false): stop admitting new
// acquisitions at runtime without taking down the process.
func (s *BackgroundAcquisitionScheduler) Pause() { s.SetEnabled(false) }

// Resume is the shorthand for SetEnabled(true): re-admit acquisitions after a
// Pause, no process restart required.
func (s *BackgroundAcquisitionScheduler) Resume() { s.SetEnabled(true) }

// Enabled reports whether acquisition is currently admitting jobs. It is false
// after Pause/SetEnabled(false) and true otherwise. Shutdown does not flip it;
// use Status/closed to observe draining.
func (s *BackgroundAcquisitionScheduler) Enabled() bool { return !s.paused.Load() }

// ScheduleReplace queues a replace acquisition. A nil error means a replace for
// the track is queued or already in flight; a non-nil error
// (ErrTrackJobInFlight, ErrAcquisitionQueueFull, ErrSchedulerShutdown) means
// nothing was queued.
func (s *BackgroundAcquisitionScheduler) ScheduleReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	return s.admitAndSpawn(ctx, userId, trackId, "", jobReplace, s.svc.ExecuteReplace)
}

// Schedule queues an acquisition. A nil error means an acquisition for the
// track is queued or already in flight; a non-nil error (ErrTrackJobInFlight,
// ErrAcquisitionQueueFull, ErrSchedulerShutdown) means nothing was queued.
func (s *BackgroundAcquisitionScheduler) Schedule(ctx context.Context, userId shared.UserId, trackId domain.TrackId, sourceURL string) error {
	return s.admitAndSpawn(ctx, userId, trackId, sourceURL, jobAcquire, s.svc.Execute)
}

// admitAndSpawn admits a job and registers it under one read-hold of admitMu,
// so a job that passes the shutdown check is counted on the WaitGroup before
// Shutdown can close admission and wait. Nothing it covers waits on a job — the
// admission slot is taken with a non-blocking select and the work runs on a new
// goroutine — so an arrival delays the drain by a registration at most.
func (s *BackgroundAcquisitionScheduler) admitAndSpawn(
	ctx context.Context,
	userId shared.UserId,
	trackId domain.TrackId,
	sourceURL string,
	kind jobKind,
	run acquisitionRun,
) error {
	s.admitMu.RLock()
	defer s.admitMu.RUnlock()

	key, admitted, err := s.admitJob(ctx, userId, trackId, kind)
	if !admitted {
		return err
	}
	s.spawnJob(ctx, userId, trackId, key, sourceURL, run)
	return nil
}

// admitJob applies the shutdown, dedup, and backpressure checks. It returns
// the job's dedup key and whether the job holds an admission slot. When not
// admitted, err is nil if a job of the same kind is already in flight (the
// request is already satisfied) and non-nil if the job was refused. Nothing
// needs releasing in either case.
func (s *BackgroundAcquisitionScheduler) admitJob(ctx context.Context, userId shared.UserId, trackId domain.TrackId, kind jobKind) (key string, admitted bool, err error) {
	if s.closed.Load() {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "schedule_after_shutdown", "track_id", trackId.String())
		return "", false, ErrSchedulerShutdown
	}

	// Runtime kill switch: a paused scheduler refuses new jobs before reserving
	// any dedup key or admission/principal slot, so pausing never leaks a
	// reservation. In-flight jobs are unaffected; Resume re-admits.
	if s.paused.Load() {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "schedule_while_paused", "track_id", trackId.String())
		return "", false, ErrAcquisitionPaused
	}

	key = trackId.String()
	if reserved, refusal := s.reserveTrack(ctx, key, kind); !reserved {
		return "", false, refusal
	}

	// Fair-share arrival: a principal past its per-principal share is rejected
	// while slots remain for others, so one user cannot starve the queue. The
	// slot is released in runJob (or below if global admission then fails).
	if !s.principals.admit(userId.String()) {
		s.inflight.Delete(key)
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.principal_queue_full",
			"track_id", key, "user_id", userId.String(), "principal_cap", s.principalCap)
		return "", false, ErrPrincipalQueueFull
	}

	// Bound arrival: acquire an admission slot synchronously before registering
	// or spawning anything. When the queue (in-flight + pending) is full, report
	// the job as rejected and release the dedup key so it can be retried later.
	select {
	case s.admit <- struct{}{}:
	default:
		s.principals.release(userId.String())
		s.inflight.Delete(key)
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.queue_full",
			"track_id", key, "queue_depth", cap(s.admit))
		return "", false, ErrAcquisitionQueueFull
	}
	return key, true, nil
}

// reserveTrack takes the track's in-flight slot for kind. A job of the same
// kind already holding it satisfies this request, so it is deduped with a nil
// refusal; a job of the other kind does not, and reporting that as queued would
// drop the request while its caller's cooldown stays burned (#1980), so it is
// refused instead.
func (s *BackgroundAcquisitionScheduler) reserveTrack(ctx context.Context, key string, kind jobKind) (reserved bool, refusal error) {
	running, loaded := s.inflight.LoadOrStore(key, kind)
	if !loaded {
		return true, nil
	}
	if running != kind {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.job_in_flight",
			"track_id", key, "requested_kind", string(kind), "running_kind", running)
		return false, ErrTrackJobInFlight
	}
	slog.InfoContext(ctx, "schedule_skip_inflight", "track_id", key, "kind", string(kind))
	return false, nil
}

// spawnJob registers an admitted job and runs it on a background goroutine,
// which owns releasing the admission slot and dedup key.
func (s *BackgroundAcquisitionScheduler) spawnJob(
	ctx context.Context,
	userId shared.UserId,
	trackId domain.TrackId,
	key, sourceURL string,
	run acquisitionRun,
) {
	// Carry the originating request's correlation ID onto the job context so the
	// slog.*Context calls throughout the acquisition pipeline trace end-to-end.
	// The job outlives the request, so we derive a fresh context from s.baseCtx
	// (for shutdown cancellation) and only transplant the corr_id value.
	corrID := logging.CorrelationIDFromContext(ctx)

	slog.InfoContext(ctx, "acquisition.scheduling", "track_id", key, "user_id", userId.String())
	s.inflightCount.Add(1)
	s.log.register(key, sourceURL)
	s.wg.Add(1)
	go s.runJob(corrID, userId, trackId, key, run)
}

func (s *BackgroundAcquisitionScheduler) runJob(
	corrID string,
	userId shared.UserId,
	trackId domain.TrackId,
	key string,
	run acquisitionRun,
) {
	defer s.wg.Done()
	defer s.principals.release(userId.String())
	defer func() { <-s.admit }()
	defer s.inflight.Delete(key)
	defer s.inflightCount.Add(-1)
	jobCtx := s.baseCtx
	if corrID != "" {
		jobCtx = logging.WithCorrelationID(jobCtx, corrID)
	}
	// A closure, not a direct deferred call: the panic log must see jobCtx as
	// reassigned below (with the job reporter), and recover must run in the
	// deferred function itself.
	defer func() {
		if r := recover(); r != nil {
			s.logJobPanic(jobCtx, key, r)
		}
	}()

	if !s.awaitWorkerSlot(jobCtx, key) {
		return
	}
	defer func() { <-s.sem }()

	s.log.markRunning(key)
	jobCtx = withJobReporter(jobCtx, schedulerJobReporter{
		ctx: jobCtx, log: s.log, events: s.events, trackID: key, userId: userId,
	})
	if err := run(jobCtx, userId, trackId); err != nil {
		// The chain embeds subprocess stderr verbatim, and this is its outermost
		// sink: the reason is served as `reason` by the admin status endpoint.
		reason := logSafeError(err)
		s.log.complete(key, JobFailed, reason)
		slog.ErrorContext(jobCtx, "background acquisition failed",
			"track_id", key, "error", reason)
		return
	}
	s.log.complete(key, JobSucceeded, "")
}

// awaitWorkerSlot takes a worker slot for the job, reporting false when the job
// was abandoned instead: the queue-wait deadline expired, or the scheduler shut
// down. It settles the job log on both abandonment paths; the caller releases
// the slot it took.
func (s *BackgroundAcquisitionScheduler) awaitWorkerSlot(jobCtx context.Context, key string) bool {
	queueWait := time.NewTimer(s.queueWaitTimeout)
	defer queueWait.Stop()
	select {
	case s.sem <- struct{}{}:
		return true
	case <-queueWait.C:
		s.log.complete(key, JobCancelled, queueWaitTimeoutReason)
		slog.WarnContext(jobCtx, "acquisition.queue_wait_timeout",
			"track_id", key, "waited", s.queueWaitTimeout.String())
		return false
	case <-s.baseCtx.Done():
		s.log.complete(key, JobCancelled, "")
		slog.InfoContext(jobCtx, "acquisition.cancelled_before_start", "track_id", key)
		return false
	}
}

func (s *BackgroundAcquisitionScheduler) logJobPanic(jobCtx context.Context, key string, r any) {
	s.log.complete(key, JobFailed, "panic")
	slog.ErrorContext(jobCtx, "acquisition_panic",
		"track_id", key,
		"panic", r,
		"stack", string(debug.Stack()),
	)
}

type schedulerJobReporter struct {
	// ctx is the job context carrying the originating request's correlation ID,
	// so events this reporter publishes stay tied to the request that scheduled
	// the job even though the job outlives it.
	ctx     context.Context
	log     *jobLog
	events  events.Publisher
	trackID string
	userId  shared.UserId
}

func (r schedulerJobReporter) meta(title, artist, album string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Title, j.Artist, j.Album = title, artist, album })
}

func (r schedulerJobReporter) stage(name string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Stage = name })
	r.events.Publish(r.ctx, r.userId, events.TypeTrackAcquisitionProgress, map[string]any{
		"track_id": r.trackID,
		"stage":    name,
	})
}

func (r schedulerJobReporter) provenance(value string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Provenance = value })
}

func (r schedulerJobReporter) source(url string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) {
		if j.ResolvedSource == "" {
			j.ResolvedSource = url
		}
	})
}

func (s *BackgroundAcquisitionScheduler) Status() ports.AcquisitionStatus {
	jobs, recent := s.log.snapshot()
	succeeded, failed := s.log.counts()
	return ports.AcquisitionStatus{
		InFlight:      int(s.inflightCount.Load()),
		Succeeded:     succeeded,
		Failed:        failed,
		Rejected:      s.rejected.Load(),
		Paused:        s.paused.Load(),
		QueueDepth:    len(s.admit),
		QueueCapacity: cap(s.admit),
		Verification:  s.verification,
		ActiveJobs:    jobs,
		Recent:        recent,
	}
}

// closeAdmission refuses new jobs and waits out any Schedule already past the
// shutdown check, so every job the drain must wait for is on the WaitGroup
// before the drain starts waiting.
func (s *BackgroundAcquisitionScheduler) closeAdmission() {
	s.admitMu.Lock()
	defer s.admitMu.Unlock()
	s.closed.Store(true)
}

func (s *BackgroundAcquisitionScheduler) Shutdown(ctx context.Context) {
	s.closeAdmission()
	s.cancel()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("background tasks drained")
	case <-ctx.Done():
		slog.Warn("background task drain timed out")
	}
}
