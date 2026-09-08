package service

import "altune/go-api/internal/catalog/domain"

var (
	ErrTrackNotFound     = &domain.CodedError{Msg: "track not found", Status: 404, Code: "catalog.track_not_found"}
	ErrPlaylistNotFound  = &domain.CodedError{Msg: "playlist not found", Status: 404, Code: "catalog.playlist_not_found"}
	ErrAudioNotAvailable = &domain.CodedError{Msg: "audio not available", Status: 404, Code: "catalog.audio_not_available"}
)
