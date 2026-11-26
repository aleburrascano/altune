package memory

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
	"context"
)

type DeezerEnricher struct {
	ResolvedID string
	Enrichment domain.DeezerEnrichment
	ResolveErr error
	LookupErr  error

	Resolves   int
	Lookups    int
	LastKind   domain.ResultKind
	LastArtist string
	LastTitle  string
}

var _ ports.DeezerEnricher = (*DeezerEnricher)(nil)

func (f *DeezerEnricher) ResolveID(_ context.Context, kind domain.ResultKind, artist, title string) (string, error) {
	f.Resolves++
	f.LastKind = kind
	f.LastArtist = artist
	f.LastTitle = title
	if f.ResolveErr != nil {
		return "", f.ResolveErr
	}
	return f.ResolvedID, nil
}

func (f *DeezerEnricher) Lookup(context.Context, domain.ResultKind, string) (domain.DeezerEnrichment, error) {
	f.Lookups++
	if f.LookupErr != nil {
		return domain.EmptyDeezerEnrichment(), f.LookupErr
	}
	return f.Enrichment, nil
}

type LyricsProvider struct {
	ResolvedID string
	Lyrics     domain.DeezerLyrics
	ResolveErr error
	LookupErr  error

	Resolves   int
	Lookups    int
	LastArtist string
	LastTitle  string
}

var _ ports.LyricsProvider = (*LyricsProvider)(nil)

func (f *LyricsProvider) ResolveTrackID(_ context.Context, artist, title string) (string, error) {
	f.Resolves++
	f.LastArtist = artist
	f.LastTitle = title
	if f.ResolveErr != nil {
		return "", f.ResolveErr
	}
	return f.ResolvedID, nil
}

func (f *LyricsProvider) Lookup(context.Context, string) (domain.DeezerLyrics, error) {
	f.Lookups++
	if f.LookupErr != nil {
		return domain.EmptyDeezerLyrics(), f.LookupErr
	}
	return f.Lyrics, nil
}
