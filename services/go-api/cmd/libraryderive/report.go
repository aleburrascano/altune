package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type category string

const (
	categoryReleaseChosen      category = "release_chosen"
	categoryReleaseUnchosen    category = "release_unchosen"
	categoryNoAnchor           category = "no_anchor"
	categoryRecordingUnknown   category = "recording_unknown"
	categoryRecordingAmbiguous category = "recording_ambiguous"
	categoryLookupFailed       category = "lookup_failed"
	categoryISRCOtherMaster    category = "isrc_other_master"
	categoryOtherMasterUnfit   category = "other_master_unfit"
)

type fix struct {
	UserID   string     `json:"user_id"`
	Version  int        `json:"version"`
	Field    string     `json:"field"`
	Mode     changeMode `json:"mode"`
	OldValue *string    `json:"old_value"`
	NewValue *string    `json:"new_value"`
	Group    string     `json:"group,omitempty"`
}

type lengthPlan struct {
	Action         string  `json:"action"`
	UserID         string  `json:"user_id"`
	Version        int     `json:"version"`
	OldISRC        string  `json:"old_isrc"`
	NewISRC        string  `json:"new_isrc"`
	FileSeconds    float64 `json:"file_seconds"`
	CatalogSeconds float64 `json:"catalog_seconds"`
}

const actionFixISRC = "fix_isrc"

type finding struct {
	TrackID    string      `json:"track_id"`
	Artist     string      `json:"artist"`
	Title      string      `json:"title"`
	Category   category    `json:"category"`
	Detail     string      `json:"detail,omitempty"`
	Source     string      `json:"source,omitempty"`
	Fix        *fix        `json:"fix,omitempty"`
	LengthPlan *lengthPlan `json:"length_plan,omitempty"`
}

type report struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Aborted     bool             `json:"aborted"`
	AbortReason string           `json:"abort_reason,omitempty"`
	Tracks      int              `json:"tracks"`
	Summary     map[category]int `json:"summary"`
	Findings    []finding        `json:"findings"`
}

func about(t boundTrack, kind category, detail string) finding {
	return finding{TrackID: t.ID, Artist: t.Artist, Title: t.Title, Category: kind, Detail: detail}
}

func findingOf(t boundTrack, d difference) finding {
	f := about(t, d.category(), valueText(d.Old)+" -> "+valueText(d.New))
	if d.Mode != modeReportOnly {
		f.Fix = &fix{UserID: t.UserID, Version: t.Version, Field: d.Field, Mode: d.Mode, OldValue: d.Old, NewValue: d.New, Group: d.Group}
	}
	return f
}

func valueText(value *string) string {
	if value == nil {
		return "(empty)"
	}
	return fmt.Sprintf("%q", *value)
}

func buildReport(tracks int, findings []finding, abortReason string, now time.Time) report {
	summary := map[category]int{}
	for _, f := range findings {
		summary[f.Category]++
	}
	return report{
		GeneratedAt: now, Aborted: abortReason != "", AbortReason: abortReason,
		Tracks: tracks, Summary: summary, Findings: findings,
	}
}

func writeReport(out io.Writer, r report) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(r); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
