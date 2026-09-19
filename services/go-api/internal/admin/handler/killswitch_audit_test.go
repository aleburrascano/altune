package handler_test

import (
	"altune/go-api/internal/admin/alert"
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/shared"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// assertFlippedBy proves the record answers "who paused this loop, and when".
// A flip that names no actor is the #1998 defect: the request logger carries no
// user id, so the audit record is the only place the operator's id appears.
func assertFlippedBy(t *testing.T, record map[string]any, actor string, notBefore time.Time) {
	t.Helper()
	if record["actor"] != actor {
		t.Errorf("audit actor = %v, want %q", record["actor"], actor)
	}
	raw, isString := record["at"].(string)
	if !isString {
		t.Fatalf("audit at = %v, want an RFC3339 timestamp", record["at"])
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("audit at = %q: %v", raw, err)
	}
	if at.Before(notBefore) || at.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("audit at = %v, outside the request window", at)
	}
}

// allLoopsHandler wires every kill switch onto one handler so each subtest
// flips its own loop through the same operator-gated admin router.
func allLoopsHandler(jobName string) *handler.AdminHandler {
	sched, _ := newLiveScheduler()
	return handler.New(nil, nil).
		WithAlertMonitor(alert.NewMonitor(alert.NopNotifier{}, time.Hour)).
		WithEvalMeter(evalmeter.New(true, time.Hour, nil)).
		WithAcquisition(sched).
		WithJobs(&oneJobSwitchboard{name: jobName, enabled: true})
}

// TestKillSwitch_EveryFlipNamesItsActorAndTime guards #1998: before it only the
// jobs flip logged actor and at, so a paused alert monitor, eval meter or
// acquisition scheduler recorded that someone paused it but never who.
func TestKillSwitch_EveryFlipNamesItsActorAndTime(t *testing.T) {
	const jobName = "corpus_refresh"
	flips := []struct{ name, loop, pausePath string }{
		{"alerts", "alert_monitor", "/admin/alerts/pause"},
		{"eval", "eval_meter", "/admin/eval/pause"},
		{"acquisition", "acquisition", "/admin/acquisition/pause"},
		{"jobs", "background_job", "/admin/jobs/" + jobName + "/disable"},
	}

	for _, f := range flips {
		t.Run(f.name, func(t *testing.T) {
			operator := shared.NewUserId(uuid.New())
			srv := mountAdminHandler(allLoopsHandler(jobName), operator.String(), operator, true)
			logs := captureLogs(t)
			notBefore := time.Now().UTC().Add(-time.Second)

			if code, _ := doAdmin(t, srv, http.MethodPost, f.pausePath); code != http.StatusOK {
				t.Fatalf("POST %s = %d, want 200", f.pausePath, code)
			}

			records := killSwitchRecords(t, logs.String())
			if len(records) != 1 {
				t.Fatalf("got %d admin.kill_switch records, want 1; logs:\n%s", len(records), logs.String())
			}
			assertFlipAudited(t, records[0], f.loop, true)
			assertFlippedBy(t, records[0], operator.String(), notBefore)
		})
	}
}
