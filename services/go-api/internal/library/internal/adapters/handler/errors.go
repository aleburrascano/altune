package handler

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"errors"
	"net/http"
)

var (
	errAudioRateLimited = apierror.NewError(apierror.CodeCatalogAudioRateLimited, "too many audio requests, try again later")
	errWriteRateLimited = apierror.NewError(apierror.CodeCatalogWriteRateLimited, "too many writes, try again later")
)

var codeOf = []struct {
	err  error
	code apierror.Code
}{
	{domain.ErrTrackNotFound, apierror.CodeCatalogTrackNotFound},
	{domain.ErrPlaylistNotFound, apierror.CodeCatalogPlaylistNotFound},
	{domain.ErrAudioNotAvailable, apierror.CodeCatalogAudioNotAvailable},
	{domain.ErrAudioTemporarilyUnavailable, apierror.CodeCatalogAudioTemporarilyDown},
	{domain.ErrCatalogTemporarilyUnavailable, apierror.CodeCatalogTemporarilyDown},
	{domain.ErrAudioOrphaned, apierror.CodeCatalogAudioOrphaned},
	{domain.ErrStorageStalled, apierror.CodeCatalogAudioTemporarilyDown},
	{domain.ErrTrackAlreadyInPlaylist, apierror.CodeCatalogTrackAlreadyInPlaylist},
	{domain.ErrPlaylistFull, apierror.CodeCatalogPlaylistFull},
	{domain.ErrLibraryFull, apierror.CodeCatalogLibraryFull},
	{domain.ErrTooManyPlaylists, apierror.CodeCatalogTooManyPlaylists},
	{domain.ErrBackfillInProgress, apierror.CodeCatalogBackfillInProgress},
	{domain.ErrBackfillCoolingDown, apierror.CodeCatalogBackfillCoolingDown},
	{domain.ErrTrackIDsRequired, apierror.CodeCatalogTrackIDsRequired},
	{domain.ErrBatchTooLarge, apierror.CodeCatalogBatchTooLarge},
	{domain.ErrInvalidTrackID, apierror.CodeCatalogInvalidTrackID},
	{domain.ErrFeaturedArtistKeyRequired, apierror.CodeCatalogFeaturedArtistKeyRequired},
	{domain.ErrInvalidDeezerID, apierror.CodeCatalogInvalidDeezerID},
}

type mappedError struct {
	coded *apierror.CodedError
	cause error
}

func (e *mappedError) Error() string   { return e.cause.Error() }
func (e *mappedError) Unwrap() []error { return []error{e.coded, e.cause} }

func HandleError(w http.ResponseWriter, r *http.Request, err error) {
	httpserver.HandleServiceError(w, r, mapError(err))
}

func mapError(err error) error {
	for _, m := range codeOf {
		if errors.Is(err, m.err) {
			return &mappedError{coded: apierror.NewError(m.code, m.err.Error()), cause: err}
		}
	}
	var invalid *domain.ValidationError
	if errors.As(err, &invalid) {
		return &mappedError{coded: apierror.NewError(apierror.CodeCatalogValidation, invalid.Error()), cause: err}
	}
	return err
}
