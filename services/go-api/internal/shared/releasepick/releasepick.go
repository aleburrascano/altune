package releasepick

const (
	PrimaryAlbum  = "Album"
	PrimarySingle = "Single"
	PrimaryEP     = "EP"

	secondaryMixtape = "Mixtape/Street"
)

type Kind int

const (
	KindNone Kind = iota
	KindStudioAlbum
	KindMixtape
	KindSingleOrEP
)

type Group struct {
	PrimaryType    string   `json:"primary_type"`
	SecondaryTypes []string `json:"secondary_types,omitempty"`
	Date           string   `json:"date,omitempty"`
}

func (g Group) Kind() Kind {
	switch {
	case g.PrimaryType == PrimaryAlbum && len(g.SecondaryTypes) == 0:
		return KindStudioAlbum
	case g.PrimaryType == PrimaryAlbum && len(g.SecondaryTypes) == 1 && g.SecondaryTypes[0] == secondaryMixtape:
		return KindMixtape
	case (g.PrimaryType == PrimarySingle || g.PrimaryType == PrimaryEP) && len(g.SecondaryTypes) == 0:
		return KindSingleOrEP
	}
	return KindNone
}

func IsEarlier(a, b string) bool {
	shared := min(len(a), len(b))
	switch {
	case a == "":
		return false
	case b == "":
		return true
	}
	return a[:shared] < b[:shared]
}

func CompareDates(a, b string) int {
	switch {
	case IsEarlier(a, b):
		return -1
	case IsEarlier(b, a):
		return 1
	}
	return 0
}

func Earliest[T any](items []T, groupOf func(T) Group, want Kind) (T, bool) {
	var best T
	found := false
	for _, item := range items {
		g := groupOf(item)
		if g.Kind() == want && (!found || IsEarlier(g.Date, groupOf(best).Date)) {
			best, found = item, true
		}
	}
	return best, found
}

func Best[T any](items []T, groupOf func(T) Group) (T, Kind, bool) {
	for _, kind := range []Kind{KindStudioAlbum, KindMixtape, KindSingleOrEP} {
		if best, ok := Earliest(items, groupOf, kind); ok {
			return best, kind, true
		}
	}
	var none T
	return none, KindNone, false
}
