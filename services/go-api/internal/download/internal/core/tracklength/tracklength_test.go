package tracklength_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/tracklength"
	"math"
	"testing"
)

func upload(seconds float64) ports.AudioCandidate {
	return downloadtest.Candidate("", downloadtest.WithDuration(seconds), downloadtest.WithKind(ports.KindUpload))
}

func artTrack(seconds float64) ports.AudioCandidate {
	return downloadtest.Candidate("", downloadtest.WithDuration(seconds), downloadtest.WithKind(ports.KindArtTrack))
}

func fileAccepted(t *testing.T, p tracklength.Policy, actual float64) bool {
	t.Helper()
	judge, known := p.FileJudge(upload(0))
	if !known {
		t.Fatalf("FileJudge gave no judge for %+v, want one: a length is known", p)
	}
	acceptable, _ := judge.Judge(actual)
	return acceptable
}

func TestPolicy_ADisagreementFallsBackToTheLooseWindow(t *testing.T) {
	p := tracklength.New(232, 276)

	if !fileAccepted(t, p, 276) {
		t.Error("the resolved album cut must be acceptable when the two lengths disagree")
	}
	if !fileAccepted(t, p, 232) {
		t.Error("the saved single edit must stay acceptable too — neither claim is proven")
	}
	if fileAccepted(t, p, 600) {
		t.Error("a 10-minute upload is outside both windows and must still be rejected")
	}
}

func TestPolicy_AgreementKeepsTheTightWindow(t *testing.T) {
	p := tracklength.New(262, 263)

	if !fileAccepted(t, p, 264) {
		t.Error("ordinary encoding trim must pass a corroborated length")
	}
	if fileAccepted(t, p, 276) {
		t.Error("a 14s excess against a corroborated length is contamination and must be rejected")
	}
}

func TestPolicy_NoResolvedLengthKeepsTheLooseWindow(t *testing.T) {
	p := tracklength.New(200, 0)

	if !fileAccepted(t, p, 210) {
		t.Error("without corroboration the 15s window applies")
	}
	if fileAccepted(t, p, 260) {
		t.Error("60s off is a different recording under any window")
	}
}

func TestPolicy_AResolvedLengthAloneKeepsTheLooseWindow(t *testing.T) {
	p := tracklength.New(0, 200)

	if !fileAccepted(t, p, 214) {
		t.Error("with only a resolved length, a 14s difference is inside the loose window")
	}
	if fileAccepted(t, p, 216) {
		t.Error("with only a resolved length, a 16s difference is outside the loose window")
	}
}

func TestPolicy_ANearSilentFileIsNeverCloseToAKnownLength(t *testing.T) {
	if fileAccepted(t, tracklength.New(0, 200), 10) {
		t.Error("a 10s file must be rejected against a resolved 200s")
	}
	if fileAccepted(t, tracklength.New(200, 0), 10) {
		t.Error("a 10s file must be rejected against a saved 200s")
	}
}

func TestPolicy_ASavedLengthAloneIsHeldToTheLooseWindow(t *testing.T) {
	tests := []struct {
		name             string
		expected, actual float64
		want             bool
	}{
		{name: "exact", expected: 226, actual: 226, want: true},
		{name: "few seconds off", expected: 226, actual: 231, want: true},
		{name: "within fraction", expected: 300, actual: 318, want: true},
		{name: "14-minute mix", expected: 226, actual: 840, want: false},
		{name: "30s preview", expected: 226, actual: 30, want: false},
		{name: "short track uses absolute slack", expected: 40, actual: 52, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fileAccepted(t, tracklength.New(tt.expected, 0), tt.actual); got != tt.want {
				t.Errorf("a %vs file against a saved %vs: accepted = %v, want %v", tt.actual, tt.expected, got, tt.want)
			}
		})
	}
}

func TestPolicy_CorroborationRequiresAgreement(t *testing.T) {
	tests := []struct {
		name            string
		saved, resolved float64
		want            bool
	}{
		{"two conflicting claims are not corroboration", 232, 276, false},
		{"two independent sources agreeing is exactly what corroboration means", 262, 263, true},
		{"a single resolved length is one source, not corroboration", 0, 262, false},
		{"a single saved length is one source, not corroboration", 262, 0, false},
		{"a missing saved length never agrees with a tiny resolved one", 0, 3, false},
		{"a missing resolved length never agrees with a tiny saved one", 3, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tracklength.New(tt.saved, tt.resolved).Corroborated(); got != tt.want {
				t.Errorf("Corroborated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPolicy_TightensWhenCorroborated(t *testing.T) {
	loose := tracklength.New(181, 0)
	if !fileAccepted(t, loose, 193) {
		t.Error("without a corroborated length, a 12s excess is inside the loose tolerance")
	}

	tight := tracklength.New(181, 181)
	if fileAccepted(t, tight, 193) {
		t.Error("with a corroborated length, a 12s excess is contamination and must be rejected")
	}
	if !fileAccepted(t, tight, 184) {
		t.Error("a 3s difference is ordinary encoding trim and must still pass")
	}
}

func TestPolicy_OneExpectedLengthForEveryReader(t *testing.T) {
	tests := []struct {
		name   string
		policy tracklength.Policy
		want   float64
	}{
		{"catalog length wins over a trusted resolved one", tracklength.New(139, 167), 139},
		{"resolved length when no target length is saved", tracklength.New(0, 140), 140},
		{"target length when the resolved one is doubted", tracklength.Doubted(139), 139},
		{"target length when nothing was resolved", tracklength.New(139, 0), 139},
		{"nothing when the only resolved length is doubted", tracklength.Doubted(0), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.Expected(); got != tt.want {
				t.Errorf("Expected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPolicy_ADoubtedResolvedLengthIsNoLength(t *testing.T) {
	if got := tracklength.Doubted(139).Resolved(); got != 0 {
		t.Errorf("Resolved() = %v, want 0 for a doubted reference", got)
	}
	if got := tracklength.New(139, 167).Resolved(); got != 167 {
		t.Errorf("Resolved() = %v, want the trusted 167", got)
	}
}

func TestPolicy_ADoubtedResolvedLengthWidensNothing(t *testing.T) {
	p := tracklength.Doubted(139)

	if fileAccepted(t, p, 167) {
		t.Error("a 167s file matches only the doubted search pick and must be rejected")
	}
}

func TestPolicy_ADoubtedResolvedLengthDoesNotMoveThePreDownloadGate(t *testing.T) {
	p := tracklength.Doubted(139)

	if !p.Plausible(upload(140)) {
		t.Error("the 140s art track must pass the pre-download gate against the catalog 139s")
	}
	if p.Plausible(upload(167)) {
		t.Error("the 167s upload must fail the pre-download gate against the catalog 139s")
	}
	if !fileAccepted(t, p, 140) || fileAccepted(t, p, 167) {
		t.Error("the file-length gate must accept 140s and reject 167s against the catalog 139s")
	}
}

func TestPolicy_ThePreDownloadGate(t *testing.T) {
	tests := []struct {
		name      string
		policy    tracklength.Policy
		candidate ports.AudioCandidate
		want      bool
	}{
		{"a candidate that claims no length passes", tracklength.New(139, 0), upload(0), true},
		{"an upload far from the saved length fails", tracklength.New(139, 0), upload(400), false},
		{"an upload is not held to a resolved length alone", tracklength.New(0, 139), upload(400), true},
		{"a resolved candidate is held to a resolved length alone", tracklength.New(0, 139), artTrack(400), false},
		{"a resolved candidate near the resolved length passes", tracklength.New(0, 139), artTrack(141), true},
		{"a resolved candidate passes when no length is known", tracklength.New(0, 0), artTrack(400), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.Plausible(tt.candidate); got != tt.want {
				t.Errorf("Plausible(%+v) = %v, want %v", tt.candidate, got, tt.want)
			}
		})
	}
}

func TestPolicy_AFileLengthIsJudgedOnlyAgainstAKnownLength(t *testing.T) {
	tests := []struct {
		name      string
		policy    tracklength.Policy
		candidate ports.AudioCandidate
		want      bool
	}{
		{"an upload with a saved length", tracklength.New(139, 0), upload(140), true},
		{"an upload with only a resolved length", tracklength.New(0, 139), upload(140), true},
		{"an upload with no known length", tracklength.New(0, 0), upload(140), false},
		{"a resolved candidate carries its own length", tracklength.New(0, 0), artTrack(140), true},
		{"a resolved candidate without a length and no known length", tracklength.New(0, 0), artTrack(0), false},
		{"an upload with only a doubted resolved length", tracklength.Doubted(0), upload(140), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, got := tt.policy.FileJudge(tt.candidate); got != tt.want {
				t.Errorf("FileJudge(%+v) gave a judge = %v, want %v", tt.candidate, got, tt.want)
			}
		})
	}
}

func TestPolicy_AResolvedCandidatesFileIsHeldToItsOwnLengthFirst(t *testing.T) {
	tests := []struct {
		name       string
		policy     tracklength.Policy
		candidate  ports.AudioCandidate
		actual     float64
		acceptable bool
		held       float64
	}{
		{"a file far from the candidate's own length", tracklength.New(0, 0), artTrack(200), 230, false, 200},
		{"a file at the candidate's length with nothing else known", tracklength.New(0, 0), artTrack(200), 202, true, 0},
		{"a file at the candidate's length but far from the saved length", tracklength.New(300, 0), artTrack(200), 202, false, 300},
		{"a file at the candidate's length and the saved length", tracklength.New(205, 0), artTrack(200), 202, true, 205},
		{"a resolved candidate that claims no length is held to the saved length", tracklength.New(205, 0), artTrack(0), 260, false, 205},
		{"an upload is held to the saved length", tracklength.New(205, 0), upload(260), 260, false, 205},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			judge, known := tt.policy.FileJudge(tt.candidate)
			if !known {
				t.Fatalf("FileJudge(%+v) gave no judge, want one", tt.candidate)
			}
			acceptable, held := judge.Judge(tt.actual)
			if acceptable != tt.acceptable || held != tt.held {
				t.Errorf("Judge(%v) for %+v = %v, %v, want %v, %v", tt.actual, tt.candidate, acceptable, held, tt.acceptable, tt.held)
			}
		})
	}
}

func TestFileJudge_ANonFiniteFileLengthIsNeverAcceptable(t *testing.T) {
	judges := []struct {
		name      string
		policy    tracklength.Policy
		candidate ports.AudioCandidate
	}{
		{"held to a saved length", tracklength.New(200, 0), upload(0)},
		{"held to a resolved length", tracklength.New(0, 200), upload(0)},
		{"held to a corroborated length", tracklength.New(200, 200), upload(0)},
		{"held to the candidate's own length", tracklength.New(0, 0), artTrack(200)},
	}
	for _, j := range judges {
		judge, known := j.policy.FileJudge(j.candidate)
		if !known {
			t.Fatalf("%s: FileJudge gave no judge, want one", j.name)
		}
		for _, actual := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
			if acceptable, _ := judge.Judge(actual); acceptable {
				t.Errorf("%s: Judge(%v) = acceptable, want refused: a probe that measured no finite length must never reach the stored length", j.name, actual)
			}
		}
	}
}

func TestFileJudge_AFileWithNoMeasuredLengthIsNotTheSameRecordingAsAShortTrack(t *testing.T) {
	judges := []struct {
		name      string
		policy    tracklength.Policy
		candidate ports.AudioCandidate
	}{
		{"held to the candidate's own four seconds", tracklength.New(0, 0), artTrack(4)},
		{"held to a corroborated four seconds", tracklength.New(4, 4), upload(0)},
		{"held to a saved fifteen seconds in the loose window", tracklength.New(15, 0), upload(0)},
		{"held to a resolved fifteen seconds in the loose window", tracklength.New(0, 15), upload(0)},
	}
	for _, j := range judges {
		judge, known := j.policy.FileJudge(j.candidate)
		if !known {
			t.Fatalf("%s: FileJudge gave no judge, want one", j.name)
		}
		if acceptable, _ := judge.Judge(0); acceptable {
			t.Errorf("%s: Judge(0) = acceptable, want refused: an unknown measurement is never a match", j.name)
		}
	}
}

func TestSameRecording_ComparesSecondsOnlyWhenBothAreKnownAndPlausible(t *testing.T) {
	tests := []struct {
		name             string
		expected, actual float64
		want             bool
	}{
		{"within five seconds", 100, 105, true},
		{"past five seconds", 100, 105.1, false},
		{"within three percent of a long track", 400, 412, true},
		{"unknown actual length", 4, 0, false},
		{"unknown expected length", 0, 4, false},
		{"negative actual length", 4, -1, false},
		{"not a number", 200, math.NaN(), false},
		{"longer than seven days", 7*24*60*60 - 1, 7*24*60*60 + 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tracklength.SameRecording(tt.expected, tt.actual); got != tt.want {
				t.Errorf("SameRecording(%v, %v) = %v, want %v", tt.expected, tt.actual, got, tt.want)
			}
		})
	}
}
