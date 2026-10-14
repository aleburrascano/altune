import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const defaultRoot = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');

const FEATURE_FOLDERS = ['domain', 'adapters', 'hooks', 'ui'];
const OPTIONAL_ROOT_FOLDERS = ['ports', '__tests__', 'native', 'web'];
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

function namedEntries(root) {
  return ['src/features', 'src/shared']
    .flatMap((top) => walkEntries(path.join(root, top)))
    .map(({ full, entry }) => ({ full, entry, relative: rel(root, full) }))
    .filter(({ entry, relative }) => !entry.name.startsWith('.') && !inTests(relative));
}

function checkN3(root) {
  const found = [];
  for (const { full, entry, relative } of namedEntries(root)) {
    const wanted = casingOf(full, entry);
    if (wanted) found.push(`N3 ${relative} is not ${wanted}`);
  }
  return found;
}

const LAYER_FOLDERS = ['domain', 'ports', 'adapters', 'hooks', 'ui', 'state', 'native', 'web'];
const SPLIT_LAYERS = ['hooks', 'ui', 'adapters'];
const FLAT_EXEMPT_FOLDERS = new Set(['__tests__', 'native', 'web']);
const SRC_TOP_LEVEL = new Set(['app', 'shell', 'wiring', 'features', 'shared']);
const SOURCE_CAP = 16;
const N4_WORDS = new Set([
  'manager',
  'helper',
  'util',
  'common',
  'misc',
  'stuff',
  'data',
  'info',
  'process',
  'processor',
  'logic',
  'parts',
  'extras',
  'wrapper',
  'thing',
  'handler',
]);
const EXPORTED_NAME =
  /^export\s+(?:default\s+)?(?:async\s+)?(?:function\*?|const|let|class|interface|type|enum)\s+(\w+)/gm;

const isSourceFile = (entry) =>
  entry.isFile() && SOURCE_FILE.test(entry.name) && !TEST_FILE.test(entry.name);
const isPlatformFolder = (entry) => entry.isDirectory() && FLAT_EXEMPT_FOLDERS.has(entry.name);
const isFolder = (entry) => entry.isDirectory() && entry.name !== 'node_modules';

function wordsOf(name) {
  return name
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1 $2')
    .split(/[^A-Za-z0-9]+/)
    .filter(Boolean);
}

const nameWords = (entryName) => wordsOf(entryName.split('.')[0]);

function singularForms(word) {
  const lower = word.toLowerCase();
  return [lower, lower.replace(/es$/, ''), lower.replace(/s$/, '')];
}

const matchesAny = (set, word) => singularForms(word).some((form) => set.has(form));

function sharedFolders(root) {
  return listDir(path.join(root, 'src/shared')).filter(
    (entry) => isFolder(entry) && entry.name !== '__tests__',
  );
}

function isLayeredShared(root, name) {
  return fs.existsSync(path.join(root, 'src/shared', name, 'ports'));
}

function layerScopes(root) {
  return [
    ...featureNames(root).map((feature) => ({ base: `src/features/${feature}`, isFeature: true })),
    ...sharedFolders(root)
      .filter((entry) => isLayeredShared(root, entry.name))
      .map((entry) => ({ base: `src/shared/${entry.name}`, isFeature: false })),
  ];
}

function nestedSourceFiles(root, folder, depthFromLayer) {
  return walk(folder).filter((file) => {
    const relative = rel(root, file);
    const depth = relative.slice(rel(root, folder).length).split('/').length - 1;
    return (
      depth > depthFromLayer &&
      !inTests(relative) &&
      SOURCE_FILE.test(file) &&
      !TEST_FILE.test(file) &&
      fs.statSync(file).isFile()
    );
  });
}

function checkM8(root) {
  const reserved = new Set([...LAYER_FOLDERS, ...featureNames(root)]);
  const found = [];
  for (const { base, isFeature } of layerScopes(root)) {
    for (const layer of SPLIT_LAYERS) {
      const layerDir = path.join(root, base, layer);
      for (const entry of listDir(layerDir)) {
        if (entry.name === '__tests__') continue;
        const where = `${base}/${layer}/${entry.name}`;
        if (entry.isFile()) {
          if (entry.name !== 'index.ts' && !TEST_FILE.test(entry.name))
            found.push(`M8 ${where} loose file in ${layer}/`);
          continue;
        }
        if (reserved.has(entry.name)) found.push(`M8 ${where} folder named for a layer or feature`);
        for (const file of nestedSourceFiles(root, path.join(layerDir, entry.name), 1)) {
          found.push(`M8 ${rel(root, file)} nested more than one folder below ${layer}/`);
        }
        const hasUiTwin = fs.existsSync(path.join(root, base, 'ui', entry.name));
        if (isFeature && layer === 'hooks' && !hasUiTwin)
          found.push(`M8 ${where} has no ui/${entry.name}/`);
      }
    }
  }
  return found;
}

function sourceFolders(root) {
  return ['src/features', 'src/shared'].flatMap((top) =>
    walkEntries(path.join(root, top))
      .filter(({ full, entry }) => isFolder(entry) && !inTests(rel(root, full)))
      .filter(({ entry }) => !FLAT_EXEMPT_FOLDERS.has(entry.name))
      .map(({ full }) => full),
  );
}

function checkM9(root) {
  const found = [];
  for (const folder of sourceFolders(root)) {
    const relative = rel(root, folder);
    const entries = listDir(folder);
    const files = entries.filter((e) => isSourceFile(e) && e.name !== 'index.ts');
    const folders = entries.filter((e) => isFolder(e) && !isPlatformFolder(e));
    const stems = new Set(files.map((e) => stem(e.name)));
    if (files.length > 0 && folders.length > 0)
      found.push(`M9 ${relative} holds both files and folders`);
    if (stems.size > SOURCE_CAP)
      found.push(`M9 ${relative} holds more than ${SOURCE_CAP} source files`);
  }
  return found;
}

function checkM10(root) {
  const found = [];
  for (const entry of listDir(path.join(root, 'src/shared'))) {
    if (entry.isFile()) found.push(`M10 src/shared/${entry.name} file at the shared root`);
  }
  for (const entry of sharedFolders(root)) {
    const base = `src/shared/${entry.name}`;
    const children = listDir(path.join(root, base));
    if (isLayeredShared(root, entry.name)) {
      for (const child of children) {
        const allowed = child.isFile()
          ? child.name === 'index.ts'
          : LAYER_FOLDERS.includes(child.name) || child.name === '__tests__';
        if (!allowed) found.push(`M10 ${base}/${child.name} not a layer folder or index.ts`);
      }
      continue;
    }
    for (const child of children) {
      if (entry.name === 'ui' && child.isFile() && child.name !== 'index.ts')
        found.push(`M10 ${base}/${child.name} loose file in shared/ui`);
      if (entry.name !== 'ui' && isFolder(child) && !FLAT_EXEMPT_FOLDERS.has(child.name))
        found.push(`M10 ${base}/${child.name} folder inside a flat shared folder`);
    }
  }
  return found;
}

function checkM11(root) {
  return listDir(path.join(root, 'src'))
    .filter((entry) => !SRC_TOP_LEVEL.has(entry.name) && entry.name !== '__tests__')
    .filter((entry) => !entry.name.startsWith('.'))
    .map((entry) => `M11 src/${entry.name} not an allowed top-level entry`);
}

function sectionOf(markdown, heading) {
  const start = markdown.search(new RegExp(`^## ${heading}\\s*$`, 'm'));
  if (start < 0) return '';
  const rest = markdown.slice(start).split('\n').slice(1);
  const end = rest.findIndex((line) => /^## /.test(line));
  return (end < 0 ? rest : rest.slice(0, end)).join('\n');
}

function boldWords(section) {
  return section
    .split('\n')
    .filter((line) => line.startsWith('- '))
    .flatMap((line) => [...line.matchAll(/\*\*([^*]+)\*\*/g)].flatMap((m) => wordsOf(m[1])))
    .map((word) => word.toLowerCase());
}

function readGlossary(file) {
  const markdown = fs.readFileSync(file, 'utf8');
  const structural = [...sectionOf(markdown, 'Structural words').matchAll(/`([^`]+)`/g)].map((m) =>
    m[1].toLowerCase(),
  );
  return {
    terms: new Set([...boldWords(sectionOf(markdown, 'Glossary')), ...structural]),
    banned: new Set(boldWords(sectionOf(markdown, 'Banned terms'))),
  };
}

function bannedNameWord(name, glossary) {
  const words = nameWords(name);
  const word = words.find(
    (candidate) => matchesAny(N4_WORDS, candidate) && !matchesAny(glossary.terms, candidate),
  );
  if (word) return word;
  const last = words[words.length - 1];
  if (words.length < 2) return null;
  return /^v\d+$/i.test(last) || /^[AB]$/.test(last) ? last : null;
}

function checkN4(root, glossary) {
  const found = [];
  for (const { full, entry, relative } of namedEntries(root)) {
    if (TEST_FILE.test(entry.name)) continue;
    const bad = bannedNameWord(entry.name, glossary);
    if (bad) found.push(`N4 ${relative} name has the word ${bad}`);
    if (!entry.isFile() || !SOURCE_FILE.test(entry.name)) continue;
    for (const match of fs.readFileSync(full, 'utf8').matchAll(EXPORTED_NAME)) {
      const badExport = bannedNameWord(match[1], glossary);
      if (badExport) found.push(`N4 ${relative} exports ${match[1]} with the word ${badExport}`);
    }
  }
  return found;
}

function structuralFolders(root) {
  const features = featureNames(root).map((feature) => `src/features/${feature}`);
  const shared = sharedFolders(root).map((entry) => `src/shared/${entry.name}`);
  const capabilities = layerScopes(root).flatMap(({ base }) =>
    SPLIT_LAYERS.flatMap((layer) =>
      listDir(path.join(root, base, layer))
        .filter((entry) => isFolder(entry) && entry.name !== '__tests__')
        .map((entry) => `${base}/${layer}/${entry.name}`),
    ),
  );
  return [...features, ...shared, ...capabilities];
}

function checkN5(root, glossary) {
  const found = [];
  for (const folder of structuralFolders(root)) {
    for (const word of nameWords(path.basename(folder))) {
      if (matchesAny(glossary.banned, word)) found.push(`N5 ${folder} uses banned term ${word}`);
      else if (!matchesAny(glossary.terms, word))
        found.push(`N5 ${folder} word ${word} is not in the glossary`);
    }
  }
  return found;
}

export function checkModuleShape(
  root = defaultRoot,
  goApiDir = path.join(root, '../services/go-api/internal'),
  glossaryFile = path.join(root, '../docs/ubiquitous-language.md'),
) {
  const hasGlossary = fs.existsSync(glossaryFile);
  const glossary = hasGlossary
    ? readGlossary(glossaryFile)
    : { terms: new Set(), banned: new Set() };
  return [
    ...checkM1(root),
    ...checkM2(root),
    ...checkM4(root),
    ...checkM8(root),
    ...checkM9(root),
    ...checkM10(root),
    ...checkM11(root),
    ...checkM7(root),
    ...checkN1(root),
    ...checkN2(root, goApiDir),
    ...checkN3(root),
    ...checkN4(root, glossary),
    ...(hasGlossary ? checkN5(root, glossary) : []),
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
  const glossaryFile = option('--glossary') ? path.resolve(root, option('--glossary')) : undefined;
  const baselineFile = path.join(root, 'baselines', 'module-shape.baseline');
  const violations = checkModuleShape(root, goApiDir, glossaryFile);

  if (args.includes('--write')) {
    fs.mkdirSync(path.dirname(baselineFile), { recursive: true });
    fs.writeFileSync(baselineFile, violations.length ? `${violations.join('\n')}\n` : '');
    console.log(
      `module-shape: wrote ${violations.length} violations to baselines/module-shape.baseline`,
    );
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
