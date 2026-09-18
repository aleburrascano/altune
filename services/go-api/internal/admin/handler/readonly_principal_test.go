package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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
