package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type field string

const (
	fieldYear        field = "year"
	fieldAlbumArtist field = "album_artist"
	fieldISRC        field = "isrc"
)

var columnOf = map[field]string{
	fieldYear:        "year",
	fieldAlbumArtist: "album_artist",
	fieldISRC:        "isrc",
}

func parseField(raw string) (field, error) {
	f := field(raw)
	if _, ok := columnOf[f]; !ok {
		return "", fmt.Errorf("unknown field %q", raw)
	}
	return f, nil
}

type plannedChange struct {
	Field       field
	AuditedOld  *string
	NewValue    *string
	TrackID     string
	UserID      string
	SeenVersion int
}

func (c plannedChange) clearsWrongISRC() bool {
	return c.Field == fieldISRC
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
)

func isBlank(f field, value *string) bool {
	if value == nil {
		return true
	}
	trimmed := strings.TrimSpace(*value)
	return trimmed == "" || (f == fieldYear && trimmed == "0")
}

func decide(change plannedChange, current *string) verdict {
	if change.clearsWrongISRC() {
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
	UserID   string  `json:"user_id"`
	Version  int     `json:"version"`
	Field    string  `json:"field"`
	OldValue *string `json:"old_value"`
	NewValue *string `json:"new_value"`
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
	return plans.tracks, nil
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
	if err := checkValues(f, fix, currentYear); err != nil {
		return plannedChange{}, fmt.Errorf("track %s: %w", finding.TrackID, err)
	}
	return plannedChange{
		Field: f, AuditedOld: fix.OldValue, NewValue: fix.NewValue,
		TrackID: finding.TrackID, UserID: fix.UserID, SeenVersion: fix.Version,
	}, nil
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
		return checkISRCClear(fix)
	}
	return nil
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

func checkISRCClear(fix *reportFix) error {
	if fix.NewValue != nil {
		return errors.New("isrc fix may only clear, new_value must be null")
	}
	if fix.OldValue == nil || len(strings.ReplaceAll(*fix.OldValue, "-", "")) != isrcLength {
		return errors.New("isrc clear needs the 12 character old_value the audit saw")
	}
	return nil
}
