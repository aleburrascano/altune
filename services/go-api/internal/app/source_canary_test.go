package app

import (
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func captureSourceCanaryLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &logs
}

// fakeCanaryProbe reports the canned outcome canaryResults[source.Name] and
// records every source it was asked to probe, so a test never needs a network
// call or a real yt-dlp binary.
type fakeCanaryProbe struct {
	results map[string]error
	probed  []string
}

func (f *fakeCanaryProbe) Canary(_ context.Context, source ytdlp.CanarySource) error {
	f.probed = append(f.probed, source.Name)
	return f.results[source.Name]
}

func TestRunSourceCanaries_NilWhenEverySourceExtracts(t *testing.T) {
	probe := &fakeCanaryProbe{results: map[string]error{}}

	err := runSourceCanaries(context.Background(), probe, sourceCanaries(sourceToggles{ytMusic: true, ytDlp: true}))
	if err != nil {
		t.Fatalf("runSourceCanaries() = %v, want nil when every source is healthy", err)
	}
	if len(probe.probed) != 3 {
		t.Fatalf("probed = %v, want youtube, ytmusic and soundcloud", probe.probed)
	}
}

func TestRunSourceCanaries_NamesEveryDarkSource(t *testing.T) {
	probe := &fakeCanaryProbe{results: map[string]error{
		"youtube":    errors.New("Sign in to confirm you're not a bot"),
		"soundcloud": errors.New("preview only"),
	}}

	err := runSourceCanaries(context.Background(), probe, sourceCanaries(sourceToggles{ytMusic: true, ytDlp: true}))

	if err == nil {
		t.Fatal("runSourceCanaries() = nil, want an error naming the dark sources")
	}
	msg := err.Error()
	for _, want := range []string{
		`youtube ("Sign in to confirm you're not a bot")`,
		`soundcloud ("preview only")`,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want it to contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "ytmusic (") {
		t.Errorf("error = %q, want the healthy ytmusic source left out", msg)
	}
}

func TestSourceCanaries_OnlyProbesEnabledSources(t *testing.T) {
	tests := []struct {
		enabled sourceToggles
		want    []string
	}{
		{sourceToggles{ytMusic: true, ytDlp: true}, []string{"youtube", "ytmusic", "soundcloud"}},
		{sourceToggles{ytMusic: false, ytDlp: true}, []string{"youtube", "soundcloud"}},
		{sourceToggles{ytMusic: true, ytDlp: false}, []string{"ytmusic"}},
		{sourceToggles{}, nil},
	}
	for _, tt := range tests {
		var got []string
		for _, c := range sourceCanaries(tt.enabled) {
			got = append(got, c.Name)
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("sourceCanaries(%+v) = %v, want %v", tt.enabled, got, tt.want)
		}
	}
}

func TestStartSourceCanary_DoesNotRegisterTheJobWhenEverySourceIsDisabled(t *testing.T) {
	a := &App{}
	a.startSourceCanary(context.Background(), ytdlp.NewYtDlpAudioSearcher("", "", ""), true, sourceToggles{})

	if _, ok := a.SetJobEnabled(jobAcquisitionSourceCanary, false); ok {
		t.Fatal("job was registered despite every canaried source being disabled")
	}
}

func TestStartSourceCanary_DoesNotRegisterTheJobWhenYtDlpIsUnavailable(t *testing.T) {
	a := &App{}
	a.startSourceCanary(context.Background(), ytdlp.NewYtDlpAudioSearcher("", "", ""), false, sourceToggles{ytMusic: true, ytDlp: true})

	if _, ok := a.SetJobEnabled(jobAcquisitionSourceCanary, false); ok {
		t.Fatal("job was registered despite yt-dlp being reported unavailable")
	}
}

func TestStartSourceCanary_RegistersTheJobWhenYtDlpIsAvailable(t *testing.T) {
	a := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.startSourceCanary(ctx, ytdlp.NewYtDlpAudioSearcher("", "", ""), true, sourceToggles{ytMusic: true, ytDlp: true})

	if _, ok := a.SetJobEnabled(jobAcquisitionSourceCanary, true); !ok {
		t.Fatal("job was not registered despite yt-dlp being reported available")
	}
}

func TestLogSourceCanaryResult_RecordsOkTrueOnSuccess(t *testing.T) {
	logs := captureSourceCanaryLogs(t)

	logSourceCanaryResult(context.Background(), "youtube", 0, nil)

	if !strings.Contains(logs.String(), `"ok":true`) {
		t.Fatalf("expected ok=true in the log line, got:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), `"ok":false`) {
		t.Fatalf("unexpected ok=false in a successful probe's log line, got:\n%s", logs.String())
	}
}

func TestLogSourceCanaryResult_RecordsOkFalseAndTheErrorOnFailure(t *testing.T) {
	logs := captureSourceCanaryLogs(t)

	logSourceCanaryResult(context.Background(), "youtube", 0, errors.New("boom"))

	logged := logs.String()
	if !strings.Contains(logged, `"ok":false`) {
		t.Fatalf("expected ok=false in the log line, got:\n%s", logged)
	}
	if !strings.Contains(logged, "boom") {
		t.Fatalf("expected the failure reason in the log line, got:\n%s", logged)
	}
}
