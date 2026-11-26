import { relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';

const CLIENT_ROOT = fileURLToPath(new URL('..', import.meta.url));
const USAGE =
  'usage: lint-by-area.mjs [path ...]   lints the client tree (default: all of it; a path is resolved from where you run it) and prints the errors as a table of area by rule; exits 1 when any error is found, 2 on a bad flag or when every path given is ignored';
const NO_RULE = '(parse error)';
const ROOT_FILES = '(client root files)';
const MODULE_FOLDERS = new Set(['shared', 'features']);

export function areaOf(relativePath) {
  const parts = relativePath.split('/');
  if (parts.length === 1) return ROOT_FILES;
  if (parts[0] === 'src' && MODULE_FOLDERS.has(parts[1]) && parts.length > 3) {
    return `src/${parts[1]}/${parts[2]}`;
  }
  if (parts[0] === 'src' && parts.length > 2) return `src/${parts[1]}`;
  return `${parts[0]}/`;
}

export function tallyByArea(results, root) {
  const areas = new Map();
  for (const result of results) {
    const area = areaOf(relative(root, result.filePath).split(sep).join('/'));
    for (const message of result.messages) {
      if (message.severity !== 2) continue;
      const rule = message.ruleId ?? NO_RULE;
      const rules = areas.get(area) ?? new Map();
      rules.set(rule, (rules.get(rule) ?? 0) + 1);
      areas.set(area, rules);
    }
  }
  return areas;
}

const sum = (counts) => [...counts].reduce((total, count) => total + count, 0);

export function renderTable(areas) {
  const ruleTotals = new Map();
  for (const rules of areas.values()) {
    for (const [rule, count] of rules) ruleTotals.set(rule, (ruleTotals.get(rule) ?? 0) + count);
  }
  const rules = [...ruleTotals.keys()].sort(
    (a, b) => ruleTotals.get(b) - ruleTotals.get(a) || a.localeCompare(b),
  );
  const rows = [...areas.entries()]
    .map(([area, counts]) => ({ area, counts, total: sum(counts.values()) }))
    .sort((a, b) => b.total - a.total || a.area.localeCompare(b.area));
  const line = (cells) => `| ${cells.join(' | ')} |`;
  return [
    line(['area', ...rules.map((rule) => `\`${rule}\``), 'total']),
    line(['---', ...rules.map(() => '---:'), '---:']),
    ...rows.map(({ area, counts, total }) =>
      line([area, ...rules.map((rule) => counts.get(rule) ?? 0), total]),
    ),
    line(['**total**', ...rules.map((rule) => ruleTotals.get(rule)), sum(ruleTotals.values())]),
  ].join('\n');
}

export function isIgnoredFile(result) {
  return (
    result.messages.length === 1 &&
    result.messages[0].severity === 1 &&
    result.messages[0].message.startsWith('File ignored')
  );
}

async function main(argv) {
  if (argv.some((arg) => arg.startsWith('-'))) {
    console.error(USAGE);
    return 2;
  }
  const eslint = new ESLint({ cwd: CLIENT_ROOT });
  const paths = argv.length > 0 ? argv.map((path) => resolve(path)) : [CLIENT_ROOT];
  const results = await eslint.lintFiles(paths);
  if (results.length > 0 && results.every(isIgnoredFile)) {
    console.error(
      `lint-by-area.mjs: every path given is ignored by the eslint config: ${argv.join(' ')}`,
    );
    return 2;
  }
  const areas = tallyByArea(results, CLIENT_ROOT);
  if (areas.size === 0) {
    console.log(`No lint errors in ${results.length} files.`);
    return 0;
  }
  console.log(renderTable(areas));
  return 1;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).then(
    (code) => process.exit(code),
    (error) => {
      console.error(`lint-by-area.mjs: ${error.message}`);
      process.exit(2);
    },
  );
}
