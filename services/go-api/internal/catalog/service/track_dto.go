package service

import (
	"time"

	"altune/go-api/internal/catalog/domain"

	"github.com/google/uuid"
)

// TrackDTO is the single wire shape of a track. Both serializers of the track
// contract use it: the HTTP handler's responses (the handler aliases it as
// TrackResponse) and the track_added_to_library event payload (trackAddedPayload
// marshals it). Keeping one struct is what makes the event's promise — "the row
// the client would have fetched" — hold by construction instead of by comment.
type TrackDTO struct {
	ID                uuid.UUID           `json:"id"`
	Title             string              `json:"title"`
	Artist            string              `json:"artist"`
	Album             *string             `json:"album"`
	DurationSeconds   *float64            `json:"duration_seconds"`
	AddedAt           time.Time           `json:"added_at"`
	AcquisitionStatus string              `json:"acquisition_status"`
	ArtworkURL        *string             `json:"artwork_url"`
	Year              *int                `json:"year,omitempty"`
	Genre             *string             `json:"genre,omitempty"`
	TrackNumber       *int                `json:"track_number,omitempty"`
	AlbumArtist       *string             `json:"album_artist,omitempty"`
	ISRC              *string             `json:"isrc,omitempty"`
	AudioRef          *string             `json:"audio_ref,omitempty"`
	FailureReason     *string             `json:"failure_reason,omitempty"`
	FeaturedArtists   []FeaturedArtistDTO `json:"featured_artists,omitempty"`
}

// FeaturedArtistDTO is the wire shape of one featured ("feat.") credit. Optional
// ids are pointer-typed and omitted when unknown so absence stays distinct from a
// zero id.
type FeaturedArtistDTO struct {
	Name     string  `json:"name"`
	MBID     *string `json:"mbid,omitempty"`
	DeezerID *int64  `json:"deezer_id,omitempty"`
}

// TrackToDTO maps a domain track to its wire shape. `album` is null when empty,
// matching the wire contract.
func TrackToDTO(t *domain.Track) TrackDTO {
	var album *string
	if t.Album != "" {
		album = &t.Album
	}
	return TrackDTO{
		ID:                t.ID.UUID(),
		Title:             t.Title,
		Artist:            t.Artist,
		Album:             album,
		DurationSeconds:   t.DurationSeconds,
		AddedAt:           t.AddedAt,
		AcquisitionStatus: t.AcquisitionStatus.String(),
		ArtworkURL:        t.ArtworkURL,
		Year:              t.Year,
		Genre:             t.Genre,
		TrackNumber:       t.TrackNumber,
		AlbumArtist:       t.AlbumArtist,
		ISRC:              t.ISRC,
		AudioRef:          t.AudioRef,
		FailureReason:     t.FailureReason,
		FeaturedArtists:   FeaturedToDTOs(t.FeaturedArtists),
	}
}

// FeaturedToDTOs converts domain value objects into wire DTOs. Returns nil for
// no credits (omitted on the wire).
func FeaturedToDTOs(feats []domain.FeaturedArtist) []FeaturedArtistDTO {
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
