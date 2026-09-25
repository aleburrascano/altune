package streamrip

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/execcmd"
	"altune/go-api/internal/shared/redact"
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
	defaultFetchTimeout = 10 * time.Minute
	minFileSize         = 10 * 1024
	defaultBin          = "rip"

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
	service      string
	bin          string
	fetchTimeout time.Duration
}

func NewSource(service string) *Source {
	return &Source{service: service, bin: defaultBin, fetchTimeout: defaultFetchTimeout}
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
	if err != nil || !isSoundCloudAddress(parsed) || !isSoundCloudTrackPath(parsed.Path) {
		return ""
	}
	return rawURL
}

var soundCloudNonTrackSegments = map[string]bool{
	"sets": true, "tracks": true, "albums": true, "popular-tracks": true,
	"likes": true, "reposts": true, "followers": true, "following": true,
	"discover": true, "search": true, "you": true, "stream": true,
}

func isSoundCloudTrackPath(path string) bool {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) != 2 {
		return false
	}
	for _, segment := range segments {
		if segment == "" {
			return false
		}
	}
	return !soundCloudNonTrackSegments[strings.ToLower(segments[0])] &&
		!soundCloudNonTrackSegments[strings.ToLower(segments[1])]
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
	runCtx, cancel := context.WithTimeout(ctx, s.fetchTimeout)
	defer cancel()
	_, stderr, err := execcmd.Run(runCtx, s.bin, "--folder", outDir, "--no-db", "url", "--", candidate.URL)
	if err != nil {
		return "", s.classifiedFailure(fmt.Errorf("streamrip %s: %w (%s)", s.service, err, diagnose(stderr)), stderr, ports.RunTimedOut(ctx, runCtx))
	}

	return largestAudioFile(outDir)
}

// classifiedFailure marks a rip run that failed for a reason carrying no
// evidence about the track — a throttle, an outage, a dead network, or a rip
// binary that is not installed — so the pipeline reports it as an unavailable
// source instead of a download the track can never satisfy.
func (s *Source) classifiedFailure(err error, stderr string, timedOut bool) error {
	if s.Available() && !timedOut && !ports.OutputShowsSourceUnavailable(stderr) {
		return err
	}
	return &ports.SourceUnavailableError{Source: s.Name(), Err: err}
}

func diagnose(stderr string) string {
	switch {
	case strings.Contains(stderr, "unable to open database file"):
		return "streamrip could not create its download database: set downloads_enabled and " +
			"failed_downloads_enabled to false in config.toml"
	case strings.Contains(stderr, "Deezer HiFi is required"):
		return "the configured Deezer account cannot stream at the requested quality; lower [deezer] quality"
	default:
		return "stderr: " + truncate(redactedStderr(stderr))
	}
}

// stderrTokenRe splits a traceback into the tokens a credential name and its
// value occupy. Quotes, brackets, commas, "=" and ":" end a token, so
// "arl=SECRET", "arl = SECRET" and "{'arl': 'SECRET'}" all put the name and the
// value in two adjacent tokens.
var stderrTokenRe = regexp.MustCompile(`[^\s'"(){}\[\],;=:]+`)

// redactedStderr strips the credentials and host layout rip prints about itself
// before the text becomes an error the caller stores and logs. redact.Secrets
// reaches only the pairs inside a URL query, and rip echoes its config as bare
// assignments in a Python traceback.
func redactedStderr(stderr string) string {
	afterCredentialName := false
	return stderrTokenRe.ReplaceAllStringFunc(redact.LogText(stderr), func(tok string) string {
		isValue := afterCredentialName
		afterCredentialName = isProviderCredential(tok)
		if isValue {
			return redact.Mask
		}
		return tok
	})
}

// isProviderCredential adds the credential names rip's own providers use to the
// codebase vocabulary: "arl" is the Deezer session cookie, a name no altune
// config carries and too short to become a marker in redact.IsSecretKey, where
// it would mask every field whose name merely contains those three letters.
func isProviderCredential(name string) bool {
	return redact.IsSecretKey(name) || strings.EqualFold(name, "arl")
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
