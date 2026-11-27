package providerrequests

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"errors"
	"fmt"
	"net/http"
)

func ArtworkFailure(source domain.ProviderKey, err error) (string, error) {
	var statusErr outbound.StatusError
	if errors.As(err, &statusErr) {
		if statusErr.UpstreamStatus() == http.StatusNotFound || statusErr.UpstreamStatus() == http.StatusBadRequest {
			return "", nil
		}
	}
	return "", fmt.Errorf("%w: %s: %w", ports.ErrArtworkUnavailable, source, err)
}
