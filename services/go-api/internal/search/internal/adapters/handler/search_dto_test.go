package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/rank"
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func servedItems(t *testing.T, results ...search.SearchResult) []json.RawMessage {
	t.Helper()
	router := buildRelatedRouter(t, &fakeRelatedTracksProvider{results: results})
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

func servedItem(t *testing.T, result search.SearchResult) handler.SearchResultDTO {
	t.Helper()
	var dto handler.SearchResultDTO
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
	dz := memory.NewResult(
		search.ResultKindAlbum,
		"Blue",
		memory.WithSubtitle("Band"),
		memory.WithUPC("123"),
		memory.WithRecordType("ep"),
		memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "1"}}),
		memory.WithExtras(map[string]any{"genre_id": 1}),
	)
	am := memory.NewResult(
		search.ResultKindAlbum,
		"Blue",
		memory.WithSubtitle("Band"),
		memory.WithUPC("123"),
		memory.WithTrackCount(5),
		memory.WithRecordType("album"),
		memory.WithSources([]search.ProviderLink{{Provider: domain.ProviderAppleMusic, ExternalID: "2"}}),
		memory.WithExtras(map[string]any{"upc": "123"}),
	)
	solo := memory.NewResult(
		search.ResultKindAlbum,
		"Red",
		memory.WithSubtitle("Band"),
		memory.WithRecordType("compile"),
		memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "3"}}),
	)
	primary := memory.NewResult(
		search.ResultKindArtist,
		"Band",
		memory.WithPopularity(9),
		memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "4"}}),
		memory.WithExtras(map[string]any{}),
	)
	dup := memory.NewResult(
		search.ResultKindArtist,
		"Band",
		memory.WithPopularity(1),
		memory.WithArtistMBID("m1"),
		memory.WithResolutionTier(domain.StampResolutionTier(domain.EntityResolutionISRC)),
		memory.WithSources([]search.ProviderLink{{Provider: search.ProviderSpotify, ExternalID: "5"}}),
	)

	results := make([]search.SearchResult, 0, 3)
	for _, e := range rank.Merge([][]search.SearchResult{{dz, solo}, {am}}) {
		results = append(results, e.Result)
	}
	results = append(results, rank.CollapseArtistDuplicates([]search.SearchResult{primary, dup})...)

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
		result search.SearchResult
		want   any
	}{
		{"result duration fills a missing extra", memory.NewResult(search.ResultKindTrack, "T", memory.WithDuration(226)), 226},
		{"no duration leaves the extra absent", memory.NewResult(search.ResultKindTrack, "T"), nil},
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
	typed := memory.NewResult(
		0,
		"",
		memory.WithRecordingMBID("rec"),
		memory.WithReleaseMBID("rel"),
		memory.WithReleaseGroupMBID("rg"),
		memory.WithArtistMBID("art"),
	)
	cases := []struct {
		kind search.ResultKind
		want string
	}{
		{search.ResultKindTrack, "rec"},
		{search.ResultKindAlbum, "rg"},
		{search.ResultKindArtist, "art"},
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
	clean := memory.NewResult(
		search.ResultKindTrack,
		"Been Waiting!",
		memory.WithSubtitle("Autumn!"),
		memory.WithISRCCode("QM42K1903228"),
		memory.WithDuration(123),
		memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "642332252"}}),
		memory.WithExplicit(domain.BoolPtr(false)),
	)
	unknown := memory.NewResult(
		search.ResultKindTrack,
		"Been Waiting!",
		memory.WithSubtitle("Autumn!"),
		memory.WithISRCCode("QM42K1903228"),
		memory.WithDuration(123),
		memory.WithSources([]search.ProviderLink{{Provider: search.ProviderSpotify, ExternalID: "s1"}}),
	)
	for i, groups := range [][][]search.SearchResult{{{clean}, {unknown}}, {{unknown}, {clean}}} {
		entities := rank.Merge(groups)
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
	raw := servedItems(t, memory.NewResult(search.ResultKindTrack, "T"))[0]
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
