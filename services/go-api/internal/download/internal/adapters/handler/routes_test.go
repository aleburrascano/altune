package handler_test

import (
	"altune/go-api/internal/download/internal/adapters/handler"
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/service"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func mountedRoutes(rt handler.Routes) []string {
	r := chi.NewRouter()
	rt.Bounded(r)
	rt.LongLived(r)
	var mounted []string
	for _, route := range r.Routes() {
		mounted = append(mounted, route.Pattern)
	}
	return mounted
}

func TestRoutes_BoundedMountsReacquireAndRetryAndLongLivedMountsNothing(t *testing.T) {
	repo := memory.NewLedger()
	store := memory.MustNewCooldownStore(time.Now)
	rt := handler.NewRoutes(
		handler.NewRetryHandler(repo, &retryFakeScheduler{}, service.NewRetryAdmission(store)),
		handler.NewReacquireHandler(repo, &reacquireFakeScheduler{}, service.NewReacquireAdmission(store)),
	)

	got := mountedRoutes(rt)

	want := map[string]bool{"/tracks/{trackId}/reacquire": true, "/tracks/{trackId}/retry": true}
	if len(got) != len(want) {
		t.Fatalf("mounted = %v, want exactly %v", got, want)
	}
	for _, pattern := range got {
		if !want[pattern] {
			t.Errorf("mounted %q, want only %v", pattern, want)
		}
	}
	r := chi.NewRouter()
	rt.LongLived(r)
	if len(r.Routes()) != 0 {
		t.Errorf("LongLived mounted %v, want none", r.Routes())
	}
}

func TestRoutes_TheZeroValueMountsNothing(t *testing.T) {
	if got := mountedRoutes(handler.Routes{}); len(got) != 0 {
		t.Errorf("mounted = %v, want none from the zero Routes", got)
	}
}
