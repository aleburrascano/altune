package service

import "altune/go-api/internal/discovery/domain"

// cohesionEdgeMin is how many releases two providers must SHARE before we treat
// them as the same artist. One shared title can be coincidence (two same-name
// artists both have a "Baddest"); a pattern of ≥2 is a real cross-platform artist.
const cohesionEdgeMin = 2

// FilterCohesive defends against a FRACTURED identity — two same-name artists
// fused into one, e.g. a MusicBrainz wrong url-relation linking the rapper Che's
// MBID to a different Che's Deezer id (doc §7). The fan-out then returns two
// artists; every release is legitimately id-verified (of a different human), so
// per-release keep rules can't separate them. The fracture is a property of the
// PROVIDER SET.
//
// Providers of one real artist corroborate: the same releases appear across
// several of them. We build a graph where two providers are linked when they
// share ≥ cohesionEdgeMin releases (a single shared title is ignored as a likely
// collision), find connected components, and keep the releases of the component
// with the MOST providers — the widest cross-platform presence, i.e. the real
// artist. A mis-bridged provider is an island (few or no genuine overlaps) and is
// dropped. With no multi-provider component at all (a genuine single-provider
// artist, or nothing corroborates) there is no signal, so everything is kept.
func FilterCohesive(releases []MergedRelease) []MergedRelease {
	shared, providers := providerCooccurrence(releases)
	if len(providers) <= 1 {
		return releases
	}

	uf := newProviderUnionFind(providers)
	for pair, n := range shared {
		if n >= cohesionEdgeMin {
			uf.union(pair.a, pair.b)
		}
	}

	best, size := uf.largestComponent()
	if size <= 1 {
		return releases // nothing corroborates ≥ the threshold — no signal to act on
	}

	out := make([]MergedRelease, 0, len(releases))
	for _, m := range releases {
		for p := range m.Providers {
			if uf.find(p) == best {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

type providerPair struct{ a, b domain.ProviderName }

// providerCooccurrence counts, for each unordered provider pair, how many merged
// releases carry both — and collects every provider seen.
func providerCooccurrence(releases []MergedRelease) (map[providerPair]int, map[domain.ProviderName]bool) {
	shared := make(map[providerPair]int)
	providers := make(map[domain.ProviderName]bool)
	for _, m := range releases {
		ps := make([]domain.ProviderName, 0, len(m.Providers))
		for p := range m.Providers {
			providers[p] = true
			ps = append(ps, p)
		}
		for i := 0; i < len(ps); i++ {
			for j := i + 1; j < len(ps); j++ {
				shared[orderedPair(ps[i], ps[j])]++
			}
		}
	}
	return shared, providers
}

func orderedPair(a, b domain.ProviderName) providerPair {
	if a.String() > b.String() {
		a, b = b, a
	}
	return providerPair{a, b}
}

// providerUnionFind is a tiny union-find over the handful of providers in a
// fan-out, used to group them into same-artist components.
type providerUnionFind struct {
	parent map[domain.ProviderName]domain.ProviderName
}

func newProviderUnionFind(providers map[domain.ProviderName]bool) *providerUnionFind {
	parent := make(map[domain.ProviderName]domain.ProviderName, len(providers))
	for p := range providers {
		parent[p] = p
	}
	return &providerUnionFind{parent: parent}
}

func (u *providerUnionFind) find(p domain.ProviderName) domain.ProviderName {
	for u.parent[p] != p {
		u.parent[p] = u.parent[u.parent[p]]
		p = u.parent[p]
	}
	return p
}

func (u *providerUnionFind) union(a, b domain.ProviderName) {
	u.parent[u.find(a)] = u.find(b)
}

// largestComponent returns the component root with the most providers, and that
// count.
func (u *providerUnionFind) largestComponent() (domain.ProviderName, int) {
	counts := make(map[domain.ProviderName]int)
	for p := range u.parent {
		counts[u.find(p)]++
	}
	var best domain.ProviderName
	max := 0
	for root, n := range counts {
		if n > max {
			best, max = root, n
		}
	}
	return best, max
}
