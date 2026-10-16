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

func fakeCanaries(results map[string]error, names ...string) ([]namedCanary, *[]string) {
	probed := &[]string{}
	var canaries []namedCanary
	for _, name := range names {
		canaries = append(canaries, namedCanary{name: name, run: func(context.Context) error {
			*probed = append(*probed, name)
			return results[name]
		}})
	}
	return canaries, probed
}

func canaryNames(canaries []namedCanary) []string {
	var got []string
	for _, c := range canaries {
		got = append(got, c.name)
	}
	return got
}

func TestRunSourceCanaries_NilWhenEverySourceExtracts(t *testing.T) {
	canaries, probed := fakeCanaries(nil, "youtube", "ytmusic", "soundcloud", "ytmsearch")

	err := runSourceCanaries(context.Background(), canaries)
	if err != nil {
		t.Fatalf("runSourceCanaries() = %v, want nil when every source is healthy", err)
	}
	if len(*probed) != 4 {
		t.Fatalf("probed = %v, want youtube, ytmusic, soundcloud and ytmsearch", *probed)
	}
}

func TestRunSourceCanaries_NamesEveryDarkSource(t *testing.T) {
	canaries, _ := fakeCanaries(map[string]error{
		"youtube":    errors.New("Sign in to confirm you're not a bot"),
		"soundcloud": errors.New("preview only"),
		"ytmsearch":  errors.New("search: HTTP 400"),
	}, "youtube", "ytmusic", "soundcloud", "ytmsearch")

	err := runSourceCanaries(context.Background(), canaries)

	if err == nil {
		t.Fatal("runSourceCanaries() = nil, want an error naming the dark sources")
	}
	msg := err.Error()
	for _, want := range []string{
		`youtube ("Sign in to confirm you're not a bot")`,
		`soundcloud ("preview only")`,
		`ytmsearch ("search: HTTP 400")`,
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
	searcher := ytdlp.NewYtDlpAudioSearcher("", "", "")
	ytm := newYTMSearchClient()
	tests := []struct {
		enabled sourceToggles
		want    []string
	}{
		{sourceToggles{ytMusic: true, ytDlp: true}, []string{"youtube", "ytmusic", "soundcloud", "ytmsearch"}},
		{sourceToggles{ytMusic: false, ytDlp: true}, []string{"youtube", "soundcloud"}},
		{sourceToggles{ytMusic: true, ytDlp: false}, []string{"ytmusic", "ytmsearch"}},
		{sourceToggles{}, nil},
	}
	for _, tt := range tests {
		got := canaryNames(sourceCanaries(searcher, true, ytm, tt.enabled))
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("sourceCanaries(%+v) = %v, want %v", tt.enabled, got, tt.want)
		}
	}
}

func TestSourceCanaries_ProbesInnerTubeWithoutYtDlp(t *testing.T) {
	ytm := newYTMSearchClient()
	enabled := sourceToggles{ytMusic: true, ytDlp: true}

	for _, searcher := range []*ytdlp.YtDlpAudioSearcher{nil, ytdlp.NewYtDlpAudioSearcher("", "", "")} {
		got := canaryNames(sourceCanaries(searcher, false, ytm, enabled))
		if strings.Join(got, ",") != "ytmsearch" {
			t.Errorf("sourceCanaries(searcher=%v, unavailable) = %v, want only ytmsearch", searcher != nil, got)
		}
	}
	if got := sourceCanaries(nil, false, nil, enabled); len(got) != 0 {
		t.Errorf("sourceCanaries without any client = %v, want none", canaryNames(got))
	}
	if got := sourceCanaries(nil, false, ytm, sourceToggles{ytDlp: true}); len(got) != 0 {
		t.Errorf("sourceCanaries with ytmusic disabled = %v, want none", canaryNames(got))
	}
}

func TestStartSourceCanary_DoesNotRegisterTheJobWhenEverySourceIsDisabled(t *testing.T) {
	a := &App{}
	a.startSourceCanary(context.Background(), ytdlp.NewYtDlpAudioSearcher("", "", ""), true, newYTMSearchClient(), sourceToggles{})

	if _, ok := a.SetJobEnabled(jobAcquisitionSourceCanary, false); ok {
		t.Fatal("job was registered despite every canaried source being disabled")
	}
}

func TestStartSourceCanary_DoesNotRegisterTheJobWhenNoProbeIsAvailable(t *testing.T) {
	a := &App{}
	a.startSourceCanary(context.Background(), ytdlp.NewYtDlpAudioSearcher("", "", ""), false, nil, sourceToggles{ytMusic: true, ytDlp: true})

	if _, ok := a.SetJobEnabled(jobAcquisitionSourceCanary, false); ok {
		t.Fatal("job was registered despite yt-dlp being unavailable and no InnerTube client")
	}
}

func TestStartSourceCanary_RegistersTheJobWhenYtDlpIsAvailable(t *testing.T) {
	a := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.startSourceCanary(ctx, ytdlp.NewYtDlpAudioSearcher("", "", ""), true, nil, sourceToggles{ytMusic: true, ytDlp: true})

	if _, ok := a.SetJobEnabled(jobAcquisitionSourceCanary, true); !ok {
		t.Fatal("job was not registered despite yt-dlp being reported available")
	}
}

func TestStartSourceCanary_RegistersTheJobForInnerTubeWhenYtDlpIsUnavailable(t *testing.T) {
	a := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.startSourceCanary(ctx, nil, false, newYTMSearchClient(), sourceToggles{ytMusic: true, ytDlp: true})

	if _, ok := a.SetJobEnabled(jobAcquisitionSourceCanary, true); !ok {
		t.Fatal("job was not registered despite the InnerTube probe being available")
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
