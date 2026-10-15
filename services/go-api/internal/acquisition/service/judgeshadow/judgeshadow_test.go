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
		ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=LsoLEjrDogU"}, nil)

	if !strings.Contains(buf.String(), "acquisition.judge_shadow_panic") {
		t.Fatalf("log = %q, want the panic recorded", buf.String())
	}
}

func TestKindOf_TheCatalogPinIsThePinnedKind(t *testing.T) {
	if got := kindOf(ports.KindCatalogPin); got != judge.PinnedVideo {
		t.Errorf("kindOf(catalog pin) = %q, want %q", got, judge.PinnedVideo)
	}
}
