package providers

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

type deezerContributor struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func (a *DeezerAdapter) LookupTrackFeatured(ctx context.Context, trackID string) ([]domain.FeaturedArtist, error) {
	var detail struct {
		Contributors []deezerContributor `json:"contributors"`
	}
	u := fmt.Sprintf("https://api.deezer.com/track/%s", url.PathEscape(trackID))
	if err := a.getJSON(ctx, u, &detail); err != nil {
		return nil, err
	}
	return extractDeezerFeatured(detail.Contributors), nil
}

func extractDeezerFeatured(cs []deezerContributor) []domain.FeaturedArtist {
	out := make([]domain.FeaturedArtist, 0, len(cs))
	seen := make(map[string]bool, len(cs))
	primarySkipped := false
	for _, c := range cs {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		if !primarySkipped && strings.EqualFold(strings.TrimSpace(c.Role), "main") {
			primarySkipped = true
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, domain.FeaturedArtist{Name: name, DeezerID: c.ID, Role: domain.RoleFeatured})
	}
	return out
}
