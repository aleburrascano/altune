"""normalize_for_match — 8-step canonicalization for JW dedup.

Per discover-music-v1 spec §3.6. Applied to both the user query and to
each provider's result before JW similarity comparison. Pure function;
deterministic; never raises.
"""

from __future__ import annotations

import re
import unicodedata

_BRACKET_SUFFIX_RE = re.compile(r"\s*[\(\[\{][^\)\]\}]*[\)\]\}]")
_FEATURE_TOKEN_RE = re.compile(r"\b(feat\.?|ft\.?|featuring|with)\b", re.IGNORECASE)
_LEADING_ARTICLE_RE = re.compile(r"^\s*(the|los|les|el|la|le)\s+", re.IGNORECASE)
_PUNCT_RE = re.compile(r"[^\w\s]")
_WHITESPACE_RE = re.compile(r"\s+")
# AIDEV-NOTE: drop ASCII apostrophe, curly right-quote (U+2019, common from
# Apple Music / iTunes), periods, and commas without leaving a gap. Using
# explicit Unicode escape avoids RUF001 noise from a literal smart-quote.
_APOSTROPHE_TRANS = str.maketrans("", "", "'’.,")  # noqa: RUF001


def _strip_leading_article(text: str) -> str:
    # AIDEV-NOTE: only strip when the article is the leading word AND there
    # are at least 2 tokens after stripping. "the the" must stay because the
    # second word is the band name.
    stripped = _LEADING_ARTICLE_RE.sub("", text)
    if stripped.strip() and stripped != text:
        return stripped
    return text


def normalize_for_match(text: str) -> str:
    """Apply the 8-step canonicalization per discover-music-v1 spec §3.6."""
    # 1. NFKC normalization
    s = unicodedata.normalize("NFKC", text)
    # 2. Lowercase
    s = s.lower()
    # 3. Strip diacritics
    s = "".join(c for c in unicodedata.normalize("NFD", s) if not unicodedata.combining(c))
    # 5. Normalize feature notation BEFORE bracket-strip so feat artists
    # in bracketed clauses get unified. ("feat." inside parens then gets dropped.)
    s = _FEATURE_TOKEN_RE.sub("feat", s)
    # 4. Drop bracketed suffixes
    s = _BRACKET_SUFFIX_RE.sub(" ", s)
    # 6. Strip leading article on artist names
    s = _strip_leading_article(s)
    # 7. & -> and; strip apostrophes/periods/commas without leaving a gap;
    # drop other punctuation as space; collapse whitespace.
    s = s.replace("&", " and ")
    s = s.translate(_APOSTROPHE_TRANS)
    s = _PUNCT_RE.sub(" ", s)
    s = _WHITESPACE_RE.sub(" ", s)
    # 8. Trim
    return s.strip()
