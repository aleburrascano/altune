package alerts

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
)

func NewNotifier(ctx context.Context, cfg *config.Config) monitoring.AlertNotifier {
	if cfg.HasAlertEmail() {
		return monitoring.NewEmailNotifier(cfg.ResendAPIKey, cfg.AlertEmailTo, outbound.NewGuardedTransport())
	}
	if cfg.AlertWebhookURL != "" {
		return monitoring.NewWebhookNotifier(cfg.AlertWebhookURL)
	}
	slog.WarnContext(ctx, "alerts are log-only: neither ALERT_WEBHOOK_URL nor RESEND_API_KEY with ALERT_EMAIL_TO is set")
	return monitoring.NopNotifier{}
}
