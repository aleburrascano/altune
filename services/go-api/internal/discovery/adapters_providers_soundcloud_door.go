package discovery

import soundcloud "altune/go-api/internal/discovery/internal/adapters/providers/soundcloud"

type (
	SoundCloudAPIAdapter = soundcloud.SoundCloudAPIAdapter
	SoundCloudAdapter    = soundcloud.SoundCloudAdapter
)

var (
	NewSoundCloudAPIAdapter = soundcloud.NewSoundCloudAPIAdapter
	NewSoundCloudAdapter    = soundcloud.NewSoundCloudAdapter
)
