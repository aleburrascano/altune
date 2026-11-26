package search

import soundcloud "altune/go-api/internal/search/internal/adapters/providers/soundcloud"

type (
	SoundCloudAPIAdapter = soundcloud.SoundCloudAPIAdapter
	NoSoundCloudFallback = soundcloud.NoFallback
)

var (
	NewSoundCloudAPIAdapter           = soundcloud.NewSoundCloudAPIAdapter
	NewSoundCloudAdapter              = soundcloud.NewSoundCloudAdapter
	ErrSoundCloudCollaboratorRequired = soundcloud.ErrCollaboratorRequired
)
