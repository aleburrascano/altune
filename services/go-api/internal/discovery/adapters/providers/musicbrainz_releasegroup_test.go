package providers

import "testing"

// MB is the discography completeness spine; its first-release-date is the
// authoritative year for albums no other provider carries. Regression guard for
// the map that used to drop it, leaving those albums blank and out of order.
func TestMapMBReleaseGroup_carriesFirstReleaseDate(t *testing.T) {
	r := mapMBReleaseGroup(mbReleaseGroup{
		ID:               "rg-1",
		Title:            "Some EP",
		PrimaryType:      "EP",
		FirstReleaseDate: "2019-05-01",
		ArtistCredit:     []mbArtistRef{{Name: "Che"}},
	})
	if r.ReleaseDate != "2019-05-01" {
		t.Errorf("ReleaseDate = %q, want 2019-05-01 (MB first-release-date must map through)", r.ReleaseDate)
	}
	if r.Subtitle != "Che" {
		t.Errorf("Subtitle = %q, want Che", r.Subtitle)
	}
	if r.MBID != "rg-1" {
		t.Errorf("MBID = %q, want rg-1", r.MBID)
	}
	if r.Extras["record_type"] != "ep" {
		t.Errorf("record_type = %v, want ep (lowercased primary-type, so EPs leave the Albums row)", r.Extras["record_type"])
	}
}

// A partial MB date (year only) still yields a usable release key.
func TestMapMBReleaseGroup_partialDate(t *testing.T) {
	r := mapMBReleaseGroup(mbReleaseGroup{ID: "rg-2", Title: "Early", FirstReleaseDate: "2005"})
	if r.ReleaseDate != "2005" {
		t.Errorf("ReleaseDate = %q, want 2005", r.ReleaseDate)
	}
}
