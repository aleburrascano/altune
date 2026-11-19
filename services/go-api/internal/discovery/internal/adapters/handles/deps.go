package handles

import (
	"altune/go-api/internal/discovery/internal/core/ports"
	"altune/go-api/internal/discovery/service/unreleased"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"expvar"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type Deps struct {
	Lifecycle       context.Context
	Background      *sync.WaitGroup
	Logger          *slog.Logger
	Transport       http.RoundTripper
	ProviderTimeout time.Duration
	ChartTimeout    time.Duration
	Pool            *pgxpool.Pool
	Redis           *goredis.Client
	CacheOps        *expvar.Map
	Activity        ports.ActivityFeed
	ClientEvents    *telemetry.ClientEvents
	IdentityStore   ports.IdentityStore
	Providers       ProviderSettings
	Search          SearchSettings
	Unreleased      UnreleasedSettings
}

type ProviderSettings struct {
	MusicBrainzUserAgent string
	DiscogsToken         string
	FanartTVAPIKey       string
	GeniusAccessToken    string
	TheAudioDBAPIKey     string
	GoogleSheetsAPIKey   string
	TrackerHubSheetID    string
}

type SearchSettings struct {
	Deadline            time.Duration
	TailDemotion        bool
	CrossKindProminence bool
	Exploration         bool
	ExplorationRate     float64
	BehavioralRanking   bool
	ProviderCacheTTL    time.Duration
}

type UnreleasedSettings struct {
	ServeFromCache bool
	Tuning         unreleased.Config
}

type requiredHandle struct {
	name    string
	missing bool
}

func (d Deps) requiredHandles() []requiredHandle {
	return []requiredHandle{
		{"Lifecycle", d.Lifecycle == nil},
		{"Background", d.Background == nil},
		{"Logger", d.Logger == nil},
		{"Transport", d.Transport == nil},
		{"CacheOps", d.CacheOps == nil},
		{"ProviderTimeout", d.ProviderTimeout <= 0},
		{"ChartTimeout", d.ChartTimeout <= 0},
	}
}

func (d Deps) MissingHandles() error {
	var missing []string
	for _, handle := range d.requiredHandles() {
		if handle.missing {
			missing = append(missing, handle.name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("discovery: deps are missing %s", strings.Join(missing, ", "))
}
