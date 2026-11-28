package download

import (
	"altune/go-api/internal/download/internal/adapters/savedlink"
	"altune/go-api/internal/download/internal/adapters/youtubemusic"
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/adapters/ytmsearch"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type (
	AudioSource    = ports.AudioSource
	AudioCandidate = ports.AudioCandidate
	FindRequest    = ports.FindRequest
	SourceName     = domain.SourceName
	SourceOrder    = ports.SourceOrder
)

const (
	YtMusicSourceName            = youtubemusic.SourceName
	YtDlpSourceName              = ytdlp.SourceName
	SavedLinkSourceName          = savedlink.SourceName
	YouTubeMusicSearchSourceName = ytmsearch.SourceName
)

type presentAudioStore struct{ ports.AudioWriter }

func testClient(timeout time.Duration, transport http.RoundTripper) *http.Client {
	return &http.Client{Timeout: timeout, Transport: transport}
}

func testDeps(cfg *config.Config, redis *goredis.Client, transport http.RoundTripper) Deps {
	return Deps{Config: cfg, Redis: redis, Transport: transport, NewClient: testClient, AudioStore: presentAudioStore{}}
}

func LiveTiers(ctx context.Context, cfg *config.Config, transport http.RoundTripper) (SourceOrder, ports.Verification, error) {
	return testDeps(cfg, nil, transport).sources(ctx)
}

func InnerTubeClient(ctx context.Context, cfg *config.Config, redis *goredis.Client) (*ytmsearch.Client, error) {
	return ytmsearch.ForClient(testDeps(cfg, redis, http.DefaultTransport).innerTubeClient(ctx))
}

func SourceCheckNames(searcher *ytdlp.YouTubeAudioSearcher, ytDlpAvailable bool, innerTube *ytmsearch.Client, ytMusicEnabled, ytDlpEnabled bool) string {
	var names []string
	probes := sourceProbes{ytDlp: searcher, ytDlpAvailable: ytDlpAvailable, innerTube: innerTube, ytMusicEnabled: ytMusicEnabled, ytDlpEnabled: ytDlpEnabled}
	for _, check := range sourceChecks(probes) {
		names = append(names, check.Name)
	}
	return strings.Join(names, ",")
}
