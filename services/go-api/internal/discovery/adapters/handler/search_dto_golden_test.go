package handler_test

import (
	"altune/go-api/internal/discovery"
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func servedItems(t *testing.T, results ...discovery.SearchResult) []json.RawMessage {
	t.Helper()
	router := buildRelatedRouter(&fakeRelatedTracksProvider{results: results})
	rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud/id-1/related", nil)
	discAssertStatus(t, rec, http.StatusOK)
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	discDecodeJSON(t, rec, &body)
	if len(body.Items) != len(results) {
		t.Fatalf("served %d items, want %d", len(body.Items), len(results))
	}
	return body.Items
}

func servedItem(t *testing.T, result discovery.SearchResult) discovery.SearchResultDTO {
	t.Helper()
	var dto discovery.SearchResultDTO
	if err := json.Unmarshal(servedItems(t, result)[0], &dto); err != nil {
		t.Fatal(err)
	}
	return dto
}

func sameJSON(t *testing.T, got, want any) bool {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(g, w)
}

const searchDTOTypedExtrasGolden = `[{"kind":"album","title":"Blue","subtitle":"Band","confidence":"high","result_signature":"album|blue|band","favorite_key":"band|blue","sources":[{"provider":"deezer","external_id":"1","url":""},{"provider":"applemusic","external_id":"2","url":""}],"extras":{"genre_id":1,"record_type":"ep","resolution_tier":"upc","track_count":5,"upc":"123"}},{"kind":"album","title":"Red","subtitle":"Band","confidence":"low","result_signature":"album|red|band","favorite_key":"band|red","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"record_type":"compile"}},{"kind":"artist","title":"Band","confidence":"low","result_signature":"artist|band|","favorite_key":"band","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{"collapsed_artists":[{"title":"Band","subtitle":"","sources":[{"Provider":11,"ExternalID":"5","URL":""}],"extras":{"mbid":"m1","resolution_tier":"isrc"}}]}}]`

func TestSearchResultToDTO_typedRecordTypeAndTierKeepWireBytes(t *testing.T) {
	dz := discovery.SearchResult{
		Kind: discovery.ResultKindAlbum, Title: "Blue", Subtitle: "Band", UPC: "123", RecordType: "ep",
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "1"}},
		Extras:  map[string]any{"genre_id": 1},
	}
	am := discovery.SearchResult{
		Kind: discovery.ResultKindAlbum, Title: "Blue", Subtitle: "Band", UPC: "123", TrackCount: 5, RecordType: "album",
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderAppleMusic, ExternalID: "2"}},
		Extras:  map[string]any{"upc": "123"},
	}
	solo := discovery.SearchResult{
		Kind: discovery.ResultKindAlbum, Title: "Red", Subtitle: "Band", RecordType: "compile",
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "3"}},
	}
	primary := discovery.SearchResult{
		Kind: discovery.ResultKindArtist, Title: "Band", Popularity: 9,
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "4"}},
		Extras:  map[string]any{},
	}
	dup := discovery.SearchResult{
		Kind: discovery.ResultKindArtist, Title: "Band", Popularity: 1, ArtistMBID: "m1",
		ResolutionTier: discovery.StampResolutionTier(discovery.EntityResolutionISRC),
		Sources:        []discovery.SourceRef{{Provider: discovery.ProviderSpotify, ExternalID: "5"}},
	}

	results := make([]discovery.SearchResult, 0, 3)
	for _, e := range discovery.RankMerge([][]discovery.SearchResult{{dz, solo}, {am}}) {
		results = append(results, e.Result)
	}
	results = append(results, discovery.RankCollapseArtistDuplicates([]discovery.SearchResult{primary, dup})...)

	got, err := json.Marshal(servedItems(t, results...))
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
		result discovery.SearchResult
		want   any
	}{
		{"result duration fills a missing extra", discovery.SearchResult{Kind: discovery.ResultKindTrack, Title: "T", Duration: 226}, 226},
		{"no duration leaves the extra absent", discovery.SearchResult{Kind: discovery.ResultKindTrack, Title: "T"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := servedItem(t, tt.result).Extras["duration"]
			if !sameJSON(t, got, tt.want) {
				t.Errorf("extras.duration = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchResultToDTO_mbidKeyIsProjectedFromTheKindsTypedField(t *testing.T) {
	typed := discovery.SearchResult{RecordingMBID: "rec", ReleaseMBID: "rel", ReleaseGroupMBID: "rg", ArtistMBID: "art"}
	cases := []struct {
		kind discovery.ResultKind
		want string
	}{
		{discovery.ResultKindTrack, "rec"},
		{discovery.ResultKindAlbum, "rg"},
		{discovery.ResultKindArtist, "art"},
	}
	for _, c := range cases {
		t.Run(c.kind.String(), func(t *testing.T) {
			r := typed
			r.Kind = c.kind
			r.Title = "T"

			dto := servedItem(t, r)

			if got := dto.Extras["mbid"]; got != c.want {
				t.Errorf("extras.mbid = %v, want %q", got, c.want)
			}
		})
	}
}

func TestSearchResultToDTO_AMergedCleanEditSendsExplicitFalseOnTheWire(t *testing.T) {
	clean := discovery.SearchResult{
		Kind: discovery.ResultKindTrack, Title: "Been Waiting!", Subtitle: "Autumn!", ISRC: "QM42K1903228", Duration: 123,
		Sources:  []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "642332252"}},
		Explicit: discovery.BoolPtr(false),
	}
	unknown := discovery.SearchResult{
		Kind: discovery.ResultKindTrack, Title: "Been Waiting!", Subtitle: "Autumn!", ISRC: "QM42K1903228", Duration: 123,
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderSpotify, ExternalID: "s1"}},
	}
	for i, groups := range [][][]discovery.SearchResult{{{clean}, {unknown}}, {{unknown}, {clean}}} {
		entities := discovery.RankMerge(groups)
		if len(entities) != 1 {
			t.Fatalf("order %d: got %d entities, want one merged track", i, len(entities))
		}
		raw := servedItems(t, entities[0].Result)[0]
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
	raw := servedItems(t, discovery.SearchResult{Kind: discovery.ResultKindTrack, Title: "T"})[0]
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
