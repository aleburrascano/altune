package service

import "altune/go-api/internal/catalog/domain"

// Sentinel errors surfaced by catalog use cases. Co-located here (rather than
// scattered across the files that first raise them) so the full catalog error
// vocabulary is discoverable in one place. All are HTTP-coded domain errors.
var (
	ErrTrackNotFound     = &domain.CodedError{Msg: "track not found", Status: 404}
	ErrPlaylistNotFound  = &domain.CodedError{Msg: "playlist not found", Status: 404}
	ErrAudioNotAvailable = &domain.CodedError{Msg: "audio not available", Status: 404}
)
