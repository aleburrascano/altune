package handler_test

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func gatedAs(principalID string, caller *ids.UserId) (*httptest.ResponseRecorder, bool) {
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	req := httptest.NewRequest(http.MethodGet, "/observe/health", nil)
	if caller != nil {
		req = req.WithContext(requestuser.ContextWithUserID(req.Context(), *caller))
	}
	rec := httptest.NewRecorder()
	monitoring.Gate(principalID)(next).ServeHTTP(rec, req)
	return rec, reached
}

func TestGate_AdmitsThePrincipal(t *testing.T) {
	principal := ids.NewUserId(uuid.New())

	rec, reached := gatedAs(principal.String(), &principal)

	if !reached || rec.Code != http.StatusOK {
		t.Errorf("principal: reached=%v status=%d, want admitted", reached, rec.Code)
	}
}

func TestGate_RefusesAnotherSubjectWithACodedError(t *testing.T) {
	stranger := ids.NewUserId(uuid.New())

	rec, reached := gatedAs(ids.NewUserId(uuid.New()).String(), &stranger)

	if reached || rec.Code != http.StatusForbidden {
		t.Fatalf("stranger: reached=%v status=%d, want 403 before the route", reached, rec.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode denial: %v", err)
	}
	if body.Code != "observe.principal_required" {
		t.Errorf("code = %q, want observe.principal_required", body.Code)
	}
}

func TestGate_EmptyPrincipalAdmitsNobody(t *testing.T) {
	caller := ids.UserId{}

	rec, reached := gatedAs("", &caller)

	if reached || rec.Code != http.StatusForbidden {
		t.Errorf("empty principal: reached=%v status=%d, want 403", reached, rec.Code)
	}
}

func TestGate_DenialLogsActorPathAndCode(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	stranger := ids.NewUserId(uuid.New())

	gatedAs(ids.NewUserId(uuid.New()).String(), &stranger)

	line := logs.String()
	for _, want := range []string{`"msg":"observe.access_denied"`, stranger.String(), `"path":"/observe/health"`, `"code":"observe.principal_required"`} {
		if !strings.Contains(line, want) {
			t.Errorf("denial log %s lacks %s", line, want)
		}
	}
}
