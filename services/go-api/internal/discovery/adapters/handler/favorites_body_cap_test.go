package handler

import (
	"net/http"
	"strings"
	"testing"
)

func TestFavoritesEndpoints_OversizedBodyIsRejectedBeforeDecoding(t *testing.T) {
	router := buildFavoritesLimitedRouter(DefaultDiscoveryRateLimits)
	body := `{"kind":"album","title":"DAMN.","subtitle":"` + strings.Repeat("a", maxFavoriteBodyBytes) + `"}`
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := discServe(t, router, method, "/discovery/favorites", strings.NewReader(body))
		assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_body")
	}
}
