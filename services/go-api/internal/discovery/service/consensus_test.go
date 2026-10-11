package service

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"testing"
)

func consensusProvider(name string, albums ...string) ConsensusProvider {
	return ConsensusProvider{
		Name: name,
		Fetcher: func(_ context.Context, _ string) ([]domain.SearchResult, error) {
			out := make([]domain.SearchResult, len(albums))
			for i, title := range albums {
				out[i] = domain.SearchResult{Kind: domain.ResultKindAlbum, Title: title, Subtitle: "Artist"}
			}
			return out, nil
		},
	}
}

func TestFanOutConsensus_CollectsEveryProvider(t *testing.T) {
	providers := []ConsensusProvider{
		consensusProvider("a", "A1"),
		consensusProvider("b", "B1", "B2"),
	}
	out := FanOutConsensus(context.Background(), providers, func(ctx context.Context, p ConsensusProvider) int {
		albums, _ := p.Fetcher(ctx, "x")
		return len(albums)
	})
	if out["a"] != 1 || out["b"] != 2 {
		t.Errorf("collected = %v, want a=1 b=2", out)
	}
}

func TestFanOutConsensus_PanickingCollectIsContained(t *testing.T) {
	providers := []ConsensusProvider{{Name: "boom"}, {Name: "ok"}}
	out := FanOutConsensus(context.Background(), providers, func(_ context.Context, p ConsensusProvider) int {
		if p.Name == "boom" {
			panic("collect exploded")
		}
		return 7
	})

	if out["ok"] != 7 {
		t.Errorf("ok = %d, want 7", out["ok"])
	}
	if _, present := out["boom"]; present {
		t.Errorf("panicking provider should be absent, got %v", out["boom"])
	}
}
