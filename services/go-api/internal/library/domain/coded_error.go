package domain

import apierror "altune/go-api/internal/shared/apierror"

var (
	ErrTrackNotFound                 = apierror.NewError(apierror.CodeCatalogTrackNotFound, "track not found")
	ErrPlaylistNotFound              = apierror.NewError(apierror.CodeCatalogPlaylistNotFound, "playlist not found")
	ErrAudioNotAvailable             = apierror.NewError(apierror.CodeCatalogAudioNotAvailable, "audio not available")
	ErrAudioTemporarilyUnavailable   = apierror.NewError(apierror.CodeCatalogAudioTemporarilyDown, "audio temporarily unavailable")
	ErrCatalogTemporarilyUnavailable = apierror.NewError(apierror.CodeCatalogTemporarilyDown, "catalog temporarily unavailable")
	ErrAudioOrphaned                 = apierror.NewError(apierror.CodeCatalogAudioOrphaned, "track deleted but audio file orphaned")
)

var ErrTrackAlreadyInPlaylist = apierror.NewError(apierror.CodeCatalogTrackAlreadyInPlaylist, "track already in playlist")

var (
	ErrTrackIDsRequired = apierror.NewError(apierror.CodeCatalogTrackIDsRequired, "track_ids required")
	ErrBatchTooLarge    = apierror.NewError(apierror.CodeCatalogBatchTooLarge, "too many track ids")
	ErrInvalidTrackID   = apierror.NewError(apierror.CodeCatalogInvalidTrackID, "invalid track id")

	ErrFeaturedArtistKeyRequired = apierror.NewError(apierror.CodeCatalogFeaturedArtistKeyRequired, "one of name, mbid, or deezer_id is required")
	ErrInvalidDeezerID           = apierror.NewError(apierror.CodeCatalogInvalidDeezerID, "deezer_id must be a positive integer")
)

func NewValidationError(msg string) *apierror.CodedError {
	return apierror.NewError(apierror.CodeCatalogValidation, msg)
}
