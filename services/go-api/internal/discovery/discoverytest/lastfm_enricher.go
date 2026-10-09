package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
)

type LastFmEnricher struct {
	Enrichment domain.LastFmEnrichment
	LookupErr  error

	Lookups    int
	LastKind   domain.ResultKind
	LastArtist string
	LastTitle  string
}

var _ ports.LastFmEnricher = (*LastFmEnricher)(nil)

func (f *LastFmEnricher) Lookup(_ context.Context, kind domain.ResultKind, artistName, entityTitle string) (domain.LastFmEnrichment, error) {
	f.Lookups++
	f.LastKind = kind
	f.LastArtist = artistName
	f.LastTitle = entityTitle
	if f.LookupErr != nil {
		return domain.EmptyLastFmEnrichment(), f.LookupErr
	}
	return f.Enrichment, nil
}
