package handler

import (
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

func gatedAs(principalID string, caller *shared.UserId) (*httptest.ResponseRecorder, bool) {
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	req := httptest.NewRequest(http.MethodGet, "/observe/health", nil)
	if caller != nil {
		req = req.WithContext(auth.ContextWithUserID(req.Context(), *caller))
	}
	rec := httptest.NewRecorder()
	Gate(principalID)(next).ServeHTTP(rec, req)
	return rec, reached
}

func TestGate_AdmitsThePrincipal(t *testing.T) {
	principal := shared.NewUserId(uuid.New())

	rec, reached := gatedAs(principal.String(), &principal)

	if !reached || rec.Code != http.StatusOK {
		t.Errorf("principal: reached=%v status=%d, want admitted", reached, rec.Code)
	}
}

func TestGate_RefusesAnotherSubjectWithACodedError(t *testing.T) {
	stranger := shared.NewUserId(uuid.New())

	rec, reached := gatedAs(shared.NewUserId(uuid.New()).String(), &stranger)

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
	caller := shared.UserId{}

	rec, reached := gatedAs("", &caller)

	if reached || rec.Code != http.StatusForbidden {
		t.Errorf("empty principal: reached=%v status=%d, want 403", reached, rec.Code)
	}
}

func TestGate_NoSubjectIsUnauthorized(t *testing.T) {
	rec, reached := gatedAs(shared.NewUserId(uuid.New()).String(), nil)

	if reached || rec.Code != http.StatusUnauthorized {
		t.Errorf("no subject: reached=%v status=%d, want 401", reached, rec.Code)
	}
}

func TestGate_DenialLogsActorPathAndCode(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	stranger := shared.NewUserId(uuid.New())

	gatedAs(shared.NewUserId(uuid.New()).String(), &stranger)

	line := logs.String()
	for _, want := range []string{`"msg":"observe.access_denied"`, stranger.String(), `"path":"/observe/health"`, `"code":"observe.principal_required"`} {
		if !strings.Contains(line, want) {
			t.Errorf("denial log %s lacks %s", line, want)
		}
	}
}
