package service

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	"altune/go-api/internal/acquisition/ports"
	"context"
	"math"
	"testing"
)

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
		{"topic one second off", Evidence{Verdict: "unknown", Channel: "topic", DurationDeltaSeconds: 1}, 204, 0.7},
		{"other channel twenty seconds off", Evidence{Verdict: "unknown", Channel: "other", DurationDeltaSeconds: 20}, 204, 0.2},
		{"artist channel on the spot", Evidence{Verdict: "unknown", Channel: "artist"}, 204, 0.7},
		{"no fingerprint run", Evidence{Channel: "topic", DurationDeltaSeconds: -1}, 204, 0.7},
		{"loose duration window", Evidence{Channel: "other", DurationDeltaSeconds: 10}, 204, 0.3},
		{"loose window grows with the track", Evidence{Channel: "other", DurationDeltaSeconds: 20}, 400, 0.3},
		{"qualifier costs 0.2", Evidence{Channel: "topic", DurationDeltaSeconds: 0, Qualifiers: []string{"radio edit"}}, 204, 0.5},
		{"resolved adds 0.1 and the cap holds", Evidence{Channel: "topic", Resolved: true}, 204, 0.8},
		{"never below zero", Evidence{Channel: "other", DurationDeltaSeconds: 90, Qualifiers: []string{"edit"}}, 204, 0},
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
	best := ScoreConfidence(Evidence{Channel: "topic", Resolved: true}, 204)
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
		DurationDeltaSeconds: -6.08,
		SourceTitle:          found[0].Title,
	}

	if got := ScoreConfidence(evidence, 245); got < defaultConfidenceFloor {
		t.Errorf("confidence = %v, want at least the %v floor", got, defaultConfidenceFloor)
	}
}
