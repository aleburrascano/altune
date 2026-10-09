package domain

import (
	"net/url"
	"regexp"
	"strings"
)

var youtubeHosts = map[string]bool{
	"youtube.com":       true,
	"www.youtube.com":   true,
	"m.youtube.com":     true,
	"music.youtube.com": true,
	shortYouTubeHost:    true,
}

const shortYouTubeHost = "youtu.be"

var videoIDPathPrefixes = map[string]bool{"embed": true, "v": true}

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func SourceKey(rawURL string) string {
	if id := youtubeVideoID(rawURL); id != "" {
		return "youtube:" + id
	}
	return normalizedURLKey(rawURL)
}

func youtubeVideoID(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !youtubeHosts[host] {
		return ""
	}
	return videoIDOnYouTubeHost(host, u)
}

func videoIDOnYouTubeHost(host string, u *url.URL) string {
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case host == shortYouTubeHost:
		return videoID(segments[0])
	case len(segments) == 1 && segments[0] == "watch":
		return videoID(u.Query().Get("v"))
	case len(segments) == 2 && videoIDPathPrefixes[segments[0]]:
		return videoID(segments[1])
	}
	return ""
}

func videoID(candidateID string) string {
	if !videoIDPattern.MatchString(candidateID) {
		return ""
	}
	return candidateID
}

func normalizedURLKey(rawURL string) string {
	key := strings.TrimSpace(rawURL)
	if i := strings.IndexByte(key, '?'); i >= 0 {
		key = key[:i]
	}
	key = strings.TrimPrefix(key, "https://")
	key = strings.TrimPrefix(key, "http://")
	key = strings.TrimPrefix(key, "www.")
	key = strings.TrimSuffix(key, "/")
	return strings.ToLower(key)
}
