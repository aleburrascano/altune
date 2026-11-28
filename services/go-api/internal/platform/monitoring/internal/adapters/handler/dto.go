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

type DependencyHealth struct {
	DB     DepStatus        `json:"db"`
	Redis  DepStatus        `json:"redis"`
	Auth   DepStatus        `json:"auth"`
	Detail DependencyDetail `json:"detail"`
}

type DependencyDetail struct {
	DBLatencyMs    int64     `json:"db_latency_ms"`
	DBError        string    `json:"db_error,omitempty"`
	RedisLatencyMs int64     `json:"redis_latency_ms"`
	RedisError     string    `json:"redis_error,omitempty"`
	AuthLatencyMs  int64     `json:"auth_latency_ms"`
	AuthError      string    `json:"auth_error,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

type healthResponse struct {
	DependencyHealth
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
