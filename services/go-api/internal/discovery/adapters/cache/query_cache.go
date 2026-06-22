package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

const queryCacheTTL = 10 * time.Minute

type RedisQueryCache struct {
	client *goredis.Client
}

func NewRedisQueryCache(client *goredis.Client) *RedisQueryCache {
	return &RedisQueryCache{client: client}
}

func (c *RedisQueryCache) Get(ctx context.Context, provider domain.ProviderName, kindsCSV, queryHash string) ([]domain.SearchResult, time.Time, bool, error) {
	if c.client == nil {
		return nil, time.Time{}, false, nil
	}

	key := queryCacheKey(provider, kindsCSV, queryHash)
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, time.Time{}, false, nil
	}

	var entry queryCacheEntry
	if err := json.Unmarshal([]byte(val), &entry); err != nil {
		return nil, time.Time{}, false, nil
	}

	return entry.Results, entry.FetchedAt, true, nil
}

func (c *RedisQueryCache) Set(ctx context.Context, provider domain.ProviderName, kindsCSV, queryHash string, results []domain.SearchResult) error {
	if c.client == nil {
		return nil
	}

	entry := queryCacheEntry{
		Results:  results,
		FetchedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	key := queryCacheKey(provider, kindsCSV, queryHash)
	return c.client.Set(ctx, key, string(data), queryCacheTTL).Err()
}

func queryCacheKey(provider domain.ProviderName, kindsCSV, queryHash string) string {
	return fmt.Sprintf("discovery:v1:%s:%s:%s", provider.String(), kindsCSV, queryHash)
}

func QueryHash(queryNorm string) string {
	return hashKey("", queryNorm)
}

func KindsCSV(kinds map[domain.ResultKind]bool) string {
	var parts []string
	for k := range kinds {
		parts = append(parts, k.String())
	}
	return strings.Join(parts, ",")
}

type queryCacheEntry struct {
	Results   []domain.SearchResult `json:"results"`
	FetchedAt time.Time             `json:"fetched_at"`
}
