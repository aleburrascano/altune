"""Merge + rank discovery results.

Per ADR-0007 (revised) + discover-music-v1 spec. The MERGE step is
unchanged: ISRC-match collapses entries to HIGH; JW >= 0.92 on normalized
(artist|title) collapses to HIGH; JW in [0.85, 0.92) collapses to MEDIUM;
otherwise entries stay separate (as LOW). Per-source priors choose the
canonical representative of a merged entry.

The RANK step is relevance-first (`fuse_and_rank`): a parameter-free match
gate drops results that share no content word with the query, then the
survivors sort by relevance-band → popularity → cross-provider agreement
(RRF) → prior → alpha. `confidence` is NOT a sort term and is no longer
displayed; it is retained only for telemetry.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import TYPE_CHECKING

from rapidfuzz import fuzz  # type: ignore[import-not-found,unused-ignore]
from rapidfuzz.distance import JaroWinkler  # type: ignore[import-not-found,unused-ignore]

from altune.application.discovery.normalize import normalize_for_match
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.domain.discovery.source_ref import SourceRef

# Per-source priors per ADR-0007 §"per-source priors".
_PRIORS: dict[ProviderName, float] = {
    ProviderName.MUSICBRAINZ: 0.95,
    ProviderName.DEEZER: 0.85,
    ProviderName.ITUNES: 0.85,
    ProviderName.LASTFM: 0.80,
    ProviderName.THEAUDIODB: 0.78,
    ProviderName.SOUNDCLOUD: 0.65,
}

_JW_HIGH = 0.92
_JW_MEDIUM = 0.85

# Reciprocal Rank Fusion constant. 60 is the value from the original RRF
# paper (Cormack et al.); it damps the gap between adjacent ranks so a
# provider's #1 and #2 don't dominate every other provider's whole list.
_RRF_K = 60

# Match gate (parameter-free, replaces the old tunable relevance floor): a
# result is kept only if it shares at least one CONTENT token with the query.
# Content tokens are normalized query tokens of length >= 2 minus these common
# stopwords. This is definitional ("share a word or you're not a match"), not a
# calibrated magnitude — there is no threshold to tune.
_STOPWORDS = frozenset(
    {
        "the",
        "a",
        "an",
        "and",
        "or",
        "of",
        "to",
        "in",
        "on",
        "for",
        "with",
        "at",
        "by",
        "feat",
        "ft",
        "featuring",
        "vs",
        "x",
        "is",
        "it",
        "my",
    }
)

# Artist names dedup at a stricter similarity than tracks/albums (a bare name
# has little text, so a loose threshold over-merges distinct artists).
_JW_ARTIST = 0.92


def _isrc_of(result: SearchResult) -> str | None:
    val = result.extras.get("isrc")
    if isinstance(val, str) and val:
        return val
    return None


def _winning_prior(result: SearchResult) -> float:
    return max(_PRIORS.get(s.provider, 0.0) for s in result.sources)


def _popularity(result: SearchResult) -> float:
    """Normalized popularity in [0, 1], or 0 when no source carries it."""
    val = result.extras.get("popularity")
    if isinstance(val, (int, float)):
        return float(val)
    return 0.0


# Low-quality release markers (in the normalized title) + record types that
# should sink below the real thing within a relevance band. Covers, karaoke,
# tributes, chiptune emulations and instrumental re-uploads frequently embed
# the real artist's name in their TITLE, so they normalize to the full query
# and land in the genuine track's relevance band — demotion is what keeps the
# real recording on top there.
_JUNK_TITLE_MARKERS = (
    "karaoke",
    "tribute",
    "made famous",
    "originally performed",
    "performed by",
    "in the style of",
    "backing track",
    "8 bit",
    "8bit",
    "8-bit",
    "16-bit",
    "lullaby",
)
# Single-word markers matched on a word boundary so they don't fire inside an
# unrelated word (e.g. "cover" must not match "Undercover").
_JUNK_TITLE_WORD_RE = re.compile(
    r"\b(cover|arrangement|instrumental|emulation|orgel)\b", re.IGNORECASE
)
_DEMOTED_RECORD_TYPES = frozenset({"compilation"})


def _is_demoted(result: SearchResult) -> bool:
    """True for karaoke/tribute/cover/compilation-style results that should rank
    below the genuine article within the same relevance band. Checks the RAW
    title (normalize strips bracketed '(Karaoke Version)' suffixes)."""
    title = result.title.lower()
    if any(marker in title for marker in _JUNK_TITLE_MARKERS):
        return True
    if _JUNK_TITLE_WORD_RE.search(title):
        return True
    record_type = result.extras.get("record_type")
    return isinstance(record_type, str) and record_type.lower() in _DEMOTED_RECORD_TYPES


def _content_tokens(text: str) -> set[str]:
    """Significant tokens of normalized text: length >= 2, minus stopwords."""
    return {t for t in text.split() if len(t) >= 2 and t not in _STOPWORDS}


def _genuine_split_exists(results: Sequence[SearchResult], query_tokens: set[str]) -> bool:
    """True when some result splits the query into song (title) + artist (subtitle).

    A genuine "song by artist" result has a title that is a STRICT subset of the
    query's content tokens and an artist whose tokens, together with the title,
    EXACTLY cover the query (song-tokens union artist-tokens == query). The coverage
    requirement is what makes this safe on a bare-title query: there the artist
    is absent from the query, so no split covers it and nothing is flagged — a
    real "Song / Artist" entry on a title-only search is never a bootleg. A
    spurious partial match (some result whose artist happens to be one query
    word) also fails coverage and can't trigger the rule.
    """
    for r in results:
        title_t = _content_tokens(normalize_for_match(r.title))
        artist_t = _content_tokens(normalize_for_match(r.subtitle or ""))
        if title_t and artist_t and title_t < query_tokens and (title_t | artist_t) == query_tokens:
            return True
    return False


def _is_bootleg(result: SearchResult, query_tokens: set[str], genuine_split: bool) -> bool:
    """True for a title-stuffed re-upload: the whole query sits in the TITLE and
    the artist field is foreign to the query (a junk label), while a genuine
    song/artist split exists in the same result set. Demoted within the band so
    the real recording wins even when the bootleg out-pops it."""
    if not genuine_split or not query_tokens:
        return False
    title_t = _content_tokens(normalize_for_match(result.title))
    artist_t = _content_tokens(normalize_for_match(result.subtitle or ""))
    return query_tokens <= title_t and bool(artist_t) and artist_t.isdisjoint(query_tokens)


def _passes_gate(result: SearchResult, query_norm: str) -> bool:
    """Keep a result only if it shares >= 1 content token with the query.

    Parameter-free match gate (no tunable threshold). If the query has no
    content tokens (all stopwords/short), don't gate — let everything through.
    """
    query_tokens = _content_tokens(query_norm)
    if not query_tokens:
        return True
    text = f"{normalize_for_match(result.subtitle or '')} {normalize_for_match(result.title)}"
    return bool(query_tokens & _content_tokens(text))


def _signature(result: SearchResult) -> str:
    """Normalized (artist|title) key for JW comparison."""
    title = normalize_for_match(result.title)
    subtitle = normalize_for_match(result.subtitle or "")
    return f"{subtitle}|{title}"


def _merge(a: SearchResult, b: SearchResult, confidence: Confidence) -> SearchResult:
    """Merge two results; higher per-source prior wins title/subtitle/extras."""
    canonical, other = (a, b) if _winning_prior(a) >= _winning_prior(b) else (b, a)
    # Dedup sources by (provider, external_id); preserve canonical's order.
    seen: set[tuple[ProviderName, str]] = set()
    sources: list[SourceRef] = []
    for s in (*canonical.sources, *other.sources):
        key = (s.provider, s.external_id)
        if key in seen:
            continue
        seen.add(key)
        sources.append(s)
    # Merge extras: canonical wins on conflict EXCEPT when canonical's value is
    # None and other has a real value (prevents high-prior providers with sparse
    # data from overwriting richer data). Popularity takes the max across sources.
    # NOTE: album name stabilization is handled post-merge in fuse_and_rank via
    # _album_lookup — the merge here uses the general rule (canonical wins).
    extras = {**other.extras}
    for k, v in canonical.extras.items():
        if v is not None or k not in extras:
            extras[k] = v
    pop = max(_popularity(a), _popularity(b))
    if pop > 0:
        extras["popularity"] = pop
    return SearchResult(
        kind=canonical.kind,
        title=canonical.title,
        subtitle=canonical.subtitle,
        image_url=canonical.image_url or other.image_url,
        confidence=confidence,
        sources=tuple(sources),
        extras=extras,
    )


def _mbid_of(result: SearchResult) -> str | None:
    val = result.extras.get("mbid")
    if isinstance(val, str) and val:
        return val
    return None


def _try_merge(a: SearchResult, b: SearchResult) -> SearchResult | None:
    """Return merged result, or None if they shouldn't merge."""
    if a.kind is not b.kind:
        return None
    isrc_a, isrc_b = _isrc_of(a), _isrc_of(b)
    if isrc_a and isrc_b and isrc_a == isrc_b:
        return _merge(a, b, Confidence.HIGH)
    # MusicBrainz ID is a high-confidence cross-source identity (MB + Last.fm
    # carry it); merge on an exact MBID match before falling back to JW.
    mbid_a, mbid_b = _mbid_of(a), _mbid_of(b)
    if mbid_a and mbid_b and mbid_a == mbid_b:
        return _merge(a, b, Confidence.HIGH)
    sim = JaroWinkler.similarity(_signature(a), _signature(b))
    if a.kind is ResultKind.ARTIST:
        # Artists merge only on a strict name match (no subtitle to disambiguate).
        return _merge(a, b, Confidence.HIGH) if sim >= _JW_ARTIST else None
    if sim >= _JW_HIGH:
        return _merge(a, b, Confidence.HIGH)
    if sim >= _JW_MEDIUM:
        return _merge(a, b, Confidence.MEDIUM)
    return None


def _as_low_confidence(result: SearchResult) -> SearchResult:
    """Standalone results carry LOW confidence; merging overrides via _merge."""
    if result.confidence is Confidence.LOW:
        return result
    return SearchResult(
        kind=result.kind,
        title=result.title,
        subtitle=result.subtitle,
        image_url=result.image_url,
        confidence=Confidence.LOW,
        sources=result.sources,
        extras=result.extras,
    )


def _relevance_score(result: SearchResult, query_norm: str) -> float:
    """Continuous query-relevance in [0, 1] — the primary ranking signal.

    Scored against the result's OWN IDENTITY (uniform, no kind favoritism):
    an artist by its name (`title`); a track/album by its `"<artist> <title>"`
    form (or the title alone, whichever matches better). We deliberately do NOT
    score a track by a bare artist-field match — otherwise a song would tie its
    own artist at band 1.0 on an artist-name query, letting a hit song steal the
    headline from the artist. With own-identity scoring, any exact artist name
    headlines that artist (mainstream or underground) and any title headlines
    its song. Its songs still appear (kept by the token gate), just below.

    `token_sort_ratio` (not `token_set_ratio`) is deliberate: token_set returns
    100 whenever the title is a subset of the query, so every same-title result
    would tie at 100. token_sort penalizes missing/extra tokens. Empty query
    (merge-only unit tests) scores 0.
    """
    query = query_norm.strip()
    if not query:
        return 0.0
    title = normalize_for_match(result.title)
    candidates = [fuzz.token_sort_ratio(query, title)]
    if result.subtitle:
        combined = f"{normalize_for_match(result.subtitle)} {title}".strip()
        candidates.append(fuzz.token_sort_ratio(query, combined))
    # Also score on CONTENT tokens (stopwords/articles dropped) so the genuine
    # recording isn't penalized a whole relevance band for an article mismatch:
    # normalize strips the leading "The" from "The Weeknd", so a query like
    # "blinding lights the weeknd" wouldn't fully match the artist's content
    # while a copycat whose bare title embeds the query would. Comparing content
    # tokens makes "<song> the <artist>" and "<song> <artist>" score alike; the
    # demotion tiebreak then keeps the genuine track above the copycat. Only
    # ever RAISES the score (it's a max), never lowers it.
    query_content = _content_tokens(query)
    if query_content:
        query_c = " ".join(sorted(query_content))
        title_c = " ".join(sorted(_content_tokens(title)))
        candidates.append(fuzz.token_sort_ratio(query_c, title_c))
        if result.subtitle:
            combined_c = " ".join(sorted(_content_tokens(combined)))
            candidates.append(fuzz.token_sort_ratio(query_c, combined_c))
    return float(max(candidates)) / 100.0


def _providers_of(result: SearchResult) -> set[ProviderName]:
    return {s.provider for s in result.sources}


@dataclass
class _Ranked:
    """A merged result tracking each contributing provider's best (lowest) rank."""

    result: SearchResult
    best_rank: dict[ProviderName, int]


@dataclass
class _Scored:
    """A finalized result with its query-relevance and RRF agreement scores."""

    result: SearchResult
    relevance: float
    rrf: float


def fuse_and_rank(
    per_provider: Sequence[Sequence[SearchResult]],
    query_norm: str,
) -> tuple[SearchResult, ...]:
    """Merge across providers and rank relevance-first.

    Each inner sequence is one provider's results in that provider's native
    relevance order (position 0 = best). A parameter-free **match gate**
    (`_passes_gate`) drops results sharing no content word with the query.
    Survivors sort by: relevance-band (0.1) → popularity → cross-provider
    agreement (RRF, `Σ 1/(_RRF_K + best_rank)` over *distinct* providers, so a
    provider returning the same item twice can't inflate it) → multi-source →
    prior → alpha.

    Confidence is computed (cross-provider corroboration; same-provider-only
    merge stays LOW) for telemetry only — it neither sorts nor displays.
    """
    # Pre-merge: capture the best album name per signature from raw provider
    # results. Lower-prior providers (Deezer 0.85, iTunes 0.85) carry the
    # primary commercial album name; MB (0.95) often returns a compilation.
    _album_best: dict[str, tuple[str, float]] = {}  # sig → (album, prior)
    for group in per_provider:
        for result in group:
            album = result.extras.get("album")
            if not isinstance(album, str) or not album:
                continue
            sig = _signature(result)
            prior = _winning_prior(result)
            prev = _album_best.get(sig)
            if prev is None or prior < prev[1]:
                _album_best[sig] = (album, prior)

    accumulated: list[_Ranked] = []
    for group in per_provider:
        for rank, incoming in enumerate(group):
            candidate = _as_low_confidence(incoming)
            cand_providers = _providers_of(candidate)
            for entry in accumulated:
                attempt = _try_merge(entry.result, candidate)
                if attempt is not None:
                    entry.result = attempt
                    for provider in cand_providers:
                        prev = entry.best_rank.get(provider)
                        entry.best_rank[provider] = rank if prev is None else min(prev, rank)
                    break
            else:
                accumulated.append(
                    _Ranked(result=candidate, best_rank=dict.fromkeys(cand_providers, rank))
                )

    # Post-merge: stabilize album names using the pre-merge lookup.
    for entry in accumulated:
        sig = _signature(entry.result)
        best = _album_best.get(sig)
        if best is not None and entry.result.extras.get("album") != best[0]:
            extras = {**entry.result.extras, "album": best[0]}
            entry.result = SearchResult(
                kind=entry.result.kind,
                title=entry.result.title,
                subtitle=entry.result.subtitle,
                image_url=entry.result.image_url,
                confidence=entry.result.confidence,
                sources=entry.result.sources,
                extras=extras,
            )

    scored: list[_Scored] = []
    for entry in accumulated:
        rrf = sum(1.0 / (_RRF_K + rank) for rank in entry.best_rank.values())
        result = entry.result
        if len(_providers_of(result)) < 2 and result.confidence is not Confidence.LOW:
            # Same-provider-only merge: demote to LOW (no cross-source agreement).
            result = _as_low_confidence(result)
        if query_norm.strip() and not _passes_gate(result, query_norm):
            # Shares no content word with the query — not a match. Parameter-free.
            continue
        rel = _relevance_score(result, query_norm)
        scored.append(_Scored(result=result, relevance=rel, rrf=rrf))

    query_tokens = _content_tokens(query_norm)
    split = _genuine_split_exists([s.result for s in scored], query_tokens)
    bootleg_ids = {id(s.result) for s in scored if _is_bootleg(s.result, query_tokens, split)}

    def _key(item: _Scored) -> tuple[float, int, int, int, float, float, float, str, str]:
        # Relevance (banded to 0.1) leads; within a band, multi-source
        # agreement outranks popularity so originals appearing in 3+ providers
        # beat single-source covers regardless of play counts.
        band = round(item.relevance, 1)
        demoted = 1 if _is_demoted(item.result) else 0
        bootleg = 1 if id(item.result) in bootleg_ids else 0
        multi_source = 1 if len(_providers_of(item.result)) > 1 else 0
        popularity = _popularity(item.result)
        prior = _winning_prior(item.result)
        return (
            -band,
            demoted,
            bootleg,
            -multi_source,
            -popularity,
            -item.rrf,
            -prior,
            item.result.subtitle or "",
            item.result.title,
        )

    return tuple(item.result for item in sorted(scored, key=_key))


def rerank(results: Sequence[SearchResult], query_norm: str) -> tuple[SearchResult, ...]:
    """Re-sort already-merged results after enrichment changed their popularity.

    Same ordering as `fuse_and_rank` minus the RRF term (provider-native ranks
    aren't retained post-merge): relevance-band → popularity → multi-source
    (agreement proxy) → prior → alpha. Results are assumed already gated; this
    only reorders.
    """

    query_tokens = _content_tokens(query_norm)
    split = _genuine_split_exists(results, query_tokens)
    bootleg_ids = {id(r) for r in results if _is_bootleg(r, query_tokens, split)}

    def _key(result: SearchResult) -> tuple[float, int, int, int, float, float, str, str]:
        band = round(_relevance_score(result, query_norm), 1)
        demoted = 1 if _is_demoted(result) else 0
        bootleg = 1 if id(result) in bootleg_ids else 0
        multi_source = 1 if len(_providers_of(result)) > 1 else 0
        popularity = _popularity(result)
        prior = _winning_prior(result)
        return (
            -band,
            demoted,
            bootleg,
            -multi_source,
            -popularity,
            -prior,
            result.subtitle or "",
            result.title,
        )

    return tuple(sorted(results, key=_key))
