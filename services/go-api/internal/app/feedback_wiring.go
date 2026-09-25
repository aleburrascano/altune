package app

import (
	"log/slog"

	feedbackHandler "altune/go-api/internal/feedback/adapters/handler"
	feedbackMetrics "altune/go-api/internal/feedback/adapters/metrics"
	feedbackProviders "altune/go-api/internal/feedback/adapters/providers"
	feedbackService "altune/go-api/internal/feedback/service"

	"github.com/go-chi/chi/v5"
)

func (a *App) wireFeedback() *feedbackHandler.FeedbackHandler {
	if !a.cfg.FeedbackEnabled {
		slog.Info("feedback: disabled via FEEDBACK_ENABLED, in-app reports disabled")
		return nil
	}
	if !a.cfg.HasIssueTracker() {
		slog.Warn("feedback: GITHUB_ISSUE_REPO and GITHUB_ISSUE_TOKEN not set, in-app reports disabled")
		return nil
	}
	tracker := feedbackProviders.NewGitHubIssueTracker(a.cfg.GitHubIssueRepo, a.cfg.GitHubIssueToken)
	metrics := feedbackMetrics.NewExpvarFeedbackMetrics()
	return feedbackHandler.NewFeedbackHandler(feedbackService.NewSubmitReportService(tracker, metrics))
}

// mountFeedback mounts the submit handler when wireFeedback built one, and the
// coded-503 disabled routes otherwise, so a switched-off feature reads as
// "disabled" to clients rather than as an unknown path.
func mountFeedback(r chi.Router, h *feedbackHandler.FeedbackHandler) {
	if h == nil {
		r.Mount("/feedback", feedbackHandler.DisabledRoutes())
		return
	}
	r.Mount("/feedback", h.Routes())
}
