package providers

import "testing"

func mbRef(name, join, mbid string) mbArtistRef {
	r := mbArtistRef{Name: name, JoinPhrase: join}
	if mbid != "" {
		r.Artist = &mbArtistLink{ID: mbid, Name: name}
	}
	return r
}

func TestExtractMBFeatured(t *testing.T) {
	tests := []struct {
		name    string
		credits []mbArtistRef
		want    []string // expected featured names, in order
	}{
		{
			name:    "no featured — solo",
			credits: []mbArtistRef{mbRef("Drake", "", "d")},
			want:    nil,
		},
		{
			name:    "single feat",
			credits: []mbArtistRef{mbRef("Drake", " feat. ", "d"), mbRef("Rihanna", "", "r")},
			want:    []string{"Rihanna"},
		},
		{
			name: "feat group carries to all after boundary",
			credits: []mbArtistRef{
				mbRef("Main", " feat. ", "m"),
				mbRef("A", " & ", "a"),
				mbRef("B", "", "b"),
			},
			want: []string{"A", "B"},
		},
		{
			name: "collaboration (& not feat) yields none",
			credits: []mbArtistRef{
				mbRef("A", " & ", "a"),
				mbRef("B", "", "b"),
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMBFeatured(tt.credits)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d featured, want %d (%v)", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				if got[i].Name != w {
					t.Errorf("featured[%d].Name = %q, want %q", i, got[i].Name, w)
				}
			}
		})
	}
}

func TestExtractMBFeatured_CarriesMBID(t *testing.T) {
	credits := []mbArtistRef{mbRef("Drake", " feat. ", "d"), mbRef("Rihanna", "", "rih-mbid")}
	got := extractMBFeatured(credits)
	if len(got) != 1 || got[0].MBID != "rih-mbid" {
		t.Fatalf("expected featured with MBID rih-mbid, got %+v", got)
	}
}
