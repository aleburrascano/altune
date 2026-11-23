package judgeshadow_test

import (
	"altune/go-api/internal/acquisition/internal/core/judge"
	"altune/go-api/internal/acquisition/internal/core/judgeshadow"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

var finesse = judge.Target{Title: "Finesse", Artist: "Bruno Mars"}

func captureLog(t *testing.T, handler func(*bytes.Buffer) slog.Handler) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(handler(&buf)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func textLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	return captureLog(t, func(buf *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buf, nil) })
}

type panicOnShadowHandler struct {
	slog.Handler
}

func (h panicOnShadowHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == "acquisition.judge_shadow" {
		panic("shadow log blew up")
	}
	return h.Handler.Handle(ctx, r)
}

func finesseCandidate(url string, kind ports.CandidateKind, seconds float64) ports.AudioCandidate {
	return ports.AudioCandidate{URL: url, Title: "Finesse", Artists: []string{"Bruno Mars"}, Kind: kind, Duration: seconds}
}

func wantLogged(t *testing.T, buf *bytes.Buffer, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if !strings.Contains(buf.String(), field) {
			t.Errorf("log = %q, want %s", buf.String(), field)
		}
	}
}

func TestLog_APanicInsideTheShadowIsLoggedAndSwallowed(t *testing.T) {
	buf := captureLog(t, func(buf *bytes.Buffer) slog.Handler {
		return panicOnShadowHandler{slog.NewTextHandler(buf, nil)}
	})

	judgeshadow.Log(context.Background(), finesse, "t1",
		&ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=LsoLEjrDogU"}, 0, nil)

	wantLogged(t, buf, "acquisition.judge_shadow_panic", "track_id=t1", `panic="shadow log blew up"`)
}

func TestLog_TheCatalogPinIsJudgedAsThePinnedVideo(t *testing.T) {
	buf := textLog(t)
	art := finesseCandidate("art", ports.KindArtTrack, 0)
	pin := finesseCandidate("pin", ports.KindCatalogPin, 0)

	judgeshadow.Log(context.Background(), finesse, "t1", &pin, 0, []ports.AudioCandidate{art, pin})

	wantLogged(t, buf, "judge_url=pin", "verdict=agree")
}

func TestLog_AnAbstainingJudgeIsNotADisagreement(t *testing.T) {
	buf := textLog(t)
	upload := finesseCandidate("https://music.youtube.com/watch?v=up", ports.KindUpload, 0)

	judgeshadow.Log(context.Background(), finesse, "t1", &upload, 191, []ports.AudioCandidate{upload})

	wantLogged(t, buf, "verdict=judge_abstained")
}

func TestLog_OnlyTheKeptCandidateGainsTheProbedLength(t *testing.T) {
	buf := textLog(t)
	target := judge.Target{Title: "Finesse", Artist: "Bruno Mars", Length: 300}
	kept := finesseCandidate("kept", ports.KindArtTrack, 0)
	other := finesseCandidate("other", ports.KindArtTrack, 0)

	judgeshadow.Log(context.Background(), target, "t1", &kept, 191, []ports.AudioCandidate{kept, other})

	wantLogged(t, buf, "pipeline_url=kept", "judge_url=other", "verdict=disagree")
}

func TestLog_APipelineThatKeptNothingFailed(t *testing.T) {
	buf := textLog(t)
	art := finesseCandidate("art", ports.KindArtTrack, 0)

	judgeshadow.Log(context.Background(), finesse, "t1", nil, 0, []ports.AudioCandidate{art})

	wantLogged(t, buf, `pipeline_url=""`, "judge_url=art", "verdict=pipeline_failed")
}
