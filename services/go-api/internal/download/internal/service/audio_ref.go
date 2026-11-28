package service

import "altune/go-api/internal/download/internal/domain"

func BuildAudioRef(track TrackRef, tempPath string) string {
	return domain.Build(refNames(track), tempPath)
}

func refNames(track TrackRef) domain.Names {
	return domain.Names{UserID: track.UserID, Artist: track.Artist, Album: track.Album, Title: track.Title}
}
