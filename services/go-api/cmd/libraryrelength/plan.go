package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

const fitToleranceSeconds = 10.0

type action string

const (
	actionFixISRC   action = "fix_isrc"
	actionReacquire action = "reacquire"
)

type change struct {
	TrackID        string
	UserID         string
	Artist         string
	Title          string
	Version        int
	Action         action
	OldISRC        string
	NewISRC        string
	FileSeconds    float64
	CatalogSeconds float64
}

var errReportAborted = errors.New("report is from an aborted audit run and is partial")

type reportFile struct {
	Aborted  bool            `json:"aborted"`
	Findings []reportFinding `json:"findings"`
}

type reportFinding struct {
	TrackID    string      `json:"track_id"`
	Artist     string      `json:"artist"`
	Title      string      `json:"title"`
	LengthPlan *reportPlan `json:"length_plan"`
}

type reportPlan struct {
	Action         string  `json:"action"`
	UserID         string  `json:"user_id"`
	Version        int     `json:"version"`
	OldISRC        string  `json:"old_isrc"`
	NewISRC        string  `json:"new_isrc"`
	FileSeconds    float64 `json:"file_seconds"`
	CatalogSeconds float64 `json:"catalog_seconds"`
}

func readPlan(r io.Reader) ([]change, error) {
	file, err := readReport(r)
	if err != nil {
		return nil, err
	}
	return file.changes()
}

func readApplyPlan(r io.Reader) ([]change, error) {
	file, err := readReport(r)
	if err != nil {
		return nil, err
	}
	if file.Aborted {
		return nil, errReportAborted
	}
	return file.changes()
}

func readReport(r io.Reader) (reportFile, error) {
	var file reportFile
	if err := json.NewDecoder(r).Decode(&file); err != nil {
		return reportFile{}, fmt.Errorf("read report: %w", err)
	}
	return file, nil
}

func (file reportFile) changes() ([]change, error) {
	var changes []change
	seen := map[string]bool{}
	for _, finding := range file.Findings {
		if finding.LengthPlan == nil || seen[finding.TrackID] {
			continue
		}
		c, err := toChange(finding)
		if err != nil {
			return nil, fmt.Errorf("track %q: %w", finding.TrackID, err)
		}
		seen[finding.TrackID] = true
		changes = append(changes, c)
	}
	return changes, nil
}

func toChange(finding reportFinding) (change, error) {
	p := finding.LengthPlan
	c := change{
		TrackID: finding.TrackID, UserID: p.UserID, Artist: finding.Artist, Title: finding.Title,
		Version: p.Version, Action: action(p.Action), OldISRC: p.OldISRC, NewISRC: p.NewISRC,
		FileSeconds: p.FileSeconds, CatalogSeconds: p.CatalogSeconds,
	}
	if err := validate(c); err != nil {
		return change{}, err
	}
	return c, nil
}

func validate(c change) error {
	switch {
	case c.TrackID == "" || c.UserID == "":
		return errors.New("plan without track_id or user_id")
	case strings.TrimSpace(c.OldISRC) == "":
		return errors.New("plan without the old_isrc the audit saw")
	case c.CatalogSeconds <= 0 || c.FileSeconds <= 0:
		return errors.New("plan without file_seconds and catalog_seconds")
	}
	switch c.Action {
	case actionFixISRC:
		return validateFix(c)
	case actionReacquire:
		return validateReacquire(c)
	}
	return fmt.Errorf("unknown action %q", c.Action)
}

func validateFix(c change) error {
	switch {
	case strings.TrimSpace(c.NewISRC) == "" || strings.EqualFold(c.NewISRC, c.OldISRC):
		return errors.New("fix_isrc needs a new_isrc different from old_isrc")
	case math.Abs(c.FileSeconds-c.CatalogSeconds) > fitToleranceSeconds:
		return errors.New("fix_isrc whose catalog length does not fit the file")
	}
	return nil
}

func validateReacquire(c change) error {
	if c.NewISRC != "" {
		return errors.New("reacquire must not carry a new_isrc")
	}
	if math.Abs(c.FileSeconds-c.CatalogSeconds) <= fitToleranceSeconds {
		return errors.New("reacquire of a file that already fits the catalog")
	}
	return nil
}
