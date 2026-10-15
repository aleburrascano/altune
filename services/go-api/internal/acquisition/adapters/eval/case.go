package eval

import (
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
)

//go:embed goldens/*.json
var goldenFS embed.FS

type Track struct {
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	ISRC     string  `json:"isrc,omitempty"`

	CatalogDuration float64         `json:"catalog_duration,omitempty"`
	CatalogMBID     string          `json:"catalog_mbid,omitempty"`
	CatalogSources  []CatalogSource `json:"catalog_sources,omitempty"`
	CatalogExplicit *bool           `json:"catalog_explicit,omitempty"`

	AuthoritativeDuration float64 `json:"authoritative_duration,omitempty"`
	MBID                  string  `json:"mbid,omitempty"`

	Resolution *Resolution `json:"resolution,omitempty"`
}

type CatalogSource struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url,omitempty"`
}

type Resolution struct {
	Search         []SearchedRecording `json:"search,omitempty"`
	ISRCRecordings []ISRCRecording     `json:"isrc_recordings,omitempty"`
	Pinned         *PinnedRecording    `json:"pinned,omitempty"`
}

type PinnedRecording struct {
	MBID           string   `json:"mbid"`
	Title          string   `json:"title,omitempty"`
	Disambiguation string   `json:"disambiguation,omitempty"`
	Artists        []string `json:"artists,omitempty"`
	ISRCs          []string `json:"isrcs,omitempty"`
	Duration       float64  `json:"duration,omitempty"`
}

func (p PinnedRecording) facts() ports.RecordingFacts { return ports.RecordingFacts(p) }

type SearchedRecording struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album,omitempty"`
	MBID     string `json:"mbid,omitempty"`
	ISRC     string `json:"isrc,omitempty"`
	Duration int    `json:"duration,omitempty"`
}

type ISRCRecording struct {
	MBID     string `json:"mbid"`
	Duration int    `json:"duration,omitempty"`
}

type Candidate struct {
	Title           string           `json:"title"`
	URL             string           `json:"url"`
	Channel         string           `json:"channel,omitempty"`
	Categories      []string         `json:"categories,omitempty"`
	Duration        float64          `json:"duration,omitempty"`
	ViewCount       int64            `json:"view_count,omitempty"`
	ActualDuration  float64          `json:"actual_duration,omitempty"`
	Undecodable     bool             `json:"undecodable,omitempty"`
	DownloadFails   bool             `json:"download_fails,omitempty"`
	RecordingMBIDs  []string         `json:"recording_mbids,omitempty"`
	AcoustID        string           `json:"acoustid,omitempty"`
	AcoustIDResults []AcoustIDResult `json:"acoustid_results,omitempty"`
	Resolved        bool             `json:"resolved,omitempty"`
	Kind            string           `json:"kind,omitempty"`
	Artists         []string         `json:"artists,omitempty"`
	Album           string           `json:"album,omitempty"`
	Explicit        bool             `json:"explicit,omitempty"`
	Correct         bool             `json:"correct,omitempty"`
	Query           string           `json:"query,omitempty"`
	DownloadSeconds float64          `json:"download_seconds,omitempty"`
	Observed        *Observed        `json:"observed,omitempty"`
}

type Observed struct {
	Song  *ytmsearch.Song `json:"ytmsearch_song,omitempty"`
	Watch *ObservedWatch  `json:"ytmusic_watch,omitempty"`
	Entry *ytdlp.Entry    `json:"ytdlp_entry,omitempty"`
}

type ObservedWatch struct {
	ytmsearch.Watch
	Pinned bool
}

func (o Observed) emit() (ports.AudioCandidate, error) {
	switch {
	case o.Song != nil && o.Watch == nil && o.Entry == nil:
		return ytmsearch.CandidateFor(*o.Song), nil
	case o.Watch != nil && o.Song == nil && o.Entry == nil:
		return ytmusic.CandidateForWatch(o.Watch.VideoID, o.Watch.Watch, o.Watch.Pinned), nil
	case o.Entry != nil && o.Song == nil && o.Watch == nil:
		return ytdlp.CandidateFromEntry(*o.Entry), nil
	}
	return ports.AudioCandidate{}, errors.New("observed must hold exactly one of ytmsearch_song, ytmusic_watch, ytdlp_entry")
}

func (c Candidate) setsEmittedFields() bool {
	return c.Title != "" || c.URL != "" || c.Channel != "" || len(c.Categories) > 0 ||
		c.Duration != 0 || c.ViewCount != 0 || c.Resolved || c.Kind != "" ||
		len(c.Artists) > 0 || c.Album != "" || c.Explicit
}

func (c Candidate) builtByItsAdapter() (Candidate, error) {
	if c.Observed == nil {
		return c, nil
	}
	if c.setsEmittedFields() {
		return Candidate{}, errors.New("an observed candidate takes its title, url, channel, duration and kind from the adapter; set none of them beside it")
	}
	emitted, err := c.Observed.emit()
	if err != nil {
		return Candidate{}, err
	}
	c.Title, c.URL, c.Channel, c.Categories = emitted.Title, emitted.URL, emitted.Channel, emitted.Categories
	c.Duration, c.ViewCount, c.Resolved, c.Kind = emitted.Duration, emitted.ViewCount, emitted.IsResolved(), string(emitted.Kind)
	c.Artists, c.Album, c.Explicit = emitted.Artists, emitted.Album, emitted.Explicit
	return c, nil
}

func (c Candidate) audioCandidate() ports.AudioCandidate {
	return ports.AudioCandidate{
		Title:       c.Title,
		Duration:    c.Duration,
		URL:         c.URL,
		Channel:     c.Channel,
		Categories:  c.Categories,
		ViewCount:   c.ViewCount,
		Kind:        ports.CandidateKind(c.Kind),
		ObservedOMV: ports.CandidateKind(c.Kind) == ports.KindPinnedVideo,
		Artists:     c.Artists,
		Album:       c.Album,
		Explicit:    c.Explicit,
	}
}

type AcoustIDResult struct {
	ID         string            `json:"id"`
	Score      float64           `json:"score"`
	Recordings []LinkedRecording `json:"recordings"`
}

type LinkedRecording struct {
	MBID     string   `json:"mbid"`
	Title    string   `json:"title"`
	Artists  []string `json:"artists,omitempty"`
	Duration float64  `json:"duration,omitempty"`
}

func (t Track) portCatalogSources() []ports.CatalogSource {
	if len(t.CatalogSources) == 0 {
		return nil
	}
	out := make([]ports.CatalogSource, len(t.CatalogSources))
	for i, s := range t.CatalogSources {
		out[i] = ports.CatalogSource(s)
	}
	return out
}

func (c Candidate) portResults() []ports.AcoustIDResult {
	results := make([]ports.AcoustIDResult, 0, len(c.AcoustIDResults))
	for _, r := range c.AcoustIDResults {
		recordings := make([]ports.LinkedRecording, 0, len(r.Recordings))
		for _, rec := range r.Recordings {
			recordings = append(recordings, ports.LinkedRecording(rec))
		}
		results = append(results, ports.AcoustIDResult{ID: r.ID, Score: r.Score, Recordings: recordings})
	}
	return results
}

const (
	adapterYTMusic    = "ytmusic"
	adapterCatalogPin = "catalogpin"
)

const (
	QueryISRC             = "isrc"
	QueryTitleArtist      = "title_artist"
	QueryTitleArtistAlbum = "title_artist_album"
	QueryTitleArtistAudio = "title_artist_audio"
)

var knownQueries = map[string]bool{
	QueryISRC:             true,
	QueryTitleArtist:      true,
	QueryTitleArtistAlbum: true,
	QueryTitleArtistAudio: true,
}

const (
	defaultSearchSeconds   = 5
	defaultDownloadSeconds = 20
)

type Source struct {
	Name          string      `json:"name"`
	Adapter       string      `json:"adapter,omitempty"`
	SearchSeconds float64     `json:"search_seconds,omitempty"`
	Fails         bool        `json:"fails,omitempty"`
	Candidates    []Candidate `json:"candidates"`
}

func (s Source) searchSeconds() float64 {
	if s.SearchSeconds > 0 {
		return s.SearchSeconds
	}
	return defaultSearchSeconds
}

func (c Candidate) downloadSeconds() float64 {
	if c.DownloadSeconds > 0 {
		return c.DownloadSeconds
	}
	return defaultDownloadSeconds
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
	ID          string            `json:"id"`
	Class       string            `json:"class"`
	Note        string            `json:"note,omitempty"`
	Track       Track             `json:"track"`
	ExcludeURLs []string          `json:"exclude_urls,omitempty"`
	Stored      *StoredFile       `json:"stored,omitempty"`
	Recordings  []PinnedRecording `json:"recordings,omitempty"`
	Candidates  []Candidate       `json:"candidates,omitempty"`
	Sources     []Source          `json:"sources,omitempty"`
	Pending     string            `json:"pending,omitempty"`

	ExpectProvenance  string `json:"expect_provenance,omitempty"`
	ExpectFailureCode string `json:"expect_failure_code,omitempty"`

	ExpectSaved *SavedExpectation `json:"expect_saved,omitempty"`
}

type StoredFile struct {
	Duration  float64 `json:"duration"`
	AcoustID  string  `json:"acoustid,omitempty"`
	SourceURL string  `json:"source_url,omitempty"`
}

type SavedExpectation struct {
	ISRC            string   `json:"isrc,omitempty"`
	ISRCSource      string   `json:"isrc_source,omitempty"`
	Provenance      string   `json:"provenance,omitempty"`
	DurationSeconds float64  `json:"duration_seconds,omitempty"`
	RejectedKeys    []string `json:"rejected_keys,omitempty"`
}

func (c Case) isPending() bool { return c.Pending != "" }

func (c Case) usesNamedSources() bool { return len(c.Sources) > 0 }

func (c Case) allCandidates() []Candidate {
	if !c.usesNamedSources() {
		return c.Candidates
	}
	var all []Candidate
	for _, src := range c.Sources {
		all = append(all, src.Candidates...)
	}
	return all
}

func (c Case) hasCorrectCandidate() bool {
	for _, cand := range c.allCandidates() {
		if cand.Correct {
			return true
		}
	}
	return false
}

func (c Case) candidateByURL(url string) (Candidate, bool) {
	for _, cand := range c.allCandidates() {
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
	for i := range s.Cases {
		if err := s.Cases[i].buildCandidatesByTheirAdapters(); err != nil {
			return nil, fmt.Errorf("%s: case %q: %w", name, s.Cases[i].ID, err)
		}
	}
	return s.Cases, nil
}

func (c *Case) buildCandidatesByTheirAdapters() error {
	for _, group := range c.candidateGroups() {
		for i := range group {
			built, err := group[i].builtByItsAdapter()
			if err != nil {
				return err
			}
			group[i] = built
		}
	}
	return nil
}

func (c *Case) candidateGroups() [][]Candidate {
	groups := [][]Candidate{c.Candidates}
	for i := range c.Sources {
		groups = append(groups, c.Sources[i].Candidates)
	}
	return groups
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
		case len(c.Candidates) > 0 && c.usesNamedSources():
			return fmt.Errorf("case %q sets both candidates and sources; keep one", c.ID)
		case len(c.allCandidates()) == 0:
			return fmt.Errorf("case %q has no candidates", c.ID)
		}
		if err := rejectUnsafeText(c.ID,
			textField{"track title", c.Track.Title},
			textField{"track artist", c.Track.Artist},
			textField{"track album", c.Track.Album},
		); err != nil {
			return err
		}
		if err := validateSources(c); err != nil {
			return err
		}
		if err := validateCandidates(c); err != nil {
			return err
		}
		if err := validateCaseExtensions(c); err != nil {
			return err
		}
		seen[c.ID] = true
	}
	return nil
}

func validateSources(c Case) error {
	for _, src := range c.Sources {
		if src.Name == "" {
			return fmt.Errorf("case %q has a source with no name", c.ID)
		}
		if src.Adapter != "" && src.Adapter != adapterYTMusic && src.Adapter != adapterCatalogPin {
			return fmt.Errorf("case %q source %q names unknown adapter %q", c.ID, src.Name, src.Adapter)
		}
		if len(src.Candidates) == 0 && !src.Fails {
			return fmt.Errorf("case %q source %q has no candidates", c.ID, src.Name)
		}
	}
	return nil
}

func validateCandidates(c Case) error {
	urls := make(map[string]bool)
	for _, cand := range c.allCandidates() {
		if cand.URL == "" {
			return fmt.Errorf("case %q has a candidate with no url", c.ID)
		}
		if urls[cand.URL] {
			return fmt.Errorf("case %q repeats candidate url %q", c.ID, cand.URL)
		}
		urls[cand.URL] = true
		if err := validateCandidateShape(c.ID, cand); err != nil {
			return err
		}
		if cand.Query != "" && !knownQueries[cand.Query] {
			return fmt.Errorf("case %q candidate %q has unknown query %q", c.ID, cand.URL, cand.Query)
		}
		if err := rejectUnsafeText(c.ID,
			textField{fmt.Sprintf("candidate %q title", cand.URL), cand.Title},
			textField{fmt.Sprintf("candidate %q channel", cand.URL), cand.Channel},
		); err != nil {
			return err
		}
	}
	return nil
}

func validateCandidateShape(caseID string, cand Candidate) error {
	kindResolved := ports.AudioCandidate{Kind: ports.CandidateKind(cand.Kind)}.IsResolved()
	if cand.Resolved != kindResolved {
		return fmt.Errorf("case %q candidate %q has resolved=%t but kind %q reads as resolved=%t", caseID, cand.URL, cand.Resolved, cand.Kind, kindResolved)
	}
	return nil
}

type textField struct {
	name  string
	value string
}

func rejectUnsafeText(caseID string, fields ...textField) error {
	for _, f := range fields {
		if r, found := firstUnsafeRune(f.value); found {
			return fmt.Errorf("case %q %s holds control or bidi rune %U", caseID, f.name, r)
		}
	}
	return nil
}

func firstUnsafeRune(s string) (rune, bool) {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return r, true
		}
	}
	return 0, false
}

func validateCaseExtensions(c Case) error {
	if c.isPending() && strings.TrimSpace(c.Pending) == "" {
		return fmt.Errorf("case %q is pending without naming its owning ticket", c.ID)
	}
	if err := validateExpectProvenance(c); err != nil {
		return err
	}
	if err := validateStoredAndSaved(c); err != nil {
		return err
	}
	if err := validateRecordings(c); err != nil {
		return err
	}
	return validateResolution(c)
}

func validateRecordings(c Case) error {
	seen := make(map[string]bool, len(c.Recordings))
	for _, rec := range c.Recordings {
		switch {
		case rec.MBID == "":
			return fmt.Errorf("case %q records a recording lookup with no mbid", c.ID)
		case seen[rec.MBID]:
			return fmt.Errorf("case %q records recording %q twice", c.ID, rec.MBID)
		}
		seen[rec.MBID] = true
	}
	return nil
}

func validateExpectProvenance(c Case) error {
	if c.ExpectProvenance != "" && !shared.AcquisitionProvenance(c.ExpectProvenance).Valid() {
		return fmt.Errorf("case %q expects unknown provenance %q", c.ID, c.ExpectProvenance)
	}
	return nil
}

func validateResolution(c Case) error {
	r := c.Track.Resolution
	switch {
	case r == nil:
		return nil
	case c.Track.MBID != "" || c.Track.AuthoritativeDuration > 0:
		return fmt.Errorf("case %q sets a resolution and a copied identity; keep one", c.ID)
	case len(r.ISRCRecordings) > 0 && c.Track.ISRC == "":
		return fmt.Errorf("case %q lists isrc recordings for a track with no isrc", c.ID)
	}
	for _, hit := range r.Search {
		if hit.Title == "" || hit.Artist == "" {
			return fmt.Errorf("case %q has a recorded search hit with no title or artist", c.ID)
		}
	}
	for _, rec := range r.ISRCRecordings {
		if rec.MBID == "" {
			return fmt.Errorf("case %q has an isrc recording with no mbid", c.ID)
		}
	}
	return validatePinned(c)
}

func validatePinned(c Case) error {
	pinned := c.Track.Resolution.Pinned
	switch {
	case pinned == nil:
		return nil
	case c.Track.CatalogMBID == "":
		return fmt.Errorf("case %q records a pinned recording for a track with no catalog mbid", c.ID)
	case pinned.MBID != c.Track.CatalogMBID:
		return fmt.Errorf("case %q records pinned recording %q but the catalog pins %q", c.ID, pinned.MBID, c.Track.CatalogMBID)
	}
	return nil
}

func validateStoredAndSaved(c Case) error {
	if c.Stored != nil && c.Stored.Duration <= 0 {
		return fmt.Errorf("case %q stores a file with no duration", c.ID)
	}
	saved := c.ExpectSaved
	if saved != nil && saved.Provenance != "" && !shared.AcquisitionProvenance(saved.Provenance).Valid() {
		return fmt.Errorf("case %q expects unknown saved provenance %q", c.ID, saved.Provenance)
	}
	return nil
}
