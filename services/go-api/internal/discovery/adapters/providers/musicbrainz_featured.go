package providers

import (
	"regexp"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

// mbFeatJoinRe matches a "feat."/"ft."/"featuring" join phrase between artist
// credits. MusicBrainz encodes a featured artist as the credit(s) following a
// credit whose joinphrase is " feat. " — distinct from a collaboration join
// (" & ", ", ") which does not denote a guest.
var mbFeatJoinRe = regexp.MustCompile(`(?i)\b(?:feat|ft|featuring|with)\b`)

// extractMBFeatured pulls the featured artists out of a MusicBrainz artist-credit
// list. The primary artist is credit [0]; a credit becomes "featured" once a
// preceding join phrase marks a feat. boundary — every credit after that
// boundary is a guest (so "Main feat. A & B" yields A and B). Each carries the
// artist's MBID when MusicBrainz linked it.
func extractMBFeatured(credits []mbArtistRef) []domain.FeaturedArtist {
	out := make([]domain.FeaturedArtist, 0, len(credits))
	featured := false
	for _, c := range credits {
		if featured {
			if fa, ok := mbCreditToFeatured(c); ok {
				out = append(out, fa)
			}
		}
		if mbFeatJoinRe.MatchString(c.JoinPhrase) {
			featured = true
		}
	}
	return out
}

func mbCreditToFeatured(c mbArtistRef) (domain.FeaturedArtist, bool) {
	name := strings.TrimSpace(c.Name)
	fa := domain.FeaturedArtist{Role: domain.RoleFeatured}
	if c.Artist != nil {
		fa.MBID = c.Artist.ID
		if name == "" {
			name = strings.TrimSpace(c.Artist.Name)
		}
	}
	fa.Name = name
	return fa, name != ""
}
