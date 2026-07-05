package handler

import "altune/go-api/internal/catalog/domain"

// FeaturedArtistDTO is the wire shape of one featured ("feat.") credit. Optional
// ids are omitted when unknown so absence stays distinct from a zero id.
type FeaturedArtistDTO struct {
	Name     string  `json:"name"`
	MBID     *string `json:"mbid,omitempty"`
	DeezerID *int64  `json:"deezer_id,omitempty"`
}

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

// featuredToDTOs converts domain value objects into wire DTOs.
func featuredToDTOs(feats []domain.FeaturedArtist) []FeaturedArtistDTO {
	if len(feats) == 0 {
		return nil
	}
	out := make([]FeaturedArtistDTO, 0, len(feats))
	for _, f := range feats {
		dto := FeaturedArtistDTO{Name: f.Name}
		if f.MBID != "" {
			mbid := f.MBID
			dto.MBID = &mbid
		}
		if f.DeezerID != 0 {
			id := f.DeezerID
			dto.DeezerID = &id
		}
		out = append(out, dto)
	}
	return out
}
