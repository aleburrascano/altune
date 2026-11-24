import * as fs from 'fs';
import * as path from 'path';
import * as config from '@shared/apiConfig';

const SRC_ROOT = path.resolve(__dirname, '../../..');
const API_CONFIG_DIR = path.resolve(SRC_ROOT, 'shared/apiConfig');
const CONFIG_NAMES = ['apiBase', 'REQUEST_TIMEOUT_MS', 'LONG_REQUEST_TIMEOUT_MS'];
const BARREL_SPECIFIER = /^(@shared\/apiClient|(\.{1,2}\/)+(.*\/)?apiClient)$/;
const CONFIG_IMPORT =
  /import\s*\{[^}]*\b(apiBase|REQUEST_TIMEOUT_MS)\b[^}]*\}\s*from\s*'@shared\/apiConfig'/;
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
  const outsideApiConfig = fs
    .readdirSync(dir, { withFileTypes: true })
    .filter((entry) => path.join(dir, entry.name) !== API_CONFIG_DIR);
  return outsideApiConfig.flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return sourceFiles(full);
    return /\.tsx?$/.test(entry.name) && !isTest(full) ? [full] : [];
  });
}

describe('apiClient config', () => {
  it('exposes the api base and the 15s timeout', () => {
    expect(typeof config.apiBase()).toBe('string');
    expect(config.REQUEST_TIMEOUT_MS).toBe(15000);
  });

  it('is imported from apiConfig by every caller, never from the apiClient barrel', () => {
    const files = sourceFiles(SRC_ROOT);
    const offenders = files.filter((f) => importsConfigFromBarrel(fs.readFileSync(f, 'utf8')));
    const users = files.filter((f) => CONFIG_IMPORT.test(fs.readFileSync(f, 'utf8')));
    expect(offenders).toEqual([]);
    expect(users.length).toBeGreaterThanOrEqual(3);
  });

  it('flags barrel imports of config constants', () => {
    expect(importsConfigFromBarrel(`import { apiBase } from '@shared/apiClient';`)).toBe(true);
    expect(importsConfigFromBarrel(`import { apiBase } from '../apiClient';`)).toBe(true);
  });

  it('allows apiConfig and non-config barrel imports', () => {
    expect(importsConfigFromBarrel(`import { apiBase } from '@shared/apiConfig';`)).toBe(false);
    expect(importsConfigFromBarrel(`import { apiFetch } from '@shared/apiClient';`)).toBe(false);
  });
});
