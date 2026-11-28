package handler

import "time"

type discographyQualityResponse struct {
	WindowDays   int                  `json:"window_days"`
	GroupBy      string               `json:"group_by"`
	Cases        []discographyCaseDTO `json:"cases"`
	SuspectRate  float64              `json:"suspect_rate"`
	LastSampleAt time.Time            `json:"last_sample_at"`
}

type tapEventDTO struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	CorrID    string    `json:"corr_id,omitempty"`
}

type evalQueryDTO struct {
	Query    string `json:"query"`
	Expect   string `json:"expect"`
	Passed   bool   `json:"passed"`
	Position int    `json:"position"`
}

type evalStatusDTO struct {
	Enabled  bool           `json:"enabled"`
	Paused   bool           `json:"paused"`
	State    string         `json:"state"`
	Score    *float64       `json:"score,omitempty"`
	Baseline *float64       `json:"baseline,omitempty"`
	Errored  int            `json:"errored,omitempty"`
	LastRun  *time.Time     `json:"last_run,omitempty"`
	Error    string         `json:"error,omitempty"`
	Queries  []evalQueryDTO `json:"queries,omitempty"`
}

type acquisitionToolsDTO struct {
	Ffprobe bool `json:"ffprobe"`
	Ffmpeg  bool `json:"ffmpeg"`
	Fpcalc  bool `json:"fpcalc"`
	YtDlp   bool `json:"yt_dlp"`
}

type acquisitionJobDTO struct {
	TrackID        string    `json:"track_id"`
	Title          string    `json:"title,omitempty"`
	Artist         string    `json:"artist,omitempty"`
	Album          string    `json:"album,omitempty"`
	SourceURL      string    `json:"source_url,omitempty"`
	ResolvedSource string    `json:"source,omitempty"`
	State          string    `json:"state"`
	Stage          string    `json:"stage,omitempty"`
	ScheduledAt    time.Time `json:"scheduled_at"`
	ElapsedMs      int64     `json:"elapsed_ms"`
	Reason         string    `json:"reason,omitempty"`
	Provenance     string    `json:"provenance,omitempty"`
}

type acquisitionStatusDTO struct {
	InFlight           int                 `json:"in_flight"`
	Succeeded          uint64              `json:"succeeded"`
	Failed             uint64              `json:"failed"`
	Rejected           uint64              `json:"rejected"`
	Paused             bool                `json:"paused"`
	QueueDepth         int                 `json:"queue_depth"`
	QueueCapacity      int                 `json:"queue_capacity"`
	OldestPendingAgeMs int64               `json:"oldest_pending_age_ms"`
	QueueDepthKnown    bool                `json:"queue_depth_known"`
	Verification       acquisitionToolsDTO `json:"verification"`
	ActiveJobs         []acquisitionJobDTO `json:"jobs"`
	Recent             []acquisitionJobDTO `json:"recent"`
}

type dependencyHealthDTO struct {
	DB     DepStatus           `json:"db"`
	Redis  DepStatus           `json:"redis"`
	Auth   DepStatus           `json:"auth"`
	Detail dependencyDetailDTO `json:"detail"`
}

type dependencyDetailDTO struct {
	DBLatencyMs    int64     `json:"db_latency_ms"`
	DBError        string    `json:"db_error,omitempty"`
	RedisLatencyMs int64     `json:"redis_latency_ms"`
	RedisError     string    `json:"redis_error,omitempty"`
	AuthLatencyMs  int64     `json:"auth_latency_ms"`
	AuthError      string    `json:"auth_error,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

func dependencyHealthWire(d DependencyHealth) dependencyHealthDTO {
	return dependencyHealthDTO{
		DB: d.DB, Redis: d.Redis, Auth: d.Auth,
		Detail: dependencyDetailDTO{
			DBLatencyMs: d.Detail.DBLatencyMs, DBError: d.Detail.DBError,
			RedisLatencyMs: d.Detail.RedisLatencyMs, RedisError: d.Detail.RedisError,
			AuthLatencyMs: d.Detail.AuthLatencyMs, AuthError: d.Detail.AuthError,
			CheckedAt: d.Detail.CheckedAt,
		},
	}
}

type eventBusStats struct {
	Dropped uint64 `json:"dropped_total"`
}

type eventTapStats struct {
	TapDropped  uint64 `json:"tap_dropped_total"`
	FeedDropped uint64 `json:"feed_dropped_total"`
}

type leaderStats struct {
	Attempts  int64 `json:"acquire_attempts_total"`
	Wins      int64 `json:"acquire_wins_total"`
	Contended int64 `json:"acquire_contended_total"`
	Failures  int64 `json:"acquire_failures_total"`
	TermEnds  int64 `json:"term_ends_total"`
	HoldsLost int64 `json:"holds_lost_total"`
}

type alertMonitorStats struct {
	LastPass            time.Time `json:"last_pass"`
	LastNotifyOK        bool      `json:"last_notify_ok"`
	ConsecutiveFailures int64     `json:"consecutive_failures"`
	ContainedPanics     uint64    `json:"contained_panics"`
	NopNotifier         bool      `json:"nop_notifier"`
	LastNotifyOKAt      time.Time `json:"last_notify_ok_at"`
	LastNotifyErrorAt   time.Time `json:"last_notify_error_at"`
}

type bucketCountStats struct {
	LeMs  string `json:"le_ms"`
	Count uint64 `json:"count"`
}

type statusClassStats struct {
	Count2xx uint64 `json:"2xx"`
	Count4xx uint64 `json:"4xx"`
	Count5xx uint64 `json:"5xx"`
}

type routeLatencyStats struct {
	Count   uint64             `json:"count"`
	SumMs   uint64             `json:"sum_ms"`
	Buckets []bucketCountStats `json:"buckets"`
	Status  statusClassStats   `json:"status"`
}

type latencyStats struct {
	Routes map[string]routeLatencyStats `json:"routes"`
}

type healthResponse struct {
	dependencyHealthDTO
	Goroutines int    `json:"goroutines"`
	HeapMB     uint64 `json:"heap_mb"`
}

type discographyCaseDTO struct {
	Artist             string         `json:"artist"`
	ArtistRef          string         `json:"artist_ref"`
	Releases           int            `json:"releases"`
	SingleProvider     int            `json:"single_provider"`
	SingleProviderNoID int            `json:"single_provider_no_id"`
	ProviderCounts     map[string]int `json:"provider_counts"`
	LastSeen           time.Time      `json:"last_seen"`
}

type discographyQualityResult struct {
	cases       []discographyCaseDTO
	suspectRate float64
	lastSample  time.Time
}

type clientEventRequest struct {
	Type             string         `json:"type"`
	EventID          string         `json:"event_id"`
	CorrelationID    string         `json:"correlation_id"`
	ClientOccurredAt string         `json:"client_occurred_at"`
	Payload          map[string]any `json:"payload"`
	IgnoredSearchID  string         `json:"search_id"`
	IgnoredQueryNorm string         `json:"query_norm"`
}
