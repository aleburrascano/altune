package notifier

import (
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/platform/outbound"
	"context"
	"log/slog"
)

type Settings struct {
	ResendAPIKey string
	EmailTo      string
	WebhookURL   string
}

func Select(ctx context.Context, s Settings) ports.AlertNotifier {
	if s.ResendAPIKey != "" && s.EmailTo != "" {
		return NewEmailNotifier(s.ResendAPIKey, s.EmailTo, outbound.NewGuardedTransport())
	}
	if s.WebhookURL != "" {
		return NewWebhookNotifier(s.WebhookURL)
	}
	slog.WarnContext(ctx, "alerts are log-only: neither ALERT_WEBHOOK_URL nor RESEND_API_KEY with ALERT_EMAIL_TO is set")
	return NopNotifier{}
}
