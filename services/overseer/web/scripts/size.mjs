import process from "node:process";
import { readFileSync } from "node:fs";
import { gzipSync } from "node:zlib";
import { join, resolve } from "node:path";

const BUDGET_BYTES = 250 * 1024;
const BASE = "/overseer/";

const distDir = resolve(process.argv[2] ?? "../internal/webui/dist");
const budget = Number(process.env.SIZE_BUDGET_BYTES ?? BUDGET_BYTES);

const html = readFileSync(join(distDir, "index.html"), "utf8");
const refs = new Set();
for (const match of html.matchAll(/(?:src|href)="([^"]+\.js)"/g)) {
  refs.add(match[1].startsWith(BASE) ? match[1].slice(BASE.length) : match[1].replace(/^\//, ""));
}

if (refs.size === 0) {
  process.stderr.write("size: index.html references no JS files" + "\n");
  process.exit(1);
}

let total = 0;
for (const ref of refs) {
  const bytes = gzipSync(readFileSync(join(distDir, ref))).length;
  total += bytes;
  process.stdout.write(`${(bytes / 1024).toFixed(1).padStart(8)} KB  ${ref}` + "\n");
}

process.stdout.write(`total gzipped JS: ${(total / 1024).toFixed(1)} KB (budget ${(budget / 1024).toFixed(1)} KB)` + "\n");
if (total > budget) {
  process.stderr.write("size: over budget" + "\n");
  process.exit(1);
}
