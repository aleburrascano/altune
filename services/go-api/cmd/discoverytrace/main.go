// Command discoverytrace runs a discovery search with a recording HTTP transport
// and dumps the EXACT payload at each stage — raw provider JSON (before parsing),
// the mapped []SearchResult, and (in pipeline mode) the merge → rank → reshape
// stages. It is pipeline observability: seeing the data
// mutate stage by stage, not just that a call happened.
//
//	go run ./cmd/discoverytrace -query "Ken Carson Olympics"                 # full pipeline
//	go run ./cmd/discoverytrace -mode single -provider soundcloud -query "…" # one provider
//
// Offline + read-only: reuses the exported Merge/Rank/reshape (no prod-path
// changes). stampIdentities (the xref bridge) and artwork enrichment are skipped
// — they don't reorder results — so bridge-only merges won't appear here.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/httptrace"
	"altune/go-api/internal/shared/textnorm"
)

func main() {
	query := flag.String("query", "", "search query (required)")
	mode := flag.String("mode", "pipeline", "pipeline (all providers → merge → rank) | single")
	provider := flag.String("provider", "soundcloud", "provider for -mode single")
	kindsFlag := flag.String("kinds", "track,album,artist", "comma-separated kinds")
	out := flag.String("out", "./tmp/trace", "directory for the raw + per-stage JSON dumps")
	flag.Parse()

	if *query == "" {
		fmt.Fprintln(os.Stderr, "error: -query is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir %s: %v\n", *out, err)
		os.Exit(1)
	}

	kinds := parseKinds(*kindsFlag)
	rec := httptrace.NewRecorder(nil)
	client := &http.Client{Timeout: 20 * time.Second, Transport: rec}

	if *mode == "single" {
		runSingle(*provider, client, rec, *query, kinds, *out)
		return
	}
	runPipeline(rec, *query, kinds, *out)
}

// runPipeline runs every search provider, then the real exported decision core
// (Merge → RankWith → Reshape, flag-gated stages included), dumping each stage.
func runPipeline(rec *httptrace.Recorder, query string, kinds map[domain.ResultKind]bool, out string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: config.Load: %v\n", err)
		os.Exit(1)
	}
	// The production provider set over the recording transport — no hand-mirror
	// (a local copy drifted once: SoundCloud lost its yt-dlp fallback).
	provs := app.BuildDiscoveryProviders(cfg, rec)
	qNorm := textnorm.NormalizeForMatch(service.CleanQuery(query))

	fmt.Printf("discoverytrace pipeline: query=%q qnorm=%q kinds=%v providers=%d\n\n", query, qNorm, flagList(kinds), len(provs))

	// Stage 1 — fan out to every provider (concurrent), capturing raw + mapped.
	perProvider := make([][]domain.SearchResult, len(provs))
	names := make([]string, len(provs))
	var wg sync.WaitGroup
	for i, p := range provs {
		names[i] = p.Name().String()
		wg.Add(1)
		go func(i int, p ports.SearchProvider) {
			defer wg.Done()
			res, err := p.Search(context.Background(), query, kinds)
			if err != nil {
				fmt.Printf("  ! %s: %v\n", p.Name(), err)
			}
			perProvider[i] = res
		}(i, p)
	}
	wg.Wait()

	fmt.Println("=== STAGE 1: per-provider (raw JSON dumped to files) ===")
	for i, name := range names {
		fmt.Printf("  %-12s %d results\n", name, len(perProvider[i]))
	}
	dumpExchanges(rec, out)
	writeJSON(out, "01-perprovider.json", labeledPerProvider(names, perProvider))

	// Stage 2 — Merge.
	entities := service.Merge(perProvider)
	fmt.Printf("\n=== STAGE 2: merged — %d entities (from %d raw) ===\n", len(entities), countAll(perProvider))
	writeJSON(out, "02-merged.json", entities)

	// Stage 3 — Rank, with the same flag-gated experiment stages production
	// applies (behavioral is a live-Service snapshot, unavailable offline → nil).
	ranked := service.RankWith(entities, qNorm, service.RankOptions{
		TailDemotion:        cfg.TailDemotionEnabled,
		CrossKindProminence: cfg.CrossKindProminenceEnabled,
	})
	fmt.Printf("\n=== STAGE 3: ranked — %d results (top 20) ===\n", len(ranked))
	printRanked(ranked, 20)
	writeJSON(out, "03-ranked.json", ranked)

	// Stage 4 — reshape (diversity + collapse).
	final := service.Reshape(ranked)
	fmt.Printf("\n=== STAGE 4: final (diversity + collapse) — %d results (top 20) ===\n", len(final))
	printRanked(final, 20)
	writeJSON(out, "04-final.json", final)

	fmt.Printf("\nwrote raw exchanges + 01-04 stage JSON to %s\n", out)
}

func runSingle(name string, client *http.Client, rec *httptrace.Recorder, query string, kinds map[domain.ResultKind]bool, out string) {
	var (
		results []domain.SearchResult
		err     error
	)
	switch name {
	case "soundcloud":
		results, err = providers.NewSoundCloudAPIAdapter(client, nil).Search(context.Background(), query, kinds)
	default:
		fmt.Fprintf(os.Stderr, "error: -mode single supports provider=soundcloud (got %q)\n", name)
		os.Exit(2)
	}

	fmt.Printf("discoverytrace single: provider=%s query=%q\n\n", name, query)
	dumpExchanges(rec, out)
	fmt.Printf("\n=== MAPPED RESULTS (%d) ===\n", len(results))
	for i, r := range results {
		fmt.Printf("[%2d] %-6s %-40s — %-20s%s\n", i, r.Kind.String(), shorten(r.Title, 40), shorten(r.Subtitle, 20), extraHint(r))
	}
	writeJSON(out, "mapped.json", results)
	if err != nil {
		fmt.Printf("\nsearch error: %v\n", err)
	}
	fmt.Printf("\nwrote raw exchanges + mapped.json to %s\n", out)
}

// printRanked shows each result's final rank position, kind, title/subtitle,
// source count, and providers — the order is the signal (which result landed in
// the top-K). The per-result relevance breakdown was boost-specific debugging and
// went away with the boost; ranking is now the parameter-free measure in
// service/rank_relevance.go.
func printRanked(results []domain.SearchResult, limit int) {
	for i, r := range results {
		if i >= limit {
			fmt.Printf("  … +%d more\n", len(results)-limit)
			break
		}
		fmt.Printf("[%2d] %-6s %-40s — %-18s src=%d [%s]\n",
			i, r.Kind.String(), shorten(r.Title, 40), shorten(r.Subtitle, 18), len(r.Sources), providerList(r))
	}
}

func providerList(r domain.SearchResult) string {
	var ps []string
	for _, s := range r.Sources {
		ps = append(ps, s.Provider.String())
	}
	sort.Strings(ps)
	return strings.Join(ps, ",")
}

func dumpExchanges(rec *httptrace.Recorder, out string) {
	exchanges := rec.Exchanges()
	fmt.Printf("raw HTTP exchanges: %d (dumped to %s)\n", len(exchanges), out)
	for i, ex := range exchanges {
		status := fmt.Sprintf("%d", ex.Status)
		if ex.Err != "" {
			status = "ERR:" + ex.Err
		}
		fmt.Printf("  [%d] %s %s -> %s (%d bytes)\n", i, ex.Method, shorten(ex.URL, 90), status, len(ex.RespBody))
		_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("exchange-%02d-%s.json", i, hostSlug(ex.URL))), indentJSON([]byte(ex.RespBody)), 0o644)
	}
}

type labeledGroup struct {
	Provider string                `json:"provider"`
	Results  []domain.SearchResult `json:"results"`
}

func labeledPerProvider(names []string, perProvider [][]domain.SearchResult) []labeledGroup {
	out := make([]labeledGroup, len(names))
	for i, name := range names {
		out[i] = labeledGroup{Provider: name, Results: perProvider[i]}
	}
	return out
}

func countAll(perProvider [][]domain.SearchResult) int {
	n := 0
	for _, g := range perProvider {
		n += len(g)
	}
	return n
}

func writeJSON(dir, name string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, name), b, 0o644)
}

func parseKinds(s string) map[domain.ResultKind]bool {
	out := map[domain.ResultKind]bool{}
	for _, part := range strings.Split(s, ",") {
		switch strings.TrimSpace(part) {
		case "track":
			out[domain.ResultKindTrack] = true
		case "album":
			out[domain.ResultKindAlbum] = true
		case "artist":
			out[domain.ResultKindArtist] = true
		}
	}
	return out
}

func flagList(kinds map[domain.ResultKind]bool) []string {
	var out []string
	for k := range kinds {
		out = append(out, k.String())
	}
	sort.Strings(out)
	return out
}

func extraHint(r domain.SearchResult) string {
	keys := []string{"playback_count", "likes", "isrc", "genre"}
	var parts []string
	for _, k := range keys {
		if v, ok := r.Extras[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, " ")
}

func indentJSON(raw []byte) []byte {
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") == nil {
		return buf.Bytes()
	}
	return raw
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func hostSlug(rawURL string) string {
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return strings.NewReplacer(".", "_", ":", "_").Replace(s)
}
