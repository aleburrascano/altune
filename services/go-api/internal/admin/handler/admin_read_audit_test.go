package handler_test

import (
	"altune/go-api/internal/shared"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func adminReadRecords(buf *bytes.Buffer) []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == "admin.read" {
			out = append(out, m)
		}
	}
	return out
}

func TestAdminRead_DataRoutesNameTheActor(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	readOnly := shared.NewUserId(uuid.New())
	ids := adminIDs{operator: operator.String(), readOnly: readOnly.String()}

	paths := []string{"/admin/requests", "/admin/requests/x", "/admin/logs", "/admin/logs/stream", "/admin/events/stream"}
	for _, caller := range []shared.UserId{operator, readOnly} {
		for _, path := range paths {
			buf := captureLogs(t)
			serveAdminAs(t, ids, caller, adminRoute{http.MethodGet, path + "?q=secret-query"})

			got := adminReadRecords(buf)
			if len(got) != 1 {
				t.Fatalf("%s as %s: admin.read records = %d, want 1", path, caller, len(got))
			}
			if got[0]["actor"] != caller.String() || got[0]["method"] != "GET" || got[0]["path"] != path {
				t.Errorf("%s: record = %v", path, got[0])
			}
			if strings.Contains(buf.String(), "secret-query") {
				t.Errorf("%s: log carries the raw query text", path)
			}
		}
	}
}

func TestAdminRead_PollingRoutesEmitNothing(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	ids := adminIDs{operator: operator.String()}
	for _, path := range []string{"/admin/health", "/admin/metrics", "/admin/metrics/live", "/admin/events/rates"} {
		buf := captureLogs(t)
		serveAdminAs(t, ids, operator, adminRoute{http.MethodGet, path})
		if got := adminReadRecords(buf); len(got) != 0 {
			t.Errorf("%s emitted %d admin.read records, want 0", path, len(got))
		}
	}
}
