# discovery domain — bounded-context local rules

Pure-Python domain types for the unified music search feature [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\docs\adr\0007-unified-music-search.md#L17-L17]. Sibling to `catalog/` (owned tracks); `discovery/` is "music not yet owned." Adapter terms (Deezer/MB/SC/Last.fm DTOs) never enter this folder — that's the ACL boundary.

## Key terms

- **SearchResult** — aggregate; carries `(kind, title, subtitle?, image_url?, confidence, sources: tuple[SourceRef, ...], extras: Mapping[str, object])`. Multi-source-merged results carry multiple `SourceRef`s. Invariants: title non-empty, sources tuple non-empty (enforced in `__post_init__`).
- **ResultKind** — `artist | album | track | playlist` enum [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\domain\discovery\result_kind.py]. Wire-serialized lowercase.
- **Confidence** — three-level enum with custom `__gt__`/`__lt__` ordering (HIGH > MEDIUM > LOW) [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\domain\discovery\confidence.py#L22-L40]. **Display-only** since the ADR-0007 ranking-overhaul addendum — the ordering operators remain for assertions/badges but ranking uses relevance (`fuse_and_rank`), not confidence.
- **ProviderName** — `DEEZER | MUSICBRAINZ | SOUNDCLOUD | LASTFM | ITUNES | THEAUDIODB` enum (iTunes + TheAudioDB added by discover-music-v2).
- **ProviderStatus** — `OK | TIMEOUT | ERROR | RATE_LIMITED | CIRCUIT_OPEN`. Maps adapter behaviors per AC#5/5a/5b/6.
- **SearchHistoryEntry** / **SearchClick** — aggregate roots for the two persistence surfaces. Identity is the `*Id` value object; equality by id (catalog/track.py precedent).

## Patterns specific here

- **No ISRC validation.** ISRC is `extras["isrc"]` in v1, not its own value object — providers vary on coverage (MB recording-search lacks it without `inc=isrc`s; SC user-uploads almost never have it). Dedup is a tuple-of-strings comparison [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\dedup.py], not a domain check.
- **SearchResult is fully frozen at construction** — `sources` and `extras` are tuples / `MappingProxyType`-wrapped. Dedup builds new `SearchResult`s rather than mutating.
- **Past-tense events** in `events.py`: `SearchPerformed`, `ResultClicked`. Carry `occurred_at`. Emitted to logs in v1; future analytics spec may persist.
- **`Artist` / `Album` / `Playlist` are NOT separate types** in v1 — they live as `ResultKind` values + `SearchResult` instances. Glossary moved them to "Canonical" pointing at SearchResult per spec deliverables [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\docs\ubiquitous-language.md#L43-L45].

## Known gotchas

- **Comparison-overlap mypy warnings** when asserting state on a `Confidence` value across multiple lines in tests — mypy narrows on `is` checks. Use `# type: ignore[comparison-overlap]` or stick to one assertion concept per test.
