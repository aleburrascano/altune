package main

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/textnorm"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

//go:embed detail_goldens.json
var detailGoldensJSON []byte

func runDetail(ctx context.Context, cfg *config.Config, opts options) error {
	var goldens []discovery.DetailGolden
	if err := json.Unmarshal(detailGoldensJSON, &goldens); err != nil {
		return fmt.Errorf("parse detail goldens: %w", err)
	}

	adapter, err := seededDetailAdapter(ctx, cfg, goldens)
	if err != nil {
		return err
	}
	once := func() (discovery.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "detail-eval over %d golden artists (live providers)...\n", len(goldens))
		report := discovery.RunDetailEval(ctx, goldens, adapter)
		if err := adapter.fetchFailure(); err != nil {
			return nil, err
		}
		return report, nil
	}
	human := func(r discovery.HarnessReport) string { return renderDetail(r.(discovery.DetailReport)) }
	return runHarness("detail", once, human, opts)
}

func seededDetailAdapter(ctx context.Context, cfg *config.Config, goldens []discovery.DetailGolden) (*detailServiceAdapter, error) {
	disc, err := app.NewEvalDiscovery(ctx, cfg, app.EvalHandles{IdentityStore: seedStore(goldens)})
	if err != nil {
		return nil, err
	}
	return &detailServiceAdapter{svc: disc.EvalArtistContent()}, nil
}

var _ discovery.IdentityStore = (*seededIdentityStore)(nil)

type identityRow struct {
	mbid string
	xref map[discovery.ProviderKey]string
}

type seededIdentityStore struct {
	entries map[string]identityRow
}

func seedStore(goldens []discovery.DetailGolden) *seededIdentityStore {
	s := &seededIdentityStore{entries: make(map[string]identityRow, len(goldens))}
	for _, g := range goldens {
		s.entries[g.SeedProvider+"|"+g.SeedID] = identityRow{mbid: g.MBID, xref: g.Identity}
	}
	return s
}

func (s *seededIdentityStore) PersistBridges(context.Context, discovery.ResultKind, string, map[discovery.ProviderKey]string) error {
	return nil
}

func (s *seededIdentityStore) Invalidate(context.Context, discovery.ResultKind, discovery.ProviderKey, string) error {
	return nil
}

func (s *seededIdentityStore) LookupByProviderID(_ context.Context, kind discovery.ResultKind, provider discovery.ProviderKey, externalID string) (string, map[discovery.ProviderKey]string, bool) {
	if kind != discovery.ResultKindArtist {
		return "", nil, false
	}
	row, ok := s.entries[provider.String()+"|"+externalID]
	if !ok {
		return "", nil, false
	}
	return row.mbid, row.xref, true
}

type detailServiceAdapter struct {
	svc      *discovery.GetArtistContentService
	failures []error
}

func (a *detailServiceAdapter) fetchFailure() error {
	if len(a.failures) == 0 {
		return nil
	}
	return fmt.Errorf("detail-eval: %d fetch(es) failed, so the recall numbers would be wrong: %w", len(a.failures), errors.Join(a.failures...))
}

func (a *detailServiceAdapter) Albums(ctx context.Context, seedProvider, seedID, name string) []discovery.DetailItem {
	return a.fetch(ctx, seedProvider, seedID, name, true)
}

func (a *detailServiceAdapter) TopTracks(ctx context.Context, seedProvider, seedID, name string) []discovery.DetailItem {
	return a.fetch(ctx, seedProvider, seedID, name, false)
}

func (a *detailServiceAdapter) fetch(ctx context.Context, seedProvider, seedID, name string, albums bool) []discovery.DetailItem {
	pn, err := discovery.ParseProviderName(seedProvider)
	if err != nil {
		a.failures = append(a.failures, fmt.Errorf("%q: seed provider: %w", name, err))
		return nil
	}
	var resp *discovery.ContentFetchResponse
	var fetchErr error
	if albums {
		resp, fetchErr = a.svc.GetAlbums(ctx, pn, seedID, nil, 100)
	} else {
		resp, fetchErr = a.svc.GetTopTracks(ctx, pn, seedID, nil, 25)
	}
	if fetchErr != nil {
		a.failures = append(a.failures, fmt.Errorf("%q: %w", name, fetchErr))
	}
	if resp == nil {
		return nil
	}
	out := make([]discovery.DetailItem, 0, len(resp.Items))
	for _, it := range resp.Items {
		srcs := make([]string, 0, len(it.Sources))
		for _, s := range it.Sources {
			srcs = append(srcs, s.Provider.String())
		}
		out = append(out, discovery.DetailItem{
			Title:      it.Title,
			Sources:    srcs,
			HasArtwork: it.ImageURL != "",
			Year:       it.Year,
		})
	}
	return out
}

func renderDetail(r discovery.DetailReport) string {
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

type liveArtist struct {
	mbid string
	name string
}

func parseLiveArtists(text string) ([]liveArtist, error) {
	out := []liveArtist{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		mbid, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || strings.TrimSpace(name) == "" || !discovery.IsMBID(mbid) {
			return nil, fmt.Errorf("detail-live artist line %q must be \"<artist mbid> <name>\"", line)
		}
		out = append(out, liveArtist{mbid: mbid, name: strings.TrimSpace(name)})
	}
	return out, nil
}

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
	searcher, drain, err := buildEvalSearcher(ctx, cfg, pool, redisClient)
	if err != nil {
		return err
	}
	defer drain()
	contentDisc, err := app.NewEvalDiscovery(ctx, cfg, app.EvalHandles{IdentityStore: &seededIdentityStore{}})
	if err != nil {
		return err
	}
	content := contentDisc.EvalArtistContent()

	artists, err := parseLiveArtists(detailLiveArtists)
	if err != nil {
		return err
	}
	var rows []detailLiveRow
	var missingCards []string
	for _, artist := range artists {
		name := artist.name
		results, err := searcher.Search(ctx, name)
		if err != nil {
			return fmt.Errorf("search %q: %w", name, err)
		}
		card, ok := artistCardWithMBID(results, artist.mbid)
		if !ok {
			missingCards = append(missingCards, fmt.Sprintf("%s (%s)", name, artist.mbid))
			continue
		}
		seed, known := card.Sources[0], cardKnownIDs(card)
		start := time.Now()
		albums, err := content.GetAlbums(ctx, seed.Provider, seed.ExternalID, known, 100)
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
		if err := os.WriteFile(opts.jsonPath, b, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", opts.jsonPath, err)
		}
	}
	if len(missingCards) > 0 {
		return fmt.Errorf("detail-live: no artist card carries the expected MBID for %s", strings.Join(missingCards, ", "))
	}
	return nil
}

func artistCardWithMBID(results []discovery.SearchResult, mbid string) (discovery.SearchResult, bool) {
	for _, r := range results {
		if r.Kind == discovery.ResultKindArtist && len(r.Sources) > 0 && strings.EqualFold(r.ArtistMBID, mbid) {
			return r, true
		}
	}
	return discovery.SearchResult{}, false
}

func artistCardNamed(results []discovery.SearchResult, name string) (discovery.SearchResult, bool) {
	want := textnorm.NormalizeForMatch(name)
	for _, r := range results {
		if r.Kind == discovery.ResultKindArtist && len(r.Sources) > 0 && textnorm.NormalizeForMatch(r.Title) == want {
			return r, true
		}
	}
	return discovery.SearchResult{}, false
}

func cardKnownIDs(card discovery.SearchResult) map[discovery.ProviderName]string {
	known := make(map[discovery.ProviderName]string, len(card.Sources))
	for _, s := range card.Sources {
		known[s.Provider] = s.ExternalID
	}
	return known
}

var detailCatalogs = []discovery.ProviderName{discovery.ProviderDeezer, discovery.ProviderAppleMusic, discovery.ProviderITunes, discovery.ProviderSpotify}

func scoreDetailLive(artist string, items []discovery.SearchResult) detailLiveRow {
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
