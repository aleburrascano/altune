import * as fs from 'fs';
import * as path from 'path';

import { apiClientPath, apiClientSourceFiles } from '@shared/apiClient/__tests__/apiClientSources';

const FETCH_WRAPPER_FILES = ['index.ts', path.join('adapters', 'http.ts')];
const BARE_FETCH_CALL_PATTERN = /\bfetch\(/;
const JSON_CONTENT_TYPE_PATTERN = /application\/json/;

function isFetchWrapper(filePath: string): boolean {
  return FETCH_WRAPPER_FILES.includes(apiClientPath(filePath));
}

function endpointFiles(): string[] {
  return apiClientSourceFiles().filter((filePath) => !isFetchWrapper(filePath));
}

describe('apiFetch is the single fetch wrapper', () => {
  it('is the only file in this slice with a bare fetch( call site', () => {
    const offenders: string[] = [];
    for (const filePath of endpointFiles()) {
      if (BARE_FETCH_CALL_PATTERN.test(fs.readFileSync(filePath, 'utf8'))) {
        offenders.push(apiClientPath(filePath));
      }
    }

    expect(offenders).toEqual([]);
  });
});

describe('apiSend is the single JSON mutation builder', () => {
  it('names the JSON content type in no file of the slice but the fetch wrapper', () => {
    const namingFiles = endpointFiles()
      .filter((filePath) => JSON_CONTENT_TYPE_PATTERN.test(fs.readFileSync(filePath, 'utf8')))
      .map(apiClientPath);

    expect(namingFiles).toEqual([]);
  });

  it('serializes every request body in the fetch wrapper, so no call site stringifies its own', () => {
    const offenders = endpointFiles()
      .filter((filePath) => /JSON\.stringify\(/.test(fs.readFileSync(filePath, 'utf8')))
      .map(apiClientPath);

    expect(offenders).toEqual([]);
  });
});

describe('security: this slice never decodes, splits, or otherwise manipulates the access token', () => {
  it("contains no atob, no jwt decoding, and no split('.') anywhere in the slice", () => {
    const offenders: string[] = [];
    for (const filePath of apiClientSourceFiles()) {
      const source = fs.readFileSync(filePath, 'utf8');
      const hasAtob = /\batob\(/.test(source);
      const hasJwtDecode = /\bjwt[A-Za-z]*\s*\(/i.test(source) || /jwt[-_]?decode/i.test(source);
      const hasDotSplit = /\.split\(\s*['"]\.['"]\s*\)/.test(source);
      if (hasAtob || hasJwtDecode || hasDotSplit) offenders.push(apiClientPath(filePath));
    }

    expect(offenders).toEqual([]);
  });
});
