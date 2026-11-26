package service_test

import (
	"altune/go-api/internal/download"
	"math"
	"testing"
)

func acoustIDResult(id string, score float64) download.AcoustIDResult {
	return download.AcoustIDResult{ID: id, Score: score}
}

func TestAudioVerdictOf_ScoreIsTheTopResultScore(t *testing.T) {
	results := []download.AcoustIDResult{acoustIDResult("low", 0.6), acoustIDResult("top", 0.93), acoustIDResult("mid", 0.7)}

	verdict := download.AudioVerdictOf(download.JudgeVerdict{}, results)

	if verdict.Score != 0.93 {
		t.Errorf("Score = %v, want 0.93", verdict.Score)
	}
}

func TestAudioVerdictOf_ScoreIgnoresANaNResultScore(t *testing.T) {
	results := []download.AcoustIDResult{
		acoustIDResult("malformed", math.NaN()),
		acoustIDResult("top", 0.93),
		acoustIDResult("mid", 0.7),
	}

	verdict := download.AudioVerdictOf(download.JudgeVerdict{}, results)

	if verdict.Score != 0.93 {
		t.Errorf("Score = %v, want 0.93 (a NaN score must not poison the top score)", verdict.Score)
	}
}
