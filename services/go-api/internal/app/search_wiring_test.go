package app

import (
	"altune/go-api/internal/shared/config"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	discoveryService "altune/go-api/internal/discovery/service"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// TestSearchServiceOptionSets pins the ordered option set each named search
// constructor composes, captured from the former rankingOnly=false/true paths,
// so the split into named constructors stays behavior-preserving.
func TestSearchServiceOptionSets(t *testing.T) {
	cfg := &config.Config{
		ExplorationEnabled:         true,
		TailDemotionEnabled:        true,
		CrossKindProminenceEnabled: true,
		IdentityVerifyOnPersist:    true,
		BehavioralRankingEnabled:   true,
		MusicBrainzUserAgent:       "altune-test",
	}
	pool := &pgxpool.Pool{}
	redisClient := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = redisClient.Close() })
	eventStore := discoveryPersistence.NewPgxEventStore(pool)
	w := newSearchWiring(cfg, nil)

	tests := []struct {
		name string
		opts []discoveryService.Option
		want []string
	}{
		{
			name: "content with redis",
			opts: contentServiceOptions(w, cfg, pool, redisClient, eventStore, nil),
			want: []string{
				"WithHistoryRepository", "WithTailDemotion", "WithCrossKindProminence", "WithExploration",
				"WithArtworkResolver", "WithFindRelatedService", "WithFavorites", "WithIdentityStore",
				"WithIdentityVerifier", "WithResultCache", "WithHeldSlateCache", "WithArtworkCache",
				"WithIdentityBridge", "WithMBIDIndex", "WithVocabularyStore", "WithEventStore",
				"WithBehavioralRanking", "WithAlbumValidator",
			},
		},
		{
			name: "content without redis",
			opts: contentServiceOptions(w, cfg, pool, nil, eventStore, nil),
			want: []string{
				"WithHistoryRepository", "WithTailDemotion", "WithCrossKindProminence", "WithExploration",
				"WithArtworkResolver", "WithFindRelatedService", "WithFavorites", "WithIdentityStore",
				"WithIdentityVerifier", "WithEventStore", "WithBehavioralRanking", "WithAlbumValidator",
			},
		},
		{
			name: "ranking only with redis",
			opts: rankingOnlyServiceOptions(w, cfg, pool, redisClient),
			want: []string{
				"WithHistoryRepository", "WithTailDemotion", "WithCrossKindProminence",
				"WithArtworkCache", "WithIdentityBridge", "WithMBIDIndex", "WithVocabularyStore",
				"WithAlbumValidator",
			},
		},
		{
			name: "ranking only without redis",
			opts: rankingOnlyServiceOptions(w, cfg, pool, nil),
			want: []string{
				"WithHistoryRepository", "WithTailDemotion", "WithCrossKindProminence", "WithAlbumValidator",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := optionNames(tt.opts); !slices.Equal(got, tt.want) {
				t.Errorf("options =\n  %v\nwant\n  %v", got, tt.want)
			}
		})
	}
}

// optionNames resolves each option closure to the discovery With* constructor
// that produced it. The runtime name depends on inlining (plain builds yield
// "service.WithX.func1", coverage builds "app.caller.WithX.func2"), so take the
// last dot-separated segment that names a With* constructor.
func optionNames(opts []discoveryService.Option) []string {
	names := make([]string, 0, len(opts))
	for _, opt := range opts {
		full := runtime.FuncForPC(reflect.ValueOf(opt).Pointer()).Name()
		names = append(names, lastWithSegment(full))
	}
	return names
}

func lastWithSegment(funcName string) string {
	segments := strings.Split(funcName, ".")
	for i := len(segments) - 1; i >= 0; i-- {
		if strings.HasPrefix(segments[i], "With") {
			return segments[i]
		}
	}
	return funcName
}
