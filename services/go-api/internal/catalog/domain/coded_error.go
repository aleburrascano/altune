package domain

import shareddomain "altune/go-api/internal/shared/domain"

type CodedError struct {
	Msg    string
	Status int
	Code   string
}

func (e *CodedError) Error() string     { return e.Msg }
func (e *CodedError) HTTPStatus() int   { return e.Status }
func (e *CodedError) ErrorCode() string { return e.Code }

var (
	ErrTrackNotFound                 = &CodedError{Msg: "track not found", Status: 404, Code: "catalog.track_not_found"}
	ErrPlaylistNotFound              = &CodedError{Msg: "playlist not found", Status: 404, Code: "catalog.playlist_not_found"}
	ErrAudioNotAvailable             = &CodedError{Msg: "audio not available", Status: 404, Code: "catalog.audio_not_available"}
	ErrAudioTemporarilyUnavailable   = &CodedError{Msg: "audio temporarily unavailable", Status: 503, Code: "catalog.audio_temporarily_unavailable"}
	ErrCatalogTemporarilyUnavailable = &CodedError{Msg: "catalog temporarily unavailable", Status: 503, Code: "catalog.temporarily_unavailable"}
	ErrAudioOrphaned                 = &CodedError{Msg: "track deleted but audio file orphaned", Status: 500, Code: "catalog.audio_orphaned"}
)

var ErrTrackAlreadyInPlaylist = &CodedError{Msg: "track already in playlist", Status: 409, Code: "catalog.track_already_in_playlist"}

var (
	ErrTrackIDsRequired = &CodedError{Msg: "track_ids required", Status: 400, Code: "catalog.track_ids_required"}
	ErrBatchTooLarge    = &CodedError{Msg: "too many track ids", Status: 400, Code: "catalog.batch_too_large"}
	ErrInvalidTrackID   = &CodedError{Msg: "invalid track id", Status: 400, Code: "catalog.invalid_track_id"}

	ErrFeaturedArtistKeyRequired = &CodedError{Msg: "one of name, mbid, or deezer_id is required", Status: 400, Code: "catalog.featured_artist_key_required"}
	ErrInvalidDeezerID           = &CodedError{Msg: "deezer_id must be a positive integer", Status: 400, Code: "catalog.invalid_deezer_id"}
)

type ValidationError = shareddomain.ValidationError

func NewValidationError(msg string) *ValidationError {
	return shareddomain.NewValidationError("catalog", msg)
}
