package service_test

import (
	"altune/go-api/internal/acquisition"
	"context"
	"testing"
)

const (
	channelTopic  = "topic"
	channelArtist = "artist"
	channelOther  = "other"

	confidenceFloor = 0.5
)

func TestSourceKindOf(t *testing.T) {
	cases := []struct {
		name    string
		kind    acquisition.CandidateKind
		omv     bool
		channel string
		want    acquisition.SourceKind
	}{
		{"art track", acquisition.KindArtTrack, false, channelOther, acquisition.ArtTrack},
		{"observed official video", acquisition.KindPinnedVideo, true, channelOther, acquisition.OfficialVideo},
		{"pinned video of an unknown type is not official", acquisition.KindPinnedVideo, false, channelTopic, acquisition.FanUpload},
		{"upload from the artist's channel", acquisition.KindUpload, false, channelArtist, acquisition.LabelUpload},
		{"upload from a topic channel", acquisition.KindUpload, false, channelTopic, acquisition.LabelUpload},
		{"upload from any other channel", acquisition.KindUpload, false, channelOther, acquisition.FanUpload},
		{"catalog pin is not assumed official", acquisition.KindCatalogPin, false, channelTopic, acquisition.FanUpload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acquisition.SourceKindOf(acquisition.AudioCandidate{Kind: tc.kind, ObservedOMV: tc.omv}, tc.channel); got != tc.want {
				t.Errorf("SourceKindOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUploaderSourceKind(t *testing.T) {
	cases := []struct {
		name, artist, channel string
		want                  acquisition.SourceKind
	}{
		{"the artist's own channel", "Daft Punk", "Daft Punk", acquisition.LabelUpload},
		{"a topic channel", "Daft Punk", "Daft Punk - Topic", acquisition.LabelUpload},
		{"someone else's channel", "Daft Punk", "lyrics hub", acquisition.FanUpload},
		{"no channel", "Daft Punk", "", acquisition.FanUpload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acquisition.UploaderSourceKind(tc.artist, tc.channel); got != tc.want {
				t.Errorf("UploaderSourceKind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFingerprintOf(t *testing.T) {
	cases := map[acquisition.VerdictKind]acquisition.GradeFingerprint{
		acquisition.VerdictHard:          acquisition.Matches,
		acquisition.VerdictSoft:          acquisition.Approximates,
		acquisition.VerdictOtherVersion:  acquisition.Contradicts,
		acquisition.VerdictDifferentSong: acquisition.FingerprintUnsupported,
		acquisition.VerdictUnknown:       acquisition.FingerprintSilent,
		"":                               acquisition.FingerprintSilent,
	}
	for verdict, want := range cases {
		if got := acquisition.FingerprintOf(verdict); got != want {
			t.Errorf("FingerprintOf(%q) = %v, want %v", verdict, got, want)
		}
	}
}

func TestScoreConfidence(t *testing.T) {
	tests := []struct {
		name  string
		e     acquisition.ConfidenceEvidence
		track float64
		want  float64
	}{
		{"hard", acquisition.ConfidenceEvidence{Verdict: "hard", AcoustIDScore: 0.97}, 204, 0.95},
		{"soft", acquisition.ConfidenceEvidence{Verdict: "soft", AcoustIDScore: 0.95}, 204, 0.85},
		{"hard with a low score", acquisition.ConfidenceEvidence{Verdict: "hard", AcoustIDScore: 0.8}, 204, 0.85},
		{"soft with a low score stays at the unknown cap", acquisition.ConfidenceEvidence{Verdict: "soft", AcoustIDScore: 0.8}, 204, 0.8},
		{"hard with no score recorded", acquisition.ConfidenceEvidence{Verdict: "hard"}, 204, 0.95},
		{"topic one second off", acquisition.ConfidenceEvidence{Verdict: "unknown", Channel: channelTopic, DurationDeltaSeconds: secs(1)}, 204, 0.7},
		{"other channel twenty seconds off", acquisition.ConfidenceEvidence{Verdict: "unknown", Channel: channelOther, DurationDeltaSeconds: secs(20)}, 204, 0.2},
		{"artist channel on the spot", acquisition.ConfidenceEvidence{Verdict: "unknown", Channel: channelArtist, DurationDeltaSeconds: secs(0)}, 204, 0.7},
		{"no fingerprint run", acquisition.ConfidenceEvidence{Channel: channelTopic, DurationDeltaSeconds: secs(-1)}, 204, 0.7},
		{"loose duration window", acquisition.ConfidenceEvidence{Channel: channelOther, DurationDeltaSeconds: secs(10)}, 204, 0.3},
		{"loose window grows with the track", acquisition.ConfidenceEvidence{Channel: channelOther, DurationDeltaSeconds: secs(20)}, 400, 0.3},
		{"qualifier costs 0.2", acquisition.ConfidenceEvidence{Channel: channelTopic, DurationDeltaSeconds: secs(0), Qualifiers: []string{"radio edit"}}, 204, 0.5},
		{"resolved adds 0.1 and the cap holds", acquisition.ConfidenceEvidence{Channel: channelTopic, Resolved: true, DurationDeltaSeconds: secs(0)}, 204, 0.8},
		{"unknown length earns no length bonus", acquisition.ConfidenceEvidence{Verdict: "unknown", Channel: channelTopic}, 204, 0.5},
		{"never below zero", acquisition.ConfidenceEvidence{Channel: channelOther, DurationDeltaSeconds: secs(90), Qualifiers: []string{"edit"}}, 204, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := acquisition.ScoreConfidence(tt.e, tt.track); got != tt.want {
				t.Errorf("ScoreConfidence = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScoreConfidence_RanksHardOverSoftOverEveryUnknown(t *testing.T) {
	best := acquisition.ScoreConfidence(acquisition.ConfidenceEvidence{Channel: channelTopic, Resolved: true, DurationDeltaSeconds: secs(0)}, 204)
	hard := acquisition.ScoreConfidence(acquisition.ConfidenceEvidence{Verdict: "hard", AcoustIDScore: 0.5}, 204)
	soft := acquisition.ScoreConfidence(acquisition.ConfidenceEvidence{Verdict: "soft", AcoustIDScore: 0.5}, 204)
	if !(hard > soft && soft >= best) {
		t.Errorf("hard %v, soft %v, best unknown %v: want hard > soft >= unknown", hard, soft, best)
	}
}

type tornerWatch struct{}

func (tornerWatch) Watch(context.Context, string) (acquisition.YtmWatch, error) {
	return acquisition.YtmWatch{
		VideoID: "UeGkPDoLKjI",
		Title:   "Tornerò",
		Artists: []string{"I Santo California"},
		Type:    acquisition.VideoTypeATV,
	}, nil
}

func TestConfidence_ACreditedPinnedVideoClearsTheFloor(t *testing.T) {
	req := acquisition.FindRequest{
		Title:          "Tornerò",
		Artist:         "I Santo California",
		CatalogSources: []acquisition.CatalogSource{{Provider: "youtube", ExternalID: "UeGkPDoLKjI"}},
	}
	found, err := acquisition.NewYtMusicSource(nil, tornerWatch{}).Find(context.Background(), req)
	if err != nil || len(found) != 1 {
		t.Fatalf("Find = %+v, %v", found, err)
	}
	evidence := acquisition.ConfidenceEvidence{
		Channel:              channelTopic,
		Resolved:             found[0].IsResolved(),
		DurationDeltaSeconds: secs(-6.08),
		SourceTitle:          found[0].Title,
	}

	if got := acquisition.ScoreConfidence(evidence, 245); got < confidenceFloor {
		t.Errorf("confidence = %v, want at least the %v floor", got, confidenceFloor)
	}
}

func secs(v float64) *float64 { return &v }

func TestScoreConfidence_AnUnknownVerdictScoresFromZeroToEightTenths(t *testing.T) {
	lowest := acquisition.ConfidenceEvidence{Channel: channelOther, DurationDeltaSeconds: secs(90), Qualifiers: []string{"edit"}}
	highest := acquisition.ConfidenceEvidence{Channel: channelTopic, Resolved: true, DurationDeltaSeconds: secs(0)}

	if got := acquisition.ScoreConfidence(lowest, 204); got != 0 {
		t.Errorf("lowest unknown score = %v, want 0: a constant change that drops it below 0 needs the zero clamp back", got)
	}
	if got := acquisition.ScoreConfidence(highest, 204); got != 0.8 {
		t.Errorf("highest unknown score = %v, want 0.8: a constant change that lifts it past the unknown cap needs the cap back", got)
	}
}
