package providers

import (
	"altune/go-api/internal/feedback/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const upstreamMarker = "UPSTREAM-MARKER-7f3a"

type silentMetrics struct{}

func (silentMetrics) TrackerCreateFailed(string) {}
func (silentMetrics) SubmissionRejected(string)  {}
func (silentMetrics) SubmissionCreated()         {}

func submitThrough(t *testing.T, tracker *GitHubIssueTracker) (status int, detail, code string) {
	t.Helper()
	svc := service.NewSubmitReportService(tracker, silentMetrics{})
	_, err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), service.SubmitReportInput{
		Kind:    "bug",
		Message: "three downloaded tracks went grey again",
	})
	if err == nil {
		t.Fatal("Execute succeeded against a failing tracker")
	}
	rec := httptest.NewRecorder()
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/feedback/reports", http.NoBody), err)
	var resp httputil.ErrorResponse
	if decodeErr := json.NewDecoder(rec.Body).Decode(&resp); decodeErr != nil {
		t.Fatalf("decode response: %v", decodeErr)
	}
	return rec.Code, resp.Detail, resp.Code
}

func captureLogLines(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestSubmitReport_ResponseDetailHidesUpstreamGitHubBody(t *testing.T) {
	cases := []struct {
		status   int
		wantCode string
	}{
		{http.StatusUnauthorized, codeUnauthorized},
		{http.StatusForbidden, codeUnauthorized},
		{http.StatusUnprocessableEntity, codeRejected},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			logs := captureLogLines(t)
			tracker, _ := newFakeGitHub(t, tc.status, `{"message":"`+upstreamMarker+`"}`)

			status, detail, code := submitThrough(t, tracker)

			if status != http.StatusBadGateway || code != tc.wantCode {
				t.Fatalf("got status %d code %q, want %d %q", status, code, http.StatusBadGateway, tc.wantCode)
			}
			statusText := "status " + strconv.Itoa(tc.status)
			if strings.Contains(detail, upstreamMarker) || strings.Contains(detail, statusText) {
				t.Fatalf("detail %q leaks the upstream response", detail)
			}
			if !createFailedLogCarries(logs, upstreamMarker, statusText) {
				t.Fatalf("feedback.create_failed log lost the upstream error:\n%s", logs)
			}
		})
	}
}

func TestSubmitReport_ResponseDetailHidesNetworkFailure(t *testing.T) {
	logs := captureLogLines(t)
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	host, _ := url.Parse(deadURL)

	status, detail, code := submitThrough(t, newTestTracker(deadURL))

	if status != http.StatusGatewayTimeout || code != codeUnreachable {
		t.Fatalf("got status %d code %q, want %d %q", status, code, http.StatusGatewayTimeout, codeUnreachable)
	}
	for _, leak := range []string{"/repos/o/r", "dial tcp", host.Host} {
		if strings.Contains(detail, leak) {
			t.Fatalf("detail %q leaks %q", detail, leak)
		}
	}
	if !createFailedLogCarries(logs, "dial tcp", "/repos/o/r") {
		t.Fatalf("feedback.create_failed log lost the network error:\n%s", logs)
	}
}

func createFailedLogCarries(logs *bytes.Buffer, fragments ...string) bool {
	for _, line := range strings.Split(logs.String(), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec["msg"] != "feedback.create_failed" {
			continue
		}
		logged, _ := rec["error"].(string)
		return containsAll(logged, fragments)
	}
	return false
}

func containsAll(text string, fragments []string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			return false
		}
	}
	return true
}
