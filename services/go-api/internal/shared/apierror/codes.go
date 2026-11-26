package apierror

var catalog []Code

func code(name string, kind Kind, title string) Code {
	c := Code{name: name, kind: kind, title: title}
	catalog = append(catalog, c)
	return c
}

func Catalog() []Code {
	return append([]Code(nil), catalog...)
}

var (
	CodeInternal         = code("internal", KindInternal, "Internal server error")
	CodeRequestInvalidID = code("request.invalid_id", KindInvalid, "Invalid identifier")
	CodeRequestInvalid   = code("request.invalid_body", KindInvalid, "Invalid request body")
	CodeRequestTooLarge  = code("request.too_large", KindTooLarge, "Request body too large")
	CodeRequestTimeout   = code("request.timeout", KindTimeout, "Request timed out")
)

var (
	CodeAuthThrottled               = code("auth.throttled", KindRateLimited, "Too many failed authentication attempts")
	CodeAuthUnavailable             = code("auth.unavailable", KindUnavailable, "Authentication unavailable")
	CodeAuthTokenInvalid            = code("auth.token_invalid", KindUnauthenticated, "Invalid token")
	CodeAuthTokenMissing            = code("auth.token_missing", KindUnauthenticated, "Authentication required")
	CodeAuthTokenMalformed          = code("auth.token_malformed", KindUnauthenticated, "Malformed authorization header")
	CodeAuthTokenSignatureInvalid   = code("auth.token_signature_invalid", KindUnauthenticated, "Invalid token signature")
	CodeAuthTokenExpired            = code("auth.token_expired", KindUnauthenticated, "Token expired")
	CodeAuthTokenRevoked            = code("auth.token_revoked", KindUnauthenticated, "Token revoked")
	CodeAuthTokenClaimMissingEXP    = code("auth.token_claim_missing_exp", KindUnauthenticated, "Token has no expiry")
	CodeAuthTokenClaimInvalidISS    = code("auth.token_claim_invalid_iss", KindUnauthenticated, "Token issuer is not accepted")
	CodeAuthTokenClaimInvalidAUD    = code("auth.token_claim_invalid_aud", KindUnauthenticated, "Token audience is not accepted")
	CodeAuthTokenClaimInvalidSUB    = code("auth.token_claim_invalid_sub", KindUnauthenticated, "Token subject is invalid")
	CodeAuthTokenClaimInvalidIAT    = code("auth.token_claim_invalid_iat", KindUnauthenticated, "Token issue time is invalid")
	CodeAuthFailureInvalidBody      = code("auth_failure.invalid_body", KindInvalid, "Invalid auth failure report")
	CodeEventStreamUnsupported      = code("eventstream.streaming_unsupported", KindInternal, "Streaming unsupported")
	CodeEventStreamTooManyStreams   = code("eventstream.too_many_streams", KindRateLimited, "Too many event streams")
	CodeObservePrincipalRequired    = code("observe.principal_required", KindForbidden, "Overseer principal required")
	CodeObserveQualityTimeout       = code("observe.discography_quality_timeout", KindTimeout, "Discography quality query timed out")
	CodeObserveEventFeedDown        = code("observe.event_feed_unavailable", KindUnavailable, "Event feed unavailable")
	CodeObserveStreamLimit          = code("observe.stream_subscriber_limit", KindRateLimited, "Too many observe streams open")
	CodeObserveStreamingUnsupported = code("observe.streaming_unsupported", KindInternal, "Streaming unsupported")
	CodeObserveStreamUnavailable    = code("observe.stream_unavailable", KindUnavailable, "Stream unavailable")
)

var (
	CodeCatalogTrackNotFound             = code("catalog.track_not_found", KindNotFound, "Track not found")
	CodeCatalogPlaylistNotFound          = code("catalog.playlist_not_found", KindNotFound, "Playlist not found")
	CodeCatalogAudioNotAvailable         = code("catalog.audio_not_available", KindNotFound, "Audio not available")
	CodeCatalogAudioTemporarilyDown      = code("catalog.audio_temporarily_unavailable", KindUnavailable, "Audio temporarily unavailable")
	CodeCatalogTemporarilyDown           = code("catalog.temporarily_unavailable", KindUnavailable, "Catalog temporarily unavailable")
	CodeCatalogAudioOrphaned             = code("catalog.audio_orphaned", KindInternal, "Track deleted but audio file orphaned")
	CodeCatalogTrackAlreadyInPlaylist    = code("catalog.track_already_in_playlist", KindConflict, "Track already in playlist")
	CodeCatalogTrackIDsRequired          = code("catalog.track_ids_required", KindInvalid, "Track ids required")
	CodeCatalogBatchTooLarge             = code("catalog.batch_too_large", KindInvalid, "Too many track ids")
	CodeCatalogInvalidTrackID            = code("catalog.invalid_track_id", KindInvalid, "Invalid track id")
	CodeCatalogFeaturedArtistKeyRequired = code("catalog.featured_artist_key_required", KindInvalid, "Featured artist key required")
	CodeCatalogInvalidDeezerID           = code("catalog.invalid_deezer_id", KindInvalid, "Invalid Deezer id")
	CodeCatalogPlaylistFull              = code("catalog.playlist_full", KindInvalid, "Playlist is full")
	CodeCatalogTooManyPlaylists          = code("catalog.too_many_playlists", KindInvalid, "Playlist limit reached")
	CodeCatalogLibraryFull               = code("catalog.library_full", KindInvalid, "Library is full")
	CodeCatalogBackfillCoolingDown       = code("catalog.backfill_cooling_down", KindRateLimited, "Featured artist backfill ran recently")
	CodeCatalogBackfillInProgress        = code("catalog.backfill_in_progress", KindConflict, "Featured artist backfill already in progress")
	CodeCatalogAudioRateLimited          = code("catalog.audio_rate_limited", KindRateLimited, "Too many audio requests")
	CodeCatalogWriteRateLimited          = code("catalog.write_rate_limited", KindRateLimited, "Too many writes")
	CodeCatalogValidation                = code("catalog.validation_error", KindInvalid, "Invalid catalog input")
)

var (
	CodeDiscoveryRateLimited         = code("discovery.rate_limited", KindRateLimited, "Too many requests")
	CodeDiscoveryInvalidFavorite     = code("discovery.invalid_favorite", KindInvalid, "Invalid favorite")
	CodeDiscoveryFavoritesFull       = code("discovery.favorites_full", KindConflict, "Favorites are full")
	CodeDiscoveryInvalidEvent        = code("discovery.invalid_event", KindInvalid, "Invalid search event")
	CodeDiscoveryInvalidBody         = code("discovery.invalid_body", KindInvalid, "Invalid request body")
	CodeDiscoveryInvalidEventType    = code("discovery.invalid_event_type", KindInvalid, "Invalid event type")
	CodeDiscoveryInvalidKind         = code("discovery.invalid_kind", KindInvalid, "Invalid kind")
	CodeDiscoveryInvalidParam        = code("discovery.invalid_param", KindInvalid, "Invalid parameter")
	CodeDiscoveryInvalidProvider     = code("discovery.invalid_provider", KindInvalid, "Unknown provider")
	CodeDiscoveryQueryRequired       = code("discovery.q_required", KindInvalid, "Query required")
	CodeDiscoveryAllProvidersFailed  = code("discovery.all_providers_failed", KindUnavailable, "Every search provider failed")
	CodeDiscoveryContentUnserved     = code("discovery.content_unserved", KindNotFound, "Content not served")
	CodeDiscoveryProviderTimeout     = code("discovery.provider_timeout", KindTimeout, "Provider timed out")
	CodeDiscoveryProviderRateLimited = code("discovery.provider_rate_limited", KindUnavailable, "Provider rate limited")
	CodeDiscoveryProviderCircuitOpen = code("discovery.provider_circuit_open", KindUnavailable, "Provider paused after repeated failures")
	CodeEnrichmentUnavailable        = code("enrichment_unavailable", KindUnavailable, "Enrichment is not configured")
	CodeDiscoveryProviderError       = code("discovery.provider_error", KindUpstreamFailed, "Provider failed")
)

var (
	CodePlaybackRateLimited           = code("playback.rate_limited", KindRateLimited, "Too many queue state requests")
	CodePlaybackUnavailable           = code("playback.unavailable", KindUnavailable, "Queue state temporarily unavailable")
	CodePlaybackStaleQueueWrite       = code("playback.stale_queue_write", KindConflict, "A newer queue snapshot is already stored")
	CodePlaybackQueuePositionMismatch = code("playback.queue_position_mismatch", KindConflict, "Queue position does not match the stored queue")
	CodePlaybackPositionMsNegative    = code("playback.position_ms_negative", KindInvalid, "Position must not be negative")
	CodePlaybackQueueTooLong          = code("playback.queue_too_long", KindInvalid, "Queue too long")
	CodePlaybackStringTooLong         = code("playback.string_too_long", KindInvalid, "Text too long")
	CodePlaybackStringContainsNul     = code("playback.string_contains_nul", KindInvalid, "Text contains a NUL byte")
	CodePlaybackCurrentIdxOutOfRange  = code("playback.current_idx_out_of_range", KindInvalid, "Current index out of range")
	CodePlaybackCurrentTrackIDMissing = code("playback.current_track_id_missing", KindInvalid, "Current track id missing")
	CodePlaybackUnknownRepeatMode     = code("playback.unknown_repeat_mode", KindInvalid, "Unknown repeat mode")
	CodePlaybackUnknownSourceKind     = code("playback.unknown_source_kind", KindInvalid, "Unknown source kind")
	CodePlaybackUnrecognizedSourceID  = code("playback.unrecognized_source_id", KindInvalid, "Unrecognized source id")
	CodePlaybackMalformedTrackID      = code("playback.malformed_track_id", KindInvalid, "Malformed track id")
	CodePlaybackNaturalOrderMismatch  = code("playback.natural_order_mismatch", KindInvalid, "Natural order does not match the queue")
)

var (
	CodeAcquisitionRetryNotFailed    = code("acquisition.retry_not_failed", KindConflict, "Track is not in failed state")
	CodeAcquisitionCooldownActive    = code("acquisition.cooldown_active", KindRateLimited, "Cooldown active")
	CodeAcquisitionReacquireNotReady = code("acquisition.reacquire_not_ready", KindConflict, "Track has no audio to replace")
	CodeAcquisitionJobInFlight       = code("acquisition.job_in_flight", KindConflict, "Another acquisition for this track is already running")
	CodeAcquisitionShuttingDown      = code("acquisition.shutting_down", KindUnavailable, "Acquisition is shutting down")
	CodeAcquisitionPaused            = code("acquisition.paused", KindUnavailable, "Acquisition is paused")
)
