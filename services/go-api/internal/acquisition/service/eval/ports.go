package eval

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

type simClock struct {
	mu       sync.Mutex
	search   float64
	download float64
	attempts int
}

func (c *simClock) recordSearch(seconds float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seconds > c.search {
		c.search = seconds
	}
}

func (c *simClock) recordDownload(seconds float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.download += seconds
	c.attempts++
}

func (c *simClock) total() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.search + c.download
}

func (c *simClock) attemptCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts
}

type casePorts struct {
	kase       Case
	byPath     map[string]Candidate
	storedRefs []string
	clock      *simClock
}

func newCasePorts(kase Case) *casePorts {
	return &casePorts{kase: kase, byPath: make(map[string]Candidate), clock: &simClock{}}
}

func (p *casePorts) sources() []ports.AudioSource {
	specs := p.kase.Sources
	if !p.kase.usesNamedSources() {
		specs = []Source{{Name: "eval", SearchSeconds: defaultSearchSeconds, Candidates: p.kase.Candidates}}
	}
	sources := make([]ports.AudioSource, 0, len(specs))
	for _, spec := range specs {
		sources = append(sources, &evalSource{spec: spec, shared: p})
	}
	return sources
}

func queryVariantsFor(req ports.FindRequest) map[string]bool {
	variants := map[string]bool{
		QueryTitleArtist:      true,
		QueryTitleArtistAudio: true,
	}
	if req.ISRC != "" {
		variants[QueryISRC] = true
	}
	if req.Album != "" {
		variants[QueryTitleArtistAlbum] = true
	}
	return variants
}

type evalSource struct {
	spec   Source
	shared *casePorts
}

func (s *evalSource) Name() string { return s.spec.Name }

func (s *evalSource) Find(_ context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.shared.clock.recordSearch(s.spec.searchSeconds())
	if s.spec.Fails {
		return nil, &ports.SourceUnavailableError{Source: s.spec.Name, Err: errors.New("eval: simulated source failure")}
	}
	variants := queryVariantsFor(req)
	out := make([]ports.AudioCandidate, 0, len(s.spec.Candidates))
	for _, c := range s.spec.Candidates {
		if c.Query != "" && !variants[c.Query] {
			continue
		}
		out = append(out, ports.AudioCandidate{
			Title:      c.Title,
			Duration:   c.Duration,
			URL:        c.URL,
			Channel:    c.Channel,
			Categories: c.Categories,
			ViewCount:  c.ViewCount,
			Resolved:   c.Resolved,
		})
	}
	return out, nil
}

func (s *evalSource) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (string, error) {
	return s.shared.Download(ctx, c.URL, outDir)
}

func (p *casePorts) Download(_ context.Context, url string, outDir string) (string, error) {
	cand, ok := p.kase.candidateByURL(url)
	if !ok {
		return "", fmt.Errorf("eval: no golden candidate for url %q", url)
	}
	p.clock.recordDownload(cand.downloadSeconds())
	if cand.DownloadFails {
		return "", fmt.Errorf("eval: simulated download failure for %q", url)
	}
	path := filepath.Join(outDir, "track.mp3")
	if err := os.WriteFile(path, []byte("eval-audio-bytes"), 0o644); err != nil {
		return "", err
	}
	p.byPath[path] = cand
	return path, nil
}

func (p *casePorts) ProbeDuration(_ context.Context, filePath string) (float64, error) {
	cand, ok := p.byPath[filePath]
	if !ok {
		return 0, fmt.Errorf("eval: probe of unknown path %q", filePath)
	}
	return cand.probedDuration(), nil
}

func (p *casePorts) ValidateDecodable(_ context.Context, filePath string) error {
	cand, ok := p.byPath[filePath]
	if !ok {
		return nil
	}
	if cand.Undecodable {
		return errors.New("eval: audio stream failed to decode")
	}
	return nil
}

func (p *casePorts) Identify(_ context.Context, filePath string, _ float64) (ports.RecordingMatch, error) {
	cand, ok := p.byPath[filePath]
	if !ok || (len(cand.RecordingMBIDs) == 0 && cand.AcoustID == "" && len(cand.AcoustIDResults) == 0) {
		return ports.RecordingMatch{}, nil
	}
	acoustID := cand.AcoustID
	if acoustID == "" && len(cand.AcoustIDResults) > 0 {
		acoustID = cand.AcoustIDResults[0].ID
	}
	return ports.RecordingMatch{AcoustID: acoustID, MBIDs: cand.RecordingMBIDs, Score: 1, Results: cand.portResults()}, nil
}

func (p *casePorts) AcoustIDsFor(_ context.Context, mbid string) ([]string, error) {
	var linked []string
	for _, c := range p.kase.allCandidates() {
		if c.AcoustID != "" && c.linksRecording(mbid) && !slices.Contains(linked, c.AcoustID) {
			linked = append(linked, c.AcoustID)
		}
	}
	return linked, nil
}

func (p *casePorts) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

func (p *casePorts) Store(_ context.Context, _ string, audioRef string) error {
	p.storedRefs = append(p.storedRefs, audioRef)
	return nil
}

func (p *casePorts) Delete(_ context.Context, audioRef string) error {
	for i, ref := range p.storedRefs {
		if ref == audioRef {
			p.storedRefs = append(p.storedRefs[:i], p.storedRefs[i+1:]...)
			return nil
		}
	}
	return nil
}
