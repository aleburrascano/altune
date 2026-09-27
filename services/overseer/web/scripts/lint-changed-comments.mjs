import process from "node:process";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { parser } from "typescript-eslint";

const SUPPRESSION_DIRECTIVE = /eslint-disable|@ts-expect-error|@ts-ignore|@ts-nocheck|biome-ignore/;

const DIFF_DETECT_FLAGS = ["-M", "-C", "--find-copies-harder"];

const rawBase = process.argv[2];
if (!rawBase || rawBase.startsWith("-")) {
  process.stderr.write("usage: lint-changed-comments.mjs <base-ref>\n");
  process.exit(2);
}

const git = (args) => execFileSync("git", args, { encoding: "utf8" });

let base;
try {
  base = git(["rev-parse", "--verify", "--end-of-options", `${rawBase}^{commit}`]).trim();
} catch {
  process.stderr.write(`lint-changed-comments.mjs: cannot resolve base ref ${rawBase}\n`);
  process.exit(2);
}

const changedFiles = () =>
  git(["diff", "--name-only", "-z", "--diff-filter=ACMR", ...DIFF_DETECT_FLAGS, "--relative", base, "--", "."])
    .split("\0")
    .filter(Boolean)
    .filter((f) => /\.(ts|tsx|js|mjs|cjs)$/.test(f));

const addedLinesByFile = () => {
  const byFile = new Map();
  const target = /^\+\+\+ b\/(.+?)\t?$/;
  const hunk = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/;
  let added = null;
  for (const line of git(["diff", ...DIFF_DETECT_FLAGS, "-U0", "--relative", base, "--", "."]).split("\n")) {
    const t = target.exec(line);
    if (t) {
      added = new Set();
      byFile.set(t[1], added);
      continue;
    }
    const m = hunk.exec(line);
    if (!m || !added) continue;
    const start = Number(m[1]);
    const count = m[2] === undefined ? 1 : Number(m[2]);
    for (let n = start; n < start + count; n += 1) added.add(n);
  }
  return byFile;
};

const files = changedFiles();
const addedByFile = addedLinesByFile();
if (files.length === 0) {
  process.stdout.write("No changed files to check for new comments.\n");
  process.exit(0);
}
process.stdout.write("Checking added lines for new comments/suppressions in:\n");
for (const f of files) process.stdout.write(`  ${f}\n`);

const spansAddedLine = (loc, added) => {
  for (let n = loc.start.line; n <= loc.end.line; n += 1) if (added.has(n)) return true;
  return false;
};

const commentHitsOf = (file) => {
  const added = addedByFile.get(file) ?? new Set();
  if (added.size === 0) return [];
  const { ast } = parser.parseForESLint(readFileSync(file, "utf8"), {
    loc: true,
    comment: true,
    jsx: /\.tsx$/.test(file),
  });
  return ast.comments
    .filter((comment) => spansAddedLine(comment.loc, added))
    .map((comment) => ({
      file,
      line: comment.loc.start.line,
      kind: SUPPRESSION_DIRECTIVE.test(comment.value) ? "suppression" : "comment",
    }));
};

const commentHits = files.flatMap(commentHitsOf);
for (const hit of commentHits) {
  process.stdout.write(`  ${hit.file}:${hit.line}  new ${hit.kind} on a changed line — zero-comments rule\n`);
}
process.stdout.write(`new-code comment/suppression violations: ${commentHits.length}\n`);

process.exit(commentHits.length > 0 ? 1 : 0);
