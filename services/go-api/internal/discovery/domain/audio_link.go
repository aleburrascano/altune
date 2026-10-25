package domain

import (
	"net/url"
	"regexp"
	"strings"
)

const maxAudioLinkLength = 200

var youtubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type AudioLink struct {
	Provider   AudioProvider `json:"provider"`
	ExternalID string        `json:"external_id"`
	URL        string        `json:"url"`
}

func SoundCloudLink(raw string) (AudioLink, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return AudioLink{}, false
	}
	host := strings.ToLower(u.Hostname())
	if host != "soundcloud.com" && !strings.HasSuffix(host, ".soundcloud.com") {
		return AudioLink{}, false
	}
	u.Scheme, u.RawQuery, u.Fragment, u.User, u.Host = "https", "", "", nil, host
	canonical := u.String()
	if len(canonical) > maxAudioLinkLength {
		return AudioLink{}, false
	}
	return AudioLink{Provider: AudioProviderSoundCloud, ExternalID: canonical, URL: canonical}, true
}

func YouTubeLink(raw string) (AudioLink, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return AudioLink{}, false
	}
	id := youTubeID(u)
	if !youtubeIDPattern.MatchString(id) {
		return AudioLink{}, false
	}
	return AudioLink{Provider: AudioProviderYouTube, ExternalID: id, URL: "https://www.youtube.com/watch?v=" + id}, true
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
