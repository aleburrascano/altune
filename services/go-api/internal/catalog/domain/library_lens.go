package domain

import (
	"strings"
	"time"
)

type LibrarySort int

const (
	SortRecent LibrarySort = iota
	SortAlphabetical
	SortYear
)

var librarySortNames = map[LibrarySort]string{
	SortRecent:       "recent",
	SortAlphabetical: "az",
	SortYear:         "year",
}

func (s LibrarySort) String() string {
	return librarySortNames[s]
}

func ParseLibrarySort(s string) (LibrarySort, error) {
	switch strings.TrimSpace(s) {
	case "", "recent":
		return SortRecent, nil
	case "az":
		return SortAlphabetical, nil
	case "year":
		return SortYear, nil
	}
	return SortRecent, NewValidationError("unknown sort: " + s)
}

const MaxLibraryPageSize = 2000

const MaxLibrarySearchLength = maxTrackTextLength

const defaultCatalogPageSize = 50

func clampPageSize(limit int) int {
	if limit <= 0 {
		return defaultCatalogPageSize
	}
	if limit > MaxLibraryPageSize {
		return MaxLibraryPageSize
	}
	return limit
}

func NormalizePage(limit, offset int) (int, error) {
	if offset < 0 {
		return 0, NewValidationError("offset must not be negative")
	}
	return clampPageSize(limit), nil
}

func NormalizeLibraryPage(query LibraryQuery) (LibraryQuery, error) {
	limit, err := NormalizePage(query.Limit, query.Offset)
	if err != nil {
		return LibraryQuery{}, err
	}
	query.Limit = limit
	return query, nil
}

type LibraryQuery struct {
	Search string
	Sort   LibrarySort
	Limit  int
	Offset int
}

type AlbumGroup struct {
	Key               string
	Album             string
	Artist            string
	ArtworkURL        *string
	Year              *int
	TrackCount        int
	MostRecentAddedAt time.Time
}

type ArtistGroup struct {
	Key               string
	Artist            string
	ArtworkURL        *string
	TrackCount        int
	MostRecentAddedAt time.Time
}

type OwnedTrackRef struct {
	ID                string
	Title             string
	Artist            string
	AcquisitionStatus string
	TrackNumber       *int
	ISRC              string
}
