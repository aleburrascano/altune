package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestService_Execute_DoesNotLogQueryText guards issue #1097: search text is
// erasable via clear-history, so it must never be copied into stdout logs.
func TestService_Execute_DoesNotLogQueryText(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const raw = "zqxj private diagnosis clinic"
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{
		track("Clinic", "Someone", domain.ProviderDeezer, nil),
	}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker())
	runSearch(t, svc, raw)

	logged := buf.String()
	if !strings.Contains(logged, "search.v2.start") || !strings.Contains(logged, "search.v2.complete") {
		t.Fatalf("expected search lifecycle logs, got:\n%s", logged)
	}
	for _, form := range []string{raw, "diagnosis", "zqxj"} {
		if strings.Contains(logged, form) {
			t.Fatalf("log output contains search text %q:\n%s", form, logged)
		}
	}
}
