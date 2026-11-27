package candidateranking_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/core/candidateranking"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"fmt"
	"math"
	"testing"
)

const (
	channelWeight  = 0.45
	lengthWeight   = 0.25
	categoryWeight = 0.20
	viewsWeight    = 0.10

	otherChannel   = 0.3
	unknownLength  = 0.5
	noMusic        = 0.2
	unknownViews   = 0.5
	rankPrecision  = 0.001
	scorePrecision = 0.01
)

func metadataRank(t *testing.T, track candidateranking.Track, c download.AudioCandidate) float64 {
	t.Helper()
	return number(t, evaluation(t, track, c), "metadata_rank")
}

func near(got, want, precision float64) bool {
	return math.Abs(got-want) <= precision
}

func TestRank_MetadataRankWeighsChannelLengthCategoryAndViews(t *testing.T) {
	track := song("Song", "Someone", 200)
	vevo := download.AudioCandidate{Title: "Someone - Song", Channel: "LabelVEVO", Duration: 210, Categories: []string{"Comedy"}, ViewCount: 500, URL: "a"}
	popular := download.AudioCandidate{Title: "Someone - Song", Channel: "x", ViewCount: 1000, URL: "b"}

	records := evaluations(t, track, vevo, popular)

	if got, want := number(t, records[0], "metadata_rank"), 0.45*0.8+0.25*0.5+0.20*0.2+0.10*0.5; !near(got, want, rankPrecision) {
		t.Errorf("metadata_rank = %v, want %v for a VEVO channel, a loose length, no music category and half the top views", got, want)
	}
}

func TestRank_LengthScoreFollowsTheTightAndLooseWindows(t *testing.T) {
	tests := []struct {
		name     string
		expected float64
		actual   float64
		want     float64
	}{
		{name: "exact match", expected: 200, actual: 200, want: 1.0},
		{name: "within tight threshold", expected: 200, actual: 202, want: 1.0},
		{name: "just past tight", expected: 200, actual: 203, want: 0.5},
		{name: "between tight and loose", expected: 200, actual: 210, want: 0.5},
		{name: "at loose boundary", expected: 200, actual: 215, want: 0.5},
		{name: "beyond loose threshold", expected: 200, actual: 216, want: 0.0},
		{name: "zero expected is unknown", expected: 0, actual: 200, want: unknownLength},
		{name: "zero actual is unknown", expected: 200, actual: 0, want: unknownLength},
		{name: "both zero is unknown", expected: 0, actual: 0, want: unknownLength},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := download.AudioCandidate{Title: "Someone - Song", Channel: "x", Duration: tt.actual}
			got := (metadataRank(t, song("Song", "Someone", tt.expected), c) - channelWeight*otherChannel - categoryWeight*noMusic - viewsWeight*unknownViews) / lengthWeight
			if !near(got, tt.want, scorePrecision) {
				t.Errorf("length score for %vs against %vs = %v, want %v", tt.actual, tt.expected, got, tt.want)
			}
		})
	}
}

func TestRank_LengthScoreUsesTheConfidenceWindows(t *testing.T) {
	tests := []struct {
		name     string
		expected float64
		delta    float64
	}{
		{name: "3s off is loose", expected: 200, delta: 3},
		{name: "tight edge", expected: 200, delta: 2},
		{name: "flat loose edge", expected: 200, delta: 15},
		{name: "past flat loose edge", expected: 200, delta: 16},
		{name: "400s track 25s off is loose", expected: 400, delta: 25},
		{name: "400s track at fractional edge", expected: 400, delta: 28},
		{name: "400s track past fractional edge", expected: 400, delta: 29},
	}
	lengthScoreOf := map[float64]float64{0.4: 1.0, 0.3: 0.5, 0.2: 0.0}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confidence := service.ScoreConfidence(service.Evidence{Channel: candidateranking.ChannelOther, DurationDeltaSeconds: &tt.delta}, tt.expected)
			want, known := lengthScoreOf[confidence]
			if !known {
				t.Fatalf("ScoreConfidence = %v, not a length bonus this test knows", confidence)
			}
			c := download.AudioCandidate{Title: "Someone - Song", Channel: "x", Duration: tt.expected + tt.delta}
			got := (metadataRank(t, song("Song", "Someone", tt.expected), c) - channelWeight*otherChannel - categoryWeight*noMusic - viewsWeight*unknownViews) / lengthWeight
			if !near(got, want, scorePrecision) {
				t.Errorf("length score %v, the confidence window implies %v", got, want)
			}
		})
	}
}

func TestRank_ChannelScoreTrustsTopicAndArtistThenVevo(t *testing.T) {
	tests := []struct {
		name      string
		artist    string
		candidate download.AudioCandidate
		want      float64
	}{
		{"topic class", "The Weeknd", download.AudioCandidate{Channel: "The Weeknd - Topic"}, 1.0},
		{"topic class without a channel", "The Weeknd", download.AudioCandidate{Kind: ports.KindArtTrack, Artists: []string{"The Weeknd"}}, 1.0},
		{"artist class", "The Weeknd", download.AudioCandidate{Channel: "TheWeeknd"}, 1.0},
		{"vevo channel", "Someone Else", download.AudioCandidate{Channel: "TheWeekndVEVO"}, 0.8},
		{"vevo mixed case", "Someone Else", download.AudioCandidate{Channel: "SomeArtistVevo"}, 0.8},
		{"regular channel", "Someone Else", download.AudioCandidate{Channel: "RandomUploader"}, otherChannel},
		{"empty channel", "Someone Else", download.AudioCandidate{}, otherChannel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.candidate.Title = "Song"
			got := (metadataRank(t, song("Song", tt.artist, 0), tt.candidate) - lengthWeight*unknownLength - categoryWeight*noMusic - viewsWeight*unknownViews) / channelWeight
			if !near(got, tt.want, scorePrecision) {
				t.Errorf("channel score for %q = %v, want %v", tt.candidate.Channel, got, tt.want)
			}
		})
	}
}

func TestRank_CategoryScoreRewardsTheMusicCategory(t *testing.T) {
	tests := []struct {
		name       string
		categories []string
		want       float64
	}{
		{name: "music category present", categories: []string{"Music"}, want: 1.0},
		{name: "music among others", categories: []string{"Entertainment", "Music"}, want: 1.0},
		{name: "no music category", categories: []string{"Education", "Howto & Style"}, want: noMusic},
		{name: "empty categories", categories: []string{}, want: noMusic},
		{name: "nil categories", categories: nil, want: noMusic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := download.AudioCandidate{Title: "Someone - Song", Channel: "x", Categories: tt.categories}
			got := (metadataRank(t, song("Song", "Someone", 0), c) - channelWeight*otherChannel - lengthWeight*unknownLength - viewsWeight*unknownViews) / categoryWeight
			if !near(got, tt.want, scorePrecision) {
				t.Errorf("category score for %v = %v, want %v", tt.categories, got, tt.want)
			}
		})
	}
}

func TestRank_ViewScoreIsAShareOfTheMostViewedCandidate(t *testing.T) {
	tests := []struct {
		name  string
		views []int64
		want  float64
	}{
		{name: "the most viewed", views: []int64{1000, 500}, want: 1.0},
		{name: "half the most viewed", views: []int64{500, 1000}, want: 0.5},
		{name: "no views anywhere is unknown", views: []int64{0, 0}, want: unknownViews},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var candidates []download.AudioCandidate
			for i, views := range tt.views {
				candidates = append(candidates, download.AudioCandidate{Title: "Someone - Song", Channel: "x", ViewCount: views, URL: string(rune('a' + i))})
			}
			records := evaluations(t, song("Song", "Someone", 0), candidates...)
			got := (number(t, records[0], "metadata_rank") - channelWeight*otherChannel - lengthWeight*unknownLength - categoryWeight*noMusic) / viewsWeight
			if !near(got, tt.want, scorePrecision) {
				t.Errorf("view score = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRank_IdentityScoreRewardsArtistAndTitleAndDiscountsTitleOnly(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		artist    string
		candidate string
		wantAbove float64
		wantBelow float64
	}{
		{name: "exact artist+title match", title: "Blinding Lights", artist: "The Weeknd", candidate: "The Weeknd - Blinding Lights", wantAbove: 80},
		{name: "title only match is penalized", title: "Die Hard", artist: "Dr. Dre", candidate: "DIE HARD", wantBelow: 65},
		{name: "correct artist+title beats wrong artist", title: "Die Hard", artist: "Dr. Dre", candidate: "Dr. Dre - Die Hard", wantAbove: 80},
		{name: "unrelated candidate scores very low", title: "HUMBLE.", artist: "Kendrick Lamar", candidate: "Cooking Tutorial Episode 47", wantBelow: 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := number(t, evaluation(t, song(tt.title, tt.artist, 0), download.AudioCandidate{Title: tt.candidate}), "identity_score")
			if tt.wantAbove > 0 && score < tt.wantAbove {
				t.Errorf("identity_score = %.1f, want >= %.1f", score, tt.wantAbove)
			}
			if tt.wantBelow > 0 && score >= tt.wantBelow {
				t.Errorf("identity_score = %.1f, want < %.1f", score, tt.wantBelow)
			}
		})
	}
}

func TestRank_AnExactTitleWithoutTheArtistSitsOnTheIdentityGateAndPasses(t *testing.T) {
	track := song("Song", "Someone With A Long Name", 200)
	c := download.AudioCandidate{Title: "Song", Channel: "RandomUploader", URL: "https://x.example/song"}

	if got := number(t, evaluation(t, track, c), "identity_score"); got != 60 {
		t.Fatalf("identity_score = %v, want exactly the 60 gate: a full title-only match scores 100 times 0.6", got)
	}
	ranked, refused := rank(track, c)
	if len(ranked) != 1 || len(refused) != 0 {
		t.Fatalf("ranked = %v refused = %+v, want a score on the gate to pass it", urls(ranked), refused)
	}
}

func TestRank_BelowTheIdentityGateTheRefusalNamesTheScore(t *testing.T) {
	c := download.AudioCandidate{Title: "Cooking Tutorial Episode 47", Channel: "CookingChannel", URL: "yt:cooking"}

	track := song("Save Your Tears", "The Weeknd", 215)
	_, refused := rank(track, c)

	if len(refused) != 1 || refused[0].Stage != candidateranking.StageIdentity || refused[0].Candidate.URL != "yt:cooking" {
		t.Fatalf("refused = %+v, want one identity refusal for yt:cooking", refused)
	}
	score := number(t, evaluation(t, track, c), "identity_score")
	if want := fmt.Sprintf("identity %.0f below threshold 60", score); refused[0].Reason != want {
		t.Errorf("reason = %q, want %q", refused[0].Reason, want)
	}
}

func TestRank_EvaluationCarriesScoresAndTheCandidate(t *testing.T) {
	track := candidateranking.Track{ID: "t1", Title: "Song", Artist: "Artist", ExpectedLength: 200}
	c := download.AudioCandidate{Source: "youtube", Title: "Song", Channel: "Artist - Topic", Duration: 200, ViewCount: 50, Categories: []string{"Music"}}

	_, _, evaluated := candidateranking.Rank(track, []download.AudioCandidate{c})

	if len(evaluated) != 1 {
		t.Fatalf("evaluated %d candidates, want 1", len(evaluated))
	}
	got := evaluated[0]
	if got.Candidate.Title != "Song" || got.Candidate.Channel != "Artist - Topic" || got.Candidate.Source != "youtube" {
		t.Errorf("candidate = %+v, want the one ranked", got.Candidate)
	}
	if got.IdentityScore != 60 || got.MetadataRank != 1 || got.QualifierDistance != 0 {
		t.Errorf("scores = %v/%v/%v, want 60/1/0", got.IdentityScore, got.MetadataRank, got.QualifierDistance)
	}
	if !got.IsTopic || !got.ArtistMatch || !got.FeatureMatch {
		t.Errorf("flags topic/artist/feature = %v/%v/%v, want all true", got.IsTopic, got.ArtistMatch, got.FeatureMatch)
	}
}

func TestRank_UnplayableCandidatesAreNotEvaluated(t *testing.T) {
	track := song("Song", "Artist", 200)
	drm := download.AudioCandidate{Title: "Song", Unplayable: "drm"}

	_, refused, evaluated := candidateranking.Rank(track, []download.AudioCandidate{drm})

	if len(evaluated) != 0 || len(refused) != 1 {
		t.Fatalf("evaluated %d, refused %d, want 0 and 1", len(evaluated), len(refused))
	}
}

func TestRank_EvaluationRoundsScores(t *testing.T) {
	track := song("Blinding Lights", "The Weeknd", 200)
	c := download.AudioCandidate{Title: "The Weeknd - Blinding Light", Channel: "LabelVEVO", Duration: 210, Categories: []string{"Comedy"}, ViewCount: 1}

	record := evaluation(t, track, c)

	identity := number(t, record, "identity_score")
	if identity != math.Round(identity*10)/10 || identity < 90 || identity >= 100 {
		t.Errorf("identity_score = %v, want a near match rounded to one decimal", identity)
	}
	if got, want := number(t, record, "metadata_rank"), 0.45*0.8+0.25*0.5+0.20*0.2+0.10*1.0; got != math.Round(want*1000)/1000 {
		t.Errorf("metadata_rank = %v, want %v rounded to three decimals", got, want)
	}
}
