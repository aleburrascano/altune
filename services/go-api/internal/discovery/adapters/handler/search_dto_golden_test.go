package handler

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"encoding/json"
	"testing"
)

// searchDTOTypedExtrasGolden was captured from the pre-#1084 code, when
// record_type and resolution_tier still lived in SearchResult.Extras. The
// mobile client reads extras["record_type"], so promoting them to typed fields
// must leave the wire bytes unchanged, including inside collapsed_artists.
const searchDTOTypedExtrasGolden = `[{"kind":"album","title":"Blue","subtitle":"Band","confidence":"high","result_signature":"album|blue|band","favorite_key":"band|blue","sources":[{"provider":"deezer","external_id":"1","url":""},{"provider":"applemusic","external_id":"2","url":""}],"extras":{"genre_id":1,"record_type":"ep","resolution_tier":"upc","track_count":5,"upc":"123"}},{"kind":"album","title":"Red","subtitle":"Band","confidence":"low","result_signature":"album|red|band","favorite_key":"band|red","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"record_type":"compile"}},{"kind":"artist","title":"Band","confidence":"low","result_signature":"artist|band|","favorite_key":"band","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{"collapsed_artists":[{"title":"Band","subtitle":"","sources":[{"Provider":11,"ExternalID":"5","URL":""}],"extras":{"mbid":"m1","resolution_tier":"isrc"}}]}}]`

func TestSearchResultToDTO_typedRecordTypeAndTierKeepWireBytes(t *testing.T) {
	dz := domain.SearchResult{
		Kind: domain.ResultKindAlbum, Title: "Blue", Subtitle: "Band", UPC: "123", RecordType: "ep",
		Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "1"}},
		Extras:  map[string]any{"genre_id": 1},
	}
	am := domain.SearchResult{
		Kind: domain.ResultKindAlbum, Title: "Blue", Subtitle: "Band", UPC: "123", TrackCount: 5, RecordType: "album",
		Sources: []domain.SourceRef{{Provider: domain.ProviderAppleMusic, ExternalID: "2"}},
		Extras:  map[string]any{"upc": "123"},
	}
	solo := domain.SearchResult{
		Kind: domain.ResultKindAlbum, Title: "Red", Subtitle: "Band", RecordType: "compile",
		Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "3"}},
	}
	primary := domain.SearchResult{
		Kind: domain.ResultKindArtist, Title: "Band", Popularity: 9,
		Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "4"}},
		Extras:  map[string]any{},
	}
	dup := domain.SearchResult{
		Kind: domain.ResultKindArtist, Title: "Band", Popularity: 1, MBID: "m1",
		ResolutionTier: domain.StampResolutionTier(domain.EntityResolutionISRC),
		Sources:        []domain.SourceRef{{Provider: domain.ProviderSpotify, ExternalID: "5"}},
	}

	results := make([]domain.SearchResult, 0, 3)
	for _, e := range service.Merge([][]domain.SearchResult{{dz, solo}, {am}}) {
		results = append(results, e.Result)
	}
	results = append(results, service.CollapseArtistDuplicates([]domain.SearchResult{primary, dup})...)

	got, err := json.Marshal(searchResultsToDTOs(results))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != searchDTOTypedExtrasGolden {
		t.Errorf("search DTO JSON drifted.\n got: %s\nwant: %s", got, searchDTOTypedExtrasGolden)
	}
}
