package search

import trackers "altune/go-api/internal/search/internal/adapters/providers/fansheets"

type (
	NoTrackerIndexCache = trackers.NoIndexCache
	TrackersClient      = trackers.Client
)

var (
	NewTrackersClient               = trackers.NewClient
	ErrTrackersCollaboratorRequired = trackers.ErrCollaboratorRequired
)
