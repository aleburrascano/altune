import * as fs from 'fs';
import * as path from 'path';
import * as config from '../config';
import { apiBase } from '../index';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const SRC_ROOT = path.resolve(__dirname, '../../..');
const API_CLIENT_DIR = path.resolve(SRC_ROOT, 'shared/api-client');
const CONFIG_NAMES = ['apiBase', 'REQUEST_TIMEOUT_MS', 'LONG_REQUEST_TIMEOUT_MS'];
const BARREL_SPECIFIER = /^(@shared\/api-client|(\.{1,2}\/)+(.*\/)?api-client)$/;
const IMPORT_STATEMENT = /import\s+(?:type\s+)?\{([^}]*)\}\s*from\s*'([^']+)'/g;

function importsConfigFromBarrel(source: string): boolean {
  for (const match of source.matchAll(IMPORT_STATEMENT)) {
    const names = (match[1] ?? '').split(',').map((n) => n.trim().split(/\s+as\s+/)[0] ?? '');
    if (BARREL_SPECIFIER.test(match[2] ?? '') && names.some((n) => CONFIG_NAMES.includes(n))) {
      return true;
    }
  }
  return false;
}

function isTest(file: string): boolean {
  return /__tests__|\.test\.tsx?$|[\\/]jest[\\/]/.test(file);
}

function sourceFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return full === API_CLIENT_DIR ? [] : sourceFiles(full);
    return /\.tsx?$/.test(entry.name) && !isTest(full) ? [full] : [];
  });
}

describe('api-client config', () => {
  it('exposes the same apiBase as the barrel and the 15s timeout', () => {
    expect(config.apiBase).toBe(apiBase);
    expect(config.REQUEST_TIMEOUT_MS).toBe(15000);
  });

  it('is imported from the leaf by every caller outside the api-client', () => {
    const files = sourceFiles(SRC_ROOT);
    const offenders = files.filter((f) => importsConfigFromBarrel(fs.readFileSync(f, 'utf8')));
    const users = files.filter((f) =>
      /import\s*\{[^}]*\b(apiBase|REQUEST_TIMEOUT_MS)\b[^}]*\}\s*from\s*'[^']*api-client\/config'/.test(
        fs.readFileSync(f, 'utf8'),
      ),
    );
    expect(offenders).toEqual([]);
    expect(users.length).toBeGreaterThanOrEqual(3);
  });

  it('flags barrel imports of config constants', () => {
    expect(importsConfigFromBarrel(`import { apiBase } from '@shared/api-client';`)).toBe(true);
    expect(importsConfigFromBarrel(`import { apiBase } from '../api-client';`)).toBe(true);
  });

  it('allows the leaf and non-config barrel imports', () => {
    expect(importsConfigFromBarrel(`import { apiBase } from '@shared/api-client/config';`)).toBe(
      false,
    );
    expect(importsConfigFromBarrel(`import { apiFetch } from '@shared/api-client';`)).toBe(false);
  });
});
