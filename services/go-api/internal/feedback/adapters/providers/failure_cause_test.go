package providers

import (
	"altune/go-api/internal/feedback/service"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// causeRecordingMetrics records the cause of every counted tracker failure.
type causeRecordingMetrics struct{ causes []string }

func (m *causeRecordingMetrics) SubmissionRejected(string) {}
func (m *causeRecordingMetrics) SubmissionCreated()        {}

func (m *causeRecordingMetrics) TrackerCreateFailed(cause string) {
	m.causes = append(m.causes, cause)
}

// TestSubmitReport_SplitsTrackerFailureMetricByCause reproduces #1111 end to end
// against a fake GitHub: a transient failure (5xx, unreachable host) and a
// permanent one (dead token, wrong repo) used to bump one undifferentiated
// counter, and a 404 wrong-repo classified identically to a 5xx outage. Each now
// lands under its own cause, and the permanent causes never collide with the
// transient ones.
func TestSubmitReport_SplitsTrackerFailureMetricByCause(t *testing.T) {
	unreachable := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachableURL := unreachable.URL
	unreachable.Close()

	cases := []struct {
		name      string
		tracker   func(t *testing.T) *GitHubIssueTracker
		wantCause string
		permanent bool
	}{
		{"github 5xx", func(t *testing.T) *GitHubIssueTracker {
			tr, _ := newFakeGitHub(t, http.StatusServiceUnavailable, `{"message":"boom"}`)
			return tr
		}, codeUnavailable, false},
		{"network error", func(*testing.T) *GitHubIssueTracker { return newTestTracker(unreachableURL) }, codeUnreachable, false},
		{"dead token 401", func(t *testing.T) *GitHubIssueTracker {
			tr, _ := newFakeGitHub(t, http.StatusUnauthorized, `{"message":"Bad credentials"}`)
			return tr
		}, codeUnauthorized, true},
		{"wrong repo 404", func(t *testing.T) *GitHubIssueTracker {
			tr, _ := newFakeGitHub(t, http.StatusNotFound, `{"message":"Not Found"}`)
			return tr
		}, codeNotFound, true},
	}
	transient := map[string]bool{}
	permanent := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metrics := &causeRecordingMetrics{}
			svc := service.NewSubmitReportService(tc.tracker(t), metrics)
			_, err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), service.SubmitReportInput{
				Kind:    "bug",
				Message: "three downloaded tracks went grey again",
			})
			if err == nil {
				t.Fatal("expected the tracker failure to surface")
			}
			if len(metrics.causes) != 1 || metrics.causes[0] != tc.wantCause {
				t.Fatalf("counted causes = %v, want [%s]", metrics.causes, tc.wantCause)
			}
			if tc.permanent {
				permanent[tc.wantCause] = true
			} else {
				transient[tc.wantCause] = true
			}
		})
	}
	for cause := range permanent {
		if transient[cause] {
			t.Fatalf("cause %q is shared by a permanent and a transient failure", cause)
		}
	}
}
