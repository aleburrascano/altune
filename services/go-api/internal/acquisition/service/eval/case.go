package eval

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

	AuthoritativeDuration float64 `json:"authoritative_duration,omitempty"`
	MBID                  string  `json:"mbid,omitempty"`
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
	Resolved       bool     `json:"resolved,omitempty"`
	Correct        bool     `json:"correct,omitempty"`
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
}

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
		seen[c.ID] = true
	}
	return nil
}
