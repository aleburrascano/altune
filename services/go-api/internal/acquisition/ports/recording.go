package ports

import "context"

// Provider identity keys stored in RecordingSource.Provider. The recording
// resolver writes them and source adapters look them up via SourceFor, so
// both sides must reference these constants rather than retyping the strings.
const (
	ProviderYouTube    = "youtube"
	ProviderDeezer     = "deezer"
	ProviderSoundCloud = "soundcloud"
	ProviderTidal      = "tidal"
	ProviderQobuz      = "qobuz"
)

type RecordingSource struct {
	Provider   string
	ExternalID string
	URL        string
}

type RecordingIdentity struct {
	ISRC      string
	MBID      string
	Duration  float64
	Sources   []RecordingSource
	AcoustIDs []string
}

func (r RecordingIdentity) IsZero() bool {
	return r.ISRC == "" && r.MBID == "" && r.Duration == 0 && len(r.Sources) == 0
}

func (r RecordingIdentity) SourceFor(provider string) (RecordingSource, bool) {
	for _, s := range r.Sources {
		if s.Provider == provider {
			return s, true
		}
	}
	return RecordingSource{}, false
}

type RecordingQuery struct {
	Title  string
	Artist string
	Album  string
	ISRC   string
}

type RecordingResolver interface {
	Resolve(ctx context.Context, q RecordingQuery) (RecordingIdentity, error)
}

func NoopRecordingResolver() RecordingResolver { return noopRecordingResolver{} }

type noopRecordingResolver struct{}

func (noopRecordingResolver) Resolve(context.Context, RecordingQuery) (RecordingIdentity, error) {
	return RecordingIdentity{}, nil
}
