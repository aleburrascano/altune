package domain

import (
	"errors"
	"fmt"
	"strings"
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

func (t TrackId) UUID() uuid.UUID { return t.value }
func (t TrackId) String() string  { return t.value.String() }
func (t TrackId) IsZero() bool    { return t.value == uuid.Nil }

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
	FeaturedArtists   []FeaturedArtist

	AcquisitionProvenance *string
}

func NewTrack(userId shared.UserId, title, artist, album string) (*Track, error) {
	if title == "" {
		return nil, &ValidationError{Message: "track title required"}
	}
	if artist == "" {
		return nil, &ValidationError{Message: "track artist required"}
	}
	resolved := resolveAlbum(album, title)
	return &Track{
		ID:                NewTrackId(),
		UserId:            userId,
		Title:             title,
		Artist:            artist,
		Album:             resolved,
		AddedAt:           time.Now().UTC(),
		AcquisitionStatus: AcquisitionPending,
		DedupKey:          computeDedupKey(title, artist, resolved),
	}, nil
}

func resolveAlbum(album, title string) string {
	if strings.TrimSpace(album) == "" {
		return title
	}
	return album
}

func (t *Track) SetAlbum(album string) {
	t.Album = resolveAlbum(album, t.Title)
	t.DedupKey = computeDedupKey(t.Title, t.Artist, t.Album)
}

type AcquisitionProvenance string

const (
	ProvenanceVerified     AcquisitionProvenance = "verified"
	ProvenanceCorroborated AcquisitionProvenance = "corroborated"
	ProvenanceBestEffort   AcquisitionProvenance = "best_effort"
)

func (p AcquisitionProvenance) Valid() bool {
	switch p {
	case ProvenanceVerified, ProvenanceCorroborated, ProvenanceBestEffort:
		return true
	}
	return false
}

func (t *Track) MarkReady(audioRef string) error {
	if audioRef == "" {
		return errors.New("audio_ref required for ready status")
	}
	t.AcquisitionStatus = AcquisitionReady
	t.AudioRef = &audioRef
	t.FailureReason = nil
	return nil
}

func (t *Track) SetAcquisitionProvenance(p AcquisitionProvenance) {
	if !p.Valid() {
		return
	}
	value := string(p)
	t.AcquisitionProvenance = &value
}

func (t *Track) SetDuration(seconds float64) {
	if seconds > 0 {
		t.DurationSeconds = &seconds
	}
}

func (t *Track) MarkFailed(reason string) error {
	if reason == "" {
		return errors.New("failure_reason required for failed status")
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

func (t *Track) IsStreamable() bool {
	return t.AcquisitionStatus == AcquisitionReady && t.AudioRef != nil
}

var failureMessages = map[string]string{
	"no_match_found":  "Couldn't find this track",
	"download_failed": "Download failed",
	"ytdlp_error":     "Download error",
}

func FailureMessage(reason *string) string {
	if reason == nil {
		return "Acquisition failed"
	}
	if msg, ok := failureMessages[*reason]; ok {
		return msg
	}
	return "Couldn't get this track"
}

func TotalDurationSeconds(tracks []*Track) float64 {
	total := 0.0
	for _, t := range tracks {
		if t.DurationSeconds != nil {
			total += *t.DurationSeconds
		}
	}
	return total
}
