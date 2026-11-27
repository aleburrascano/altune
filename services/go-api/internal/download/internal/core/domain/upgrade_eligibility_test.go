package domain_test

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/domain"
	"testing"
	"time"
)

var specNow = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

func eligibleRow() domain.UpgradeRow {
	return domain.UpgradeRow{Ready: true, HasAudio: true, SourceURL: "https://example.test/a", Grade: audioquality.C}
}

func specRules() domain.UpgradeRules {
	return domain.UpgradeRules{Generation: 2, RetryAfter: time.Hour, MaxQueued: 5, Now: specNow}
}

func TestUpgradeEligible(t *testing.T) {
	longAgo, justNow := specNow.Add(-2*time.Hour), specNow.Add(-time.Minute)
	cases := []struct {
		name   string
		row    func(*domain.UpgradeRow)
		rules  func(*domain.UpgradeRules)
		wanted bool
	}{
		{"a ready graded file with a source", func(*domain.UpgradeRow) {}, func(*domain.UpgradeRules) {}, true},
		{"not ready", func(r *domain.UpgradeRow) { r.Ready = false }, func(*domain.UpgradeRules) {}, false},
		{"no audio", func(r *domain.UpgradeRow) { r.HasAudio = false }, func(*domain.UpgradeRules) {}, false},
		{"no source", func(r *domain.UpgradeRow) { r.SourceURL = "" }, func(*domain.UpgradeRules) {}, false},
		{"no source but an in place judgement is due", func(r *domain.UpgradeRow) { r.SourceURL = "" }, func(u *domain.UpgradeRules) { u.CheckInPlace = true }, true},
		{"no source and the judgement is done", func(r *domain.UpgradeRow) { r.SourceURL = ""; r.InPlaceGeneration = 2 }, func(u *domain.UpgradeRules) { u.CheckInPlace = true }, false},
		{"a job is queued", func(r *domain.UpgradeRow) { r.JobQueued = true }, func(*domain.UpgradeRules) {}, false},
		{"ungraded counts as the worst gradeable", func(r *domain.UpgradeRow) { r.Grade = "" }, func(*domain.UpgradeRules) {}, true},
		{"the user's own upload", func(r *domain.UpgradeRow) { r.Grade = audioquality.AUser }, func(*domain.UpgradeRules) {}, false},
		{"best available", func(r *domain.UpgradeRow) { r.Grade = audioquality.BestAvailable }, func(*domain.UpgradeRules) {}, false},
		{"grade A is left alone", func(r *domain.UpgradeRow) { r.Grade = audioquality.A }, func(*domain.UpgradeRules) {}, false},
		{"grade A awaiting the in place judgement", func(r *domain.UpgradeRow) { r.Grade = audioquality.A }, func(u *domain.UpgradeRules) { u.CheckInPlace = true }, true},
		{"grade A muxed under the floor", func(r *domain.UpgradeRow) { r.Grade = audioquality.A; r.SourceFormatID = "18" }, func(u *domain.UpgradeRules) { u.LowBitrateFloorKbps = 112 }, true},
		{"grade A muxed with the rescue off", func(r *domain.UpgradeRow) { r.Grade = audioquality.A; r.SourceFormatID = "18" }, func(*domain.UpgradeRules) {}, false},
		{"grade A below the floor", func(r *domain.UpgradeRow) { r.Grade = audioquality.A; r.SourceKbps = 111 }, func(u *domain.UpgradeRules) { u.LowBitrateFloorKbps = 112 }, true},
		{"grade A at the floor", func(r *domain.UpgradeRow) { r.Grade = audioquality.A; r.SourceKbps = 112 }, func(u *domain.UpgradeRules) { u.LowBitrateFloorKbps = 112 }, false},
		{"grade A with an unknown bitrate", func(r *domain.UpgradeRow) { r.Grade = audioquality.A }, func(u *domain.UpgradeRules) { u.LowBitrateFloorKbps = 112 }, false},
		{"given up this generation", func(r *domain.UpgradeRow) { r.BestAvailableGeneration = 2 }, func(*domain.UpgradeRules) {}, false},
		{"given up an earlier generation", func(r *domain.UpgradeRow) { r.BestAvailableGeneration = 1 }, func(*domain.UpgradeRules) {}, true},
		{"tried long ago", func(r *domain.UpgradeRow) { r.UpgradeTriedAt = &longAgo }, func(*domain.UpgradeRules) {}, true},
		{"tried just now", func(r *domain.UpgradeRow) { r.UpgradeTriedAt = &justNow }, func(*domain.UpgradeRules) {}, false},
		{"the upgrade queue is full", func(*domain.UpgradeRow) {}, func(u *domain.UpgradeRules) { u.QueuedUpgrades = 5 }, false},
		{"one slot left in the upgrade queue", func(*domain.UpgradeRow) {}, func(u *domain.UpgradeRules) { u.QueuedUpgrades = 4 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, rules := eligibleRow(), specRules()
			tc.row(&row)
			tc.rules(&rules)
			if got := domain.UpgradeEligible(row, rules); got != tc.wanted {
				t.Errorf("UpgradeEligible = %v, want %v", got, tc.wanted)
			}
		})
	}
}
