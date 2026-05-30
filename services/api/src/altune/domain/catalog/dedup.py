"""dedup_key — the canonical 'same track' natural key.

A pure normalizer used by every TrackRepository implementation to decide
whether two saves refer to the same track. Backed by the
UNIQUE(user_id, dedup_key) constraint, it makes Save idempotent (spec AC#7)
without an idempotency-key store.

Normalization: case-fold, trim, collapse internal whitespace, join with a
separator that cannot appear after collapsing. A null album folds to "".
"""

from __future__ import annotations

_SEP = "\x1f"  # unit separator — never present in normalized text


def _norm(value: str) -> str:
    return " ".join(value.split()).casefold()


def dedup_key(title: str, artist: str, album: str | None) -> str:
    return _SEP.join((_norm(title), _norm(artist), _norm(album or "")))
