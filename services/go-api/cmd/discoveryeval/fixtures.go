package main

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/egress"
	"altune/go-api/internal/shared/httptrace"
	"altune/go-api/internal/shared/runloop"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type fixtureFile struct {
	Label     string               `json:"label"`
	Exchanges []httptrace.Exchange `json:"exchanges"`
}

func saveExchanges(dir, name string, exchanges []httptrace.Exchange) error {
	data, err := json.Marshal(fixtureFile{Label: name, Exchanges: exchanges})
	if err != nil {
		return fmt.Errorf("marshal fixtures: %w", err)
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write fixtures %s: %w", path, err)
	}
	return nil
}

func loadAllFixtures(dir string) ([]httptrace.Exchange, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read fixtures dir %s: %w", dir, err)
	}
	var all []httptrace.Exchange
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read fixture %s: %w", e.Name(), err)
		}
		var fx fixtureFile
		if err := json.Unmarshal(data, &fx); err != nil {
			return nil, fmt.Errorf("parse fixture %s: %w", e.Name(), err)
		}
		all = append(all, fx.Exchanges...)
	}
	return all, nil
}

func dedupExchanges(in []httptrace.Exchange) []httptrace.Exchange {
	seen := make(map[string]struct{}, len(in))
	out := make([]httptrace.Exchange, 0, len(in))
	for _, e := range in {
		k := e.Method + "\n" + e.URL + "\n" + e.ReqBody
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, e)
	}
	return out
}

func recordCorpus(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	dir string,
	ents []discovery.LibraryEntity,
	mode discovery.QueryMode,
	concurrency, topK int,
	progress func(done, total int),
) (discovery.HarnessReport, error) {
	rec := httptrace.NewRecorder(egress.NewLiveTransport())
	svc, flush, err := rankingOnlySearchService(ctx, cfg, pool, rec)
	if err != nil {
		return nil, err
	}
	searcher := searchAdapter{svc: svc}

	report := discovery.RunLibraryEvalMode(ctx, ents, searcher, concurrency, topK, mode, progress)
	flush()

	exchanges := dedupExchanges(rec.Exchanges())
	if err := saveExchanges(dir, "corpus", exchanges); err != nil {
		return nil, err
	}
	return report, nil
}

func recordArtistCorpus(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	dir string,
	artists []string,
	corpus string,
	concurrency, topK int,
	progress func(done, total int),
) (discovery.HarnessReport, error) {
	rec := httptrace.NewRecorder(egress.NewLiveTransport())
	svc, flush, err := rankingOnlySearchService(ctx, cfg, pool, rec)
	if err != nil {
		return nil, err
	}
	searcher := searchAdapter{svc: svc}

	report := discovery.RunArtistIntentEval(ctx, artists, searcher, concurrency, topK, corpus, progress)
	flush()

	exchanges := dedupExchanges(rec.Exchanges())
	if err := saveExchanges(dir, "corpus", exchanges); err != nil {
		return nil, err
	}
	return report, nil
}

func buildReplaySearcher(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, dir string) (searchAdapter, error) {
	exchanges, err := loadAllFixtures(dir)
	if err != nil {
		return searchAdapter{}, err
	}
	svc, _, err := rankingOnlySearchService(ctx, cfg, pool, httptrace.NewReplayer(exchanges))
	if err != nil {
		return searchAdapter{}, err
	}
	return searchAdapter{svc: svc}, nil
}

func rankingOnlySearchService(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, transport http.RoundTripper) (*discovery.SearchService, func(), error) {
	bg, err := newEvalTracker()
	if err != nil {
		return nil, nil, err
	}
	disc, err := app.NewEvalDiscovery(ctx, cfg, app.EvalHandles{Pool: pool, Transport: transport, Tracker: bg})
	if err != nil {
		return nil, nil, err
	}
	return disc.RankingOnlySearch(), bg.Wait, nil
}

const evalBackgroundMaxInFlight = 64

func newEvalTracker() (*runloop.Tracker, error) {
	return runloop.NewTracker(runloop.TrackerLimits{MaxInFlight: evalBackgroundMaxInFlight})
}
