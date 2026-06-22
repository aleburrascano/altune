package service

import (
	"context"
	"sync"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"

	"golang.org/x/sync/errgroup"
)

// signalBTitleMatchMinTSR mirrors the consensus album-merge threshold so the
// diagnostic groups entities the same way the live pipeline does.
const signalBTitleMatchMinTSR = 85

// ProviderGap is one provider's entity-level miss rate against the union.
type ProviderGap struct {
	Provider string  `json:"provider"`
	Missing  int     `json:"missing"` // union entities this provider lacked
	Union    int     `json:"union"`   // union entities across artists where it responded
	GapPct   float64 `json:"gap_pct"` // missing / union, in [0,1]
}

// CoverageReportB is the entity-level cross-provider imbalance diagnostic.
type CoverageReportB struct {
	ArtistsScanned int           `json:"artists_scanned"`
	TotalEntities  int           `json:"total_entities"`
	ProviderGaps   []ProviderGap `json:"provider_gaps"`
	Caveats        []string      `json:"caveats"`
}

// CoverageSignalBService fans out per artist to the album providers and measures
// which provider misses which canonical album. Diagnostic only: it measures
// provider imbalance, not absolute coverage (see Caveats in the report).
type CoverageSignalBService struct {
	providers []ConsensusProvider
}

func NewCoverageSignalBService(providers []ConsensusProvider) *CoverageSignalBService {
	return &CoverageSignalBService{providers: providers}
}

type provResult struct {
	albums []domain.SearchResult
	ok     bool // provider responded (no error)
}

type artistCoverage struct {
	responded []string          // providers that responded for this artist
	entities  []map[string]bool // per canonical album: the set of providers that had it
}

func (s *CoverageSignalBService) Execute(ctx context.Context, artists []string, concurrency int) (*CoverageReportB, error) {
	if concurrency < 1 {
		concurrency = 1
	}

	coverage := make([]artistCoverage, len(artists))
	g := new(errgroup.Group)
	g.SetLimit(concurrency)
	for i, artist := range artists {
		i, artist := i, artist
		g.Go(func() error {
			fan := s.fanOut(ctx, artist)
			responded := []string{}
			for _, p := range s.providers {
				if r, ok := fan[p.Name]; ok && r.ok {
					responded = append(responded, p.Name)
				}
			}
			coverage[i] = artistCoverage{responded: responded, entities: s.clusterEntities(fan)}
			return nil
		})
	}
	_ = g.Wait()

	missing := map[string]int{}
	union := map[string]int{}
	report := &CoverageReportB{
		ProviderGaps: make([]ProviderGap, 0, len(s.providers)),
		Caveats:      signalBCaveats(),
	}
	for _, ac := range coverage {
		if len(ac.responded) == 0 {
			continue
		}
		report.ArtistsScanned++
		report.TotalEntities += len(ac.entities)
		for _, prov := range ac.responded {
			union[prov] += len(ac.entities)
			for _, ent := range ac.entities {
				if !ent[prov] {
					missing[prov]++
				}
			}
		}
	}

	for _, p := range s.providers {
		u := union[p.Name]
		gap := 0.0
		if u > 0 {
			gap = float64(missing[p.Name]) / float64(u)
		}
		report.ProviderGaps = append(report.ProviderGaps, ProviderGap{
			Provider: p.Name,
			Missing:  missing[p.Name],
			Union:    u,
			GapPct:   gap,
		})
	}
	return report, nil
}

// fanOut queries every provider for an artist's albums in parallel. A provider
// that errors is marked not-responded (ok=false) so transient failures don't
// inflate its gap.
func (s *CoverageSignalBService) fanOut(ctx context.Context, artist string) map[string]provResult {
	var mu sync.Mutex
	out := make(map[string]provResult, len(s.providers))
	var wg sync.WaitGroup
	for _, p := range s.providers {
		wg.Add(1)
		go func(p ConsensusProvider) {
			defer wg.Done()
			albums, err := p.Fetcher(ctx, artist)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out[p.Name] = provResult{ok: false}
				return
			}
			out[p.Name] = provResult{albums: albums, ok: true}
		}(p)
	}
	wg.Wait()
	return out
}

// clusterEntities groups albums across providers into canonical entities by
// fuzzy title match (same mechanism as consensus). Providers are iterated in
// declaration order so cluster membership is deterministic.
func (s *CoverageSignalBService) clusterEntities(fan map[string]provResult) []map[string]bool {
	type cluster struct {
		key   string
		provs map[string]bool
	}
	var clusters []*cluster

	for _, p := range s.providers {
		r, ok := fan[p.Name]
		if !ok || !r.ok {
			continue
		}
		for _, album := range r.albums {
			titleNorm := textnorm.NormalizeForMatch(album.Title)
			if titleNorm == "" {
				continue
			}
			matched := false
			for _, c := range clusters {
				if textnorm.TokenSortRatio(titleNorm, c.key) >= signalBTitleMatchMinTSR {
					c.provs[p.Name] = true
					matched = true
					break
				}
			}
			if !matched {
				clusters = append(clusters, &cluster{key: titleNorm, provs: map[string]bool{p.Name: true}})
			}
		}
	}

	entities := make([]map[string]bool, len(clusters))
	for i, c := range clusters {
		entities[i] = c.provs
	}
	return entities
}

func signalBCaveats() []string {
	return []string{
		"Measures provider IMBALANCE, not absolute coverage: an album missing from ALL providers is invisible here.",
		"Entity matching is fuzzy-title only (no dedup cleaning); accuracy improves with the rebuild's Layer-2.",
		"Gap % is entities-missed / union-entities at entity level, not raw album count.",
		"An artist counts toward a provider only when that provider responded (transient failures excluded).",
	}
}
