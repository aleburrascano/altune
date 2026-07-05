package providers

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

// deezerContributor is one entry in a Deezer track's `contributors` array. Role
// is "Main" for the primary artist(s) and "Featured" for guests.
type deezerContributor struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// LookupTrackFeatured fetches a Deezer track by id and returns its featured
// contributors (role == "Featured") as FeaturedArtists carrying the Deezer artist
// id. A non-200 or decode failure returns an error so the resolver can degrade.
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

// extractDeezerFeatured keeps only the "Featured"-role contributors, mapping each
// to a FeaturedArtist with its Deezer artist id.
// extractDeezerFeatured surfaces a track's collaborators: every "Featured"
// contributor, plus co-primary artists (a 2nd+ "Main"). The scene the app is used
// for credits guests as co-primary "Main" rather than "Featured", so the first
// "Main" is treated as the track's own artist (skipped) and everyone after —
// whatever their role — is a credit. Deduped by name; blanks dropped.
func extractDeezerFeatured(cs []deezerContributor) []domain.FeaturedArtist {
	out := make([]domain.FeaturedArtist, 0, len(cs))
	seen := make(map[string]bool, len(cs))
	primarySkipped := false
	for _, c := range cs {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		// The first "Main" contributor is the primary artist — skip it once.
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
