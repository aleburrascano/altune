package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/titlemarkers"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestRank_PicksTheBestCandidate(t *testing.T) {
	tests := []struct {
		name       string
		track      service.RankTrack
		candidates []domain.AudioCandidate
		wantNil    bool
		wantTitle  string
		wantReason string
	}{
		{
			name: "topic channel preferred when identity >= 60",
			track: service.RankTrack{
				Title:          "Blinding Lights",
				Artist:         "The Weeknd",
				ExpectedLength: 200,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=vevo1",
					downloadtest.WithTitle("The Weeknd - Blinding Lights"),
					downloadtest.WithChannel("TheWeekndVEVO"),
					downloadtest.WithDuration(203),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(500_000_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=topic1",
					downloadtest.WithTitle("Blinding Lights"),
					downloadtest.WithChannel("The Weeknd - Topic"),
					downloadtest.WithDuration(200),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(10_000_000),
				),
			},
			wantTitle:  "Blinding Lights",
			wantReason: "topic channel candidate should be preferred over VEVO when identity >= 60",
		},
		{
			name: "exact duration match scores highest among non-topic candidates",
			track: service.RankTrack{
				Title:          "Starboy",
				Artist:         "The Weeknd",
				ExpectedLength: 230,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=far",
					downloadtest.WithTitle("The Weeknd - Starboy (Extended Remix)"),
					downloadtest.WithChannel("RandomUploader"),
					downloadtest.WithDuration(300),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(1_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=close",
					downloadtest.WithTitle("The Weeknd - Starboy (Audio)"),
					downloadtest.WithChannel("AnotherUploader"),
					downloadtest.WithDuration(231),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(1_000),
				),
			},
			wantTitle:  "The Weeknd - Starboy (Audio)",
			wantReason: "candidate with duration within the tight length window (2s) should score higher than one 70s off",
		},
		{
			name: "no candidates returns nil",
			track: service.RankTrack{
				Title:  "Nonexistent",
				Artist: "Nobody",
			},
			candidates: []domain.AudioCandidate{},
			wantNil:    true,
			wantReason: "empty candidate list must return nil",
		},
		{
			name: "all candidates below identity threshold filtered out",
			track: service.RankTrack{
				Title:          "Save Your Tears",
				Artist:         "The Weeknd",
				ExpectedLength: 215,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=cook1",
					downloadtest.WithTitle("Cooking Tutorial Episode 47"),
					downloadtest.WithChannel("CookingChannel"),
					downloadtest.WithDuration(215),
					downloadtest.WithCategories([]string{"Howto & Style"}),
					downloadtest.WithViewCount(50_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=fin1",
					downloadtest.WithTitle("Random Podcast About Finance"),
					downloadtest.WithChannel("FinanceBro"),
					downloadtest.WithDuration(3600),
					downloadtest.WithCategories([]string{"Education"}),
					downloadtest.WithViewCount(10_000),
				),
			},
			wantNil:    true,
			wantReason: "candidates with titles completely unrelated to track should score identity < 60 and be filtered out",
		},
		{
			name: "highest composite score wins among non-topic candidates",
			track: service.RankTrack{
				Title:          "Die For You",
				Artist:         "The Weeknd",
				ExpectedLength: 260,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=vevo2",
					downloadtest.WithTitle("The Weeknd - Die For You (Official Video)"),
					downloadtest.WithChannel("TheWeekndVEVO"),
					downloadtest.WithDuration(262),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(900_000_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=lyrics1",
					downloadtest.WithTitle("The Weeknd - Die For You (Lyrics)"),
					downloadtest.WithChannel("LyricsChannel"),
					downloadtest.WithDuration(261),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(50_000_000),
				),
			},
			wantTitle:  "The Weeknd - Die For You (Official Video)",
			wantReason: "VEVO channel (0.8) + highest views + Music category should beat a lyrics channel (0.3)",
		},
		{
			name: "nil candidates returns nil",
			track: service.RankTrack{
				Title:  "Test",
				Artist: "Test",
			},
			candidates: nil,
			wantNil:    true,
			wantReason: "nil candidate slice must return nil",
		},
		{
			name: "topic channel wins over VEVO even with fewer views",
			track: service.RankTrack{
				Title:          "After Hours",
				Artist:         "The Weeknd",
				ExpectedLength: 361,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=vevo3",
					downloadtest.WithTitle("The Weeknd - After Hours (Official Video)"),
					downloadtest.WithChannel("TheWeekndVEVO"),
					downloadtest.WithDuration(362),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(800_000_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=topic2",
					downloadtest.WithTitle("After Hours"),
					downloadtest.WithChannel("The Weeknd - Topic"),
					downloadtest.WithDuration(361),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(5_000_000),
				),
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
	track := service.RankTrack{Title: "Blinding Lights", Artist: "The Weeknd", ExpectedLength: 200}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://youtube.com/watch?v=slowed",
			downloadtest.WithTitle("Blinding Lights"),
			downloadtest.WithChannel("The Weeknd - Topic"),
			downloadtest.WithDuration(240),
			downloadtest.WithCategories([]string{"Music"}),
		),
		downloadtest.Candidate(
			"https://youtube.com/watch?v=master",
			downloadtest.WithTitle("Blinding Lights"),
			downloadtest.WithChannel("The Weeknd - Topic"),
			downloadtest.WithDuration(200),
			downloadtest.WithCategories([]string{"Music"}),
		),
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
	track := service.RankTrack{Title: "Song", Artist: "Artist"}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://b.example/x",
			downloadtest.WithTitle("Artist - Song"),
			downloadtest.WithChannel("Artist - Topic"),
		),
		downloadtest.Candidate(
			"https://a.example/x",
			downloadtest.WithTitle("Artist - Song"),
			downloadtest.WithChannel("Artist - Topic"),
		),
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
		track        service.RankTrack
		candidates   []domain.AudioCandidate
		wantNil      bool
		wantChannel  string
		wantContains string
		description  string
	}{
		{
			name: "die hard by intended artist prefers correct Topic channel",
			track: service.RankTrack{
				Title:          "Die Hard",
				Artist:         "Dr. Dre",
				ExpectedLength: 280,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=kendrick",
					downloadtest.WithTitle("DIE HARD"),
					downloadtest.WithChannel("Kendrick Lamar - Topic"),
					downloadtest.WithDuration(240),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(65_000_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=dre",
					downloadtest.WithTitle("Die Hard"),
					downloadtest.WithChannel("Dr. Dre - Topic"),
					downloadtest.WithDuration(282),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(5_000_000),
				),
			},
			wantChannel:  "Dr. Dre - Topic",
			wantContains: "Die Hard",
			description:  "must prefer Topic channel matching track artist over wrong-artist Topic channel",
		},
		{
			name: "die hard kendrick is correct when artist is kendrick",
			track: service.RankTrack{
				Title:          "DIE HARD",
				Artist:         "Kendrick Lamar",
				ExpectedLength: 238,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=kendrick",
					downloadtest.WithTitle("Kendrick Lamar - DIE HARD (Official Audio)"),
					downloadtest.WithChannel("Kendrick Lamar - Topic"),
					downloadtest.WithDuration(240),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(65_000_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=dre",
					downloadtest.WithTitle("Dr. Dre - Die Hard ft. Eminem"),
					downloadtest.WithChannel("Dr. Dre - Topic"),
					downloadtest.WithDuration(282),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(5_000_000),
				),
			},
			wantChannel:  "Kendrick Lamar - Topic",
			wantContains: "Kendrick",
			description:  "must pick Kendrick's Topic channel when artist IS Kendrick",
		},
		{
			name: "topic channel beats vevo for exact match",
			track: service.RankTrack{
				Title:          "Blinding Lights",
				Artist:         "The Weeknd",
				ExpectedLength: 200,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=vevo",
					downloadtest.WithTitle("The Weeknd - Blinding Lights (Official Video)"),
					downloadtest.WithChannel("TheWeekndVEVO"),
					downloadtest.WithDuration(203),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(500_000_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=topic",
					downloadtest.WithTitle("Blinding Lights"),
					downloadtest.WithChannel("The Weeknd - Topic"),
					downloadtest.WithDuration(200),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(10_000_000),
				),
			},
			wantChannel: "The Weeknd - Topic",
			description: "topic channel is preferred over VEVO",
		},
		{
			name: "title-only match penalized below combined match",
			track: service.RankTrack{
				Title:          "Lose Yourself",
				Artist:         "Eminem",
				ExpectedLength: 326,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=eminem",
					downloadtest.WithTitle("Eminem - Lose Yourself (Official Video)"),
					downloadtest.WithChannel("EminemVEVO"),
					downloadtest.WithDuration(328),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(1_000_000_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=motivation",
					downloadtest.WithTitle("Lose Yourself - Motivational Speech"),
					downloadtest.WithChannel("MotivationHub"),
					downloadtest.WithDuration(600),
					downloadtest.WithCategories([]string{"Education"}),
					downloadtest.WithViewCount(50_000_000),
				),
			},
			wantContains: "Eminem",
			description:  "combined artist+title match must beat title-only non-music result",
		},
		{
			name: "unrelated candidates filtered out",
			track: service.RankTrack{
				Title:          "Save Your Tears",
				Artist:         "The Weeknd",
				ExpectedLength: 215,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=cook1",
					downloadtest.WithTitle("Cooking Tutorial Episode 47"),
					downloadtest.WithChannel("CookingChannel"),
					downloadtest.WithDuration(215),
					downloadtest.WithCategories([]string{"Howto & Style"}),
					downloadtest.WithViewCount(50_000),
				),
			},
			wantNil:     true,
			description: "completely unrelated candidates must be filtered by identity threshold",
		},
		{
			name: "feat track prefers candidate naming the featured artist",
			track: service.RankTrack{
				Title:          "Smaxk Or Die (feat. Playboi Carti)",
				Artist:         "Fatt Smaxk",
				ExpectedLength: 0,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=solo",
					downloadtest.WithTitle("Fatt Smaxk - Smaxk Or Die (Official Music Video) | [Dir. By Kharkee]"),
					downloadtest.WithChannel("FattSmaxkVEVO"),
					downloadtest.WithDuration(150),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(2_000_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=feat",
					downloadtest.WithTitle("Fatt Smaxk - Smaxk Or Die (feat. Playboi Carti)"),
					downloadtest.WithChannel("FattSmaxk"),
					downloadtest.WithDuration(181),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(50_000),
				),
			},
			wantContains: "feat. Playboi Carti",
			description:  "a (feat. X) track must prefer a candidate naming X over an equally-scored, higher-view solo cut",
		},
		{
			name: "same title different artists picks correct one by channel",
			track: service.RankTrack{
				Title:          "Circles",
				Artist:         "Post Malone",
				ExpectedLength: 215,
			},
			candidates: []domain.AudioCandidate{
				downloadtest.Candidate(
					"https://youtube.com/watch?v=postmalone",
					downloadtest.WithTitle("Circles"),
					downloadtest.WithChannel("Post Malone - Topic"),
					downloadtest.WithDuration(215),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(20_000_000),
				),
				downloadtest.Candidate(
					"https://youtube.com/watch?v=macmiller",
					downloadtest.WithTitle("Circles"),
					downloadtest.WithChannel("Mac Miller - Topic"),
					downloadtest.WithDuration(283),
					downloadtest.WithCategories([]string{"Music"}),
					downloadtest.WithViewCount(15_000_000),
				),
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
	track := service.RankTrack{Title: "Fell In Love", Artist: "Lil Tecca", ExpectedLength: 150}

	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://soundcloud.com/liltecca/fell-in-love",
			downloadtest.WithTitle("Lil Tecca - Fell In Love"),
			downloadtest.WithChannel("Lil Tecca"),
			downloadtest.WithDuration(150),
		),
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
	track := service.RankTrack{Title: "Blinding Lights", Artist: "The Weeknd", ExpectedLength: 200}

	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://soundcloud.com/x/blinding-lights",
			downloadtest.WithTitle("The Weeknd - Blinding Lights"),
			downloadtest.WithChannel("The Weeknd"),
			downloadtest.WithDuration(200),
		),
		downloadtest.Candidate(
			"https://youtube.com/watch?v=topic",
			downloadtest.WithTitle("Blinding Lights"),
			downloadtest.WithChannel("The Weeknd - Topic"),
			downloadtest.WithDuration(200),
			downloadtest.WithCategories([]string{"Music"}),
		),
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
	veto, qualifiers := service.UnrequestedQualifiers("Rollacoasta", "prettifun", "prettifun - Rollacoasta (Instrumental) [100% Accurate]")
	assertLabels(t, "veto", veto, []string{"instrumental", "100% accurate"})
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestUnrequestedQualifiers_ZeroWidthSpaceInsideQualifierIsStillVetoed(t *testing.T) {
	veto, qualifiers := service.UnrequestedQualifiers("Rollacoasta", "prettifun", "prettifun - Rollacoasta (Instru\u200bmental)")
	assertLabels(t, "veto", veto, []string{"instrumental"})
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestUnrequestedQualifiers_ReactionVideoIsVetoed(t *testing.T) {
	veto, qualifiers := service.UnrequestedQualifiers("8AM In Charlotte", "Drake", "ImDontai Reacts To Drake 8AM In Charlotte")
	assertLabels(t, "veto", veto, []string{"reacts"})
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestUnrequestedQualifiers_RadioEditIsQualifierNotEdit(t *testing.T) {
	veto, qualifiers := service.UnrequestedQualifiers("Song", "Someone", "Song (Radio Edit)")
	assertLabels(t, "veto", veto, nil)
	assertLabels(t, "qualifiers", qualifiers, []string{"radio edit"})
}

func TestUnrequestedQualifiers_RequestedWordInTrackTitleIsNotAQualifier(t *testing.T) {
	veto, qualifiers := service.UnrequestedQualifiers("Live Forever", "Oasis", "Live Forever (Remastered)")
	assertLabels(t, "veto", veto, nil)
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestUnrequestedQualifiers_RequestedWordAtEndOfTrackTitleIsNotAQualifier(t *testing.T) {
	veto, qualifiers := service.UnrequestedQualifiers("Forever Live", "Oasis", "Forever Live (Remastered)")
	assertLabels(t, "veto", veto, nil)
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestRankCandidates_AcousticLosesToTheMasterOnTheSameTopicChannel(t *testing.T) {
	track := service.RankTrack{Title: "Sunglasses at Night", Artist: "Corey Hart", ExpectedLength: 232}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://youtube.com/watch?v=acoustic000",
			downloadtest.WithTitle("Sunglasses at Night (Acoustic Version)"),
			downloadtest.WithChannel("Corey Hart - Topic"),
			downloadtest.WithDuration(232),
			downloadtest.WithCategories([]string{"Music"}),
			downloadtest.WithViewCount(9_000_000),
		),
		downloadtest.Candidate(
			"https://youtube.com/watch?v=master00000",
			downloadtest.WithTitle("Sunglasses at Night"),
			downloadtest.WithChannel("Corey Hart - Topic"),
			downloadtest.WithDuration(232),
			downloadtest.WithCategories([]string{"Music"}),
			downloadtest.WithViewCount(1_000),
		),
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
	track := service.RankTrack{Title: "Never Surrender", Artist: "Corey Hart", ExpectedLength: 262}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://youtube.com/watch?v=video000000",
			downloadtest.WithTitle("Never Surrender (Official Music Video)"),
			downloadtest.WithChannel("Corey Hart - Topic"),
			downloadtest.WithDuration(262),
			downloadtest.WithCategories([]string{"Music"}),
			downloadtest.WithViewCount(8_000_000),
		),
		downloadtest.Candidate(
			"https://youtube.com/watch?v=audio000000",
			downloadtest.WithTitle("Never Surrender"),
			downloadtest.WithChannel("Corey Hart - Topic"),
			downloadtest.WithDuration(262),
			downloadtest.WithCategories([]string{"Music"}),
			downloadtest.WithViewCount(3_000),
		),
	}

	ranked, _ := rank(track, candidates...)
	if ranked[0].Title != "Never Surrender" {
		t.Fatalf("selected %q — the plain audio must outrank the video container", ranked[0].Title)
	}
}

func TestRankCandidates_ProvenanceStillBeatsQualifierDistanceOffTopic(t *testing.T) {
	track := service.RankTrack{Title: "Die For You", Artist: "The Weeknd", ExpectedLength: 260}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://youtube.com/watch?v=lyrics00000",
			downloadtest.WithTitle("The Weeknd - Die For You (Lyrics)"),
			downloadtest.WithChannel("LyricsChannel"),
			downloadtest.WithDuration(261),
			downloadtest.WithCategories([]string{"Music"}),
			downloadtest.WithViewCount(50_000_000),
		),
		downloadtest.Candidate(
			"https://youtube.com/watch?v=vevo0000000",
			downloadtest.WithTitle("The Weeknd - Die For You (Official Video)"),
			downloadtest.WithChannel("TheWeekndVEVO"),
			downloadtest.WithDuration(262),
			downloadtest.WithCategories([]string{"Music"}),
			downloadtest.WithViewCount(900_000_000),
		),
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
			veto, qualifiers := service.UnrequestedQualifiers(tt.title, tt.artist, tt.candidate)
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
		veto, qualifiers := service.UnrequestedQualifiers(title, artist, candidate)
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
			veto, qualifiers := service.UnrequestedQualifiers(tt.title, tt.artist, tt.candidate)
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
			veto, _ := service.UnrequestedQualifiers("Song", "Someone", tt.candidate)
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
			veto, _ := service.UnrequestedQualifiers("Song", "Someone", candidate)
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
		veto, qualifiers := service.UnrequestedQualifiers("Come Together", "The Beatles", title)
		assertLabels(t, title+" veto", veto, nil)
		assertLabels(t, title+" qualifiers", qualifiers, nil)
	}
}

func TestUnrequestedQualifiers_OtherMixesStayVetoed(t *testing.T) {
	for _, title := range []string{"Song (Who Mix?)", "Song (Club Mix)"} {
		veto, _ := service.UnrequestedQualifiers("Song", "A", title)
		assertLabels(t, title+" veto", veto, []string{"mix"})
	}
}

func TestRank_RejectsUnrequestedVersionBeforeDownload(t *testing.T) {
	track := service.RankTrack{Title: "Rollacoasta", Artist: "prettifun", ExpectedLength: 159}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://youtube.com/watch?v=instrumental",
			downloadtest.WithTitle("prettifun - Rollacoasta (Instrumental) [100% Accurate]"),
			downloadtest.WithChannel("ProdADN"),
			downloadtest.WithDuration(159),
			downloadtest.WithCategories([]string{"Music"}),
		),
		downloadtest.Candidate(
			"https://youtube.com/watch?v=clean",
			downloadtest.WithTitle("Rollacoasta"),
			downloadtest.WithChannel("prettifun"),
			downloadtest.WithDuration(159),
			downloadtest.WithCategories([]string{"Music"}),
		),
	}

	ranked, rejected := rank(track, candidates...)

	if len(ranked) != 1 || ranked[0].URL != "https://youtube.com/watch?v=clean" {
		t.Fatalf("ranked = %v, want only the clean upload", ranked)
	}
	if len(rejected) != 1 || rejected[0].Stage != service.StageQualifier || rejected[0].Reason != "unrequested instrumental, 100% accurate" {
		t.Fatalf("rejected = %+v, want one qualifier rejection naming its labels", rejected)
	}
}

func TestRank_RadioEditRanksAfterEveryCleanCandidate(t *testing.T) {
	track := service.RankTrack{Title: "Harbour Lights", Artist: "The Marram", ExpectedLength: 236}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://x.example/edit",
			downloadtest.WithTitle("Harbour Lights (Radio Edit)"),
			downloadtest.WithChannel("The Marram - Topic"),
			downloadtest.WithDuration(236),
			downloadtest.WithCategories([]string{"Music"}),
		),
		downloadtest.Candidate(
			"https://x.example/clean",
			downloadtest.WithTitle("Harbour Lights"),
			downloadtest.WithChannel("The Marram - Topic"),
			downloadtest.WithDuration(200),
			downloadtest.WithCategories([]string{"Music"}),
		),
	}

	ranked, rejected := rank(track, candidates...)

	if len(rejected) != 0 || len(ranked) != 2 || ranked[0].URL != "https://x.example/clean" {
		t.Fatalf("ranked = %v rejected = %v, want the clean upload first and the edit kept", ranked, rejected)
	}
}

func TestRank_MustHold8_UnplayableCandidatesAreRejectedBeforeRanking(t *testing.T) {
	track := service.RankTrack{Title: "Drinking in L.A.", Artist: "Bran Van 3000", ExpectedLength: 240}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"sc:drm",
			downloadtest.WithTitle("Drinking in L.A."),
			downloadtest.WithUnplayable("drm"),
		),
		downloadtest.Candidate(
			"sc:preview",
			downloadtest.WithTitle("Drinking in L.A."),
			downloadtest.WithUnplayable("preview"),
		),
		downloadtest.Candidate("sc:ok", downloadtest.WithTitle("Drinking in L.A.")),
	}

	ranked, rejected := rank(track, candidates...)

	if len(ranked) != 1 || ranked[0].URL != "sc:ok" {
		t.Fatalf("ranked = %v, want only the playable candidate so the downloader never sees the others", ranked)
	}
	if len(rejected) != 2 || rejected[0].Stage != service.StageDRM || rejected[1].Stage != service.StagePreview {
		t.Fatalf("rejected = %v, want drm then preview", rejected)
	}
}

func TestUnrequestedQualifiers_FullwidthBracketsGetTheSameVetoAsAscii(t *testing.T) {
	wantVeto, _ := service.UnrequestedQualifiers("Song", "Artist", "Artist - Song [Slowed]")
	veto, qualifiers := service.UnrequestedQualifiers("Song", "Artist", "Artist - Song ［Ｓｌｏｗｅｄ］")
	assertLabels(t, "veto", veto, wantVeto)
	assertLabels(t, "qualifiers", qualifiers, nil)
	if len(veto) == 0 {
		t.Fatal("veto is empty, want the slowed qualifier")
	}
}

func TestUnrequestedQualifiers_TitleWithoutBracketHasNoVeto(t *testing.T) {
	veto, qualifiers := service.UnrequestedQualifiers("Song", "Artist", "Artist - Song")
	assertLabels(t, "veto", veto, nil)
	assertLabels(t, "qualifiers", qualifiers, nil)
}

func TestRank_AnArtTrackIsResolvedByItsKind(t *testing.T) {
	track := service.RankTrack{Title: "Matches", Artist: "Tezzus", ExpectedLength: 180}
	weak := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=weakAAAAAAA",
		downloadtest.WithTitle("Unrelated Words"),
		downloadtest.WithDuration(180),
	)

	art := weak
	art.Kind = domain.KindArtTrack
	if got := best(track, art); got == nil || got.URL != art.URL {
		t.Errorf("selected %v, want the art track: its kind skips the identity gate", got)
	}

	upload := weak
	upload.Kind = domain.KindUpload
	if got := best(track, upload); got != nil {
		t.Errorf("selected %v, want nothing: an upload goes through the identity gate", got)
	}
}

func TestRank_AnExplicitResolvedTrackBeatsACloserCleanOne(t *testing.T) {
	track := service.RankTrack{Title: "Matches", Artist: "Tezzus", ExpectedLength: 180}
	clean := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=cleanAAAAAA",
		downloadtest.WithTitle("Matches"),
		downloadtest.WithDuration(180),
		downloadtest.WithKind(domain.KindArtTrack),
	)
	explicit := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=explicitAAA",
		downloadtest.WithTitle("Matches"),
		downloadtest.WithDuration(182),
		downloadtest.WithKind(domain.KindArtTrack),
		downloadtest.WithExplicit(true),
	)

	if got := best(track, clean, explicit); got == nil || got.URL != explicit.URL {
		t.Fatalf("selected %v, want the explicit track even though the clean one is closer in length", got)
	}
}

func TestRank_TheCatalogExplicitFlagPicksTheTwin(t *testing.T) {
	clean := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=cleanAAAAAA",
		downloadtest.WithTitle("Matches"),
		downloadtest.WithDuration(182),
		downloadtest.WithKind(domain.KindArtTrack),
	)
	explicit := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=explicitAAA",
		downloadtest.WithTitle("Matches"),
		downloadtest.WithDuration(180),
		downloadtest.WithKind(domain.KindArtTrack),
		downloadtest.WithExplicit(true),
	)
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
			track := service.RankTrack{Title: "Matches", Artist: "Tezzus", ExpectedLength: 180, CatalogExplicit: tt.catalog}
			if got := best(track, explicit, clean); got == nil || got.URL != tt.want {
				t.Fatalf("selected %v, want %s", got, tt.want)
			}
		})
	}
}

func TestUploaderOf_ArtTrackIsTrustedOnlyWhenItsArtistsCreditTheTrackArtist(t *testing.T) {
	artTrack := func(artists ...string) domain.AudioCandidate {
		return downloadtest.Candidate("", downloadtest.WithKind(domain.KindArtTrack), downloadtest.WithArtists(artists))
	}
	tests := []struct {
		name      string
		artist    string
		candidate domain.AudioCandidate
		want      string
	}{
		{"credited art track counts as topic", "ezcodylee", artTrack("ezcodylee"), service.ChannelTopic},
		{"credit matches through normalisation", "ASAP Rocky", artTrack("A$AP Rocky"), service.ChannelTopic},
		{"other artist's art track is untrusted", "ezcodylee", artTrack("Someone Else"), service.ChannelOther},
		{"art track without credits is untrusted", "ezcodylee", artTrack(), service.ChannelOther},
		{"upload on a topic channel is topic", "ezcodylee", downloadtest.Candidate("", downloadtest.WithChannel("ezcodylee - Topic")), service.ChannelTopic},
	}
	for _, tt := range tests {
		if got := service.UploaderOf(tt.artist, tt.candidate); got != tt.want {
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
		veto, qualifiers := service.UnrequestedQualifiers("Song", "Artist", title)
		assertLabels(t, title+" veto", veto, []string{label})
		assertLabels(t, title+" qualifiers", qualifiers, nil)
	}
	for _, title := range []string{"Song (Remastered)", "Song (2011 Remaster)"} {
		veto, qualifiers := service.UnrequestedQualifiers("Song", "Artist", title)
		assertLabels(t, title+" veto", veto, nil)
		assertLabels(t, title+" qualifiers", qualifiers, nil)
	}
	veto, _ := service.UnrequestedQualifiers("Song (Acoustic)", "Artist", "Song (Acoustic)")
	assertLabels(t, "requested acoustic veto", veto, nil)
}

func TestRank_RejectsPlainlyTitledAcousticAndDemoUploads(t *testing.T) {
	track := service.RankTrack{Title: "Sunglasses at Night", Artist: "Corey Hart", ExpectedLength: 233}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://www.youtube.com/watch?v=acousticUp1",
			downloadtest.WithTitle("Sunglasses at Night (Acoustic)"),
			downloadtest.WithChannel("Corey Hart - Topic"),
			downloadtest.WithDuration(233),
			downloadtest.WithCategories([]string{"Music"}),
		),
		downloadtest.Candidate(
			"https://www.youtube.com/watch?v=demoUpload1",
			downloadtest.WithTitle("Sunglasses at Night (Demo)"),
			downloadtest.WithChannel("Corey Hart - Topic"),
			downloadtest.WithDuration(233),
			downloadtest.WithCategories([]string{"Music"}),
		),
		downloadtest.Candidate(
			"https://www.youtube.com/watch?v=masterUp001",
			downloadtest.WithTitle("Sunglasses at Night"),
			downloadtest.WithChannel("Corey Hart"),
			downloadtest.WithDuration(232),
			downloadtest.WithCategories([]string{"Music"}),
		),
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
	pin := downloadtest.Candidate(
		"",
		downloadtest.WithKind(domain.KindCatalogPin),
		downloadtest.WithArtists([]string{"Sophie Ellis-Bextor"}),
	)
	if got := service.UploaderOf("Sophie Ellis-Bextor", pin); got != service.ChannelTopic {
		t.Errorf("UploaderOf = %q, want the user's own pick trusted as %q", got, service.ChannelTopic)
	}
	pin.Title, pin.Channel = "Murder On The Dancefloor", "someone-else"
	if got := evaluation(t, song("Murder On The Dancefloor", "Sophie Ellis-Bextor", 230), pin)["artist_match"]; got != true {
		t.Errorf("artist_match = %v, want the user's own pick matched for a channel that does not name the artist", got)
	}
}

func TestUploaderOf_APinnedVideoIsTrustedByItsCredits(t *testing.T) {
	pinned := func(artists ...string) domain.AudioCandidate {
		return downloadtest.Candidate("", downloadtest.WithKind(domain.KindPinnedVideo), downloadtest.WithArtists(artists))
	}
	tests := []struct {
		name      string
		artist    string
		candidate domain.AudioCandidate
		want      string
	}{
		{"credited pinned video counts as topic", "slayr", pinned("slayr"), service.ChannelTopic},
		{"credit matches through normalisation", "ASAP Rocky", pinned("A$AP Rocky"), service.ChannelTopic},
		{"other artist's pinned video is untrusted", "slayr", pinned("Someone Else"), service.ChannelOther},
		{"pinned video without credits is untrusted", "slayr", pinned(), service.ChannelOther},
		{"upload crediting the artist stays untrusted", "slayr", downloadtest.Candidate(
			"",
			downloadtest.WithKind(domain.KindUpload),
			downloadtest.WithArtists([]string{"slayr"}),
		), service.ChannelOther},
	}
	for _, tt := range tests {
		if got := service.UploaderOf(tt.artist, tt.candidate); got != tt.want {
			t.Errorf("%s: UploaderOf = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRankCandidates_TheUsersPickGoesBeforeTheArtTrack(t *testing.T) {
	track := service.RankTrack{Title: "Murder On The Dancefloor", Artist: "Sophie Ellis-Bextor", ExpectedLength: 230}
	artTrack := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=RCtTDJgxPDA",
		downloadtest.WithTitle("Murder On The Dancefloor"),
		downloadtest.WithDuration(232),
		downloadtest.WithArtists([]string{"Sophie Ellis-Bextor"}),
		downloadtest.WithKind(domain.KindArtTrack),
	)
	pickedUpload := downloadtest.Candidate(
		"https://soundcloud.com/sophie-ellis-bextor-official/murder-on-the-dancefloor-album",
		downloadtest.WithTitle("Murder On The Dancefloor"),
		downloadtest.WithArtists([]string{"Sophie Ellis-Bextor"}),
		downloadtest.WithKind(domain.KindCatalogPin),
	)
	catalogVideo := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=Zv6M1xG4T0w",
		downloadtest.WithTitle("Murder On The Dancefloor"),
		downloadtest.WithArtists([]string{"Sophie Ellis-Bextor"}),
		downloadtest.WithKind(domain.KindPinnedVideo),
	)
	tests := []struct {
		name       string
		candidates []domain.AudioCandidate
		wantFirst  string
	}{
		{"pick listed after the art track", []domain.AudioCandidate{artTrack, pickedUpload}, pickedUpload.URL},
		{"pick listed before the art track", []domain.AudioCandidate{pickedUpload, artTrack}, pickedUpload.URL},
		{"a pinned video stays behind the art track", []domain.AudioCandidate{catalogVideo, artTrack}, artTrack.URL},
		{"pick still first beside a pinned video and an art track", []domain.AudioCandidate{artTrack, catalogVideo, pickedUpload}, pickedUpload.URL},
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
	upload := func(channel string) domain.AudioCandidate {
		return downloadtest.Candidate("", downloadtest.WithChannel(channel), downloadtest.WithKind(domain.KindUpload))
	}
	tests := []struct {
		name      string
		artist    string
		candidate domain.AudioCandidate
		want      string
	}{
		{"the artist's own topic channel is topic", "Cutting Crew", upload("Cutting Crew - Topic"), service.ChannelTopic},
		{"topic channel matches through normalisation", "ASAP Rocky", upload("A$AP Rocky - Topic"), service.ChannelTopic},
		{"a karaoke topic channel is untrusted", "Cutting Crew", upload("Karaoke Hits - Topic"), service.ChannelOther},
		{"a namesake topic channel is untrusted", "Che", upload("Chechive - Topic"), service.ChannelOther},
		{"a substring namesake channel is untrusted", "Eve", upload("Steve Lacy"), service.ChannelOther},
		{"the artist's own channel is artist", "Cutting Crew", upload("Cutting Crew"), service.ChannelArtist},
		{"the artist's VEVO channel is artist", "The Weeknd", upload("TheWeekndVEVO"), service.ChannelArtist},
		{"the artist's official channel is artist", "Eve", upload("Eve Official"), service.ChannelArtist},
	}
	for _, tt := range tests {
		if got := service.UploaderOf(tt.artist, tt.candidate); got != tt.want {
			t.Errorf("%s: UploaderOf = %q, want %q", tt.name, got, tt.want)
		}
	}
	steve := downloadtest.Candidate(
		"",
		downloadtest.WithTitle("Eve - Song"),
		downloadtest.WithChannel("Steve Lacy"),
		downloadtest.WithKind(domain.KindUpload),
	)
	if got := evaluation(t, song("Song", "Eve", 200), steve)["artist_match"]; got != false {
		t.Errorf("artist_match for Eve on Steve Lacy's channel = %v, want a whole-name comparison", got)
	}
}

func TestUploaderOf_AnyCreditedArtistOwnsTheTopicChannel(t *testing.T) {
	upload := func(channel string) domain.AudioCandidate {
		return downloadtest.Candidate("", downloadtest.WithChannel(channel), downloadtest.WithKind(domain.KindUpload))
	}
	tests := []struct {
		name      string
		artist    string
		candidate domain.AudioCandidate
		want      string
	}{
		{"first of a featuring credit", "Usher featuring Pitbull", upload("Usher - Topic"), service.ChannelTopic},
		{"second of a featuring credit", "Usher featuring Pitbull", upload("Pitbull - Topic"), service.ChannelTopic},
		{"first of an ampersand credit", "Fatt Smaxk & Playboi Carti", upload("Fatt Smaxk - Topic"), service.ChannelTopic},
		{"the duo's whole name", "Nico & Vinz", upload("Nico & Vinz - Topic"), service.ChannelTopic},
		{"a comma credit", "Earth, Wind & Fire", upload("Earth, Wind & Fire - Topic"), service.ChannelTopic},
		{"first credited artist's own channel", "Usher featuring Pitbull", upload("Usher"), service.ChannelArtist},
		{"a karaoke topic channel stays untrusted", "Usher featuring Pitbull", upload("Karaoke Hits - Topic"), service.ChannelOther},
		{"a namesake topic channel stays untrusted", "Fatt Smaxk & Playboi Carti", upload("Chechive - Topic"), service.ChannelOther},
		{"another artist's topic channel stays untrusted", "Usher featuring Pitbull", upload("DJ's United - Topic"), service.ChannelOther},
	}
	for _, tt := range tests {
		if got := service.UploaderOf(tt.artist, tt.candidate); got != tt.want {
			t.Errorf("%s: UploaderOf = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRank_AnArtistChannelUploadOutranksAnEqualReupload(t *testing.T) {
	track := service.RankTrack{Title: "Blinding Lights", Artist: "The Weeknd", ExpectedLength: 200}
	reupload := downloadtest.Candidate(
		"https://www.youtube.com/watch?v=aaaaaaaaaaa",
		downloadtest.WithTitle("Blinding Lights"),
		downloadtest.WithChannel("RandomUploader"),
		downloadtest.WithDuration(200),
		downloadtest.WithKind(domain.KindUpload),
	)
	artist := downloadtest.Candidate(
		"https://www.youtube.com/watch?v=zzzzzzzzzzz",
		downloadtest.WithTitle("Blinding Lights"),
		downloadtest.WithChannel("The Weeknd"),
		downloadtest.WithDuration(200),
		downloadtest.WithKind(domain.KindUpload),
	)

	got := best(track, reupload, artist)
	if got == nil || got.URL != artist.URL {
		t.Fatalf("selected %v, want the upload from the artist's own channel over an equal re-upload", got)
	}
}

func TestRank_RejectsRerecordingTitleTheTrackDoesNotCarry(t *testing.T) {
	track := service.RankTrack{Title: "It's Only Love", Artist: "Bryan Adams", ExpectedLength: 195}
	for _, title := range []string{
		"It's Only Love (Classic Version)",
		"It's Only Love (Taylor's Version)",
		"It's Only Love (Re-Recorded)",
		"It's Only Love (Re-Recording)",
	} {
		for _, kind := range []domain.CandidateKind{domain.KindArtTrack, domain.KindUpload} {
			c := downloadtest.Candidate(
				"https://music.youtube.com/watch?v=Ml2N72dMv_8",
				downloadtest.WithTitle(title),
				downloadtest.WithDuration(194),
				downloadtest.WithKind(kind),
				downloadtest.WithArtists([]string{"Bryan Adams"}),
				downloadtest.WithChannel("Bryan Adams - Topic"),
			)
			ranked, rejected := rank(track, c)
			if len(ranked) != 0 || len(rejected) != 1 || rejected[0].Stage != service.StageQualifier || rejected[0].Reason != "unrequested re-recorded" {
				t.Errorf("%q kind %q: ranked=%d rejected=%+v", title, kind, len(ranked), rejected)
			}
		}
	}
}

func TestRank_RanksRerecordingWhenTrackCarriesIt(t *testing.T) {
	track := service.RankTrack{Title: "It’s Only Love (classic version)", Artist: "Bryan Adams", ExpectedLength: 194}
	c := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=Ml2N72dMv_8",
		downloadtest.WithTitle("It's Only Love (Classic Version)"),
		downloadtest.WithDuration(194),
		downloadtest.WithKind(domain.KindArtTrack),
		downloadtest.WithArtists([]string{"Bryan Adams"}),
		downloadtest.WithChannel("Bryan Adams - Topic"),
	)
	ranked, rejected := rank(track, c)
	if len(ranked) != 1 || len(rejected) != 0 {
		t.Errorf("ranked=%d rejected=%+v", len(ranked), rejected)
	}
}

func TestRank_TheUsersOwnPickIsStillRefusedWhenItIsARerecording(t *testing.T) {
	track := service.RankTrack{Title: "It's Only Love", Artist: "Bryan Adams", ExpectedLength: 195}
	for _, kind := range []domain.CandidateKind{domain.KindCatalogPin, domain.KindPinnedVideo} {
		c := downloadtest.Candidate(
			"https://music.youtube.com/watch?v=pin",
			downloadtest.WithTitle("It's Only Love (Classic Version)"),
			downloadtest.WithDuration(194),
			downloadtest.WithKind(kind),
			downloadtest.WithArtists([]string{"Bryan Adams"}),
			downloadtest.WithChannel("Bryan Adams - Topic"),
		)
		ranked, rejected := rank(track, c)
		if len(ranked) != 0 || len(rejected) != 1 || rejected[0].Stage != service.StageQualifier || rejected[0].Reason != "unrequested re-recorded" {
			t.Errorf("kind %q: ranked=%d rejected=%+v, want one re-recorded qualifier rejection", kind, len(ranked), rejected)
		}
	}
}

func TestRank_RerecordingPhraseIsRefusedWhateverItsCaseApostropheOrBracket(t *testing.T) {
	track := service.RankTrack{Title: "Love Story", Artist: "Taylor Swift", ExpectedLength: 235}
	for _, title := range []string{
		"Love Story (Taylor’s Version)",
		"Love Story (TAYLOR'S VERSION)",
		"Love Story [Taylors Version]",
		"Love Story - Taylor's Version",
		"Love Story (classic version)",
		"Love Story (RE-RECORDING)",
		"Love Story （Taylor's Version）",
	} {
		c := downloadtest.Candidate(
			"https://music.youtube.com/watch?v=x",
			downloadtest.WithTitle(title),
			downloadtest.WithDuration(235),
			downloadtest.WithKind(domain.KindArtTrack),
			downloadtest.WithArtists([]string{"Taylor Swift"}),
			downloadtest.WithChannel("Taylor Swift - Topic"),
		)
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
		track := service.RankTrack{Title: c.track, Artist: "Bryan Adams", ExpectedLength: 195}
		cand := downloadtest.Candidate(
			"https://music.youtube.com/watch?v=y",
			downloadtest.WithTitle(c.candidate),
			downloadtest.WithDuration(195),
			downloadtest.WithKind(domain.KindArtTrack),
			downloadtest.WithArtists([]string{"Bryan Adams"}),
			downloadtest.WithChannel("Bryan Adams - Topic"),
		)
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
	track := service.RankTrack{Title: "It's Only Love", Artist: "Bryan Adams", ExpectedLength: 195}
	c := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=z",
		downloadtest.WithTitle("It's Only Love"),
		downloadtest.WithDuration(195),
		downloadtest.WithKind(domain.KindArtTrack),
		downloadtest.WithArtists([]string{"Bryan Adams"}),
		downloadtest.WithChannel("Taylor's Version Archive"),
		downloadtest.WithAlbum("Classic Version Hits"),
	)
	ranked, rejected := rank(track, c)
	if len(ranked) != 1 || len(rejected) != 0 {
		t.Errorf("ranked=%d rejected=%+v, want the plainly titled candidate ranked", len(ranked), rejected)
	}
}

func TestRank_TrackTitledAsATaylorsVersionAcceptsEitherApostrophe(t *testing.T) {
	track := service.RankTrack{Title: "Love Story (Taylor's Version)", Artist: "Taylor Swift", ExpectedLength: 235}
	c := downloadtest.Candidate(
		"https://music.youtube.com/watch?v=tv",
		downloadtest.WithTitle("Love Story (Taylor’s Version)"),
		downloadtest.WithDuration(235),
		downloadtest.WithKind(domain.KindArtTrack),
		downloadtest.WithArtists([]string{"Taylor Swift"}),
		downloadtest.WithChannel("Taylor Swift - Topic"),
	)
	ranked, rejected := rank(track, c)
	if len(ranked) != 1 || len(rejected) != 0 {
		t.Errorf("ranked=%d rejected=%+v, want the Taylor's Version ranked", len(ranked), rejected)
	}
}

func TestRank_RefusingTheRerecordingStillRanksTheOriginalBesideIt(t *testing.T) {
	track := service.RankTrack{Title: "It's Only Love", Artist: "Bryan Adams", ExpectedLength: 195}
	candidates := []domain.AudioCandidate{
		downloadtest.Candidate(
			"https://music.youtube.com/watch?v=classic",
			downloadtest.WithTitle("It's Only Love (Classic Version)"),
			downloadtest.WithDuration(194),
			downloadtest.WithKind(domain.KindArtTrack),
			downloadtest.WithArtists([]string{"Bryan Adams"}),
			downloadtest.WithChannel("Bryan Adams - Topic"),
		),
		downloadtest.Candidate(
			"https://www.youtube.com/watch?v=original",
			downloadtest.WithTitle("It's Only Love"),
			downloadtest.WithDuration(195),
			downloadtest.WithChannel("Bryan Adams"),
			downloadtest.WithCategories([]string{"Music"}),
		),
	}
	ranked, rejected := rank(track, candidates...)
	if len(ranked) != 1 || ranked[0].URL != "https://www.youtube.com/watch?v=original" {
		t.Fatalf("ranked=%v, want only the original", ranked)
	}
	if len(rejected) != 1 || rejected[0].Reason != "unrequested re-recorded" {
		t.Fatalf("rejected=%+v, want the Classic Version refused as re-recorded", rejected)
	}
}

func TestRank_EveryCandidateIsRankedOrRefusedExactlyOnceWhateverTheOrder(t *testing.T) {
	titles := []string{"Song", "Song (Live)", "Song (Remix)", "Song [Instrumental]", "Other Thing", "Artist - Song"}
	channels := []string{"Artist - Topic", "ArtistVEVO", "Someone Else", ""}
	rapid.Check(t, func(t *rapid.T) {
		count := rapid.IntRange(0, 12).Draw(t, "count")
		candidates := make([]domain.AudioCandidate, count)
		for i := range candidates {
			candidates[i] = domain.AudioCandidate{
				Title:      rapid.SampledFrom(titles).Draw(t, "title"),
				Channel:    rapid.SampledFrom(channels).Draw(t, "channel"),
				Duration:   rapid.Float64Range(60, 400).Draw(t, "duration"),
				ViewCount:  rapid.Int64Range(0, 1_000_000).Draw(t, "views"),
				URL:        fmt.Sprintf("https://example.com/watch?v=%d", i),
				Unplayable: rapid.SampledFrom([]string{"", "", "", domain.UnplayablePreview}).Draw(t, "unplayable"),
			}
		}
		track := service.RankTrack{Title: "Song", Artist: "Artist", ExpectedLength: 200}
		shuffled := rapid.Permutation(candidates).Draw(t, "shuffled")

		ranked, refused, _ := service.Rank(track, candidates)
		rankedAgain, refusedAgain, _ := service.Rank(track, shuffled)

		if len(ranked)+len(refused) != len(candidates) {
			t.Fatalf("%d ranked + %d refused, want %d candidates accounted for", len(ranked), len(refused), len(candidates))
		}
		if got, want := urlSet(ranked), urlSet(rankedAgain); !reflect.DeepEqual(got, want) {
			t.Fatalf("ranked set changed with the input order: %v then %v", got, want)
		}
		if got, want := refusalSet(refused), refusalSet(refusedAgain); !reflect.DeepEqual(got, want) {
			t.Fatalf("refused set changed with the input order: %v then %v", got, want)
		}
		seen := map[string]bool{}
		for _, c := range ranked {
			seen[c.URL] = true
		}
		for _, r := range refused {
			if seen[r.Candidate.URL] {
				t.Fatalf("%s is both ranked and refused", r.Candidate.URL)
			}
		}
	})
}

func urlSet(candidates []domain.AudioCandidate) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.URL
	}
	slices.Sort(out)
	return out
}

func refusalSet(refused []service.RankRefusal) []string {
	out := make([]string, len(refused))
	for i, r := range refused {
		out[i] = r.Candidate.URL + "|" + string(r.Stage)
	}
	slices.Sort(out)
	return out
}

func song(title, artist string, length float64) service.RankTrack {
	return service.RankTrack{Title: title, Artist: artist, ExpectedLength: length}
}

func rank(track service.RankTrack, candidates ...domain.AudioCandidate) ([]domain.AudioCandidate, []service.RankRefusal) {
	ranked, refused, _ := service.Rank(track, candidates)
	return ranked, refused
}

func best(track service.RankTrack, candidates ...domain.AudioCandidate) *domain.AudioCandidate {
	ranked, _ := rank(track, candidates...)
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

func urls(candidates []domain.AudioCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.URL)
	}
	return out
}

func evaluations(t *testing.T, track service.RankTrack, candidates ...domain.AudioCandidate) []map[string]any {
	t.Helper()
	_, _, evaluated := service.Rank(track, candidates)
	records := make([]map[string]any, 0, len(evaluated))
	for _, ev := range evaluated {
		records = append(records, map[string]any{
			"identity_score":     ev.IdentityScore,
			"metadata_rank":      ev.MetadataRank,
			"qualifier_distance": float64(ev.QualifierDistance),
			"is_topic":           ev.IsTopic,
			"artist_match":       ev.ArtistMatch,
			"feature_match":      ev.FeatureMatch,
		})
	}
	return records
}

func evaluation(t *testing.T, track service.RankTrack, c domain.AudioCandidate) map[string]any {
	t.Helper()
	records := evaluations(t, track, c)
	if len(records) != 1 {
		t.Fatalf("evaluated %d candidates for %q, want 1", len(records), c.Title)
	}
	return records[0]
}

func number(t *testing.T, record map[string]any, key string) float64 {
	t.Helper()
	value, ok := record[key].(float64)
	if !ok {
		t.Fatalf("%s = %v, want a number in %v", key, record[key], record)
	}
	return value
}

func joinLabels(labels []string) string {
	return strings.Join(labels, "|")
}

func assertLabels(t *testing.T, name string, got, want []string) {
	t.Helper()
	if joinLabels(got) != joinLabels(want) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
