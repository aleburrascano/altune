import * as fs from 'fs';
import * as path from 'path';

const API_CLIENT_DIR = path.join(__dirname, '..');
const BARE_FETCH_CALL_PATTERN = /\bfetch\(/;
const JSON_CONTENT_TYPE_PATTERN = /application\/json/;

function listApiClientSourceFiles(): string[] {
  return fs
    .readdirSync(API_CLIENT_DIR)
    .filter((f) => f.endsWith('.ts') && !f.endsWith('.test.ts'))
    .map((f) => path.join(API_CLIENT_DIR, f));
}

describe('apiFetch is the single fetch wrapper', () => {
  it('is the only file in this slice with a bare fetch( call site', () => {
    const offenders: string[] = [];
    for (const filePath of listApiClientSourceFiles()) {
      const source = fs.readFileSync(filePath, 'utf8');
      if (path.basename(filePath) !== 'index.ts' && BARE_FETCH_CALL_PATTERN.test(source)) {
        offenders.push(path.basename(filePath));
      }
    }

    expect(offenders).toEqual([]);
  });
});

describe('apiSend is the single JSON mutation builder', () => {
  it('names the JSON content type in no file of the slice but index.ts', () => {
    const namingFiles = listApiClientSourceFiles()
      .filter((filePath) => path.basename(filePath) !== 'index.ts')
      .filter((filePath) => JSON_CONTENT_TYPE_PATTERN.test(fs.readFileSync(filePath, 'utf8')))
      .map((filePath) => path.basename(filePath));

    expect(namingFiles).toEqual([]);
  });

  it('serializes every request body in index.ts, so no call site stringifies its own', () => {
    const offenders = listApiClientSourceFiles()
      .filter((filePath) => path.basename(filePath) !== 'index.ts')
      .filter((filePath) => /JSON\.stringify\(/.test(fs.readFileSync(filePath, 'utf8')))
      .map((filePath) => path.basename(filePath));

    expect(offenders).toEqual([]);
  });
});

describe('dependency direction: apiClient imports nothing from shared/telemetry or features/*', () => {
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
