package downloadsetup

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/config"
	"context"
	"fmt"
	"net/http"

	goredis "github.com/redis/go-redis/v9"
)

type SourceOrder = download.SourceOrder

func EnabledSources(ctx context.Context, cfg *config.Config, redis *goredis.Client, transport http.RoundTripper) (SourceOrder, download.Verification, error) {
	tiers, tools, err := download.EnabledSources(ctx, sourceDeps(cfg, redis, transport))
	if err != nil {
		return SourceOrder{}, download.Verification{}, fmt.Errorf("enable audio sources: %w", err)
	}
	return tiers, tools, nil
}

func YtDlpSearcher(cfg *config.Config) *download.YouTubeAudioSearcher {
	return download.YtDlpSearcher(cfg)
}

func CanaryYTMClient(ctx context.Context, cfg *config.Config, redis *goredis.Client, transport http.RoundTripper) (*download.YouTubeMusicSearchClient, error) {
	client, err := download.CanaryYTMClient(ctx, sourceDeps(cfg, redis, transport))
	if err != nil {
		return nil, fmt.Errorf("canary ytmusic client: %w", err)
	}
	return client, nil
}

func sourceDeps(cfg *config.Config, redis *goredis.Client, transport http.RoundTripper) download.SourceDeps {
	return download.SourceDeps{Config: cfg, Redis: redis, Transport: transport, NewClient: newClient}
}
