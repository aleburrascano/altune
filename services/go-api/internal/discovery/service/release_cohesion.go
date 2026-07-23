package service

import "altune/go-api/internal/discovery/domain"

// FilterCohesive defends against a FRACTURED identity — two same-name artists
// fused into one, e.g. a MusicBrainz wrong url-relation linking the rapper Che's
// MBID to a different Che's Deezer id (doc §7). Such a mis-bridged provider is an
// ISLAND: its releases overlap with no other provider in the fan-out, while the
// real artist's providers corroborate (the same release appears across several).
//
// The rule operates on the PROVIDER SET, where the fracture lives, not per
// release (every fractured release is legitimately id-verified — of a different
// human — so per-release keep rules can't separate them). It computes the
// cohesive core — providers that appear in at least one multi-source release —
// and drops releases sourced ONLY from providers outside it. With no
// corroboration anywhere (a genuine single-provider artist), there is no signal,
// so everything is kept. Runs alongside FilterKept, not instead of it.
func FilterCohesive(releases []MergedRelease) []MergedRelease {
	core := make(map[domain.ProviderName]bool)
	for _, m := range releases {
		if len(m.Providers) >= 2 {
			for p := range m.Providers {
				core[p] = true
			}
		}
	}
	if len(core) == 0 {
		return releases
	}

	out := make([]MergedRelease, 0, len(releases))
	for _, m := range releases {
		if hasCoreProvider(m.Providers, core) {
			out = append(out, m)
		}
	}
	return out
}

func hasCoreProvider(providers map[domain.ProviderName]bool, core map[domain.ProviderName]bool) bool {
	for p := range providers {
		if core[p] {
			return true
		}
	}
	return false
}
