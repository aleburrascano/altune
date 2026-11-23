package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func productionRouter(t *testing.T) *chi.Mux {
	t.Helper()
	return productionRouterFor(t, jwksConfig(t))
}

func productionRouterFor(t *testing.T, cfg *config.Config) *chi.Mux {
	t.Helper()
	return assembledRouter(t, cfg, unreachablePool(t))
}

func assembledRouter(t *testing.T, cfg *config.Config, pool *pgxpool.Pool) *chi.Mux {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	assembly, err := app.Assemble(ctx, cfg, app.Handles{Pool: pool, Transport: refuseEgress{}})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		assembly.Shutdown()
	})
	mux, ok := assembly.Handler().(*chi.Mux)
	if !ok {
		t.Fatalf("Handler() is a %T, want the production *chi.Mux route table", assembly.Handler())
	}
	return mux
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

func TestRouter_MountsNoTestLoginInDevelopment(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.Env = "development"
	r := productionRouterFor(t, cfg)

	var observed int
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/test") {
			t.Errorf("%s %s: the test-login backdoor is gone and no route may come back under /test", method, route)
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
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/test/login", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /test/login = %d, want 404", rec.Code)
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
	r := assembledRouter(t, jwksConfig(t), integrationPool(t))

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

func TestRouter_V1ResponsesCarryNosniffAndNoStore(t *testing.T) {
	r := assembledRouter(t, jwksConfig(t), integrationPool(t))

	for _, tc := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"authed route rejected", http.MethodGet, "/v1/library", "", http.StatusUnauthorized},
		{"public route", http.MethodPost, "/v1/public/auth-failures", `{"reason":"invalid_credentials","app_version":"1.2.3"}`, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestRoutes_OnlyAWiredSchedulerMountsRetryAndReacquire(t *testing.T) {
	for _, tt := range []struct {
		name      string
		ytMusic   bool
		wantRoute bool
	}{
		{name: "no sources leaves acquisition unwired", ytMusic: false, wantRoute: false},
		{name: "a source wires the scheduler and its retry routes", ytMusic: true, wantRoute: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newJWKSFixture(t)
			f.cfg.YtMusicEnabled = tt.ytMusic
			h := assembledHandler(t, f.cfg)
			token := signedToken(t, f.key, f.operator)

			for _, action := range []string{"retry", "reacquire"} {
				req := httptest.NewRequest(http.MethodPost, "/v1/tracks/"+uuid.NewString()+"/"+action, nil)
				req.RemoteAddr = "192.0.2.1:1234"
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if mounted := rec.Code != http.StatusNotFound; mounted != tt.wantRoute {
					t.Errorf("POST %s answered %d: mounted = %v, want %v", action, rec.Code, mounted, tt.wantRoute)
				}
			}
		})
	}
}
