package observe

import notifier "altune/go-api/internal/observe/internal/adapters/notifier"

type NopNotifier = notifier.NopNotifier

var (
	NewWebhookNotifier = notifier.NewWebhookNotifier
	NewEmailNotifier   = notifier.NewEmailNotifier
)
