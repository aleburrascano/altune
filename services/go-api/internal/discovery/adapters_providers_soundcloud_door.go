package discovery

import soundcloud "altune/go-api/internal/discovery/internal/adapters/providers/soundcloud"

var (
	NewSoundCloudAPIAdapter = soundcloud.NewSoundCloudAPIAdapter
	NewSoundCloudAdapter    = soundcloud.NewSoundCloudAdapter
)
