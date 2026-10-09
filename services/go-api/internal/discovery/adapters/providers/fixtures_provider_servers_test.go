package providers

import (
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func artworkProviderFailureHandlers() map[string]http.HandlerFunc {
	return providertest.ArtworkFailureHandlers()
}

func artworkProviderVerifiedMissHandlers() map[string]http.HandlerFunc {
	return providertest.ArtworkVerifiedMissHandlers()
}

func cancelMidRequestServer(t *testing.T) (*httptest.Server, context.Context) {
	t.Helper()
	return providertest.CancelMidRequestServer(t)
}
