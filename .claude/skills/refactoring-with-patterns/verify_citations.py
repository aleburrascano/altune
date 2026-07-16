#!/usr/bin/env python3
"""Verify every file:line citation in an audit report resolves to a real line.

A finding whose citation doesn't resolve is a finding that was invented. This
catches that before the report is handed off.

    python3 scripts/verify_citations.py STRUCTURE-AUDIT.md
    python3 scripts/verify_citations.py STRUCTURE-AUDIT.md --root backend/

Exit 0 = clean. Exit 1 = problems listed on stdout, with enough detail to fix.
"""

import argparse
import pathlib
import re
import sys

# Matches `path/to/file.go:42` inside backticks. Backticks are required: prose
# like "see order.go:42" is ambiguous with sentence punctuation, and the report
# format specifies backticks.
CITATION = re.compile(r"`([\w./\-]+\.\w+):(\d+)(?:-(\d+))?`")

# A finding must cite something. These headers start findings.
FINDING_HEADER = re.compile(r"^###\s+F\d+\.", re.M)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("report")
    ap.add_argument("--root", default=".",
                    help="repo root that citation paths are relative to")
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

    # --- every finding cites something -------------------------------------
    sections = re.split(FINDING_HEADER, text)
    headers = FINDING_HEADER.findall(text)
    for header, body in zip(headers, sections[1:]):
        if not CITATION.search(body):
            title = body.strip().split("\n")[0][:60]
            problems.append(
                f"{header.strip()} {title} — no `file:line` citation. "
                f"Findings need evidence.")

    # --- the rejected section exists and is non-empty ----------------------
    rej = re.search(r"^##\s+Considered and rejected\s*$(.*?)(?=^##\s|\Z)",
                    text, re.M | re.S)
    if not rej:
        problems.append("missing '## Considered and rejected' section — required.")
    elif not re.search(r"^\s*[-*]\s+\S", rej.group(1), re.M):
        problems.append(
            "'Considered and rejected' is empty. An audit that rejected nothing "
            "did not discriminate; it collected.")

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
        print(f"\nChecked {seen} citation(s) across {n_findings} finding(s).")
        return 1

    print(f"OK: {seen} citation(s) resolve across {n_findings} finding(s).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
