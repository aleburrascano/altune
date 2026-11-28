package service_test

import (
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func captureLogs(t *testing.T) *logging.Capture {
	t.Helper()
	return logging.CaptureJSON(t, slog.LevelDebug)
}

func fakeChecks(results map[string]error, names ...string) ([]service.Check, *[]string) {
	probed := &[]string{}
	var checks []service.Check
	for _, name := range names {
		checks = append(checks, service.Check{Name: name, Run: func(context.Context) error {
			*probed = append(*probed, name)
			return results[name]
		}})
	}
	return checks, probed
}

func TestRun_NilWhenEverySourceExtracts(t *testing.T) {
	checks, probed := fakeChecks(nil, "youtube", "ytmusic", "soundcloud", "ytmsearch")

	if err := service.RunChecks(context.Background(), clock.Now, checks); err != nil {
		t.Fatalf("RunChecks() = %v, want nil when every source is healthy", err)
	}

	if strings.Join(*probed, ",") != "youtube,ytmusic,soundcloud,ytmsearch" {
		t.Fatalf("probed = %v, want youtube, ytmusic, soundcloud and ytmsearch in order", *probed)
	}
}

func TestRun_NamesEveryDarkSource(t *testing.T) {
	checks, probed := fakeChecks(map[string]error{
		"youtube":    errors.New("Sign in to confirm you're not a bot"),
		"soundcloud": errors.New("preview only"),
		"ytmsearch":  errors.New("search: HTTP 400"),
	}, "youtube", "ytmusic", "soundcloud", "ytmsearch")

	err := service.RunChecks(context.Background(), clock.Now, checks)

	want := `sources dark: youtube ("Sign in to confirm you're not a bot"), soundcloud ("preview only"), ytmsearch ("search: HTTP 400")`
	if err == nil || err.Error() != want {
		t.Fatalf("RunChecks() = %v, want %q", err, want)
	}
	if len(*probed) != 4 {
		t.Fatalf("probed = %v, want a dark source not to stop the pass", *probed)
	}
}

func TestRun_LogsEachProbeWithItsOutcome(t *testing.T) {
	logs := captureLogs(t)
	checks, _ := fakeChecks(map[string]error{"youtube": errors.New("boom")}, "youtube", "ytmusic")

	_ = service.RunChecks(context.Background(), clock.Now, checks)

	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("log line %q: %v", raw, err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want one per probe:\n%s", len(lines), logs.String())
	}
	failed, passed := lines[0], lines[1]
	if failed["msg"] != "acquisition.source_canary" || failed["source"] != "youtube" || failed["ok"] != false || failed["error"] != "boom" || failed["level"] != "INFO" {
		t.Errorf("failed probe logged %v, want source youtube, ok false and the error at info", failed)
	}
	if passed["msg"] != "acquisition.source_canary" || passed["source"] != "ytmusic" || passed["ok"] != true || passed["level"] != "INFO" {
		t.Errorf("passing probe logged %v, want source ytmusic and ok true at info", passed)
	}
	if _, hasErr := passed["error"]; hasErr {
		t.Errorf("passing probe logged an error: %v", passed)
	}
	for _, line := range lines {
		if _, ok := line["duration_ms"].(float64); !ok {
			t.Errorf("probe logged no duration: %v", line)
		}
	}
}

func TestRun_RefusesAMissingClock(t *testing.T) {
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "RunChecks") {
			t.Errorf("panic = %q, want a refusal naming RunChecks", msg)
		}
	}()

	_ = service.RunChecks(context.Background(), nil, nil)
}
