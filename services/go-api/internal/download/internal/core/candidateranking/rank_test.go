package candidateranking_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/titlemarkers"
	"strings"
	"testing"
)

func TestRank_PicksTheBestCandidate(t *testing.T) {
	tests := []struct {
		name       string
		track      download.RankingTrack
		candidates []download.AudioCandidate
		wantNil    bool
		wantTitle  string
		wantReason string
	}{
		{
			name: "topic channel preferred when identity >= 60",
			track: download.RankingTrack{
				Title:          "Blinding Lights",
				Artist:         "The Weeknd",
				ExpectedLength: 200,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "The Weeknd - Blinding Lights",
					Channel:    "TheWeekndVEVO",
					Duration:   203,
					URL:        "https://youtube.com/watch?v=vevo1",
					Categories: []string{"Music"},
					ViewCount:  500_000_000,
				},
				{
					Title:      "Blinding Lights",
					Channel:    "The Weeknd - Topic",
					Duration:   200,
					URL:        "https://youtube.com/watch?v=topic1",
					Categories: []string{"Music"},
					ViewCount:  10_000_000,
				},
			},
			wantTitle:  "Blinding Lights",
			wantReason: "topic channel candidate should be preferred over VEVO when identity >= 60",
		},
		{
			name: "exact duration match scores highest among non-topic candidates",
			track: download.RankingTrack{
				Title:          "Starboy",
				Artist:         "The Weeknd",
				ExpectedLength: 230,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "The Weeknd - Starboy (Extended Remix)",
					Channel:    "RandomUploader",
					Duration:   300,
					URL:        "https://youtube.com/watch?v=far",
					Categories: []string{"Music"},
					ViewCount:  1_000,
				},
				{
					Title:      "The Weeknd - Starboy (Audio)",
					Channel:    "AnotherUploader",
					Duration:   231,
					URL:        "https://youtube.com/watch?v=close",
					Categories: []string{"Music"},
					ViewCount:  1_000,
				},
			},
			wantTitle:  "The Weeknd - Starboy (Audio)",
			wantReason: "candidate with duration within the tight length window (2s) should score higher than one 70s off",
		},
		{
			name: "no candidates returns nil",
			track: download.RankingTrack{
				Title:  "Nonexistent",
				Artist: "Nobody",
			},
			candidates: []download.AudioCandidate{},
			wantNil:    true,
			wantReason: "empty candidate list must return nil",
		},
		{
			name: "all candidates below identity threshold filtered out",
			track: download.RankingTrack{
				Title:          "Save Your Tears",
				Artist:         "The Weeknd",
				ExpectedLength: 215,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "Cooking Tutorial Episode 47",
					Channel:    "CookingChannel",
					Duration:   215,
					URL:        "https://youtube.com/watch?v=cook1",
					Categories: []string{"Howto & Style"},
					ViewCount:  50_000,
				},
				{
					Title:      "Random Podcast About Finance",
					Channel:    "FinanceBro",
					Duration:   3600,
					URL:        "https://youtube.com/watch?v=fin1",
					Categories: []string{"Education"},
					ViewCount:  10_000,
				},
			},
			wantNil:    true,
			wantReason: "candidates with titles completely unrelated to track should score identity < 60 and be filtered out",
		},
		{
			name: "highest composite score wins among non-topic candidates",
			track: download.RankingTrack{
				Title:          "Die For You",
				Artist:         "The Weeknd",
				ExpectedLength: 260,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "The Weeknd - Die For You (Official Video)",
					Channel:    "TheWeekndVEVO",
					Duration:   262,
					URL:        "https://youtube.com/watch?v=vevo2",
					Categories: []string{"Music"},
					ViewCount:  900_000_000,
				},
				{
					Title:      "The Weeknd - Die For You (Lyrics)",
					Channel:    "LyricsChannel",
					Duration:   261,
					URL:        "https://youtube.com/watch?v=lyrics1",
					Categories: []string{"Music"},
					ViewCount:  50_000_000,
				},
			},
			wantTitle:  "The Weeknd - Die For You (Official Video)",
			wantReason: "VEVO channel (0.8) + highest views + Music category should beat a lyrics channel (0.3)",
		},
		{
			name: "nil candidates returns nil",
			track: download.RankingTrack{
				Title:  "Test",
				Artist: "Test",
			},
			candidates: nil,
			wantNil:    true,
			wantReason: "nil candidate slice must return nil",
		},
		{
			name: "topic channel wins over VEVO even with fewer views",
			track: download.RankingTrack{
				Title:          "After Hours",
				Artist:         "The Weeknd",
				ExpectedLength: 361,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "The Weeknd - After Hours (Official Video)",
					Channel:    "TheWeekndVEVO",
					Duration:   362,
					URL:        "https://youtube.com/watch?v=vevo3",
					Categories: []string{"Music"},
					ViewCount:  800_000_000,
				},
				{
					Title:      "After Hours",
					Channel:    "The Weeknd - Topic",
					Duration:   361,
					URL:        "https://youtube.com/watch?v=topic2",
					Categories: []string{"Music"},
					ViewCount:  5_000_000,
				},
			},
			wantTitle:  "After Hours",
			wantReason: "topic channel is unconditionally preferred over non-topic when identity passes threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := best(tt.track, tt.candidates...)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got candidate with title %q — %s", got.Title, tt.wantReason)
				}
				return
			}

			if got == nil {
				t.Fatalf("expected candidate with title %q, got nil — %s", tt.wantTitle, tt.wantReason)
			}
			if got.Title != tt.wantTitle {
				t.Errorf("got title %q, want %q — %s", got.Title, tt.wantTitle, tt.wantReason)
			}
		})
	}
}

func TestRankCandidates_TieIsDeterministicAndPrefersExpectedLength(t *testing.T) {
	track := download.RankingTrack{Title: "Blinding Lights", Artist: "The Weeknd", ExpectedLength: 200}
	candidates := []download.AudioCandidate{
		{
			Title:      "Blinding Lights",
			Channel:    "The Weeknd - Topic",
			Duration:   240,
			URL:        "https://youtube.com/watch?v=slowed",
			Categories: []string{"Music"},
		},
		{
			Title:      "Blinding Lights",
			Channel:    "The Weeknd - Topic",
			Duration:   200,
			URL:        "https://youtube.com/watch?v=master",
			Categories: []string{"Music"},
		},
	}

	for i := 0; i < 50; i++ {
		ranked, _ := rank(track, candidates...)
		if len(ranked) != 2 {
			t.Fatalf("expected both candidates ranked, got %d", len(ranked))
		}
		if ranked[0].URL != "https://youtube.com/watch?v=master" {
			t.Fatalf("run %d selected %q; the master must win a full identity tie on expected length",
				i, ranked[0].URL)
		}
	}
}

func TestRankCandidates_TotalTieIsStillDeterministic(t *testing.T) {
	track := download.RankingTrack{Title: "Song", Artist: "Artist"}
	candidates := []download.AudioCandidate{
		{Title: "Artist - Song", Channel: "Artist - Topic", URL: "https://b.example/x"},
		{Title: "Artist - Song", Channel: "Artist - Topic", URL: "https://a.example/x"},
	}

	first, _ := rank(track, candidates...)
	if len(first) == 0 {
		t.Fatal("expected candidates to survive the identity gate")
	}
	for i := 0; i < 50; i++ {
		again, _ := rank(track, candidates...)
		if again[0].URL != first[0].URL {
			t.Fatalf("run %d picked %q, first run picked %q — ranking is not deterministic",
				i, again[0].URL, first[0].URL)
		}
	}
}

func TestAcquisitionMatchingRegression(t *testing.T) {
	tests := []struct {
		name         string
		track        download.RankingTrack
		candidates   []download.AudioCandidate
		wantNil      bool
		wantChannel  string
		wantContains string
		description  string
	}{
		{
			name: "die hard by intended artist prefers correct Topic channel",
			track: download.RankingTrack{
				Title:          "Die Hard",
				Artist:         "Dr. Dre",
				ExpectedLength: 280,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "DIE HARD",
					Channel:    "Kendrick Lamar - Topic",
					Duration:   240,
					URL:        "https://youtube.com/watch?v=kendrick",
					Categories: []string{"Music"},
					ViewCount:  65_000_000,
				},
				{
					Title:      "Die Hard",
					Channel:    "Dr. Dre - Topic",
					Duration:   282,
					URL:        "https://youtube.com/watch?v=dre",
					Categories: []string{"Music"},
					ViewCount:  5_000_000,
				},
			},
			wantChannel:  "Dr. Dre - Topic",
			wantContains: "Die Hard",
			description:  "must prefer Topic channel matching track artist over wrong-artist Topic channel",
		},
		{
			name: "die hard kendrick is correct when artist is kendrick",
			track: download.RankingTrack{
				Title:          "DIE HARD",
				Artist:         "Kendrick Lamar",
				ExpectedLength: 238,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "Kendrick Lamar - DIE HARD (Official Audio)",
					Channel:    "Kendrick Lamar - Topic",
					Duration:   240,
					URL:        "https://youtube.com/watch?v=kendrick",
					Categories: []string{"Music"},
					ViewCount:  65_000_000,
				},
				{
					Title:      "Dr. Dre - Die Hard ft. Eminem",
					Channel:    "Dr. Dre - Topic",
					Duration:   282,
					URL:        "https://youtube.com/watch?v=dre",
					Categories: []string{"Music"},
					ViewCount:  5_000_000,
				},
			},
			wantChannel:  "Kendrick Lamar - Topic",
			wantContains: "Kendrick",
			description:  "must pick Kendrick's Topic channel when artist IS Kendrick",
		},
		{
			name: "topic channel beats vevo for exact match",
			track: download.RankingTrack{
				Title:          "Blinding Lights",
				Artist:         "The Weeknd",
				ExpectedLength: 200,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "The Weeknd - Blinding Lights (Official Video)",
					Channel:    "TheWeekndVEVO",
					Duration:   203,
					URL:        "https://youtube.com/watch?v=vevo",
					Categories: []string{"Music"},
					ViewCount:  500_000_000,
				},
				{
					Title:      "Blinding Lights",
					Channel:    "The Weeknd - Topic",
					Duration:   200,
					URL:        "https://youtube.com/watch?v=topic",
					Categories: []string{"Music"},
					ViewCount:  10_000_000,
				},
			},
			wantChannel: "The Weeknd - Topic",
			description: "topic channel is preferred over VEVO",
		},
		{
			name: "title-only match penalized below combined match",
			track: download.RankingTrack{
				Title:          "Lose Yourself",
				Artist:         "Eminem",
				ExpectedLength: 326,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "Eminem - Lose Yourself (Official Video)",
					Channel:    "EminemVEVO",
					Duration:   328,
					URL:        "https://youtube.com/watch?v=eminem",
					Categories: []string{"Music"},
					ViewCount:  1_000_000_000,
				},
				{
					Title:      "Lose Yourself - Motivational Speech",
					Channel:    "MotivationHub",
					Duration:   600,
					URL:        "https://youtube.com/watch?v=motivation",
					Categories: []string{"Education"},
					ViewCount:  50_000_000,
				},
			},
			wantContains: "Eminem",
			description:  "combined artist+title match must beat title-only non-music result",
		},
		{
			name: "unrelated candidates filtered out",
			track: download.RankingTrack{
				Title:          "Save Your Tears",
				Artist:         "The Weeknd",
				ExpectedLength: 215,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "Cooking Tutorial Episode 47",
					Channel:    "CookingChannel",
					Duration:   215,
					URL:        "https://youtube.com/watch?v=cook1",
					Categories: []string{"Howto & Style"},
					ViewCount:  50_000,
				},
			},
			wantNil:     true,
			description: "completely unrelated candidates must be filtered by identity threshold",
		},
		{
			name: "feat track prefers candidate naming the featured artist",
			track: download.RankingTrack{
				Title:          "Smaxk Or Die (feat. Playboi Carti)",
				Artist:         "Fatt Smaxk",
				ExpectedLength: 0,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "Fatt Smaxk - Smaxk Or Die (Official Music Video) | [Dir. By Kharkee]",
					Channel:    "FattSmaxkVEVO",
					Duration:   150,
					URL:        "https://youtube.com/watch?v=solo",
					Categories: []string{"Music"},
					ViewCount:  2_000_000,
				},
				{
					Title:      "Fatt Smaxk - Smaxk Or Die (feat. Playboi Carti)",
					Channel:    "FattSmaxk",
					Duration:   181,
					URL:        "https://youtube.com/watch?v=feat",
					Categories: []string{"Music"},
					ViewCount:  50_000,
				},
			},
			wantContains: "feat. Playboi Carti",
			description:  "a (feat. X) track must prefer a candidate naming X over an equally-scored, higher-view solo cut",
		},
		{
			name: "same title different artists picks correct one by channel",
			track: download.RankingTrack{
				Title:          "Circles",
				Artist:         "Post Malone",
				ExpectedLength: 215,
			},
			candidates: []download.AudioCandidate{
				{
					Title:      "Circles",
					Channel:    "Post Malone - Topic",
					Duration:   215,
					URL:        "https://youtube.com/watch?v=postmalone",
					Categories: []string{"Music"},
					ViewCount:  20_000_000,
				},
				{
					Title:      "Circles",
					Channel:    "Mac Miller - Topic",
					Duration:   283,
					URL:        "https://youtube.com/watch?v=macmiller",
					Categories: []string{"Music"},
					ViewCount:  15_000_000,
				},
			},
			wantChannel: "Post Malone - Topic",
			description: "when both are Topic channels, prefer the one matching the expected artist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := best(tt.track, tt.candidates...)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %q by %q — %s", got.Title, got.Channel, tt.description)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected a candidate, got nil — %s", tt.description)
			}

			if tt.wantChannel != "" && got.Channel != tt.wantChannel {
				t.Errorf("wrong channel: got %q, want %q — %s", got.Channel, tt.wantChannel, tt.description)
			}
			if tt.wantContains != "" && !strings.Contains(got.Title, tt.wantContains) {
				t.Errorf("title %q does not contain %q — %s", got.Title, tt.wantContains, tt.description)
			}
		})
	}
}

func TestRank_SoundCloudFillsGap(t *testing.T) {
	track := download.RankingTrack{Title: "Fell In Love", Artist: "Lil Tecca", ExpectedLength: 150}

	candidates := []download.AudioCandidate{
		{
			Title:    "Lil Tecca - Fell In Love",
			Channel:  "Lil Tecca",
			Duration: 150,
			URL:      "https://soundcloud.com/liltecca/fell-in-love",
		},
	}

	got := best(track, candidates...)
	if got == nil {
		t.Fatal("expected the SoundCloud candidate to be selected, got nil")
	}
	if got.URL != "https://soundcloud.com/liltecca/fell-in-love" {
		t.Fatalf("selected wrong candidate: %+v", got)
	}
}

func TestRank_TopicChannelBeatsSoundCloud(t *testing.T) {
	track := download.RankingTrack{Title: "Blinding Lights", Artist: "The Weeknd", ExpectedLength: 200}

	candidates := []download.AudioCandidate{
		{
			Title:    "The Weeknd - Blinding Lights",
			Channel:  "The Weeknd",
			Duration: 200,
			URL:      "https://soundcloud.com/x/blinding-lights",
		},
		{
			Title:      "Blinding Lights",
			Channel:    "The Weeknd - Topic",
			Duration:   200,
			Categories: []string{"Music"},
			URL:        "https://youtube.com/watch?v=topic",
		},
	}

	got := best(track, candidates...)
	if got == nil {
		t.Fatal("expected a candidate to be selected, got nil")
	}
	if got.URL != "https://youtube.com/watch?v=topic" {
		t.Fatalf("Topic channel must win over SoundCloud; got %+v", got)
	}
}

func TestUnrequestedQualifiers_InstrumentalAndAccuracyClaimAreVetoed(t *testing.T) {
	veto, qualifiers := download.UnrequestedQualifiers("Rollacoasta", "prettifun", "prettifun - Rollacoasta (Instrumental) [100% Accurate]")
	assertLabels(t, "veto", veto, []string{"instrumental", "100% accurate"})
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestUnrequestedQualifiers_ZeroWidthSpaceInsideQualifierIsStillVetoed(t *testing.T) {
	veto, qualifiers := download.UnrequestedQualifiers("Rollacoasta", "prettifun", "prettifun - Rollacoasta (Instru\u200bmental)")
	assertLabels(t, "veto", veto, []string{"instrumental"})
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestUnrequestedQualifiers_ReactionVideoIsVetoed(t *testing.T) {
	veto, qualifiers := download.UnrequestedQualifiers("8AM In Charlotte", "Drake", "ImDontai Reacts To Drake 8AM In Charlotte")
	assertLabels(t, "veto", veto, []string{"reacts"})
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestUnrequestedQualifiers_RadioEditIsQualifierNotEdit(t *testing.T) {
	veto, qualifiers := download.UnrequestedQualifiers("Song", "Someone", "Song (Radio Edit)")
	assertLabels(t, "veto", veto, nil)
	assertLabels(t, "qualifiers", qualifiers, []string{"radio edit"})
}

func TestUnrequestedQualifiers_RequestedWordInTrackTitleIsNotAQualifier(t *testing.T) {
	veto, qualifiers := download.UnrequestedQualifiers("Live Forever", "Oasis", "Live Forever (Remastered)")
	assertLabels(t, "veto", veto, nil)
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestUnrequestedQualifiers_RequestedWordAtEndOfTrackTitleIsNotAQualifier(t *testing.T) {
	veto, qualifiers := download.UnrequestedQualifiers("Forever Live", "Oasis", "Forever Live (Remastered)")
	assertLabels(t, "veto", veto, nil)
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestRankCandidates_AcousticLosesToTheMasterOnTheSameTopicChannel(t *testing.T) {
	track := download.RankingTrack{Title: "Sunglasses at Night", Artist: "Corey Hart", ExpectedLength: 232}
	candidates := []download.AudioCandidate{
		{
			Title:      "Sunglasses at Night (Acoustic Version)",
			Channel:    "Corey Hart - Topic",
			Duration:   232,
			URL:        "https://youtube.com/watch?v=acoustic000",
			Categories: []string{"Music"},
			ViewCount:  9_000_000,
		},
		{
			Title:      "Sunglasses at Night",
			Channel:    "Corey Hart - Topic",
			Duration:   232,
			URL:        "https://youtube.com/watch?v=master00000",
			Categories: []string{"Music"},
			ViewCount:  1_000,
		},
	}

	ranked, _ := rank(track, candidates...)
	if len(ranked) != 1 {
		t.Fatalf("ranked = %d, want exactly the master (the acoustic upload is vetoed outright)", len(ranked))
	}
	if ranked[0].Title != "Sunglasses at Night" {
		t.Fatalf("selected %q — an identical-length acoustic take must never beat the master",
			ranked[0].Title)
	}
}

func TestRankCandidates_MusicVideoLosesToPlainAudio(t *testing.T) {
	track := download.RankingTrack{Title: "Never Surrender", Artist: "Corey Hart", ExpectedLength: 262}
	candidates := []download.AudioCandidate{
		{
			Title:      "Never Surrender (Official Music Video)",
			Channel:    "Corey Hart - Topic",
			Duration:   262,
			URL:        "https://youtube.com/watch?v=video000000",
			Categories: []string{"Music"},
			ViewCount:  8_000_000,
		},
		{
			Title:      "Never Surrender",
			Channel:    "Corey Hart - Topic",
			Duration:   262,
			URL:        "https://youtube.com/watch?v=audio000000",
			Categories: []string{"Music"},
			ViewCount:  3_000,
		},
	}

	ranked, _ := rank(track, candidates...)
	if ranked[0].Title != "Never Surrender" {
		t.Fatalf("selected %q — the plain audio must outrank the video container", ranked[0].Title)
	}
}

func TestRankCandidates_ProvenanceStillBeatsQualifierDistanceOffTopic(t *testing.T) {
	track := download.RankingTrack{Title: "Die For You", Artist: "The Weeknd", ExpectedLength: 260}
	candidates := []download.AudioCandidate{
		{
			Title:      "The Weeknd - Die For You (Lyrics)",
			Channel:    "LyricsChannel",
			Duration:   261,
			URL:        "https://youtube.com/watch?v=lyrics00000",
			Categories: []string{"Music"},
			ViewCount:  50_000_000,
		},
		{
			Title:      "The Weeknd - Die For You (Official Video)",
			Channel:    "TheWeekndVEVO",
			Duration:   262,
			URL:        "https://youtube.com/watch?v=vevo0000000",
			Categories: []string{"Music"},
			ViewCount:  900_000_000,
		},
	}

	ranked, _ := rank(track, candidates...)
	if ranked[0].Channel != "TheWeekndVEVO" {
		t.Fatalf("selected %q — off Topic, label provenance outranks a shorter qualifier list", ranked[0].Channel)
	}
}

func TestUnrequestedQualifiers(t *testing.T) {
	tests := []struct {
		name           string
		title, artist  string
		candidate      string
		wantVeto       []string
		wantQualifiers []string
	}{
		{"unbracketed slowed and reverb", "Song", "Someone", "Someone - Song slowed + reverb", []string{"slowed", "reverb"}, nil},
		{"punctuation folded", "Song", "Someone", "Song 100 accurate", []string{"100% accurate"}, nil},
		{"fullwidth brackets", "Song", "Someone", "Song （Live）", []string{"live"}, nil},
		{"remixed labelled remix", "Song", "Someone", "Song remixed", []string{"remix"}, nil},
		{"mix is a whole word", "Song", "Someone", "Song (Remix) mix", []string{"remix", "mix"}, nil},
		{"remix does not match mix", "Song", "Someone", "Song (Remix)", []string{"remix"}, nil},
		{"acapella spellings share a label", "Song", "Someone", "Song acapella (A Cappella)", []string{"a cappella"}, nil},
		{"title order and deduplicated", "Song", "Someone", "Song (Live) [Snippet] live leaked leak", []string{"live", "snippet", "leak"}, nil},
		{"phrases", "Song", "Someone", "Someone Type Beat - Song In The Booth Sped Up 8D", []string{"type beat", "in the booth", "sped up", "8d"}, nil},
		{"lone phrase word is not the phrase", "Song", "Someone", "Song in the studio type", nil, nil},
		{"veto and qualifiers split", "Song", "Someone", "Song (Extended Version) [Karaoke]", []string{"karaoke"}, []string{"extended", "version"}},
		{"bare edit is a qualifier", "Song", "Someone", "Song (Edit)", nil, []string{"edit"}},
		{"artist word is requested", "Song", "Live", "Live - Song", nil, nil},
		{"title phrase is requested", "Slowed Down", "Someone", "Slowed Down (Reverb)", []string{"reverb"}, nil},
		{"requested qualifier in title brackets", "Song (Remix)", "Someone", "Song (Remix)", nil, nil},
		{"feature credit is never a qualifier", "Song", "Someone", "Song (feat. Live Cover Band)", nil, nil},
		{"unbracketed feature credit", "Song", "Someone", "Song ft. Live Band", nil, nil},
		{"feature credit stops at a separator", "Song", "Someone", "Song ft. Other - Instrumental", []string{"instrumental"}, nil},
		{"fullwidth feature credit stops at its bracket", "Song", "Someone", "Song （feat. Other） （Instrumental）", []string{"instrumental"}, nil},
		{"remastered is not a qualifier", "Song", "Someone", "Song (Remastered 2011)", nil, nil},
		{"empty candidate", "Song", "Someone", "", nil, nil},
		{"empty track", "", "", "Song (Cover)", []string{"cover"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			veto, qualifiers := download.UnrequestedQualifiers(tt.title, tt.artist, tt.candidate)
			assertLabels(t, "veto of "+tt.candidate, veto, tt.wantVeto)
			assertLabels(t, "qualifiers of "+tt.candidate, qualifiers, tt.wantQualifiers)
		})
	}
}

func FuzzUnrequestedQualifiers(f *testing.F) {
	f.Add("Rollacoasta", "prettifun", "prettifun - Rollacoasta (Instrumental) [100% Accurate]")
	f.Add("8AM In Charlotte", "Drake", "ImDontai Reacts To Drake 8AM In Charlotte")
	f.Add("Song", "Someone", "Song （feat. Other） （Radio Edit） a")
	labels := titlemarkers.KnownLabels()
	f.Fuzz(func(t *testing.T, title, artist, candidate string) {
		veto, qualifiers := download.UnrequestedQualifiers(title, artist, candidate)
		seen := make(map[string]bool)
		for _, label := range append(veto, qualifiers...) {
			if !labels[label] || seen[label] {
				t.Fatalf("acquisition.UnrequestedQualifiers(%q, %q, %q) = %v, %v: %q is unknown or repeated", title, artist, candidate, veto, qualifiers, label)
			}
			seen[label] = true
		}
	})
}

func TestUnrequestedQualifiers_TitlesFromTheOutsideWorld(t *testing.T) {
	longCandidate := "Song" + strings.Repeat(" (Live)", 5000) + " [Instrumental]"
	tests := []struct {
		name           string
		title, artist  string
		candidate      string
		wantVeto       []string
		wantQualifiers []string
	}{
		{"all caps instrumental upload is vetoed", "SPEED DEMON", "Lucy Bedroque", "Lucy Bedroque - SPEED DEMON (INSTRUMENTAL)", []string{"instrumental"}, nil},
		{"lower case track title still requests the word", "live forever", "oasis", "LIVE FOREVER (LIVE)", nil, nil},
		{"veto order follows the title not the lexicon", "Song", "Someone", "Song [Karaoke] (Instrumental) reverb slowed", []string{"karaoke", "instrumental", "reverb", "slowed"}, nil},
		{"fullwidth letters are folded", "Song", "Someone", "Song （ＬＩＶＥ）", []string{"live"}, nil},
		{"lenticular brackets are separators", "Song", "Someone", "Song【Instrumental】", []string{"instrumental"}, nil},
		{"corner brackets are separators", "Song", "Someone", "Song「Live」", []string{"live"}, nil},
		{"en dash is a separator", "Song", "Someone", "Song–Instrumental", []string{"instrumental"}, nil},
		{"unclosed bracket still carries its qualifier", "Song", "Someone", "Song (Instrumental", []string{"instrumental"}, nil},
		{"stray closing bracket still carries its qualifier", "Song", "Someone", "Song Instrumental)", []string{"instrumental"}, nil},
		{"nested brackets carry both qualifiers", "Song", "Someone", "Song [Live (Instrumental)]", []string{"live", "instrumental"}, nil},
		{"hyphenated sped up", "Song", "Someone", "Song (sped-up)", []string{"sped up"}, nil},
		{"accuracy claim without a space", "Song", "Someone", "Song [100%Accurate]", []string{"100% accurate"}, nil},
		{"live version splits into both families", "Song", "Someone", "Song (Live Version)", []string{"live"}, []string{"version"}},
		{"repeated qualifier in mixed case is reported once", "Song", "Someone", "Song (LIVE) live Live", []string{"live"}, nil},
		{"live inside a longer word is not live", "Song", "Someone", "Song (Livestream Audio)", nil, nil},
		{"mix inside a longer word is not mix", "Song", "Someone", "Song (Mixtape Version)", nil, []string{"version"}},
		{"8d inside a longer token is not 8d", "Song", "Someone", "Song 18d", nil, nil},
		{"title containing live as a substring does not request live", "Delivery", "Someone", "Delivery (Live)", []string{"live"}, nil},
		{"title word mix does not request remix", "Mix Tape", "Someone", "Mix Tape (Remix)", []string{"remix"}, nil},
		{"nightcore uploader prefix is vetoed", "Song", "Someone", "Nightcore - Song", []string{"nightcore"}, nil},
		{"very long candidate is reported once per qualifier", "Song", "Someone", longCandidate, []string{"live", "instrumental"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			veto, qualifiers := download.UnrequestedQualifiers(tt.title, tt.artist, tt.candidate)
			assertLabels(t, "veto", veto, tt.wantVeto)
			assertLabels(t, "qualifiers", qualifiers, tt.wantQualifiers)
		})
	}
}

func TestUnrequestedQualifiers_DotCommaAndUnderscoreAreSeparators(t *testing.T) {
	tests := []struct {
		candidate string
		wantVeto  []string
	}{
		{"Song_Instrumental", []string{"instrumental"}},
		{"Song.Instrumental", []string{"instrumental"}},
		{"Song,Live", []string{"live"}},
		{"Song...Live", []string{"live"}},
		{"Song (Slowed.Reverb)", []string{"slowed", "reverb"}},
		{"Song (Slowed_Reverb)", []string{"slowed", "reverb"}},
	}
	for _, tt := range tests {
		t.Run(tt.candidate, func(t *testing.T) {
			veto, _ := download.UnrequestedQualifiers("Song", "Someone", tt.candidate)
			assertLabels(t, "veto", veto, tt.wantVeto)
		})
	}
}

func TestUnrequestedQualifiers_UnicodeDashSeparatorsAfterFeatureCredit(t *testing.T) {
	tests := []struct {
		name      string
		separator rune
	}{
		{"hyphen", '‐'},
		{"non-breaking hyphen", '‑'},
		{"figure dash", '‒'},
		{"horizontal bar", '―'},
		{"minus sign", '−'},
		{"small em dash", '﹘'},
		{"small hyphen-minus", '﹣'},
		{"fullwidth hyphen-minus", '－'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := "Someone " + string(tt.separator) + " Song ft. Other " + string(tt.separator) + " Instrumental"
			veto, _ := download.UnrequestedQualifiers("Song", "Someone", candidate)
			assertLabels(t, "veto", veto, []string{"instrumental"})
		})
	}
}

func TestUnrequestedQualifiers_RemasterStyleMixIsNotAQualifier(t *testing.T) {
	for _, title := range []string{
		"Come Together (2019 Mix)",
		"Come Together (Original Mix)",
		"Come Together (Stereo Mix)",
		"Come Together (Mono Mix)",
		"Come Together (Album Mix)",
	} {
		veto, qualifiers := download.UnrequestedQualifiers("Come Together", "The Beatles", title)
		assertLabels(t, title+" veto", veto, nil)
		assertLabels(t, title+" qualifiers", qualifiers, nil)
	}
}

func TestUnrequestedQualifiers_OtherMixesStayVetoed(t *testing.T) {
	for _, title := range []string{"Song (Who Mix?)", "Song (Club Mix)"} {
		veto, _ := download.UnrequestedQualifiers("Song", "A", title)
		assertLabels(t, title+" veto", veto, []string{"mix"})
	}
}

func TestRank_RejectsUnrequestedVersionBeforeDownload(t *testing.T) {
	track := download.RankingTrack{Title: "Rollacoasta", Artist: "prettifun", ExpectedLength: 159}
	candidates := []download.AudioCandidate{
		{
			Title:      "prettifun - Rollacoasta (Instrumental) [100% Accurate]",
			Channel:    "ProdADN",
			Duration:   159,
			URL:        "https://youtube.com/watch?v=instrumental",
			Categories: []string{"Music"},
		},
		{Title: "Rollacoasta", Channel: "prettifun", Duration: 159, URL: "https://youtube.com/watch?v=clean", Categories: []string{"Music"}},
	}

	ranked, rejected := rank(track, candidates...)

	if len(ranked) != 1 || ranked[0].URL != "https://youtube.com/watch?v=clean" {
		t.Fatalf("ranked = %v, want only the clean upload", ranked)
	}
	if len(rejected) != 1 || rejected[0].Stage != download.RankingStageQualifier || rejected[0].Reason != "unrequested instrumental, 100% accurate" {
		t.Fatalf("rejected = %+v, want one qualifier rejection naming its labels", rejected)
	}
}

func TestRank_RadioEditRanksAfterEveryCleanCandidate(t *testing.T) {
	track := download.RankingTrack{Title: "Harbour Lights", Artist: "The Marram", ExpectedLength: 236}
	candidates := []download.AudioCandidate{
		{Title: "Harbour Lights (Radio Edit)", Channel: "The Marram - Topic", Duration: 236, URL: "https://x.example/edit", Categories: []string{"Music"}},
		{Title: "Harbour Lights", Channel: "The Marram - Topic", Duration: 200, URL: "https://x.example/clean", Categories: []string{"Music"}},
	}

	ranked, rejected := rank(track, candidates...)

	if len(rejected) != 0 || len(ranked) != 2 || ranked[0].URL != "https://x.example/clean" {
		t.Fatalf("ranked = %v rejected = %v, want the clean upload first and the edit kept", ranked, rejected)
	}
}

func TestRank_MustHold8_UnplayableCandidatesAreRejectedBeforeRanking(t *testing.T) {
	track := download.RankingTrack{Title: "Drinking in L.A.", Artist: "Bran Van 3000", ExpectedLength: 240}
	candidates := []download.AudioCandidate{
		{Title: "Drinking in L.A.", URL: "sc:drm", Unplayable: "drm"},
		{Title: "Drinking in L.A.", URL: "sc:preview", Unplayable: "preview"},
		{Title: "Drinking in L.A.", URL: "sc:ok"},
	}

	ranked, rejected := rank(track, candidates...)

	if len(ranked) != 1 || ranked[0].URL != "sc:ok" {
		t.Fatalf("ranked = %v, want only the playable candidate so the downloader never sees the others", ranked)
	}
	if len(rejected) != 2 || rejected[0].Stage != download.RankingStageDRM || rejected[1].Stage != download.RankingStagePreview {
		t.Fatalf("rejected = %v, want drm then preview", rejected)
	}
}

func TestUnrequestedQualifiers_FullwidthBracketsGetTheSameVetoAsAscii(t *testing.T) {
	wantVeto, _ := download.UnrequestedQualifiers("Song", "Artist", "Artist - Song [Slowed]")
	veto, qualifiers := download.UnrequestedQualifiers("Song", "Artist", "Artist - Song ［Ｓｌｏｗｅｄ］")
	assertLabels(t, "veto", veto, wantVeto)
	assertLabels(t, "qualifiers", qualifiers, nil)
	if len(veto) == 0 {
		t.Fatal("veto is empty, want the slowed qualifier")
	}
}

func TestUnrequestedQualifiers_TitleWithoutBracketHasNoVeto(t *testing.T) {
	veto, qualifiers := download.UnrequestedQualifiers("Song", "Artist", "Artist - Song")
	assertLabels(t, "veto", veto, nil)
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestRank_AnArtTrackIsResolvedByItsKind(t *testing.T) {
	track := download.RankingTrack{Title: "Matches", Artist: "Tezzus", ExpectedLength: 180}
	weak := download.AudioCandidate{Title: "Unrelated Words", URL: "https://music.youtube.com/watch?v=weakAAAAAAA", Duration: 180}

	art := weak
	art.Kind = download.KindArtTrack
	if got := best(track, art); got == nil || got.URL != art.URL {
		t.Errorf("selected %v, want the art track: its kind skips the identity gate", got)
	}

	upload := weak
	upload.Kind = download.KindUpload
	if got := best(track, upload); got != nil {
		t.Errorf("selected %v, want nothing: an upload goes through the identity gate", got)
	}
}

func TestRank_AnExplicitResolvedTrackBeatsACloserCleanOne(t *testing.T) {
	track := download.RankingTrack{Title: "Matches", Artist: "Tezzus", ExpectedLength: 180}
	clean := download.AudioCandidate{Title: "Matches", URL: "https://music.youtube.com/watch?v=cleanAAAAAA", Duration: 180, Kind: download.KindArtTrack}
	explicit := download.AudioCandidate{Title: "Matches", URL: "https://music.youtube.com/watch?v=explicitAAA", Duration: 182, Kind: download.KindArtTrack, Explicit: true}

	if got := best(track, clean, explicit); got == nil || got.URL != explicit.URL {
		t.Fatalf("selected %v, want the explicit track even though the clean one is closer in length", got)
	}
}

func TestRank_TheCatalogExplicitFlagPicksTheTwin(t *testing.T) {
	clean := download.AudioCandidate{Title: "Matches", URL: "https://music.youtube.com/watch?v=cleanAAAAAA", Duration: 182, Kind: download.KindArtTrack}
	explicit := download.AudioCandidate{Title: "Matches", URL: "https://music.youtube.com/watch?v=explicitAAA", Duration: 180, Kind: download.KindArtTrack, Explicit: true}
	yes, no := true, false
	tests := []struct {
		name    string
		catalog *bool
		want    string
	}{
		{"clean catalog flag picks the clean twin", &no, clean.URL},
		{"explicit catalog flag picks the explicit twin", &yes, explicit.URL},
		{"no catalog flag prefers explicit", nil, explicit.URL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := download.RankingTrack{Title: "Matches", Artist: "Tezzus", ExpectedLength: 180, CatalogExplicit: tt.catalog}
			if got := best(track, explicit, clean); got == nil || got.URL != tt.want {
				t.Fatalf("selected %v, want %s", got, tt.want)
			}
		})
	}
}

func TestUploaderOf_ArtTrackIsTrustedOnlyWhenItsArtistsCreditTheTrackArtist(t *testing.T) {
	artTrack := func(artists ...string) download.AudioCandidate {
		return download.AudioCandidate{Kind: download.KindArtTrack, Artists: artists}
	}
	tests := []struct {
		name      string
		artist    string
		candidate download.AudioCandidate
		want      string
	}{
		{"credited art track counts as topic", "ezcodylee", artTrack("ezcodylee"), download.ChannelTopic},
		{"credit matches through normalisation", "ASAP Rocky", artTrack("A$AP Rocky"), download.ChannelTopic},
		{"other artist's art track is untrusted", "ezcodylee", artTrack("Someone Else"), download.ChannelOther},
		{"art track without credits is untrusted", "ezcodylee", artTrack(), download.ChannelOther},
		{"upload on a topic channel is topic", "ezcodylee", download.AudioCandidate{Channel: "ezcodylee - Topic"}, download.ChannelTopic},
	}
	for _, tt := range tests {
		if got := download.UploaderOf(tt.artist, tt.candidate); got != tt.want {
			t.Errorf("%s: UploaderOf = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestUnrequestedQualifiers_ReleaseVariantsAreQualifiers(t *testing.T) {
	vetoed := map[string]string{
		"Song (Acoustic)":    "acoustic",
		"Song (Demo)":        "demo",
		"Song (Re-Recorded)": "re-recorded",
		"Song [Acoustic]":    "acoustic",
	}
	for title, label := range vetoed {
		veto, qualifiers := download.UnrequestedQualifiers("Song", "Artist", title)
		assertLabels(t, title+" veto", veto, []string{label})
		assertLabels(t, title+" qualifiers", qualifiers, nil)
	}
	for _, title := range []string{"Song (Remastered)", "Song (2011 Remaster)"} {
		veto, qualifiers := download.UnrequestedQualifiers("Song", "Artist", title)
		assertLabels(t, title+" veto", veto, nil)
		assertLabels(t, title+" qualifiers", qualifiers, nil)
	}
	veto, _ := download.UnrequestedQualifiers("Song (Acoustic)", "Artist", "Song (Acoustic)")
	assertLabels(t, "requested acoustic veto", veto, nil)
}

func TestRank_RejectsPlainlyTitledAcousticAndDemoUploads(t *testing.T) {
	track := download.RankingTrack{Title: "Sunglasses at Night", Artist: "Corey Hart", ExpectedLength: 233}
	candidates := []download.AudioCandidate{
		{Title: "Sunglasses at Night (Acoustic)", Channel: "Corey Hart - Topic", Duration: 233, URL: "https://www.youtube.com/watch?v=acousticUp1", Categories: []string{"Music"}},
		{Title: "Sunglasses at Night (Demo)", Channel: "Corey Hart - Topic", Duration: 233, URL: "https://www.youtube.com/watch?v=demoUpload1", Categories: []string{"Music"}},
		{Title: "Sunglasses at Night", Channel: "Corey Hart", Duration: 232, URL: "https://www.youtube.com/watch?v=masterUp001", Categories: []string{"Music"}},
	}

	ranked, rejected := rank(track, candidates...)

	if len(ranked) != 1 || ranked[0].URL != "https://www.youtube.com/watch?v=masterUp001" {
		t.Fatalf("ranked = %v, want only the master", ranked)
	}
	if len(rejected) != 2 || rejected[0].Reason != "unrequested acoustic" || rejected[1].Reason != "unrequested demo" {
		t.Fatalf("rejected = %+v, want acoustic and demo qualifier rejections", rejected)
	}
}

func TestUploaderOf_TheCatalogPinIsTrusted(t *testing.T) {
	pin := download.AudioCandidate{Kind: download.KindCatalogPin, Artists: []string{"Sophie Ellis-Bextor"}}
	if got := download.UploaderOf("Sophie Ellis-Bextor", pin); got != download.ChannelTopic {
		t.Errorf("UploaderOf = %q, want the user's own pick trusted as %q", got, download.ChannelTopic)
	}
	pin.Title, pin.Channel = "Murder On The Dancefloor", "someone-else"
	if got := evaluation(t, song("Murder On The Dancefloor", "Sophie Ellis-Bextor", 230), pin)["artist_match"]; got != true {
		t.Errorf("artist_match = %v, want the user's own pick matched for a channel that does not name the artist", got)
	}
}

func TestUploaderOf_APinnedVideoIsTrustedByItsCredits(t *testing.T) {
	pinned := func(artists ...string) download.AudioCandidate {
		return download.AudioCandidate{Kind: download.KindPinnedVideo, Artists: artists}
	}
	tests := []struct {
		name      string
		artist    string
		candidate download.AudioCandidate
		want      string
	}{
		{"credited pinned video counts as topic", "slayr", pinned("slayr"), download.ChannelTopic},
		{"credit matches through normalisation", "ASAP Rocky", pinned("A$AP Rocky"), download.ChannelTopic},
		{"other artist's pinned video is untrusted", "slayr", pinned("Someone Else"), download.ChannelOther},
		{"pinned video without credits is untrusted", "slayr", pinned(), download.ChannelOther},
		{"upload crediting the artist stays untrusted", "slayr", download.AudioCandidate{Kind: download.KindUpload, Artists: []string{"slayr"}}, download.ChannelOther},
	}
	for _, tt := range tests {
		if got := download.UploaderOf(tt.artist, tt.candidate); got != tt.want {
			t.Errorf("%s: UploaderOf = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRankCandidates_TheUsersPickGoesBeforeTheArtTrack(t *testing.T) {
	track := download.RankingTrack{Title: "Murder On The Dancefloor", Artist: "Sophie Ellis-Bextor", ExpectedLength: 230}
	artTrack := download.AudioCandidate{
		Title:    "Murder On The Dancefloor",
		Duration: 232,
		URL:      "https://music.youtube.com/watch?v=RCtTDJgxPDA",
		Artists:  []string{"Sophie Ellis-Bextor"},
		Kind:     download.KindArtTrack,
	}
	pickedUpload := download.AudioCandidate{
		Title:   "Murder On The Dancefloor",
		URL:     "https://soundcloud.com/sophie-ellis-bextor-official/murder-on-the-dancefloor-album",
		Artists: []string{"Sophie Ellis-Bextor"},
		Kind:    download.KindCatalogPin,
	}
	catalogVideo := download.AudioCandidate{
		Title:   "Murder On The Dancefloor",
		URL:     "https://music.youtube.com/watch?v=Zv6M1xG4T0w",
		Artists: []string{"Sophie Ellis-Bextor"},
		Kind:    download.KindPinnedVideo,
	}
	tests := []struct {
		name       string
		candidates []download.AudioCandidate
		wantFirst  string
	}{
		{"pick listed after the art track", []download.AudioCandidate{artTrack, pickedUpload}, pickedUpload.URL},
		{"pick listed before the art track", []download.AudioCandidate{pickedUpload, artTrack}, pickedUpload.URL},
		{"a pinned video stays behind the art track", []download.AudioCandidate{catalogVideo, artTrack}, artTrack.URL},
		{"pick still first beside a pinned video and an art track", []download.AudioCandidate{artTrack, catalogVideo, pickedUpload}, pickedUpload.URL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ranked, _ := rank(track, tt.candidates...)
			if len(ranked) != len(tt.candidates) {
				t.Fatalf("ranked %d candidates, want all %d", len(ranked), len(tt.candidates))
			}
			if ranked[0].URL != tt.wantFirst {
				t.Errorf("first = %q, want %q", ranked[0].URL, tt.wantFirst)
			}
		})
	}
}

func TestUploaderOf_ATopicChannelMustBeTheTracksArtist(t *testing.T) {
	upload := func(channel string) download.AudioCandidate {
		return download.AudioCandidate{Channel: channel, Kind: download.KindUpload}
	}
	tests := []struct {
		name      string
		artist    string
		candidate download.AudioCandidate
		want      string
	}{
		{"the artist's own topic channel is topic", "Cutting Crew", upload("Cutting Crew - Topic"), download.ChannelTopic},
		{"topic channel matches through normalisation", "ASAP Rocky", upload("A$AP Rocky - Topic"), download.ChannelTopic},
		{"a karaoke topic channel is untrusted", "Cutting Crew", upload("Karaoke Hits - Topic"), download.ChannelOther},
		{"a namesake topic channel is untrusted", "Che", upload("Chechive - Topic"), download.ChannelOther},
		{"a substring namesake channel is untrusted", "Eve", upload("Steve Lacy"), download.ChannelOther},
		{"the artist's own channel is artist", "Cutting Crew", upload("Cutting Crew"), download.ChannelArtist},
		{"the artist's VEVO channel is artist", "The Weeknd", upload("TheWeekndVEVO"), download.ChannelArtist},
		{"the artist's official channel is artist", "Eve", upload("Eve Official"), download.ChannelArtist},
	}
	for _, tt := range tests {
		if got := download.UploaderOf(tt.artist, tt.candidate); got != tt.want {
			t.Errorf("%s: UploaderOf = %q, want %q", tt.name, got, tt.want)
		}
	}
	steve := download.AudioCandidate{Title: "Eve - Song", Channel: "Steve Lacy", Kind: download.KindUpload}
	if got := evaluation(t, song("Song", "Eve", 200), steve)["artist_match"]; got != false {
		t.Errorf("artist_match for Eve on Steve Lacy's channel = %v, want a whole-name comparison", got)
	}
}

func TestUploaderOf_AnyCreditedArtistOwnsTheTopicChannel(t *testing.T) {
	upload := func(channel string) download.AudioCandidate {
		return download.AudioCandidate{Channel: channel, Kind: download.KindUpload}
	}
	tests := []struct {
		name      string
		artist    string
		candidate download.AudioCandidate
		want      string
	}{
		{"first of a featuring credit", "Usher featuring Pitbull", upload("Usher - Topic"), download.ChannelTopic},
		{"second of a featuring credit", "Usher featuring Pitbull", upload("Pitbull - Topic"), download.ChannelTopic},
		{"first of an ampersand credit", "Fatt Smaxk & Playboi Carti", upload("Fatt Smaxk - Topic"), download.ChannelTopic},
		{"the duo's whole name", "Nico & Vinz", upload("Nico & Vinz - Topic"), download.ChannelTopic},
		{"a comma credit", "Earth, Wind & Fire", upload("Earth, Wind & Fire - Topic"), download.ChannelTopic},
		{"first credited artist's own channel", "Usher featuring Pitbull", upload("Usher"), download.ChannelArtist},
		{"a karaoke topic channel stays untrusted", "Usher featuring Pitbull", upload("Karaoke Hits - Topic"), download.ChannelOther},
		{"a namesake topic channel stays untrusted", "Fatt Smaxk & Playboi Carti", upload("Chechive - Topic"), download.ChannelOther},
		{"another artist's topic channel stays untrusted", "Usher featuring Pitbull", upload("DJ's United - Topic"), download.ChannelOther},
	}
	for _, tt := range tests {
		if got := download.UploaderOf(tt.artist, tt.candidate); got != tt.want {
			t.Errorf("%s: UploaderOf = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRank_AnArtistChannelUploadOutranksAnEqualReupload(t *testing.T) {
	track := download.RankingTrack{Title: "Blinding Lights", Artist: "The Weeknd", ExpectedLength: 200}
	reupload := download.AudioCandidate{
		Title: "Blinding Lights", Channel: "RandomUploader", Duration: 200,
		URL: "https://www.youtube.com/watch?v=aaaaaaaaaaa", Kind: download.KindUpload,
	}
	artist := download.AudioCandidate{
		Title: "Blinding Lights", Channel: "The Weeknd", Duration: 200,
		URL: "https://www.youtube.com/watch?v=zzzzzzzzzzz", Kind: download.KindUpload,
	}

	got := best(track, reupload, artist)
	if got == nil || got.URL != artist.URL {
		t.Fatalf("selected %v, want the upload from the artist's own channel over an equal re-upload", got)
	}
}

func TestRank_RejectsRerecordingTitleTheTrackDoesNotCarry(t *testing.T) {
	track := download.RankingTrack{Title: "It's Only Love", Artist: "Bryan Adams", ExpectedLength: 195}
	for _, title := range []string{
		"It's Only Love (Classic Version)",
		"It's Only Love (Taylor's Version)",
		"It's Only Love (Re-Recorded)",
		"It's Only Love (Re-Recording)",
	} {
		for _, kind := range []download.CandidateKind{download.KindArtTrack, download.KindUpload} {
			c := download.AudioCandidate{Title: title, URL: "https://music.youtube.com/watch?v=Ml2N72dMv_8", Duration: 194, Kind: kind, Artists: []string{"Bryan Adams"}, Channel: "Bryan Adams - Topic"}
			ranked, rejected := rank(track, c)
			if len(ranked) != 0 || len(rejected) != 1 || rejected[0].Stage != download.RankingStageQualifier || rejected[0].Reason != "unrequested re-recorded" {
				t.Errorf("%q kind %q: ranked=%d rejected=%+v", title, kind, len(ranked), rejected)
			}
		}
	}
}

func TestRank_RanksRerecordingWhenTrackCarriesIt(t *testing.T) {
	track := download.RankingTrack{Title: "It’s Only Love (classic version)", Artist: "Bryan Adams", ExpectedLength: 194}
	c := download.AudioCandidate{Title: "It's Only Love (Classic Version)", URL: "https://music.youtube.com/watch?v=Ml2N72dMv_8", Duration: 194, Kind: download.KindArtTrack, Artists: []string{"Bryan Adams"}, Channel: "Bryan Adams - Topic"}
	ranked, rejected := rank(track, c)
	if len(ranked) != 1 || len(rejected) != 0 {
		t.Errorf("ranked=%d rejected=%+v", len(ranked), rejected)
	}
}

func TestRank_TheUsersOwnPickIsStillRefusedWhenItIsARerecording(t *testing.T) {
	track := download.RankingTrack{Title: "It's Only Love", Artist: "Bryan Adams", ExpectedLength: 195}
	for _, kind := range []download.CandidateKind{download.KindCatalogPin, download.KindPinnedVideo} {
		c := download.AudioCandidate{Title: "It's Only Love (Classic Version)", URL: "https://music.youtube.com/watch?v=pin", Duration: 194, Kind: kind, Artists: []string{"Bryan Adams"}, Channel: "Bryan Adams - Topic"}
		ranked, rejected := rank(track, c)
		if len(ranked) != 0 || len(rejected) != 1 || rejected[0].Stage != download.RankingStageQualifier || rejected[0].Reason != "unrequested re-recorded" {
			t.Errorf("kind %q: ranked=%d rejected=%+v, want one re-recorded qualifier rejection", kind, len(ranked), rejected)
		}
	}
}

func TestRank_RerecordingPhraseIsRefusedWhateverItsCaseApostropheOrBracket(t *testing.T) {
	track := download.RankingTrack{Title: "Love Story", Artist: "Taylor Swift", ExpectedLength: 235}
	for _, title := range []string{
		"Love Story (Taylor’s Version)",
		"Love Story (TAYLOR'S VERSION)",
		"Love Story [Taylors Version]",
		"Love Story - Taylor's Version",
		"Love Story (classic version)",
		"Love Story (RE-RECORDING)",
		"Love Story （Taylor's Version）",
	} {
		c := download.AudioCandidate{Title: title, URL: "https://music.youtube.com/watch?v=x", Duration: 235, Kind: download.KindArtTrack, Artists: []string{"Taylor Swift"}, Channel: "Taylor Swift - Topic"}
		ranked, rejected := rank(track, c)
		if len(ranked) != 0 || len(rejected) != 1 || rejected[0].Reason != "unrequested re-recorded" {
			t.Errorf("%q: ranked=%d rejected=%+v, want it refused as re-recorded", title, len(ranked), rejected)
		}
	}
}

func TestRank_ClassicOrVersionAloneIsNotARerecording(t *testing.T) {
	cases := []struct{ track, candidate string }{
		{"Classic Man", "Classic Man"},
		{"Classical Gas", "Classical Gas"},
		{"Classic", "Classic"},
		{"Version", "Version"},
		{"It's Only Love", "It's Only Love (Remastered 2014)"},
		{"It's Only Love", "It's Only Love (Album Version)"},
		{"It's Only Love", "It's Only Love (Classic)"},
		{"Classic Rock Version Of Me", "Classic Rock Version Of Me"},
	}
	for _, c := range cases {
		track := download.RankingTrack{Title: c.track, Artist: "Bryan Adams", ExpectedLength: 195}
		cand := download.AudioCandidate{Title: c.candidate, URL: "https://music.youtube.com/watch?v=y", Duration: 195, Kind: download.KindArtTrack, Artists: []string{"Bryan Adams"}, Channel: "Bryan Adams - Topic"}
		ranked, rejected := rank(track, cand)
		for _, r := range rejected {
			if r.Reason == "unrequested re-recorded" {
				t.Errorf("track %q candidate %q: refused as re-recorded", c.track, c.candidate)
			}
		}
		if len(ranked) != 1 {
			t.Errorf("track %q candidate %q: ranked=%d rejected=%+v, want it ranked", c.track, c.candidate, len(ranked), rejected)
		}
	}
}

func TestRank_RerecordingPhraseOnlyInChannelOrAlbumDoesNotRefuseTheCandidate(t *testing.T) {
	track := download.RankingTrack{Title: "It's Only Love", Artist: "Bryan Adams", ExpectedLength: 195}
	c := download.AudioCandidate{Title: "It's Only Love", URL: "https://music.youtube.com/watch?v=z", Duration: 195, Kind: download.KindArtTrack, Artists: []string{"Bryan Adams"}, Channel: "Taylor's Version Archive", Album: "Classic Version Hits"}
	ranked, rejected := rank(track, c)
	if len(ranked) != 1 || len(rejected) != 0 {
		t.Errorf("ranked=%d rejected=%+v, want the plainly titled candidate ranked", len(ranked), rejected)
	}
}

func TestRank_TrackTitledAsATaylorsVersionAcceptsEitherApostrophe(t *testing.T) {
	track := download.RankingTrack{Title: "Love Story (Taylor's Version)", Artist: "Taylor Swift", ExpectedLength: 235}
	c := download.AudioCandidate{Title: "Love Story (Taylor’s Version)", URL: "https://music.youtube.com/watch?v=tv", Duration: 235, Kind: download.KindArtTrack, Artists: []string{"Taylor Swift"}, Channel: "Taylor Swift - Topic"}
	ranked, rejected := rank(track, c)
	if len(ranked) != 1 || len(rejected) != 0 {
		t.Errorf("ranked=%d rejected=%+v, want the Taylor's Version ranked", len(ranked), rejected)
	}
}

func TestRank_RefusingTheRerecordingStillRanksTheOriginalBesideIt(t *testing.T) {
	track := download.RankingTrack{Title: "It's Only Love", Artist: "Bryan Adams", ExpectedLength: 195}
	candidates := []download.AudioCandidate{
		{Title: "It's Only Love (Classic Version)", URL: "https://music.youtube.com/watch?v=classic", Duration: 194, Kind: download.KindArtTrack, Artists: []string{"Bryan Adams"}, Channel: "Bryan Adams - Topic"},
		{Title: "It's Only Love", URL: "https://www.youtube.com/watch?v=original", Duration: 195, Channel: "Bryan Adams", Categories: []string{"Music"}},
	}
	ranked, rejected := rank(track, candidates...)
	if len(ranked) != 1 || ranked[0].URL != "https://www.youtube.com/watch?v=original" {
		t.Fatalf("ranked=%v, want only the original", ranked)
	}
	if len(rejected) != 1 || rejected[0].Reason != "unrequested re-recorded" {
		t.Fatalf("rejected=%+v, want the Classic Version refused as re-recorded", rejected)
	}
}
