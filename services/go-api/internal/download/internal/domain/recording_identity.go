package domain

type RecordingProvider string

const (
	ProviderYouTube    RecordingProvider = "youtube"
	ProviderDeezer     RecordingProvider = "deezer"
	ProviderSoundCloud RecordingProvider = "soundcloud"
)

type ProviderRef struct {
	Provider   RecordingProvider
	ExternalID string
	URL        string
}

type RecordingIdentity struct {
	ISRC             string
	MBID             string
	Duration         float64
	Sources          []ProviderRef
	MBIDs            []string
	ReferenceDoubted bool
}

func (r RecordingIdentity) IsZero() bool {
	return r.ISRC == "" && r.MBID == "" && r.Duration == 0 && len(r.Sources) == 0
}

func (r RecordingIdentity) SourceFor(provider RecordingProvider) (ProviderRef, bool) {
	for _, s := range r.Sources {
		if s.Provider == provider {
			return s, true
		}
	}
	return ProviderRef{}, false
}
