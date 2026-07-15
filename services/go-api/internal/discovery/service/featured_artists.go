package service

import "altune/go-api/internal/discovery/domain"

// MergeFeaturedArtists combines MusicBrainz-sourced featured artists (the
// identity authority — they carry MBIDs) with Deezer-sourced ones (they carry
// Deezer artist ids and an explicit "featured" role). MB-primary, Deezer fills
// gaps: MB order is preserved; a Deezer credit matching an MB credit by
// normalized name enriches that credit with its Deezer id; a Deezer credit with
// no MB match is appended (a gap MB missed). This is the merge the resolver
// applies for both the live path and the backfill.
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
