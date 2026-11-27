package domain

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"slices"
	"time"
)

const LowBitrateSourceFormatID = "18"

func SettledGrades() []audioquality.Grade {
	return []audioquality.Grade{audioquality.AUser, audioquality.BestAvailable}
}

type UpgradeRow struct {
	Ready                   bool
	HasAudio                bool
	SourceURL               string
	Grade                   audioquality.Grade
	SourceFormatID          string
	SourceKbps              int
	InPlaceGeneration       int
	BestAvailableGeneration int
	UpgradeTriedAt          *time.Time
	JobQueued               bool
}

type UpgradeRules struct {
	Generation          int
	RetryAfter          time.Duration
	MaxQueued           int
	QueuedUpgrades      int
	CheckInPlace        bool
	LowBitrateFloorKbps int
	Now                 time.Time
}

func UpgradeEligible(row UpgradeRow, rules UpgradeRules) bool {
	grade := row.Grade
	if grade == "" {
		grade = audioquality.Ungraded
	}
	inPlaceDue := rules.CheckInPlace && row.InPlaceGeneration < rules.Generation
	return row.Ready &&
		row.HasAudio &&
		(row.SourceURL != "" || inPlaceDue) &&
		!row.JobQueued &&
		!slices.Contains(SettledGrades(), grade) &&
		(grade != audioquality.A || inPlaceDue || lowBitrate(row, rules)) &&
		row.BestAvailableGeneration < rules.Generation &&
		(row.UpgradeTriedAt == nil || row.UpgradeTriedAt.Before(rules.Now.Add(-rules.RetryAfter))) &&
		rules.QueuedUpgrades < rules.MaxQueued
}

func lowBitrate(row UpgradeRow, rules UpgradeRules) bool {
	return rules.LowBitrateFloorKbps > 0 &&
		(row.SourceFormatID == LowBitrateSourceFormatID ||
			(row.SourceKbps >= 1 && row.SourceKbps < rules.LowBitrateFloorKbps))
}
