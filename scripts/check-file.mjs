import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join, relative, resolve } from 'node:path';

const MAX_REPORTED = 20;
const ERROR = 2;

const CHECKS = { eslint: eslintFindings, prettier: prettierFindings };

const [checkName, projectArg, fileArg] = process.argv.slice(2);
if (!Object.hasOwn(CHECKS, checkName ?? '') || !projectArg || !fileArg) {
  console.error(`usage: node scripts/check-file.mjs <${Object.keys(CHECKS).join('|')}> <js-project-dir> <file>`);
  process.exit(2);
}
const projectDir = resolve(projectArg);
const file = resolve(fileArg);
const shown = relative(process.cwd(), file);
const requireFromProject = createRequire(join(projectDir, 'package.json'));

function load(name) {
  try {
    return requireFromProject(name);
  } catch (error) {
    if (error.code !== 'MODULE_NOT_FOUND') throw error;
    return null;
  }
}

function committedSource() {
  try {
    return execFileSync('git', ['show', `HEAD:./${relative(projectDir, file)}`], {
      cwd: projectDir,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
      maxBuffer: 64 << 20,
    });
  } catch {
    return null;
  }
}

const findingKey = (message) => `${message.ruleId}\0${message.message}`;

function withoutCommitted(now, before) {
  const left = new Map();
  for (const message of before) left.set(findingKey(message), (left.get(findingKey(message)) ?? 0) + 1);
  return now.filter((message) => {
    const count = left.get(findingKey(message)) ?? 0;
    if (count === 0) return true;
    left.set(findingKey(message), count - 1);
    return false;
  });
}

const errorsOf = (result) => result.messages.filter((m) => m.fatal || m.severity === ERROR);

async function eslintFindings() {
  const eslintModule = load('eslint');
  if (!eslintModule) {
    return { missing: `eslint is not installed in ${relative(process.cwd(), projectDir)}/node_modules` };
  }
  const tsPlugin = load('@typescript-eslint/eslint-plugin');
  const untyped = tsPlugin?.configs['flat/disable-type-checked'];
  const eslint = new eslintModule.ESLint({ cwd: projectDir, ...(untyped && { overrideConfig: untyped }) });
  if (await eslint.isPathIgnored(file)) return { lines: [] };
  const [result] = await eslint.lintFiles([file]);
  let errors = errorsOf(result);
  const before = errors.length > 0 ? committedSource() : null;
  if (before !== null) {
    const [old] = await eslint.lintText(before, { filePath: file });
    errors = withoutCommitted(errors, errorsOf(old));
  }
  return {
    lines: errors.map((m) => `${shown}:${m.line ?? 0}:${m.column ?? 0}  ${m.message}  (${m.ruleId ?? 'parse'})`),
  };
}

async function prettierFindings() {
  const prettier = load('prettier');
  if (!prettier) return { lines: [] };
  const info = await prettier.getFileInfo(file, { ignorePath: join(projectDir, '.prettierignore') });
  const config = await prettier.resolveConfig(file);
  if (info.ignored || !info.inferredParser || config === null) return { lines: [] };
  const options = { ...config, filepath: file };
  const formatted = (source) => prettier.check(source, options).catch(() => false);
  try {
    if (await prettier.check(readFileSync(file, 'utf8'), options)) return { lines: [] };
  } catch (error) {
    return { lines: [`${shown}: Prettier cannot parse it: ${String(error.message).split('\n')[0]}`] };
  }
  const before = committedSource();
  if (before !== null && !(await formatted(before))) return { lines: [] };
  return {
    lines: [`${shown}: not Prettier-formatted. Fix: (cd ${relative(process.cwd(), projectDir)} && npx prettier --write ${relative(projectDir, file)})`],
  };
}

const { lines = [], missing } = await CHECKS[checkName]();

if (missing) {
  console.error(`${missing}. Run: bash scripts/worktree-deps.sh ${relative(process.cwd(), projectDir)}`);
  process.exit(3);
}
if (lines.length > 0) {
  console.error(lines.slice(0, MAX_REPORTED).join('\n'));
  if (lines.length > MAX_REPORTED) console.error(`...and ${lines.length - MAX_REPORTED} more`);
  process.exit(1);
}
