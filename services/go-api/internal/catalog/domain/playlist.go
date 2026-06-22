package domain

import (
	"time"

	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// CodedError is a domain/service error that carries its own HTTP status and a
// client-safe message. httputil.HandleServiceError maps it without the adapter
// re-deciding the status per handler. The status is a plain int so the domain
// layer stays free of net/http.
type CodedError struct {
	Msg    string
	Status int
}

func (e *CodedError) Error() string   { return e.Msg }
func (e *CodedError) HTTPStatus() int { return e.Status }

var ErrTrackAlreadyInPlaylist = &CodedError{Msg: "track already in playlist", Status: 409}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string   { return e.Message }
func (e *ValidationError) HTTPStatus() int { return 400 }

type PlaylistId struct {
	value uuid.UUID
}

func NewPlaylistId() PlaylistId {
	return PlaylistId{value: uuid.New()}
}

func ParsePlaylistId(s string) (PlaylistId, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return PlaylistId{}, err
	}
	return PlaylistId{value: id}, nil
}

func PlaylistIdFromUUID(id uuid.UUID) PlaylistId {
	return PlaylistId{value: id}
}

func (p PlaylistId) UUID() uuid.UUID { return p.value }
func (p PlaylistId) String() string  { return p.value.String() }
func (p PlaylistId) IsZero() bool    { return p.value == uuid.Nil }

type PlaylistTrack struct {
	TrackId  TrackId
	Position int
}

type Playlist struct {
	ID        PlaylistId
	UserId    shared.UserId
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Tracks    []PlaylistTrack
	// TrackCount is a read-side projection populated by list queries.
	TrackCount int
}

func NewPlaylist(userId shared.UserId, name string) (*Playlist, error) {
	if err := validatePlaylistName(name); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Playlist{
		ID:        NewPlaylistId(),
		UserId:    userId,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
		Tracks:    nil,
	}, nil
}

func (p *Playlist) Rename(name string) error {
	if err := validatePlaylistName(name); err != nil {
		return err
	}
	p.Name = name
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (p *Playlist) AddTrack(trackId TrackId) error {
	for _, t := range p.Tracks {
		if t.TrackId == trackId {
			return ErrTrackAlreadyInPlaylist
		}
	}
	p.Tracks = append(p.Tracks, PlaylistTrack{
		TrackId:  trackId,
		Position: len(p.Tracks),
	})
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (p *Playlist) RemoveTrack(trackId TrackId) bool {
	idx := -1
	for i, t := range p.Tracks {
		if t.TrackId == trackId {
			idx = i
			break
		}
	}
	if idx == -1 {
		return false
	}
	p.Tracks = append(p.Tracks[:idx], p.Tracks[idx+1:]...)
	for i := idx; i < len(p.Tracks); i++ {
		p.Tracks[i].Position = i
	}
	p.UpdatedAt = time.Now().UTC()
	return true
}

func (p *Playlist) Reorder(trackIds []TrackId) error {
	if len(trackIds) != len(p.Tracks) {
		return &ValidationError{Message: "track list length mismatch"}
	}

	existing := make(map[TrackId]bool)
	for _, t := range p.Tracks {
		existing[t.TrackId] = true
	}
	for _, id := range trackIds {
		if !existing[id] {
			return &ValidationError{Message: "unknown track in reorder list"}
		}
	}

	newTracks := make([]PlaylistTrack, len(trackIds))
	for i, id := range trackIds {
		newTracks[i] = PlaylistTrack{TrackId: id, Position: i}
	}
	p.Tracks = newTracks
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func validatePlaylistName(name string) error {
	if name == "" {
		return &ValidationError{Message: "playlist name required"}
	}
	if len(name) > 100 {
		return &ValidationError{Message: "playlist name exceeds 100 characters"}
	}
	return nil
}
