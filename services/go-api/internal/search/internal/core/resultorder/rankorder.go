package resultorder

import (
	"cmp"
	"strings"
)

type Key struct {
	Demoted       bool
	TopPick       bool
	OfPick        bool
	Relevance     float64
	NamedArtist   bool
	Prominence    float64
	Behavioral    float64
	Popularity    float64
	MultiProvider bool
	RRF           float64
	Subtitle      string
	Title         string
}

func Less(a, b Key) bool {
	return compare(a, b) < 0
}

func compare(a, b Key) int {
	return cmp.Or(
		preferTrue(b.Demoted, a.Demoted),
		preferTrue(a.TopPick, b.TopPick),
		preferTrue(a.OfPick, b.OfPick),
		cmp.Compare(b.Relevance, a.Relevance),
		preferTrue(a.NamedArtist, b.NamedArtist),
		cmp.Compare(b.Prominence, a.Prominence),
		cmp.Compare(b.Behavioral, a.Behavioral),
		cmp.Compare(b.Popularity, a.Popularity),
		preferTrue(a.MultiProvider, b.MultiProvider),
		cmp.Compare(b.RRF, a.RRF),
		strings.Compare(a.Subtitle, b.Subtitle),
		strings.Compare(a.Title, b.Title),
	)
}

func preferTrue(a, b bool) int {
	if a == b {
		return 0
	}
	if a {
		return -1
	}
	return 1
}
