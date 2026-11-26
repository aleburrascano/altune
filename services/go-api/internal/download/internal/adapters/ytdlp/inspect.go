package ytdlp

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/childprocess"
	"altune/go-api/internal/shared/parallel"
	"altune/go-api/internal/shared/redact"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"time"
)

const (
	inspectTimeout     = 20 * time.Second
	inspectConcurrency = 2
	inspectCacheTTL    = 24 * time.Hour
	inspectCacheMax    = 500

	previewDurationSlack = 0.5
	previewFlatMargin    = 5.0
)

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
		return ports.UnplayablePreview
	}
	if i.AudioFormats > 0 && i.EncryptedAudio == i.AudioFormats {
		return ports.UnplayableDRM
	}
	return ""
}

func isSoundCloudPreviewLength(seconds float64) bool {
	return math.Abs(seconds-soundCloudPreviewDuration) <= previewDurationSlack
}

func (i inspection) isPreviewDuration(flatDuration float64) bool {
	return isSoundCloudPreviewLength(i.Duration) &&
		flatDuration > soundCloudPreviewDuration+previewFlatMargin
}

func isSoundCloudURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "soundcloud.com" || strings.HasSuffix(host, ".soundcloud.com")
}

func (s *YouTubeAudioSearcher) MarkUnplayable(ctx context.Context, candidates []ports.AudioCandidate) {
	g := parallel.New("acquisition.soundcloud_inspect_panic", inspectConcurrency)
	for i := range candidates {
		if !isSoundCloudURL(candidates[i].URL) {
			continue
		}
		g.Go(ctx, func() {
			candidates[i].Unplayable = s.unplayableReason(ctx, candidates[i])
		}, "url", candidates[i].URL)
	}
	_ = g.Wait()
}

func (s *YouTubeAudioSearcher) unplayableReason(ctx context.Context, c ports.AudioCandidate) string {
	result, err := s.inspectCached(ctx, c.URL)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.soundcloud_inspect_failed",
			"url", c.URL, "error", redact.LogError(err))
		return ""
	}
	return result.unplayableFor(c.Duration)
}

func (s *YouTubeAudioSearcher) inspectCached(ctx context.Context, trackURL string) (inspection, error) {
	if result, ok := s.inspections.Get(trackURL); ok {
		return result, nil
	}
	result, err := s.runYtDlpInspect(ctx, trackURL)
	if err != nil {
		return inspection{}, err
	}
	s.inspections.Put(trackURL, result)
	return result, nil
}

func (s *YouTubeAudioSearcher) runYtDlpInspect(ctx context.Context, trackURL string) (inspection, error) {
	inspectCtx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()

	cookies, err := s.leaseCookieJar(ctx, "inspect", trackURL)
	if err != nil {
		return inspection{}, fmt.Errorf("yt-dlp inspect: copy cookie jar: %w", err)
	}
	defer cookies.cleanup()

	args := s.authFlags([]string{"-J", "--no-download", "--no-warnings", "--", trackURL}, cookies.path)
	stdout, stderr, err := childprocess.Run(inspectCtx, ytDlpBinary, args...)
	cookies.settle(ctx, err, stderr)
	if err != nil {
		return inspection{}, fmt.Errorf("yt-dlp inspect: %w (stderr: %s)", err, embeddedStderr(stderr))
	}
	var info inspectedInfo
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		return inspection{}, fmt.Errorf("yt-dlp inspect: parse: %w", err)
	}
	return inspectionFromInfo(info), nil
}
