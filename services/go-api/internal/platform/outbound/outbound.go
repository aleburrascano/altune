package outbound

import (
	"altune/go-api/internal/platform/outbound/internal/adapters/egress"
	"altune/go-api/internal/platform/outbound/internal/adapters/youtubemusic"
	"altune/go-api/internal/platform/outbound/internal/adapters/ytdlp"
)

type (
	BadPayloadError            = egress.BadPayloadError
	PersistentAuthError        = egress.PersistentAuthError
	ReqOption                  = egress.ReqOption
	StatusError                = egress.StatusError
	TransportOptions           = egress.TransportOptions
	YouTubeMusicBudgetSettings = youtubemusic.Settings
)

const (
	BodyCap               = egress.BodyCap
	YouTubeMusicBaseURL   = youtubemusic.MusicURL
	YouTubeMusicUserAgent = youtubemusic.UserAgent
)

var (
	DefaultTransport    = egress.DefaultTransport
	DoCapped            = egress.DoCapped
	ErrBodyTooLarge     = egress.ErrBodyTooLarge
	GetBytes            = egress.GetBytes
	GetBytesCapped      = egress.GetBytesCapped
	GetJSON             = egress.GetJSON
	GetJSONWithStatus   = egress.GetJSONWithStatus
	GetText             = egress.GetText
	IsAuthStatus        = egress.IsAuthStatus
	MustNewClient       = egress.MustNewClient
	NewClient           = egress.NewClient
	NewGuardedTransport = egress.NewGuardedTransport
	NewHeadRequest      = egress.NewHeadRequest
	NewLiveTransport    = egress.NewLiveTransport
	NewTransport        = egress.NewTransport
	PostBytesCapped     = egress.PostBytesCapped
	PostBytesCappedOK   = egress.PostBytesCappedOK
	PostJSON            = egress.PostJSON
	Send                = egress.Send
	WithBodyCap         = egress.WithBodyCap
	WithHeader          = egress.WithHeader

	NewYouTubeMusicBudget        = youtubemusic.NewBudget
	ReserveYouTubeMusicSlot      = youtubemusic.ReserveSlot
	YouTubeMusicBody             = youtubemusic.Body
	YouTubeMusicEndpointURL      = youtubemusic.URL
	YouTubeMusicHasExplicitBadge = youtubemusic.HasExplicitBadge
	YouTubeMusicToolClient       = youtubemusic.ToolClient

	YtdlpDumpJSON                     = ytdlp.DumpJSON
	YtdlpOutputShowsSourceUnavailable = ytdlp.OutputShowsSourceUnavailable
)
