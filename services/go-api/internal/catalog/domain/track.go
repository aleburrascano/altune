package domain

import (
	"altune/go-api/internal/shared"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

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

const (
	acquisitionPendingWire = "pending"
	acquisitionReadyWire   = "ready"
	acquisitionFailedWire  = "failed"
)

func (s AcquisitionStatus) String() string {
	switch s {
	case AcquisitionPending:
		return acquisitionPendingWire
	case AcquisitionReady:
		return acquisitionReadyWire
	case AcquisitionFailed:
		return acquisitionFailedWire
	default:
		return "unknown"
	}
}

func ParseAcquisitionStatus(s string) (AcquisitionStatus, error) {
	switch s {
	case acquisitionPendingWire:
		return AcquisitionPending, nil
	case acquisitionReadyWire:
		return AcquisitionReady, nil
	case acquisitionFailedWire:
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
	IdempotencyKey    *string
	Year              *int
	Genre             *string
	TrackNumber       *int
	AlbumArtist       *string
	ISRC              *string
	AudioRef          *string
	AudioVersion      string
	FailureReason     *string
	FeaturedArtists   []FeaturedArtist

	AcquisitionProvenance *string
	AudioSourceURL        *string
	RejectedSourceKeys    []string

	AcquisitionStartedAt *time.Time

	Version int
}

const maxRejectedSourceKeys = 25

const maxTrackTextLength = 300

const MaxDurationSeconds = 7 * 24 * 60 * 60

func ValidateDurationSeconds(seconds float64) error {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return NewValidationError("duration_seconds must be a finite number")
	}
	if seconds < 0 {
		return NewValidationError("duration_seconds must not be negative")
	}
	if seconds > MaxDurationSeconds {
		return NewValidationError(fmt.Sprintf("duration_seconds exceeds %d", MaxDurationSeconds))
	}
	return nil
}

func trackTextTooLongError(field string) error {
	return NewValidationError(fmt.Sprintf("track %s exceeds %d characters", field, maxTrackTextLength))
}

func ValidateText(value, field string) error {
	if strings.ContainsRune(value, '\x00') {
		return NewValidationError(field + " must not contain a NUL byte")
	}
	return nil
}

func NewTrack(userId shared.UserId, title, artist, album string) (*Track, error) {
	title = strings.TrimSpace(title)
	if err := validateTrackText(title, "title"); err != nil {
		return nil, err
	}
	artist = canonicalizeField(artist)
	if err := validateTrackText(artist, "artist"); err != nil {
		return nil, err
	}
	resolved := resolveAlbum(album, title)
	if err := ValidateText(resolved, "track album"); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Track{
		ID:                   NewTrackId(),
		UserId:               userId,
		Title:                title,
		Artist:               artist,
		Album:                resolved,
		AddedAt:              now,
		AcquisitionStatus:    AcquisitionPending,
		AcquisitionStartedAt: &now,
		DedupKey:             computeDedupKey(title, artist, resolved),
	}, nil
}

func validateTrackText(value, field string) error {
	if value == "" {
		return NewValidationError("track " + field + " required")
	}
	if err := ValidateText(value, "track "+field); err != nil {
		return err
	}
	if len(value) > maxTrackTextLength {
		return trackTextTooLongError(field)
	}
	return nil
}

func ValidateOptionalTrackText(value *string, field string) error {
	if value == nil {
		return nil
	}
	if err := ValidateText(*value, "track "+field); err != nil {
		return err
	}
	if len(*value) > maxTrackTextLength {
		return trackTextTooLongError(field)
	}
	return nil
}

func resolveAlbum(album, title string) string {
	album = canonicalizeField(album)
	if album == "" {
		return title
	}
	return album
}

func (t *Track) SetAlbum(album string) {
	t.Album = resolveAlbum(album, t.Title)
	t.DedupKey = computeDedupKey(t.Title, t.Artist, t.Album)
}

func (t *Track) SetAlbumArtist(albumArtist string) {
	canonical := canonicalizeField(albumArtist)
	if canonical == "" {
		t.AlbumArtist = nil
		return
	}
	t.AlbumArtist = &canonical
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

var ErrIllegalAcquisitionTransition = errors.New("illegal acquisition status transition")

func illegalTransition(op string, from AcquisitionStatus) error {
	return fmt.Errorf("%w: %s from %s", ErrIllegalAcquisitionTransition, op, from)
}

func (t *Track) MarkReady(audioRef string) error {
	if audioRef == "" {
		return errors.New("audio_ref required for ready status")
	}
	if t.AcquisitionStatus == AcquisitionReady && t.AudioRef != nil && *t.AudioRef != audioRef {
		return illegalTransition("mark ready", t.AcquisitionStatus)
	}
	t.setReadyAudio(audioRef)
	return nil
}

func (t *Track) ReplaceAudio(audioRef string) error {
	if audioRef == "" {
		return errors.New("audio_ref required for ready status")
	}
	if !t.IsStreamable() {
		return illegalTransition("replace audio", t.AcquisitionStatus)
	}
	t.setReadyAudio(audioRef)
	return nil
}

func (t *Track) setReadyAudio(audioRef string) {
	t.AcquisitionStatus = AcquisitionReady
	t.AudioRef = &audioRef
	t.AudioVersion = uuid.NewString()
	t.FailureReason = nil
	t.AcquisitionStartedAt = nil
}

func (t *Track) SetAudioSource(url string) {
	if url == "" {
		return
	}
	t.AudioSourceURL = &url
}

func (t *Track) RejectAudioSource(key string) {
	if key == "" {
		return
	}
	for _, existing := range t.RejectedSourceKeys {
		if existing == key {
			return
		}
	}
	t.RejectedSourceKeys = append(t.RejectedSourceKeys, key)
	if len(t.RejectedSourceKeys) > maxRejectedSourceKeys {
		t.RejectedSourceKeys = t.RejectedSourceKeys[len(t.RejectedSourceKeys)-maxRejectedSourceKeys:]
	}
}

func (t *Track) SetAcquisitionProvenance(p AcquisitionProvenance) {
	if !p.Valid() {
		return
	}
	value := string(p)
	t.AcquisitionProvenance = &value
}

func (t *Track) SetDuration(seconds float64) error {
	if err := ValidateDurationSeconds(seconds); err != nil {
		return err
	}
	if seconds > 0 {
		t.DurationSeconds = &seconds
	}
	return nil
}

func (t *Track) MarkFailed(reason string) error {
	if reason == "" {
		return errors.New("failure_reason required for failed status")
	}
	if t.AcquisitionStatus == AcquisitionFailed {
		return illegalTransition("mark failed", t.AcquisitionStatus)
	}
	t.AcquisitionStatus = AcquisitionFailed
	t.FailureReason = &reason
	t.AudioRef = nil
	t.AcquisitionStartedAt = nil
	return nil
}

func (t *Track) FailAcquisition(reason string) error {
	if t.AcquisitionStatus != AcquisitionPending {
		return illegalTransition("fail acquisition", t.AcquisitionStatus)
	}
	return t.MarkFailed(reason)
}

func (t *Track) RevertToPending() error {
	if t.AcquisitionStatus == AcquisitionPending {
		return illegalTransition("revert to pending", t.AcquisitionStatus)
	}
	now := time.Now().UTC()
	t.AcquisitionStatus = AcquisitionPending
	t.AudioRef = nil
	t.FailureReason = nil
	t.AcquisitionStartedAt = &now
	return nil
}

func (t *Track) IsStreamable() bool {
	return t.AcquisitionStatus == AcquisitionReady && t.AudioRef != nil
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
