#!/usr/bin/env node
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import yaml from "js-yaml";

const WORKFLOWS = ".gitea/workflows";
const PR_CHECKS = ".github/workflows/pr-checks.yml";
const GATE_SCRIPT = /bash scripts\/ci\/([\w.-]+\.sh)/g;

export const HEAVY = [
  { name: "go test ./...", re: /\bgo test\b.*\.\/\.\.\./ },
  { name: "jest", re: /\bjest\b/ },
  { name: "nilaway", re: /\bnilaway\b/ },
  { name: "fallow", re: /\bnpm run fallow\b|\bfallow\b/ },
  { name: "react-doctor", re: /react-doctor/ },
  { name: "playwright", re: /\bplaywright test\b/ },
];

function triggersOnPullRequest(doc) {
  const on = doc?.on;
  if (typeof on === "string") return on === "pull_request";
  if (Array.isArray(on)) return on.includes("pull_request");
  return on != null && typeof on === "object" && "pull_request" in on;
}

function runBlocks(doc) {
  const blocks = [];
  for (const [job, spec] of Object.entries(doc?.jobs ?? {})) {
    for (const step of spec.steps ?? []) {
      if (typeof step.run === "string") blocks.push({ job, run: step.run });
    }
  }
  return blocks;
}

function lines(text) {
  return text.split("\n");
}

export function commands(root) {
  const out = [];
  const dir = join(root, WORKFLOWS);
  const files = existsSync(dir) ? readdirSync(dir).filter((f) => /\.ya?ml$/.test(f)).sort() : [];
  for (const file of files) {
    const doc = yaml.load(readFileSync(join(dir, file), "utf8"));
    if (!triggersOnPullRequest(doc)) continue;
    for (const { job, run } of runBlocks(doc)) {
      const place = `${file}#${job}`;
      for (const line of lines(run)) {
        out.push({ place, line });
        for (const m of line.matchAll(GATE_SCRIPT)) {
          const script = join(root, "scripts/ci", m[1]);
          if (!existsSync(script)) continue;
          for (const inner of lines(readFileSync(script, "utf8"))) {
            out.push({ place: `${place} via scripts/ci/${m[1]}`, line: inner });
          }
        }
      }
    }
  }
  const prChecks = join(root, PR_CHECKS);
  if (existsSync(prChecks)) {
    const doc = yaml.load(readFileSync(prChecks, "utf8"));
    for (const { job, run } of runBlocks(doc)) {
      for (const line of lines(run)) out.push({ place: `pr-checks.yml#${job}`, line });
    }
  }
  return out;
}

export function dupes(root) {
  const cmds = commands(root);
  const found = [];
  for (const { name, re } of HEAVY) {
    const places = [...new Set(cmds.filter((c) => re.test(c.line)).map((c) => c.place))];
    if (places.length >= 2) found.push({ name, places });
  }
  return found;
}

function main() {
  const found = dupes(process.argv[2] ?? ".");
  for (const { name, places } of found) {
    console.log(`pr-ci-dupes: ${name} runs in ${places.join(" and ")}`);
  }
  process.exit(found.length > 0 ? 1 : 0);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main();
