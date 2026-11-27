package app_test

import (
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

var (
	pathParam   = regexp.MustCompile(`\{[^}]*\}`)
	clientVerbs = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
)

func TestWire_TheProductionRouterListsEveryV1RouteTheClientMayCall(t *testing.T) {
	r := productionRouter(t)
	var routes []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/v1/") && slices.Contains(clientVerbs, method) {
			mounted := strings.TrimSuffix(strings.ReplaceAll(route, "/*/", "/"), "/*")
			routes = append(routes, method+" "+pathParam.ReplaceAllString(strings.TrimSuffix(mounted, "/"), "{}"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk production router: %v", err)
	}
	slices.Sort(routes)
	body, err := json.Marshal(slices.Compact(routes))
	if err != nil {
		t.Fatalf("marshal routes: %v", err)
	}

	jsoncontract.AssertWireExact(t, "routes", body)
}
