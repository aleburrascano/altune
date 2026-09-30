package ports

import "context"

type RecordingProvider string

const (
	ProviderYouTube    RecordingProvider = "youtube"
	ProviderDeezer     RecordingProvider = "deezer"
	ProviderSoundCloud RecordingProvider = "soundcloud"
	ProviderTidal      RecordingProvider = "tidal"
	ProviderQobuz      RecordingProvider = "qobuz"
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
	AcoustIDs        []string
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

type RecordingQuery struct {
	Title    string
	Artist   string
	Album    string
	ISRC     string
	Duration float64
}

type RecordingResolver interface {
	Resolve(ctx context.Context, q RecordingQuery) (RecordingIdentity, error)
}

func NoopRecordingResolver() RecordingResolver { return noopRecordingResolver{} }

type noopRecordingResolver struct{}

func (noopRecordingResolver) Resolve(context.Context, RecordingQuery) (RecordingIdentity, error) {
	return RecordingIdentity{}, nil
}
