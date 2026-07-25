package service

import "altune/go-api/internal/catalog/domain"

var (
	ErrTrackNotFound     = &domain.CodedError{Msg: "track not found", Status: 404}
	ErrPlaylistNotFound  = &domain.CodedError{Msg: "playlist not found", Status: 404}
	ErrAudioNotAvailable = &domain.CodedError{Msg: "audio not available", Status: 404}
)
