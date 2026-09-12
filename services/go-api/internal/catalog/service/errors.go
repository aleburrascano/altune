package service

import "altune/go-api/internal/catalog/domain"

var (
	ErrTrackNotFound     = &domain.CodedError{Msg: "track not found", Status: 404, Code: "catalog.track_not_found"}
	ErrPlaylistNotFound  = &domain.CodedError{Msg: "playlist not found", Status: 404, Code: "catalog.playlist_not_found"}
	ErrAudioNotAvailable = &domain.CodedError{Msg: "audio not available", Status: 404, Code: "catalog.audio_not_available"}
	// ErrAudioTemporarilyUnavailable signals a transient storage failure where
	// the audio file is not confirmed absent (the stream call failed but the
	// file is present, or its existence could not be verified). The client
	// should retry rather than treat the track as genuinely gone.
	ErrAudioTemporarilyUnavailable = &domain.CodedError{Msg: "audio temporarily unavailable", Status: 503, Code: "catalog.audio_temporarily_unavailable"}
)
