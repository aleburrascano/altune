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
    # --- More popular / happy-path cases ---
    GoldenCase(
        query="smells like teen spirit nirvana",
        note="Multi-source agreement + exact artist match.",
        expected_title="Smells Like Teen Spirit",
        expected_subtitle="Nirvana",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Smells Like Teen Spirit", "Nirvana", isrc="USGF19942501"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Smells Like Teen Spirit", "Nirvana"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Smells Like Teen Spirit", "Nirvana"),
            ],
        },
    ),
    GoldenCase(
        query="shape of you ed sheeran",
        note="Canonical must beat a cover/karaoke version.",
        expected_title="Shape of You",
        expected_subtitle="Ed Sheeran",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Shape of You", "Ed Sheeran", isrc="GBAHS1600463"),
                ProviderHit("Shape of You (Karaoke)", "Sing2Guitar"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Shape of You", "Ed Sheeran"),
            ],
        },
    ),
    GoldenCase(
        query="blinding lights the weeknd",
        note="Canonical must beat unrelated noise.",
        expected_title="Blinding Lights",
        expected_subtitle="The Weeknd",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Blinding Lights", "The Weeknd", isrc="USUG11904181"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Blinding Lights", "The Weeknd"),
                ProviderHit("Blinding Lights (Remix)", "DJ Snake"),
            ],
        },
    ),
    GoldenCase(
        query="lose yourself eminem",
        note="Clear canonical, multi-source.",
        expected_title="Lose Yourself",
        expected_subtitle="Eminem",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Lose Yourself", "Eminem"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Lose Yourself", "Eminem"),
            ],
        },
    ),
    GoldenCase(
        query="hey jude beatles",
        note="Leading article normalization; 'The Beatles' must match 'beatles'.",
        expected_title="Hey Jude",
        expected_subtitle="The Beatles",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Hey Jude", "The Beatles"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Hey Jude", "The Beatles"),
            ],
        },
    ),
    GoldenCase(
        query="hotel california eagles",
        note="Multi-source clear canonical.",
        expected_title="Hotel California",
        expected_subtitle="Eagles",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Hotel California", "Eagles"),
                ProviderHit("Hotel California (Acoustic)", "Eagles Tribute"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Hotel California", "Eagles"),
            ],
        },
    ),
    GoldenCase(
        query="thriller michael jackson",
        note="Iconic track, should be trivial.",
        expected_title="Thriller",
        expected_subtitle="Michael Jackson",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Thriller", "Michael Jackson"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Thriller", "Michael Jackson"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Thriller", "Michael Jackson"),
            ],
        },
    ),
    GoldenCase(
        query="stairway to heaven led zeppelin",
        note="Multi-source, clear canonical.",
        expected_title="Stairway to Heaven",
        expected_subtitle="Led Zeppelin",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Stairway to Heaven", "Led Zeppelin"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Stairway to Heaven", "Led Zeppelin"),
            ],
        },
    ),
    # --- Ambiguous / short-name queries ---
    GoldenCase(
        query="sia",
        note="Short artist name. Track by Sia must rank above unrelated.",
        expected_title="Chandelier",
        expected_subtitle="Sia",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Chandelier", "Sia"),
                ProviderHit("Sia Diffusion", "DJ SIA"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Chandelier", "Sia"),
            ],
        },
    ),
    GoldenCase(
        query="seal",
        note="Short artist name, common word.",
        expected_title="Kiss from a Rose",
        expected_subtitle="Seal",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Kiss from a Rose", "Seal"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Kiss from a Rose", "Seal"),
            ],
        },
    ),
    GoldenCase(
        query="mika",
        note="Short artist name.",
        expected_title="Grace Kelly",
        expected_subtitle="MIKA",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Grace Kelly", "MIKA"),
                ProviderHit("Mika Nakashima", "MIKA NAKASHIMA"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Grace Kelly", "MIKA"),
            ],
        },
    ),
    GoldenCase(
        query="banks",
        note="Short artist name that's also a common word.",
        expected_title="Beggin for Thread",
        expected_subtitle="BANKS",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Beggin for Thread", "BANKS"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Beggin for Thread", "BANKS"),
            ],
        },
    ),
    GoldenCase(
        query="low",
        note="Short name — could be band Low or Flo Rida song.",
        expected_title="Low",
        expected_subtitle="Flo Rida",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Low", "Flo Rida"),
                ProviderHit("Lullaby", "Low"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Low", "Flo Rida"),
            ],
        },
    ),
    GoldenCase(
        query="bush",
        note="Band name that's a common word.",
        expected_title="Glycerine",
        expected_subtitle="Bush",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Glycerine", "Bush"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Glycerine", "Bush"),
                ProviderHit("Kate Bush Running", "Kate Bush"),
            ],
        },
    ),
    GoldenCase(
        query="hurt",
        note="Both Johnny Cash and Nine Inch Nails have 'Hurt'. JW-same title "
        "but different artists — relevance should favour exact title match.",
        expected_title="Hurt",
        expected_subtitle="Johnny Cash",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Hurt", "Johnny Cash"),
                ProviderHit("Hurt", "Nine Inch Nails"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Hurt", "Johnny Cash"),
                ProviderHit("Hurt", "Nine Inch Nails"),
            ],
        },
    ),
    GoldenCase(
        query="closer",
        note="Extremely common track title: Chainsmokers, NIN, Joy Division.",
        expected_title="Closer",
        expected_subtitle="The Chainsmokers",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Closer", "The Chainsmokers"),
                ProviderHit("Closer", "Nine Inch Nails"),
                ProviderHit("Closer", "Joy Division"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Closer", "The Chainsmokers"),
            ],
        },
    ),
    GoldenCase(
        query="stay",
        note="Very common title: Rihanna, Post Malone, Lisa Loeb.",
        expected_title="Stay",
        expected_subtitle="Rihanna",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Stay", "Rihanna"),
                ProviderHit("Stay", "Post Malone"),
                ProviderHit("Stay (I Missed You)", "Lisa Loeb"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Stay", "Rihanna"),
            ],
        },
    ),
    GoldenCase(
        query="happy",
        note="Common word as title. Pharrell's version is the canonical.",
        expected_title="Happy",
        expected_subtitle="Pharrell Williams",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Happy", "Pharrell Williams"),
                ProviderHit("Happy Together", "The Turtles"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Happy", "Pharrell Williams"),
            ],
        },
    ),
    # --- Partial / misspelled queries ---
    GoldenCase(
        query="playboi cart",
        note="Misspelled artist ('cart' → 'Carti'). Fuzzy match must still resolve.",
        expected_title="Magnolia",
        expected_subtitle="Playboi Carti",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Magnolia", "Playboi Carti"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Magnolia", "Playboi Carti"),
            ],
        },
    ),
    GoldenCase(
        query="daft pnk",
        note="Misspelled artist ('pnk' → 'Punk'). Fuzzy match.",
        expected_title="Get Lucky",
        expected_subtitle="Daft Punk",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Get Lucky", "Daft Punk"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Get Lucky", "Daft Punk"),
            ],
        },
    ),
    GoldenCase(
        query="nirvna",
        note="Misspelled artist. Fuzzy match must resolve.",
        expected_title="Smells Like Teen Spirit",
        expected_subtitle="Nirvana",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Smells Like Teen Spirit", "Nirvana"),
            ],
            ProviderName.ITUNES: [
                ProviderHit("Smells Like Teen Spirit", "Nirvana"),
            ],
        },
    ),
    GoldenCase(
        query="radiohed",
        note="Misspelled artist. Fuzzy match must resolve.",
        expected_title="Creep",
        expected_subtitle="Radiohead",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Creep", "Radiohead"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Creep", "Radiohead"),
            ],
        },
    ),
    GoldenCase(
        query="billie",
        note="Partial artist name. Should resolve to Billie Eilish as most popular.",
        expected_title="bad guy",
        expected_subtitle="Billie Eilish",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("bad guy", "Billie Eilish"),
                ProviderHit("Billie Jean", "Michael Jackson"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("bad guy", "Billie Eilish"),
            ],
        },
    ),
    GoldenCase(
        query="kendrick",
        note="Partial artist name (Kendrick Lamar).",
        expected_title="HUMBLE.",
        expected_subtitle="Kendrick Lamar",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("HUMBLE.", "Kendrick Lamar"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("HUMBLE.", "Kendrick Lamar"),
            ],
        },
    ),
    GoldenCase(
        query="arctic",
        note="Partial band name (Arctic Monkeys).",
        expected_title="Do I Wanna Know?",
        expected_subtitle="Arctic Monkeys",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Do I Wanna Know?", "Arctic Monkeys"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Do I Wanna Know?", "Arctic Monkeys"),
            ],
        },
    ),
    GoldenCase(
        query="imagine",
        note="Could be John Lennon 'Imagine' or Imagine Dragons. Title match "
        "should rank the exact-title hit highest.",
        expected_title="Imagine",
        expected_subtitle="John Lennon",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Imagine", "John Lennon"),
                ProviderHit("Radioactive", "Imagine Dragons"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Imagine", "John Lennon"),
            ],
        },
    ),
    GoldenCase(
        query="super shy",
        note="Partial title; should resolve to NewJeans.",
        expected_title="Super Shy",
        expected_subtitle="NewJeans",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Super Shy", "NewJeans"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Super Shy", "NewJeans"),
            ],
        },
    ),
    # --- Nonsense / unsatisfiable queries ---
    GoldenCase(
        query="asdfghjkl123",
        note="Keyboard mash. No canonical. Relevance floor must exclude everything.",
        expected_title=None,
        expected_subtitle=None,
        forbidden=(),
        providers={
            ProviderName.DEEZER: [
                ProviderHit("A Song", "Random Artist"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Different Song", "Other Artist"),
            ],
        },
    ),
    GoldenCase(
        query="xyzzy12345",
        note="Random string. No canonical.",
        expected_title=None,
        expected_subtitle=None,
        forbidden=(),
        providers={
            ProviderName.DEEZER: [
                ProviderHit("XYZ", "Alphabet Band"),
            ],
        },
    ),
    GoldenCase(
        query="qqqqwwww",
        note="Repetitive keys. No canonical.",
        expected_title=None,
        expected_subtitle=None,
        forbidden=(),
        providers={
            ProviderName.DEEZER: [
                ProviderHit("QQ", "Some DJ"),
            ],
        },
    ),
    GoldenCase(
        query="aaa bbb ccc ddd",
        note="Nonsense multi-word. No canonical.",
        expected_title=None,
        expected_subtitle=None,
        forbidden=(),
        providers={
            ProviderName.DEEZER: [
                ProviderHit("ABC", "Jackson 5"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("ABC", "Jackson 5"),
            ],
        },
    ),
    GoldenCase(
        query="zzznotatrack ever",
        note="Gibberish. No canonical.",
        expected_title=None,
        expected_subtitle=None,
        forbidden=(),
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Never Gonna Give You Up", "Rick Astley"),
            ],
        },
    ),
    GoldenCase(
        query="jkdshfkjdshf music",
        note="Gibberish with 'music' appended. No canonical.",
        expected_title=None,
        expected_subtitle=None,
        forbidden=(),
        providers={
            ProviderName.LASTFM: [
                ProviderHit("Music", "Madonna"),
            ],
        },
    ),
    # --- Cover / karaoke / tribute traps ---
    GoldenCase(
        query="someone like you adele",
        note="Genuine must rank above karaoke version.",
        expected_title="Someone Like You",
        expected_subtitle="Adele",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Someone Like You", "Adele"),
                ProviderHit("Someone Like You (Karaoke)", "Karaoke Kings"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Someone Like You", "Adele"),
            ],
        },
    ),
    GoldenCase(
        query="hallelujah leonard cohen",
        note="Many artists cover this. Genuine by Cohen must be top.",
        expected_title="Hallelujah",
        expected_subtitle="Leonard Cohen",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Hallelujah", "Leonard Cohen"),
                ProviderHit("Hallelujah", "Jeff Buckley"),
                ProviderHit("Hallelujah", "Pentatonix"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Hallelujah", "Leonard Cohen"),
                ProviderHit("Hallelujah", "Jeff Buckley"),
            ],
        },
    ),
    GoldenCase(
        query="yesterday beatles",
        note="Genuine must beat instrumental/cover.",
        expected_title="Yesterday",
        expected_subtitle="The Beatles",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Yesterday", "The Beatles"),
                ProviderHit("Yesterday (Instrumental)", "Piano Covers"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Yesterday", "The Beatles"),
            ],
        },
    ),
    GoldenCase(
        query="rolling in the deep",
        note="Adele's genuine must beat covers.",
        expected_title="Rolling in the Deep",
        expected_subtitle="Adele",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Rolling in the Deep", "Adele"),
                ProviderHit("Rolling in the Deep (Cover)", "Aretha Franklin"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Rolling in the Deep", "Adele"),
            ],
        },
    ),
    GoldenCase(
        query="let it be",
        note="Beatles genuine above covers. Title-only query.",
        expected_title="Let It Be",
        expected_subtitle="The Beatles",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Let It Be", "The Beatles"),
                ProviderHit("Let It Be (Tribute)", "Studio Band"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Let It Be", "The Beatles"),
            ],
        },
    ),
    GoldenCase(
        query="fix you coldplay",
        note="Genuine must beat karaoke version.",
        expected_title="Fix You",
        expected_subtitle="Coldplay",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Fix You", "Coldplay"),
                ProviderHit("Fix You (Made Famous By Coldplay)", "Karaoke Star"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Fix You", "Coldplay"),
            ],
        },
    ),
    GoldenCase(
        query="imagine john lennon",
        note="Genuine must beat tribute versions.",
        expected_title="Imagine",
        expected_subtitle="John Lennon",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Imagine", "John Lennon"),
                ProviderHit("Imagine (Originally Performed By John Lennon)", "Cover Guru"),
            ],
            ProviderName.MUSICBRAINZ: [
                ProviderHit("Imagine", "John Lennon"),
            ],
        },
    ),
    GoldenCase(
        query="dance monkey",
        note="Genuine by Tones and I must beat covers.",
        expected_title="Dance Monkey",
        expected_subtitle="Tones and I",
        providers={
            ProviderName.DEEZER: [
                ProviderHit("Dance Monkey", "Tones and I"),
                ProviderHit("Dance Monkey (8 Bit)", "8 Bit Universe"),
            ],
            ProviderName.LASTFM: [
                ProviderHit("Dance Monkey", "Tones and I"),
            ],
        },
    ),
)
