"""Golden ranking dataset for the discovery eval harness.

Each :class:`GoldenCase` carries the per-provider, provider-native-ranked
hits for one realistic query plus the canonical answer the user expects at
the top. The cases are hand-built to mirror the real failure mode the user
reported: the obviously-correct match returned by a provider at its own
rank 0 gets buried beneath agreed-upon-but-less-relevant entries, because
the legacy ranker sorts on provider-agreement + alphabetical, never on
relevance to the query.

These are fixtures, not provider captures — small, deterministic, and
focused on ranking behavior. They are the regression guard for the
RRF + exact-match-boost ranking model.
"""

from __future__ import annotations

from dataclasses import dataclass

from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef


@dataclass(frozen=True)
class ProviderHit:
    """One provider's hit in its native relevance order."""

    title: str
    subtitle: str
    isrc: str | None = None


@dataclass(frozen=True)
class GoldenCase:
    """A query, the per-provider ranked hits, and the expected top answer.

    `expected_title is None` marks an **unsatisfiable** query (no real canonical
    in the catalog) — scored not by MRR but by the no-junk invariant: every
    `forbidden` (title, subtitle) pair must be excluded from the response by the
    relevance floor.
    """

    query: str
    providers: dict[ProviderName, list[ProviderHit]]
    expected_title: str | None
    expected_subtitle: str | None
    note: str = ""
    forbidden: tuple[tuple[str, str], ...] = ()

    def provider_groups(self) -> list[tuple[SearchResult, ...]]:
        """Build per-provider tuples of SearchResults, preserving native order."""
        groups: list[tuple[SearchResult, ...]] = []
        n = 0
        for provider, hits in self.providers.items():
            group: list[SearchResult] = []
            for hit in hits:
                n += 1
                extras: dict[str, object] = {}
                if hit.isrc is not None:
                    extras["isrc"] = hit.isrc
                group.append(
                    SearchResult(
                        kind=ResultKind.TRACK,
                        title=hit.title,
                        subtitle=hit.subtitle,
                        image_url=None,
                        confidence=Confidence.LOW,
                        sources=(
                            SourceRef(
                                provider=provider,
                                external_id=f"{provider.value}-{n}",
                                url=f"https://{provider.value}/{n}",
                            ),
                        ),
                        extras=extras,
                    )
                )
            groups.append(tuple(group))
        return groups


# AIDEV-NOTE: Golden cases are the contract for ranking quality. Each "burial"
# case reproduces the user-reported symptom under the legacy ranker and must
# resolve to rank 0 under RRF + exact-match boost. Don't weaken these without
# re-running scripts/ranking_eval.py against live providers.
GOLDEN_CASES: tuple[GoldenCase, ...] = (
    GoldenCase(
        query="africa toto",
        note="5 same-title 'Africa' artists all multi-source; Toto loses the "
        "alphabetical tiebreak under the legacy ranker (last by subtitle).",
        expected_title="Africa",
        expected_subtitle="TOTO",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Africa", "TOTO"),
                ProviderHit("Africa", "Angelique Kidjo"),
                ProviderHit("Africa", "D'Angelo"),
                ProviderHit("Africa", "Karol G"),
                ProviderHit("Africa", "Peter Tosh"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Africa", "Angelique Kidjo"),
                ProviderHit("Africa", "D'Angelo"),
                ProviderHit("Africa", "Karol G"),
                ProviderHit("Africa", "Peter Tosh"),
                ProviderHit("Africa", "TOTO"),
            ],
        },
    ),
    GoldenCase(
        query="creep radiohead",
        note="Canonical is single-source (LOW) from Deezer; a 'Creep' by TLC "
        "is multi-source HIGH and outranks it under the legacy ranker.",
        expected_title="Creep",
        expected_subtitle="Radiohead",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Creep", "Radiohead", isrc="GBAYE9200001"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Creep", "TLC"),
                ProviderHit("Creep (Live)", "Postmodern Jukebox"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Creep", "TLC"),
            ],
        },
    ),
    GoldenCase(
        query="shallow lady gaga",
        note="Canonical single-source LOW; a karaoke 'Shallow' is multi-source "
        "HIGH with the highest prior (MB) and sorts first under the legacy ranker.",
        expected_title="Shallow",
        expected_subtitle="Lady Gaga",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Shallow", "Lady Gaga"),
                ProviderHit("Shallow", "Kids Karaoke"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Shallow", "Kids Karaoke"),
            ],
        },
    ),
    GoldenCase(
        query="bohemian rhapsody queen",
        note="Clean case: every provider agrees, only one result — already top "
        "under both rankers. Guards against regressions on the easy path.",
        expected_title="Bohemian Rhapsody",
        expected_subtitle="Queen",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Bohemian Rhapsody", "Queen", isrc="GBUM71029604"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Bohemian Rhapsody", "Queen"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Bohemian Rhapsody", "Queen"),
            ],
        },
    ),
    GoldenCase(
        query="wonderwall oasis",
        note="Multi-source agreement coincides with relevance — should be top under both rankers.",
        expected_title="Wonderwall",
        expected_subtitle="Oasis",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Wonderwall", "Oasis", isrc="GBARL9500098"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Wonderwall", "Oasis"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Wonderwall", "Oasis"),
            ],
        },
    ),
    GoldenCase(
        query="despacito luis fonsi",
        note="Bracketed (feat. ...) suffix normalizes to the canonical; a remix "
        "cover stays separate. Canonical should be top.",
        expected_title="Despacito (feat. Daddy Yankee)",
        expected_subtitle="Luis Fonsi",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Despacito (feat. Daddy Yankee)", "Luis Fonsi", isrc="USUM71700001"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Despacito", "Bieber Remix Cover"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Despacito", "Luis Fonsi"),
            ],
        },
    ),
    # --- Beyond the happy path: partial / typo / artist-only / nonsense ---
    GoldenCase(
        query="wonderwall",
        note="Partial (title-only) query. The canonical must still top a "
        "differently-titled decoy by the same kind of artist.",
        expected_title="Wonderwall",
        expected_subtitle="Oasis",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Champagne Supernova", "Oasis"),
                ProviderHit("Wonderwall", "Oasis"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Wonderwall", "Oasis"),
            ],
        },
    ),
    GoldenCase(
        query="bohemian rapsody",
        note="Misspelled title ('rapsody'). Graded similarity must still rank "
        "the correct track first over an unrelated track by the same artist.",
        expected_title="Bohemian Rhapsody",
        expected_subtitle="Queen",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Radio Ga Ga", "Queen"),
                ProviderHit("Bohemian Rhapsody", "Queen"),
            ],
            ProviderName.ITUNES: [
                ProviderHit("Bohemian Rhapsody", "Queen"),
            ],
        },
    ),
    GoldenCase(
        query="queen",
        note="Artist-only query. A track by the artist must clear the floor and "
        "rank first (the floor must not drop the whole artist).",
        expected_title="Bohemian Rhapsody",
        expected_subtitle="Queen",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Bohemian Rhapsody", "Queen"),
                ProviderHit("Radio Ga Ga", "Queen"),
            ],
            ProviderName.ITUNES: [
                ProviderHit("Bohemian Rhapsody", "Queen"),
            ],
        },
    ),
    GoldenCase(
        query="che rest in bass",
        note="Unsatisfiable/nonsense query. No real canonical; the relevance "
        "floor must exclude the zero-relevance junk (Under Pressure) and the "
        "same-provider-duplicate inflation case, surfacing only partial matches.",
        expected_title=None,
        expected_subtitle=None,
        forbidden=(("Under Pressure", "Queen & David Bowie"),),
        providers={
            ProviderName.DEEZER: [
                ProviderHit("BA$$", "che"),
                ProviderHit("BA$$", "che"),
            ],
            ProviderName.ITUNES: [
                ProviderHit("Under Pressure", "Queen & David Bowie"),
                ProviderHit("Under Pressure", "Queen & David Bowie"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Rest in the Bass", "FARNOISE"),
            ],
        },
    ),
)
