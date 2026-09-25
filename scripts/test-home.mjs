#!/usr/bin/env node
// Vendored from ~/.claude/bin/test-home.mjs so CI can run it; keep the two in step.
// A test file belongs to the unit it tests, not to the ticket that prompted it.
// Fails when a change adds a test file that (a) duplicates a unit that already
// has a test file of the same kind, naming the file the tests belong in, or
// (b) names no unit at all, the per-bug file named after the scenario, or
// (c) is a unit's first test file but carries a scenario in its name.
// Policy: ~/.claude/workflow/build/test-conventions.md "Where a test lives".
//
// Usage: test-home.mjs [base-ref]   (run from the repo root; default origin/main)
// Exit: 0 every added test file is its unit's first, 1 a test file breaks the
// rule, 3 could not run.
//
// A unit is a source file: Go `<unit>[_<scenario>]_test.go`, <unit>.go the
// longest matching file in the package; TS/JS `<unit>[.<scenario>].test.ts(x)`,
// <unit> a source file (or an exported function) under the test's folder.
// A separate kind gets its own file; integration and e2e need not name a source file:
// Go `_integration`, `_internal`, `_external`, `_e2e`, `_fuzz`, `_bench`
// suffixes or an integration/e2e build tag; TS `.property`, `.integration`,
// `.e2e`, `.bench` before `.test`. Cross-cutting files (contract, invariants,
// leak, main, export, helpers, fixtures, testutil, example) are not judged,
// nor are e2e/ trees or Python.
//
// A vendored copy may live in a repo (for CI); keep it in step with this file.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { posix } from "node:path";
import { fileURLToPath } from "node:url";

const GO_KINDS = ["integration", "internal", "external", "e2e", "fuzz", "bench"];
const TS_KINDS = ["property", "integration", "e2e", "bench"];
// Feature-level kinds: they span units, so they need not name a source file.
const FREE_KINDS = ["integration", "e2e"];
const CROSS = /^(contracts?|invariants?|leak|main|export|helpers?|fixtures?|testutil|testing|examples?|doc)([_.-]|$)|[_-]?(contracts?|invariants?)$/i;
const TS_TEST = /^(.+?)\.(test|spec)\.[cm]?[jt]sx?$/;
const TS_SRC = /\.(ts|tsx|js|jsx|mjs|cjs)$/;
const GO_TAG = /^\/\/go:build\b.*\b(integration|e2e)\b/m;

// repo: { list(dir) -> names in dir, tree(dir) -> paths under dir, read(path) -> text,
// exports(dir, name) -> a source under dir exports name }. Returns null when the
// file is fine, else { reason: "duplicate", homes } or { reason: "no-unit" }.
export function judge(path, repo) {
  const dir = posix.dirname(path), name = posix.basename(path);
  if (/(^|\/)e2e\//.test(path)) return null;
  if (name.endsWith("_test.go")) return judgeGo(dir, name, repo);
  if (TS_TEST.test(name)) return judgeTs(dir, name, repo);
  return null;
}

function goUnit(dir, name, sources, read) {
  let stem = name.slice(0, -"_test.go".length), kind = "unit";
  const suffix = GO_KINDS.find((k) => stem.endsWith(`_${k}`));
  if (suffix) { kind = suffix; stem = stem.slice(0, -(suffix.length + 1)); }
  else { const tag = read(posix.join(dir, name)).match(GO_TAG); if (tag) kind = tag[1]; }
  const parts = stem.split("_");
  for (let i = parts.length; i > 0; i--) {
    const unit = parts.slice(0, i).join("_");
    if (sources.has(`${unit}.go`)) return { unit, kind, stem };
  }
  return { unit: null, kind, stem };
}

function judgeGo(dir, name, repo) {
  const names = repo.list(dir);
  const sources = new Set(names.filter((n) => n.endsWith(".go") && !n.endsWith("_test.go")));
  const me = goUnit(dir, name, sources, repo.read);
  if (CROSS.test(me.stem)) return null;
  if (!me.unit) return FREE_KINDS.includes(me.kind) ? null : { reason: "no-unit" };
  const homes = names
    .filter((n) => n !== name && n.endsWith("_test.go"))
    .filter((n) => { const u = goUnit(dir, n, sources, repo.read); return u.unit === me.unit && u.kind === me.kind; })
    .map((n) => posix.join(dir, n));
  if (homes.length) return { reason: "duplicate", homes };
  if (!FREE_KINDS.includes(me.kind) && me.stem !== me.unit) {
    return { reason: "misnamed", want: posix.join(dir, `${me.unit}${me.kind === "unit" || !GO_KINDS.includes(me.kind) ? "" : `_${me.kind}`}_test.go`) };
  }
  return null;
}

function tsRoot(dir) {
  return posix.basename(dir) === "__tests__" ? posix.dirname(dir) : dir;
}

function tsUnit(root, name, repo) {
  const segs = name.match(TS_TEST)[1].split(".");
  let kind = "unit";
  if (segs.length > 1 && TS_KINDS.includes(segs[segs.length - 1])) kind = segs.pop();
  const stems = new Set();
  for (const p of repo.tree(root)) {
    if (/(^|\/)__tests__\//.test(p) || TS_TEST.test(posix.basename(p))) continue;
    const b = posix.basename(p);
    if (TS_SRC.test(b)) stems.add(/^index\./.test(b) ? posix.basename(posix.dirname(p)) : b.replace(TS_SRC, ""));
  }
  for (let i = segs.length; i > 0; i--) {
    const unit = segs.slice(0, i).join(".");
    if (stems.has(unit)) return { unit, kind, first: segs[0], stem: segs.join(".") };
  }
  if (repo.exports(root, segs[0])) return { unit: segs[0], kind, first: segs[0], stem: segs.join(".") };
  return { unit: null, kind, first: segs[0], stem: segs.join(".") };
}

function judgeTs(dir, name, repo) {
  const root = tsRoot(dir);
  const me = tsUnit(root, name, repo);
  if (CROSS.test(me.first)) return null;
  if (!me.unit) return FREE_KINDS.includes(me.kind) ? null : { reason: "no-unit" };
  const dirs = root === dir ? [dir, posix.join(dir, "__tests__")] : [dir, root];
  const homes = [];
  for (const d of dirs) {
    for (const n of repo.list(d)) {
      if ((d === dir && n === name) || !TS_TEST.test(n)) continue;
      const u = tsUnit(root, n, repo);
      if (u.unit === me.unit && u.kind === me.kind) homes.push(posix.join(d, n));
    }
  }
  if (homes.length) return { reason: "duplicate", homes };
  if (!FREE_KINDS.includes(me.kind) && me.stem !== me.unit) {
    const m = name.match(/\.(test|spec)\.([cm]?[jt]sx?)$/);
    return { reason: "misnamed", want: posix.join(dir, `${me.unit}${me.kind === "unit" ? "" : `.${me.kind}`}.${m[1]}.${m[2]}`) };
  }
  return null;
}

// Shortest name first, so `scheduler_test.go` leads `scheduler_edge_test.go`.
export function pickHome(homes) {
  return [...homes].sort((a, b) => a.length - b.length || a.localeCompare(b))[0];
}

// A repo view at `base`, overlaid with `added` paths (files this change creates).
export function gitRepo(cwd, base, added = []) {
  const git = (args) => execFileSync("git", ["-C", cwd, ...args], { encoding: "utf8", maxBuffer: 64 << 20, stdio: ["ignore", "pipe", "ignore"] });
  const lines = (s) => s.split("\n").filter(Boolean);
  const all = new Set([...lines(git(["ls-tree", "-r", "--name-only", base])), ...added]);
  const byDir = new Map();
  for (const p of all) {
    const d = posix.dirname(p);
    if (!byDir.has(d)) byDir.set(d, []);
    byDir.get(d).push(posix.basename(p));
  }
  const exported = new Map(), trees = new Map();
  return {
    list: (dir) => byDir.get(dir) || [],
    tree: (dir) => {
      if (!trees.has(dir)) trees.set(dir, [...all].filter((p) => p.startsWith(`${dir}/`)));
      return trees.get(dir);
    },
    read: (path) => {
      try { return git(["show", `${base}:${path}`]); } catch {}
      try { return readFileSync(posix.join(cwd, path), "utf8"); } catch { return ""; }
    },
    exports: (dir, name) => {
      const key = `${dir}\0${name}`;
      if (!exported.has(key)) {
        const re = `export (default )?(async )?(function|const|class|let) ${name}\\b`;
        let hit = false;
        try { hit = git(["grep", "-lE", re, base, "--", `${dir}/`]).trim() !== ""; } catch { hit = false; }
        exported.set(key, hit);
      }
      return exported.get(key);
    },
  };
}

// Every added path that breaks the rule, as report lines. Two new files for one
// unit: the shortest is its home, the rest are duplicates of it.
export function violations(added, repo) {
  const out = [];
  const addedSet = new Set(added);
  for (const path of added) {
    const v = judge(path, repo);
    if (!v) continue;
    if (v.reason === "no-unit") {
      out.push(`${path}: names no source file it tests; name the test file after its unit's source file (or add to that file's existing test file), never after the bug or scenario`);
      continue;
    }
    if (v.reason === "misnamed") {
      out.push(`${path}: the first test file for its unit is named after the unit: ${v.want}`);
      continue;
    }
    const home = pickHome([...v.homes, path]);
    if (home === path && v.homes.every((h) => addedSet.has(h))) continue;
    const others = v.homes.filter((h) => h !== home).length;
    out.push(`${path}: its unit already has ${home}${others ? ` (+${others} more)` : ""}; add these tests there`);
  }
  return out;
}

function main() {
  const ref = process.argv[2] || "origin/main";
  const git = (args) => execFileSync("git", args, { encoding: "utf8", maxBuffer: 64 << 20 });
  let base, added, repo;
  try {
    base = git(["merge-base", ref, "HEAD"]).trim();
    added = [...new Set([
      ...git(["diff", "--name-only", "--diff-filter=A", base]).split("\n"),
      ...git(["ls-files", "--others", "--exclude-standard"]).split("\n"),
    ].filter(Boolean))];
    repo = gitRepo(".", base, added);
  } catch (e) {
    console.error(`test-home: could not diff against ${ref}: ${e.message.split("\n")[0]}`);
    process.exit(3);
  }
  const bad = violations(added, repo);
  if (bad.length) {
    console.log(bad.join("\n"));
    console.log("test-home: a test file belongs to the unit it tests, not to the ticket (~/.claude/workflow/build/test-conventions.md \"Where a test lives\")");
    process.exit(1);
  }
  console.log(`test-home: ${added.length} added file(s), every new test file is its unit's first`);
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) main();
