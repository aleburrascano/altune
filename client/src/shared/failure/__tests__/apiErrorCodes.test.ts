import * as fs from 'fs';
import * as path from 'path';

import { API_ERROR_CODES, isApiErrorCode } from '@shared/failure';

const CATALOG_RELATIVE_PATH = path.join(
  'services',
  'go-api',
  'internal',
  'shared',
  'apierror',
  'codes.go',
);
const GO_CATALOG_ENTRY = /\bcode\("([a-z0-9_.]+)"/g;
const MAX_HOPS = 12;

function findCatalog(): string | null {
  let dir = __dirname;
  for (let hop = 0; hop < MAX_HOPS; hop += 1) {
    const candidate = path.join(dir, CATALOG_RELATIVE_PATH);
    if (fs.existsSync(candidate)) return candidate;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

const CATALOG_PATH = findCatalog();

describe('the ApiErrorCode union against the server catalog', () => {
  it('finds the server catalog in the checkout', () => {
    expect(CATALOG_PATH).not.toBeNull();
  });

  it('lists exactly the codes the server can send', () => {
    const source = CATALOG_PATH === null ? '' : fs.readFileSync(CATALOG_PATH, 'utf8');
    const serverCodes = [...source.matchAll(GO_CATALOG_ENTRY)].map((match) => match[1]);

    expect([...API_ERROR_CODES].sort()).toEqual([...serverCodes].sort());
  });

  it('has no duplicate code', () => {
    expect(new Set(API_ERROR_CODES).size).toBe(API_ERROR_CODES.length);
  });
});

describe('isApiErrorCode', () => {
  it.each(API_ERROR_CODES)('accepts the catalog code %s', (code) => {
    expect(isApiErrorCode(code)).toBe(true);
  });

  it.each<unknown>([
    '',
    'discovery.unheard_of',
    'INTERNAL',
    true,
    null,
    undefined,
    {},
    ['internal'],
  ])('rejects %p', (value) => {
    expect(isApiErrorCode(value)).toBe(false);
  });
});
