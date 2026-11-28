package handler

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/service"
	"time"

	"github.com/google/uuid"
)

type resolveAudioURLsRequest struct {
	TrackIDs []string `json:"track_ids"`
}

type audioURLDTO struct {
	TrackID   string `json:"track_id"`
	URL       string `json:"url"`
	Version   string `json:"version"`
	ExpiresAt string `json:"expires_at"`
}

type resolveAudioURLsResponse struct {
	URLs            []audioURLDTO `json:"urls"`
	PrefetchEnabled bool          `json:"prefetch_enabled"`
}

type AlbumGroupDTO struct {
	Key               string  `json:"key"`
	Album             string  `json:"album"`
	Artist            string  `json:"artist"`
	ArtworkURL        *string `json:"artwork_url"`
	Year              *int    `json:"year"`
	TrackCount        int     `json:"track_count"`
	MostRecentAddedAt string  `json:"most_recent_added_at"`
}

type ArtistGroupDTO struct {
	Key               string  `json:"key"`
	Artist            string  `json:"artist"`
	ArtworkURL        *string `json:"artwork_url"`
	TrackCount        int     `json:"track_count"`
	MostRecentAddedAt string  `json:"most_recent_added_at"`
}

type CreatePlaylistRequest struct {
	Name string `json:"name"`
}

type RenamePlaylistRequest struct {
	Name string `json:"name"`
}

type AddTrackToPlaylistRequest struct {
	TrackID uuid.UUID `json:"track_id"`
}

type AddTracksToPlaylistRequest struct {
	TrackIDs []uuid.UUID `json:"track_ids"`
}

type AddTracksToPlaylistResponse struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

type RemoveTracksFromPlaylistRequest struct {
	TrackIDs []uuid.UUID `json:"track_ids"`
}

type RemoveTracksFromPlaylistResponse struct {
	Removed int `json:"removed"`
}

type ReorderTracksRequest struct {
	TrackIDs []uuid.UUID `json:"track_ids"`
}

type PlaylistResponse struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	TrackCount         int       `json:"track_count"`
	PreviewArtworkURLs []string  `json:"preview_artwork_urls"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type PlaylistDetailResponse struct {
	PlaylistResponse
	TotalDurationSeconds float64         `json:"total_duration_seconds"`
	Tracks               []TrackResponse `json:"tracks"`
}

func playlistToResponse(p *domain.Playlist, trackCount int, artworkURLs []string) PlaylistResponse {
	if artworkURLs == nil {
		artworkURLs = []string{}
	}
	return PlaylistResponse{
		ID:                 p.ID.UUID(),
		Name:               p.Name,
		TrackCount:         trackCount,
		PreviewArtworkURLs: artworkURLs,
		CreatedAt:          p.CreatedAt,
		UpdatedAt:          p.UpdatedAt,
	}
}

type SetTrackNumberRequest struct {
	TrackNumber int `json:"track_number"`
}

type CreateTrackRequest struct {
	Title           string                 `json:"title"`
	Artist          string                 `json:"artist"`
	Album           *string                `json:"album,omitempty"`
	DurationSeconds *float64               `json:"duration_seconds,omitempty"`
	ArtworkURL      *string                `json:"artwork_url,omitempty"`
	ISRC            *string                `json:"isrc,omitempty"`
	Year            *int                   `json:"year,omitempty"`
	Genre           *string                `json:"genre,omitempty"`
	AlbumArtist     *string                `json:"album_artist,omitempty"`
	TrackNumber     *int                   `json:"track_number,omitempty"`
	FeaturedArtists []FeaturedArtistDTO    `json:"featured_artists,omitempty"`
	SourceURL       *string                `json:"source_url,omitempty"`
	MBID            *string                `json:"mbid,omitempty"`
	Explicit        *bool                  `json:"explicit,omitempty"`
	Sources         []CatalogSourceRequest `json:"sources,omitempty"`
}

type CatalogSourceRequest struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
}

type TrackResponse = TrackDTO

type ListTracksResponse struct {
	Items   []TrackResponse `json:"items"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	HasMore bool            `json:"has_more"`
}

func tracksToDTO(tracks []*domain.Track) []TrackResponse {
	out := make([]TrackResponse, len(tracks))
	for i, t := range tracks {
		out[i] = TrackToDTO(t)
	}
	return out
}

type TrackStatusResponse struct {
	ID             uuid.UUID `json:"id"`
	DownloadStatus string    `json:"acquisition_status"`
	FailureReason  *string   `json:"failure_reason,omitempty"`
}

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

type TrackDTO struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	Artist          string              `json:"artist"`
	Album           *string             `json:"album"`
	DurationSeconds *float64            `json:"duration_seconds"`
	AddedAt         time.Time           `json:"added_at"`
	DownloadStatus  string              `json:"acquisition_status"`
	ArtworkURL      *string             `json:"artwork_url"`
	Year            *int                `json:"year,omitempty"`
	Genre           *string             `json:"genre,omitempty"`
	TrackNumber     *int                `json:"track_number,omitempty"`
	AlbumArtist     *string             `json:"album_artist,omitempty"`
	ISRC            *string             `json:"isrc,omitempty"`
	FailureReason   *string             `json:"failure_reason,omitempty"`
	FailureMessage  *string             `json:"failure_message,omitempty"`
	FeaturedArtists []FeaturedArtistDTO `json:"featured_artists,omitempty"`

	AudioRef *string `json:"-"`
}

type FeaturedArtistDTO struct {
	Name     string  `json:"name"`
	MBID     *string `json:"mbid,omitempty"`
	DeezerID *int64  `json:"deezer_id,omitempty"`
}

func TrackToDTO(t *domain.Track) TrackDTO {
	var album *string
	if name := t.Album(); name != "" {
		album = &name
	}
	var failureMessage *string
	if t.DownloadStatus() == domain.AcquisitionFailed {
		msg := domain.FailureMessage(t.FailureReason())
		failureMessage = &msg
	}
	return TrackDTO{
		ID:              t.ID().String(),
		Title:           t.Title(),
		Artist:          t.Artist(),
		Album:           album,
		DurationSeconds: t.DurationSeconds(),
		AddedAt:         t.AddedAt(),
		DownloadStatus:  t.DownloadStatus().String(),
		ArtworkURL:      t.ArtworkURL(),
		Year:            t.Year(),
		Genre:           t.Genre(),
		TrackNumber:     t.TrackNumber(),
		AlbumArtist:     t.AlbumArtist(),
		ISRC:            t.ISRC(),
		AudioRef:        t.AudioRef(),
		FailureReason:   t.FailureReason(),
		FailureMessage:  failureMessage,
		FeaturedArtists: FeaturedToDTOs(t.FeaturedArtists()),
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

type BackfillFeaturedResponse struct {
	Scanned    int  `json:"scanned"`
	Updated    int  `json:"updated"`
	Failed     int  `json:"failed"`
	Truncated  bool `json:"truncated"`
	NextOffset int  `json:"next_offset"`
}

func backfillResponse(r *service.BackfillFeaturedResult) BackfillFeaturedResponse {
	return BackfillFeaturedResponse{Scanned: r.Scanned, Updated: r.Updated, Failed: r.Failed, Truncated: r.Truncated, NextOffset: r.NextOffset}
}
