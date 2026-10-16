package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/audiojudge"
	"math"
	"testing"
)

func acoustIDResult(id string, score float64) ports.AcoustIDResult {
	return ports.AcoustIDResult{ID: id, Score: score}
}

func TestAudioVerdictOf_ScoreIsTheTopResultScore(t *testing.T) {
	results := []ports.AcoustIDResult{acoustIDResult("low", 0.6), acoustIDResult("top", 0.93), acoustIDResult("mid", 0.7)}

	verdict := audioVerdictOf(audiojudge.Verdict{}, results)

	if verdict.Score != 0.93 {
		t.Errorf("Score = %v, want 0.93", verdict.Score)
	}
}

func TestAudioVerdictOf_ScoreIgnoresANaNResultScore(t *testing.T) {
	results := []ports.AcoustIDResult{
		acoustIDResult("malformed", math.NaN()),
		acoustIDResult("top", 0.93),
		acoustIDResult("mid", 0.7),
	}

	verdict := audioVerdictOf(audiojudge.Verdict{}, results)

	if verdict.Score != 0.93 {
		t.Errorf("Score = %v, want 0.93 (a NaN score must not poison the top score)", verdict.Score)
	}
}
