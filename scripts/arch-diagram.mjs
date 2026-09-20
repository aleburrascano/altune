// Render graft's wiring graph into docs/architecture.md: one module-level
// dependency diagram per world, a health summary, and a mutual-dependency
// (cycle) report. Run by `npm run arch` (after `graft build`) and, in --check
// mode, by `npm run arch:check` to fail on drift.
//
// Input:  graft/.graph/wiring.json (structural graph; git-ignored, regenerable).
// Output: docs/architecture.md (committed).
// CLI:    --min-weight N (default 3), --check (exit non-zero on drift),
//         --fail-on-cycles (exit non-zero if any world has a mutual pair).

import { readFileSync, writeFileSync } from "node:fs";

const WIRING = "graft/.graph/wiring.json";
const DOC = "docs/architecture.md";
const SUPPORTED_GRAPH_VERSION = 1;
const DEFAULT_MIN_WEIGHT = 3;
const DENSE_EDGE_COUNT = 40;
const UTILITY_FAN_FRACTION = 0.4;
const SINK_MAX_FAN_OUT = 2;
const MUTUAL_FILES_SHOWN = 6;

const SHARED_ALIAS = "@shared";
const SHARED_ROOT = "apps/mobile/src/shared";

const WORLDS = [
  { name: "services/go-api", prefix: "services/go-api/internal/", grouped: false },
  { name: "services/overseer", prefix: "services/overseer/internal/", grouped: false },
  { name: "apps/mobile", prefix: "apps/mobile/src/", grouped: true },
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
  const world = WORLDS.find((w) => module.startsWith(w.prefix));
  return world ? world.name : null;
}

function collectModuleEdges(edges) {
  const crossings = new Map();
  for (const edge of edges) {
    if (edge.relation !== "imports") continue;
    const sourcePath = filePathOf(edge.source);
    const targetPath = filePathOf(edge.target);
    if (isTestPath(sourcePath) || isTestPath(targetPath)) continue;

    const source = moduleOf(resolveSharedAlias(sourcePath));
    const target = moduleOf(resolveSharedAlias(targetPath));
    if (!source || !target || source === target) continue;
    if (worldOf(source) !== worldOf(target)) continue;

    const key = source + NUL + target;
    let crossing = crossings.get(key);
    if (!crossing) {
      crossing = { source, target, files: new Set() };
      crossings.set(key, crossing);
    }
    crossing.files.add(sourcePath);
  }

  return [...crossings.values()]
    .map((c) => ({ source: c.source, target: c.target, weight: c.files.size, files: [...c.files].sort() }))
    .sort(byEdge);
}

function byEdge(a, b) {
  return a.source.localeCompare(b.source) || a.target.localeCompare(b.target);
}

function analyzeWorld(world, moduleEdges) {
  const edges = moduleEdges.filter((e) => worldOf(e.source) === world.name);
  const modules = [...new Set(edges.flatMap((e) => [e.source, e.target]))].sort();
  const present = new Set(edges.map((e) => e.source + NUL + e.target));

  const fanIn = new Map(modules.map((m) => [m, 0]));
  const fanOut = new Map(modules.map((m) => [m, 0]));
  for (const edge of edges) {
    fanOut.set(edge.source, fanOut.get(edge.source) + 1);
    fanIn.set(edge.target, fanIn.get(edge.target) + 1);
  }

  const threshold = UTILITY_FAN_FRACTION * (modules.length - 1);
  const utility = new Map();
  for (const module of modules) {
    const inDegree = fanIn.get(module);
    const outDegree = fanOut.get(module);
    if (inDegree >= threshold && outDegree <= SINK_MAX_FAN_OUT) {
      utility.set(module, { kind: "sink", fanIn: inDegree, fanOut: outDegree });
    } else if (inDegree === 0 && outDegree >= threshold) {
      utility.set(module, { kind: "root", fanIn: inDegree, fanOut: outDegree });
    }
  }

  const isMutual = (edge) => present.has(edge.target + NUL + edge.source);
  const mutualPairs = buildMutualPairs(edges, isMutual);

  return { world, edges, modules, utility, isMutual, mutualPairs };
}

function buildMutualPairs(edges, isMutual) {
  const byPair = new Map();
  for (const edge of edges) {
    if (!isMutual(edge)) continue;
    const [low, high] = [edge.source, edge.target].sort();
    let pair = byPair.get(low + NUL + high);
    if (!pair) {
      pair = { low, high, forward: null, backward: null };
      byPair.set(low + NUL + high, pair);
    }
    if (edge.source === low) pair.forward = edge;
    else pair.backward = edge;
  }
  return [...byPair.values()].sort((a, b) => a.low.localeCompare(b.low) || a.high.localeCompare(b.high));
}

function assignNodeIds(modules) {
  const ids = new Map();
  const used = new Set();
  for (const module of [...modules].sort()) {
    let base = module.replace(/[^A-Za-z0-9_]/g, "_");
    let id = base;
    let suffix = 2;
    while (used.has(id)) id = base + "_" + suffix++;
    used.add(id);
    ids.set(module, id);
  }
  return ids;
}

function drawnEdgesFor(analysis, minWeight) {
  const { edges, utility, isMutual } = analysis;
  const dense = edges.length > DENSE_EDGE_COUNT;
  return edges.filter((edge) => {
    if (isMutual(edge)) return true;
    if (utility.has(edge.source) || utility.has(edge.target)) return false;
    if (dense && edge.weight < minWeight) return false;
    return true;
  });
}

function drawnNodesFor(analysis, drawnEdges) {
  const nodes = new Set(analysis.modules.filter((m) => !analysis.utility.has(m)));
  for (const edge of drawnEdges) {
    nodes.add(edge.source);
    nodes.add(edge.target);
  }
  return [...nodes].sort();
}

function labelOf(module, world) {
  return module.slice(world.name.length + 1);
}

function layerOf(module) {
  return module.split("/")[3];
}

function renderDoc(moduleEdges, minWeight) {
  const lines = [
    "# Module architecture",
    "",
    "<!-- Generated by `npm run arch` (scripts/arch-diagram.mjs). Do not edit by hand. -->",
    "",
    "Arrows mean **depends on**. The number on an arrow counts the source files crossing",
    "that boundary. Test files are excluded. **Red** marks a mutual dependency (a cycle).",
    "",
  ];
  for (const world of WORLDS) {
    lines.push(...renderWorld(analyzeWorld(world, moduleEdges), minWeight));
  }
  return lines.join("\n") + "\n";
}

function renderWorld(analysis, minWeight) {
  const { world, edges, modules, utility, mutualPairs } = analysis;
  const nodeIds = assignNodeIds(modules);
  const drawnEdges = drawnEdgesFor(analysis, minWeight);
  const drawnNodes = drawnNodesFor(analysis, drawnEdges);

  const lines = [
    `## ${world.name}`,
    "",
    `${modules.length} modules · ${edges.length} dependencies · ${mutualPairs.length} mutual`,
    "",
  ];
  lines.push(...renderUtilityNote(utility, world));
  lines.push(...renderMutualList(mutualPairs, world));
  lines.push(...renderDiagram(analysis, nodeIds, drawnNodes, drawnEdges));
  return lines;
}

function renderUtilityNote(utility, world) {
  if (utility.size === 0) return [];
  const entries = [...utility.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([module, info]) => `- \`${labelOf(module, world)}\` (${info.kind}, in ${info.fanIn}, out ${info.fanOut})`);
  return ["Utility modules (expected background, omitted from the diagram unless mutual):", "", ...entries, ""];
}

function renderMutualList(mutualPairs, world) {
  if (mutualPairs.length === 0) return [];
  const lines = ["### Mutual dependencies", ""];
  for (const pair of mutualPairs) {
    lines.push(`- \`${labelOf(pair.low, world)}\` ⇄ \`${labelOf(pair.high, world)}\``);
    lines.push("  " + renderDirection(pair.forward, world));
    lines.push("  " + renderDirection(pair.backward, world));
  }
  lines.push("");
  return lines;
}

function renderDirection(edge, world) {
  const shown = edge.files.slice(0, MUTUAL_FILES_SHOWN);
  const overflow = edge.files.length - shown.length;
  const files = shown.join(", ") + (overflow > 0 ? `, and ${overflow} more` : "");
  return `- \`${labelOf(edge.source, world)}\` → \`${labelOf(edge.target, world)}\` (${edge.weight}): ${files}`;
}

function renderDiagram(analysis, nodeIds, drawnNodes, drawnEdges) {
  const { world, isMutual } = analysis;
  const lines = ["```mermaid", "flowchart LR"];
  lines.push(...renderNodes(drawnNodes, nodeIds, world));

  const mutualNodeIds = new Set();
  const mutualEdgeIndexes = [];
  drawnEdges.forEach((edge, index) => {
    lines.push(`  ${nodeIds.get(edge.source)} -->|${edge.weight}| ${nodeIds.get(edge.target)}`);
    if (isMutual(edge)) {
      mutualEdgeIndexes.push(index);
      mutualNodeIds.add(nodeIds.get(edge.source));
      mutualNodeIds.add(nodeIds.get(edge.target));
    }
  });

  if (mutualNodeIds.size > 0) {
    lines.push("  classDef mutual stroke:#d33,color:#d33,stroke-width:2px;");
    lines.push(`  class ${[...mutualNodeIds].sort().join(",")} mutual;`);
  }
  for (const index of mutualEdgeIndexes) {
    lines.push(`  linkStyle ${index} stroke:#d33,color:#d33;`);
  }
  lines.push("```", "");
  return lines;
}

function renderNodes(drawnNodes, nodeIds, world) {
  const declare = (module) => `${nodeIds.get(module)}["${labelOf(module, world)}"]`;
  if (!world.grouped) {
    return drawnNodes.map((module) => "  " + declare(module));
  }
  const lines = [];
  const layers = [...new Set(drawnNodes.map(layerOf))].sort();
  for (const layer of layers) {
    lines.push(`  subgraph ${layer}`);
    for (const module of drawnNodes.filter((m) => layerOf(m) === layer)) {
      lines.push("    " + declare(module));
    }
    lines.push("  end");
  }
  return lines;
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
        `The graft format changed — update scripts/arch-diagram.mjs.`,
    );
    process.exit(1);
  }
  return wiring;
}

function parseArgs(argv) {
  let minWeight = DEFAULT_MIN_WEIGHT;
  let check = false;
  let failOnCycles = false;
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--check") {
      check = true;
    } else if (arg === "--fail-on-cycles") {
      failOnCycles = true;
    } else if (arg === "--min-weight") {
      minWeight = Number(argv[++i]);
    } else if (arg.startsWith("--min-weight=")) {
      minWeight = Number(arg.slice("--min-weight=".length));
    } else {
      console.error(`Unknown argument: ${arg}`);
      process.exit(1);
    }
  }
  if (!Number.isInteger(minWeight) || minWeight < 1) {
    console.error(`--min-weight must be a positive integer, got ${minWeight}`);
    process.exit(1);
  }
  return { minWeight, check, failOnCycles };
}

function reportCyclesAndExit(moduleEdges) {
  const offenders = WORLDS.flatMap((world) =>
    analyzeWorld(world, moduleEdges).mutualPairs.map((pair) => ({ world, pair })),
  );
  if (offenders.length === 0) {
    console.log("No dependency cycles.");
    return;
  }
  console.error(`Found ${offenders.length} dependency cycle(s):`);
  for (const { world, pair } of offenders) {
    console.error(`  ${world.name}: \`${labelOf(pair.low, world)}\` ⇄ \`${labelOf(pair.high, world)}\``);
  }
  process.exit(1);
}

function main() {
  const { minWeight, check, failOnCycles } = parseArgs(process.argv.slice(2));
  const moduleEdges = collectModuleEdges(readWiring().edges);

  if (failOnCycles) {
    reportCyclesAndExit(moduleEdges);
    return;
  }

  const doc = renderDoc(moduleEdges, minWeight);

  if (check) {
    let committed = null;
    try {
      committed = readFileSync(DOC, "utf8");
    } catch {
      committed = null;
    }
    if (committed !== doc) {
      console.error(`${DOC} is out of date — run \`npm run arch\`.`);
      process.exit(1);
    }
    console.log(`${DOC} is up to date.`);
    return;
  }

  writeFileSync(DOC, doc);
  console.log(`Wrote ${DOC}.`);
}

main();
