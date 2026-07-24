package main

// detail mode — the offline quality gate for the artist detail/discography path
// (okf/backend/discovery/artist-detail.md), sibling of the ranking eval. It runs
// the real detail service in-process against LIVE providers, but over a SEEDED
// in-memory identity store built from the goldens — so a golden can carry a
// deliberately fractured identity (a wrong streaming edge, the "Che" bug) and the
// harness verifies the read-time guards drop the contamination. No DB identity
// data is touched; the corpus is the committed detail_goldens.json.

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	discoveryEval "altune/go-api/internal/discovery/service/eval"
	"altune/go-api/internal/shared/config"
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

// ---- seeded identity store ----------------------------------------------

var _ discoveryPorts.IdentityStore = (*seededIdentityStore)(nil)

type identityRow struct {
	mbid string
	xref map[string]string
}

// seededIdentityStore answers LookupByProviderID from a fixed map built out of
// the goldens — the harness's stand-in for the durable store, so it can feed
// exactly the (possibly fractured) identity each golden declares.
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

func (s *seededIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	return nil
}

func (s *seededIdentityStore) Invalidate(context.Context, domain.ResultKind, string, string) error {
	return nil
}

func (s *seededIdentityStore) LookupByProviderID(_ context.Context, kind domain.ResultKind, provider, externalID string) (string, map[string]string, bool) {
	if kind != domain.ResultKindArtist {
		return "", nil, false
	}
	row, ok := s.entries[provider+"|"+externalID]
	if !ok {
		return "", nil, false
	}
	return row.mbid, row.xref, true
}

// ---- service adapter (real ContentFetchResponse -> eval.DetailItem) -----

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
		resp, _ = a.svc.GetAlbums(ctx, pn, seedID, name, 100)
	} else {
		resp, _ = a.svc.GetTopTracks(ctx, pn, seedID, name, 25)
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
