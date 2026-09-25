package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func newReqWithUser(t *testing.T, id shared.UserId, withUser bool) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	if withUser {
		req = req.WithContext(auth.ContextWithUserID(req.Context(), id))
	}
	return req
}

func TestOperatorOnly(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	other := shared.NewUserId(uuid.New())

	tests := []struct {
		name           string
		operatorUserID string
		userID         shared.UserId
		withUser       bool
		wantStatus     int
		wantNext       bool
		wantCode       string
	}{
		{
			name:           "operator account passes through",
			operatorUserID: operator.String(),
			userID:         operator,
			withUser:       true,
			wantStatus:     http.StatusOK,
			wantNext:       true,
		},
		{
			name:           "non-operator account is forbidden",
			operatorUserID: operator.String(),
			userID:         other,
			withUser:       true,
			wantStatus:     http.StatusForbidden,
			wantNext:       false,
			wantCode:       "admin.operator_required",
		},
		{
			name:           "unauthenticated request is rejected before the operator check",
			operatorUserID: operator.String(),
			withUser:       false,
			wantStatus:     http.StatusUnauthorized,
			wantNext:       false,
		},
		{
			name:           "unset operator id fails closed even with a valid user",
			operatorUserID: "",
			userID:         operator,
			withUser:       true,
			wantStatus:     http.StatusForbidden,
			wantNext:       false,
			wantCode:       "admin.operator_required",
		},
		{
			name:           "zero-value user id with unset config is denied",
			operatorUserID: "",
			userID:         shared.UserId{},
			withUser:       true,
			wantStatus:     http.StatusForbidden,
			wantNext:       false,
			wantCode:       "admin.operator_required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			rec := httptest.NewRecorder()
			req := newReqWithUser(t, tt.userID, tt.withUser)
			handler.OperatorOnly(tt.operatorUserID)(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if nextCalled != tt.wantNext {
				t.Errorf("next called = %v, want %v", nextCalled, tt.wantNext)
			}
			if tt.wantCode != "" {
				if got := decodeErrorCode(t, rec.Body.Bytes()); got != tt.wantCode {
					t.Errorf("code = %q, want %q (body %s)", got, tt.wantCode, rec.Body.String())
				}
			}
		})
	}
}

func decodeErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Detail string `json:"detail"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	if resp.Detail == "" {
		t.Errorf("error response has empty detail: %s", body)
	}
	return resp.Code
}

// TestOperatorOnly_RouterRejectionCarriesCode drives a non-operator caller
// through the admin router exactly as admin_wiring mounts it (OperatorOnly in
// front of RegisterData) and asserts the 403 carries the stable code (#1003).
func TestOperatorOnly_RouterRejectionCarriesCode(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	other := shared.NewUserId(uuid.New())

	r := chi.NewRouter()
	r.Route("/admin", func(ar chi.Router) {
		ar.Group(func(gr chi.Router) {
			gr.Use(handler.OperatorOnly(operator.String()))
			handler.New(nil, nil).RegisterData(gr)
		})
	})

	for _, path := range []string{"/admin/health", "/admin/metrics?metric=x", "/admin/logs/stream"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req = req.WithContext(auth.ContextWithUserID(req.Context(), other))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
			}
			if got := decodeErrorCode(t, rec.Body.Bytes()); got != "admin.operator_required" {
				t.Errorf("code = %q, want admin.operator_required (body %s)", got, rec.Body.String())
			}
		})
	}
}

func TestOperatorGate_DenialEmitsOneAccessDeniedRecord(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	operator := shared.NewUserId(uuid.New())
	other := shared.NewUserId(uuid.New())
	gate := handler.OperatorOnly(operator.String())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/admin/killswitch", nil)
	req = req.WithContext(auth.ContextWithUserID(req.Context(), other))
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var denied []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == "admin.access_denied" {
			denied = append(denied, m)
		}
	}
	if len(denied) != 1 {
		t.Fatalf("admin.access_denied records = %d, want 1; logs:\n%s", len(denied), buf.String())
	}
	got := denied[0]
	if got["level"] != "WARN" || got["actor"] != other.String() || got["method"] != "POST" ||
		got["path"] != "/admin/killswitch" || got["code"] != "admin.operator_required" {
		t.Errorf("record = %v", got)
	}
}

// routeParam matches a chi pattern's {placeholder} so a walked route can be
// turned back into a concrete request path.
var routeParam = regexp.MustCompile(`\{[^}]*\}`)

// adminIDs names the two configured principals so a test cannot swap them by
// position — swapping would hand the read-only id the operator's write scope,
// exactly the bug these tests exist to catch.
type adminIDs struct{ operator, readOnly string }

type adminRoute struct{ method, path string }

func (r adminRoute) String() string { return r.method + " " + r.path }

// serveAdminAs drives one route through the /admin group shape of
// internal/app/admin_wiring.go (auth, then the two-principal gate) as caller.
// The request context is already cancelled, so the SSE routes return instead of
// tailing forever; the gate itself never reads the context.
func serveAdminAs(t *testing.T, ids adminIDs, caller shared.UserId, route adminRoute) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, route.method, route.path, nil)
	adminRouter(ids, caller).ServeHTTP(rec, req)
	return rec
}

// adminRouter mounts the admin data surface behind the gate. The handler is
// wired with a probe and a log ring so every route reaches its own body rather
// than a nil dereference, which is what makes "not 403" mean "the gate let it
// through".
func adminRouter(ids adminIDs, caller shared.UserId) *chi.Mux {
	h := handler.New(
		func(context.Context) handler.DependencyHealth { return handler.DependencyHealth{} },
		logging.NewRingBuffer(8),
	)
	r := chi.NewRouter()
	r.Route("/admin", func(ar chi.Router) {
		ar.Group(func(gr chi.Router) {
			gr.Use(injectUser(caller, true))
			gr.Use(handler.OperatorOrReadOnly(ids.operator, ids.readOnly))
			h.RegisterData(gr)
		})
	})
	return r
}

// adminRoutes reads the admin surface from the router itself, so a route added
// later is held to these rules without anyone remembering to list it here.
func adminRoutes(t *testing.T, r *chi.Mux) []adminRoute {
	t.Helper()
	var routes []adminRoute
	walk := func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, adminRoute{method: method, path: routeParam.ReplaceAllString(pattern, "x")})
		return nil
	}
	if err := chi.Walk(r, walk); err != nil {
		t.Fatalf("walk admin routes: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("walked no admin routes; the table below would pass vacuously")
	}
	return routes
}

// TestAdminReadOnly_DeniedOnEveryMutatingRoute is the server-side half of the
// observe-only invariant (#1810): the read-only principal is refused on every
// non-GET admin route, while the operator still reaches it.
func TestAdminReadOnly_DeniedOnEveryMutatingRoute(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	readOnly := shared.NewUserId(uuid.New())
	ids := adminIDs{operator: operator.String(), readOnly: readOnly.String()}

	mutating := 0
	for _, route := range adminRoutes(t, adminRouter(ids, operator)) {
		if route.method == http.MethodGet {
			continue
		}
		mutating++
		t.Run(route.String(), func(t *testing.T) {
			rec := serveAdminAs(t, ids, readOnly, route)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("read-only status = %d, want %d (body %s)", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			if got := decodeErrorCode(t, rec.Body.Bytes()); got != "admin.read_only_forbidden" {
				t.Errorf("read-only code = %q, want admin.read_only_forbidden", got)
			}
			if opRec := serveAdminAs(t, ids, operator, route); opRec.Code == http.StatusForbidden {
				t.Errorf("operator was denied its own write route: %s", opRec.Body.String())
			}
		})
	}
	if mutating == 0 {
		t.Fatal("no mutating admin route found; the deny assertions never ran")
	}
}

// TestAdminReadOnly_ReachesEveryRead pins the other half: the read-only
// principal gets past the gate on every admin GET (Overseer's whole surface),
// and a third party still gets nothing.
func TestAdminReadOnly_ReachesEveryRead(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	readOnly := shared.NewUserId(uuid.New())
	stranger := shared.NewUserId(uuid.New())
	ids := adminIDs{operator: operator.String(), readOnly: readOnly.String()}

	reads := 0
	for _, route := range adminRoutes(t, adminRouter(ids, operator)) {
		if route.method != http.MethodGet {
			continue
		}
		reads++
		t.Run(route.String(), func(t *testing.T) {
			if rec := serveAdminAs(t, ids, readOnly, route); rec.Code == http.StatusForbidden {
				t.Errorf("read-only was denied an admin read: %s", rec.Body.String())
			}
			rec := serveAdminAs(t, ids, stranger, route)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("stranger status = %d, want %d (body %s)", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			if got := decodeErrorCode(t, rec.Body.Bytes()); got != "admin.operator_required" {
				t.Errorf("stranger code = %q, want admin.operator_required", got)
			}
		})
	}
	if reads == 0 {
		t.Fatal("no admin read route found; the allow assertions never ran")
	}
}

// TestAdminReadOnly_UnsetIDFailsClosed pins that an unconfigured read-only
// principal matches nobody: a blank OPERATOR_READONLY_USER_ID must not admit a
// caller whose id is somehow blank, and must not widen the operator gate.
func TestAdminReadOnly_UnsetIDFailsClosed(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	ids := adminIDs{operator: operator.String()}

	for _, route := range []adminRoute{
		{method: http.MethodGet, path: "/admin/health"},
		{method: http.MethodPost, path: "/admin/alerts/pause"},
	} {
		t.Run(route.String(), func(t *testing.T) {
			rec := serveAdminAs(t, ids, shared.UserId{}, route)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			if got := decodeErrorCode(t, rec.Body.Bytes()); got != "admin.operator_required" {
				t.Errorf("code = %q, want admin.operator_required", got)
			}
		})
	}
}

func adminReadRecords(buf *bytes.Buffer) []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == "admin.read" {
			out = append(out, m)
		}
	}
	return out
}

func TestAdminRead_DataRoutesNameTheActor(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	readOnly := shared.NewUserId(uuid.New())
	ids := adminIDs{operator: operator.String(), readOnly: readOnly.String()}

	paths := []string{"/admin/logs/stream", "/admin/events/stream"}
	for _, caller := range []shared.UserId{operator, readOnly} {
		for _, path := range paths {
			buf := captureLogs(t)
			serveAdminAs(t, ids, caller, adminRoute{http.MethodGet, path + "?q=secret-query"})

			got := adminReadRecords(buf)
			if len(got) != 1 {
				t.Fatalf("%s as %s: admin.read records = %d, want 1", path, caller, len(got))
			}
			if got[0]["actor"] != caller.String() || got[0]["method"] != "GET" || got[0]["path"] != path {
				t.Errorf("%s: record = %v", path, got[0])
			}
			if strings.Contains(buf.String(), "secret-query") {
				t.Errorf("%s: log carries the raw query text", path)
			}
		}
	}
}

func TestAdminRead_PollingRoutesEmitNothing(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	ids := adminIDs{operator: operator.String()}
	for _, path := range []string{"/admin/health", "/admin/metrics", "/admin/metrics/live"} {
		buf := captureLogs(t)
		serveAdminAs(t, ids, operator, adminRoute{http.MethodGet, path})
		if got := adminReadRecords(buf); len(got) != 0 {
			t.Errorf("%s emitted %d admin.read records, want 0", path, len(got))
		}
	}
}
