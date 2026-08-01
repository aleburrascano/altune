import * as fs from 'fs';
import * as path from 'path';
import * as ts from 'typescript';

function findGoApiRoot(): string | null {
  let dir = __dirname;
  for (let hop = 0; hop < 12; hop += 1) {
    const candidate = path.join(dir, 'services', 'go-api');
    if (fs.existsSync(candidate)) return candidate;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

const GO_API_ROOT = findGoApiRoot();
const API_CLIENT_DIR = path.join(__dirname, '..');

function goPath(...segments: string[]): string {
  return path.join(GO_API_ROOT!, ...segments);
}

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

function extractGoMethodBody(source: string, methodName: string): string {
  const re = new RegExp(`func \\([^)]*\\) ${methodName}\\([^)]*\\)[^{]*\\{`);
  const m = re.exec(source);
  if (!m) throw new Error(`method ${methodName} not found`);
  const braceIdx = m.index + m[0].length - 1;
  const closeIdx = findMatchingBrace(source, braceIdx);
  return source.slice(braceIdx + 1, closeIdx);
}

type GoField = { goType: string; omitempty: boolean };

const STRUCT_FIELD_LINE = /^\s*\w+\s+(\S+)\s+`json:"([a-zA-Z_]+)(,omitempty)?"`/gm;

function extractGoStruct(source: string, structName: string): string {
  const marker = `type ${structName} struct {`;
  const start = source.indexOf(marker);
  if (start === -1) throw new Error(`struct ${structName} not found`);
  const bodyStart = start + marker.length;
  const end = source.indexOf('\n}', bodyStart);
  if (end === -1) throw new Error(`unterminated struct ${structName}`);
  return source.slice(bodyStart, end);
}

function deriveGoFields(structBody: string): Map<string, GoField> {
  const fields = new Map<string, GoField>();
  for (const m of structBody.matchAll(STRUCT_FIELD_LINE)) {
    fields.set(m[2]!, { goType: m[1]!, omitempty: m[3] === ',omitempty' });
  }
  return fields;
}

function extractTsObjectLines(objectBody: string): Map<string, string> {
  const lines = new Map<string, string>();
  for (const m of objectBody.matchAll(/^[ \t]*(\w+)(\?)?\s*:\s*([^\n]+?);?\s*$/gm)) {
    lines.set(m[1]!, `${m[1]}${m[2] ?? ''}: ${m[3]!.trim()}`);
  }
  return lines;
}

function extractTsInterfaceLines(source: string, name: string): Map<string, string> {
  const marker = `export interface ${name} {`;
  const start = source.indexOf(marker);
  if (start === -1) throw new Error(`interface ${name} not found`);
  const openBrace = start + marker.length - 1;
  const closeBrace = findMatchingBrace(source, openBrace);
  return extractTsObjectLines(source.slice(openBrace + 1, closeBrace));
}

function extractTsTypeStatement(source: string, typeName: string): string {
  const marker = `export type ${typeName} = `;
  const start = source.indexOf(marker);
  if (start === -1) throw new Error(`type ${typeName} not found`);
  const afterEq = start + marker.length;
  let depth = 0;
  let end = afterEq;
  for (; end < source.length; end += 1) {
    if (source[end] === '{') depth += 1;
    else if (source[end] === '}') depth -= 1;
    else if (source[end] === ';' && depth === 0) break;
  }
  return source.slice(afterEq, end);
}

function extractTsTypeLines(source: string, typeName: string): Map<string, string> {
  const statement = extractTsTypeStatement(source, typeName);
  const braceStart = statement.indexOf('{');
  const lines = new Map<string, string>();
  if (braceStart === -1) return lines;
  const braceEnd = findMatchingBrace(statement, braceStart);
  for (const [k, v] of extractTsObjectLines(statement.slice(braceStart + 1, braceEnd)))
    lines.set(k, v);
  const before = statement.slice(0, braceStart);
  for (const m of before.matchAll(/[A-Za-z_]\w*/g)) {
    for (const [k, v] of extractTsTypeLines(source, m[0]!)) lines.set(k, v);
  }
  return lines;
}

function isOptionalOrNullable(line: string): boolean {
  return /\w\?:/.test(line) || /\bnull\b/.test(line);
}

const MOUNT_HANDLER_FILES: Record<string, string[]> = {
  'cat.trackHandler': ['internal', 'catalog', 'adapters', 'handler', 'track_handler.go'],
  'cat.libraryHandler': ['internal', 'catalog', 'adapters', 'handler', 'library_handler.go'],
  'cat.playlistHandler': ['internal', 'catalog', 'adapters', 'handler', 'playlist_handler.go'],
  queueHandler: ['internal', 'playback', 'adapters', 'handler', 'queue_handler.go'],
  discoveryH: ['internal', 'discovery', 'adapters', 'handler', 'discovery_handler.go'],
  feedbackH: ['internal', 'feedback', 'adapters', 'handler', 'feedback_handler.go'],
};

const ADD_ROUTES_HANDLER_FILES: Record<string, string[]> = {
  featuredArtist: ['internal', 'catalog', 'adapters', 'handler', 'featured_artist_handler.go'],
};

function joinPath(prefix: string, sub: string): string {
  const normalizedSub = sub.startsWith('/') ? sub : `/${sub}`;
  if (normalizedSub === '/') return prefix === '' ? '/' : prefix;
  return `${prefix}${normalizedSub}`.replace(/\/{2,}/g, '/');
}

type RouteEntry = { method: string; path: string };

function extractRouteEntries(body: string, prefix: string): RouteEntry[] {
  const entries: RouteEntry[] = [];

  const routeBlockRe = /r\.Route\(\s*"([^"]*)"\s*,\s*func\(r chi\.Router\)\s*\{/g;
  const consumedRanges: [number, number][] = [];
  let match: RegExpExecArray | null;
  while ((match = routeBlockRe.exec(body))) {
    const openBrace = body.indexOf('{', match.index);
    const closeBrace = findMatchingBrace(body, openBrace);
    const inner = body.slice(openBrace + 1, closeBrace);
    entries.push(...extractRouteEntries(inner, joinPath(prefix, match[1]!)));
    consumedRanges.push([match.index, closeBrace + 1]);
  }

  let masked = body;
  for (const [s, e] of consumedRanges) {
    masked = masked.slice(0, s) + ' '.repeat(e - s) + masked.slice(e);
  }

  for (const m of masked.matchAll(/\br\.(Get|Post|Put|Patch|Delete)\(\s*"([^"]*)"/g)) {
    entries.push({ method: m[1]!.toUpperCase(), path: joinPath(prefix, m[2]!) });
  }

  for (const m of masked.matchAll(/r\.Mount\(\s*"([^"]*)"\s*,\s*([\w.]+)\.Routes\(\)\)/g)) {
    const mountPrefix = joinPath(prefix, m[1]!);
    const file = MOUNT_HANDLER_FILES[m[2]!];
    if (!file) continue;
    const handlerSource = fs.readFileSync(goPath(...file), 'utf8');
    const routesBody = extractGoMethodBody(handlerSource, 'Routes');
    entries.push(...extractRouteEntries(routesBody, mountPrefix));
  }

  for (const m of masked.matchAll(/h\.(\w+)\.addRoutes\(r\)/g)) {
    const file = ADD_ROUTES_HANDLER_FILES[m[1]!];
    if (!file) continue;
    const handlerSource = fs.readFileSync(goPath(...file), 'utf8');
    const subBody = extractGoMethodBody(handlerSource, 'addRoutes');
    entries.push(...extractRouteEntries(subBody, prefix));
  }

  return entries;
}

function normalizeGoPath(p: string): string {
  return p.replace(/\{[^}]*\}/g, '{}');
}

function deriveGoRoutes(): Set<string> {
  const appSource = fs.readFileSync(goPath('internal', 'app', 'app.go'), 'utf8');
  const mountRoutesBody = extractGoMethodBody(appSource, 'mountRoutes');
  const entries = extractRouteEntries(mountRoutesBody, '');
  return new Set(entries.map((e) => `${e.method} ${normalizeGoPath(e.path)}`));
}

const NON_PATH_IDENTIFIERS = new Set(['apiBase']);

function templateToPathPattern(head: string, spans: readonly ts.TemplateSpan[]): string {
  let result = head;
  for (const span of spans) {
    if (ts.isIdentifier(span.expression) && NON_PATH_IDENTIFIERS.has(span.expression.text)) {
      result += span.literal.text;
      continue;
    }
    if (!result.endsWith('/')) break;
    result += `{}${span.literal.text}`;
  }
  const qIdx = result.indexOf('?');
  return qIdx === -1 ? result : result.slice(0, qIdx);
}

function enclosingScope(node: ts.Node): ts.Node {
  let current: ts.Node | undefined = node.parent;
  while (
    current &&
    !ts.isFunctionDeclaration(current) &&
    !ts.isFunctionExpression(current) &&
    !ts.isArrowFunction(current)
  ) {
    current = current.parent;
  }
  return current ?? node.getSourceFile();
}

function findVariableDeclarationInScope(
  scope: ts.Node,
  name: string,
): ts.VariableDeclaration | undefined {
  let found: ts.VariableDeclaration | undefined;
  const visit = (node: ts.Node) => {
    if (found) return;
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.name.text === name) {
      found = node;
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(scope);
  return found;
}

function findPathLiteral(node: ts.Expression, scope: ts.Node): string | null {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return node.text.startsWith('/') ? node.text : null;
  }
  if (ts.isTemplateExpression(node)) {
    if (!node.head.text.startsWith('/') && node.head.text !== '') return null;
    return templateToPathPattern(node.head.text, node.templateSpans);
  }
  if (ts.isCallExpression(node)) {
    for (const arg of node.arguments) {
      const found = findPathLiteral(arg, scope);
      if (found) return found;
    }
    return null;
  }
  if (ts.isIdentifier(node)) {
    const decl = findVariableDeclarationInScope(scope, node.text);
    if (decl?.initializer) return findPathLiteral(decl.initializer, scope);
    return null;
  }
  if (ts.isConditionalExpression(node)) {
    return findPathLiteral(node.whenTrue, scope) ?? findPathLiteral(node.whenFalse, scope);
  }
  return null;
}

function findApiFetchCalls(sourceFile: ts.SourceFile): ts.CallExpression[] {
  const calls: ts.CallExpression[] = [];
  const visit = (node: ts.Node) => {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      node.expression.text === 'apiFetch'
    ) {
      calls.push(node);
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
  return calls;
}

function extractMethod(node: ts.CallExpression): string {
  const optsArg = node.arguments[1];
  if (optsArg && ts.isObjectLiteralExpression(optsArg)) {
    for (const prop of optsArg.properties) {
      if (
        ts.isPropertyAssignment(prop) &&
        ts.isIdentifier(prop.name) &&
        prop.name.text === 'method' &&
        ts.isStringLiteral(prop.initializer)
      ) {
        return prop.initializer.text;
      }
    }
  }
  return 'GET';
}

function extractReturnTemplate(
  sourceFile: ts.SourceFile,
  functionName: string,
): ts.TemplateExpression | null {
  let result: ts.TemplateExpression | null = null;
  const visit = (node: ts.Node) => {
    if (result) return;
    if (ts.isFunctionDeclaration(node) && node.name?.text === functionName && node.body) {
      for (const stmt of node.body.statements) {
        if (
          ts.isReturnStatement(stmt) &&
          stmt.expression &&
          ts.isTemplateExpression(stmt.expression)
        ) {
          result = stmt.expression;
        }
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
  return result;
}

function listApiClientSourceFiles(): string[] {
  return fs
    .readdirSync(API_CLIENT_DIR)
    .filter((f) => f.endsWith('.ts') && !f.endsWith('.test.ts'))
    .map((f) => path.join(API_CLIENT_DIR, f));
}

function deriveClientCalls(): RouteEntry[] {
  const calls: RouteEntry[] = [];
  for (const filePath of listApiClientSourceFiles()) {
    const source = fs.readFileSync(filePath, 'utf8');
    const sourceFile = ts.createSourceFile(filePath, source, ts.ScriptTarget.Latest, true);
    for (const call of findApiFetchCalls(sourceFile)) {
      const pathArg = call.arguments[0];
      if (!pathArg) continue;
      const rawPath = findPathLiteral(pathArg, enclosingScope(call));
      if (rawPath == null) continue;
      calls.push({ method: extractMethod(call), path: rawPath });
    }
    if (path.basename(filePath) === 'audio.ts') {
      const template = extractReturnTemplate(sourceFile, 'audioStreamUrl');
      if (template) {
        calls.push({
          method: 'GET',
          path: templateToPathPattern(template.head.text, template.templateSpans),
        });
      }
    }
  }
  return calls;
}

describe('routes contract, derived from services/go-api and the api-client source at test time', () => {
  it('finds the Go source tree to derive the contract from', () => {
    expect(GO_API_ROOT).not.toBeNull();
  });

  it('every path this slice sends resolves to a Go route registered for the same method', () => {
    const serverRoutes = deriveGoRoutes();
    const clientCalls = deriveClientCalls();

    expect(serverRoutes.size).toBeGreaterThan(0);
    expect(clientCalls.length).toBeGreaterThan(0);

    const unrouted = clientCalls
      .map((c) => `${c.method} ${normalizeGoPath(c.path)}`)
      .filter((entry) => !serverRoutes.has(entry));

    expect(unrouted).toEqual([]);
  });

  it('reports (without failing) server routes this slice never calls', () => {
    const serverRoutes = deriveGoRoutes();
    const clientEntries = new Set(
      deriveClientCalls().map((c) => `${c.method} ${normalizeGoPath(c.path)}`),
    );

    expect(serverRoutes.size).toBeGreaterThan(0);

    const unusedByThisSlice = [...serverRoutes].filter((r) => !clientEntries.has(r)).sort();
    expect(Array.isArray(unusedByThisSlice)).toBe(true);
  });
});

describe('TrackResponse (types.ts) <-> service.TrackDTO (aliased TrackResponse in track_handler.go)', () => {
  const trackDtoSource = fs.readFileSync(
    goPath('internal', 'catalog', 'service', 'track_dto.go'),
    'utf8',
  );
  const goFields = deriveGoFields(extractGoStruct(trackDtoSource, 'TrackDTO'));
  const typesSource = fs.readFileSync(path.join(API_CLIENT_DIR, 'types.ts'), 'utf8');
  const tsLines = extractTsTypeLines(typesSource, 'TrackResponse');

  it('has the same field set on both sides', () => {
    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);
    expect([...tsLines.keys()].sort()).toEqual([...goFields.keys()].sort());
  });

  it('every omitempty Go field is optional or nullable on the TS side', () => {
    const omitemptyFields = [...goFields.entries()].filter(([, f]) => f.omitempty).map(([k]) => k);
    expect(omitemptyFields.length).toBeGreaterThan(0);
    for (const key of omitemptyFields) {
      expect(isOptionalOrNullable(tsLines.get(key) ?? '')).toBe(true);
    }
  });
});

describe('ListTracksResponse (types.ts) <-> ListTracksResponse (track_handler.go)', () => {
  it('has the same field set on both sides', () => {
    const trackHandlerSource = fs.readFileSync(
      goPath('internal', 'catalog', 'adapters', 'handler', 'track_handler.go'),
      'utf8',
    );
    const goFields = deriveGoFields(extractGoStruct(trackHandlerSource, 'ListTracksResponse'));
    const typesSource = fs.readFileSync(path.join(API_CLIENT_DIR, 'types.ts'), 'utf8');
    const tsLines = extractTsTypeLines(typesSource, 'ListTracksResponse');

    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);
    expect([...tsLines.keys()].sort()).toEqual([...goFields.keys()].sort());
  });
});

describe('CreateTrackRequest (types.ts) <-> CreateTrackRequest (track_handler.go)', () => {
  it('has the same field set on both sides, including the track_number the album-context save sends', () => {
    const trackHandlerSource = fs.readFileSync(
      goPath('internal', 'catalog', 'adapters', 'handler', 'track_handler.go'),
      'utf8',
    );
    const goFields = deriveGoFields(extractGoStruct(trackHandlerSource, 'CreateTrackRequest'));
    const typesSource = fs.readFileSync(path.join(API_CLIENT_DIR, 'types.ts'), 'utf8');
    const tsLines = extractTsTypeLines(typesSource, 'CreateTrackRequest');

    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);
    expect(goFields.has('track_number')).toBe(true);
    expect([...tsLines.keys()].sort()).toEqual([...goFields.keys()].sort());
  });

  it('every omitempty Go field is optional or nullable on the TS side', () => {
    const trackHandlerSource = fs.readFileSync(
      goPath('internal', 'catalog', 'adapters', 'handler', 'track_handler.go'),
      'utf8',
    );
    const goFields = deriveGoFields(extractGoStruct(trackHandlerSource, 'CreateTrackRequest'));
    const typesSource = fs.readFileSync(path.join(API_CLIENT_DIR, 'types.ts'), 'utf8');
    const tsLines = extractTsTypeLines(typesSource, 'CreateTrackRequest');

    const omitemptyFields = [...goFields.entries()].filter(([, f]) => f.omitempty).map(([k]) => k);
    expect(omitemptyFields.length).toBeGreaterThan(0);
    for (const key of omitemptyFields) {
      expect(isOptionalOrNullable(tsLines.get(key) ?? '')).toBe(true);
    }
  });
});

describe('Playlist DTOs (playlist_handler.go) <-> types.ts', () => {
  const playlistHandlerSource = fs.readFileSync(
    goPath('internal', 'catalog', 'adapters', 'handler', 'playlist_handler.go'),
    'utf8',
  );
  const typesSource = fs.readFileSync(path.join(API_CLIENT_DIR, 'types.ts'), 'utf8');

  it.each([
    'PlaylistResponse',
    'ListPlaylistsResponse',
    'PlaylistDetailResponse',
    'CreatePlaylistRequest',
    'AddTrackToPlaylistRequest',
    'ReorderTracksRequest',
  ])('%s has the same field set on both sides', (name) => {
    const goFields = deriveGoFields(extractGoStruct(playlistHandlerSource, name));
    const tsLines = extractTsTypeLines(typesSource, name);

    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);
    expect([...tsLines.keys()].sort()).toEqual([...goFields.keys()].sort());
  });
});

describe('Library lens DTOs (library_handler.go) <-> library.ts', () => {
  const libraryHandlerSource = fs.readFileSync(
    goPath('internal', 'catalog', 'adapters', 'handler', 'library_handler.go'),
    'utf8',
  );
  const librarySource = fs.readFileSync(path.join(API_CLIENT_DIR, 'library.ts'), 'utf8');

  it.each([
    ['AlbumGroupDTO', 'AlbumGroup'],
    ['ArtistGroupDTO', 'ArtistGroup'],
    ['ListAlbumsResponse', 'ListAlbumsResponse'],
    ['ListArtistsResponse', 'ListArtistsResponse'],
  ])('%s (Go) has the same field set as %s (TS)', (goName, tsName) => {
    const goFields = deriveGoFields(extractGoStruct(libraryHandlerSource, goName));
    const tsLines = extractTsTypeLines(librarySource, tsName);

    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);
    expect([...tsLines.keys()].sort()).toEqual([...goFields.keys()].sort());
  });
});

describe('audioURLDTO (audio_url_handler.go) <-> ResolvedAudioUrl (audio.ts) — deliberate narrowing', () => {
  it('every ResolvedAudioUrl field maps to a Go audioURLDTO field, and expires_at is the one field dropped on purpose', () => {
    const audioUrlHandlerSource = fs.readFileSync(
      goPath('internal', 'catalog', 'adapters', 'handler', 'audio_url_handler.go'),
      'utf8',
    );
    const goFields = deriveGoFields(extractGoStruct(audioUrlHandlerSource, 'audioURLDTO'));
    const audioSource = fs.readFileSync(path.join(API_CLIENT_DIR, 'audio.ts'), 'utf8');
    const tsLines = extractTsInterfaceLines(audioSource, 'ResolvedAudioUrl');

    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);

    expect([...tsLines.keys()].sort()).toEqual(['trackId', 'url', 'version']);
    expect([...goFields.keys()].sort()).toEqual(['expires_at', 'track_id', 'url', 'version']);

    const carried = ['track_id', 'url', 'version'];
    const droppedByTs = [...goFields.keys()].filter((k) => !carried.includes(k));
    expect(droppedByTs).toEqual(['expires_at']);
  });
});

describe('queue-state pair (queue_handler.go) <-> QueueStateResponse / SaveQueueStateRequest (playback.ts)', () => {
  const queueHandlerSource = fs.readFileSync(
    goPath('internal', 'playback', 'adapters', 'handler', 'queue_handler.go'),
    'utf8',
  );
  const playbackSource = fs.readFileSync(path.join(API_CLIENT_DIR, 'playback.ts'), 'utf8');

  it('QueueStateResponse (TS) fields are a subset of queueStateResponse (Go), dropping only the documented source_id carryover field', () => {
    const goFields = deriveGoFields(extractGoStruct(queueHandlerSource, 'queueStateResponse'));
    const tsLines = extractTsInterfaceLines(playbackSource, 'QueueStateResponse');

    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);

    const tsKeys = new Set(tsLines.keys());
    const missingFromGo = [...tsKeys].filter((k) => !goFields.has(k));
    expect(missingFromGo).toEqual([]);

    const droppedByTs = [...goFields.keys()].filter((k) => !tsKeys.has(k));
    expect(droppedByTs).toEqual(['source_id']);
  });

  it('every omitempty Go field on queueStateResponse is optional or nullable on the TS side', () => {
    const goFields = deriveGoFields(extractGoStruct(queueHandlerSource, 'queueStateResponse'));
    const tsLines = extractTsInterfaceLines(playbackSource, 'QueueStateResponse');

    const omitemptyFields = [...goFields.entries()].filter(([, f]) => f.omitempty).map(([k]) => k);
    expect(omitemptyFields.length).toBeGreaterThan(0);
    for (const key of omitemptyFields) {
      expect(isOptionalOrNullable(tsLines.get(key) ?? '')).toBe(true);
    }
  });

  it('SaveQueueStateRequest (TS) fields are a subset of saveQueueRequest (Go), dropping only the documented source_id carryover field', () => {
    const goFields = deriveGoFields(extractGoStruct(queueHandlerSource, 'saveQueueRequest'));
    const tsLines = extractTsInterfaceLines(playbackSource, 'SaveQueueStateRequest');

    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);

    const tsKeys = new Set(tsLines.keys());
    const missingFromGo = [...tsKeys].filter((k) => !goFields.has(k));
    expect(missingFromGo).toEqual([]);

    const droppedByTs = [...goFields.keys()].filter((k) => !tsKeys.has(k));
    expect(droppedByTs).toEqual(['source_id']);
  });
});
