package cache

import (
	"context"
	"fmt"
	"crypto/sha256"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	popularityPositiveTTL = 7 * 24 * time.Hour
	popularityNegativeTTL = 2 * time.Hour
	popularityNoneSentinel = "__none__"
)

type PopularityResolverFunc func(ctx context.Context, title, artist string) (float64, error)

type CachedPopularityResolver struct {
	client *goredis.Client
	inner  PopularityResolverFunc
}

func NewCachedPopularityResolver(client *goredis.Client, inner PopularityResolverFunc) *CachedPopularityResolver {
	return &CachedPopularityResolver{client: client, inner: inner}
}

func (r *CachedPopularityResolver) GetPopularity(ctx context.Context, title, artist string) (float64, bool, error) {
	if r.client != nil {
		key := popularityCacheKey(title, artist)
		val, err := r.client.Get(ctx, key).Result()
		if err == nil {
			if val == popularityNoneSentinel {
				return 0, true, nil
			}
			f, err := strconv.ParseFloat(val, 64)
			if err == nil {
				return f, true, nil
			}
		}
	}

	pop, err := r.inner(ctx, title, artist)
	if err != nil {
		return 0, false, nil
	}

	if r.client != nil {
		key := popularityCacheKey(title, artist)
		if pop > 0 {
			_ = r.client.Set(ctx, key, strconv.FormatFloat(pop, 'f', -1, 64), popularityPositiveTTL).Err()
		} else {
			_ = r.client.Set(ctx, key, popularityNoneSentinel, popularityNegativeTTL).Err()
		}
	}

	return pop, pop > 0, nil
}

func popularityCacheKey(title, artist string) string {
	input := fmt.Sprintf("%s|%s", title, artist)
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("discovery:popularity:v1:%x", h[:16])
}
