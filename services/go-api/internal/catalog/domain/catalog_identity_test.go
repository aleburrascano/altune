package domain

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const validMBID = "5b11f4ce-a62d-471e-81fc-a69a8278c7da"

func TestNormalizeCatalogMBID(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"canonical uuid is kept", validMBID, validMBID, false},
		{"uppercase uuid is lowercased", strings.ToUpper(validMBID), validMBID, false},
		{"empty is rejected", "", "", true},
		{"free text is rejected", "not-a-uuid", "", true},
		{"braced uuid is rejected", "{" + validMBID + "}", "", true},
		{"urn uuid is rejected", "urn:uuid:" + validMBID, "", true},
		{"undashed uuid is rejected", strings.ReplaceAll(validMBID, "-", ""), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeCatalogMBID(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeCatalogSources_RejectsInvalid(t *testing.T) {
	tooMany := make([]CatalogSource, MaxCatalogSources+1)
	for i := range tooMany {
		tooMany[i] = CatalogSource{Provider: "deezer", ExternalID: strings.Repeat("a", i+1)}
	}
	tests := []struct {
		name    string
		sources []CatalogSource
		wantErr string
	}{
		{"unknown provider", []CatalogSource{{Provider: "napster", ExternalID: "1"}}, "provider"},
		{"empty provider", []CatalogSource{{Provider: "", ExternalID: "1"}}, "provider"},
		{"empty external id", []CatalogSource{{Provider: "deezer", ExternalID: ""}}, "external_id"},
		{"NUL in external id", []CatalogSource{{Provider: "deezer", ExternalID: "a\x00b"}}, "NUL"},
		{"external id over 200", []CatalogSource{{Provider: "deezer", ExternalID: strings.Repeat("x", 201)}}, "external_id"},
		{"too many sources", tooMany, "sources exceeds"},
		{"pixeldrain id with a path", []CatalogSource{{Provider: "pixeldrain", ExternalID: "../x"}}, "pixeldrain id"},
		{"pixeldrain id too short", []CatalogSource{{Provider: "pixeldrain", ExternalID: "ab"}}, "pixeldrain id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCatalogSources(tt.sources)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeCatalogSources_AcceptsBoundaryAndDropsExactDuplicates(t *testing.T) {
	in := []CatalogSource{
		{Provider: "deezer", ExternalID: strings.Repeat("x", 200)},
		{Provider: "deezer", ExternalID: "1"},
		{Provider: "spotify", ExternalID: "1"},
		{Provider: "deezer", ExternalID: "1"},
	}
	got, err := NormalizeCatalogSources(in)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := in[:3]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTrack_SetCatalogIdentity_StoresNormalizedFacts(t *testing.T) {
	track := &Track{}
	mbid := strings.ToUpper(validMBID)
	explicit := true
	sources := []CatalogSource{{Provider: "itunes", ExternalID: "9"}, {Provider: "itunes", ExternalID: "9"}}

	if err := track.SetCatalogIdentity(&mbid, &explicit, sources); err != nil {
		t.Fatalf("err = %v", err)
	}
	if track.CatalogMBID == nil || *track.CatalogMBID != validMBID {
		t.Errorf("CatalogMBID = %v, want %q", track.CatalogMBID, validMBID)
	}
	if track.CatalogExplicit == nil || !*track.CatalogExplicit {
		t.Errorf("CatalogExplicit = %v, want true", track.CatalogExplicit)
	}
	if len(track.CatalogSources) != 1 {
		t.Errorf("CatalogSources = %+v, want the duplicate dropped", track.CatalogSources)
	}
}

func TestTrack_SetCatalogIdentity_RejectsBadMBID(t *testing.T) {
	bad := "nope"
	if err := (&Track{}).SetCatalogIdentity(&bad, nil, nil); err == nil {
		t.Fatal("want a validation error for a non-UUID mbid")
	}
}

func TestWithSoundCloudURL(t *testing.T) {
	const pick = "https://soundcloud.com/artist/track"
	tests := []struct {
		name    string
		sources []CatalogSource
		url     string
		want    []CatalogSource
	}{
		{
			name:    "creates the entry when absent",
			sources: []CatalogSource{{Provider: "deezer", ExternalID: "1"}},
			url:     pick,
			want: []CatalogSource{
				{Provider: "deezer", ExternalID: "1"},
				{Provider: "soundcloud", ExternalID: pick, URL: pick},
			},
		},
		{
			name:    "sets it on the existing entry",
			sources: []CatalogSource{{Provider: "soundcloud", ExternalID: "123"}},
			url:     pick,
			want:    []CatalogSource{{Provider: "soundcloud", ExternalID: "123", URL: pick}},
		},
		{
			name:    "a full list is left unchanged",
			sources: fullCatalogSources(),
			url:     pick,
			want:    fullCatalogSources(),
		},
		{
			name: "a subdomain is soundcloud",
			url:  "https://m.soundcloud.com/a/b",
			want: []CatalogSource{{Provider: "soundcloud", ExternalID: "https://m.soundcloud.com/a/b", URL: "https://m.soundcloud.com/a/b"}},
		},
		{"another host is ignored", nil, "https://notsoundcloud.com/a/b", nil},
		{"empty is ignored", nil, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WithSoundCloudURL(tt.sources, tt.url); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNormalizeCatalogSources_KeepsTheURL(t *testing.T) {
	in := []CatalogSource{{Provider: "soundcloud", ExternalID: "1", URL: "https://soundcloud.com/a/b"}}
	got, err := NormalizeCatalogSources(in)
	if err != nil || !reflect.DeepEqual(got, in) {
		t.Fatalf("got %+v, %v", got, err)
	}
	bad := []CatalogSource{{Provider: "soundcloud", ExternalID: "1", URL: "javascript:alert(1)"}}
	if _, err := NormalizeCatalogSources(bad); err == nil {
		t.Fatal("want a non-http url rejected")
	}
}

func fullCatalogSources() []CatalogSource {
	out := make([]CatalogSource, MaxCatalogSources)
	for i := range out {
		out[i] = CatalogSource{Provider: "deezer", ExternalID: strconv.Itoa(i + 1)}
	}
	return out
}

func TestNormalizeCatalogSources_AcceptsAPixeldrainFileID(t *testing.T) {
	got, err := NormalizeCatalogSources([]CatalogSource{{Provider: "pixeldrain", ExternalID: "ozSpSjn7"}})
	if err != nil || len(got) != 1 || got[0].ExternalID != "ozSpSjn7" {
		t.Fatalf("NormalizeCatalogSources = %+v, %v; want the pixeldrain pin kept", got, err)
	}
}

func TestNormalizeCatalogSources_AcceptsEveryFileHostAndRejectsABadID(t *testing.T) {
	for _, ok := range []CatalogSource{{Provider: "pillows", ExternalID: "84310b5d2c21df85e4570aec299d1ead"}} {
		if _, err := NormalizeCatalogSources([]CatalogSource{ok}); err != nil {
			t.Errorf("%s pin rejected: %v", ok.Provider, err)
		}
	}
	if _, err := NormalizeCatalogSources([]CatalogSource{{Provider: "pillows", ExternalID: "../x"}}); err == nil {
		t.Error("a malformed pillows id must be rejected")
	}
}
