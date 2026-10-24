package domain

import (
	"net/url"
	"strings"
)

const maxAudioLinkLength = 200

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
