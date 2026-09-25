#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { posix } from "node:path";
import { fileURLToPath } from "node:url";

const GO_KINDS = ["integration", "internal", "external", "e2e", "fuzz", "bench"];
const TS_KINDS = ["property", "integration", "e2e", "bench"];
const FREE_KINDS = ["integration", "e2e"];
const CROSS_LEAD = /^(contracts?|invariants?|leak|main|export|helpers?|fixtures?|testutil|testing|examples?|doc)([_.-]|$)/i;
const CROSS_TAIL = /[_-](contracts?|invariants?)$|[a-z0-9](Contracts?|Invariants?)$/;
const isCross = (stem) => CROSS_LEAD.test(stem) || CROSS_TAIL.test(stem);
const TS_TEST = /^(.+?)\.(test|spec)\.[cm]?[jt]sx?$/;
const TS_SRC = /\.(ts|tsx|js|jsx|mjs|cjs)$/;
const GO_TAG = /^\/\/go:build\b.*\b(integration|e2e)\b/m;

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
  if (isCross(me.stem)) return null;
  if (!me.unit) return FREE_KINDS.includes(me.kind) ? null : { reason: "no-unit" };
  const homes = names
    .filter((n) => n !== name && n.endsWith("_test.go"))
    .filter((n) => { const u = goUnit(dir, n, sources, repo.read); return !isCross(u.stem) && u.unit === me.unit && u.kind === me.kind; })
    .map((n) => posix.join(dir, n));
  const want = posix.join(dir, `${me.unit}${me.kind === "unit" || !GO_KINDS.includes(me.kind) ? "" : `_${me.kind}`}_test.go`);
  if (homes.length) return { reason: "duplicate", homes, exact: me.stem === me.unit, want };
  if (!FREE_KINDS.includes(me.kind) && me.stem !== me.unit) return { reason: "misnamed", want };
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
  if (isCross(me.first)) return null;
  if (!me.unit) return FREE_KINDS.includes(me.kind) ? null : { reason: "no-unit" };
  const dirs = root === dir ? [dir, posix.join(dir, "__tests__")] : [dir, root];
  const homes = [];
  for (const d of dirs) {
    for (const n of repo.list(d)) {
      if ((d === dir && n === name) || !TS_TEST.test(n)) continue;
      const u = tsUnit(root, n, repo);
      if (!isCross(u.first) && u.unit === me.unit && u.kind === me.kind) homes.push(posix.join(d, n));
    }
  }
  const m = name.match(/\.(test|spec)\.([cm]?[jt]sx?)$/);
  const want = posix.join(dir, `${me.unit}${me.kind === "unit" ? "" : `.${me.kind}`}.${m[1]}.${m[2]}`);
  if (homes.length) return { reason: "duplicate", homes, exact: me.stem === me.unit, want };
  if (!FREE_KINDS.includes(me.kind) && me.stem !== me.unit) return { reason: "misnamed", want };
  return null;
}

export function pickHome(homes) {
  return [...homes].sort((a, b) => a.length - b.length || a.localeCompare(b))[0];
}

export function gitRepo(cwd, base, added = [], removed = []) {
  const git = (args) => execFileSync("git", ["-C", cwd, ...args], { encoding: "utf8", maxBuffer: 64 << 20, stdio: ["ignore", "pipe", "ignore"] });
  const lines = (s) => s.split("\n").filter(Boolean);
  const all = new Set([...lines(git(["ls-tree", "-r", "--name-only", base])), ...added]);
  for (const p of removed) all.delete(p);
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
        const local = new RegExp(re);
        hit ||= added.some((p) => p.startsWith(`${dir}/`) && TS_SRC.test(p) && !TS_TEST.test(posix.basename(p))
          && local.test((() => { try { return readFileSync(posix.join(cwd, p), "utf8"); } catch { return ""; } })()));
        exported.set(key, hit);
      }
      return exported.get(key);
    },
  };
}

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
    if (v.exact) continue;
    const home = pickHome([...v.homes, path]);
    if (home === path && v.homes.every((h) => addedSet.has(h))) {
      out.push(`${path}: the first test file for its unit is named after the unit: ${v.want}`);
      continue;
    }
    const others = v.homes.filter((h) => h !== home).length;
    out.push(`${path}: its unit already has ${home}${others ? ` (+${others} more)` : ""}; add these tests there`);
  }
  return out;
}

function main() {
  const ref = process.argv[2] || "origin/main";
  const git = (args) => execFileSync("git", args, { encoding: "utf8", maxBuffer: 64 << 20 });
  let base, added, removed, repo;
  try {
    base = git(["merge-base", ref, "HEAD"]).trim();
    added = [...new Set([
      ...git(["diff", "--no-renames", "--name-only", "--diff-filter=A", base]).split("\n"),
      ...git(["ls-files", "--others", "--exclude-standard"]).split("\n"),
    ].filter(Boolean))];
    removed = git(["diff", "--no-renames", "--name-only", "--diff-filter=D", base]).split("\n").filter(Boolean);
    repo = gitRepo(".", base, added, removed);
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
