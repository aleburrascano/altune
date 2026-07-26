package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"altune/go-api/internal/acquisition/ports"
)

type casePorts struct {
	kase       Case
	byPath     map[string]Candidate
	storedRefs []string
}

func newCasePorts(kase Case) *casePorts {
	return &casePorts{kase: kase, byPath: make(map[string]Candidate)}
}

func (p *casePorts) Name() string { return "eval" }

func (p *casePorts) Find(ctx context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return p.Search(ctx, "")
}

func (p *casePorts) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (string, error) {
	return p.Download(ctx, c.URL, outDir)
}

func (p *casePorts) Search(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
	out := make([]ports.AudioCandidate, 0, len(p.kase.Candidates))
	for _, c := range p.kase.Candidates {
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

func (p *casePorts) Identify(_ context.Context, filePath string) (ports.RecordingMatch, error) {
	cand, ok := p.byPath[filePath]
	if !ok || len(cand.RecordingMBIDs) == 0 {
		return ports.RecordingMatch{}, nil
	}
	return ports.RecordingMatch{MBIDs: cand.RecordingMBIDs, Score: 1}, nil
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
