package ytdlp

import (
	"net/url"
	"regexp"
	"strings"
)

var youTubeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

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
	if !youTubeVideoID.MatchString(id) {
		return raw
	}
	return "https://music.youtube.com/watch?v=" + id
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
