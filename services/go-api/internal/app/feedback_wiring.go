package app

import (
	"log/slog"

	feedbackGithub "altune/go-api/internal/feedback/adapters/github"
	feedbackHandler "altune/go-api/internal/feedback/adapters/handler"
	feedbackService "altune/go-api/internal/feedback/service"
)

func (a *App) wireFeedback() *feedbackHandler.FeedbackHandler {
	if !a.cfg.HasIssueTracker() {
		slog.Info("feedback: issue tracker not configured, in-app reports disabled")
		return nil
	}
	tracker := feedbackGithub.NewIssueTracker(a.cfg.GitHubIssueRepo, a.cfg.GitHubIssueToken)
	return feedbackHandler.NewFeedbackHandler(feedbackService.NewSubmitReportService(tracker))
}
