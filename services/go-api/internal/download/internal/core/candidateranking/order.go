package candidateranking

import (
	"altune/go-api/internal/download/internal/core/ports"
	"cmp"
	"slices"
	"strings"
)

func appendSorted(ranked []ports.AudioCandidate, bucket []entry, compare func(a, b entry) int) []ports.AudioCandidate {
	slices.SortStableFunc(bucket, compare)
	for _, e := range bucket {
		ranked = append(ranked, e.candidate)
	}
	return ranked
}

func compareResolved(a, b entry) int {
	return cmp.Or(
		trueFirst(isUsersPick(a), isUsersPick(b)),
		trueFirst(a.explicitFits, b.explicitFits),
		compareTie(a, b),
	)
}

func compareTopic(a, b entry) int {
	return cmp.Or(
		falseFirst(a.isOtherVersion, b.isOtherVersion),
		trueFirst(a.artistMatch, b.artistMatch),
		trueFirst(a.featMatch, b.featMatch),
		cmp.Compare(a.qualDistance, b.qualDistance),
		cmp.Compare(b.ident, a.ident),
		compareTie(a, b),
	)
}

func compareOther(a, b entry) int {
	return cmp.Or(
		falseFirst(a.isOtherVersion, b.isOtherVersion),
		cmp.Compare(b.ident, a.ident),
		trueFirst(a.featMatch, b.featMatch),
		cmp.Compare(b.meta, a.meta),
		cmp.Compare(a.qualDistance, b.qualDistance),
		compareTie(a, b),
	)
}

func compareTie(a, b entry) int {
	return cmp.Or(
		cmp.Compare(a.durationDelta, b.durationDelta),
		strings.Compare(a.candidate.URL, b.candidate.URL),
	)
}

func isUsersPick(e entry) bool {
	return e.candidate.Kind == ports.KindCatalogPin
}

func trueFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

func falseFirst(a, b bool) int {
	return trueFirst(b, a)
}
