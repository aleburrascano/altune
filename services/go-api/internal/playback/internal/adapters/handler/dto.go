package handler

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/playback/internal/service"
)

type saveQueueRequest struct {
	TrackIds     []string        `json:"track_ids"`
	CurrentIdx   int             `json:"current_index"`
	PositionMs   int64           `json:"position_ms"`
	Shuffled     bool            `json:"shuffled"`
	RepeatMode   string          `json:"repeat_mode"`
	SourceId     string          `json:"source_id"`
	Source       *queueSourceDTO `json:"source"`
	NaturalOrder []string        `json:"natural_order"`
}

type saveQueuePositionRequest struct {
	CurrentIdx     int    `json:"current_index"`
	CurrentTrackId string `json:"current_track_id"`
	PositionMs     int64  `json:"position_ms"`
}

type queueSourceDTO struct {
	Kind       string `json:"kind"`
	PlaylistId string `json:"playlist_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Query      string `json:"query,omitempty"`
}

type queueStateResponse struct {
	TrackIds                []string              `json:"track_ids"`
	CurrentIdx              int                   `json:"current_index"`
	PositionMs              int64                 `json:"position_ms"`
	Shuffled                bool                  `json:"shuffled"`
	RepeatMode              string                `json:"repeat_mode"`
	SourceId                string                `json:"source_id"`
	Source                  *queueSourceDTO       `json:"source"`
	NaturalOrder            []string              `json:"natural_order"`
	CurrentTrack            *currentTrackResponse `json:"current_track,omitempty"`
	CurrentTrackUnavailable bool                  `json:"current_track_unavailable,omitempty"`
}

type currentTrackResponse struct {
	Id                string   `json:"id"`
	Title             string   `json:"title"`
	Artist            string   `json:"artist"`
	ArtworkURL        *string  `json:"artwork_url"`
	DurationSeconds   *float64 `json:"duration_seconds"`
	AcquisitionStatus string   `json:"acquisition_status"`
}

func toResponse(view *service.ResumeView) queueStateResponse {
	state := view.State
	resp := queueStateResponse{
		TrackIds:     state.TrackIds,
		CurrentIdx:   state.CurrentIdx,
		PositionMs:   state.PositionMs,
		Shuffled:     state.Shuffled,
		RepeatMode:   state.RepeatMode.String(),
		SourceId:     state.SourceId,
		Source:       sourceToDTO(domain.ParseQueueSource(state.SourceId)),
		NaturalOrder: state.NaturalOrder,

		CurrentTrackUnavailable: view.CurrentTrackUnavailable,
	}
	if c := view.CurrentTrack; c != nil {
		resp.CurrentTrack = toCurrentTrackResponse(c)
	}
	return resp
}

func toCurrentTrackResponse(c *ports.NowPlayingTrack) *currentTrackResponse {
	return &currentTrackResponse{
		Id:                c.Id,
		Title:             c.Title,
		Artist:            c.Artist,
		ArtworkURL:        c.ArtworkURL,
		DurationSeconds:   c.DurationSeconds,
		AcquisitionStatus: c.AcquisitionStatus,
	}
}

func sourceFromDTO(dto *queueSourceDTO) domain.QueueSource {
	if dto == nil {
		return domain.QueueSource{}
	}
	return domain.QueueSource{
		Kind:       dto.Kind,
		PlaylistId: dto.PlaylistId,
		Name:       dto.Name,
		Query:      dto.Query,
	}
}

func sourceToDTO(source domain.QueueSource) *queueSourceDTO {
	if source.IsZero() {
		return nil
	}
	return &queueSourceDTO{
		Kind:       source.Kind,
		PlaylistId: source.PlaylistId,
		Name:       source.Name,
		Query:      source.Query,
	}
}
