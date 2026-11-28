package domain

const cohesionEdgeMin = 2

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
		return releases
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

type providerPair struct{ a, b ProviderName }

func providerCooccurrence(releases []MergedRelease) (map[providerPair]int, map[ProviderName]bool) {
	shared := make(map[providerPair]int)
	providers := make(map[ProviderName]bool)
	for _, m := range releases {
		ps := make([]ProviderName, 0, len(m.Providers))
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

func orderedPair(a, b ProviderName) providerPair {
	if a.String() > b.String() {
		a, b = b, a
	}
	return providerPair{a, b}
}

type providerUnionFind struct {
	parent map[ProviderName]ProviderName
}

func newProviderUnionFind(providers map[ProviderName]bool) *providerUnionFind {
	parent := make(map[ProviderName]ProviderName, len(providers))
	for p := range providers {
		parent[p] = p
	}
	return &providerUnionFind{parent: parent}
}

func (u *providerUnionFind) find(p ProviderName) ProviderName {
	for u.parent[p] != p {
		u.parent[p] = u.parent[u.parent[p]]
		p = u.parent[p]
	}
	return p
}

func (u *providerUnionFind) union(a, b ProviderName) {
	u.parent[u.find(a)] = u.find(b)
}

func (u *providerUnionFind) largestComponent() (ProviderName, int) {
	counts := make(map[ProviderName]int)
	minMember := make(map[ProviderName]string)
	for p := range u.parent {
		root := u.find(p)
		counts[root]++
		if s := p.String(); minMember[root] == "" || s < minMember[root] {
			minMember[root] = s
		}
	}
	var best ProviderName
	bestCount := 0
	for root, n := range counts {
		if n > bestCount || (n == bestCount && n > 0 && minMember[root] < minMember[best]) {
			best, bestCount = root, n
		}
	}
	return best, bestCount
}
