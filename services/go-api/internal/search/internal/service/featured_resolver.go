package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type MBFeaturedSearcher interface {
	SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

type DeezerFeaturedLookup interface {
	ResolveID(ctx context.Context, kind domain.ResultKind, artist, title string) (string, error)
	LookupTrackFeatured(ctx context.Context, trackID string) ([]domain.FeaturedArtist, error)
}

var errFeaturedSourceAbsent = errors.New("featured source is switched off")

type FeaturedArtistResolver struct {
	mb     MBFeaturedSearcher
	deezer DeezerFeaturedLookup
}

func NewFeaturedArtistResolver(mb MBFeaturedSearcher, deezer DeezerFeaturedLookup) (*FeaturedArtistResolver, error) {
	if err := requireCollaborators("the featured artist resolver",
		collaborator{"a MusicBrainz featured searcher", mb == nil},
		collaborator{"a Deezer featured lookup", deezer == nil},
	); err != nil {
		return nil, err
	}
	return &FeaturedArtistResolver{mb: mb, deezer: deezer}, nil
}

type featuredSource struct {
	name  string
	fetch func(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error)
}

func (r *FeaturedArtistResolver) Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error) {
	var attempted int
	var errs []error
	sources := []featuredSource{
		{"musicbrainz", r.mbFeatured},
		{"deezer", r.deezerFeatured},
	}
	found := make([][]domain.FeaturedArtist, len(sources))
	for i, src := range sources {
		feats, err := src.fetch(ctx, artist, title)
		if errors.Is(err, errFeaturedSourceAbsent) {
			continue
		}
		attempted++
		if err != nil {
			errs = append(errs, fmt.Errorf("%s featured: %w", src.name, err))
			logFeaturedFailure(ctx, src.name, err)
		}
		found[i] = feats
	}
	merged := MergeFeaturedArtists(found[0], found[1])
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
		"source", source, logging.KeyError, redact.Secrets(err.Error()))
}

func (r *FeaturedArtistResolver) mbFeatured(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error) {
	results, err := r.mb.SearchStructured(ctx, artist, title, map[domain.ResultKind]bool{domain.ResultKindTrack: true})
	if err != nil {
		return nil, fmt.Errorf("search structured: %w", err)
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
		return nil, fmt.Errorf("resolve id: %w", err)
	}
	if id == "" {
		return nil, nil
	}
	r0, err := r.deezer.LookupTrackFeatured(ctx, id)
	if err != nil {
		return r0, fmt.Errorf("lookup track featured: %w", err)
	}
	return r0, nil
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
