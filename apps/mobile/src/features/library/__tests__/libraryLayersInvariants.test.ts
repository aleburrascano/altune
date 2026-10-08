import { readdirSync, readFileSync } from 'fs';
import { join } from 'path';

const DOMAIN_ROOT = join(__dirname, '..', 'domain');

function domainFiles(): string[] {
  return readdirSync(DOMAIN_ROOT).filter((name) => /\.(ts|tsx)$/.test(name));
}

describe('library layers convention doc and domain contents', () => {
  it('keeps all ten pure files in domain/', () => {
    expect(domainFiles().sort()).toEqual([
      'activeView.ts',
      'failureLogFields.ts',
      'featuringDetailRoute.ts',
      'goBackOrToLibrary.ts',
      'groupPaging.ts',
      'library-to-discovery.ts',
      'pagedListControls.ts',
      'refresh.ts',
      'sort.ts',
      'state.ts',
    ]);
  });

  it('has a feature-layers doc that names every port file and every domain file', () => {
    const doc = readFileSync(
      join(
        __dirname,
        '..',
        '..',
        '..',
        '..',
        '..',
        '..',
        'docs',
        'conventions',
        'feature-layers.md',
      ),
      'utf8',
    );
    const missing = [
      'libraryPort.ts',
      'LibraryPortProvider.tsx',
      'httpLibraryPort.ts',
      'memoryLibraryPort.ts',
    ].filter((name) => !doc.includes(name));
    const undocumentedDomain = domainFiles().filter(
      (file) => !doc.includes(file.replace(/\.tsx?$/, '')),
    );
    expect({ missing, undocumentedDomain, domainCount: domainFiles().length }).toEqual({
      missing: [],
      undocumentedDomain: [],
      domainCount: 10,
    });
  });
});
