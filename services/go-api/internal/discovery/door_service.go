package discovery

import (
	domain "altune/go-api/internal/discovery/domain"
	ports "altune/go-api/internal/discovery/internal/core/ports"
	unreleased "altune/go-api/internal/discovery/service/unreleased"
)

type (
	UnreleasedArtistKey = ports.UnreleasedArtistKey
	UnreleasedLookup    = unreleased.Lookup
)

var ErrTrackerPartial = ports.ErrTrackerPartial

const AudioProviderYouTube = domain.AudioProviderYouTube
