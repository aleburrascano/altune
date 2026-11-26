package monitoring

import notifier "altune/go-api/internal/platform/monitoring/internal/adapters/notifier"

type NopNotifier = notifier.NopNotifier

var (
	NewWebhookNotifier = notifier.NewWebhookNotifier
	NewEmailNotifier   = notifier.NewEmailNotifier
)
