package main

// Deterministic-eval fixtures (requirements doc 2026-06-25 constant-free-discovery-ranking).
//
// The ranking eval is non-deterministic because it hits live providers: catalog
// drift and network jitter churn the failure set run to run, so a real ranking
// regression hides in the noise. Fixtures freeze the provider I/O: record every
// provider's raw HTTP responses once, then replay them through the REAL ranking
// pipeline (app.BuildSearchServiceWithTransport, rankingOnly) so the same wiring
// the user sees runs against frozen inputs.
//
// Record and replay both use ONE shared Service over a single recorder/replayer.
// One Service matters for size and speed: SoundCloud bootstraps its client_id
// once (a ~3MB JS asset) instead of re-fetching it per query, and YouTube Music's
// package-global HTTP client is set once — so recording runs concurrently. The
// Replayer matches each request by URL, so one combined set serves every query.

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

// fixtureFile is the recorded provider exchanges for the whole corpus, persisted
// as one JSON file. (loadAllFixtures still concatenates across files, so a sharded
// recording would also load — but record writes one "corpus.json".)
type fixtureFile struct {
	Label     string               `json:"label"`
	Exchanges []httptrace.Exchange `json:"exchanges"`
}

// saveExchanges writes the recorded exchanges to <dir>/<name>.json. Compact (not
// indented): the corpus file reaches gigabytes, where indentation only inflates
// size and the marshal memory spike.
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

// loadAllFixtures reads every *.json fixture in dir and concatenates their
// exchanges into one slice — the input to a single combined Replayer. Order
// across files does not matter: the Replayer matches by request identity, and
// repeated identical requests carry identical responses.
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

// dedupExchanges keeps one exchange per (method, URL, request body). Identical
// requests carry identical responses, so collapsing them is lossless — and it
// removes any duplicate client_id-bootstrap fetches a concurrent first burst of
// queries may have raced into before the SoundCloud adapter cached it.
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

// recordCorpus runs the whole corpus against live providers through ONE shared
// recording Service, then writes the deduped exchanges to a single fixture file.
// One Service lets SoundCloud bootstrap once and YouTube Music set its global
// once, so this can run at full concurrency. nil redis matches the replay Service
// so record and replay issue the same requests.
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
	// Recorder over the live (rate-limiting, retrying) transport so a bulk record
	// paces itself within each provider's limit instead of hammering them into
	// throttling — the captured responses are clean, not self-inflicted timeouts.
	rec := httptrace.NewRecorder(app.NewLiveTransport())
	svc := app.BuildSearchServiceWithTransport(cfg, pool, nil, nil, rec, true)
	searcher := searchAdapter{svc: svc}

	report := discoveryEval.RunLibraryEvalMode(ctx, ents, searcher, concurrency, topK, mode, progress)
	svc.WaitForBackground()

	exchanges := dedupExchanges(rec.Exchanges())
	if err := saveExchanges(dir, "corpus", exchanges); err != nil {
		return nil, err
	}
	return report, nil
}

// recordArtistCorpus is recordCorpus for the artist-intent driver: it issues the
// bare-artist-name queries through one shared recording Service to capture all
// provider HTTP exchanges, then writes the deduped fixture. The captured traffic
// is identical whatever the ranking flags — replay then A/Bs the ranker over it.
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
	svc := app.BuildSearchServiceWithTransport(cfg, pool, nil, nil, rec, true)
	searcher := searchAdapter{svc: svc}

	report := discoveryEval.RunArtistIntentEval(ctx, artists, searcher, concurrency, topK, corpus, progress)
	svc.WaitForBackground()

	exchanges := dedupExchanges(rec.Exchanges())
	if err := saveExchanges(dir, "corpus", exchanges); err != nil {
		return nil, err
	}
	return report, nil
}

// buildReplaySearcher loads every fixture into one combined Replayer and builds a
// single Service over it: the deterministic, offline searcher. Redis is left nil
// so cache state cannot add variance — the frozen provider responses are the only
// inputs that vary between ranking changes.
func buildReplaySearcher(cfg *config.Config, pool *pgxpool.Pool, dir string) (searchAdapter, error) {
	exchanges, err := loadAllFixtures(dir)
	if err != nil {
		return searchAdapter{}, err
	}
	replayer := httptrace.NewReplayer(exchanges)
	svc := app.BuildSearchServiceWithTransport(cfg, pool, nil, nil, replayer, true)
	return searchAdapter{svc: svc}, nil
}
