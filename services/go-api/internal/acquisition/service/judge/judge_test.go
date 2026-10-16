package judge

import "testing"

func artTrack(id, title, album string, length float64, explicit bool, artists ...string) Candidate {
	return Candidate{
		URL: "https://music.youtube.com/watch?v=" + id, Kind: ArtTrack,
		Title: title, Artists: artists, Album: album, Length: length, Explicit: explicit,
	}
}

type realCase struct {
	target      Target
	current     float64
	results     []Candidate
	wantURL     string
	wantReplace ReplaceOutcome
}

func realLibraryCases() map[string]realCase {
	return map[string]realCase{
		"Human: the album cut, not the 2003 remaster or the extended version": {
			target:  Target{Title: "Human", Artist: "The Human League", Album: "Crash"},
			current: 233,
			results: []Candidate{
				artTrack("a", "Human", "Crash", 266, false, "The Human League"),
				artTrack("b", "Human (Remastered 2003)", "The Very Best Of The Human League", 230, false, "The Human League"),
				artTrack("c", "Human (Extended Version)", "Crash", 305, false, "The Human League"),
			},
			wantURL: "https://music.youtube.com/watch?v=a", wantReplace: ReplaceUnsure,
		},
		"Finesse: never the remix": {
			target:  Target{Title: "Finesse", Artist: "Bruno Mars", Album: "24K Magic"},
			current: 223,
			results: []Candidate{
				artTrack("a", "Finesse", "24K Magic", 191, false, "Bruno Mars"),
				artTrack("b", "Finesse (Remix)", "Finesse (Remix)", 218, false, "Bruno Mars", "Cardi B"),
				artTrack("c", "FINESSE", "FINESSE", 159, true, "Orco", "Bartofso", "Rockywhereyoubeen"),
			},
			wantURL: "https://music.youtube.com/watch?v=a", wantReplace: ReplaceUnsure,
		},
		"Diamonds: the album track, but our own old download cannot justify a swap": {
			target:  Target{Title: "Diamonds", Artist: "Rihanna", Album: "Unapologetic"},
			current: 283,
			results: []Candidate{
				artTrack("a", "Diamonds", "Unapologetic", 226, false, "Rihanna"),
				artTrack("b", "DIAMONDS (Sped Up)", "DIAMONDS (Sped Up)", 103, true, "Maktivity"),
			},
			wantURL: "https://music.youtube.com/watch?v=a", wantReplace: ReplaceUnsure,
		},
		"I Ran: a compilation cut has no match": {
			target:  Target{Title: "I Ran (So Far Away)", Artist: "A Flock Of Seagulls", Album: "80s: 100 Remixes"},
			current: 238,
			results: []Candidate{
				artTrack("a", "I Ran (So Far Away)", "A Flock of Seagulls", 305, false, "A Flock of Seagulls"),
				artTrack("b", "I Ran (So Far Away) (Re-Recorded)", "100 '80s Classics (Re-Recorded / Remastered Versions)", 303, false, "A Flock Of Seagulls"),
			},
		},
		"More Than a Feeling: a greatest-hits edit has no match": {
			target:  Target{Title: "More Than a Feeling", Artist: "Boston", Album: "Greatest Hits"},
			current: 217,
			results: []Candidate{artTrack("a", "More Than a Feeling", "Boston", 286, false, "Boston")},
		},
		"JACKIE BROWN: the catalog's 169 s rules out the 3:11 art track": {
			target:  Target{Title: "JACKIE BROWN", Artist: "Brent Faiyaz", Album: "WASTELAND", Length: 169},
			current: 169,
			results: []Candidate{artTrack("a", "JACKIE BROWN", "WASTELAND", 191, true, "Brent Faiyaz")},
		},
		"Nonstop: explicit over clean when both match, same cut as the current file": {
			target:  Target{Title: "Nonstop", Artist: "Drake", Album: "Scorpion"},
			current: 239,
			results: []Candidate{
				artTrack("clean", "Nonstop", "Scorpion", 239, false, "Drake"),
				artTrack("explicit", "Nonstop", "Scorpion", 239, true, "Drake"),
			},
			wantURL: "https://music.youtube.com/watch?v=explicit", wantReplace: ReplaceSwap,
		},
		"Cold Heart (PNAU Remix): a single's album is its title, so it cannot disagree": {
			target:  Target{Title: "Cold Heart (PNAU Remix)", Artist: "Elton John", Album: "Cold Heart (PNAU Remix)"},
			current: 203,
			results: []Candidate{
				artTrack("a", "Cold Heart (PNAU Remix)", "The Lockdown Sessions", 203, false, "Elton John", "Dua Lipa"),
				artTrack("b", "Hold Me Closer", "The Lockdown Sessions", 203, false, "Elton John", "Britney Spears"),
			},
			wantURL: "https://music.youtube.com/watch?v=a", wantReplace: ReplaceSwap,
		},
		"Solo (Reprise): the reprise, not Solo": {
			target:  Target{Title: "Solo (Reprise)", Artist: "Frank Ocean", Album: "Blonde"},
			current: 79,
			results: []Candidate{
				artTrack("a", "Solo (Reprise)", "Blonde", 79, false, "Frank Ocean"),
				artTrack("b", "Solo", "Blonde", 258, false, "Frank Ocean"),
			},
			wantURL: "https://music.youtube.com/watch?v=a", wantReplace: ReplaceSwap,
		},
		"Valentina: the plain track over the bonus version with a feature": {
			target:  Target{Title: "Valentina", Artist: "Daniel Caesar", Album: "NEVER ENOUGH (Bonus Version)"},
			current: 153,
			results: []Candidate{
				artTrack("a", "Valentina", "NEVER ENOUGH", 155, false, "Daniel Caesar"),
				artTrack("b", "Valentina (Bonus) (feat. Rick Ross)", "NEVER ENOUGH (Bonus Version)", 237, true, "Daniel Caesar", "Rick Ross"),
			},
			wantURL: "https://music.youtube.com/watch?v=a", wantReplace: ReplaceSwap,
		},
	}
}

func TestChoose_RealLibraryCases(t *testing.T) {
	for name, tc := range realLibraryCases() {
		t.Run(name, func(t *testing.T) {
			got, decision, ok := Choose(tc.target, tc.results)
			if tc.wantURL == "" {
				if ok {
					t.Fatalf("chose %s (%s), want no match", got.URL, decision.Reason)
				}
				return
			}
			if !ok || got.URL != tc.wantURL {
				t.Fatalf("chose %q ok=%v, want %q", got.URL, ok, tc.wantURL)
			}
			if r := Replace(tc.target, tc.current, got); r != tc.wantReplace {
				t.Errorf("replace = %s, want %s", r, tc.wantReplace)
			}
		})
	}
}

func TestReplace_ACatalogLengthJustifiesASwap(t *testing.T) {
	target := Target{Title: "Diamonds", Artist: "Rihanna", Album: "Unapologetic", Length: 226}
	chosen := artTrack("a", "Diamonds", "Unapologetic", 226, false, "Rihanna")
	if r := Replace(target, 283, chosen); r != ReplaceSwap {
		t.Errorf("replace = %s, want swap once the catalog vouches for the length", r)
	}
}

func TestEvaluate_UploadsNeedAFingerprintMatch(t *testing.T) {
	d := Evaluate(Target{Title: "Nonstop", Artist: "Drake"},
		Candidate{Kind: Upload, Title: "Nonstop", Artists: []string{"Drake"}, Length: 273})
	if d.Outcome != Verify {
		t.Errorf("decision = %+v, want verify", d)
	}
}

func TestEvaluate_RejectsWithTheReason(t *testing.T) {
	devil := Target{Title: "Devil In A New Dress", Artist: "Kanye West", Album: "My Beautiful Dark Twisted Fantasy"}
	cases := []struct {
		name string
		t    Target
		c    Candidate
		want string
	}{
		{
			"common words are part of the title", devil,
			Candidate{Kind: ArtTrack, Title: "Devil New Dress", Artists: []string{"Kanye West"}, Album: devil.Album},
			"different title",
		},
		{
			"another artist", devil,
			Candidate{Kind: ArtTrack, Title: "Devil In A New Dress", Artists: []string{"Rick Ross"}, Album: devil.Album},
			"artist not credited",
		},
		{
			"another version",
			Target{Title: "Finesse", Artist: "Bruno Mars", Length: 191},
			Candidate{Kind: ArtTrack, Title: "Finesse (Remix)", Artists: []string{"Bruno Mars", "Cardi B"}, Length: 218},
			"different version: want none, got remix",
		},
		{
			"catalog length disagrees",
			Target{Title: "I Ran (So Far Away)", Artist: "A Flock of Seagulls", Length: 238},
			Candidate{Kind: ArtTrack, Title: "I Ran (So Far Away)", Artists: []string{"A Flock of Seagulls"}, Length: 305},
			"length disagrees with the catalog length",
		},
		{
			"no catalog length and another album",
			Target{Title: "More Than a Feeling", Artist: "Boston", Album: "Greatest Hits"},
			Candidate{Kind: ArtTrack, Title: "More Than a Feeling", Artists: []string{"Boston"}, Album: "Boston", Length: 286},
			"no catalog length and another album: cannot confirm the cut",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if d := Evaluate(tc.t, tc.c); d.Outcome != Reject || d.Reason != tc.want {
				t.Errorf("decision = %+v, want reject %q", d, tc.want)
			}
		})
	}
}

func TestEvaluate_LengthAgreesOnTheAuthoritativeWindow(t *testing.T) {
	target := Target{Title: "Finesse", Artist: "Bruno Mars", Album: "24K Magic", Length: 200}
	tests := []struct {
		name   string
		length float64
		want   Outcome
	}{
		{"6 s off agrees (3% of 200)", 206, Accept},
		{"7 s off disagrees", 207, Reject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := artTrack("x", "Finesse", "24K Magic", tt.length, false, "Bruno Mars")
			if got := Evaluate(target, c).Outcome; got != tt.want {
				t.Errorf("Evaluate = %v, want %v", got, tt.want)
			}
		})
	}
}
