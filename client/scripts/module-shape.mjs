import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const defaultRoot = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');

const FEATURE_FOLDERS = ['domain', 'ports', 'adapters', 'hooks', 'ui'];
const OPTIONAL_ROOT_FOLDERS = ['__tests__', 'native', 'web'];
const FORBIDDEN_NAMES = new Set([
  'util',
  'utils',
  'helper',
  'helpers',
  'common',
  'misc',
  'lib',
  'core',
  'manager',
  'base',
  'generic',
  'data',
  'stuff',
  'tmp',
  'temp',
  'new',
  'old',
  'types',
  'constants',
]);
const SOURCE_FILE = /\.(ts|tsx)$/;
const TEST_FILE = /\.test\.(ts|tsx)$/;
const SPECIFIER = /(?:from\s*|import\s*\(?\s*|require\(\s*)['"]([^'"]+)['"]/g;
const DEFAULT_EXPORT = /^export\s+default\s+(?:async\s+)?(?:function\s+|class\s+|memo\()?(\w+)/gm;
const VALUE_EXPORT = /^export\s+(?:async\s+)?(?:function|const|class)\s+(\w+)/gm;
const CAMEL_CASE = /^[a-z][a-zA-Z0-9]*$/;
const PASCAL_CASE = /^[A-Z](?=.*[a-z])[a-zA-Z0-9]*$/;
const ALLOWED_SUFFIXES = new Set(['web', 'native', 'android', 'ios', 'test', 'd']);
const NEAR_MISS_PREFIX = 5;

function listDir(dir) {
  if (!fs.existsSync(dir)) return [];
  return fs
    .readdirSync(dir, { withFileTypes: true })
    .filter((entry) => entry.name !== 'node_modules');
}

function walk(dir) {
  return listDir(dir).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    return entry.isDirectory() ? [full, ...walk(full)] : [full];
  });
}

const inTests = (relative) => relative.split('/').includes('__tests__');
const stem = (name) => name.split('.')[0];

function featureNames(root) {
  return listDir(path.join(root, 'src/features'))
    .filter((entry) => entry.isDirectory() && entry.name !== '__tests__')
    .map((entry) => entry.name);
}

function rel(root, file) {
  return path.relative(root, file).split(path.sep).join('/');
}

function checkM1(root) {
  return featureNames(root).flatMap((feature) => {
    const base = `src/features/${feature}`;
    const entries = listDir(path.join(root, base));
    const folders = entries.filter((e) => e.isDirectory()).map((e) => e.name);
    const found = [];
    for (const folder of FEATURE_FOLDERS) {
      if (!folders.includes(folder)) found.push(`M1 ${base} missing ${folder}/`);
    }
    for (const folder of folders) {
      if (![...FEATURE_FOLDERS, ...OPTIONAL_ROOT_FOLDERS].includes(folder)) {
        found.push(`M1 ${base}/${folder} unexpected root folder`);
      }
    }
    if (!entries.some((e) => e.isFile() && e.name === 'index.ts'))
      found.push(`M1 ${base} missing index.ts`);
    for (const entry of entries) {
      if (entry.isFile() && entry.name !== 'index.ts')
        found.push(`M1 ${base}/${entry.name} loose root file`);
    }
    return found;
  });
}

function importsOf(file) {
  return [...fs.readFileSync(file, 'utf8').matchAll(SPECIFIER)].map((m) => m[1]);
}

function checkM2(root) {
  const found = [];
  const scopes = [
    { dir: 'src/app', owner: null },
    ...featureNames(root).map((feature) => ({ dir: `src/features/${feature}`, owner: feature })),
  ];
  for (const { dir, owner } of scopes) {
    for (const file of walk(path.join(root, dir))) {
      if (!SOURCE_FILE.test(file)) continue;
      for (const specifier of new Set(importsOf(file))) {
        const match = /^@features\/([^/]+)\/.+/.exec(specifier);
        if (!match || match[1] === owner) continue;
        found.push(`M2 ${rel(root, file)} deep import ${specifier}`);
      }
    }
  }
  return found;
}

function checkM4(root) {
  const found = [];
  for (const feature of featureNames(root)) {
    const base = path.join(root, 'src/features', feature);
    for (const file of walk(base)) {
      const relative = rel(root, file);
      if (!SOURCE_FILE.test(file) || inTests(relative) || !fs.statSync(file).isFile()) continue;
      const name = path.basename(file);
      const inHooks = relative.startsWith(`src/features/${feature}/hooks/`);
      const isHook = /^use[A-Z]/.test(name);
      if (isHook && !inHooks) found.push(`M4 ${relative} use* file outside hooks/`);
      if (!isHook && inHooks && !TEST_FILE.test(name))
        found.push(`M4 ${relative} non-use* file in hooks/`);
    }
  }
  return found;
}

function checkM7(root) {
  const factories = path.join(root, 'jest/factories.ts');
  if (!fs.existsSync(factories)) return [];
  const exported = [...fs.readFileSync(factories, 'utf8').matchAll(/^export const (\w+)/gm)].map(
    (m) => m[1],
  );
  const imported = new Set();
  for (const file of walk(path.join(root, 'src'))) {
    if (!SOURCE_FILE.test(file)) continue;
    const source = fs.readFileSync(file, 'utf8');
    for (const match of source.matchAll(
      /import\s+(?:type\s+)?\{([^}]*)\}\s*from\s*['"]@testing\/factories['"]/g,
    )) {
      for (const part of match[1].split(',')) imported.add(part.trim().split(/\s+as\s+/)[0]);
    }
  }
  return exported
    .filter((name) => !imported.has(name))
    .map((name) => `M7 jest/factories.ts ${name} has no importer under src/`);
}

function checkN1(root) {
  const found = [];
  for (const top of ['src/features', 'src/shared']) {
    for (const file of walk(path.join(root, top))) {
      const relative = rel(root, file);
      if (inTests(relative) || TEST_FILE.test(file)) continue;
      const name = path.basename(file);
      if (FORBIDDEN_NAMES.has(stem(name).toLowerCase())) found.push(`N1 ${relative} generic name`);
    }
  }
  return found;
}

function checkN2(root, goApiDir) {
  const modules = listDir(goApiDir)
    .filter((e) => e.isDirectory() && !['app', 'shared'].includes(e.name))
    .map((e) => e.name);
  const found = [];
  for (const feature of featureNames(root)) {
    if (modules.includes(feature)) continue;
    const near = modules.find(
      (m) =>
        feature.slice(0, NEAR_MISS_PREFIX) === m.slice(0, NEAR_MISS_PREFIX) &&
        feature.length >= NEAR_MISS_PREFIX,
    );
    if (near) found.push(`N2 src/features/${feature} go-api module is ${near}`);
  }
  return found;
}

function mainExports(source, base) {
  const defaults = [...source.matchAll(DEFAULT_EXPORT)].map((m) => m[1]);
  if (defaults.length > 0) return defaults.slice(0, 1);
  const values = [...source.matchAll(VALUE_EXPORT)].map((m) => m[1]);
  const named = values.filter((name) => name.toLowerCase() === base.toLowerCase());
  return named.length > 0 ? named : values;
}

function isComponentFile(file, base) {
  if (!file.endsWith('.tsx')) return false;
  const main = mainExports(fs.readFileSync(file, 'utf8'), base);
  return main.length > 0 && main.every((name) => PASCAL_CASE.test(name));
}

function casingOf(file, entry) {
  if (entry.isDirectory()) return CAMEL_CASE.test(entry.name) ? null : 'camelCase';
  const [base, ...suffixes] = entry.name.split('.');
  if (!SOURCE_FILE.test(entry.name) || base === 'index') return null;
  if (suffixes.slice(0, -1).some((suffix) => !ALLOWED_SUFFIXES.has(suffix))) return 'camelCase';
  const wanted = isComponentFile(file, base) ? 'PascalCase' : 'camelCase';
  const pattern = wanted === 'PascalCase' ? PASCAL_CASE : CAMEL_CASE;
  return pattern.test(base) ? null : wanted;
}

function walkEntries(dir) {
  return listDir(dir).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    return entry.isDirectory() ? [{ full, entry }, ...walkEntries(full)] : [{ full, entry }];
  });
}

function checkN3(root) {
  const found = [];
  for (const top of ['src/features', 'src/shared']) {
    for (const { full, entry } of walkEntries(path.join(root, top))) {
      const relative = rel(root, full);
      if (entry.name.startsWith('.') || inTests(relative)) continue;
      const wanted = casingOf(full, entry);
      if (wanted) found.push(`N3 ${relative} is not ${wanted}`);
    }
  }
  return found;
}

export function checkModuleShape(
  root = defaultRoot,
  goApiDir = path.join(root, '../services/go-api/internal'),
) {
  return [
    ...checkM1(root),
    ...checkM2(root),
    ...checkM4(root),
    ...checkM7(root),
    ...checkN1(root),
    ...checkN2(root, goApiDir),
    ...checkN3(root),
  ]
    .filter((line, index, all) => all.indexOf(line) === index)
    .sort();
}

function readBaseline(file) {
  if (!fs.existsSync(file)) return [];
  return fs
    .readFileSync(file, 'utf8')
    .split('\n')
    .filter((line) => line.trim() !== '');
}

function main() {
  const args = process.argv.slice(2);
  const option = (flag) => (args.includes(flag) ? args[args.indexOf(flag) + 1] : undefined);
  const root = option('--root') ? path.resolve(option('--root')) : defaultRoot;
  const goApiDir = option('--go-api') ? path.resolve(option('--go-api')) : undefined;
  const baselineFile = path.join(root, 'module-shape.baseline');
  const violations = checkModuleShape(root, goApiDir);

  if (args.includes('--write')) {
    fs.writeFileSync(baselineFile, violations.length ? `${violations.join('\n')}\n` : '');
    console.log(`module-shape: wrote ${violations.length} violations to module-shape.baseline`);
    return;
  }

  const baseline = new Set(readBaseline(baselineFile));
  const current = new Set(violations);
  const added = violations.filter((line) => !baseline.has(line));
  const fixed = [...baseline].filter((line) => !current.has(line));
  for (const line of added) console.log(`new violation: ${line}`);
  for (const line of fixed) console.log(`fixed: remove it from the baseline: ${line}`);
  if (added.length > 0 || fixed.length > 0) process.exit(1);
  console.log(`module-shape: ${violations.length} violations, all in baseline`);
}

if (import.meta.url === `file://${process.argv[1]}`) main();
