package events

import (
	"encoding/json"
	"fmt"
)

type Payload interface {
	EventType() EventType
}

type Subjected interface {
	Subject() string
}

type TrackAddedToLibrary struct {
	TrackID string
	Track   any
}

func (TrackAddedToLibrary) EventType() EventType { return TypeTrackAddedToLibrary }

func (p TrackAddedToLibrary) Subject() string { return p.TrackID }

func (p TrackAddedToLibrary) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(p.Track)
	if err != nil {
		return nil, fmt.Errorf("marshal track: %w", err)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("unmarshal track fields: %w", err)
	}
	id, err := json.Marshal(p.TrackID)
	if err != nil {
		return nil, fmt.Errorf("marshal track id: %w", err)
	}
	fields["track_id"] = id
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	return out, nil
}

type TrackDeleted struct {
	TrackID string `json:"track_id"`
}

func (TrackDeleted) EventType() EventType { return typeTrackDeleted }

func (p TrackDeleted) Subject() string { return p.TrackID }

type TrackAcquisitionStarted struct {
	TrackID string `json:"track_id"`
	Title   string `json:"title"`
	Artist  string `json:"artist"`
}

func (TrackAcquisitionStarted) EventType() EventType { return TypeTrackAcquisitionStarted }

func (p TrackAcquisitionStarted) Subject() string { return p.TrackID }

type TrackAcquisitionProgress struct {
	TrackID string `json:"track_id"`
	Stage   string `json:"stage"`
	Title   string `json:"title"`
	Artist  string `json:"artist"`
}

func (TrackAcquisitionProgress) EventType() EventType { return TypeTrackAcquisitionProgress }

func (p TrackAcquisitionProgress) Subject() string { return p.TrackID }

type TrackAcquisitionCompleted struct {
	TrackID  string `json:"track_id"`
	AudioRef string `json:"audio_ref"`
}

func (TrackAcquisitionCompleted) EventType() EventType { return typeTrackAcquisitionCompleted }

func (p TrackAcquisitionCompleted) Subject() string { return p.TrackID }

type TrackAcquisitionFailed struct {
	TrackID string `json:"track_id"`
	Reason  string `json:"reason"`
}

func (TrackAcquisitionFailed) EventType() EventType { return TypeTrackAcquisitionFailed }

func (p TrackAcquisitionFailed) Subject() string { return p.TrackID }

type TrackReplaceFailed struct {
	TrackID string `json:"track_id"`
	Reason  string `json:"reason"`
}

func (TrackReplaceFailed) EventType() EventType { return TypeTrackReplaceFailed }

func (p TrackReplaceFailed) Subject() string { return p.TrackID }

type TrackAddedToPlaylist struct {
	PlaylistID string `json:"playlist_id"`
	TrackID    string `json:"track_id"`
}

func (TrackAddedToPlaylist) EventType() EventType { return typeTrackAddedToPlaylist }

func (p TrackAddedToPlaylist) Subject() string { return p.TrackID }

type TrackRemovedFromPlaylist struct {
	PlaylistID string `json:"playlist_id"`
	TrackID    string `json:"track_id"`
}

func (TrackRemovedFromPlaylist) EventType() EventType { return typeTrackRemovedFromPlaylist }

func (p TrackRemovedFromPlaylist) Subject() string { return p.TrackID }

type TracksAddedToPlaylist struct {
	PlaylistID string   `json:"playlist_id"`
	TrackIDs   []string `json:"track_ids"`
}

func (TracksAddedToPlaylist) EventType() EventType { return typeTracksAddedToPlaylist }

type TracksRemovedFromPlaylist struct {
	PlaylistID string   `json:"playlist_id"`
	TrackIDs   []string `json:"track_ids"`
}

func (TracksRemovedFromPlaylist) EventType() EventType { return typeTracksRemovedFromPlaylist }

type PlaylistReordered struct {
	PlaylistID string   `json:"playlist_id"`
	TrackIDs   []string `json:"track_ids"`
}

func (PlaylistReordered) EventType() EventType { return typePlaylistReordered }

type PlaylistCreated struct {
	PlaylistID string `json:"playlist_id"`
	Name       string `json:"name"`
}

func (PlaylistCreated) EventType() EventType { return typePlaylistCreated }

type PlaylistDeleted struct {
	PlaylistID string `json:"playlist_id"`
}

func (PlaylistDeleted) EventType() EventType { return typePlaylistDeleted }

type PlaylistRenamed struct {
	PlaylistID string `json:"playlist_id"`
	Name       string `json:"name"`
}

func (PlaylistRenamed) EventType() EventType { return TypePlaylistRenamed }
