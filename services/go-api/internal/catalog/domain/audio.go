package domain

import "strings"

// AudioContentType maps a stored audio ref to its MIME type — the single source
// for both the upload side (object storage sets it on PutObject) and the serve
// side (the proxy stream endpoint labels the response). The two must agree:
// iOS/expo-audio decodes progressive audio by Content-Type, so an m4a sent as
// audio/mpeg fails to play. Defaults to audio/mpeg for legacy mp3 refs.
func AudioContentType(audioRef string) string {
	switch {
	case strings.HasSuffix(audioRef, ".m4a"):
		return "audio/mp4"
	case strings.HasSuffix(audioRef, ".opus"):
		return "audio/opus"
	case strings.HasSuffix(audioRef, ".ogg"):
		return "audio/ogg"
	default:
		return "audio/mpeg"
	}
}
