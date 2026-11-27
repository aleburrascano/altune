package albumordering

import (
	"altune/go-api/internal/search/internal/core/ports"
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
)

func Rank(cases []ports.DiscographyCase, groupBy ports.DiscographyGroupBy) ([]ports.DiscographyCase, error) {
	switch groupBy {
	case ports.GroupByProvider:
		return clusterByProvider(cases), nil
	case ports.GroupByContaminationBand:
		ranked := slices.Clone(cases)
		sort.SliceStable(ranked, func(i, j int) bool { return bandThenWorse(ranked[i], ranked[j]) })
		return ranked, nil
	case ports.GroupByArtist:
		ranked := slices.Clone(cases)
		sort.SliceStable(ranked, func(i, j int) bool { return worseThan(ranked[i], ranked[j]) })
		return ranked, nil
	default:
		return nil, fmt.Errorf("%w: %q", ports.ErrUnknownDiscographyGroupBy, groupBy)
	}
}

func ratio(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	return float64(max(part, 0)) / float64(whole)
}

func noIDSuspectRatio(c ports.DiscographyCase) float64 {
	return ratio(c.SingleProviderNoID, c.Releases)
}

func contaminationRatio(c ports.DiscographyCase) float64 {
	return ratio(c.SingleProvider, c.Releases)
}

func providerImbalance(c ports.DiscographyCase) int {
	if len(c.ProviderCounts) < 2 {
		return 0
	}
	minN, maxN := math.MaxInt, 0
	for _, n := range c.ProviderCounts {
		n = max(n, 0)
		minN = min(minN, n)
		maxN = max(maxN, n)
	}
	return maxN - minN
}

func dominantProvider(c ports.DiscographyCase) string {
	best, bestN := "", -1
	for _, p := range slices.Sorted(maps.Keys(c.ProviderCounts)) {
		if n := c.ProviderCounts[p]; n > bestN {
			best, bestN = p, n
		}
	}
	return best
}

func precedes(order int) bool { return order < 0 }

func worseOrder(a, b ports.DiscographyCase) int {
	return cmp.Or(
		cmp.Compare(noIDSuspectRatio(b), noIDSuspectRatio(a)),
		cmp.Compare(contaminationRatio(b), contaminationRatio(a)),
		cmp.Compare(providerImbalance(b), providerImbalance(a)),
		cmp.Compare(b.Releases, a.Releases),
		strings.Compare(a.ArtistRef, b.ArtistRef),
	)
}

func worseThan(a, b ports.DiscographyCase) bool { return precedes(worseOrder(a, b)) }

const (
	severeContamination   = 0.5
	moderateContamination = 0.2
)

func contaminationBand(c ports.DiscographyCase) int {
	r := contaminationRatio(c)
	if r >= severeContamination {
		return 0
	}
	if r >= moderateContamination {
		return 1
	}
	return 2
}

func bandThenWorse(a, b ports.DiscographyCase) bool {
	return precedes(cmp.Or(cmp.Compare(contaminationBand(a), contaminationBand(b)), worseOrder(a, b)))
}

type cluster struct {
	provider         string
	single, releases int
	members          []ports.DiscographyCase
}

func (cl *cluster) add(c ports.DiscographyCase) {
	cl.members = append(cl.members, c)
	if c.Releases <= 0 {
		return
	}
	cl.releases += c.Releases
	cl.single += max(c.SingleProvider, 0)
}

func clusterWorseThan(a, b *cluster) bool {
	return precedes(cmp.Or(
		cmp.Compare(ratio(b.single, b.releases), ratio(a.single, a.releases)),
		cmp.Compare(b.releases, a.releases),
		strings.Compare(a.provider, b.provider),
	))
}

func groupByDominantProvider(cases []ports.DiscographyCase) []*cluster {
	byProvider := map[string]*cluster{}
	var clusters []*cluster
	for _, c := range cases {
		p := dominantProvider(c)
		cl, ok := byProvider[p]
		if !ok {
			cl = &cluster{provider: p}
			byProvider[p] = cl
			clusters = append(clusters, cl)
		}
		cl.add(c)
	}
	return clusters
}

func clusterByProvider(cases []ports.DiscographyCase) []ports.DiscographyCase {
	clusters := groupByDominantProvider(cases)
	sort.SliceStable(clusters, func(i, j int) bool { return clusterWorseThan(clusters[i], clusters[j]) })
	out := make([]ports.DiscographyCase, 0, len(cases))
	for _, cl := range clusters {
		members := cl.members
		sort.SliceStable(members, func(i, j int) bool { return worseThan(members[i], members[j]) })
		out = append(out, members...)
	}
	return out
}
