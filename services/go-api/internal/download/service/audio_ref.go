package service

import "altune/go-api/internal/download/internal/core/audiokey"

func BuildAudioRef(track TrackRef, tempPath string) string {
	return audiokey.Build(refNames(track), tempPath)
}

func BuildLegacyAudioRef(track TrackRef, tempPath string) string {
	return audiokey.BuildLegacy(refNames(track), tempPath)
}

func BuildLegacyAudioRefUncapped(track TrackRef, tempPath string) string {
	return audiokey.BuildLegacyUncapped(refNames(track), tempPath)
}

func refNames(track TrackRef) audiokey.Names {
	return audiokey.Names{UserID: track.UserID, Artist: track.Artist, Album: track.Album, Title: track.Title}
}
