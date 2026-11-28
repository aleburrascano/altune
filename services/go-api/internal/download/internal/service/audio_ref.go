package service

import "altune/go-api/internal/download/internal/domain"

func BuildAudioRef(track TrackRef, tempPath string) string {
	return domain.NewAudioRef(refNames(track), tempPath)
}

func refNames(track TrackRef) domain.AudioRefParts {
	return domain.AudioRefParts{UserID: track.UserID, Artist: track.Artist, Album: track.Album, Title: track.Title}
}
