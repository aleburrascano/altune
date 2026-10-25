package genius

import "altune/go-api/internal/discovery/domain"

func normalizeAudioLink(provider, rawURL string) (domain.AudioLink, bool) {
	switch domain.AudioProvider(provider) {
	case domain.AudioProviderYouTube:
		return domain.YouTubeLink(rawURL)
	case domain.AudioProviderSoundCloud:
		return domain.SoundCloudLink(rawURL)
	}
	return domain.AudioLink{}, false
}
