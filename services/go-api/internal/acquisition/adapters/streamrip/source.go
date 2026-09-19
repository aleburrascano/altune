package streamrip

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/execcmd"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	fetchTimeout = 10 * time.Minute
	minFileSize  = 10 * 1024
	defaultBin   = "rip"

	// maxFileSize caps what a fetch may hand back. rip has no size flag of its
	// own, so the cap lands after the walk: a lossless single track stays far
	// below it, and a file above it is a set or a mix that would fill /tmp for
	// every concurrent worker and be rejected on duration anyway.
	maxFileSize = 200 * 1024 * 1024
)

var audioExtensions = []string{".flac", ".m4a", ".mp3", ".opus", ".ogg"}

var trackURLs = map[string]string{
	ports.ProviderTidal:      "https://tidal.com/browse/track/",
	ports.ProviderDeezer:     "https://www.deezer.com/track/",
	ports.ProviderQobuz:      "https://open.qobuz.com/track/",
	ports.ProviderSoundCloud: "",
}

// A poisoned provider record must not get to choose the address the server
// fetches: the id is concatenated onto a fixed prefix and the permalink reaches
// rip verbatim, both of them third-party discovery data.
var (
	catalogIDPattern = regexp.MustCompile(`^\d+$`)

	soundCloudHosts = map[string]bool{
		"soundcloud.com":     true,
		"www.soundcloud.com": true,
		"m.soundcloud.com":   true,
	}
)

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

// Available reports whether the configured rip binary resolves to something
// runnable, so wiring can surface a missing binary at startup instead of at the
// first background Fetch.
func (s *Source) Available() bool {
	return binpath.Runnable(s.bin)
}

// Binary is the rip binary the source invokes: the configured path, or "rip"
// resolved on PATH when none was configured.
func (s *Source) Binary() string { return s.bin }

func (s *Source) Name() string { return "streamrip:" + s.service }

func (s *Source) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	source, ok := req.Identity.SourceFor(s.service)
	if !ok {
		return nil, nil
	}
	candidateURL := s.trackURL(source)
	if candidateURL == "" {
		logUnusableSource(ctx, s.service, source)
		return nil, nil
	}

	slog.InfoContext(ctx, "acquisition.streamrip_resolved",
		"service", s.service, "external_id", source.ExternalID, "title", req.Title)

	return []ports.AudioCandidate{{
		Title:      req.Title,
		Duration:   req.Identity.Duration,
		URL:        candidateURL,
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
		return soundCloudPermalink(source.URL)
	}
	if !catalogIDPattern.MatchString(source.ExternalID) {
		return ""
	}
	return prefix + source.ExternalID
}

func soundCloudPermalink(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || !isSoundCloudAddress(parsed) {
		return ""
	}
	return rawURL
}

// isSoundCloudAddress rejects credentials and a port alongside the host, so the
// string rip parses cannot carry an authority Go read one way and Python another.
func isSoundCloudAddress(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && soundCloudHosts[strings.ToLower(u.Host)]
}

// logUnusableSource surfaces a provider record that named this service yet
// carried nothing fetchable, so a poisoned or drifted catalog is visible rather
// than an unexplained missing candidate.
func logUnusableSource(ctx context.Context, service string, source ports.RecordingSource) {
	if source.ExternalID == "" && source.URL == "" {
		return
	}
	slog.WarnContext(ctx, "acquisition.streamrip_source_rejected",
		"service", service, "external_id", source.ExternalID, "url", source.URL)
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error) {
	_, stderr, err := execcmd.RunWithTimeout(ctx, fetchTimeout, s.bin, "--folder", outDir, "--no-db", "url", "--", candidate.URL)
	if err != nil {
		return "", fmt.Errorf("streamrip %s: %w (%s)", s.service, err, diagnose(stderr))
	}

	return largestAudioFile(outDir)
}

func diagnose(stderr string) string {
	switch {
	case strings.Contains(stderr, "unable to open database file"):
		return "streamrip could not create its download database: set downloads_enabled and " +
			"failed_downloads_enabled to false in config.toml"
	case strings.Contains(stderr, "Deezer HiFi is required"):
		return "the configured Deezer account cannot stream at the requested quality; lower [deezer] quality"
	default:
		return "stderr: " + truncate(stderr)
	}
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
			return nil //nolint:nilerr // skip an unreadable entry, keep scanning for the largest audio file
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil //nolint:nilerr // entry vanished mid-walk (race); skip it and keep scanning
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
	if bestSize > maxFileSize {
		return "", fmt.Errorf("downloaded file too large (%d bytes, cap %d), not a single track", bestSize, maxFileSize)
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
