// Fail if any two modules import each other, in any of the three codebases.
// Reads graft's structural graph, so run `graft build` first.
//
// A module is services/<svc>/internal/<module> for Go, and
// apps/mobile/src/<layer>/<slice> for the mobile app. Test files are ignored,
// and imports that cross between codebases are not counted.
//
// Input: graft/.graph/wiring.json (git-ignored, regenerable).
// Run by `npm run cycles` and by the pr-gate `cycles` job.

import { readFileSync } from "node:fs";

const WIRING = "graft/.graph/wiring.json";
const SUPPORTED_GRAPH_VERSION = 1;

const SHARED_ALIAS = "@shared";
const SHARED_ROOT = "apps/mobile/src/shared";

const WORLDS = [
  { name: "services/go-api", prefix: "services/go-api/internal/" },
  { name: "services/overseer", prefix: "services/overseer/internal/" },
  { name: "apps/mobile", prefix: "apps/mobile/src/" },
];

const NUL = "\u0000";

function isTestPath(path) {
  return (
    path.endsWith("_test.go") ||
    path.endsWith(".test.ts") ||
    path.endsWith(".test.tsx") ||
    path.endsWith(".test.js") ||
    path.endsWith(".spec.ts") ||
    path.includes("/__tests__/") ||
    path.includes("/jest/")
  );
}

function filePathOf(id) {
  const hash = id.indexOf("#");
  return hash === -1 ? id : id.slice(0, hash);
}

function resolveSharedAlias(path) {
  if (path === SHARED_ALIAS) return SHARED_ROOT;
  if (path.startsWith(SHARED_ALIAS + "/")) return SHARED_ROOT + path.slice(SHARED_ALIAS.length);
  return path;
}

function isFileSegment(segment) {
  return segment.includes(".");
}

function moduleOf(path) {
  const segments = path.split("/");
  if (path.startsWith("apps/mobile/src/")) {
    if (segments.length >= 6) return segments.slice(0, 5).join("/");
    if (segments.length === 5) return isFileSegment(segments[4]) ? segments.slice(0, 4).join("/") : path;
    return null;
  }
  if (segments[0] === "services" && segments[2] === "internal" && segments.length >= 5) {
    return segments.slice(0, 4).join("/");
  }
  return null;
}

function worldOf(module) {
  return WORLDS.find((w) => module.startsWith(w.prefix)) ?? null;
}

function collectModuleEdges(edges) {
  const seen = new Set();
  for (const edge of edges) {
    if (edge.relation !== "imports") continue;
    const sourcePath = filePathOf(edge.source);
    const targetPath = filePathOf(edge.target);
    if (isTestPath(sourcePath) || isTestPath(targetPath)) continue;

    const source = moduleOf(resolveSharedAlias(sourcePath));
    const target = moduleOf(resolveSharedAlias(targetPath));
    if (!source || !target || source === target) continue;
    if (worldOf(source) !== worldOf(target)) continue;

    seen.add(source + NUL + target);
  }
  return seen;
}

function findMutualPairs(present) {
  const pairs = new Set();
  for (const key of present) {
    const [source, target] = key.split(NUL);
    if (!present.has(target + NUL + source)) continue;
    const [low, high] = [source, target].sort();
    pairs.add(low + NUL + high);
  }
  return [...pairs].sort().map((key) => key.split(NUL));
}

function readWiring() {
  let raw;
  try {
    raw = readFileSync(WIRING, "utf8");
  } catch {
    console.error(`Cannot read ${WIRING}. Run \`graft build\` first.`);
    process.exit(1);
  }
  const wiring = JSON.parse(raw);
  if (wiring.meta?.version !== SUPPORTED_GRAPH_VERSION) {
    console.error(
      `Unsupported graft graph version ${wiring.meta?.version} (expected ${SUPPORTED_GRAPH_VERSION}). ` +
        `The graft format changed — update scripts/check-cycles.mjs.`,
    );
    process.exit(1);
  }
  return wiring;
}

function main() {
  const pairs = findMutualPairs(collectModuleEdges(readWiring().edges));
  if (pairs.length === 0) {
    console.log("No dependency cycles.");
    return;
  }
  console.error(`Found ${pairs.length} dependency cycle(s):`);
  for (const [low, high] of pairs) {
    const world = worldOf(low);
    const label = (module) => module.slice(world.name.length + 1);
    console.error(`  ${world.name}: \`${label(low)}\` ⇄ \`${label(high)}\``);
  }
  process.exit(1);
}

main();
