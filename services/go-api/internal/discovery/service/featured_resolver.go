package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type mbFeaturedSearcher interface {
	SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

type deezerFeaturedLookup interface {
	ResolveID(ctx context.Context, kind domain.ResultKind, artist, title string) (string, error)
	LookupTrackFeatured(ctx context.Context, trackID string) ([]domain.FeaturedArtist, error)
}

type FeaturedArtistResolver struct {
	mb     mbFeaturedSearcher
	deezer deezerFeaturedLookup
}

func NewFeaturedArtistResolver(mb mbFeaturedSearcher, deezer deezerFeaturedLookup) *FeaturedArtistResolver {
	return &FeaturedArtistResolver{mb: mb, deezer: deezer}
}

func (r *FeaturedArtistResolver) Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error) {
	var attempted int
	var errs []error
	var mbFeats, dzFeats []domain.FeaturedArtist
	if r.mb != nil {
		attempted++
		feats, err := r.mbFeatured(ctx, artist, title)
		if err != nil {
			errs = append(errs, fmt.Errorf("musicbrainz featured: %w", err))
			logFeaturedFailure(ctx, "musicbrainz", err)
		}
		mbFeats = feats
	}
	if r.deezer != nil {
		attempted++
		feats, err := r.deezerFeatured(ctx, artist, title)
		if err != nil {
			errs = append(errs, fmt.Errorf("deezer featured: %w", err))
			logFeaturedFailure(ctx, "deezer", err)
		}
		dzFeats = feats
	}
	merged := MergeFeaturedArtists(mbFeats, dzFeats)
	if len(merged) > 0 {
		return merged, nil
	}
	if attempted > 0 && len(errs) == attempted {
		return nil, fmt.Errorf("resolve featured artists: all sources failed: %w", errors.Join(errs...))
	}
	if attempted > 0 && ctx.Err() != nil {
		return nil, fmt.Errorf("resolve featured artists: %w", ctx.Err())
	}
	return nil, nil
}

func logFeaturedFailure(ctx context.Context, source string, err error) {
	slog.WarnContext(ctx, "featured.resolve.source_failed",
		"source", source, "error", redact.Secrets(err.Error()))
}

func (r *FeaturedArtistResolver) mbFeatured(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error) {
	results, err := r.mb.SearchStructured(ctx, artist, title, map[domain.ResultKind]bool{domain.ResultKindTrack: true})
	if err != nil {
		return nil, err
	}
	for _, res := range results {
		if res.Kind != domain.ResultKindTrack {
			continue
		}
		if feats := domain.FeaturedArtistsFromExtras(res.Extras); feats != nil {
			return feats, nil
		}
	}
	return nil, nil
}

func (r *FeaturedArtistResolver) deezerFeatured(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error) {
	id, err := r.deezer.ResolveID(ctx, domain.ResultKindTrack, artist, title)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}
	return r.deezer.LookupTrackFeatured(ctx, id)
}

func MergeFeaturedArtists(mb, deezer []domain.FeaturedArtist) []domain.FeaturedArtist {
	out := make([]domain.FeaturedArtist, 0, len(mb)+len(deezer))
	indexByName := make(map[string]int, len(mb)+len(deezer))
	for _, f := range mb {
		indexByName[domain.NormalizeFeaturedName(f.Name)] = len(out)
		out = append(out, f)
	}
	for _, d := range deezer {
		key := domain.NormalizeFeaturedName(d.Name)
		if i, ok := indexByName[key]; ok {
			if out[i].DeezerID == 0 {
				out[i].DeezerID = d.DeezerID
			}
			continue
		}
		indexByName[key] = len(out)
		out = append(out, d)
	}
	return out
}
