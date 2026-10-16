package judgeshadow

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/judge"
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestLog_APanickingJudgeIsLoggedAndSwallowed(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	realChoose := choose
	choose = func(judge.Target, []judge.Candidate) (judge.Candidate, judge.Decision, bool) {
		panic("judge blew up")
	}
	t.Cleanup(func() { choose = realChoose })

	Log(context.Background(), judge.Target{Title: "Finesse", Artist: "Bruno Mars"}, "t1",
		&ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=LsoLEjrDogU"}, 0, nil)

	if !strings.Contains(buf.String(), "acquisition.judge_shadow_panic") {
		t.Fatalf("log = %q, want the panic recorded", buf.String())
	}
}

func TestKindOf_TheCatalogPinIsThePinnedKind(t *testing.T) {
	if got := kindOf(ports.KindCatalogPin); got != judge.PinnedVideo {
		t.Errorf("kindOf(catalog pin) = %q, want %q", got, judge.PinnedVideo)
	}
}

func TestLog_AnAbstainingJudgeIsNotADisagreement(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	upload := ports.AudioCandidate{
		URL: "https://music.youtube.com/watch?v=up", Title: "Finesse", Artists: []string{"Bruno Mars"}, Kind: ports.KindUpload,
	}

	Log(context.Background(), judge.Target{Title: "Finesse", Artist: "Bruno Mars"}, "t1", &upload, 191, []ports.AudioCandidate{upload})

	if !strings.Contains(buf.String(), "verdict=judge_abstained") {
		t.Fatalf("log = %q, want verdict judge_abstained", buf.String())
	}
}

func TestObserved_OnlyTheKeptCandidateGainsTheProbedLength(t *testing.T) {
	kept := ports.AudioCandidate{URL: "a"}
	other := ports.AudioCandidate{URL: "b"}
	got := observed([]ports.AudioCandidate{other, kept}, "a", 191)
	if got[0].Length != 0 || got[1].Length != 191 {
		t.Errorf("lengths = %v, %v, want 0 and 191", got[0].Length, got[1].Length)
	}
}
