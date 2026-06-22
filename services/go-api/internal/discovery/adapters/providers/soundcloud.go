package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
)

func (a *SoundCloudAdapter) SearchTimeout() time.Duration { return 5 * time.Second }

// SoundCloudAdapter uses yt-dlp CLI subprocess for SoundCloud search
// since the Python library API is not available in Go.
type SoundCloudAdapter struct{}

func NewSoundCloudAdapter() *SoundCloudAdapter {
	return &SoundCloudAdapter{}
}

func (a *SoundCloudAdapter) Name() domain.ProviderName { return domain.ProviderSoundCloud }

func (a *SoundCloudAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	}
}

func (a *SoundCloudAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	if !kinds[domain.ResultKindTrack] {
		return nil, nil
	}

	cmd := exec.CommandContext(ctx, "yt-dlp",
		"--dump-json",
		"--flat-playlist",
		"--no-download",
		fmt.Sprintf("scsearch5:%s", query),
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("yt-dlp soundcloud search: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("yt-dlp soundcloud search: %w", err)
	}

	var results []domain.SearchResult
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry struct {
			Title         string  `json:"title"`
			Uploader      string  `json:"uploader"`
			Duration      float64 `json:"duration"`
			WebpageURL    string  `json:"webpage_url"`
			ID            string  `json:"id"`
			Thumbnail     string  `json:"thumbnail"`
			PlaybackCount int64   `json:"playback_count"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		extras := map[string]any{
			"duration": entry.Duration,
		}
		if entry.PlaybackCount > 0 {
			extras["playback_count"] = entry.PlaybackCount
		}

		results = append(results, domain.NewProviderResult(domain.ResultKindTrack, entry.Title, entry.Uploader, entry.Thumbnail,
			domain.SourceRef{Provider: domain.ProviderSoundCloud, ExternalID: entry.ID, URL: entry.WebpageURL},
			extras))
	}

	return results, nil
}
