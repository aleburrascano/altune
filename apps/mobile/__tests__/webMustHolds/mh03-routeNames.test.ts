import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';

const APP_DIR = path.join(__dirname, '..', '..', 'src', 'app');
const SERVER_OWNED_SEGMENTS = ['v1', 'health', 'test', 'admin', 'operator', 'overseer'];

function isRouteGroup(segment: string): boolean {
  return segment.startsWith('(') && segment.endsWith(')');
}

function firstUrlSegment(routeFile: string): string | undefined {
  const withoutExtension = routeFile.replace(/(\.(web|ios|android|native))?\.(tsx?|jsx?)$/, '');
  return withoutExtension.split(path.sep).find((segment) => !isRouteGroup(segment));
}

function routeFilesUnder(dir: string, relativeTo = dir): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return routeFilesUnder(full, relativeTo);
    return /\.(tsx?|jsx?)$/.test(entry.name) ? [path.relative(relativeTo, full)] : [];
  });
}

function serverOwnedRoutes(appDir: string): string[] {
  return routeFilesUnder(appDir).filter((routeFile) => {
    const segment = firstUrlSegment(routeFile)?.toLowerCase();
    return segment !== undefined && SERVER_OWNED_SEGMENTS.includes(segment);
  });
}

function fixtureAppDir(routeFiles: string[]): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'mh03-'));
  for (const routeFile of routeFiles) {
    fs.mkdirSync(path.dirname(path.join(root, routeFile)), { recursive: true });
    fs.writeFileSync(path.join(root, routeFile), '');
  }
  return root;
}

describe('mh03: no web route shadows a server-owned path prefix', () => {
  it('finds no route under src/app whose first URL segment belongs to go-api or Overseer', () => {
    expect(routeFilesUnder(APP_DIR).length).toBeGreaterThan(0);

    expect(serverOwnedRoutes(APP_DIR)).toEqual([]);
  });

  it('flags a server-owned segment even when it hides inside a route group or differs in case', () => {
    const appDir = fixtureAppDir([
      '(tabs)/Admin/index.tsx',
      'health.tsx',
      'library/test.tsx',
      '(auth)/sign-in.tsx',
      'testimonials.tsx',
    ]);

    expect(serverOwnedRoutes(appDir).sort()).toEqual(
      [path.join('(tabs)', 'Admin', 'index.tsx'), 'health.tsx'].sort(),
    );
  });

  it('flags a server-owned segment behind a platform suffix and skips files that are not URL segments', () => {
    const appDir = fixtureAppDir([
      'admin.web.tsx',
      '(tabs)/v1.tsx',
      'overseer.native.ts',
      'library/health.ios.tsx',
      '_layout.tsx',
      '+not-found.tsx',
      '+html.tsx',
      '(tabs)/_layout.tsx',
      '(tabs)/+not-found.web.tsx',
    ]);

    expect(serverOwnedRoutes(appDir).sort()).toEqual(
      ['admin.web.tsx', path.join('(tabs)', 'v1.tsx'), 'overseer.native.ts'].sort(),
    );
  });
});
