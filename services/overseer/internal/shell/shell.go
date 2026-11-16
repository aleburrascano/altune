package shell

import "altune/overseer/internal/shell/internal/adapters/httpapi"

type (
	Registry      = httpapi.Registry
	CollectStatus = httpapi.CollectStatus
	ClientConfig  = httpapi.ClientConfig
	Option        = httpapi.Option
)

var (
	NewHandler           = httpapi.NewHandler
	WithVerifier         = httpapi.WithVerifier
	WithOwnerUserID      = httpapi.WithOwnerUserID
	WithStaticFS         = httpapi.WithStaticFS
	WithClientConfig     = httpapi.WithClientConfig
	WithStreamInterval   = httpapi.WithStreamInterval
	WithCollectStatus    = httpapi.WithCollectStatus
	WithCredentialHealth = httpapi.WithCredentialHealth
	WithSeries           = httpapi.WithSeries
)
