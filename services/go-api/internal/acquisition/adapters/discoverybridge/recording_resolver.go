package discoverybridge

import (
	"context"
	"fmt"
	"strings"

	acqports "altune/go-api/internal/acquisition/ports"
	discoverydomain "altune/go-api/internal/discovery/domain"
	discoveryservice "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"
)

const resolveLimit = 10

var _ acqports.RecordingResolver = (*RecordingResolver)(nil)

type RecordingResolver struct {
	search *discoveryservice.Service
}

func NewRecordingResolver(search *discoveryservice.Service) *RecordingResolver {
	return &RecordingResolver{search: search}
}

func (r *RecordingResolver) Resolve(ctx context.Context, q acqports.RecordingQuery) (acqports.RecordingIdentity, error) {
	if r.search == nil || q.Title == "" || q.Artist == "" {
		return acqports.RecordingIdentity{}, nil
	}

	query, err := discoverydomain.NewSearchQuery(
		q.Artist+" "+q.Title,
		map[discoverydomain.ResultKind]bool{discoverydomain.ResultKindTrack: true},
		resolveLimit,
	)
	if err != nil {
		return acqports.RecordingIdentity{}, fmt.Errorf("build resolve query: %w", err)
	}

	out, err := r.search.Execute(ctx, shared.UserId{}, query, false)
	if err != nil {
		return acqports.RecordingIdentity{}, fmt.Errorf("resolve recording: %w", err)
	}
	if out == nil || len(out.Results) == 0 {
		return acqports.RecordingIdentity{}, nil
	}

	best, ok := pickRecording(out.Results, q)
	if !ok {
		return acqports.RecordingIdentity{}, nil
	}
	return toIdentity(best), nil
}

func pickRecording(results []discoverydomain.SearchResult, q acqports.RecordingQuery) (discoverydomain.SearchResult, bool) {
	if q.ISRC != "" {
		for _, res := range results {
			if strings.EqualFold(res.ISRC, q.ISRC) {
				return res, true
			}
		}
	}

	wantTitle := textnorm.NormalizeForMatch(q.Title)
	wantArtist := textnorm.NormalizeForMatch(q.Artist)
	for _, res := range results {
		if res.Kind != discoverydomain.ResultKindTrack {
			continue
		}
		if textnorm.NormalizeForMatch(res.Title) != wantTitle {
			continue
		}
		if wantArtist != "" && textnorm.NormalizeForMatch(res.Subtitle) != wantArtist {
			continue
		}
		return res, true
	}
	return discoverydomain.SearchResult{}, false
}

func toIdentity(res discoverydomain.SearchResult) acqports.RecordingIdentity {
	identity := acqports.RecordingIdentity{
		ISRC:     res.ISRC,
		MBID:     res.MBID,
		Duration: float64(res.Duration),
	}
	for _, src := range res.Sources {
		if src.ExternalID == "" {
			continue
		}
		identity.Sources = append(identity.Sources, acqports.RecordingSource{
			Provider:   src.Provider.String(),
			ExternalID: src.ExternalID,
			URL:        src.URL,
		})
	}
	return identity
}
