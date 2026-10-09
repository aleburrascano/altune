package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
)

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
