package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/execcmd"
	"altune/go-api/internal/shared/redact"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	inspectTimeout     = 20 * time.Second
	inspectConcurrency = 2
	inspectCacheTTL    = 24 * time.Hour
	inspectCacheMax    = 500

	unplayableDRM     = "drm"
	unplayablePreview = "preview"

	previewDurationSlack = 0.5
	previewFlatMargin    = 5.0
)

type inspectRunner func(ctx context.Context, trackURL string) (inspection, error)

type inspection struct {
	Duration       float64
	AudioFormats   int
	EncryptedAudio int
	PreviewFormat  bool
}

type inspectedFormat struct {
	FormatID string `json:"format_id"`
	URL      string `json:"url"`
	Protocol string `json:"protocol"`
	ACodec   string `json:"acodec"`
	VCodec   string `json:"vcodec"`
	HasDRM   any    `json:"has_drm"`
}

type inspectedInfo struct {
	Duration float64           `json:"duration"`
	Formats  []inspectedFormat `json:"formats"`
}

func (f inspectedFormat) isAudio() bool {
	return f.VCodec == "none"
}

func (f inspectedFormat) isEncrypted() bool {
	if drm, ok := f.HasDRM.(bool); ok && drm {
		return true
	}
	return strings.Contains(strings.ToLower(f.Protocol), "encrypted") ||
		strings.Contains(strings.ToLower(f.FormatID), "encrypted")
}

func (f inspectedFormat) isPreview() bool {
	for _, field := range []string{f.URL, f.FormatID} {
		lower := strings.ToLower(field)
		if strings.Contains(lower, "preview") || strings.Contains(lower, "snipped") {
			return true
		}
	}
	return false
}

func inspectionFromInfo(info inspectedInfo) inspection {
	result := inspection{Duration: info.Duration}
	for _, f := range info.Formats {
		if f.isPreview() {
			result.PreviewFormat = true
		}
		if !f.isAudio() {
			continue
		}
		result.AudioFormats++
		if f.isEncrypted() {
			result.EncryptedAudio++
		}
	}
	return result
}

func (i inspection) unplayableFor(flatDuration float64) string {
	if i.PreviewFormat || i.isPreviewDuration(flatDuration) {
		return unplayablePreview
	}
	if i.AudioFormats > 0 && i.EncryptedAudio == i.AudioFormats {
		return unplayableDRM
	}
	return ""
}

func (i inspection) isPreviewDuration(flatDuration float64) bool {
	return math.Abs(i.Duration-soundCloudPreviewDuration) <= previewDurationSlack &&
		flatDuration > soundCloudPreviewDuration+previewFlatMargin
}

type cachedInspection struct {
	result  inspection
	expires time.Time
}

type inspectionCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]cachedInspection
	order   []string
}

func newInspectionCache() *inspectionCache {
	return &inspectionCache{now: time.Now, entries: make(map[string]cachedInspection)}
}

func (c *inspectionCache) get(key string) (inspection, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.now().Before(entry.expires) {
		return inspection{}, false
	}
	return entry.result, true
}

func (c *inspectionCache) put(key string, result inspection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		c.order = append(c.order, key)
	}
	c.entries[key] = cachedInspection{result: result, expires: c.now().Add(inspectCacheTTL)}
	for len(c.order) > inspectCacheMax {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
}

func isSoundCloudURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "soundcloud.com" || strings.HasSuffix(host, ".soundcloud.com")
}

func (s *YtDlpAudioSearcher) MarkUnplayable(ctx context.Context, candidates []ports.AudioCandidate) {
	g := new(errgroup.Group)
	g.SetLimit(inspectConcurrency)
	for i := range candidates {
		if !isSoundCloudURL(candidates[i].URL) {
			continue
		}
		g.Go(func() error {
			candidates[i].Unplayable = s.unplayableReason(ctx, candidates[i])
			return nil
		})
	}
	_ = g.Wait()
}

func (s *YtDlpAudioSearcher) unplayableReason(ctx context.Context, c ports.AudioCandidate) string {
	result, err := s.inspectCached(ctx, c.URL)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.soundcloud_inspect_failed",
			"url", c.URL, "error", redact.LogError(err))
		return ""
	}
	return result.unplayableFor(c.Duration)
}

func (s *YtDlpAudioSearcher) inspectCached(ctx context.Context, trackURL string) (inspection, error) {
	if result, ok := s.inspections.get(trackURL); ok {
		return result, nil
	}
	result, err := s.inspect(ctx, trackURL)
	if err != nil {
		return inspection{}, err
	}
	s.inspections.put(trackURL, result)
	return result, nil
}

func (s *YtDlpAudioSearcher) runYtDlpInspect(ctx context.Context, trackURL string) (inspection, error) {
	inspectCtx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()

	cookieFile, cleanup, err := s.cookieJarCopy("acquisition-inspect-cookies-*.txt")
	if err != nil {
		return inspection{}, fmt.Errorf("yt-dlp inspect: copy cookie jar: %w", err)
	}
	defer cleanup()

	args := s.authFlags([]string{"-J", "--no-download", "--no-warnings", "--", trackURL}, cookieFile)
	stdout, stderr, err := execcmd.Run(inspectCtx, s.binary, args...)
	if err != nil {
		return inspection{}, fmt.Errorf("yt-dlp inspect: %w (stderr: %s)", err, stderr)
	}
	var info inspectedInfo
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		return inspection{}, fmt.Errorf("yt-dlp inspect: parse: %w", err)
	}
	return inspectionFromInfo(info), nil
}
