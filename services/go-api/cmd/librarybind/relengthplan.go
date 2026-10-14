package main

import (
	"encoding/json"
	"fmt"
	"os"
)

const actionFixISRC = "fix_isrc"

type relengthFile struct {
	Aborted  bool              `json:"aborted"`
	Findings []relengthFinding `json:"findings"`
}

type relengthFinding struct {
	TrackID    string        `json:"track_id"`
	Artist     string        `json:"artist"`
	Title      string        `json:"title"`
	LengthPlan relengthEntry `json:"length_plan"`
}

type relengthEntry struct {
	Action         string  `json:"action"`
	UserID         string  `json:"user_id"`
	Version        int     `json:"version"`
	OldISRC        string  `json:"old_isrc"`
	NewISRC        string  `json:"new_isrc"`
	FileSeconds    float64 `json:"file_seconds"`
	CatalogSeconds float64 `json:"catalog_seconds"`
}

func relengthPlan(decisions []decided, aborted bool) relengthFile {
	plan := relengthFile{Aborted: aborted, Findings: []relengthFinding{}}
	for _, d := range decisions {
		replaced := d.Found.Evidence.Replaced
		if d.Found.Status != statusBound || replaced == nil || !replaced.Independent || !isRepairableRefusal(replaced.Refusal) {
			continue
		}
		plan.Findings = append(plan.Findings, relengthFinding{
			TrackID: d.Track.ID, Artist: d.Track.Artist, Title: d.Track.Title,
			LengthPlan: relengthEntry{
				Action: actionFixISRC, UserID: d.Track.UserID, Version: d.Track.Version,
				OldISRC: replaced.ISRC, NewISRC: d.Found.ISRC,
				FileSeconds: d.Track.FileSeconds, CatalogSeconds: replaced.CatalogSeconds,
			},
		})
	}
	return plan
}

func writeRelengthPlan(path string, plan relengthFile) error {
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode relength plan: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write relength plan: %w", err)
	}
	return nil
}
