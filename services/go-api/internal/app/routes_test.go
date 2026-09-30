package app

import (
	"altune/go-api/internal/auth"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	"altune/go-api/internal/observe/eventtap"
	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil/httputiltest"
	"altune/go-api/internal/shared/logging"
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRouter_PanickingHandlerLogsRequestCompleteAs500Error(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("error", false)

	a := &App{cfg: &config.Config{Env: "test"}}
	r := a.newRouter(apiWriteTimeout)
	r.Get("/v1/boom", func(http.ResponseWriter, *http.Request) {
		panic("handler exploded")
	})
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want 500", rec.Code)
	}
	var completes []logging.CapturedRecord
	for _, captured := range ring.Snapshot() {
		if captured.Message == "request.complete" {
			completes = append(completes, captured)
		}
	}
	if len(completes) != 1 {
		t.Fatalf("request.complete records = %d, want 1", len(completes))
	}
	if got := completes[0].Attrs["status"]; got != "500" {
		t.Errorf("request.complete status = %s, want 500", got)
	}
	if got := completes[0].Level; got != slog.LevelError.String() {
		t.Errorf("request.complete level = %s, want %s", got, slog.LevelError)
	}
}

const routeWriteDeadline = 150 * time.Millisecond

func newDeadlineRouter(uid shared.UserId, sse *sseHandler, bulkErrs chan<- error) *chi.Mux {
	a := &App{cfg: &config.Config{Env: "test"}}
	r := a.newRouter(routeWriteDeadline)
	r.Get("/v1/bulk", func(w http.ResponseWriter, _ *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 16<<10)
		giveUp := time.Now().Add(10 * time.Second)
		for time.Now().Before(giveUp) {
			if _, err := w.Write(chunk); err != nil {
				bulkErrs <- err
				return
			}
		}
		bulkErrs <- nil
	})
	r.Handle("/v1/events", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sse.ServeHTTP(w, req.WithContext(auth.ContextWithUserID(req.Context(), uid)))
	}))
	return r
}

func TestRouter_WriteDeadlineCutsOffSlowReaderOnNonSSERoute(t *testing.T) {
	bulkErrs := make(chan error, 1)
	sse := newSSEHandler(events.NewInProcessBus(), 0)
	srv := httputiltest.NewServer(t, newDeadlineRouter(shared.NewUserId(uuid.New()), sse, bulkErrs))

	began := time.Now()
	httputiltest.Get(t, srv, "/v1/bulk", nil)

	select {
	case err := <-bulkErrs:
		if err == nil {
			t.Fatal("bulk writes never failed: slow reader held the handler open")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bulk handler still blocked in Write after 3s: no write deadline on non-SSE route")
	}
	if elapsed := time.Since(began); elapsed < routeWriteDeadline {
		t.Errorf("cut off after %s, before the %s deadline", elapsed, routeWriteDeadline)
	}
}

func TestRouter_SSEStreamOutlivesRouteWriteDeadline(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	sse := newSSEHandler(bus, 0)
	sse.heartbeat = routeWriteDeadline / 3
	srv := httputiltest.NewServer(t, newDeadlineRouter(uid, sse, make(chan error, 1)))

	conn, br := httputiltest.Get(t, srv, "/v1/events", nil)
	resp := httputiltest.ReadResponse(t, conn, br)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := bufio.NewReader(resp.Body)

	time.Sleep(4 * routeWriteDeadline)
	bus.Publish(context.Background(), uid, "late.event", map[string]any{"k": "v"})
	readUntil(t, body, func(l string) bool { return strings.HasPrefix(l, "event: late.event") })
}

func TestRouter_SSEPerFrameDeadlineReachesConnection(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	slog.SetDefault(slog.New(slog.DiscardHandler))

	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	sse := newSSEHandler(bus, 0)
	sse.writeTimeout = routeWriteDeadline
	sse.heartbeat = time.Hour
	done := make(chan struct{})
	router := newDeadlineRouter(uid, sse, make(chan error, 1))
	srv := httputiltest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		router.ServeHTTP(w, r)
	}))

	httputiltest.Get(t, srv, "/v1/events", nil)

	payload := map[string]any{"pad": strings.Repeat("x", 16<<10)}
	giveUp := time.After(5 * time.Second)
	for {
		select {
		case <-done:
			return
		case <-giveUp:
			t.Fatal("SSE handler still blocked on a stalled client after 5s: per-frame deadline not applied")
		default:
			bus.Publish(context.Background(), uid, "bulk", payload)
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func productionRouter(t *testing.T) *chi.Mux {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), blackHoleDatabase(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	a := &App{cfg: &config.Config{MusicDir: t.TempDir()}, sem: make(chan struct{}, 1), pool: pool}
	cat, err := a.wireCatalog(nil, discoveryWiring{})
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
	})
	r := a.mountRoutes(verifier, cat, playbackHandler.NewQueueHandler(nil),
		discoveryHandler.NewDiscoveryHandler(discoveryHandler.DiscoveryServices{}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.wireObserve(ctx, r, verifier, eventtap.New(events.NewInProcessBus()))
	return r
}

func TestRouter_MountsNoAdminRoute(t *testing.T) {
	r := productionRouter(t)

	var observed int
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/admin") {
			t.Errorf("%s %s: the /admin tree is gone and no route may come back under it", method, route)
		}
		if strings.HasPrefix(route, "/observe/") {
			observed++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk production router: %v", err)
	}
	if observed == 0 {
		t.Fatal("walked no /observe route, so the router under test is not the production one")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/health", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /admin/health = %d, want 404", rec.Code)
	}
}

var (
	backtickSpan   = regexp.MustCompile("`([^`\\n]+)`")
	httpMethodLead = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE) `)
)

func namedGoAPIRoutes(note string) []string {
	var routes []string
	for _, span := range backtickSpan.FindAllStringSubmatch(note, -1) {
		path, _, _ := strings.Cut(httpMethodLead.ReplaceAllString(span[1], ""), "?")
		isGoAPI := strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/observe/") ||
			strings.HasPrefix(path, "/admin/") || path == "/health"
		if isGoAPI {
			routes = append(routes, path)
		}
	}
	return routes
}

func routeMatchesPattern(route, pattern string) bool {
	routeSegments := strings.Split(route, "/")
	patternSegments := strings.Split(pattern, "/")
	if len(routeSegments) != len(patternSegments) {
		return false
	}
	for i, segment := range patternSegments {
		isParam := strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
		if !isParam && segment != routeSegments[i] {
			return false
		}
	}
	return true
}

func unmountedRoutes(note string, mounted []string) []string {
	var missing []string
	for _, route := range namedGoAPIRoutes(note) {
		found := false
		for _, pattern := range mounted {
			found = found || routeMatchesPattern(route, pattern)
		}
		if !found {
			missing = append(missing, route)
		}
	}
	return missing
}

func TestUnmountedRoutes(t *testing.T) {
	mounted := []string{"/health", "/v1/discovery/search", "/v1/tracks/{trackId}/retry", "/observe/metrics/live"}
	cases := []struct {
		name string
		note string
		want []string
	}{
		{"mounted route passes", "see `/v1/discovery/search`", nil},
		{"unmounted admin route is reported", "see `/admin/metrics/live`", []string{"/admin/metrics/live"}},
		{"concrete id matches a param segment", "`POST /v1/tracks/abc123/retry`", nil},
		{"query string is dropped", "`GET /observe/metrics/live?window_days=<n>&by=x`", nil},
		{"method prefix is dropped", "`GET /v1/discovery/gone`", []string{"/v1/discovery/gone"}},
		{"overseer route is ignored", "`/overseer/api/state`", nil},
		{"health matches exactly", "`/health` and `/healthz`", nil},
		{"longer route than pattern is a miss", "`/v1/discovery/search/extra`", []string{"/v1/discovery/search/extra"}},
		{"span outside backticks is ignored", "/admin/metrics/live", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := unmountedRoutes(c.note, mounted)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("unmountedRoutes(%q) = %v, want %v", c.note, got, c.want)
			}
		})
	}
}

func TestCapabilityNotes_NameMountedRoutes(t *testing.T) {
	var mounted []string
	err := chi.Walk(productionRouter(t), func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted = append(mounted, route)
		return nil
	})
	if err != nil {
		t.Fatalf("walk production router: %v", err)
	}
	notes, err := filepath.Glob("../../../../docs/features/*/notes.md")
	if err != nil {
		t.Fatalf("glob capability notes: %v", err)
	}
	for _, notePath := range notes {
		body, err := os.ReadFile(notePath)
		if err != nil {
			t.Fatalf("read %s: %v", notePath, err)
		}
		for _, route := range unmountedRoutes(string(body), mounted) {
			t.Errorf("%s: %s is not mounted", notePath, route)
		}
	}
}

func TestRouter_PublicAuthFailureRouteIsAnonymousAndLibraryStaysAuthed(t *testing.T) {
	r := productionRouter(t)

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"reason":"invalid_credentials","app_version":"1.2.3"}`)
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/public/auth-failures", body))
	if rec.Code != http.StatusNoContent {
		t.Errorf("POST /v1/public/auth-failures without Authorization = %d, want 204", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/library", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/library without Authorization = %d, want 401", rec.Code)
	}
}

func TestRouter_RequestTimeoutReleasesBlockedV1HandlerButNotLongLivedRoutes(t *testing.T) {
	const budget = 100 * time.Millisecond
	a := &App{cfg: &config.Config{Env: "test"}, requestTimeout: budget}
	r := a.newRouter(apiWriteTimeout)
	blockUntilDone := func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-req.Context().Done():
			w.WriteHeader(http.StatusGatewayTimeout)
		case <-time.After(3 * budget):
			w.WriteHeader(http.StatusOK)
		}
	}
	r.Get("/v1/blocked", blockUntilDone)
	r.Get("/v1/events", blockUntilDone)
	r.Get("/v1/tracks/{trackId}/audio", blockUntilDone)
	r.Get("/health", blockUntilDone)

	cases := []struct {
		path string
		want int
	}{
		{"/v1/blocked", http.StatusGatewayTimeout},
		{"/v1/events", http.StatusOK},
		{"/v1/tracks/abc/audio", http.StatusOK},
		{"/health", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.want {
				t.Errorf("GET %s = %d, want %d", tc.path, rec.Code, tc.want)
			}
		})
	}
}
