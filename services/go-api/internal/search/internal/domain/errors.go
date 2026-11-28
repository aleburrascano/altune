package domain

import (
	"altune/go-api/internal/shared/queuetimeout"
	"errors"
	"fmt"
)

type InvalidFavoriteError struct{ Reason string }

func NewInvalidFavoriteError(reason string) *InvalidFavoriteError {
	return &InvalidFavoriteError{Reason: reason}
}

func (e *InvalidFavoriteError) Error() string { return e.Reason }

type FavoritesFullError struct{ Limit int }

func NewFavoritesFullError(limit int) *FavoritesFullError { return &FavoritesFullError{Limit: limit} }

func (e *FavoritesFullError) Error() string {
	return fmt.Sprintf("favorites are full: at most %d per account", e.Limit)
}

type InvalidEventError struct{ Reason string }

func NewInvalidEventError(reason string) *InvalidEventError {
	return &InvalidEventError{Reason: reason}
}

func (e *InvalidEventError) Error() string { return e.Reason }

type PartialResultError struct {
	Page int
	Err  error
}

func (e *PartialResultError) Error() string {
	return fmt.Sprintf("partial result: page %d failed: %v", e.Page, e.Err)
}

func (e *PartialResultError) Unwrap() error { return e.Err }

type PartialKindsError struct {
	Failed []ResultKind
	Err    error
}

func (e *PartialKindsError) Error() string {
	return fmt.Sprintf("partial result: kinds %v failed: %v", e.Failed, e.Err)
}

func (e *PartialKindsError) Unwrap() error { return e.Err }

var (
	ErrArtistIdentityNotFound        = errors.New("no artist matches the name exactly")
	ErrDiscogsArtistNotFound         = errors.New("no discogs artist matches the name")
	ErrArtworkDegraded               = errors.New("artwork resolution degraded")
	ErrArtworkUnavailable            = errors.New("artwork provider unavailable")
	ErrEnrichmentDegraded            = errors.New("enrichment degraded")
	ErrFavoritesFull                 = errors.New("favorites are full")
	ErrIdentityStoreUnavailable      = errors.New("identity store unavailable")
	ErrInvalidSearchOption           = errors.New("search service option is out of range")
	ErrPartialCatalog                = errors.New("catalog titles are partial")
	ErrTrackerPartial                = errors.New("tracker sheet read in part")
	ErrProviderRateLimitQueueTimeout = queuetimeout.Err
	ErrSourceNotConfigured           = errors.New("unreleased source not configured")
	ErrUnknownDiscographyGroupBy     = errors.New("unknown discography grouping")
	ErrYtdlpBusy                     = errors.New("yt-dlp soundcloud search: all slots busy")
)

var (
	ErrActivityCollaboratorRequired        = errors.New("record event service needs its event store, activity feed and clock, pass the noop feed to switch the feed off")
	ErrArtistPageCollaboratorRequired      = errors.New("artist page services need every collaborator, pass the null adapter to switch one off")
	ErrArtworkCollaboratorRequired         = errors.New("artwork filler needs every collaborator, pass the absent adapter to switch one off")
	ErrBehaviorRequired                    = errors.New("ranking experiments need a behavioral scorer, pass NoBehavioralScoring to switch it off")
	ErrBehavioralConsumerRequired          = errors.New("behavioral scoring needs its listening feedback consumer and clock")
	ErrDetailsCollaboratorRequired         = errors.New("enrichment service needs every collaborator, pass the null object to switch one off")
	ErrFavoritesCollaboratorRequired       = errors.New("favorites services need their repository, pass the absent adapter to switch it off")
	ErrHandlerClockRequired                = errors.New("handler: the discovery handler requires a clock")
	ErrHandlerServiceRequired              = errors.New("handler: the discovery handler requires every service")
	ErrHistoryClockRequired                = errors.New("history: the service requires a clock")
	ErrHistoryCollaboratorRequired         = errors.New("history services need their repository, pass the absent adapter to switch it off")
	ErrHistoryIDSourceRequired             = errors.New("history: an id source is required")
	ErrJitterRequired                      = errors.New("ytmusic: the adapter needs a jitter source")
	ErrOwnershipCollaboratorRequired       = errors.New("ownership enrichment needs its background tracker, ownership reader and track number filler, pass the absent filler to switch filling off")
	ErrOwnershipRequired                   = errors.New("handler: the ownership stamper is required, pass NoOwnership to switch it off")
	ErrRelatedCollaboratorRequired         = errors.New("related services need every collaborator")
	ErrSearchCollaboratorRequired          = errors.New("search service needs every collaborator, pass the absent adapter to switch one off")
	ErrSleeperRequired                     = errors.New("ytmusic: the adapter needs a sleeper")
	ErrSoundCloudCollaboratorRequired      = errors.New("soundcloud api adapter needs its http client and search fallback, pass NoFallback to switch the fallback off")
	ErrTrackerSheetCollaboratorRequired    = errors.New("tracker sheet client needs its http client and index cache")
	ErrUnreleasedCollaboratorRequired      = errors.New("unreleased service needs every collaborator, pass the null object to switch one off")
	ErrVocabularyCollaboratorRequired      = errors.New("vocabulary services need every collaborator, pass the absent adapter to switch one off")
	ErrVocabularyStoreCollaboratorRequired = errors.New("vocabulary store needs its normalizer and metaphone, pass NoMetaphone to switch phonetic matching off")
)
