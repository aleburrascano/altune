package domain

import (
	"errors"
	"fmt"
	"time"

	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type TrackId struct {
	value uuid.UUID
}

func NewTrackId() TrackId {
	return TrackId{value: uuid.New()}
}

func ParseTrackId(s string) (TrackId, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return TrackId{}, err
	}
	return TrackId{value: id}, nil
}

func TrackIdFromUUID(id uuid.UUID) TrackId {
	return TrackId{value: id}
}

func (t TrackId) UUID() uuid.UUID   { return t.value }
func (t TrackId) String() string    { return t.value.String() }
func (t TrackId) IsZero() bool      { return t.value == uuid.Nil }

type AcquisitionStatus int

const (
	AcquisitionPending AcquisitionStatus = iota
	AcquisitionReady
	AcquisitionFailed
)

func (s AcquisitionStatus) String() string {
	switch s {
	case AcquisitionPending:
		return "pending"
	case AcquisitionReady:
		return "ready"
	case AcquisitionFailed:
		return "failed"
	default:
		return "unknown"
	}
}

func ParseAcquisitionStatus(s string) (AcquisitionStatus, error) {
	switch s {
	case "pending":
		return AcquisitionPending, nil
	case "ready":
		return AcquisitionReady, nil
	case "failed":
		return AcquisitionFailed, nil
	default:
		return 0, fmt.Errorf("unknown acquisition status: %s", s)
	}
}

type Track struct {
	ID                TrackId
	UserId            shared.UserId
	Title             string
	Artist            string
	Album             string
	DurationSeconds   *float64
	AddedAt           time.Time
	ArtworkURL        *string
	AcquisitionStatus AcquisitionStatus
	DedupKey          string
	Year              *int
	Genre             *string
	TrackNumber       *int
	AlbumArtist       *string
	ISRC              *string
	AudioRef          *string
	FailureReason     *string
}

func NewTrack(userId shared.UserId, title, artist, album string) *Track {
	return &Track{
		ID:                NewTrackId(),
		UserId:            userId,
		Title:             title,
		Artist:            artist,
		Album:             album,
		AddedAt:           time.Now().UTC(),
		AcquisitionStatus: AcquisitionPending,
		DedupKey:          ComputeDedupKey(title, artist, album),
	}
}

func (t *Track) MarkReady(audioRef string) error {
	if audioRef == "" {
		return errors.New("audio_ref is required to mark track as ready")
	}
	t.AcquisitionStatus = AcquisitionReady
	t.AudioRef = &audioRef
	t.FailureReason = nil
	return nil
}

func (t *Track) MarkFailed(reason string) error {
	if reason == "" {
		return errors.New("failure_reason is required to mark track as failed")
	}
	t.AcquisitionStatus = AcquisitionFailed
	t.FailureReason = &reason
	t.AudioRef = nil
	return nil
}

func (t *Track) RevertToPending() {
	t.AcquisitionStatus = AcquisitionPending
	t.AudioRef = nil
	t.FailureReason = nil
}
