package service

import "altune/go-api/internal/acquisition/internal/core/audioref"

func BuildAudioRef(track TrackRef, tempPath string) string {
	return audioref.Build(refNames(track), tempPath)
}

func BuildLegacyAudioRef(track TrackRef, tempPath string) string {
	return audioref.BuildLegacy(refNames(track), tempPath)
}

func BuildLegacyAudioRefUncapped(track TrackRef, tempPath string) string {
	return audioref.BuildLegacyUncapped(refNames(track), tempPath)
}

func refNames(track TrackRef) audioref.Names {
	return audioref.Names{UserID: track.UserID, Artist: track.Artist, Album: track.Album, Title: track.Title}
}
