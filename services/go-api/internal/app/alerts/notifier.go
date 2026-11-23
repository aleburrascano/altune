package alerts

import (
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"net/http"
)

func NewNotifier(ctx context.Context, cfg *config.Config) observe.AlertNotifier {
	if cfg.HasAlertEmail() {
		return observe.NewEmailNotifier(cfg.ResendAPIKey, cfg.AlertEmailTo, http.DefaultTransport)
	}
	if cfg.AlertWebhookURL != "" {
		return observe.NewWebhookNotifier(cfg.AlertWebhookURL)
	}
	slog.WarnContext(ctx, "alerts are log-only: neither ALERT_WEBHOOK_URL nor RESEND_API_KEY with ALERT_EMAIL_TO is set")
	return observe.NopNotifier{}
}
