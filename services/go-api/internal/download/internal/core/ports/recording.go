package ports

import (
	"altune/go-api/internal/shared/songfields"
	"context"
)

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

type RecordingFacts struct {
	MBID           string
	Title          string
	Disambiguation string
	Artists        []string
	ISRCs          []string
	Duration       float64
}

type RecordingLookup interface {
	RecordingByMBID(ctx context.Context, mbid string) (RecordingFacts, bool, error)
}

type RecordingResolver interface {
	Resolve(ctx context.Context, q songfields.RecordingIdentity) (RecordingIdentity, error)
}

func NoopRecordingResolver() RecordingResolver { return noopRecordingResolver{} }

type noopRecordingResolver struct{}

func (noopRecordingResolver) Resolve(context.Context, songfields.RecordingIdentity) (RecordingIdentity, error) {
	return RecordingIdentity{}, nil
}

type RerecordingQuery struct {
	OriginalMBID string
}

type RerecordingFinder interface {
	FindRerecording(ctx context.Context, q RerecordingQuery) (TrackLabel, bool, error)
}
