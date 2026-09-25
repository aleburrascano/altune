package domain

import "altune/go-api/internal/shared"

type CodedError struct {
	Msg    string
	Status int
	Code   string
}

func (e *CodedError) Error() string     { return e.Msg }
func (e *CodedError) HTTPStatus() int   { return e.Status }
func (e *CodedError) ErrorCode() string { return e.Code }

var ErrTrackAlreadyInPlaylist = &CodedError{Msg: "track already in playlist", Status: 409, Code: "catalog.track_already_in_playlist"}

// The request-shape rejections the catalog's HTTP surface answers with. They
// are values rather than plain 400 writes so a caller branches on the code: the
// audio-urls worker has to tell a batch it should split from an id it should
// drop, and message text is not a contract.
var (
	ErrTrackIDsRequired = &CodedError{Msg: "track_ids required", Status: 400, Code: "catalog.track_ids_required"}
	ErrBatchTooLarge    = &CodedError{Msg: "too many track ids", Status: 400, Code: "catalog.batch_too_large"}
	ErrInvalidTrackID   = &CodedError{Msg: "invalid track id", Status: 400, Code: "catalog.invalid_track_id"}

	ErrFeaturedArtistKeyRequired = &CodedError{Msg: "one of name, mbid, or deezer_id is required", Status: 400, Code: "catalog.featured_artist_key_required"}
	ErrInvalidDeezerID           = &CodedError{Msg: "deezer_id must be a positive integer", Status: 400, Code: "catalog.invalid_deezer_id"}
)

// ValidationError aliases the shared type so this package keeps one name for
// its 400s while the implementation lives in internal/shared.
type ValidationError = shared.ValidationError

// NewValidationError builds a catalog validation error
// ("catalog.validation_error").
func NewValidationError(msg string) *ValidationError {
	return shared.NewValidationError("catalog", msg)
}
