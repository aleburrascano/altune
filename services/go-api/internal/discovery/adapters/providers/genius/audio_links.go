package genius

import (
	"altune/go-api/internal/discovery/domain"
	"net/url"
	"regexp"
	"strings"
)

const maxAudioLinkLength = 200

var youtubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func normalizeAudioLink(provider, rawURL string) (domain.AudioLink, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !isWebURL(u) {
		return domain.AudioLink{}, false
	}
	var normalized string
	switch provider {
	case "youtube":
		normalized = canonicalYouTube(u)
	case "soundcloud":
		normalized = canonicalSoundCloud(u)
	}
	if normalized == "" || len(normalized) > maxAudioLinkLength {
		return domain.AudioLink{}, false
	}
	return domain.AudioLink{Provider: provider, URL: normalized}, true
}

func isWebURL(u *url.URL) bool {
	return u.Scheme == "http" || u.Scheme == "https"
}

func canonicalYouTube(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	id := ""
	switch {
	case host == "youtu.be":
		id = strings.Trim(u.Path, "/")
	case host == "youtube.com" || strings.HasSuffix(host, ".youtube.com"):
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		}
	}
	if !youtubeIDPattern.MatchString(id) {
		return ""
	}
	return "https://www.youtube.com/watch?v=" + id
}

func canonicalSoundCloud(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if host != "soundcloud.com" && !strings.HasSuffix(host, ".soundcloud.com") {
		return ""
	}
	u.Scheme, u.RawQuery, u.Fragment, u.User = "https", "", "", nil
	u.Host = host
	return u.String()
}
