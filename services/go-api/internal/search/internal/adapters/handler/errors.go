package handler

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"errors"
	"net/http"
)

var (
	ErrRateLimited        = apierror.NewError(apierror.CodeDiscoveryRateLimited, "too many requests, try again later")
	errAllProvidersFailed = apierror.NewError(apierror.CodeDiscoveryAllProvidersFailed, "")
	errContentUnserved    = apierror.NewError(apierror.CodeDiscoveryContentUnserved, "")
)

type mappedError struct {
	coded *apierror.CodedError
	cause error
}

func (e *mappedError) Error() string   { return e.cause.Error() }
func (e *mappedError) Unwrap() []error { return []error{e.coded, e.cause} }

func handleError(w http.ResponseWriter, r *http.Request, err error) {
	httpserver.HandleServiceError(w, r, MapError(err))
}

func MapError(err error) error {
	var invalidFavorite *domain.InvalidFavoriteError
	if errors.As(err, &invalidFavorite) {
		return &mappedError{coded: apierror.NewError(apierror.CodeDiscoveryInvalidFavorite, invalidFavorite.Error()), cause: err}
	}
	var favoritesFull *domain.FavoritesFullError
	if errors.As(err, &favoritesFull) {
		return &mappedError{coded: apierror.NewError(apierror.CodeDiscoveryFavoritesFull, favoritesFull.Error()), cause: err}
	}
	var invalidEvent *domain.InvalidEventError
	if errors.As(err, &invalidEvent) {
		return &mappedError{coded: apierror.NewError(apierror.CodeDiscoveryInvalidEvent, invalidEvent.Error()), cause: err}
	}
	return err
}
