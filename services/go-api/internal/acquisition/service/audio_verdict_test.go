package service_test

import (
	"altune/go-api/internal/acquisition"
	"math"
	"testing"
)

func acoustIDResult(id string, score float64) acquisition.AcoustIDResult {
	return acquisition.AcoustIDResult{ID: id, Score: score}
}

func TestAudioVerdictOf_ScoreIsTheTopResultScore(t *testing.T) {
	results := []acquisition.AcoustIDResult{acoustIDResult("low", 0.6), acoustIDResult("top", 0.93), acoustIDResult("mid", 0.7)}

	verdict := acquisition.AudioVerdictOf(acquisition.JudgeVerdict{}, results)

	if verdict.Score != 0.93 {
		t.Errorf("Score = %v, want 0.93", verdict.Score)
	}
}

func TestAudioVerdictOf_ScoreIgnoresANaNResultScore(t *testing.T) {
	results := []acquisition.AcoustIDResult{
		acoustIDResult("malformed", math.NaN()),
		acoustIDResult("top", 0.93),
		acoustIDResult("mid", 0.7),
	}

	verdict := acquisition.AudioVerdictOf(acquisition.JudgeVerdict{}, results)

	if verdict.Score != 0.93 {
		t.Errorf("Score = %v, want 0.93 (a NaN score must not poison the top score)", verdict.Score)
	}
}
