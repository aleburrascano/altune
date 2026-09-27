package eval

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

//go:embed goldens/*.json
var goldenFS embed.FS

type Track struct {
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	ISRC     string  `json:"isrc,omitempty"`

	AuthoritativeDuration float64  `json:"authoritative_duration,omitempty"`
	MBID                  string   `json:"mbid,omitempty"`
	AcoustIDs             []string `json:"acoustids,omitempty"`

	Resolution *Resolution `json:"resolution,omitempty"`
}

type Resolution struct {
	Search         *SearchedRecording `json:"search,omitempty"`
	ISRCRecordings []ISRCRecording    `json:"isrc_recordings,omitempty"`
}

type SearchedRecording struct {
	MBID     string `json:"mbid,omitempty"`
	ISRC     string `json:"isrc,omitempty"`
	Duration int    `json:"duration,omitempty"`
}

type ISRCRecording struct {
	MBID     string `json:"mbid"`
	Duration int    `json:"duration,omitempty"`
}

type Candidate struct {
	Title          string   `json:"title"`
	URL            string   `json:"url"`
	Channel        string   `json:"channel,omitempty"`
	Categories     []string `json:"categories,omitempty"`
	Duration       float64  `json:"duration,omitempty"`
	ViewCount      int64    `json:"view_count,omitempty"`
	ActualDuration float64  `json:"actual_duration,omitempty"`
	Undecodable    bool     `json:"undecodable,omitempty"`
	DownloadFails  bool     `json:"download_fails,omitempty"`
	RecordingMBIDs []string `json:"recording_mbids,omitempty"`
	AcoustID       string   `json:"acoustid,omitempty"`
	Resolved       bool     `json:"resolved,omitempty"`
	Correct        bool     `json:"correct,omitempty"`
}

func (c Candidate) linksRecording(mbid string) bool {
	return mbid != "" && slices.Contains(c.RecordingMBIDs, mbid)
}

func (c Candidate) probedDuration() float64 {
	if c.ActualDuration > 0 {
		return c.ActualDuration
	}
	return c.Duration
}

type Case struct {
	ID            string      `json:"id"`
	Class         string      `json:"class"`
	Note          string      `json:"note,omitempty"`
	Track         Track       `json:"track"`
	ExcludeURLs   []string    `json:"exclude_urls,omitempty"`
	SkipTopRanked bool        `json:"skip_top_ranked,omitempty"`
	Candidates    []Candidate `json:"candidates"`
	Pending       string      `json:"pending,omitempty"`
}

func (c Case) isPending() bool { return c.Pending != "" }

func (c Case) hasCorrectCandidate() bool {
	for _, cand := range c.Candidates {
		if cand.Correct {
			return true
		}
	}
	return false
}

func (c Case) candidateByURL(url string) (Candidate, bool) {
	for _, cand := range c.Candidates {
		if cand.URL == url {
			return cand, true
		}
	}
	return Candidate{}, false
}

type suite struct {
	Cases []Case `json:"cases"`
}

func LoadEmbedded() ([]Case, error) {
	return loadFS(goldenFS, "goldens")
}

func LoadDir(dir string) ([]Case, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read golden dir: %w", err)
	}
	var cases []Case
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		parsed, err := decodeSuite(e.Name(), raw)
		if err != nil {
			return nil, err
		}
		cases = append(cases, parsed...)
	}
	return sortedByID(cases), validateCases(cases)
}

func loadFS(fsys fs.FS, dir string) ([]Case, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read embedded goldens: %w", err)
	}
	var cases []Case
	for _, e := range entries {
		raw, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(dir, e.Name())))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		parsed, err := decodeSuite(e.Name(), raw)
		if err != nil {
			return nil, err
		}
		cases = append(cases, parsed...)
	}
	return sortedByID(cases), validateCases(cases)
}

func decodeSuite(name string, raw []byte) ([]Case, error) {
	var s suite
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	return s.Cases, nil
}

func sortedByID(cases []Case) []Case {
	sort.SliceStable(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases
}

func validateCases(cases []Case) error {
	seen := make(map[string]bool, len(cases))
	for _, c := range cases {
		switch {
		case c.ID == "":
			return fmt.Errorf("golden case with no id")
		case seen[c.ID]:
			return fmt.Errorf("duplicate golden case id %q", c.ID)
		case c.Class == "":
			return fmt.Errorf("case %q has no failure class", c.ID)
		case c.Track.Title == "" || c.Track.Artist == "":
			return fmt.Errorf("case %q needs a track title and artist", c.ID)
		case len(c.Candidates) == 0:
			return fmt.Errorf("case %q has no candidates", c.ID)
		}
		urls := make(map[string]bool, len(c.Candidates))
		for _, cand := range c.Candidates {
			if cand.URL == "" {
				return fmt.Errorf("case %q has a candidate with no url", c.ID)
			}
			if urls[cand.URL] {
				return fmt.Errorf("case %q repeats candidate url %q", c.ID, cand.URL)
			}
			urls[cand.URL] = true
		}
		if err := validateCaseExtensions(c); err != nil {
			return err
		}
		seen[c.ID] = true
	}
	return nil
}

func validateCaseExtensions(c Case) error {
	if c.isPending() && strings.TrimSpace(c.Pending) == "" {
		return fmt.Errorf("case %q is pending without naming its owning ticket", c.ID)
	}
	return validateResolution(c)
}

func validateResolution(c Case) error {
	r := c.Track.Resolution
	switch {
	case r == nil:
		return nil
	case c.Track.MBID != "" || len(c.Track.AcoustIDs) > 0 || c.Track.AuthoritativeDuration > 0:
		return fmt.Errorf("case %q sets a resolution and a copied identity; keep one", c.ID)
	case len(r.ISRCRecordings) > 0 && c.Track.ISRC == "":
		return fmt.Errorf("case %q lists isrc recordings for a track with no isrc", c.ID)
	}
	for _, rec := range r.ISRCRecordings {
		if rec.MBID == "" {
			return fmt.Errorf("case %q has an isrc recording with no mbid", c.ID)
		}
	}
	return nil
}
