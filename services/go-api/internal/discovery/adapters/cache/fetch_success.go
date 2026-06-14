package cache

import (
	"context"
	"fmt"
	"strconv"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

const fetchSuccessWindowSize = 10

type RedisFetchSuccessStore struct {
	client *goredis.Client
}

func NewRedisFetchSuccessStore(client *goredis.Client) *RedisFetchSuccessStore {
	return &RedisFetchSuccessStore{client: client}
}

func (s *RedisFetchSuccessStore) Record(ctx context.Context, provider domain.ProviderName, success bool) error {
	if s.client == nil {
		return nil
	}

	key := fetchSuccessKey(provider)
	val := "0"
	if success {
		val = "1"
	}

	pipe := s.client.Pipeline()
	pipe.LPush(ctx, key, val)
	pipe.LTrim(ctx, key, 0, fetchSuccessWindowSize-1)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisFetchSuccessStore) GetRate(ctx context.Context, provider domain.ProviderName) (float64, error) {
	if s.client == nil {
		return 1.0, nil
	}

	key := fetchSuccessKey(provider)
	vals, err := s.client.LRange(ctx, key, 0, fetchSuccessWindowSize-1).Result()
	if err != nil || len(vals) == 0 {
		return 1.0, nil
	}

	successes := 0
	for _, v := range vals {
		n, _ := strconv.Atoi(v)
		successes += n
	}

	return float64(successes) / float64(len(vals)), nil
}

func fetchSuccessKey(provider domain.ProviderName) string {
	return fmt.Sprintf("discovery:fetch_success:v1:%s", provider.String())
}
