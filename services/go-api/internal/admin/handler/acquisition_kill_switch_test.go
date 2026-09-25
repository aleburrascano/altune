package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"

	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"
)

// The acquisition kill switch is driven against a real
// BackgroundAcquisitionScheduler, not a fake: the admin handler must toggle the
// same live instance that gates job admission. Jobs drain immediately because
// the stub repo returns no track, so admission is the only observable variable.

type stubTrackRepo struct{}

func (stubTrackRepo) GetByID(context.Context, domain.TrackId, shared.UserId) (*domain.Track, error) {
	return nil, nil
}
func (stubTrackRepo) Update(context.Context, *domain.Track, int) error { return nil }
func (stubTrackRepo) AudioRefInUse(context.Context, string, domain.TrackId) (bool, error) {
	return false, nil
}

type stubAudioSource struct{}

func (stubAudioSource) Name() string { return "stub" }
func (stubAudioSource) Find(context.Context, acqPorts.FindRequest) ([]acqPorts.AudioCandidate, error) {
	return nil, nil
}

func (stubAudioSource) Fetch(context.Context, acqPorts.AudioCandidate, string) (string, error) {
	return "", nil
}

type stubAudioStore struct{}

func (stubAudioStore) Exists(context.Context, string) (bool, error) { return false, nil }
func (stubAudioStore) Store(context.Context, string, string) error  { return nil }
func (stubAudioStore) Delete(context.Context, string) error         { return nil }

func newLiveScheduler() (*acqService.BackgroundAcquisitionScheduler, *sync.WaitGroup) {
	svc := acqService.NewAcquireTrackAudioService(
		stubTrackRepo{}, acqService.NewSourceRegistry(stubAudioSource{}), stubAudioStore{})
	wg := &sync.WaitGroup{}
	return acqService.NewBackgroundAcquisitionScheduler(svc, wg, make(chan struct{}, 1)), wg
}

// TestAcquisitionKillSwitch_TogglesLiveScheduler drives the operator-gated
// pause/resume routes and proves each transition reaches the live scheduler:
// admission is refused with ErrAcquisitionPaused after the pause POST and
// admitted again after the resume POST, and the /acquisition status surface
// reflects the paused flag both ways.
func TestAcquisitionKillSwitch_TogglesLiveScheduler(t *testing.T) {
	sched, wg := newLiveScheduler()
	operator := shared.NewUserId(uuid.New())
	srv := mountAdminHandler(handler.New(nil, nil).WithAcquisition(sched), operator.String(), operator, true)
	user := shared.NewUserId(uuid.New())

	// Enabled by default: a job admits.
	if err := sched.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule while enabled = %v, want nil", err)
	}
	wg.Wait()

	// Pause through the handler: the live scheduler must refuse new jobs and the
	// status surface must report paused.
	code, body := doAdmin(t, srv, http.MethodPost, "/admin/acquisition/pause")
	if code != http.StatusOK || !body.Paused {
		t.Fatalf("POST pause = %d paused=%v, want 200 paused=true", code, body.Paused)
	}
	if code, body = doAdmin(t, srv, http.MethodGet, "/admin/acquisition"); code != http.StatusOK || !body.Paused {
		t.Fatalf("GET acquisition = %d paused=%v, want 200 paused=true", code, body.Paused)
	}
	if err := sched.Schedule(context.Background(), user, domain.NewTrackId(), ""); !errors.Is(err, acqService.ErrAcquisitionPaused) {
		t.Fatalf("schedule after pause = %v, want ErrAcquisitionPaused", err)
	}

	// Resume through the handler: admission and the status surface recover.
	code, body = doAdmin(t, srv, http.MethodPost, "/admin/acquisition/resume")
	if code != http.StatusOK || body.Paused {
		t.Fatalf("POST resume = %d paused=%v, want 200 paused=false", code, body.Paused)
	}
	if code, body = doAdmin(t, srv, http.MethodGet, "/admin/acquisition"); code != http.StatusOK || body.Paused {
		t.Fatalf("GET acquisition = %d paused=%v, want 200 paused=false", code, body.Paused)
	}
	if err := sched.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule after resume = %v, want nil (kill switch must be reversible)", err)
	}
	wg.Wait()
}

// TestAcquisitionKillSwitch_OperatorOnly pins that an unauthenticated or
// non-operator caller is rejected before the live scheduler is touched — no open
// toggle, matching the sibling admin kill switches.
func TestAcquisitionKillSwitch_OperatorOnly(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	other := shared.NewUserId(uuid.New())

	paths := []string{"/admin/acquisition/pause", "/admin/acquisition/resume"}
	callers := []struct {
		name       string
		caller     shared.UserId
		authed     bool
		wantStatus int
	}{
		{"unauthenticated", shared.UserId{}, false, http.StatusUnauthorized},
		{"non-operator", other, true, http.StatusForbidden},
	}

	for _, path := range paths {
		for _, c := range callers {
			t.Run(c.name+" "+path, func(t *testing.T) {
				sched, _ := newLiveScheduler()
				srv := mountAdminHandler(handler.New(nil, nil).WithAcquisition(sched), operator.String(), c.caller, c.authed)

				req := httptest.NewRequest(http.MethodPost, path, nil)
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)

				if rec.Code != c.wantStatus {
					t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
				}
				if sched.Status().Paused {
					t.Fatal("rejected caller flipped the acquisition kill switch")
				}
			})
		}
	}
}

// TestAcquisitionKillSwitch_UnwiredIsUnavailable pins the 503 when no scheduler
// is configured, matching the other kill switches' unwired behaviour.
func TestAcquisitionKillSwitch_UnwiredIsUnavailable(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	srv := mountAdminHandler(handler.New(nil, nil), operator.String(), operator, true)

	for _, path := range []string{"/admin/acquisition/pause", "/admin/acquisition/resume"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("POST %s = %d, want 503 (body %s)", path, rec.Code, rec.Body.String())
		}
	}
}
