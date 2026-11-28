package domain

import (
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/music"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

type TrackId = ids.TrackId

var ParseTrackId = ids.ParseTrackId

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
	id              TrackId
	userId          ids.UserId
	title           string
	artist          string
	album           string
	durationSeconds *float64
	addedAt         time.Time
	artworkURL      *string
	downloadStatus  DownloadStatus
	dedupKey        string
	idempotencyKey  *string
	year            *int
	genre           *string
	trackNumber     *int
	albumArtist     *string
	isrc            *string
	audioRef        *string
	audioVersion    string
	failureReason   *string
	featuredArtists []FeaturedArtist

	acquisitionProvenance *string
	audioSourceURL        *string

	confidenceScore *float64
	evidenceJSON    json.RawMessage

	catalogDurationSeconds *float64
	catalogMBID            *string
	catalogExplicit        *bool
	catalogSources         []CatalogSource
	audioLengthMismatch    bool

	acquisitionStartedAt *time.Time

	version int

	acquisitionFence int

	relabelled bool
}

const maxTrackTextLength = 300

func TrackLengthOf(seconds float64) (music.TrackLength, error) {
	length, err := music.TrackLengthOf(seconds)
	if err != nil {
		return music.TrackLength{}, NewValidationError("duration_seconds must be a finite number of seconds from 0 to 7 days")
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

type NewTrackInput struct {
	UserId          ids.UserId
	Title           string
	Artist          string
	Album           string
	ArtworkURL      *string
	Year            *int
	Genre           *string
	TrackNumber     *int
	ISRC            *string
	FeaturedArtists []FeaturedArtist
	IdempotencyKey  *string
}

func NewTrack(in NewTrackInput, wallClock func() time.Time, source ids.Source) (*Track, error) {
	if wallClock == nil {
		return nil, ErrTrackTimestampClockRequired
	}
	if source == nil {
		return nil, ErrIDSourceRequired
	}
	title := strings.TrimSpace(in.Title)
	if err := validateTrackText(title, "title"); err != nil {
		return nil, err
	}
	artist := canonicalizeField(in.Artist)
	if err := validateTrackText(artist, "artist"); err != nil {
		return nil, err
	}
	resolved := resolveAlbum(in.Album, title)
	if err := validateTrackText(resolved, "album"); err != nil {
		return nil, err
	}
	if in.Year != nil {
		if err := ValidateYear(*in.Year, wallClock()); err != nil {
			return nil, err
		}
	}
	now := wallClock().UTC()
	return &Track{
		id:                   source.NewTrackId(),
		userId:               in.UserId,
		title:                title,
		artist:               artist,
		album:                resolved,
		addedAt:              now,
		downloadStatus:       AcquisitionPending,
		acquisitionStartedAt: &now,
		dedupKey:             computeDedupKey(title, artist, resolved),
		artworkURL:           in.ArtworkURL,
		year:                 in.Year,
		genre:                in.Genre,
		trackNumber:          in.TrackNumber,
		isrc:                 in.ISRC,
		featuredArtists:      in.FeaturedArtists,
		idempotencyKey:       in.IdempotencyKey,
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
	t.album = resolveAlbum(album, t.title)
	t.dedupKey = computeDedupKey(t.title, t.artist, t.album)
}

func (t *Track) SetAlbumArtist(albumArtist string) {
	canonical := canonicalizeField(albumArtist)
	if canonical == "" {
		t.albumArtist = nil
		return
	}
	t.albumArtist = &canonical
}

type AcquisitionProvenance = music.AcquisitionProvenance

const (
	ProvenanceVerified     = music.ProvenanceVerified
	ProvenanceCorroborated = music.ProvenanceCorroborated
	ProvenanceBestEffort   = music.ProvenanceBestEffort
)

func illegalTransition(op string, from DownloadStatus) error {
	return fmt.Errorf("%w: %s from %s", ErrIllegalAcquisitionTransition, op, from)
}

func (t *Track) MarkReady(audioRef string, source ids.Source) error {
	if audioRef == "" {
		return errors.New("audio_ref required for ready status")
	}
	if source == nil {
		return ErrIDSourceRequired
	}
	if t.downloadStatus == AcquisitionReady && t.audioRef != nil && *t.audioRef != audioRef {
		return illegalTransition(EventMarkReady.String(), t.downloadStatus)
	}
	next, err := Transition(t.downloadStatus, EventMarkReady)
	if err != nil {
		return err
	}
	t.setReadyAudio(next, audioRef, source)
	return nil
}

func (t *Track) ReplaceAudio(audioRef string, source ids.Source) error {
	if audioRef == "" {
		return errors.New("audio_ref required for ready status")
	}
	if source == nil {
		return ErrIDSourceRequired
	}
	next, err := Transition(t.downloadStatus, EventReplaceAudio)
	if err != nil {
		return err
	}
	if !t.IsStreamable() {
		return illegalTransition(EventReplaceAudio.String(), t.downloadStatus)
	}
	t.setReadyAudio(next, audioRef, source)
	return nil
}

func (t *Track) setReadyAudio(next DownloadStatus, audioRef string, source ids.Source) {
	t.downloadStatus = next
	t.audioRef = &audioRef
	t.audioVersion = source.NewString()
	t.failureReason = nil
	t.acquisitionStartedAt = nil
}

func (t *Track) SetAudioSource(url string) {
	if url == "" {
		return
	}
	t.audioSourceURL = &url
}

func (t *Track) SetAcquisitionProvenance(p AcquisitionProvenance) {
	if !p.Valid() {
		return
	}
	value := string(p)
	t.acquisitionProvenance = &value
}

func (t *Track) SetAcquisitionConfidence(confidence float64, evidence json.RawMessage) error {
	if math.IsNaN(confidence) || confidence < 0 || confidence > 1 {
		return fmt.Errorf("acquisition confidence must be within [0, 1], got %v", confidence)
	}
	if len(evidence) > 0 && !json.Valid(evidence) {
		return errors.New("acquisition evidence must be valid JSON")
	}
	t.confidenceScore = &confidence
	t.evidenceJSON = evidence
	return nil
}

func (t *Track) AcquisitionConfidence() float64 {
	if t.confidenceScore == nil {
		return 0
	}
	return *t.confidenceScore
}

func (t *Track) AcquisitionEvidence() json.RawMessage {
	return slices.Clone(t.evidenceJSON)
}

func (t *Track) SetDuration(seconds float64) error {
	length, err := TrackLengthOf(seconds)
	if err != nil {
		return err
	}
	if length.Known() {
		t.durationSeconds = &seconds
	}
	return nil
}

func (t *Track) SetCatalogDuration(seconds float64) error {
	length, err := TrackLengthOf(seconds)
	if err != nil {
		return err
	}
	if length.Known() {
		t.catalogDurationSeconds = &seconds
		t.durationSeconds = &seconds
	}
	return nil
}

func (t *Track) setCatalogMBID(mbid *string) error {
	if mbid == nil {
		return nil
	}
	normalized, err := NormalizeCatalogMBID(*mbid)
	if err != nil {
		return err
	}
	t.catalogMBID = &normalized
	return nil
}

func (t *Track) SetCatalogIdentity(mbid *string, explicit *bool, sources []CatalogSource) error {
	if err := t.setCatalogMBID(mbid); err != nil {
		return err
	}
	unique, err := NormalizeCatalogSources(sources)
	if err != nil {
		return err
	}
	t.catalogExplicit = explicit
	t.catalogSources = unique
	return nil
}

func (t *Track) RecordAudioLength(measured music.TrackLength) bool {
	catalog := t.catalogLength()
	if !catalog.Known() || !measured.Known() {
		return false
	}
	t.audioLengthMismatch = !catalog.SameRecording(measured)
	return true
}

func (t *Track) catalogLength() music.TrackLength {
	if t.catalogDurationSeconds == nil {
		return music.TrackLength{}
	}
	length, err := music.TrackLengthOf(*t.catalogDurationSeconds)
	if err != nil {
		return music.TrackLength{}
	}
	return length
}

func (t *Track) AdoptISRC(isrc string) {
	if isrc == "" || (t.isrc != nil && *t.isrc != "") {
		return
	}
	t.isrc = &isrc
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

func (t *Track) RelabelAsRecording(mbid, title, album string, year int, isrc string, durationSeconds float64, wallClock func() time.Time) error {
	if wallClock == nil {
		return ErrTrackTimestampClockRequired
	}
	label, err := validateRecordingLabel(mbid, title, album, durationSeconds)
	if err != nil {
		return err
	}
	if year != 0 {
		if err := ValidateYear(year, wallClock()); err != nil {
			return err
		}
	}
	t.title = label.title
	t.album = label.album
	t.dedupKey = computeDedupKey(label.title, t.artist, label.album)
	t.catalogMBID = &label.mbid
	t.year, t.isrc, t.catalogDurationSeconds = nil, nil, nil
	t.setRelabelOptionals(year, isrc, durationSeconds)
	t.relabelled = true
	return nil
}

const minPlausibleYear = 1860

func PlausibleYear(year int, now time.Time) bool {
	return year >= minPlausibleYear && year <= now.UTC().Year()+1
}

func ValidateYear(year int, now time.Time) error {
	if !PlausibleYear(year, now) {
		return NewValidationError("year is implausible")
	}
	return nil
}

func (t *Track) setRelabelOptionals(year int, isrc string, durationSeconds float64) {
	if year != 0 {
		t.year = &year
	}
	if isrc != "" {
		t.isrc = &isrc
	}
	if durationSeconds > 0 {
		t.catalogDurationSeconds = &durationSeconds
	}
}

func (t *Track) Relabelled() bool {
	return t.relabelled
}

func (t *Track) MarkFailed(reason string) error {
	if reason == "" {
		return errors.New("failure_reason required for failed status")
	}
	next, err := Transition(t.downloadStatus, EventMarkFailed)
	if err != nil {
		return err
	}
	t.downloadStatus = next
	t.failureReason = &reason
	t.audioRef = nil
	t.acquisitionStartedAt = nil
	return nil
}

func (t *Track) FailAcquisition(reason string) error {
	if _, err := Transition(t.downloadStatus, EventFailPending); err != nil {
		return err
	}
	return t.MarkFailed(reason)
}

func (t *Track) RevertToPending(wallClock func() time.Time) error {
	if wallClock == nil {
		return ErrTrackTimestampClockRequired
	}
	next, err := Transition(t.downloadStatus, EventRevertToPending)
	if err != nil {
		return err
	}
	now := wallClock().UTC()
	t.downloadStatus = next
	t.audioRef = nil
	t.failureReason = nil
	t.acquisitionStartedAt = &now
	return nil
}

func (t *Track) IsStreamable() bool {
	return t.downloadStatus == AcquisitionReady && t.audioRef != nil
}

func TotalDurationSeconds(tracks []*Track) float64 {
	total := 0.0
	for _, t := range tracks {
		if t.durationSeconds != nil {
			total += *t.durationSeconds
		}
	}
	return total
}
