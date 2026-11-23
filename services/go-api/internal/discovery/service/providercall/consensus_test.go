package providercall_test

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/providercall"
	"context"
	"testing"
)

func consensusProvider(name string, albums ...string) providercall.ConsensusProvider {
	return providercall.ConsensusProvider{
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
	providers := []providercall.ConsensusProvider{
		consensusProvider("a", "A1"),
		consensusProvider("b", "B1", "B2"),
	}
	out := providercall.FanOutConsensus(context.Background(), providers, func(ctx context.Context, p providercall.ConsensusProvider) int {
		albums, _ := p.Fetcher(ctx, "x")
		return len(albums)
	})
	if out["a"] != 1 || out["b"] != 2 {
		t.Errorf("collected = %v, want a=1 b=2", out)
	}
}

func TestFanOutConsensus_PanickingCollectIsContained(t *testing.T) {
	providers := []providercall.ConsensusProvider{{Name: "boom"}, {Name: "ok"}}
	out := providercall.FanOutConsensus(context.Background(), providers, func(_ context.Context, p providercall.ConsensusProvider) int {
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
