# ADR-0020: Split the `PgxTrackRepository` god repo into focused pgx adapters behind the unified port

- **Status:** Accepted
- **Date:** 2026-09-07
- **Deciders:** solo + Claude
- **Context tags:** [pattern | layer | arch]

## Context

One concrete struct, `PgxTrackRepository`, carries ~14 methods with three unrelated reasons to change: track CRUD (`services/go-api/internal/catalog/adapters/persistence/track_repo.go` — `Add`, `GetByID`, `ListForUser`, `ListByIDs`, `Update`, `SetTrackNumber`, `Delete`, `GetByDedupKey`), library grouping/filtering SQL (`library_lens_repo.go` — `ListAlbumsForUser` :60, `ListArtistsForUser` :92, `ListFilteredForUser` :135, `ListOwnedTrackRefs` :183), and featured-artist joins (`featured_artist_repo.go` — `ReplaceFeaturedArtists` :99, `ListTracksFeaturing` :132). A change to library grouping SQL and a change to the featured-artist join schema touch the same object for no shared reason — the classic god-object smell.

ADR predecessor context: ticket #80 (merged) deliberately unified the *interface* into a single `ports.TrackRepository` (`internal/catalog/ports/track_repo.go`, 13 methods). This ADR does **not** reverse that; it splits the *concrete struct* that sits behind the unified port. Per `docs/workflows/refactor.md` this is a structure-only change with green tests before and after, and because the struct is a seam shared across modules — acquisition (`internal/acquisition/ports/ports.go` :94, `GetByID`+`Update`), playback (`internal/playback/adapters/catalogbridge/now_playing_reader.go` :14, `GetByID`), and discovery (`internal/discovery/adapters/catalogbridge/ownership_reader.go` :14, `ListOwnedTrackRefs`) — it needs this ADR before code.

## Decision

Split the concrete struct into three focused pgx adapters in the existing `persistence` package, each with one reason to change, and realize the unified `ports.TrackRepository` with a thin composite that embeds all three:

- `PgxTrackRepository` (core, keeps `track_repo.go`) — track CRUD: `Add`, `GetByID`, `ListByIDs`, `ListForUser`, `Update`, `SetTrackNumber`, `Delete`, `GetByDedupKey`, and `ListOwnedTrackRefs`.
- `PgxLibraryLensRepository` (`library_lens_repo.go`) — library read-models: `ListFilteredForUser`, `ListAlbumsForUser`, `ListArtistsForUser`.
- `PgxFeaturedArtistRepository` (`featured_artist_repo.go`) — featured-artist joins: `ReplaceFeaturedArtists`, `ListTracksFeaturing`.
- `PgxCatalogTrackRepository` — a composite that embeds `*PgxTrackRepository`, `*PgxLibraryLensRepository`, and `*PgxFeaturedArtistRepository`. Go method promotion makes it satisfy the full 13-method `ports.TrackRepository`; it is what catalog services bind to.

**`OwnedTrackRef` / `ListOwnedTrackRefs` are out of scope and stay put.** `ListOwnedTrackRefs` keeps its `*PgxTrackRepository` receiver and its signature and domain type verbatim — its body relocates into `track_repo.go` (it is a track-ref projection, not a library grouping) so it stays on the core struct once the library methods move off. Discovery's `OwnershipReader` binds to it structurally (`ownedTrackLister`), so leaving it on the core struct means discovery needs no change at all.

The package-level free helpers `writeTrackFeatured` / `loadFeaturedForTracks` / `scanTrack*` / `nullString` / `nullInt64` are not methods, so they stay package-level and are shared by whichever struct needs them (e.g. core `Add` and `PgxFeaturedArtistRepository.ReplaceFeaturedArtists` both call `writeTrackFeatured`). No new coupling.

### How each consumer rebinds (zero behavior change)

The cross-module consumers already depend on narrow, structurally-satisfied interfaces, and every method they name still lives on the trimmed **core** `PgxTrackRepository` — so they rebind by touching nothing:

| Consumer | Binds to | Methods needed | Rebind |
|---|---|---|---|
| Catalog services (`app.go` `wireCatalog`, :316–:350) | `ports.TrackRepository` (full) | mixed subsets | Wire the composite `PgxCatalogTrackRepository`; service constructor signatures stay `ports.TrackRepository`, so no service file changes. |
| Acquisition (`acquisition/ports/ports.go` :94) | own 2-method `TrackRepository` (`GetByID`+`Update`) | on core | Pass `cat.trackRepo` (core) as today (`app.go` :304, :342–:343). No change. |
| Playback (`playback/.../now_playing_reader.go` :14) | local `trackReader` (`GetByID`) | on core | Pass core `trackRepo` via `wirePlayback` (`app.go` :378). No change. |
| Discovery (`discovery/.../ownership_reader.go` :14) | local `ownedTrackLister` (`ListOwnedTrackRefs`) | on core | Pass `cat.trackRepo` (core) (`app.go` :232). No change — explicitly out of scope. |

Only `app.go`'s catalog wiring gains one line (build the composite for the fat unified port); acquisition, playback, and discovery wiring stay byte-for-byte identical.

### Strangler / step order (each commit green)

Follows `docs/workflows/refactor.md` — green before and after, each step independently committable and revertible; no SQL text changes, methods only change receiver and file.

0. **Safety net.** Confirm green: `go test ./...` plus the `DATABASE_URL`-gated `track_repo_test.go`, `library_lens_repo_test.go`, `featured_artist_repo_test.go`. Add coverage first if thin.
1. **Seam.** Introduce `PgxCatalogTrackRepository` embedding only the current `*PgxTrackRepository` (a no-op facade — it already satisfies the port); wire catalog services through it in `app.go`. Behaviorally inert. Green.
2. **Extract featured.** Move `ReplaceFeaturedArtists` / `ListTracksFeaturing` receivers to a new `PgxFeaturedArtistRepository`; embed it in the composite and build it in the composite constructor. Cross-module consumers untouched. Green.
3. **Extract library lens.** Move `ListFilteredForUser` / `ListAlbumsForUser` / `ListArtistsForUser` to `PgxLibraryLensRepository`; embed in the composite. Relocate `ListOwnedTrackRefs` into `track_repo.go` keeping its `*PgxTrackRepository` receiver (do **not** move it onto the lens repo). Green.
4. **Land docs.** Core `PgxTrackRepository` is now pure track CRUD (no rename — the name stays accurate and keeps acquisition/playback/discovery wiring literally unchanged). Update `internal/catalog/CLAUDE.md` (persistence file map), `internal/app/CLAUDE.md` if a wiring name changed, and `okf/backend/catalog/index.md` in the same commit as the code (pre-commit hooks enforce). Confirm green.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Re-split `ports.TrackRepository` into per-concern sub-ports and narrow each service | Reverses ticket #80's just-merged decision to unify the interface. The god-object smell is in the concrete struct, not the port; re-splitting the interface churns every service signature for no persistence-layer gain. |
| Narrow each catalog service to a per-use-case sub-interface now (full ISP) | Real win but orthogonal to breaking up the concrete struct, and larger churn. It is additive and needs no further ADR, so defer it. |
| Reorganize files only, leave the one struct | The three files already exist; the problem is one struct with three reasons to change, not file layout. Moving code between files without splitting the receiver changes nothing. |
| Big-bang split in a single commit | `refactor.md` bans partial/god rewrites; the strangler order keeps every commit green and revertible. |
| Composite via manual delegation instead of embedding | Embedding promotes the methods for free with zero delegation boilerplate; hand-written delegation is code to maintain for no benefit. |

## Consequences

### What becomes easier
- Each concrete repo has exactly one reason to change: `tracks` row schema, library grouping SQL, or featured-artist joins. The `DATABASE_URL`-gated tests already partition along these same three files.
- Cross-module seams stay narrow and untouched; acquisition/playback/discovery keep binding to small structural interfaces the core struct satisfies.

### What becomes harder
- One extra type (`PgxCatalogTrackRepository`) to understand in the composition root: a reader must know the unified port is realized by embedding three structs rather than by one struct implementing it directly.

### What we're committing to (and the cost to reverse)
- The composite-over-focused-structs shape behind an unchanged unified port. Reversal is cheap: re-merge the methods onto one struct and drop the composite — no SQL, no port contract, and no migration changes, so the blast radius is the `persistence` package plus one wiring line.

## Implementation notes

Execution is ticket #83 (epic #70), which follows the step order above. This ADR is the decision record only; no code changes ship with it.

## Vault references

- [vault: wiki/concepts/Single Responsibility Principle.md]
- [vault: wiki/concepts/Coupling and Cohesion.md]
- [vault: wiki/concepts/Interface Segregation Principle.md]
- [vault: wiki/concepts/Strangler Fig Pattern.md]

## Related

- Epic: #70 (catalog persistence god-object breakup)
- Predecessor: ticket #80 — unified the `ports.TrackRepository` interface (this ADR splits the concrete struct behind it)
- Executed by: ticket #83 (the split code)
- Workflow: `docs/workflows/refactor.md` (architectural refactor → ADR → strangler plan)
