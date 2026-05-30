"""Query corpus generation for the discovery eval harness.

Pure functions, no I/O (except `load_library`, which reads the committed
snapshot). From each labeled track we emit several realistic query variants,
each tagged with the category and the expected (kind, title, subtitle) the
search should surface. The user can type anything in the search bar, so we
stress the messy shapes: lowercased/punctuation-stripped, partial titles,
single-character typos, bare artist, album queries.

`expected_title` / `expected_subtitle` map directly onto the scorer's
normalized (title, subtitle) match — for an artist query the artist name is
the title and the subtitle is None; for an album query the album is the title
and the artist is the subtitle.
"""

from __future__ import annotations

import json
import random
import re
from dataclasses import dataclass
from itertools import zip_longest
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from pathlib import Path

# Mirror production normalize_for_match: apostrophes/periods/commas are
# stripped (M.A.A.D -> MAAD, don't -> dont), other punctuation becomes a space.
_STRIP = re.compile(r"['.,]", flags=re.UNICODE)
_PUNCT = re.compile(r"[^\w\s]", flags=re.UNICODE)
_WS = re.compile(r"\s+")

# Upper bound on what variants_for can emit (5 base + album + partial); used to
# bound the sampled track count so a small cap still spans every category.
_MAX_VARIANTS_PER_TRACK = 7


@dataclass(frozen=True, slots=True)
class LibraryTrack:
    """One labeled track from the ground-truth library."""

    title: str
    artist: str
    album: str | None = None
    album_artist: str | None = None
    year: int | None = None
    genre: str | None = None


@dataclass(frozen=True, slots=True)
class EvalQuery:
    """One query to run, with the answer the search is expected to surface."""

    query: str
    category: str
    expected_kind: str  # "track" | "artist" | "album"
    expected_title: str
    expected_subtitle: str | None
    source: str  # "library" | "mainstream"


def messy(text: str) -> str:
    """Lowercase, drop punctuation, collapse whitespace — a sloppy typed query."""
    return _WS.sub(" ", _PUNCT.sub(" ", _STRIP.sub("", text)).lower()).strip()


def typo(text: str) -> str:
    """Deterministic single-character transposition (no RNG seed drift).

    Swaps the two characters straddling the midpoint, so 'radiohead' becomes a
    near-miss of equal length. Returns the input unchanged when too short.
    """
    if len(text) < 4:
        return text
    mid = len(text) // 2
    chars = list(text)
    chars[mid - 1], chars[mid] = chars[mid], chars[mid - 1]
    return "".join(chars)


def _partial_title(title: str, n: int = 2) -> str | None:
    """First n words of a title, or None if the title is too short to shorten."""
    words = title.split()
    if len(words) <= n:
        return None
    return " ".join(words[:n])


def variants_for(track: LibraryTrack, source: str) -> list[EvalQuery]:
    """All query variants for one labeled track."""
    t, a = track.title, track.artist
    out: list[EvalQuery] = [
        EvalQuery(f"{t} {a}", "track_exact", "track", t, a, source),
        EvalQuery(t, "track_only", "track", t, a, source),
        EvalQuery(a, "artist_only", "artist", a, None, source),
        EvalQuery(messy(f"{t} {a}"), "messy_case", "track", t, a, source),
        EvalQuery(f"{typo(t)} {a}", "typo", "track", t, a, source),
    ]
    if track.album:
        out.append(EvalQuery(f"{track.album} {a}", "album", "album", track.album, a, source))
    partial = _partial_title(t)
    if partial is not None:
        out.append(EvalQuery(f"{partial} {a}", "partial", "track", t, a, source))
    return out


def _stratify(tracks: list[LibraryTrack], seed: int) -> list[LibraryTrack]:
    """Order tracks to diversify artists: one per artist round-robin, seeded."""
    rng = random.Random(seed)  # noqa: S311  -- sampling for a dev eval tool, not crypto
    by_artist: dict[str, list[LibraryTrack]] = {}
    for tr in tracks:
        by_artist.setdefault(tr.artist.lower(), []).append(tr)
    buckets = list(by_artist.values())
    rng.shuffle(buckets)
    ordered: list[LibraryTrack] = []
    for group in zip_longest(*buckets):
        ordered.extend(tr for tr in group if tr is not None)
    return ordered


def build_corpus(
    tracks: list[LibraryTrack],
    *,
    max_queries: int,
    seed: int,
    source: str = "library",
) -> list[EvalQuery]:
    """Stratified, deterministic, capped corpus.

    Diversifies by artist, then interleaves categories (one variant per track
    in round-robin) so a small cap still spans every category rather than
    exhausting all variants of the first few tracks.

    The track count is bounded to ~`max_queries / variants-per-track` BEFORE
    interleaving: otherwise the first column (track_exact, one per track) alone
    fills the cap when there are more tracks than `max_queries`, and no other
    category ever appears.
    """
    ordered = _stratify(tracks, seed)
    n_tracks = max(1, -(-max_queries // _MAX_VARIANTS_PER_TRACK))  # ceil division
    per_track = [variants_for(tr, source) for tr in ordered[:n_tracks]]
    interleaved: list[EvalQuery] = [
        q for column in zip_longest(*per_track) for q in column if q is not None
    ]
    return interleaved[:max_queries]


def load_library(path: Path) -> list[LibraryTrack]:
    """Load the committed library snapshot."""
    rows = json.loads(path.read_text(encoding="utf-8"))
    return [
        LibraryTrack(
            title=r["title"],
            artist=r["artist"],
            album=r.get("album"),
            album_artist=r.get("album_artist"),
            year=r.get("year"),
            genre=r.get("genre"),
        )
        for r in rows
    ]


# A small hand-built mainstream set across genres — the well-covered catalog
# path (the kind of query Spotify nails). Albums included so the album variant
# fires. These are deliberately canonical spellings.
MAINSTREAM: list[LibraryTrack] = [
    LibraryTrack("Bohemian Rhapsody", "Queen", "A Night at the Opera", genre="Rock"),
    LibraryTrack("Billie Jean", "Michael Jackson", "Thriller", genre="Pop"),
    LibraryTrack("Smells Like Teen Spirit", "Nirvana", "Nevermind", genre="Rock"),
    LibraryTrack("Rolling in the Deep", "Adele", "21", genre="Pop"),
    LibraryTrack("Hotel California", "Eagles", "Hotel California", genre="Rock"),
    LibraryTrack("Lose Yourself", "Eminem", "8 Mile", genre="Hip Hop"),
    LibraryTrack(
        "Bad Guy", "Billie Eilish", "When We All Fall Asleep, Where Do We Go?", genre="Pop"
    ),
    LibraryTrack("Blinding Lights", "The Weeknd", "After Hours", genre="Pop"),
    LibraryTrack("Shape of You", "Ed Sheeran", "Divide", genre="Pop"),
    LibraryTrack("Uptown Funk", "Mark Ronson", "Uptown Special", genre="Funk"),
    LibraryTrack("HUMBLE.", "Kendrick Lamar", "DAMN.", genre="Hip Hop"),
    LibraryTrack("One Dance", "Drake", "Views", genre="Hip Hop"),
    LibraryTrack("Get Lucky", "Daft Punk", "Random Access Memories", genre="Electronic"),
    LibraryTrack("Take Five", "The Dave Brubeck Quartet", "Time Out", genre="Jazz"),
    LibraryTrack("No Woman No Cry", "Bob Marley & The Wailers", "Natty Dread", genre="Reggae"),
    LibraryTrack("Wonderwall", "Oasis", "(What's the Story) Morning Glory?", genre="Rock"),
    LibraryTrack("Seven Nation Army", "The White Stripes", "Elephant", genre="Rock"),
    LibraryTrack("Hey Ya!", "OutKast", "Speakerboxxx/The Love Below", genre="Hip Hop"),
    LibraryTrack("Levitating", "Dua Lipa", "Future Nostalgia", genre="Pop"),
    LibraryTrack("Mr. Brightside", "The Killers", "Hot Fuss", genre="Rock"),
]
