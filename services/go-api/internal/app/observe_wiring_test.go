package app

import (
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/observe/eventtap"
	observeHandler "altune/go-api/internal/observe/handler"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/logging"
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	observePrincipalToken = "observe-principal"
	observeStrangerToken  = "observe-stranger"
	observeReadOnlyToken  = "observe-readonly"
)

var observeRouteParam = regexp.MustCompile(`\{[^}]*\}`)

type observeSubjects struct {
	principal shared.UserId
	stranger  shared.UserId
	readOnly  shared.UserId
}

func newObserveSubjects() observeSubjects {
	return observeSubjects{
		principal: shared.NewUserId(uuid.New()),
		stranger:  shared.NewUserId(uuid.New()),
		readOnly:  shared.NewUserId(uuid.New()),
	}
}

func (s observeSubjects) verifier() auth.TokenVerifier {
	byToken := map[string]shared.UserId{
		observePrincipalToken: s.principal,
		observeStrangerToken:  s.stranger,
		observeReadOnlyToken:  s.readOnly,
	}
	return auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		subject, known := byToken[token]
		if !known {
			return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
		}
		return auth.VerifiedToken{UserID: subject, ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
}

func observeTestDeps() observeHandler.Deps {
	shutdown := make(chan struct{})
	close(shutdown)
	return observeHandler.Deps{
		Health: func(context.Context) observeHandler.DependencyHealth {
			return observeHandler.DependencyHealth{DB: observeHandler.DepUp, Redis: observeHandler.DepUp, Auth: observeHandler.DepUp}
		},
		Logs:        logging.NewRingBuffer(8),
		Events:      eventtap.NewFeed(),
		Eval:        evalmeter.New(false, 0, nil),
		LiveMetrics: func() observeHandler.LiveMetrics { return observeHandler.LiveMetrics{} },
		Shutdown:    shutdown,
	}
}

func mountedObserveTree(subjects observeSubjects, cfg *config.Config) *chi.Mux {
	r := chi.NewRouter()
	mountObserve(r, subjects.verifier(), observePrincipal(cfg), observeHandler.New(observeTestDeps()))
	return r
}

func callObserve(t *testing.T, srv http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

type observeRoute struct{ method, path string }

func observeRoutes(t *testing.T, tree *chi.Mux) []observeRoute {
	t.Helper()
	var routes []observeRoute
	walk := func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, observeRoute{method: method, path: observeRouteParam.ReplaceAllString(pattern, "x")})
		return nil
	}
	if err := chi.Walk(tree, walk); err != nil {
		t.Fatalf("walk observe routes: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("walked no observe routes, so the assertions proved nothing")
	}
	return routes
}

func assertObserveStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, what string) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("%s: status %d, want %d; body %s", what, rec.Code, want, rec.Body.String())
	}
}

func TestObserveRoutes_EveryRouteHoldsTheGate(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OverseerPrincipalID: subjects.principal.String()})

	for _, route := range observeRoutes(t, tree) {
		label := route.method + " " + route.path
		assertObserveStatus(t, callObserve(t, tree, route.method, route.path, ""), http.StatusUnauthorized, label+" without a token")
		assertObserveStatus(t, callObserve(t, tree, route.method, route.path, observeStrangerToken), http.StatusForbidden, label+" as another subject")
		assertObserveStatus(t, callObserve(t, tree, http.MethodPost, route.path, observePrincipalToken), http.StatusMethodNotAllowed, "POST "+route.path+" as the principal")
	}
}

func TestObserveRoutes_EveryRouteOnlyRegistersGet(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OverseerPrincipalID: subjects.principal.String()})

	for _, route := range observeRoutes(t, tree) {
		if route.method != http.MethodGet {
			t.Errorf("%s %s: observe serves GET only", route.method, route.path)
		}
	}
}

func TestObserveRoutes_EveryJSONRouteSendsNoStoreAndNosniff(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OverseerPrincipalID: subjects.principal.String()})

	for _, route := range observeRoutes(t, tree) {
		if strings.HasSuffix(route.path, "/stream") {
			continue
		}
		rec := callObserve(t, tree, route.method, route.path, observePrincipalToken)
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Errorf("%s %s as the principal: status %d, want the gate to admit it", route.method, route.path, rec.Code)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s %s: X-Content-Type-Options = %q, want nosniff", route.method, route.path, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control = %q, want no-store", route.method, route.path, got)
		}
	}
}

func TestObserveRoutes_DenialStillSendsNosniff(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OverseerPrincipalID: subjects.principal.String()})

	for _, route := range observeRoutes(t, tree) {
		rec := callObserve(t, tree, route.method, route.path, observeStrangerToken)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s %s denied: X-Content-Type-Options = %q, want nosniff", route.method, route.path, got)
		}
	}
}

func TestObservePrincipal_FallsBackToReadOnlyWhenUnset(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{OperatorReadOnlyUserID: subjects.readOnly.String()})

	assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", observeReadOnlyToken), http.StatusOK, "read-only id with no overseer principal")
	assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", observeStrangerToken), http.StatusForbidden, "another subject under the fallback")
}

func TestObservePrincipal_SetPrincipalReplacesReadOnly(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{
		OverseerPrincipalID:    subjects.principal.String(),
		OperatorReadOnlyUserID: subjects.readOnly.String(),
	})

	assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", observePrincipalToken), http.StatusOK, "the overseer principal")
	assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", observeReadOnlyToken), http.StatusForbidden, "the read-only id once a principal is set")
}

func TestObserveAcquisition_AbsentSchedulerIsANilReader(t *testing.T) {
	if reader := (&App{}).observeAcquisition(); reader != nil {
		t.Errorf("reader = %#v, want a nil interface so the route reports it unavailable", reader)
	}
}

func TestObserveAcquisition_WiredSchedulerIsTheReader(t *testing.T) {
	scheduler := &acqService.BackgroundAcquisitionScheduler{}
	if reader := (&App{scheduler: scheduler}).observeAcquisition(); reader != scheduler {
		t.Errorf("reader = %#v, want the app's scheduler", reader)
	}
}

func TestObservePrincipal_BothUnsetFailsClosed(t *testing.T) {
	subjects := newObserveSubjects()
	tree := mountedObserveTree(subjects, &config.Config{})

	for _, token := range []string{observePrincipalToken, observeStrangerToken, observeReadOnlyToken} {
		assertObserveStatus(t, callObserve(t, tree, http.MethodGet, "/observe/health", token), http.StatusForbidden, token+" with no principal configured")
	}
}
