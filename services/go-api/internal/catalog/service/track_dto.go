package service

import (
	"time"

	"altune/go-api/internal/catalog/domain"

	"github.com/google/uuid"
)

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
	FailureMessage    *string             `json:"failure_message,omitempty"`
	FeaturedArtists   []FeaturedArtistDTO `json:"featured_artists,omitempty"`
}

type FeaturedArtistDTO struct {
	Name     string  `json:"name"`
	MBID     *string `json:"mbid,omitempty"`
	DeezerID *int64  `json:"deezer_id,omitempty"`
}

func TrackToDTO(t *domain.Track) TrackDTO {
	var album *string
	if t.Album != "" {
		album = &t.Album
	}
	var failureMessage *string
	if t.AcquisitionStatus == domain.AcquisitionFailed {
		msg := domain.FailureMessage(t.FailureReason)
		failureMessage = &msg
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
		FailureMessage:    failureMessage,
		FeaturedArtists:   FeaturedToDTOs(t.FeaturedArtists),
	}
}

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
