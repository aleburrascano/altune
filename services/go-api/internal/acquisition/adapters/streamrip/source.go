package streamrip

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"altune/go-api/internal/acquisition/ports"
)

const (
	fetchTimeout = 10 * time.Minute
	minFileSize  = 10 * 1024
	defaultBin   = "rip"
)

var audioExtensions = []string{".flac", ".m4a", ".mp3", ".opus", ".ogg"}

var trackURLs = map[string]string{
	"tidal":      "https://tidal.com/browse/track/",
	"deezer":     "https://www.deezer.com/track/",
	"qobuz":      "https://open.qobuz.com/track/",
	"soundcloud": "",
}

func Supported(service string) bool {
	_, ok := trackURLs[service]
	return ok
}

var _ ports.AudioSource = (*Source)(nil)

type Source struct {
	service string
	bin     string
}

func NewSource(service string) *Source {
	return &Source{service: service, bin: defaultBin}
}

func (s *Source) WithBinary(bin string) *Source {
	if bin != "" {
		s.bin = bin
	}
	return s
}

func (s *Source) Name() string { return "streamrip:" + s.service }

func (s *Source) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	source, ok := req.Identity.SourceFor(s.service)
	if !ok {
		return nil, nil
	}
	url := s.trackURL(source)
	if url == "" {
		return nil, nil
	}

	slog.InfoContext(ctx, "acquisition.streamrip_resolved",
		"service", s.service, "external_id", source.ExternalID, "title", req.Title)

	return []ports.AudioCandidate{{
		Title:      req.Title,
		Duration:   req.Identity.Duration,
		URL:        url,
		Channel:    s.service + " catalog",
		Categories: []string{"Music"},
		Resolved:   true,
	}}, nil
}

func (s *Source) trackURL(source ports.RecordingSource) string {
	prefix, ok := trackURLs[s.service]
	if !ok {
		return ""
	}
	if prefix == "" {
		if strings.HasPrefix(source.URL, "http") {
			return source.URL
		}
		return ""
	}
	if source.ExternalID == "" {
		return ""
	}
	return prefix + source.ExternalID
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(fetchCtx, s.bin, "--folder", outDir, "--no-db", "url", candidate.URL)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("streamrip %s: %w (stderr: %s)", s.service, err, truncate(stderr.String()))
	}

	return largestAudioFile(outDir)
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

func largestAudioFile(dir string) (string, error) {
	best, bestSize := "", int64(-1)

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isAudio(path) {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if info.Size() > bestSize {
			best, bestSize = path, info.Size()
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("scan %s: %w", dir, err)
	}
	if best == "" {
		return "", fmt.Errorf("no audio file produced in %s", dir)
	}
	if bestSize < minFileSize {
		return "", fmt.Errorf("downloaded file too small (%d bytes), likely corrupt", bestSize)
	}
	return best, nil
}

func isAudio(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, candidate := range audioExtensions {
		if ext == candidate {
			return true
		}
	}
	return false
}
