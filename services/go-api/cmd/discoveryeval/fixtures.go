package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"altune/go-api/internal/app"
	discoveryEval "altune/go-api/internal/discovery/service/eval"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/httptrace"

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
	ents []discoveryEval.LibraryEntity,
	mode discoveryEval.QueryMode,
	concurrency, topK int,
	progress func(done, total int),
) (discoveryEval.HarnessReport, error) {
	rec := httptrace.NewRecorder(app.NewLiveTransport())
	svc := app.BuildSearchServiceWithTransport(cfg, pool, nil, nil, rec, nil, true)
	searcher := searchAdapter{svc: svc}

	report := discoveryEval.RunLibraryEvalMode(ctx, ents, searcher, concurrency, topK, mode, progress)
	svc.WaitForBackground()

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
) (discoveryEval.HarnessReport, error) {
	rec := httptrace.NewRecorder(app.NewLiveTransport())
	svc := app.BuildSearchServiceWithTransport(cfg, pool, nil, nil, rec, nil, true)
	searcher := searchAdapter{svc: svc}

	report := discoveryEval.RunArtistIntentEval(ctx, artists, searcher, concurrency, topK, corpus, progress)
	svc.WaitForBackground()

	exchanges := dedupExchanges(rec.Exchanges())
	if err := saveExchanges(dir, "corpus", exchanges); err != nil {
		return nil, err
	}
	return report, nil
}

func buildReplaySearcher(cfg *config.Config, pool *pgxpool.Pool, dir string) (searchAdapter, error) {
	exchanges, err := loadAllFixtures(dir)
	if err != nil {
		return searchAdapter{}, err
	}
	replayer := httptrace.NewReplayer(exchanges)
	svc := app.BuildSearchServiceWithTransport(cfg, pool, nil, nil, replayer, nil, true)
	return searchAdapter{svc: svc}, nil
}
