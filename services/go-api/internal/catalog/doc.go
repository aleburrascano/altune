// Package catalog is the seam map for the catalog module: a user's saved
// tracks, their playlists, and the audio behind them. It holds no code; the
// module lives in the subpackages below, and this file only states how they fit
// together so a newcomer need not reconstruct the map from every file.
//
// # Layout
//
// Dependencies point inward: adapters depend on service, ports and domain;
// service depends on ports and domain; ports depends on domain; domain depends
// on nothing in the module (only internal/shared).
//
//   - domain: the aggregates, value types and domain errors. No I/O.
//   - ports: the interfaces the services consume (repositories, audio store,
//     scheduler, resolver, metrics), plus Noop defaults for the optional ones.
//   - service: one application service per use case.
//   - adapters/handler: chi HTTP handlers that decode requests and call services.
//   - adapters/persistence: pgx/Postgres implementations of the repository ports.
//   - adapters/storage: audio store implementations (filesystem, S3-compatible
//     object storage).
//   - adapters/metrics: the expvar implementations of ports.AudioStoreMetrics
//     and ports.DBCallMetrics, read by the operator-only GET /admin/metrics/live.
//   - adapters/discoverybridge: adapts the discovery module's featured-artist
//     lookup to ports.FeaturedArtistResolver.
//   - catalogtest: in-memory fakes of the ports (one file per fake) for the
//     catalog's tests.
//
// # Aggregates
//
// Track (domain/track.go) is the central aggregate: an owner-scoped saved song
// identified by TrackId, carrying its metadata, a content-derived DedupKey and
// an optional client IdempotencyKey, featured artists, and its acquisition
// state. AcquisitionStatus moves pending to ready (MarkReady sets AudioRef) or
// to failed; each transition method refuses a disallowed source state with
// ErrIllegalAcquisitionTransition; FailureCode is the stable prefix of the persisted failure reason,
// and FailureMessage maps it to user-facing text. The acquisition module
// drives these transitions; the catalog owns the type.
//
// Playlist (domain/playlist.go) is the second aggregate: an owner-scoped named,
// ordered list of PlaylistTrack references to tracks, identified by PlaylistId.
//
// Supporting value types: FeaturedArtist (domain/featured_artist.go), and the
// library-lens read model in domain/library_lens.go (LibraryQuery, AlbumGroup,
// ArtistGroup, OwnedTrackRef). MaxLibraryPageSize there is the one row cap
// every bounded read references. Errors are CodedError values carrying an HTTP
// status and a stable "catalog.*" code; ValidationError aliases the shared type.
//
// # Port groups
//
// Track persistence is consumed through narrow per-capability ports
// (ports/track_repo.go), so each service depends only on the methods it calls:
// TrackAdder, TrackGetter, TrackBatchGetter, TrackLister, TrackUpdater,
// TrackNumberSetter and TrackDeleter, composed as TrackReadWriter,
// TrackNumberFiller, TrackAddUpdater and TrackLookup. Beside them sit LibraryLensRepository
// (grouped and filtered library reads) and FeaturedArtistRepository.
// StalePendingFailer (ports/stale_pending.go) sweeps orphaned pending tracks.
//
// Playlist persistence splits into PlaylistLifecycleRepository (create, list,
// get, rename, delete) and PlaylistMembershipRepository (add, remove, reorder
// tracks; every write owner-scoped, refusing with ErrPlaylistNotOwned).
//
// Audio: AudioStore (exists, store, stream, delete) with the optional
// capabilities AudioURLSigner (presigned URLs, clamped to MaxPresignTTL) and
// AudioLister, discovered by type assertion on the store.
//
// Collaborators: AcquisitionScheduler (implemented by the acquisition module),
// FeaturedArtistResolver (implemented by discoverybridge) and AudioStoreMetrics.
// The scheduler and the metrics are optional, each with a Noop default so a
// service works without it; the resolver is a required constructor argument of
// BackfillFeaturedService, which is the only service that consumes it.
//
// # Composed repositories
//
// In adapters/persistence, PgxTrackRepository, PgxLibraryLensRepository and
// PgxFeaturedArtistRepository each implement one slice over the same pool.
// PgxCatalogTrackRepository embeds all three, and its compile-time assertions
// (catalog_track_repo.go) pin it to every track port plus LibraryLensRepository
// and FeaturedArtistRepository, so one value is passed to every track-backed
// service. PgxPlaylistRepository alone implements both playlist ports. All
// adapters bound each database call with withDBTimeout (pool.go), which reports
// every call its own deadline cuts off to the DBCallMetrics set by SetDBCallMetrics.
//
// PgxTrackRepository is also used directly outside the catalog services: the
// acquisition module writes tracks through it, the stale-pending reconcile job
// uses its FailStalePending, discovery's catalogbridge reads ListOwnedTrackRefs,
// and playback's catalogbridge reads the now-playing track.
//
// # Services and use cases
//
// Each service lives in its own file under service/, takes its required ports
// as constructor arguments, and takes optional collaborators (events publisher,
// scheduler, metrics) as functional options applied through applyOptions.
//
//   - AddTrackService (TrackAddUpdater): save a track, then schedule acquisition.
//   - ListTracksService, LibraryLensService (LibraryLensRepository): library
//     pages and album/artist lenses.
//   - GetTrackStatusService (TrackGetter): acquisition status polling.
//   - SetTrackNumberService (TrackNumberFiller): album position; a no-op
//     write on a missing or foreign track surfaces ErrTrackNotFound.
//   - DeleteTrackService (TrackDeleter, AudioStore): delete the row, then its
//     audio; a failed audio delete surfaces ErrAudioOrphaned and is recorded
//     for ReconcileOrphanedAudioService (OrphanedAudioQueue, AudioStore) to
//     retry, never deleting a key a track still references.
//   - StreamTrackService (TrackReadWriter, AudioStore): stream ready audio,
//     re-scheduling acquisition when the object is missing.
//   - AudioURLService (TrackBatchGetter, AudioStore): batch presigned URLs.
//   - BackfillFeaturedService (TrackLister, FeaturedArtistRepository,
//     FeaturedArtistResolver) and ListFeaturingService (FeaturedArtistRepository):
//     featured-artist backfill and lookup.
//   - PlaylistLifecycleService (PlaylistLifecycleRepository) and
//     PlaylistMembershipService (PlaylistMembershipRepository, TrackLookup):
//     playlists and their contents.
//   - ReconcileStalePendingService (StalePendingFailer): background sweep of
//     pending tracks orphaned by a dead process.
//
// Handlers group services by resource: TrackHandler (add, list, status, track
// number, delete, plus the FeaturedArtistHandler routes), LibraryHandler,
// PlaylistHandler, StreamHandler and AudioURLHandler.
//
// # Wiring
//
// Production wiring lives outside this module in internal/app/catalog_wiring.go.
// It builds the audio store and acquisition scheduler, then one
// PgxCatalogTrackRepository and one PgxPlaylistRepository, constructs every
// service over them, and wraps the services in handlers that
// internal/app/routes.go mounts. The stale-pending sweep is started from
// internal/app/background_jobs.go.
package catalog
