package service

import "altune/go-api/internal/discovery/domain"

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
