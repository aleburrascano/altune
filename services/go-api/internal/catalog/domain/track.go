package domain

import (
	"altune/go-api/internal/shared"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
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
	// IdempotencyKey is an optional client-supplied token, stable across retries
	// of one logical save. When present it lets two concurrent creates — or a
	// retry after a dropped response — collapse to a single row, independent of
	// the content-derived DedupKey. Nil means the client sent no key.
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

	// AcquisitionStartedAt is the durable "in-flight since T" marker. It is set
	// while an acquisition is believed to be running (status pending) and cleared
	// once the track reaches a terminal state (ready or failed). A pending track
	// whose marker is older than the grace window is presumed orphaned by a
	// process that died mid-flight, and is swept to failed so retry can pick it up.
	AcquisitionStartedAt *time.Time

	// Version is the monotonic row version behind the optimistic-lock CAS on
	// writes (#1419). A read carries the version it observed; the matching write
	// is scoped to that version and bumps it, so two writers that read the same
	// version cannot both land — the loser gets ports.ErrTrackVersionConflict
	// rather than silently overwriting the winner. A freshly built track is at
	// version 0; the persistence adapter advances it on every successful Update.
	Version int
}

const maxRejectedSourceKeys = 25

const maxTrackTextLength = 300

// MaxDurationSeconds caps a track's duration at one week. Beyond keeping the
// value finite, the cap keeps any sum of durations (a playlist's total) finite:
// encoding/json cannot marshal +Inf, so an unbounded duration would silently
// truncate every response embedding the total.
const MaxDurationSeconds = 7 * 24 * 60 * 60

// ValidateDurationSeconds refuses a duration that is non-finite, negative, or
// above MaxDurationSeconds.
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

// trackTextTooLongError reports a track field longer than maxTrackTextLength,
// deriving the stated limit from the constant so the message cannot drift.
func trackTextTooLongError(field string) error {
	return NewValidationError(fmt.Sprintf("track %s exceeds %d characters", field, maxTrackTextLength))
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
	if len(value) > maxTrackTextLength {
		return trackTextTooLongError(field)
	}
	return nil
}

// ValidateOptionalTrackText applies the same length cap used for title/artist to
// an optional free-form field. A nil pointer means the field is absent and is
// left untouched; a present value must not exceed maxTrackTextLength.
func ValidateOptionalTrackText(value *string, field string) error {
	if value == nil {
		return nil
	}
	if len(*value) > maxTrackTextLength {
		return trackTextTooLongError(field)
	}
	return nil
}

// ValidateSourceURL rejects an acquisition source URL that is oversized, not a
// well-formed http(s) URL, or aimed at a non-public host (see
// validateSourceHost), before it is handed to the acquisition scheduler. An
// empty value is allowed: it signals that no source was supplied.
func ValidateSourceURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) > maxTrackTextLength {
		return trackTextTooLongError("source_url")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return NewValidationError("track source_url is malformed")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return NewValidationError("track source_url must be an http or https URL")
	}
	if parsed.Hostname() == "" {
		return NewValidationError("track source_url must include a host")
	}
	return validateSourceHost(parsed.Hostname())
}

// blockedSourceHostnames are names that always resolve to the server itself or
// to a cloud instance-metadata service.
var blockedSourceHostnames = map[string]bool{
	"localhost":                true,
	"metadata":                 true,
	"metadata.google.internal": true,
	"instance-data":            true,
}

// blockedSourcePrefixes are IP ranges that netip's IsGlobalUnicast/IsPrivate do
// not exclude but that are not publicly routable, or that tunnel to an
// embedded IPv4 address (NAT64, 6to4, IPv4-compatible) which may be internal.
var blockedSourcePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),      // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),  // RFC 6598 carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),   // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"),  // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),    // reserved, broadcast
	netip.MustParsePrefix("::/96"),          // IPv4-compatible IPv6
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64 well-known prefix
	netip.MustParsePrefix("64:ff9b:1::/48"), // NAT64 local-use prefix
	netip.MustParsePrefix("2002::/16"),      // 6to4
}

// validateSourceHost rejects a source host the server must never fetch on a
// caller's behalf (SSRF / confused deputy): loopback, private, link-local
// (including the 169.254.169.254 metadata endpoint), unspecified, multicast and
// reserved IP literals; localhost and metadata hostnames; and numeric IPv4
// shorthands such as "2130706433" or "0x7f.1" that inet_aton-style resolvers
// expand to internal addresses. It is a syntactic check only: a public name
// that resolves to an internal address must be caught at fetch time.
func validateSourceHost(host string) error {
	host = canonicalSourceHost(host)
	if addr, err := netip.ParseAddr(host); err == nil {
		return validateSourceAddr(addr)
	}
	if isBlockedSourceHostname(host) {
		return errNonPublicSourceHost()
	}
	return nil
}

// canonicalSourceHost folds a host the way an IDNA-aware fetcher would before
// resolving it: NFKC (fullwidth "１２７" becomes "127"), the ideographic full
// stop as a label separator, lower case, and no trailing root dot.
func canonicalSourceHost(host string) string {
	host = norm.NFKC.String(host)
	host = strings.ReplaceAll(host, "。", ".")
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func validateSourceAddr(addr netip.Addr) error {
	addr = addr.Unmap()
	if addr.Zone() != "" || !addr.IsGlobalUnicast() || addr.IsPrivate() || inBlockedSourcePrefix(addr) {
		return errNonPublicSourceHost()
	}
	return nil
}

func inBlockedSourcePrefix(addr netip.Addr) bool {
	for _, prefix := range blockedSourcePrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func isBlockedSourceHostname(host string) bool {
	if host == "" || blockedSourceHostnames[host] || strings.HasSuffix(host, ".localhost") {
		return true
	}
	return isNumericLabel(host[strings.LastIndex(host, ".")+1:])
}

// isNumericLabel reports whether a final host label is decimal or 0x-hex. No
// real TLD is numeric, so such a host is an IPv4 shorthand that netip refuses
// to parse but system resolvers accept.
func isNumericLabel(label string) bool {
	if hex, ok := strings.CutPrefix(label, "0x"); ok {
		return strings.Trim(hex, "0123456789abcdef") == ""
	}
	return label != "" && strings.Trim(label, "0123456789") == ""
}

func errNonPublicSourceHost() error {
	return NewValidationError("track source_url must target a public host")
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

// SetAlbumArtist stores the album-artist in the same canonical form as album and
// artist so the library-lens grouping coalesces values that differ only by stray
// whitespace or Unicode form. A value that is empty after canonicalization clears
// the field (it falls back to the track artist in the grouping query).
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

// ErrIllegalAcquisitionTransition reports an acquisition-status change the
// track's current state does not allow, such as a duplicate or out-of-order
// acquisition callback. The track is left unchanged.
var ErrIllegalAcquisitionTransition = errors.New("illegal acquisition status transition")

func illegalTransition(op string, from AcquisitionStatus) error {
	return fmt.Errorf("%w: %s from %s", ErrIllegalAcquisitionTransition, op, from)
}

// MarkReady records a completed acquisition. It is allowed from pending, and
// from failed (a late success after the track was swept or failed stays
// usable). On a ready track it is allowed only for the same audio_ref: a
// duplicate completion rewrote that object, so only the version moves. A
// completion under a different ref is refused, since swapping audio is
// ReplaceAudio's job and taking the new ref would orphan the served object.
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

// ReplaceAudio swaps the audio of a track that is already ready with audio.
// Any other state is refused: there is no audio to replace.
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

// SetDuration records a positive duration; zero leaves it unknown. A value
// ValidateDurationSeconds refuses is returned as an error and not stored.
func (t *Track) SetDuration(seconds float64) error {
	if err := ValidateDurationSeconds(seconds); err != nil {
		return err
	}
	if seconds > 0 {
		t.DurationSeconds = &seconds
	}
	return nil
}

// MarkFailed fails a pending track, or a ready track whose audio went missing.
// A track that is already failed is refused, so a duplicate failure cannot
// overwrite the reason recorded by the first.
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

// FailAcquisition records that an acquisition attempt failed. Only a pending
// track has an attempt in flight; once it is ready or failed another path has
// settled it, so a stale failure callback is refused rather than clobbering
// good audio.
func (t *Track) FailAcquisition(reason string) error {
	if t.AcquisitionStatus != AcquisitionPending {
		return illegalTransition("fail acquisition", t.AcquisitionStatus)
	}
	return t.MarkFailed(reason)
}

// RevertToPending readies a ready or failed track for a new acquisition. A
// pending track is refused: refreshing its in-flight marker would hide an
// orphaned acquisition from the stale-pending sweep.
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

// ReasonAcquisitionInterrupted marks a track whose acquisition job was lost
// before completing (the process died mid-flight) and was swept from a stale
// pending state to failed so the existing retry path can reclaim it.
const ReasonAcquisitionInterrupted = "acquisition_interrupted"

// ReasonAcquisitionRefused marks a track whose acquisition job was never
// queued (the scheduler shed it under load or was shutting down), so it is
// failed immediately and the retry path can reclaim it.
const ReasonAcquisitionRefused = "acquisition_refused"

// FailureCode is the stable, machine-readable prefix of a track's
// failure_reason. The acquisition side emits these codes; FailureMessage
// derives the user-facing failure_message from them. A persisted reason may
// carry a human-readable detail after the code, separated by
// FailureDetailSeparator.
type FailureCode string

const (
	FailureNoMatchFound           FailureCode = "no_match_found"
	FailureDownloadFailed         FailureCode = "download_failed"
	FailureStorageFailed          FailureCode = "storage_failed"
	FailureAcquisitionCancelled   FailureCode = "acquisition_cancelled"
	FailureAcquisitionFailed      FailureCode = "acquisition_failed"
	FailureYtdlpError             FailureCode = "ytdlp_error"
	FailureAcquisitionInterrupted FailureCode = ReasonAcquisitionInterrupted
	FailureAcquisitionRefused     FailureCode = ReasonAcquisitionRefused
)

// FailureDetailSeparator splits a failure_reason into its code and an optional
// human-readable detail (e.g. a candidate-rejection summary).
const FailureDetailSeparator = ": "

const genericFailureMessage = "Couldn't get this track"

var failureMessages = map[FailureCode]string{
	FailureNoMatchFound:           "Couldn't find this track",
	FailureDownloadFailed:         "Download failed",
	FailureStorageFailed:          "Couldn't save this track",
	FailureAcquisitionCancelled:   "Acquisition was cancelled",
	FailureAcquisitionFailed:      genericFailureMessage,
	FailureYtdlpError:             "Download error",
	FailureAcquisitionInterrupted: "Acquisition was interrupted",
	FailureAcquisitionRefused:     "Too busy to get this track, try again",
}

// Known reports whether c has an entry in the failure-message table.
func (c FailureCode) Known() bool {
	_, ok := failureMessages[c]
	return ok
}

func FailureMessage(reason *string) string {
	if reason == nil {
		return "Acquisition failed"
	}
	code, _, _ := strings.Cut(*reason, FailureDetailSeparator)
	if msg, ok := failureMessages[FailureCode(code)]; ok {
		return msg
	}
	return genericFailureMessage
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
