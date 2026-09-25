package domain

import (
	"altune/go-api/internal/shared"
	"strings"
	"time"

	"github.com/google/uuid"
)

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

const PreviewArtworkLimit = 4

// MaxPlaylistTracks is the most tracks a playlist may hold. It equals the
// catalog's bounded-read size on purpose: tracks past one bounded read can
// neither be listed nor reordered, so capping growth at that same number is
// what keeps every playlist readable whole (#2196).
const MaxPlaylistTracks = MaxLibraryPageSize

// ErrPlaylistFull refuses an add that would take a playlist past
// MaxPlaylistTracks. A playlist stored over the cap before it existed keeps
// every track it has; only further adds are refused.
var ErrPlaylistFull = &CodedError{Msg: "playlist is full", Status: 400, Code: "catalog.playlist_full"}

type Playlist struct {
	ID        PlaylistId
	UserId    shared.UserId
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Tracks    []PlaylistTrack
}

type PlaylistSummary struct {
	TrackCount         int
	PreviewArtworkURLs []string
}

type PlaylistWithSummary struct {
	Playlist *Playlist
	Summary  PlaylistSummary
}

// NewPlaylist builds an empty playlist with the trimmed name, stamped at now
// (stored as UTC). The time source is the caller's so tests can pin
// CreatedAt/UpdatedAt.
func NewPlaylist(userId shared.UserId, name string, now time.Time) (*Playlist, error) {
	name, err := validatePlaylistName(name)
	if err != nil {
		return nil, err
	}
	stamp := now.UTC()
	return &Playlist{
		ID:        NewPlaylistId(),
		UserId:    userId,
		Name:      name,
		CreatedAt: stamp,
		UpdatedAt: stamp,
		Tracks:    nil,
	}, nil
}

// Rename sets the trimmed name and stamps UpdatedAt with now (stored as UTC).
func (p *Playlist) Rename(name string, now time.Time) error {
	name, err := validatePlaylistName(name)
	if err != nil {
		return err
	}
	p.Name = name
	p.UpdatedAt = now.UTC()
	return nil
}

// AddTrack appends trackId and stamps UpdatedAt with now (stored as UTC).
func (p *Playlist) AddTrack(trackId TrackId, now time.Time) error {
	for _, t := range p.Tracks {
		if t.TrackId == trackId {
			return ErrTrackAlreadyInPlaylist
		}
	}
	p.Tracks = append(p.Tracks, PlaylistTrack{
		TrackId:  trackId,
		Position: len(p.Tracks),
	})
	p.UpdatedAt = now.UTC()
	return nil
}

// RemoveTrack drops trackId, if present, and stamps UpdatedAt with now
// (stored as UTC). It reports whether the track was present.
func (p *Playlist) RemoveTrack(trackId TrackId, now time.Time) bool {
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
	p.UpdatedAt = now.UTC()
	return true
}

// Reorder applies trackIds as the new order and stamps UpdatedAt with now
// (stored as UTC).
func (p *Playlist) Reorder(trackIds []TrackId, now time.Time) error {
	if len(trackIds) != len(p.Tracks) {
		return NewValidationError("track list length mismatch")
	}

	existing := make(map[TrackId]bool)
	for _, t := range p.Tracks {
		existing[t.TrackId] = true
	}
	seen := make(map[TrackId]bool)
	for _, id := range trackIds {
		if !existing[id] {
			return NewValidationError("unknown track in reorder list")
		}
		if seen[id] {
			return NewValidationError("duplicate track in reorder list")
		}
		seen[id] = true
	}

	newTracks := make([]PlaylistTrack, len(trackIds))
	for i, id := range trackIds {
		newTracks[i] = PlaylistTrack{TrackId: id, Position: i}
	}
	p.Tracks = newTracks
	p.UpdatedAt = now.UTC()
	return nil
}

// validatePlaylistName trims surrounding whitespace before checking, so a
// whitespace-only name is rejected as empty, and returns the trimmed name.
func validatePlaylistName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", NewValidationError("playlist name required")
	}
	if err := ValidateText(name, "playlist name"); err != nil {
		return "", err
	}
	if len(name) > 100 {
		return "", NewValidationError("playlist name exceeds 100 characters")
	}
	return name, nil
}

func PreviewArtworkURLs(tracks []*Track) []string {
	urls := []string{}
	seen := make(map[string]bool)
	for _, t := range tracks {
		if t.ArtworkURL != nil && !seen[*t.ArtworkURL] && len(urls) < PreviewArtworkLimit {
			urls = append(urls, *t.ArtworkURL)
			seen[*t.ArtworkURL] = true
		}
	}
	return urls
}
