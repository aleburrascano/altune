package genius

import (
	"altune/go-api/internal/discovery/domain"
	"net/url"
	"regexp"
	"strings"
)

var youtubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func normalizeAudioLink(provider, rawURL string) (domain.AudioLink, bool) {
	switch domain.AudioProvider(provider) {
	case domain.AudioProviderYouTube:
		return youTubeLink(rawURL)
	case domain.AudioProviderSoundCloud:
		return domain.SoundCloudLink(rawURL)
	}
	return domain.AudioLink{}, false
}

func youTubeLink(rawURL string) (domain.AudioLink, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return domain.AudioLink{}, false
	}
	id := youTubeID(u)
	if !youtubeIDPattern.MatchString(id) {
		return domain.AudioLink{}, false
	}
	return domain.AudioLink{Provider: domain.AudioProviderYouTube, ExternalID: id, URL: "https://www.youtube.com/watch?v=" + id}, true
}

func youTubeID(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "youtu.be":
		return strings.Trim(u.Path, "/")
	case (host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")) && u.Path == "/watch":
		return u.Query().Get("v")
	}
	return ""
}
