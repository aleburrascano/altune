import * as fs from 'fs';
import * as path from 'path';

const API_CLIENT_DIR = path.join(__dirname, '..');

function findMatchingBrace(text: string, openIndex: number): number {
  let depth = 0;
  for (let i = openIndex; i < text.length; i += 1) {
    if (text[i] === '{') depth += 1;
    else if (text[i] === '}') {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  throw new Error(`unbalanced braces starting at ${openIndex}`);
}

function extractFunctionBody(source: string, marker: string): string {
  const start = source.indexOf(marker);
  if (start === -1) throw new Error(`marker not found: ${marker}`);
  const braceStart = source.indexOf('{', start);
  const braceEnd = findMatchingBrace(source, braceStart);
  return source.slice(braceStart + 1, braceEnd);
}

function listApiClientSourceFiles(): string[] {
  return fs
    .readdirSync(API_CLIENT_DIR)
    .filter((f) => f.endsWith('.ts') && !f.endsWith('.test.ts'))
    .map((f) => path.join(API_CLIENT_DIR, f));
}

function readIndexSource(): string {
  return fs.readFileSync(path.join(API_CLIENT_DIR, 'index.ts'), 'utf8');
}

describe('apiFetch is the single fetch wrapper', () => {
  it('is the only file in this slice with a bare fetch( call site', () => {
    const offenders: string[] = [];
    let indexFetchCount = 0;
    for (const filePath of listApiClientSourceFiles()) {
      const source = fs.readFileSync(filePath, 'utf8');
      const matches = source.match(/\bfetch\(/g) ?? [];
      if (path.basename(filePath) === 'index.ts') {
        indexFetchCount = matches.length;
      } else if (matches.length > 0) {
        offenders.push(path.basename(filePath));
      }
    }

    expect(offenders).toEqual([]);
    expect(indexFetchCount).toBe(1);
  });

  it('the one fetch( call site in index.ts is inside send(), nowhere else', () => {
    const indexSource = readIndexSource();
    const sendBody = extractFunctionBody(indexSource, 'async function send(');

    expect((sendBody.match(/\bfetch\(/g) ?? []).length).toBe(1);

    const outsideSend = indexSource.replace(sendBody, '');
    expect((outsideSend.match(/\bfetch\(/g) ?? []).length).toBe(0);
  });
});

describe('every request gets a deadline', () => {
  it('the single fetch( call always passes deadline.signal as its abort signal', () => {
    const indexSource = readIndexSource();
    const sendBody = extractFunctionBody(indexSource, 'async function send(');

    expect(sendBody).toMatch(/fetch\([^)]*signal:\s*deadline\.signal/);
  });

  it('apiFetch always constructs a deadline via startDeadline before sending', () => {
    const indexSource = readIndexSource();
    const apiFetchBody = extractFunctionBody(indexSource, 'export async function apiFetch<T>(');

    expect(apiFetchBody).toMatch(/startDeadline\(/);
    expect(apiFetchBody).toMatch(/\bsend\(/);
  });
});

describe('retry policy lives only in the QueryClient predicate, never inside apiFetch', () => {
  it('index.ts contains no loop construct around the fetch call', () => {
    const indexSource = readIndexSource();

    expect(indexSource).not.toMatch(/\bfor\s*\(/);
    expect(indexSource).not.toMatch(/\bwhile\s*\(/);
  });

  it('send() never re-invokes itself and apiFetch() never re-invokes itself', () => {
    const indexSource = readIndexSource();
    const sendBody = extractFunctionBody(indexSource, 'async function send(');
    const apiFetchBody = extractFunctionBody(indexSource, 'export async function apiFetch<T>(');

    expect(sendBody).not.toMatch(/\bsend\(/);
    expect(apiFetchBody).not.toMatch(/\bapiFetch(<[^>]*>)?\(/);
  });

  it('the mobile QueryClient wires isRetryable into its retry predicate', () => {
    const layoutSource = fs.readFileSync(
      path.join(__dirname, '..', '..', '..', 'app', '_layout.tsx'),
      'utf8',
    );

    expect(layoutSource).toMatch(/isRetryable/);
    expect(layoutSource).toMatch(/retry:\s*\([^)]*\)\s*=>[^\n]*isRetryable/);
  });
});

describe('dependency direction: api-client imports nothing from shared/telemetry or features/*', () => {
  it('no file in this slice imports from shared/telemetry or a features/* module', () => {
    const offenders: string[] = [];
    for (const filePath of listApiClientSourceFiles()) {
      const source = fs.readFileSync(filePath, 'utf8');
      const importsTelemetry = /from\s+['"][^'"]*\btelemetry\/[^'"]*['"]/.test(source);
      const importsFeature = /from\s+['"][^'"]*\bfeatures\/[^'"]*['"]/.test(source);
      if (importsTelemetry || importsFeature) offenders.push(path.basename(filePath));
    }

    expect(offenders).toEqual([]);
  });
});

describe('security: this slice never decodes, splits, or otherwise manipulates the access token', () => {
  it("contains no atob, no jwt decoding, and no split('.') anywhere in the slice", () => {
    const offenders: string[] = [];
    for (const filePath of listApiClientSourceFiles()) {
      const source = fs.readFileSync(filePath, 'utf8');
      const hasAtob = /\batob\(/.test(source);
      const hasJwtDecode = /\bjwt[A-Za-z]*\s*\(/i.test(source) || /jwt[-_]?decode/i.test(source);
      const hasDotSplit = /\.split\(\s*['"]\.['"]\s*\)/.test(source);
      if (hasAtob || hasJwtDecode || hasDotSplit) offenders.push(path.basename(filePath));
    }

    expect(offenders).toEqual([]);
  });
});
