package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

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
