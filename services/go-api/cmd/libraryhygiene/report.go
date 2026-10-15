package main

import (
	"encoding/json"
	"io"
	"time"
)

type report struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Tracks      int            `json:"tracks"`
	Summary     map[string]int `json:"summary"`
	Findings    []finding      `json:"findings"`
}

func buildReport(tracks []unmatchedTrack, now time.Time) report {
	rep := report{GeneratedAt: now.UTC(), Tracks: len(tracks), Summary: map[string]int{}, Findings: []finding{}}
	for _, t := range tracks {
		for _, f := range hygieneFindings(t) {
			rep.Findings = append(rep.Findings, f)
			rep.Summary[f.Category]++
		}
	}
	return rep
}

func writeReport(out io.Writer, rep report) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(rep)
}
