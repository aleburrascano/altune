package service

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	"altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"context"
	"math"
	"testing"
)

func TestSourceKindOf(t *testing.T) {
	cases := []struct {
		name    string
		kind    ports.CandidateKind
		channel string
		want    grade.SourceKind
	}{
		{"art track", ports.KindArtTrack, channelOther, grade.ArtTrack},
		{"official video", ports.KindPinnedVideo, channelOther, grade.OfficialVideo},
		{"upload from the artist's channel", ports.KindUpload, channelArtist, grade.LabelUpload},
		{"upload from a topic channel", ports.KindUpload, channelTopic, grade.LabelUpload},
		{"upload from any other channel", ports.KindUpload, channelOther, grade.FanUpload},
		{"catalog pin is not assumed official", ports.KindCatalogPin, channelTopic, grade.FanUpload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SourceKindOf(ports.AudioCandidate{Kind: tc.kind}, tc.channel); got != tc.want {
				t.Errorf("SourceKindOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUploaderSourceKind(t *testing.T) {
	cases := []struct {
		name, artist, channel string
		want                  grade.SourceKind
	}{
		{"the artist's own channel", "Daft Punk", "Daft Punk", grade.LabelUpload},
		{"a topic channel", "Daft Punk", "Daft Punk - Topic", grade.LabelUpload},
		{"someone else's channel", "Daft Punk", "lyrics hub", grade.FanUpload},
		{"no channel", "Daft Punk", "", grade.FanUpload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UploaderSourceKind(tc.artist, tc.channel); got != tc.want {
				t.Errorf("UploaderSourceKind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFingerprintOf(t *testing.T) {
	cases := map[VerdictKind]grade.Fingerprint{
		VerdictHard:          grade.Matches,
		VerdictSoft:          grade.Approximates,
		VerdictOtherVersion:  grade.Contradicts,
		VerdictDifferentSong: grade.Contradicts,
		VerdictUnknown:       grade.Silent,
		"":                   grade.Silent,
	}
	for verdict, want := range cases {
		if got := FingerprintOf(verdict); got != want {
			t.Errorf("FingerprintOf(%q) = %v, want %v", verdict, got, want)
		}
	}
}

func TestScoreConfidence(t *testing.T) {
	tests := []struct {
		name  string
		e     Evidence
		track float64
		want  float64
	}{
		{"hard", Evidence{Verdict: "hard", AcoustIDScore: 0.97}, 204, 0.95},
		{"soft", Evidence{Verdict: "soft", AcoustIDScore: 0.95}, 204, 0.85},
		{"hard with a low score", Evidence{Verdict: "hard", AcoustIDScore: 0.8}, 204, 0.85},
		{"soft with a low score stays at the unknown cap", Evidence{Verdict: "soft", AcoustIDScore: 0.8}, 204, 0.8},
		{"hard with no score recorded", Evidence{Verdict: "hard"}, 204, 0.95},
		{"topic one second off", Evidence{Verdict: "unknown", Channel: "topic", DurationDeltaSeconds: secs(1)}, 204, 0.7},
		{"other channel twenty seconds off", Evidence{Verdict: "unknown", Channel: "other", DurationDeltaSeconds: secs(20)}, 204, 0.2},
		{"artist channel on the spot", Evidence{Verdict: "unknown", Channel: "artist", DurationDeltaSeconds: secs(0)}, 204, 0.7},
		{"no fingerprint run", Evidence{Channel: "topic", DurationDeltaSeconds: secs(-1)}, 204, 0.7},
		{"loose duration window", Evidence{Channel: "other", DurationDeltaSeconds: secs(10)}, 204, 0.3},
		{"loose window grows with the track", Evidence{Channel: "other", DurationDeltaSeconds: secs(20)}, 400, 0.3},
		{"qualifier costs 0.2", Evidence{Channel: "topic", DurationDeltaSeconds: secs(0), Qualifiers: []string{"radio edit"}}, 204, 0.5},
		{"resolved adds 0.1 and the cap holds", Evidence{Channel: "topic", Resolved: true, DurationDeltaSeconds: secs(0)}, 204, 0.8},
		{"unknown length earns no length bonus", Evidence{Verdict: "unknown", Channel: "topic"}, 204, 0.5},
		{"never below zero", Evidence{Channel: "other", DurationDeltaSeconds: secs(90), Qualifiers: []string{"edit"}}, 204, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScoreConfidence(tt.e, tt.track); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("ScoreConfidence = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScoreConfidence_RanksHardOverSoftOverEveryUnknown(t *testing.T) {
	best := ScoreConfidence(Evidence{Channel: "topic", Resolved: true, DurationDeltaSeconds: secs(0)}, 204)
	hard := ScoreConfidence(Evidence{Verdict: "hard", AcoustIDScore: 0.5}, 204)
	soft := ScoreConfidence(Evidence{Verdict: "soft", AcoustIDScore: 0.5}, 204)
	if !(hard > soft && soft >= best) {
		t.Errorf("hard %v, soft %v, best unknown %v: want hard > soft >= unknown", hard, soft, best)
	}
}

type tornerWatch struct{}

func (tornerWatch) Watch(context.Context, string) (ytmsearch.Watch, error) {
	return ytmsearch.Watch{
		VideoID: "UeGkPDoLKjI",
		Title:   "Tornerò",
		Artists: []string{"I Santo California"},
		Type:    ytmsearch.VideoTypeATV,
	}, nil
}

func TestConfidence_ACreditedPinnedVideoClearsTheFloor(t *testing.T) {
	req := ports.FindRequest{
		Title:          "Tornerò",
		Artist:         "I Santo California",
		CatalogSources: []ports.CatalogSource{{Provider: "youtube", ExternalID: "UeGkPDoLKjI"}},
	}
	found, err := ytmusic.NewSource(nil, tornerWatch{}).Find(context.Background(), req)
	if err != nil || len(found) != 1 {
		t.Fatalf("Find = %+v, %v", found, err)
	}
	evidence := Evidence{
		Channel:              uploaderClass(req.Artist, found[0]),
		Resolved:             found[0].Resolved,
		DurationDeltaSeconds: secs(-6.08),
		SourceTitle:          found[0].Title,
	}

	if got := ScoreConfidence(evidence, 245); got < defaultConfidenceFloor {
		t.Errorf("confidence = %v, want at least the %v floor", got, defaultConfidenceFloor)
	}
}

func secs(v float64) *float64 { return &v }

func TestFallbackLengthMatches(t *testing.T) {
	allowance := domain.LengthAuthoritative.Allowance(204)
	tests := []struct {
		name  string
		delta *float64
		want  bool
	}{
		{"unknown length", nil, false},
		{"on the spot", secs(0), true},
		{"at the allowance", secs(allowance), true},
		{"past the allowance", secs(allowance + 0.5), false},
		{"short past the allowance", secs(-allowance - 0.5), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fallbackLengthMatches(tt.delta, 204); got != tt.want {
				t.Errorf("fallbackLengthMatches = %v, want %v", got, tt.want)
			}
		})
	}
}
