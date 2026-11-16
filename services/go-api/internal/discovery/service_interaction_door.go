package discovery

import interaction "altune/go-api/internal/discovery/service/interaction"

type RecordEventService = interaction.RecordEventService

var (
	NewRecordEventService       = interaction.NewRecordEventService
	WithRecordEventActivityFeed = interaction.WithRecordEventActivityFeed
)
