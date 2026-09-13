package app

import (
	"log/slog"

	// aliased to disambiguate the generic package name "github", not the struct.
	feedbackGithub "altune/go-api/internal/feedback/adapters/github"
	feedbackHandler "altune/go-api/internal/feedback/adapters/handler"
	feedbackMetrics "altune/go-api/internal/feedback/adapters/metrics"
	feedbackService "altune/go-api/internal/feedback/service"
)

func (a *App) wireFeedback() *feedbackHandler.FeedbackHandler {
	if !a.cfg.FeedbackEnabled {
		slog.Info("feedback: disabled via FEEDBACK_ENABLED, in-app reports disabled")
		return nil
	}
	if !a.cfg.HasIssueTracker() {
		slog.Info("feedback: issue tracker not configured, in-app reports disabled")
		return nil
	}
	tracker := feedbackGithub.NewGitHubIssueTracker(a.cfg.GitHubIssueRepo, a.cfg.GitHubIssueToken)
	metrics := feedbackMetrics.NewExpvarFeedbackMetrics()
	return feedbackHandler.NewFeedbackHandler(feedbackService.NewSubmitReportService(tracker, metrics))
}
