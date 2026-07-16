#!/usr/bin/env python3
"""okf-lint: mechanical health checks for the okf/ knowledge bundle.

Checks (in order):
  ERRORS  - unparseable/missing frontmatter, missing required fields,
            broken relative links, duplicate concept slugs
  STALE   - resource files changed since the concept's verified_commit
  WARN    - concepts missing from their directory's index.md,
            concepts with no inbound links (orphans),
            unknown/invalid verified_commit

Exit code: 1 if any ERROR, else 0 (STALE and WARN never fail the run).
Run from the repo root: python .claude/skills/okf-audit/scripts/okf-lint.py
"""

import os
import re
import subprocess
import sys

ROOT = "okf"
LINK_RE = re.compile(r"\]\(([^)#\s]+\.md)\)")
RESERVED = ("index.md", "log.md")

errors, stale, warns = [], [], []


def parse_frontmatter(text):
    if not text.startswith("---"):
        return None
    end = text.find("\n---", 3)
    if end == -1:
        return None
    fm = {}
    for line in text[3:end].strip().splitlines():
        m = re.match(r"^(\w[\w-]*):\s*(.*)$", line)
        if m:
            fm[m.group(1)] = m.group(2).strip()
    return fm


def git(*args):
    r = subprocess.run(["git", *args], capture_output=True, text=True)
    return r.returncode, r.stdout.strip()


def main():
    files = []  # (path, is_concept, frontmatter, text)
    slugs = {}
    for dirpath, _, names in os.walk(ROOT):
        for name in sorted(names):
            if not name.endswith(".md"):
                continue
            path = os.path.join(dirpath, name).replace("\\", "/")
            text = open(path, encoding="utf-8").read()
            is_concept = name not in RESERVED
            fm = parse_frontmatter(text)
            files.append((path, is_concept, fm, text))
            if is_concept:
                slug = name[:-3]
                if slug in slugs:
                    errors.append(f"duplicate slug '{slug}': {slugs[slug]} and {path}")
                slugs[slug] = path

    linked_to = set()
    for path, is_concept, fm, text in files:
        # frontmatter
        if fm is None:
            errors.append(f"{path}: missing or unparseable frontmatter")
        else:
            for field in ("type", "title", "description"):
                if not fm.get(field):
                    errors.append(f"{path}: missing frontmatter field '{field}'")
            if is_concept and fm.get("resource") and not fm.get("verified_commit"):
                warns.append(f"{path}: has resource but no verified_commit")

        # links
        for m in LINK_RE.finditer(text):
            target = os.path.normpath(
                os.path.join(os.path.dirname(path), m.group(1))
            ).replace("\\", "/")
            if os.path.exists(target):
                linked_to.add(target)
            else:
                errors.append(f"{path}: broken link -> {m.group(1)}")

        # staleness vs verified_commit
        if is_concept and fm and fm.get("resource") and fm.get("verified_commit"):
            vc = fm["verified_commit"]
            rc, _ = git("cat-file", "-e", f"{vc}^{{commit}}")
            if rc != 0:
                warns.append(f"{path}: verified_commit {vc[:12]} not found in repo")
                continue
            resources = [r.strip().strip("\"'") for r in fm["resource"].split(",")]
            resources = [r for r in resources if r]
            rc, out = git("diff", "--name-only", f"{vc}..HEAD", "--", *resources)
            if rc == 0 and out:
                changed = [
                    f for f in out.splitlines() if os.path.basename(f) != "CLAUDE.md"
                ]
                if changed:
                    stale.append(
                        f"{path}: {len(changed)} resource file(s) changed since "
                        f"{vc[:12]} (e.g. {changed[0]})"
                    )

    # index sync + orphans
    for path, is_concept, fm, text in files:
        if not is_concept:
            continue
        index = os.path.join(os.path.dirname(path), "index.md").replace("\\", "/")
        if os.path.exists(index) and path not in linked_to:
            warns.append(f"{path}: not linked from any file (orphan)")
        elif os.path.exists(index):
            index_text = open(index, encoding="utf-8").read()
            base = os.path.basename(path)
            if base not in index_text:
                warns.append(f"{path}: not listed in {index}")

    for label, bucket in (("ERROR", errors), ("STALE", stale), ("WARN", warns)):
        for msg in bucket:
            print(f"{label}: {msg}")
    print(
        f"okf-lint: {len(errors)} error(s), {len(stale)} stale, "
        f"{len(warns)} warning(s) across {len(files)} files"
    )
    sys.exit(1 if errors else 0)


if __name__ == "__main__":
    main()
