#!/usr/bin/env node
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import yaml from "js-yaml";

const WORKFLOWS = ".gitea/workflows";
const MANIFEST = "scripts/ci/gate-parity.json";
const PRECHECK = "scripts/precheck.sh";
const PR_CHECKS = ".github/workflows/pr-checks.yml";
const GATE_WORKFLOW = `${WORKFLOWS}/precheck.yml`;
const CHECK_NAME = /\b(?:test_|jest_)?check "([^"]+)"/g;
const GATE_SCRIPT = /bash scripts\/ci\/([\w.-]+\.sh)/g;

function read(root, path) {
  return readFileSync(join(root, path), "utf8");
}

function runsOnMain(doc) {
  const branches = doc?.on?.push?.branches;
  return Array.isArray(branches) && branches.includes("main");
}

export function mainSteps(root) {
  const keys = [];
  const files = readdirSync(join(root, WORKFLOWS)).filter((f) => /\.ya?ml$/.test(f)).sort();
  for (const file of files) {
    const doc = yaml.load(read(root, `${WORKFLOWS}/${file}`));
    if (!runsOnMain(doc)) continue;
    for (const [job, spec] of Object.entries(doc.jobs ?? {})) {
      for (const step of spec.steps ?? []) keys.push(`${file}#${job}#${step.name ?? step.uses ?? step.run}`);
    }
  }
  return keys;
}

export function gateNames(root) {
  const names = new Set();
  for (const m of read(root, PRECHECK).matchAll(CHECK_NAME)) names.add(m[1]);
  if (existsSync(join(root, GATE_WORKFLOW))) {
    for (const m of read(root, GATE_WORKFLOW).matchAll(GATE_SCRIPT)) names.add(m[1]);
  }
  if (existsSync(join(root, PR_CHECKS))) {
    const doc = yaml.load(read(root, PR_CHECKS));
    for (const job of Object.keys(doc?.jobs ?? {})) names.add(`pr-checks/${job}`);
  }
  return names;
}

export function gaps(root) {
  const manifest = JSON.parse(read(root, MANIFEST));
  const steps = mainSteps(root);
  const gates = gateNames(root);
  const out = [];
  for (const key of steps) {
    const entry = manifest[key];
    if (entry === undefined) out.push(`${key} has no gate counterpart or allow reason`);
    else if ("allow" in entry && !String(entry.allow ?? "").trim()) out.push(`${key} has an allow with an empty reason`);
    else if ("gate" in entry && !gates.has(entry.gate)) out.push(`${key} names gate "${entry.gate}", which precheck does not define`);
    else if (!("gate" in entry) && !("allow" in entry)) out.push(`${key} has no gate counterpart or allow reason`);
  }
  const live = new Set(steps);
  for (const key of Object.keys(manifest)) if (!live.has(key)) out.push(`${key} is stale: no main-triggered step matches it`);
  return out;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const found = gaps(process.argv[2] ?? process.cwd());
  for (const line of found) console.log(`gate-parity: ${line}`);
  process.exit(found.length > 0 ? 1 : 0);
}
