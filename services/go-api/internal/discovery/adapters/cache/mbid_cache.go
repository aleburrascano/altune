package cache

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	mbidPositiveTTL = 30 * 24 * time.Hour
	mbidNegativeTTL = 24 * time.Hour
	mbidNoneSentinel = "__none__"
)

type CachedMbidResolver struct {
	client *goredis.Client
	inner  MbidResolverFunc
}

type MbidResolverFunc func(ctx context.Context, url string) (string, error)

func NewCachedMbidResolver(client *goredis.Client, inner MbidResolverFunc) *CachedMbidResolver {
	return &CachedMbidResolver{client: client, inner: inner}
}

func (r *CachedMbidResolver) Resolve(ctx context.Context, url string) (string, error) {
	if r.client != nil {
		key := mbidCacheKey(url)
		val, err := r.client.Get(ctx, key).Result()
		if err == nil {
			if val == mbidNoneSentinel {
				return "", nil
			}
			return val, nil
		}
	}

	mbid, err := r.inner(ctx, url)
	if err != nil {
		return "", err
	}

	if r.client != nil {
		key := mbidCacheKey(url)
		if mbid != "" {
			_ = r.client.Set(ctx, key, mbid, mbidPositiveTTL).Err()
		} else {
			_ = r.client.Set(ctx, key, mbidNoneSentinel, mbidNegativeTTL).Err()
		}
	}

	return mbid, nil
}

func mbidCacheKey(url string) string {
	return hashKey("discovery:mbid:v1:", url)
}
