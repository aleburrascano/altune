package main

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/textnorm"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type field string

const (
	fieldYear        field = "year"
	fieldAlbumArtist field = "album_artist"
	fieldISRC        field = "isrc"

	fieldTrackNumber field = "track_number"
	fieldGenre       field = "genre"
	fieldCatalogLen  field = "catalog_duration_seconds"
	fieldArtworkURL  field = "artwork_url"

	fieldTitle    field = "title"
	fieldArtist   field = "artist"
	fieldAlbum    field = "album"
	fieldDedupKey field = "dedup_key"
)

const identityGroup = "identity"

var columnOf = map[field]string{
	fieldYear:        "year",
	fieldAlbumArtist: "album_artist",
	fieldISRC:        "isrc",
	fieldTrackNumber: "track_number",
	fieldGenre:       "genre",
	fieldCatalogLen:  "catalog_duration_seconds",
	fieldArtworkURL:  "artwork_url",
	fieldTitle:       "title",
	fieldArtist:      "artist",
	fieldAlbum:       "album",
	fieldDedupKey:    "dedup_key",
}

var castOf = map[field]string{
	fieldYear:        "::text::int",
	fieldTrackNumber: "::text::int",
	fieldCatalogLen:  "::text::double precision",
}

func isIdentity(f field) bool {
	return f == fieldTitle || f == fieldArtist || f == fieldAlbum
}

func isNumeric(f field) bool {
	_, numeric := castOf[f]
	return numeric
}

func parseField(raw string) (field, error) {
	f := field(raw)
	if _, ok := columnOf[f]; !ok {
		return "", fmt.Errorf("unknown field %q", raw)
	}
	return f, nil
}

type changeMode string

const (
	modeFill    changeMode = "fill"
	modeCorrect changeMode = "correct"
)

func parseMode(raw string) (changeMode, error) {
	switch changeMode(raw) {
	case "", modeFill:
		return modeFill, nil
	case modeCorrect:
		return modeCorrect, nil
	}
	return "", fmt.Errorf("unknown mode %q", raw)
}

type plannedChange struct {
	Mode        changeMode
	Field       field
	AuditedOld  *string
	NewValue    *string
	TrackID     string
	UserID      string
	SeenVersion int
	Group       string
}

func (c plannedChange) effectiveGroup() string {
	if c.Group == "" && isIdentity(c.Field) {
		return identityGroup
	}
	return c.Group
}

func (c plannedChange) clearsWrongISRC() bool {
	return c.Field == fieldISRC && c.NewValue == nil
}

func (c plannedChange) clearsWrongCatalogLength() bool {
	return c.Field == fieldCatalogLen && c.NewValue == nil
}

func (c plannedChange) replacesAuditedValue() bool {
	return c.Mode == modeCorrect || c.clearsWrongISRC() || c.clearsWrongCatalogLength() || c.Field == fieldArtworkURL
}

type trackPlan struct {
	TrackID string
	UserID  string
	Version int
	Changes []plannedChange
}

type verdict string

const (
	verdictApplied         verdict = "applied"
	verdictSkippedNotEmpty verdict = "skipped-not-empty"
	verdictStale           verdict = "stale"
	verdictFailed          verdict = "failed"

	verdictSkippedGroup     verdict = "skipped-group"
	verdictSkippedDuplicate verdict = "skipped-duplicate"
)

func isBlank(f field, value *string) bool {
	if value == nil {
		return true
	}
	trimmed := strings.TrimSpace(*value)
	return trimmed == "" || (isNumeric(f) && trimmed == "0")
}

func decide(change plannedChange, current *string) verdict {
	if change.replacesAuditedValue() {
		if current != nil && change.AuditedOld != nil && *current == *change.AuditedOld {
			return verdictApplied
		}
		return verdictSkippedNotEmpty
	}
	if isBlank(change.Field, current) {
		return verdictApplied
	}
	return verdictSkippedNotEmpty
}

type reportFile struct {
	Findings []reportFinding `json:"findings"`
}

type reportFinding struct {
	TrackID string     `json:"track_id"`
	Fix     *reportFix `json:"fix"`
}

type reportFix struct {
	UserID   string   `json:"user_id"`
	Version  int      `json:"version"`
	Field    string   `json:"field"`
	Mode     string   `json:"mode"`
	OldValue *string  `json:"old_value"`
	NewValue *string  `json:"new_value"`
	Sources  []string `json:"sources"`
	Group    string   `json:"group"`
}

func readPlan(r io.Reader, currentYear int) ([]trackPlan, error) {
	var file reportFile
	if err := json.NewDecoder(r).Decode(&file); err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}
	var plans planBook
	for _, finding := range file.Findings {
		if finding.Fix == nil {
			continue
		}
		change, err := toChange(finding, currentYear)
		if err != nil {
			return nil, err
		}
		if err := plans.add(change); err != nil {
			return nil, err
		}
	}
	for _, track := range plans.tracks {
		if err := track.checkIdentityGroup(); err != nil {
			return nil, err
		}
	}
	return plans.tracks, nil
}

func (p trackPlan) checkIdentityGroup() error {
	group := ""
	for _, change := range p.Changes {
		if !isIdentity(change.Field) {
			continue
		}
		if group != "" && change.effectiveGroup() != group {
			return fmt.Errorf("track %s: title, artist and album fixes must share one group", p.TrackID)
		}
		group = change.effectiveGroup()
	}
	return nil
}

type planBook struct {
	tracks   []trackPlan
	position map[string]int
}

func (b *planBook) add(change plannedChange) error {
	if b.position == nil {
		b.position = map[string]int{}
	}
	index, seen := b.position[change.TrackID]
	if !seen {
		index = len(b.tracks)
		b.position[change.TrackID] = index
		b.tracks = append(b.tracks, trackPlan{TrackID: change.TrackID, UserID: change.UserID, Version: change.SeenVersion})
	}
	track := &b.tracks[index]
	if track.UserID != change.UserID || track.Version != change.SeenVersion {
		return fmt.Errorf("track %s: findings disagree on user or version", change.TrackID)
	}
	track.addUnique(change)
	return nil
}

func (p *trackPlan) addUnique(change plannedChange) {
	for _, existing := range p.Changes {
		if existing.Field == change.Field {
			return
		}
	}
	p.Changes = append(p.Changes, change)
}

const (
	earliestYear = 1900
	isrcLength   = 12

	maxTrackNumber    = 999
	maxCatalogSeconds = 24 * 60 * 60

	sourceDeezer      = "deezer"
	sourceMusicBrainz = "musicbrainz"
)

func toChange(finding reportFinding, currentYear int) (plannedChange, error) {
	fix := finding.Fix
	f, err := parseField(fix.Field)
	if err != nil {
		return plannedChange{}, fmt.Errorf("track %s: %w", finding.TrackID, err)
	}
	if finding.TrackID == "" || fix.UserID == "" {
		return plannedChange{}, fmt.Errorf("track %q: fix without track_id or user_id", finding.TrackID)
	}
	if f == fieldDedupKey {
		return plannedChange{}, fmt.Errorf("track %s: the dedup key is recomputed, never planned", finding.TrackID)
	}
	mode, err := parseMode(fix.Mode)
	if err != nil {
		return plannedChange{}, fmt.Errorf("track %s: %w", finding.TrackID, err)
	}
	if err := checkChange(mode, f, fix, currentYear); err != nil {
		return plannedChange{}, fmt.Errorf("track %s: %w", finding.TrackID, err)
	}
	return plannedChange{
		Mode: mode, Field: f, AuditedOld: fix.OldValue, NewValue: fix.NewValue,
		TrackID: finding.TrackID, UserID: fix.UserID, SeenVersion: fix.Version, Group: fix.Group,
	}, nil
}

func checkChange(mode changeMode, f field, fix *reportFix, currentYear int) error {
	if mode != modeCorrect {
		if isIdentity(f) {
			return fmt.Errorf("a %s fix must be correct-mode", f)
		}
		return checkValues(f, fix, currentYear)
	}
	if f == fieldISRC {
		return errors.New("an isrc correction goes through libraryrelength, not a correct-mode fix")
	}
	if fix.OldValue == nil || isBlank(f, fix.OldValue) {
		return errors.New("a correct-mode fix needs the old_value the derive saw")
	}
	if fix.NewValue == nil {
		return errors.New("a correct-mode fix needs a new_value")
	}
	return checkValues(f, fix, currentYear)
}

func checkValues(f field, fix *reportFix, currentYear int) error {
	switch f {
	case fieldYear:
		return checkYear(fix.NewValue, currentYear)
	case fieldAlbumArtist:
		if fix.NewValue == nil || strings.TrimSpace(*fix.NewValue) == "" {
			return errors.New("album_artist fix has a blank new_value")
		}
	case fieldISRC:
		return checkISRC(fix)
	case fieldTrackNumber:
		return checkTrackNumber(fix.NewValue)
	case fieldGenre:
		if fix.NewValue == nil || strings.TrimSpace(*fix.NewValue) == "" {
			return errors.New("genre fix has a blank new_value")
		}
	case fieldCatalogLen:
		if fix.NewValue == nil {
			return checkCatalogLengthClear(fix.OldValue)
		}
		return checkCatalogLength(fix.NewValue)
	case fieldArtworkURL:
		return checkArtworkReplacement(fix)
	case fieldTitle, fieldArtist, fieldAlbum:
		return checkIdentityText(f, fix.NewValue)
	}
	return nil
}

func checkIdentityText(f field, value *string) error {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fmt.Errorf("%s fix has a blank new_value", f)
	}
	if canonical := strings.Join(strings.Fields(textnorm.NFKC(*value)), " "); canonical != *value {
		return fmt.Errorf("%s fix new_value %q is not canonical, want %q", f, *value, canonical)
	}
	return domain.ValidateOptionalTrackText(value, string(f))
}

func checkTrackNumber(value *string) error {
	if value == nil {
		return errors.New("track_number fix has no new_value")
	}
	number, err := strconv.Atoi(*value)
	if err != nil || strconv.Itoa(number) != *value || number < 1 || number > maxTrackNumber {
		return fmt.Errorf("track number %q is outside 1 to %d", *value, maxTrackNumber)
	}
	return nil
}

func checkCatalogLength(value *string) error {
	if value == nil {
		return errors.New("catalog_duration_seconds fix has no new_value")
	}
	seconds, err := strconv.ParseFloat(*value, 64)
	if err != nil || math.IsNaN(seconds) || seconds <= 0 || seconds > maxCatalogSeconds {
		return fmt.Errorf("catalog length %q is outside 0 to %d seconds", *value, maxCatalogSeconds)
	}
	return nil
}

func checkCatalogLengthClear(old *string) error {
	if old == nil {
		return errors.New("catalog length clear needs the old_value the audit saw")
	}
	return checkCatalogLength(old)
}

func checkYear(value *string, currentYear int) error {
	if value == nil {
		return errors.New("year fix has no new_value")
	}
	year, err := strconv.Atoi(*value)
	if err != nil || strconv.Itoa(year) != *value || year < earliestYear || year > currentYear+1 {
		return fmt.Errorf("year %q is outside %d to %d", *value, earliestYear, currentYear+1)
	}
	return nil
}

func checkISRC(fix *reportFix) error {
	if fix.NewValue == nil {
		return checkISRCClear(fix)
	}
	return checkISRCFill(fix)
}

func checkISRCClear(fix *reportFix) error {
	if fix.OldValue == nil || len(strings.ReplaceAll(*fix.OldValue, "-", "")) != isrcLength {
		return errors.New("isrc clear needs the 12 character old_value the audit saw")
	}
	return nil
}

func checkISRCFill(fix *reportFix) error {
	if fix.OldValue != nil {
		return errors.New("isrc fill needs a null old_value")
	}
	if !isCanonicalISRC(*fix.NewValue) {
		return fmt.Errorf("isrc fill value %q is not 12 upper-case letters and digits", *fix.NewValue)
	}
	if !slices.Contains(fix.Sources, sourceDeezer) || !slices.Contains(fix.Sources, sourceMusicBrainz) {
		return errors.New("isrc fill needs both deezer and musicbrainz in sources")
	}
	return nil
}

func isCanonicalISRC(value string) bool {
	if len(value) != isrcLength {
		return false
	}
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func checkArtworkReplacement(fix *reportFix) error {
	if fix.OldValue == nil || strings.TrimSpace(*fix.OldValue) == "" {
		return errors.New("artwork_url fix needs the old_value the audit saw")
	}
	if fix.NewValue == nil {
		return errors.New("artwork_url fix has no new_value")
	}
	parsed, err := url.Parse(*fix.NewValue)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("artwork_url %q is not an https URL", *fix.NewValue)
	}
	return nil
}
