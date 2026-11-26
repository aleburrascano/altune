package search

import soundcloud "altune/go-api/internal/search/internal/adapters/providers/soundcloud"

type SoundCloudAPIAdapter = soundcloud.SoundCloudAPIAdapter

var (
	NewSoundCloudAPIAdapter = soundcloud.NewSoundCloudAPIAdapter
	NewSoundCloudAdapter    = soundcloud.NewSoundCloudAdapter
)
