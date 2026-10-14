package domain

import (
	"reflect"
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
