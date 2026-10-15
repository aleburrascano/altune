package eval

import (
	"altune/go-api/internal/acquisition/adapters/catalogpin"
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type simClock struct {
	mu     sync.Mutex
	search float64
}

func (c *simClock) recordSearch(seconds float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seconds > c.search {
		c.search = seconds
	}
}

func (c *simClock) searchSeconds() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.search
}

type casePorts struct {
	kase       Case
	pathsMu    sync.Mutex
	byPath     map[string]Candidate
	storedRefs []string
	clock      *simClock
}

func newCasePorts(kase Case) *casePorts {
	return &casePorts{kase: kase, byPath: make(map[string]Candidate), clock: &simClock{}}
}

const ytdlpTier acqdomain.SourceName = "ytdlp"

var prodFallbackTier = map[string]bool{string(ytdlpTier): true}

func (p *casePorts) registry() *service.SourceRegistry {
	var first, fallback []ports.AudioSource
	for _, source := range p.sources() {
		if prodFallbackTier[source.spec.Name] {
			fallback = append(fallback, source)
			continue
		}
		first = append(first, source)
	}
	return service.NewSourceRegistry(first...).WithFallback(fallback...)
}

func (p *casePorts) sources() []*evalSource {
	specs := p.kase.Sources
	if !p.kase.usesNamedSources() {
		specs = flatTiers(p.kase.Candidates)
	}
	sources := make([]*evalSource, 0, len(specs))
	for _, spec := range specs {
		sources = append(sources, &evalSource{spec: spec, shared: p})
	}
	return sources
}

var flatTierOrder = []acqdomain.SourceName{ytmsearch.SourceName, ytmusic.SourceName, catalogpin.SourceName, ytdlpTier}

func flatTierOf(c Candidate) acqdomain.SourceName {
	switch ports.CandidateKind(c.Kind) {
	case ports.KindArtTrack:
		return ytmsearch.SourceName
	case ports.KindPinnedVideo:
		return ytmusic.SourceName
	case ports.KindCatalogPin:
		return catalogpin.SourceName
	}
	return ytdlpTier
}

func flatTiers(candidates []Candidate) []Source {
	var tiers []Source
	for _, name := range flatTierOrder {
		var members []Candidate
		for _, c := range candidates {
			if flatTierOf(c) == name {
				members = append(members, c)
			}
		}
		if len(members) > 0 {
			tiers = append(tiers, Source{Name: string(name), SearchSeconds: defaultSearchSeconds, Candidates: members})
		}
	}
	return tiers
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

func (s *evalSource) Name() acqdomain.SourceName { return acqdomain.SourceName(s.spec.Name) }

func (s *evalSource) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.shared.clock.recordSearch(s.spec.searchSeconds())
	if s.spec.Fails {
		return nil, &acqdomain.SourceUnavailableError{Source: s.Name(), Err: errors.New("eval: simulated source failure")}
	}
	switch s.spec.Adapter {
	case adapterYTMusic:
		return s.pinnedVideos(ctx, req)
	case adapterCatalogPin:
		return catalogpin.NewSource(nil).Find(ctx, req)
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
			Kind:       ports.CandidateKind(c.Kind),
			Artists:    c.Artists,
			Album:      c.Album,
			Explicit:   c.Explicit,
		})
	}
	return out, nil
}

func (s *evalSource) pinnedVideos(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	return ytmusic.NewSource(nil, goldenWatches{candidates: s.spec.Candidates}).Find(ctx, req)
}

type goldenWatches struct{ candidates []Candidate }

func (g goldenWatches) Watch(_ context.Context, videoID string) (ytmsearch.Watch, error) {
	for _, c := range g.candidates {
		if strings.HasSuffix(c.URL, "="+videoID) {
			return ytmsearch.Watch{VideoID: videoID, Title: c.Title, Artists: c.Artists, Explicit: c.Explicit}, nil
		}
	}
	return ytmsearch.Watch{}, fmt.Errorf("eval: no golden watch for video %q", videoID)
}

func (s *evalSource) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	path, err := s.shared.Download(ctx, c.URL, outDir)
	return ports.FetchedAudio{Path: path}, err
}

func (p *casePorts) Download(_ context.Context, url string, outDir string) (string, error) {
	cand, ok := p.kase.candidateByURL(url)
	if !ok {
		return "", fmt.Errorf("eval: no golden candidate for url %q", url)
	}
	if cand.DownloadFails {
		return "", fmt.Errorf("eval: simulated download failure for %q", url)
	}
	path := filepath.Join(outDir, "track.mp3")
	if err := os.WriteFile(path, []byte("eval-audio-bytes"), 0o644); err != nil {
		return "", err
	}
	p.pathsMu.Lock()
	defer p.pathsMu.Unlock()
	p.byPath[path] = cand
	return path, nil
}

func (p *casePorts) candidateAt(path string) (Candidate, bool) {
	p.pathsMu.Lock()
	defer p.pathsMu.Unlock()
	cand, ok := p.byPath[path]
	return cand, ok
}

func (p *casePorts) ProbeDuration(_ context.Context, filePath string) (float64, error) {
	cand, ok := p.candidateAt(filePath)
	if !ok {
		return 0, fmt.Errorf("eval: probe of unknown path %q", filePath)
	}
	return cand.probedDuration(), nil
}

func (p *casePorts) ValidateDecodable(_ context.Context, filePath string) error {
	cand, ok := p.candidateAt(filePath)
	if !ok {
		return nil
	}
	if cand.Undecodable {
		return errors.New("eval: audio stream failed to decode")
	}
	return nil
}

func (p *casePorts) Identify(_ context.Context, filePath string, _ float64) (ports.RecordingMatch, error) {
	cand, ok := p.candidateAt(filePath)
	if !ok || (len(cand.RecordingMBIDs) == 0 && cand.AcoustID == "" && len(cand.AcoustIDResults) == 0) {
		return ports.RecordingMatch{}, nil
	}
	acoustID := cand.AcoustID
	if acoustID == "" && len(cand.AcoustIDResults) > 0 {
		acoustID = cand.AcoustIDResults[0].ID
	}
	return ports.RecordingMatch{AcoustID: acoustID, MBIDs: cand.RecordingMBIDs, Score: 1, Results: cand.chromaprintResults(p.kase.Track.Artist)}, nil
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

func (p *casePorts) CopyStored(_ context.Context, _, dir string) (string, error) {
	path := filepath.Join(dir, "stored.mp3")
	if err := os.WriteFile(path, []byte("eval-stored-audio-bytes"), 0o644); err != nil {
		return "", err
	}
	p.pathsMu.Lock()
	defer p.pathsMu.Unlock()
	p.byPath[path] = Candidate{AcoustID: p.kase.Stored.AcoustID}
	return path, nil
}

type copiedIdentity ports.RecordingIdentity

func (c copiedIdentity) Resolve(context.Context, ports.RecordingQuery) (ports.RecordingIdentity, error) {
	return ports.RecordingIdentity(c), nil
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

func (c Candidate) chromaprintResults(trackArtist string) []ports.AcoustIDResult {
	if len(c.AcoustIDResults) > 0 {
		return c.portResults()
	}
	recordings := make([]ports.LinkedRecording, 0, len(c.RecordingMBIDs))
	for _, mbid := range c.RecordingMBIDs {
		recordings = append(recordings, ports.LinkedRecording{MBID: mbid, Title: c.Title, Artists: []string{trackArtist}, Duration: c.Duration})
	}
	return []ports.AcoustIDResult{{ID: c.AcoustID, Score: 1, Recordings: recordings}}
}
