package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"errors"
	"fmt"
	"net/http"
)

func artworkFailure(source domain.ProviderKey, err error) (string, error) {
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		if statusErr.HTTPStatus() == http.StatusNotFound || statusErr.HTTPStatus() == http.StatusBadRequest {
			return "", nil
		}
	}
	return "", fmt.Errorf("%w: %s: %w", ports.ErrArtworkUnavailable, source, err)
}
