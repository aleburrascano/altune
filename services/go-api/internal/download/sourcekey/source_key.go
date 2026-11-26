package sourcekey

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

	"youtube-nocookie.com":     true,
	"www.youtube-nocookie.com": true,
}

const shortYouTubeHost = "youtu.be"

var videoIDPathPrefixes = map[string]bool{"embed": true, "v": true, "shorts": true, "live": true}

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

const youTubeMusicWatchURLPrefix = "https://music.youtube.com/watch?v="

func IsYouTubeVideoID(id string) bool {
	return videoIDPattern.MatchString(id)
}

func YouTubeMusicWatchURL(videoID string) string {
	return youTubeMusicWatchURLPrefix + videoID
}

func SourceKey(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return hostlessKey(rawURL)
	}
	host := strings.ToLower(u.Hostname())
	if !youtubeHosts[host] {
		return otherHostKey(host, u)
	}
	if id := videoIDOnYouTubeHost(host, u); id != "" {
		return "youtube:" + id
	}
	return ""
}

var pathIdentifiedHosts = map[string]bool{
	"soundcloud.com":     true,
	"api.soundcloud.com": true,
}

func otherHostKey(host string, u *url.URL) string {
	host = strings.TrimPrefix(strings.TrimPrefix(host, "www."), "m.")
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	if pathIdentifiedHosts[host] {
		return strings.ToLower(host + path)
	}
	if query := u.Query().Encode(); query != "" {
		return host + path + "?" + query
	}
	return host + path
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
	if !IsYouTubeVideoID(candidateID) {
		return ""
	}
	return candidateID
}

func hostlessKey(rawURL string) string {
	key := strings.ToLower(strings.TrimSpace(rawURL))
	key = strings.TrimPrefix(strings.TrimPrefix(key, "www."), "m.")
	return strings.TrimSuffix(key, "/")
}
