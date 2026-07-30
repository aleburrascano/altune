package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func testReport(t *testing.T, kind domain.Kind, message string, diag domain.Diagnostics) *domain.Report {
	t.Helper()
	report, err := domain.NewReport(shared.NewUserId(uuid.New()), kind, message, diag)
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	return report
}

func TestCreate_PostsTitleBodyAndLabels(t *testing.T) {
	var got createIssueRequest
	var path, auth, version string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		version = r.Header.Get("X-GitHub-Api-Version")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/o/r/issues/7"}`))
	}))
	defer server.Close()

	tracker := NewIssueTracker("o/r", "tok").WithBaseURL(server.URL)
	report := testReport(t, domain.KindIdea, "let me sort albums by year", domain.Diagnostics{
		AppVersion: "1.4.0", Platform: "ios", OSVersion: "18.2", Screen: "settings",
	})

	ref, err := tracker.Create(context.Background(), report)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ref.Number != 7 || ref.URL != "https://github.com/o/r/issues/7" {
		t.Fatalf("ref = %+v, want issue 7", ref)
	}
	if path != "/repos/o/r/issues" {
		t.Fatalf("path = %q", path)
	}
	if auth != "Bearer tok" || version != apiVersion {
		t.Fatalf("auth = %q, version = %q", auth, version)
	}
	if got.Title != "[idea] let me sort albums by year" {
		t.Fatalf("title = %q", got.Title)
	}
	if strings.Join(got.Labels, ",") != "enhancement,from-app" {
		t.Fatalf("labels = %v", got.Labels)
	}
	if !strings.Contains(got.Body, "let me sort albums by year") {
		t.Fatalf("body missing the message: %q", got.Body)
	}
	if !strings.Contains(got.Body, "| Platform | ios 18.2 |") {
		t.Fatalf("body missing the diagnostics row: %q", got.Body)
	}
}

func TestCreate_AttributesTheReportToItsReporter(t *testing.T) {
	var got createIssueRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":1,"html_url":"u"}`))
	}))
	defer server.Close()

	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	tracker := NewIssueTracker("o/r", "tok").WithBaseURL(server.URL)
	if _, err := tracker.Create(context.Background(), report); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !strings.Contains(got.Body, report.Reporter.String()) {
		t.Fatalf("issue body did not name the reporter: %q", got.Body)
	}
	if strings.Contains(got.Title, report.Reporter.String()) {
		t.Fatalf("the reporter belongs in the body, not the title: %q", got.Title)
	}
}

func TestCreate_EscapesPipesInDiagnostics(t *testing.T) {
	var got createIssueRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":1,"html_url":"u"}`))
	}))
	defer server.Close()

	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{
		Screen: "settings | fake | row",
	})
	tracker := NewIssueTracker("o/r", "tok").WithBaseURL(server.URL)
	if _, err := tracker.Create(context.Background(), report); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.Contains(got.Body, `settings \| fake \| row`) {
		t.Fatalf("body did not escape the pipes: %q", got.Body)
	}
}

func TestCreate_FailsOnNonCreatedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	tracker := NewIssueTracker("o/r", "tok").WithBaseURL(server.URL)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	_, err := tracker.Create(context.Background(), report)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want a 401 failure", err)
	}
}

func TestCreate_FailsWhenResponseCarriesNoIssueNumber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	tracker := NewIssueTracker("o/r", "tok").WithBaseURL(server.URL)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("expected a missing issue number to fail")
	}
}
