package domain

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/tracklength"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type TrackId = ids.TrackId

func NewTrackId() TrackId { return ids.NewTrackId() }

func ParseTrackId(s string) (TrackId, error) { return ids.ParseTrackId(s) }

func TrackIdFromUUID(id [16]byte) TrackId { return ids.TrackIdFromUUID(id) }

type DownloadStatus int

const (
	AcquisitionPending DownloadStatus = iota
	AcquisitionReady
	AcquisitionFailed
)

const (
	acquisitionPendingWire = "pending"
	acquisitionReadyWire   = "ready"
	acquisitionFailedWire  = "failed"
)

func (s DownloadStatus) String() string {
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

func ParseAcquisitionStatus(s string) (DownloadStatus, error) {
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
	ID              TrackId
	UserId          ids.UserId
	Title           string
	Artist          string
	Album           string
	DurationSeconds *float64
	AddedAt         time.Time
	ArtworkURL      *string
	DownloadStatus  DownloadStatus
	DedupKey        string
	IdempotencyKey  *string
	Year            *int
	Genre           *string
	TrackNumber     *int
	AlbumArtist     *string
	ISRC            *string
	AudioRef        *string
	AudioVersion    string
	FailureReason   *string
	FeaturedArtists []FeaturedArtist

	AcquisitionProvenance *string
	AudioSourceURL        *string
	RejectedSourceKeys    []string

	ConfidenceScore *float64
	EvidenceJSON    json.RawMessage

	CatalogDurationSeconds *float64
	CatalogMBID            *string
	CatalogExplicit        *bool
	CatalogSources         []CatalogSource
	AudioLengthMismatch    bool

	AcquisitionStartedAt *time.Time

	Version int

	AcquisitionFence int

	relabelled bool
}

const maxRejectedSourceKeys = 25

const maxTrackTextLength = 300

func TrackLengthOf(seconds float64) (tracklength.TrackLength, error) {
	length, err := tracklength.TrackLengthOf(seconds)
	if err != nil {
		return tracklength.TrackLength{}, NewValidationError("duration_seconds must be a finite number of seconds from 0 to 7 days")
	}
	return length, nil
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

func NewTrack(userId ids.UserId, title, artist, album string, wallClock clock.Func) (*Track, error) {
	if wallClock == nil {
		panic("domain: NewTrack requires a clock")
	}
	title = strings.TrimSpace(title)
	if err := validateTrackText(title, "title"); err != nil {
		return nil, err
	}
	artist = canonicalizeField(artist)
	if err := validateTrackText(artist, "artist"); err != nil {
		return nil, err
	}
	resolved := resolveAlbum(album, title)
	if err := validateTrackText(resolved, "album"); err != nil {
		return nil, err
	}
	now := wallClock.UTC()
	return &Track{
		ID:                   NewTrackId(),
		UserId:               userId,
		Title:                title,
		Artist:               artist,
		Album:                resolved,
		AddedAt:              now,
		DownloadStatus:       AcquisitionPending,
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

type AcquisitionProvenance = downloadprovenance.AcquisitionProvenance

const (
	ProvenanceVerified     = downloadprovenance.ProvenanceVerified
	ProvenanceCorroborated = downloadprovenance.ProvenanceCorroborated
	ProvenanceBestEffort   = downloadprovenance.ProvenanceBestEffort
)

var ErrIllegalAcquisitionTransition = errors.New("illegal acquisition status transition")

func illegalTransition(op string, from DownloadStatus) error {
	return fmt.Errorf("%w: %s from %s", ErrIllegalAcquisitionTransition, op, from)
}

func (t *Track) MarkReady(audioRef string) error {
	if audioRef == "" {
		return errors.New("audio_ref required for ready status")
	}
	if t.DownloadStatus == AcquisitionReady && t.AudioRef != nil && *t.AudioRef != audioRef {
		return illegalTransition("mark ready", t.DownloadStatus)
	}
	t.setReadyAudio(audioRef)
	return nil
}

func (t *Track) ReplaceAudio(audioRef string) error {
	if audioRef == "" {
		return errors.New("audio_ref required for ready status")
	}
	if !t.IsStreamable() {
		return illegalTransition("replace audio", t.DownloadStatus)
	}
	t.setReadyAudio(audioRef)
	return nil
}

func (t *Track) setReadyAudio(audioRef string) {
	t.DownloadStatus = AcquisitionReady
	t.AudioRef = &audioRef
	t.AudioVersion = ids.NewEntityId().String()
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

func (t *Track) SetAcquisitionConfidence(confidence float64, evidence json.RawMessage) error {
	if math.IsNaN(confidence) || confidence < 0 || confidence > 1 {
		return fmt.Errorf("acquisition confidence must be within [0, 1], got %v", confidence)
	}
	if len(evidence) > 0 && !json.Valid(evidence) {
		return errors.New("acquisition evidence must be valid JSON")
	}
	t.ConfidenceScore = &confidence
	t.EvidenceJSON = evidence
	return nil
}

func (t *Track) AcquisitionConfidence() float64 {
	if t.ConfidenceScore == nil {
		return 0
	}
	return *t.ConfidenceScore
}

func (t *Track) AcquisitionEvidence() json.RawMessage {
	return t.EvidenceJSON
}

func (t *Track) SetDuration(seconds float64) error {
	length, err := TrackLengthOf(seconds)
	if err != nil {
		return err
	}
	if length.Known() {
		t.DurationSeconds = &seconds
	}
	return nil
}

func (t *Track) SetCatalogDuration(seconds float64) error {
	length, err := TrackLengthOf(seconds)
	if err != nil {
		return err
	}
	if length.Known() {
		t.CatalogDurationSeconds = &seconds
		t.DurationSeconds = &seconds
	}
	return nil
}

func (t *Track) SetCatalogIdentity(mbid *string, explicit *bool, sources []CatalogSource) error {
	if mbid != nil {
		normalized, err := NormalizeCatalogMBID(*mbid)
		if err != nil {
			return err
		}
		t.CatalogMBID = &normalized
	}
	unique, err := NormalizeCatalogSources(sources)
	if err != nil {
		return err
	}
	t.CatalogExplicit = explicit
	t.CatalogSources = unique
	return nil
}

func (t *Track) RecordAudioLength(measured tracklength.TrackLength) bool {
	catalog := t.catalogLength()
	if !catalog.Known() || !measured.Known() {
		return false
	}
	t.AudioLengthMismatch = !catalog.SameRecording(measured)
	return true
}

func (t *Track) catalogLength() tracklength.TrackLength {
	if t.CatalogDurationSeconds == nil {
		return tracklength.TrackLength{}
	}
	length, err := tracklength.TrackLengthOf(*t.CatalogDurationSeconds)
	if err != nil {
		return tracklength.TrackLength{}
	}
	return length
}

func (t *Track) AdoptISRC(isrc string) {
	if isrc == "" || (t.ISRC != nil && *t.ISRC != "") {
		return
	}
	t.ISRC = &isrc
}

type recordingLabel struct {
	mbid     string
	title    string
	album    string
	duration float64
}

func validateRecordingLabel(mbid, title, album string, durationSeconds float64) (recordingLabel, error) {
	normalizedMBID, err := NormalizeCatalogMBID(mbid)
	if err != nil {
		return recordingLabel{}, err
	}
	title = strings.TrimSpace(title)
	if err := validateTrackText(title, "title"); err != nil {
		return recordingLabel{}, err
	}
	resolved := resolveAlbum(album, title)
	if err := validateTrackText(resolved, "album"); err != nil {
		return recordingLabel{}, err
	}
	if _, err := TrackLengthOf(durationSeconds); err != nil {
		return recordingLabel{}, err
	}
	return recordingLabel{mbid: normalizedMBID, title: title, album: resolved, duration: durationSeconds}, nil
}

func (t *Track) RelabelAsRecording(mbid, title, album string, year int, isrc string, durationSeconds float64) error {
	label, err := validateRecordingLabel(mbid, title, album, durationSeconds)
	if err != nil {
		return err
	}
	t.Title = label.title
	t.Album = label.album
	t.DedupKey = computeDedupKey(label.title, t.Artist, label.album)
	t.CatalogMBID = &label.mbid
	t.Year, t.ISRC, t.CatalogDurationSeconds = nil, nil, nil
	t.setRelabelOptionals(year, isrc, durationSeconds)
	t.relabelled = true
	return nil
}

func (t *Track) setRelabelOptionals(year int, isrc string, durationSeconds float64) {
	if year != 0 {
		t.Year = &year
	}
	if isrc != "" {
		t.ISRC = &isrc
	}
	if durationSeconds > 0 {
		t.CatalogDurationSeconds = &durationSeconds
	}
}

func (t *Track) Relabelled() bool {
	return t.relabelled
}

func (t *Track) MarkFailed(reason string) error {
	if reason == "" {
		return errors.New("failure_reason required for failed status")
	}
	if t.DownloadStatus == AcquisitionFailed {
		return illegalTransition("mark failed", t.DownloadStatus)
	}
	t.DownloadStatus = AcquisitionFailed
	t.FailureReason = &reason
	t.AudioRef = nil
	t.AcquisitionStartedAt = nil
	return nil
}

func (t *Track) FailAcquisition(reason string) error {
	if t.DownloadStatus != AcquisitionPending {
		return illegalTransition("fail acquisition", t.DownloadStatus)
	}
	return t.MarkFailed(reason)
}

func (t *Track) RevertToPending(wallClock clock.Func) error {
	if wallClock == nil {
		panic("domain: RevertToPending requires a clock")
	}
	if t.DownloadStatus == AcquisitionPending {
		return illegalTransition("revert to pending", t.DownloadStatus)
	}
	now := wallClock.UTC()
	t.DownloadStatus = AcquisitionPending
	t.AudioRef = nil
	t.FailureReason = nil
	t.AcquisitionStartedAt = &now
	return nil
}

func (t *Track) IsStreamable() bool {
	return t.DownloadStatus == AcquisitionReady && t.AudioRef != nil
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
