package handler

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
)

// FeaturedArtistDTO is the wire shape of one featured ("feat.") credit — owned by
// the service layer (see service.TrackDTO) so the event payload and the HTTP
// responses share it.
type FeaturedArtistDTO = service.FeaturedArtistDTO

// domainFeaturedFromDTOs converts request DTOs into domain value objects, dropping
// entries with an empty name.
func domainFeaturedFromDTOs(dtos []FeaturedArtistDTO) []domain.FeaturedArtist {
	if len(dtos) == 0 {
		return nil
	}
	out := make([]domain.FeaturedArtist, 0, len(dtos))
	for _, d := range dtos {
		mbid := ""
		if d.MBID != nil {
			mbid = *d.MBID
		}
		deezerID := int64(0)
		if d.DeezerID != nil {
			deezerID = *d.DeezerID
		}
		if fa, ok := domain.NewFeaturedArtist(d.Name, mbid, deezerID); ok {
			out = append(out, fa)
		}
	}
	return out
}
