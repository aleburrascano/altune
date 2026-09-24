package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestOperatorGate_DenialEmitsOneAccessDeniedRecord(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	operator := shared.NewUserId(uuid.New())
	other := shared.NewUserId(uuid.New())
	gate := handler.OperatorOnly(operator.String())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/admin/killswitch", nil)
	req = req.WithContext(auth.ContextWithUserID(req.Context(), other))
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var denied []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == "admin.access_denied" {
			denied = append(denied, m)
		}
	}
	if len(denied) != 1 {
		t.Fatalf("admin.access_denied records = %d, want 1; logs:\n%s", len(denied), buf.String())
	}
	got := denied[0]
	if got["level"] != "WARN" || got["actor"] != other.String() || got["method"] != "POST" ||
		got["path"] != "/admin/killswitch" || got["code"] != "admin.operator_required" {
		t.Errorf("record = %v", got)
	}
}
