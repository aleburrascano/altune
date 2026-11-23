package discographyrank

import (
	"altune/go-api/internal/discovery/internal/core/ports"
	"math"
	"sort"
)

func Rank(cases []ports.DiscographyCase, groupBy ports.DiscographyGroupBy) []ports.DiscographyCase {
	switch groupBy {
	case ports.GroupByProvider:
		return clusterByProvider(cases)
	case ports.GroupByContaminationBand:
		sort.SliceStable(cases, func(i, j int) bool { return bandThenWorse(cases[i], cases[j]) })
		return cases
	default:
		sort.SliceStable(cases, func(i, j int) bool { return worseThan(cases[i], cases[j]) })
		return cases
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
	for p, n := range c.ProviderCounts {
		if n > bestN || (n == bestN && p < best) {
			best, bestN = p, n
		}
	}
	return best
}

func worseThan(a, b ports.DiscographyCase) bool {
	if na, nb := noIDSuspectRatio(a), noIDSuspectRatio(b); na != nb {
		return na > nb
	}
	if ra, rb := contaminationRatio(a), contaminationRatio(b); ra != rb {
		return ra > rb
	}
	if ia, ib := providerImbalance(a), providerImbalance(b); ia != ib {
		return ia > ib
	}
	if a.Releases != b.Releases {
		return a.Releases > b.Releases
	}
	return a.ArtistRef < b.ArtistRef
}

func contaminationBand(c ports.DiscographyCase) int {
	switch r := contaminationRatio(c); {
	case r >= 0.5:
		return 0
	case r >= 0.2:
		return 1
	default:
		return 2
	}
}

func bandThenWorse(a, b ports.DiscographyCase) bool {
	if ba, bb := contaminationBand(a), contaminationBand(b); ba != bb {
		return ba < bb
	}
	return worseThan(a, b)
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
	if ra, rb := ratio(a.single, a.releases), ratio(b.single, b.releases); ra != rb {
		return ra > rb
	}
	if a.releases != b.releases {
		return a.releases > b.releases
	}
	return a.provider < b.provider
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
