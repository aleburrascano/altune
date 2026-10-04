import { readdirSync, readFileSync } from 'fs';
import { join } from 'path';

const DOMAIN_ROOT = join(__dirname, '..', 'domain');
const IMPORT_STATEMENT = /^\s*(import|export)\s+(type\s+)?([^;]*?\s+from\s+)?['"]([^'"]+)['"]/gms;
const FORBIDDEN_VALUE_SOURCE =
  /^(react|react-native|expo(-.*)?|lucide-react-native|@shared\/ui|.*\/(hooks|ui|adapters)(\/.*)?)$/;

function domainFiles(): string[] {
  return readdirSync(DOMAIN_ROOT).filter((name) => /\.(ts|tsx)$/.test(name));
}

function forbiddenValueImports(source: string): string[] {
  return [...source.matchAll(IMPORT_STATEMENT)]
    .filter((match) => match[2] === undefined)
    .map((match) => match[4] ?? '')
    .filter((specifier) => FORBIDDEN_VALUE_SOURCE.test(specifier));
}

describe('library domain layer invariant — pure rules import no framework, UI or outer layer', () => {
  it('has domain files to scan, so a broken walk cannot pass by finding nothing', () => {
    expect(domainFiles().length).toBeGreaterThanOrEqual(3);
  });

  it.each(domainFiles())('%s has no value import from a framework or outer layer', (file) => {
    const source = readFileSync(join(DOMAIN_ROOT, file), 'utf8');
    expect({ file, forbidden: forbiddenValueImports(source) }).toEqual({ file, forbidden: [] });
  });

  it('flags a value import of react but allows the same import as a type', () => {
    expect(forbiddenValueImports("import { useMemo } from 'react';")).toEqual(['react']);
    expect(forbiddenValueImports("import type { ReactNode } from 'react';")).toEqual([]);
    expect(forbiddenValueImports("import { x } from '../hooks/useThing';")).toEqual([
      '../hooks/useThing',
    ]);
  });
});
