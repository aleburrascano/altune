package ytdlp

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"net/url"
	"strings"
)

var youTubeWatchHosts = map[string]bool{
	"www.youtube.com": true,
	"youtube.com":     true,
	"m.youtube.com":   true,
}

func ytDlpURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	id := youTubeWatchID(parsed)
	if !acqdomain.IsYouTubeVideoID(id) {
		return raw
	}
	return acqdomain.YouTubeMusicWatchURL(id)
}

func youTubeWatchID(u *url.URL) string {
	host := strings.ToLower(u.Host)
	switch {
	case host == "youtu.be":
		return strings.TrimPrefix(u.Path, "/")
	case youTubeWatchHosts[host] && u.Path == "/watch":
		return u.Query().Get("v")
	}
	return ""
}
