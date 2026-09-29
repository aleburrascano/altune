import * as fs from 'node:fs';
import * as path from 'node:path';
import { execFileSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';
import { loadRules } from '../../eslint/consistency';

const mobileRoot = path.join(__dirname, '..', '..');
const ratchetScript = path.join(mobileRoot, 'scripts', 'consistency-ratchet.mjs');
const ratchetUrl = pathToFileURL(ratchetScript).href;

async function lintSnippet(rule: ReturnType<typeof loadRules>[number], source: string) {
  const bridge = [
    `const { lintSource } = await import(${JSON.stringify(ratchetUrl)});`,
    `const messages = await lintSource(${JSON.stringify(rule)}, ${JSON.stringify(source)}, ${JSON.stringify(`${rule.id}.tsx`)});`,
    'process.stdout.write(JSON.stringify(messages));',
  ].join('\n');
  const output = execFileSync('node', ['--input-type=module', '-e', bridge], {
    cwd: mobileRoot,
    encoding: 'utf8',
  });
  return JSON.parse(output) as { message: string; ruleId?: string | null }[];
}

function runRatchetCli() {
  try {
    const stdout = execFileSync('node', [ratchetScript], { cwd: mobileRoot, encoding: 'utf8' });
    return { status: 0, stdout };
  } catch (error) {
    const failure = error as { status: number | null; stdout?: string; stderr?: string };
    return { status: failure.status, stdout: `${failure.stdout ?? ''}${failure.stderr ?? ''}` };
  }
}

const rules = loadRules();

test('at least one rule module is registered', () => {
  expect(rules.length).toBeGreaterThan(0);
});

describe.each(rules)('$id', (rule) => {
  test('flags every invalid example', async () => {
    for (const invalid of rule.examples.invalid) {
      const messages = await lintSnippet(rule, invalid);
      expect(messages.length).toBeGreaterThan(0);
    }
  });

  test('passes every valid example', async () => {
    for (const valid of rule.examples.valid) {
      const messages = await lintSnippet(rule, valid);
      expect(messages).toHaveLength(0);
    }
  });
});

test('checklist doc lists MC-1 through MC-7', () => {
  const doc = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'docs', 'conventions', 'mobile-consistency.md'),
    'utf8',
  );
  for (let n = 1; n <= 7; n += 1) {
    expect(doc).toMatch(new RegExp(`MC-${n}\\b`));
  }
});

test('running the real script exits 0 on the current src tree, one line per rule module', () => {
  const { status, stdout } = runRatchetCli();

  expect(status).toBe(0);
  for (const rule of rules) {
    expect(stdout).toMatch(new RegExp(`^${rule.id}: \\d+ / baseline`, 'm'));
  }
});

test('running the real script reports MC-1 at its 9 supabaseClient imports and no raw network', () => {
  const { status, stdout } = runRatchetCli();

  expect(status).toBe(0);
  expect(stdout).toMatch(/^MC-1: 9 \/ baseline 9$/m);
});

test('running the real script exits 1 and names MC-1 and the file when a raw fetch is added', () => {
  const relativePath = path.join('src', '__consistency_ratchet_fixture__.ts');
  const fixturePath = path.join(mobileRoot, relativePath);
  fs.writeFileSync(
    fixturePath,
    "export function f() {\n  return fetch('https://example.com');\n}\n",
  );

  try {
    const { status, stdout } = runRatchetCli();

    expect(status).toBe(1);
    expect(stdout).toContain('MC-1');
    expect(stdout).toContain(relativePath);
  } finally {
    fs.rmSync(fixturePath, { force: true });
  }
});
