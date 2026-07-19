package service

import "altune/go-api/internal/catalog/domain"

// Sentinel errors raised directly by catalog use cases (as opposed to errors
// raised by aggregate methods, which live in domain/errors.go — e.g.
// ErrTrackAlreadyInPlaylist, raised from Playlist.AddTrack). Co-located here
// rather than scattered across the files that first raise them. All are
// HTTP-coded domain errors.
var (
	ErrTrackNotFound     = &domain.CodedError{Msg: "track not found", Status: 404}
	ErrPlaylistNotFound  = &domain.CodedError{Msg: "playlist not found", Status: 404}
	ErrAudioNotAvailable = &domain.CodedError{Msg: "audio not available", Status: 404}
)
