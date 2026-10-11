package main

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/textnorm"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	discoveryEval "altune/go-api/internal/discovery/service/eval"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

//go:embed detail_goldens.json
var detailGoldensJSON []byte

func runDetail(ctx context.Context, cfg *config.Config, opts options) error {
	var goldens []discoveryEval.DetailGolden
	if err := json.Unmarshal(detailGoldensJSON, &goldens); err != nil {
		return fmt.Errorf("parse detail goldens: %w", err)
	}

	svc := app.BuildArtistContentService(cfg, nil, seedStore(goldens))
	adapter := detailServiceAdapter{svc: svc}

	once := func() (discoveryEval.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "detail-eval over %d golden artists (live providers)...\n", len(goldens))
		return discoveryEval.RunDetailEval(ctx, goldens, adapter), nil
	}
	human := func(r discoveryEval.HarnessReport) string { return renderDetail(r.(discoveryEval.DetailReport)) }
	return runHarness("detail", once, human, opts)
}

var _ discoveryPorts.IdentityStore = (*seededIdentityStore)(nil)

type identityRow struct {
	mbid string
	xref map[domain.ProviderKey]string
}

type seededIdentityStore struct {
	entries map[string]identityRow
}

func seedStore(goldens []discoveryEval.DetailGolden) *seededIdentityStore {
	s := &seededIdentityStore{entries: make(map[string]identityRow, len(goldens))}
	for _, g := range goldens {
		s.entries[g.SeedProvider+"|"+g.SeedID] = identityRow{mbid: g.MBID, xref: g.Identity}
	}
	return s
}

func (s *seededIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[domain.ProviderKey]string) error {
	return nil
}

func (s *seededIdentityStore) Invalidate(context.Context, domain.ResultKind, domain.ProviderKey, string) error {
	return nil
}

func (s *seededIdentityStore) LookupByProviderID(_ context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (string, map[domain.ProviderKey]string, bool) {
	if kind != domain.ResultKindArtist {
		return "", nil, false
	}
	row, ok := s.entries[provider.String()+"|"+externalID]
	if !ok {
		return "", nil, false
	}
	return row.mbid, row.xref, true
}

type detailServiceAdapter struct {
	svc *discoveryService.GetArtistContentService
}

func (a detailServiceAdapter) Albums(ctx context.Context, seedProvider, seedID, name string) []discoveryEval.DetailItem {
	return a.fetch(ctx, seedProvider, seedID, name, true)
}

func (a detailServiceAdapter) TopTracks(ctx context.Context, seedProvider, seedID, name string) []discoveryEval.DetailItem {
	return a.fetch(ctx, seedProvider, seedID, name, false)
}

func (a detailServiceAdapter) fetch(ctx context.Context, seedProvider, seedID, name string, albums bool) []discoveryEval.DetailItem {
	pn, err := domain.ParseProviderName(seedProvider)
	if err != nil {
		return nil
	}
	var resp *discoveryService.ContentFetchResponse
	if albums {
		resp, _ = a.svc.GetAlbums(ctx, pn, seedID, nil, name, 100)
	} else {
		resp, _ = a.svc.GetTopTracks(ctx, pn, seedID, nil, name, 25)
	}
	if resp == nil {
		return nil
	}
	out := make([]discoveryEval.DetailItem, 0, len(resp.Items))
	for _, it := range resp.Items {
		srcs := make([]string, 0, len(it.Sources))
		for _, s := range it.Sources {
			srcs = append(srcs, s.Provider.String())
		}
		out = append(out, discoveryEval.DetailItem{
			Title:      it.Title,
			Sources:    srcs,
			HasArtwork: it.ImageURL != "",
			Year:       it.Year,
		})
	}
	return out
}

func renderDetail(r discoveryEval.DetailReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n# detail-eval — %d golden artists\n\n", r.Goldens)
	fmt.Fprintf(&b, "  contamination:     %d  (want 0)\n", r.ContaminationCount)
	fmt.Fprintf(&b, "  album recall:      %.3f\n", r.AlbumRecall)
	fmt.Fprintf(&b, "  track recall:      %.3f\n", r.TrackRecall)
	fmt.Fprintf(&b, "  metadata coverage: %.3f\n\n", r.MetadataCoverage)
	for _, a := range r.PerArtist {
		fmt.Fprintf(&b, "  %-26s albums=%-3d tracks=%-3d contam=%-2d recall(a/t)=%.2f/%.2f cover=%.2f\n",
			a.Name, a.Albums, a.Tracks, a.Contamination, a.AlbumRecall, a.TrackRecall, a.MetadataCoverage)
	}
	return b.String()
}

//go:embed detail_live_artists.txt
var detailLiveArtists string

type detailLiveRow struct {
	Artist         string   `json:"artist"`
	CardIDs        []string `json:"card_ids"`
	Items          int      `json:"items"`
	Confirmed      int      `json:"confirmed"`
	SoundCloudOnly int      `json:"soundcloud_only"`
	AlbumsMs       int64    `json:"albums_ms"`
}

type detailLiveReport struct {
	Rows           []detailLiveRow `json:"rows"`
	Items          int             `json:"items"`
	ConfirmedShare float64         `json:"confirmed_share"`
	SoundCloudOnly int             `json:"soundcloud_only"`
	P50Ms          int64           `json:"p50_ms"`
	MaxMs          int64           `json:"max_ms"`
}

func runDetailLive(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	searcher, drain := buildEvalSearcher(ctx, cfg, pool, redisClient)
	defer drain()
	content := app.BuildArtistContentService(cfg, nil, &seededIdentityStore{})

	var rows []detailLiveRow
	for _, name := range strings.Split(strings.TrimSpace(detailLiveArtists), "\n") {
		results, err := searcher.Search(ctx, name)
		if err != nil {
			return fmt.Errorf("search %q: %w", name, err)
		}
		card, ok := artistCardNamed(results, name)
		if !ok {
			fmt.Fprintf(os.Stderr, "detail-live: no artist card named %q\n", name)
			continue
		}
		seed, known := card.Sources[0], cardKnownIDs(card)
		start := time.Now()
		albums, err := content.GetAlbums(ctx, seed.Provider, seed.ExternalID, known, card.Title, 100)
		if err != nil {
			return fmt.Errorf("albums %q: %w", name, err)
		}
		row := scoreDetailLive(name, albums.Items)
		row.AlbumsMs = time.Since(start).Milliseconds()
		for p, id := range known {
			row.CardIDs = append(row.CardIDs, p.String()+":"+id)
		}
		rows = append(rows, row)
	}
	report := summarizeDetailLive(rows)
	fmt.Print(renderDetailLive(report))
	if opts.jsonPath != "" {
		b, _ := json.MarshalIndent(report, "", "  ")
		return os.WriteFile(opts.jsonPath, b, 0o600)
	}
	return nil
}

func artistCardNamed(results []domain.SearchResult, name string) (domain.SearchResult, bool) {
	want := textnorm.NormalizeForMatch(name)
	for _, r := range results {
		if r.Kind == domain.ResultKindArtist && len(r.Sources) > 0 && textnorm.NormalizeForMatch(r.Title) == want {
			return r, true
		}
	}
	return domain.SearchResult{}, false
}

func cardKnownIDs(card domain.SearchResult) map[domain.ProviderName]string {
	known := make(map[domain.ProviderName]string, len(card.Sources))
	for _, s := range card.Sources {
		known[s.Provider] = s.ExternalID
	}
	return known
}

var detailCatalogs = []domain.ProviderName{domain.ProviderDeezer, domain.ProviderAppleMusic, domain.ProviderITunes, domain.ProviderSpotify}

func scoreDetailLive(artist string, items []domain.SearchResult) detailLiveRow {
	row := detailLiveRow{Artist: artist, Items: len(items)}
	for _, it := range items {
		catalogs := 0
		for _, s := range it.Sources {
			if slices.Contains(detailCatalogs, s.Provider) {
				catalogs++
			}
		}
		switch {
		case catalogs >= 2:
			row.Confirmed++
		case catalogs == 0:
			row.SoundCloudOnly++
		}
	}
	return row
}

func summarizeDetailLive(rows []detailLiveRow) detailLiveReport {
	report := detailLiveReport{Rows: rows}
	confirmed := 0
	ms := make([]int64, 0, len(rows))
	for _, r := range rows {
		report.Items += r.Items
		report.SoundCloudOnly += r.SoundCloudOnly
		confirmed += r.Confirmed
		ms = append(ms, r.AlbumsMs)
	}
	if report.Items > 0 {
		report.ConfirmedShare = float64(confirmed) / float64(report.Items)
	}
	if len(ms) > 0 {
		slices.Sort(ms)
		report.P50Ms, report.MaxMs = ms[len(ms)/2], ms[len(ms)-1]
	}
	return report
}

func renderDetailLive(r detailLiveReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n# detail-live — %d artists, opened from their search card\n\n", len(r.Rows))
	fmt.Fprintf(&b, "  confirmed by 2+ catalogs: %.0f%% of %d items\n", r.ConfirmedShare*100, r.Items)
	fmt.Fprintf(&b, "  soundcloud only:          %d  (want 0)\n", r.SoundCloudOnly)
	fmt.Fprintf(&b, "  albums latency:           p50 %dms, max %dms\n\n", r.P50Ms, r.MaxMs)
	for _, row := range r.Rows {
		fmt.Fprintf(&b, "  %-16s items=%-4d confirmed=%-4d soundcloud_only=%-3d %5dms\n", row.Artist, row.Items, row.Confirmed, row.SoundCloudOnly, row.AlbumsMs)
	}
	return b.String()
}
