package handler

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/rank"
	"encoding/json"
	"testing"
)

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
		Kind: domain.ResultKindArtist, Title: "Band", Popularity: 1, ArtistMBID: "m1",
		ResolutionTier: domain.StampResolutionTier(domain.EntityResolutionISRC),
		Sources:        []domain.SourceRef{{Provider: domain.ProviderSpotify, ExternalID: "5"}},
	}

	results := make([]domain.SearchResult, 0, 3)
	for _, e := range rank.Merge([][]domain.SearchResult{{dz, solo}, {am}}) {
		results = append(results, e.Result)
	}
	results = append(results, rank.CollapseArtistDuplicates([]domain.SearchResult{primary, dup})...)

	got, err := json.Marshal(searchResultsToDTOs(results))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != searchDTOTypedExtrasGolden {
		t.Errorf("search DTO JSON drifted.\n got: %s\nwant: %s", got, searchDTOTypedExtrasGolden)
	}
}

func TestSearchResultToDTO_FillsExtrasDurationFromTheResult(t *testing.T) {
	tests := []struct {
		name   string
		result domain.SearchResult
		want   any
	}{
		{"result duration fills a missing extra", domain.SearchResult{Kind: domain.ResultKindTrack, Title: "T", Duration: 226}, 226},
		{"no duration leaves the extra absent", domain.SearchResult{Kind: domain.ResultKindTrack, Title: "T"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchResultToDTO(tt.result).Extras["duration"]
			if got != tt.want {
				t.Errorf("extras.duration = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchResultToDTO_mbidKeyIsProjectedFromTheKindsTypedField(t *testing.T) {
	typed := domain.SearchResult{RecordingMBID: "rec", ReleaseMBID: "rel", ReleaseGroupMBID: "rg", ArtistMBID: "art"}
	cases := []struct {
		kind domain.ResultKind
		want string
	}{
		{domain.ResultKindTrack, "rec"},
		{domain.ResultKindAlbum, "rg"},
		{domain.ResultKindArtist, "art"},
	}
	for _, c := range cases {
		t.Run(c.kind.String(), func(t *testing.T) {
			r := typed
			r.Kind = c.kind
			r.Title = "T"

			dtos := searchResultsToDTOs([]domain.SearchResult{r})

			if got := dtos[0].Extras["mbid"]; got != c.want {
				t.Errorf("extras.mbid = %v, want %q", got, c.want)
			}
		})
	}
}

func TestSearchResultToDTO_AMergedCleanEditSendsExplicitFalseOnTheWire(t *testing.T) {
	clean := domain.SearchResult{
		Kind: domain.ResultKindTrack, Title: "Been Waiting!", Subtitle: "Autumn!", ISRC: "QM42K1903228", Duration: 123,
		Sources:  []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "642332252"}},
		Explicit: domain.BoolPtr(false),
	}
	unknown := domain.SearchResult{
		Kind: domain.ResultKindTrack, Title: "Been Waiting!", Subtitle: "Autumn!", ISRC: "QM42K1903228", Duration: 123,
		Sources: []domain.SourceRef{{Provider: domain.ProviderSpotify, ExternalID: "s1"}},
	}
	for i, groups := range [][][]domain.SearchResult{{{clean}, {unknown}}, {{unknown}, {clean}}} {
		entities := rank.Merge(groups)
		if len(entities) != 1 {
			t.Fatalf("order %d: got %d entities, want one merged track", i, len(entities))
		}
		raw, err := json.Marshal(searchResultToDTO(entities[0].Result))
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			Extras map[string]json.RawMessage `json:"extras"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		if got := string(wire.Extras["explicit"]); got != "false" {
			t.Errorf("order %d: wire extras.explicit = %q, want false in %s", i, got, raw)
		}
	}
}

func TestSearchResultToDTO_AnUnknownExplicitLeavesTheWireKeyAbsent(t *testing.T) {
	raw, err := json.Marshal(searchResultToDTO(domain.SearchResult{Kind: domain.ResultKindTrack, Title: "T"}))
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Extras map[string]json.RawMessage `json:"extras"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if got, present := wire.Extras["explicit"]; present {
		t.Errorf("wire extras.explicit = %s, want the key absent when no provider knows", got)
	}
}
