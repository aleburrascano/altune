import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const MIN_RUNS = 3;
const SAMPLE = 10;
const GITHUB_REPO = "aleburrascano/altune";
const EPOCH_2000 = Date.parse("2000-01-01T00:00:00Z");

const median = (values) => {
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
};

export function verdicts(budgets, durationsByKey) {
  return Object.entries(budgets).map(([key, budget]) => {
    const durations = durationsByKey[key] ?? [];
    if (durations.length < MIN_RUNS) return { key, median: null, budget, over: false, skipped: true };
    const m = median(durations);
    return { key, median: m, budget, over: m > budget, skipped: false };
  });
}

const durations = (runs, startField, endField) =>
  runs
    .map((r) => [Date.parse(r[startField]), Date.parse(r[endField])])
    .filter(([start, end]) => Number.isFinite(start) && Number.isFinite(end) && end >= EPOCH_2000)
    .map(([start, end]) => Math.round((end - start) / 1000));

async function getJson(url, headers = {}) {
  const res = await fetch(url, { headers });
  if (!res.ok) throw new Error(`${url} answered ${res.status}`);
  return res.json();
}

async function fetchDurations(source, file, env) {
  if (source === "gitea") {
    const url = `${env.GITEA_API}/repos/${env.REPO}/actions/workflows/${file}/runs?status=success&limit=${SAMPLE}`;
    const body = await getJson(url, { Authorization: `token ${env.GITEA_TOKEN}` });
    return durations(body.workflow_runs ?? [], "started_at", "completed_at");
  }
  const url = `https://api.github.com/repos/${GITHUB_REPO}/actions/workflows/${file}/runs?status=success&per_page=${SAMPLE}`;
  const body = await getJson(url);
  return durations(body.workflow_runs ?? [], "run_started_at", "updated_at");
}

async function openIssue(env, v, runDurations) {
  const headers = { Authorization: `token ${env.GITEA_TOKEN}`, "Content-Type": "application/json" };
  const open = await getJson(`${env.GITEA_API}/repos/${env.REPO}/issues?state=open&type=issues&limit=50`, headers);
  if (open.some((i) => i.title.startsWith(`ci budget: ${v.key} `))) return;
  const title = `ci budget: ${v.key} median ${v.median}s is over its ${v.budget}s budget`;
  const body = `Last ${runDurations.length} successful runs of ${v.key} took (seconds, newest first):\n\n${runDurations.map((d) => `- ${d}`).join("\n")}\n`;
  const res = await fetch(`${env.GITEA_API}/repos/${env.REPO}/issues`, {
    method: "POST",
    headers,
    body: JSON.stringify({ title, body }),
  });
  if (!res.ok) throw new Error(`opening issue for ${v.key} answered ${res.status}`);
}

export async function main(env = process.env) {
  const budgets = JSON.parse(
    readFileSync(fileURLToPath(new URL("./ci-budget.json", import.meta.url)), "utf8"),
  );
  const flat = {};
  const byKey = {};
  for (const [source, files] of Object.entries(budgets)) {
    for (const [file, budget] of Object.entries(files)) {
      const key = `${source}/${file}`;
      flat[key] = budget;
      byKey[key] = await fetchDurations(source, file, env);
    }
  }
  const results = verdicts(flat, byKey);
  for (const v of results) {
    const runs = byKey[v.key].length;
    const verdict = v.skipped ? "skipped" : v.over ? "OVER" : "ok";
    const med = v.skipped ? "n/a" : `${v.median}s`;
    console.log(`ci-budget: ${v.key} median ${med} over ${runs} runs, budget ${v.budget}s ${verdict}`);
  }
  const over = results.filter((v) => v.over);
  for (const v of over) await openIssue(env, v, byKey[v.key]);
  return over.length ? 1 : 0;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main().then(
    (code) => process.exit(code),
    (err) => {
      console.error(`ci-budget: ${err.message}`);
      process.exit(2);
    },
  );
}
