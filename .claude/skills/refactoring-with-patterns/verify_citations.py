#!/usr/bin/env python3
"""Verify every file:line citation in an audit report resolves to a real line.

A finding whose citation doesn't resolve is a finding that was invented. This
catches that before the report is handed off.

    python3 scripts/verify_citations.py STRUCTURE-AUDIT.md
    python3 scripts/verify_citations.py STRUCTURE-AUDIT.md --root backend/

Exit 0 = clean. Exit 1 = problems listed on stdout, with enough detail to fix.
"""

import argparse
import html as htmllib
import pathlib
import re
import sys

# Matches `path/to/file.go:42` inside backticks. Backticks are required: prose
# like "see order.go:42" is ambiguous with sentence punctuation, and the report
# format specifies backticks.
CITATION = re.compile(r"`([\w./\-]+\.\w+):(\d+)(?:-(\d+))?`")

# Matches `lexicon:go/section/entry` inside backticks — a pattern-lexicon
# citation resolving to ~/.claude/lexicon/site/<path>/index.html.
LEXICON = re.compile(r"`lexicon:([\w./\-]+)`")

# Minimum length for a quoted cost fragment — filters code literals like "mbid".
MIN_QUOTE = 15

# A finding must cite something. These headers start findings.
FINDING_HEADER = re.compile(r"^###\s+F\d+\.", re.M)


def normalize(text: str) -> str:
    """Whitespace-collapsed, lowercased, typography-flattened for quote matching."""
    text = text.replace("’", "'").replace("‘", "'")
    text = text.replace("“", '"').replace("”", '"')
    text = text.replace("—", "-").replace("–", "-").replace(" ", " ")
    return re.sub(r"\s+", " ", text).strip().lower()


_ENTRY_CACHE: dict[str, str | None] = {}


def entry_text(lexicon_root: pathlib.Path, rel: str) -> str | None:
    """Tag-stripped, normalized text of one lexicon entry (None if missing)."""
    if rel not in _ENTRY_CACHE:
        target = lexicon_root / rel / "index.html"
        if not target.is_file():
            _ENTRY_CACHE[rel] = None
        else:
            raw = target.read_text(encoding="utf-8", errors="replace")
            _ENTRY_CACHE[rel] = normalize(re.sub(r"<[^>]+>", " ", htmllib.unescape(raw)))
    return _ENTRY_CACHE[rel]


def quote_verifies(block: str, entries: list[str], lexicon_root: pathlib.Path) -> bool:
    """True if at least one quoted span in block appears in a cited entry.

    Candidates are every segment between double quotes (parity-free: blocks
    dense with short code literals like "mbid" would misalign a paired-quote
    regex). A false candidate simply won't match entry text.
    """
    texts = [t for rel in entries if (t := entry_text(lexicon_root, rel))]
    if not texts:
        return False
    segments = normalize(block).split('"')[1:-1]
    for seg in segments:
        needle = seg.strip()
        if len(needle) >= MIN_QUOTE and any(needle in t for t in texts):
            return True
    return False


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("report")
    ap.add_argument("--root", default=".",
                    help="repo root that citation paths are relative to")
    ap.add_argument("--lexicon-root",
                    default=str(pathlib.Path.home() / ".claude" / "lexicon" / "site"),
                    help="lexicon site root that lexicon: paths resolve under")
    args = ap.parse_args()

    report_path = pathlib.Path(args.report)
    if not report_path.is_file():
        print(f"FAIL: report not found: {report_path}")
        return 1
    root = pathlib.Path(args.root)

    text = report_path.read_text(encoding="utf-8", errors="replace")
    problems = []

    # --- citations resolve -------------------------------------------------
    seen = 0
    for m in CITATION.finditer(text):
        seen += 1
        rel, start, end = m.group(1), int(m.group(2)), m.group(3)
        target = root / rel

        if not target.is_file():
            # Try to help rather than just failing: find same-named files.
            matches = list(root.rglob(pathlib.Path(rel).name))
            hint = ""
            if matches:
                opts = ", ".join(str(p.relative_to(root)) for p in matches[:3])
                hint = f" Did you mean: {opts}?"
            problems.append(f"no such file: {rel}{hint}")
            continue

        try:
            n_lines = sum(1 for _ in target.open(encoding="utf-8", errors="replace"))
        except OSError as e:
            problems.append(f"cannot read {rel}: {e}")
            continue

        last = int(end) if end else start
        if start < 1 or last > n_lines:
            problems.append(
                f"{rel}:{m.group(2)}{'-' + end if end else ''} out of range "
                f"({rel} has {n_lines} lines)")

    lexicon_root = pathlib.Path(args.lexicon_root)

    # --- lexicon citations resolve ------------------------------------------
    lex_seen = 0
    for m in LEXICON.finditer(text):
        lex_seen += 1
        rel = m.group(1)
        if entry_text(lexicon_root, rel) is None:
            problems.append(
                f"lexicon entry not found: {rel} "
                f"(expected {lexicon_root / rel / 'index.html'}). "
                f"Paths come from the manifest — don't guess them.")

    # --- every finding cites code AND is lexicon-reconciled -----------------
    sections = re.split(FINDING_HEADER, text)
    headers = FINDING_HEADER.findall(text)
    for header, body in zip(headers, sections[1:]):
        title = body.strip().split("\n")[0][:60]
        if not CITATION.search(body):
            problems.append(
                f"{header.strip()} {title} — no `file:line` citation. "
                f"Findings need evidence.")
        entries = LEXICON.findall(body)
        if not entries:
            problems.append(
                f"{header.strip()} {title} — no `lexicon:` citation. Every "
                f"finding names its pattern or the closest entry it beats "
                f"(SKILL step 6).")
        elif not quote_verifies(body, entries, lexicon_root):
            problems.append(
                f"{header.strip()} {title} — no quoted cost text found in its "
                f"cited lexicon entr{'y' if len(entries) == 1 else 'ies'}. "
                f"Quote the entry's cost/avoid-when line verbatim (15+ chars, "
                f"in double quotes) — paraphrase and memory don't verify.")

    # --- the rejected section exists, is non-empty, and is reconciled -------
    rej = re.search(r"^##\s+Considered and rejected\s*$(.*?)(?=^##\s|\Z)",
                    text, re.M | re.S)
    if not rej:
        problems.append("missing '## Considered and rejected' section — required.")
    elif not re.search(r"^\s*[-*]\s+\S", rej.group(1), re.M):
        problems.append(
            "'Considered and rejected' is empty. An audit that rejected nothing "
            "did not discriminate; it collected.")
    else:
        bullets = re.split(r"^-\s+", rej.group(1), flags=re.M)[1:]
        for bullet in bullets:
            label = re.sub(r"\s+", " ", bullet)[:60]
            entries = LEXICON.findall(bullet)
            if not entries:
                if "no manifest entry" not in bullet.lower():
                    problems.append(
                        f"rejected: '{label}' — cite the entry it rejects "
                        f"(`lexicon:…`) or state \"no manifest entry\".")
            elif not quote_verifies(bullet, entries, lexicon_root):
                problems.append(
                    f"rejected: '{label}' — cites an entry but no quoted cost "
                    f"text from it. The cost line is the rejection; quote it.")

    # --- finding cap -------------------------------------------------------
    n_findings = len(headers)
    if n_findings > 10:
        problems.append(
            f"{n_findings} detailed findings, cap is 10. Rank by pain and move the "
            f"tail to '## Deferred' as one-liners — don't delete them.")

    # --- boundary section --------------------------------------------------
    if not re.search(r"^##\s+Boundary\s*$", text, re.M):
        problems.append(
            "missing '## Boundary' section — required. State where the feature "
            "lives and whether it could be lifted out.")

    if problems:
        print(f"FAIL: {len(problems)} problem(s)\n")
        for p in problems:
            print(f"  - {p}")
        print(f"\nChecked {seen} file citation(s) + {lex_seen} lexicon citation(s) "
              f"across {n_findings} finding(s).")
        return 1

    print(f"OK: {seen} file citation(s) + {lex_seen} lexicon citation(s) resolve "
          f"across {n_findings} finding(s).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
