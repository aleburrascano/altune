package main

import "testing"

func TestArtistMatches(t *testing.T) {
	cases := []struct {
		name     string
		stored   string
		provider []string
		want     bool
	}{
		{"identical", "Kool & The Gang", []string{"Kool & The Gang"}, true},
		{"case and accents", "Beyoncé", []string{"BEYONCE"}, true},
		{"a credited collaborator on the provider side", "Drake", []string{"Drake & Future"}, true},
		{"a collaborator list on our side", "Drake, Future", []string{"Future"}, true},
		{"a musicbrainz alias", "Wham!", []string{"George Michael", "Wham!"}, true},
		{"a cover by someone else", "Wham!", []string{"Jimmy Eat World"}, false},
		{"the stored uploader is not the artist", "MINDA (민다)", []string{"aespa"}, false},
		{"no provider artist", "Drake", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := artistMatches(tc.stored, tc.provider); got != tc.want {
				t.Fatalf("artistMatches(%q, %v) = %v", tc.stored, tc.provider, got)
			}
		})
	}
}

func TestDistance_WeightsRankAnExactCandidateFirst(t *testing.T) {
	track := celebration()
	exact := distance(track, catalogCelebration("1"))
	lengthOff := distance(track, with(catalogCelebration("2"), func(c *candidate) { c.LengthSeconds = 296 }))
	titleOff := distance(track, with(catalogCelebration("3"), func(c *candidate) { c.Title = "Celebrations" }))
	idOff := distance(track, with(catalogCelebration("4"), func(c *candidate) { c.ISRC = "USXX00000000" }))
	if exact != 0 {
		t.Fatalf("exact = %v, want 0", exact)
	}
	if !(exact < titleOff && exact < lengthOff && lengthOff < idOff && titleOff < idOff) {
		t.Fatalf("exact %v title %v length %v id %v", exact, titleOff, lengthOff, idOff)
	}
}

func TestDistance_IDMismatchWeighsFiveOfThirteen(t *testing.T) {
	track := celebration()
	got := distance(track, with(catalogCelebration("4"), func(c *candidate) { c.ISRC = "USXX00000000" }))
	if got < 5.0/13-0.0001 || got > 5.0/13+0.0001 {
		t.Fatalf("distance = %v, want 5/13", got)
	}
}
