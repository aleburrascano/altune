package app

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"strings"
	"testing"

	discoveryPorts "altune/go-api/internal/discovery/ports"
)

type errSearchProvider struct {
	name domain.ProviderName
	err  error
}

func (f errSearchProvider) Name() domain.ProviderName { return f.name }

func (f errSearchProvider) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return nil, f.err
}

func (f errSearchProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindTrack: true}
}

// TestFanOutRerun_redactsSecretInProviderError reproduces the leak: a provider's
// network/DNS/timeout error embeds the full outbound URL (with the live api key)
// and fanOutRerun copied err.Error() verbatim into ProviderTrace.Err, exposing
// the key on the /admin/rerun JSON response.
func TestFanOutRerun_redactsSecretInProviderError(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	provs := []discoveryPorts.SearchProvider{
		errSearchProvider{
			name: domain.ProviderLastFM,
			err:  fmt.Errorf(`Get "https://ws.audioscrobbler.com/2.0/?method=x&api_key=%s&format=json": dial tcp: i/o timeout`, secret),
		},
	}

	_, traces := fanOutRerun(context.Background(), provs, "q", map[domain.ResultKind]bool{domain.ResultKindTrack: true})

	if strings.Contains(traces[0].Err, secret) {
		t.Fatalf("provider error leaked raw api key into the rerun trace: %q", traces[0].Err)
	}
	if !strings.Contains(traces[0].Err, "REDACTED") {
		t.Errorf("expected the api_key value to be redacted, got %q", traces[0].Err)
	}
}
