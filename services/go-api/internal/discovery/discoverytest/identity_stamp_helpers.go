package discoverytest

import "altune/go-api/internal/discovery/domain"

func WithMBID(r domain.SearchResult, mbid string) domain.SearchResult {
	r.MBID = mbid
	return r
}
