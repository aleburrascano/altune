package service

import "altune/go-api/internal/discovery/domain"

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

type providerPair struct{ a, b domain.ProviderName }

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

func (u *providerUnionFind) largestComponent() (domain.ProviderName, int) {
	counts := make(map[domain.ProviderName]int)
	minMember := make(map[domain.ProviderName]string)
	for p := range u.parent {
		root := u.find(p)
		counts[root]++
		if s := p.String(); minMember[root] == "" || s < minMember[root] {
			minMember[root] = s
		}
	}
	var best domain.ProviderName
	max := 0
	for root, n := range counts {
		if n > max || (n == max && n > 0 && minMember[root] < minMember[best]) {
			best, max = root, n
		}
	}
	return best, max
}
