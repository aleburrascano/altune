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

// Matches both a method (`func (h *T) Name(...)`) and a plain package function
// (`func Name(...)`), so route helpers like app.mountFeedback resolve too.
function extractGoMethodBody(source: string, methodName: string): string {
  const re = new RegExp(`func (?:\\([^)]*\\) )?${methodName}\\([^)]*\\)[^{]*\\{`);
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

const EMBEDDED_STRUCT_LINE = /^\s*([A-Z]\w*)\s*$/gm;

function deriveGoFields(source: string, structBody: string): Map<string, GoField> {
  const fields = new Map<string, GoField>();
  for (const m of structBody.matchAll(EMBEDDED_STRUCT_LINE)) {
    for (const [k, v] of deriveGoFields(source, extractGoStruct(source, m[1]!))) {
      fields.set(k, v);
    }
  }
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
  // Follow referenced local types, so a type composed purely from others
  // (`A & (B | C)`, e.g. TrackResponse = TrackFields & TrackAcquisition)
  // still yields the union of their fields.
  if (braceStart !== -1) {
    const braceEnd = findMatchingBrace(statement, braceStart);
    for (const [k, v] of extractTsObjectLines(statement.slice(braceStart + 1, braceEnd)))
      lines.set(k, v);
  }
  const before = braceStart === -1 ? statement : statement.slice(0, braceStart);
  for (const m of before.matchAll(/[A-Za-z_]\w*/g)) {
    if (braceStart === -1 && !source.includes(`export type ${m[0]} = `)) continue;
    for (const [k, v] of extractTsTypeLines(source, m[0])) lines.set(k, v);
  }
  return lines;
}

function isOptionalOrNullable(line: string): boolean {
  return /\w\?:/.test(line) || /\bnull\b/.test(line);
}

// List endpoints no longer have per-endpoint named response structs; they all
// serialize the generic httputil.List[T] envelope ({items, total}). We read that
// one struct from list.go and assert each mobile list type matches its wire shape.
function listEnvelopeGoFields(): Map<string, GoField> {
  const listSource = fs.readFileSync(
    goPath('internal', 'shared', 'httputil', 'list.go'),
    'utf8',
  );
  return deriveGoFields(listSource, extractGoStruct(listSource, 'List[T any]'));
}

function expectListEnvelope(tsLines: Map<string, string>, itemType: string): void {
  const goFields = listEnvelopeGoFields();

  expect(goFields.size).toBeGreaterThan(0);
  expect(tsLines.size).toBeGreaterThan(0);

  // same envelope field set as httputil.List[T]: {items, total}
  expect([...tsLines.keys()].sort()).toEqual([...goFields.keys()].sort());
  // items carries the endpoint's item DTO (validated separately), as an array
  expect(tsLines.get('items')).toContain(`${itemType}[]`);
}

const MOUNT_HANDLER_FILES: Record<string, string[]> = {
  'cat.trackHandler': ['internal', 'catalog', 'adapters', 'handler', 'track_handler.go'],
  'cat.libraryHandler': ['internal', 'catalog', 'adapters', 'handler', 'library_handler.go'],
  'cat.playlistHandler': ['internal', 'catalog', 'adapters', 'handler', 'playlist_handler.go'],
  queueHandler: ['internal', 'playback', 'adapters', 'handler', 'queue_handler.go'],
  discoveryH: ['internal', 'discovery', 'adapters', 'handler', 'discovery_handler.go'],
  feedbackH: ['internal', 'feedback', 'adapters', 'handler', 'feedback_handler.go'],
};

// Handlers that register onto a shared router via `<recv>.Routes(r)` instead of
// being mounted via `r.Mount(prefix, X.Routes())`. Keyed by the receiver's final
// field segment (cat.streamHandler -> streamHandler, h.featuredArtist ->
// featuredArtist). Their paths interleave with an existing prefix, so we recurse
// into each handler's Routes method body with the current prefix.
const SHARED_ROUTER_HANDLER_FILES: Record<string, string[]> = {
  streamHandler: ['internal', 'catalog', 'adapters', 'handler', 'stream_handler.go'],
  audioURLHandler: ['internal', 'catalog', 'adapters', 'handler', 'audio_url_handler.go'],
  featuredArtist: ['internal', 'catalog', 'adapters', 'handler', 'featured_artist_handler.go'],
};

// Some handlers are mounted through a small wiring helper `helper(r, handler)`
// instead of a direct `r.Mount(...)` in mountRoutes. Feedback's mountFeedback
// (app/feedback_wiring.go) picks between the live routes and coded-503
// DisabledRoutes behind FEEDBACK_ENABLED, so its real r.Mount lives in the
// helper body, not in mountRoutes. Keyed by helper name; the helper's own body
// carries the mount prefix + `<handler>.Routes()` we recurse into.
const MOUNT_HELPER_FILES: Record<string, string[]> = {
  mountFeedback: ['internal', 'app', 'feedback_wiring.go'],
};

function joinPath(prefix: string, sub: string): string {
  const normalizedSub = sub.startsWith('/') ? sub : `/${sub}`;
  if (normalizedSub === '/') return prefix === '' ? '/' : prefix;
  return `${prefix}${normalizedSub}`.replace(/\/{2,}/g, '/');
}

type RouteEntry = { method: string; path: string };

function extractRouteEntries(body: string, prefix: string, source: string): RouteEntry[] {
  const entries: RouteEntry[] = [];

  const routeBlockRe = /r\.Route\(\s*"([^"]*)"\s*,\s*func\(r chi\.Router\)\s*\{/g;
  const consumedRanges: [number, number][] = [];
  let match: RegExpExecArray | null;
  while ((match = routeBlockRe.exec(body))) {
    const openBrace = body.indexOf('{', match.index);
    const closeBrace = findMatchingBrace(body, openBrace);
    const inner = body.slice(openBrace + 1, closeBrace);
    entries.push(...extractRouteEntries(inner, joinPath(prefix, match[1]!), source));
    consumedRanges.push([match.index, closeBrace + 1]);
  }

  let masked = body;
  for (const [s, e] of consumedRanges) {
    masked = masked.slice(0, s) + ' '.repeat(e - s) + masked.slice(e);
  }

  // A route may be registered through an inline middleware chain, e.g.
  // `r.With(h.limiter.middleware).Get("/queue-state", ...)`. `.With(...)` only
  // wraps the handler; it registers the same method+path on the same router,
  // so zero or more chained With(...) calls are accepted before the verb. Their
  // arguments may nest one level of parentheses (`r.With(mw(cfg))`).
  const withChain = String.raw`(?:\s*\.With\((?:[^()]|\([^()]*\))*\))*`;
  const verbRe = new RegExp(String.raw`\br${withChain}\s*\.(Get|Post|Put|Patch|Delete)\(\s*"([^"]*)"`, 'g');
  for (const m of masked.matchAll(verbRe)) {
    entries.push({ method: m[1]!.toUpperCase(), path: joinPath(prefix, m[2]!) });
  }

  for (const m of masked.matchAll(/r\.Mount\(\s*"([^"]*)"\s*,\s*([\w.]+)\.Routes\(\)\)/g)) {
    const mountPrefix = joinPath(prefix, m[1]!);
    const file = MOUNT_HANDLER_FILES[m[2]!];
    if (!file) continue;
    const handlerSource = fs.readFileSync(goPath(...file), 'utf8');
    const routesBody = extractGoMethodBody(handlerSource, 'Routes');
    entries.push(...extractRouteEntries(routesBody, mountPrefix, handlerSource));
  }

  for (const m of masked.matchAll(/([\w.]+)\.Routes\(\s*r\s*\)/g)) {
    const segments = m[1]!.split('.');
    const field = segments[segments.length - 1]!;
    const file = SHARED_ROUTER_HANDLER_FILES[field];
    if (!file) continue;
    const handlerSource = fs.readFileSync(goPath(...file), 'utf8');
    const subBody = extractGoMethodBody(handlerSource, 'Routes');
    entries.push(...extractRouteEntries(subBody, prefix, handlerSource));
  }

  // A middleware group registers routes on the same path prefix via a method
  // value: `r.Group(h.contentRoutes)`. The grouped routes live in that method's
  // body in the same source (inline `r.Group(func(...) {...})` bodies are read
  // in place by the verb scan above), so read the method and recurse under the
  // same prefix.
  for (const m of masked.matchAll(/r\.Group\(\s*[\w.]+\.(\w+)\s*\)/g)) {
    const groupBody = extractGoMethodBody(source, m[1]!);
    entries.push(...extractRouteEntries(groupBody, prefix, source));
  }

  // Mount-helper calls: `helper(r, handlerVar)`. The helper's body holds the
  // real `r.Mount("<prefix>", handlerVar.Routes())`, so we read the helper,
  // take that live mount prefix (ignoring the coded-503 DisabledRoutes branch),
  // and recurse into the handler's own Routes under it.
  for (const m of masked.matchAll(/\b(\w+)\(\s*r\s*,\s*([\w.]+)\s*\)/g)) {
    const helperFile = MOUNT_HELPER_FILES[m[1]!];
    const handlerFile = MOUNT_HANDLER_FILES[m[2]!];
    if (!helperFile || !handlerFile) continue;
    const helperSource = fs.readFileSync(goPath(...helperFile), 'utf8');
    const helperBody = extractGoMethodBody(helperSource, m[1]!);
    const liveMount = /r\.Mount\(\s*"([^"]*)"\s*,\s*\w+\.Routes\(\)\)/.exec(helperBody);
    if (!liveMount) continue;
    const handlerSource = fs.readFileSync(goPath(...handlerFile), 'utf8');
    const routesBody = extractGoMethodBody(handlerSource, 'Routes');
    entries.push(...extractRouteEntries(routesBody, joinPath(prefix, liveMount[1]!), handlerSource));
  }

  return entries;
}

function normalizeGoPath(p: string): string {
  return p.replace(/\{[^}]*\}/g, '{}');
}

function deriveGoRoutes(): Set<string> {
  const routesSource = fs.readFileSync(goPath('internal', 'app', 'routes.go'), 'utf8');
  const mountRoutesBody = extractGoMethodBody(routesSource, 'mountRoutes');
  const entries = extractRouteEntries(mountRoutesBody, '', routesSource);
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

  it('recognises routes registered through an inline r.With(...) middleware chain, and only for their own verb', () => {
    const entries = extractRouteEntries(
      [
        'r.With(h.limiter.middleware).Put("/queue-state", h.handleSave)',
        'r.With(mw(cfg), other).With(third).Get("/search", h.handleSearch)',
        'r.Delete("/queue-state", h.handleForget)',
        'x.With(h.limiter.middleware).Post("/not-a-router", h.handle)',
      ].join('\n'),
      '/v1',
      '',
    );

    expect(entries).toEqual([
      { method: 'PUT', path: '/v1/queue-state' },
      { method: 'GET', path: '/v1/search' },
      { method: 'DELETE', path: '/v1/queue-state' },
    ]);
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
  const goFields = deriveGoFields(trackDtoSource, extractGoStruct(trackDtoSource, 'TrackDTO'));
  const typesSource = fs.readFileSync(path.join(API_CLIENT_DIR, 'types.ts'), 'utf8');
  const tsLines = extractTsTypeLines(typesSource, 'TrackResponse');

  // Go fields tagged json:"-" exist on TrackDTO but never reach the wire. The
  // mobile type may still carry them as client-only cache fields (audio_ref is
  // the storage key hidden by #1046; the client learns it only from the
  // track_acquisition_completed SSE event), so they are modelled explicitly.
  const hiddenGoFields = [...extractGoStruct(trackDtoSource, 'TrackDTO').matchAll(
    /^\s*(\w+)\s+\S+\s+`json:"-"`/gm,
  )].map((m) => m[1]!.replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase());
  const CLIENT_ONLY_FIELDS = ['audio_ref'];

  it('has the same wire field set on both sides, apart from the documented client-only fields', () => {
    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);
    const wireTsKeys = [...tsLines.keys()].filter((k) => !CLIENT_ONLY_FIELDS.includes(k));
    expect(wireTsKeys.sort()).toEqual([...goFields.keys()].sort());
  });

  it('every client-only TS field is one Go deliberately hides from the wire, and is optional on the TS side', () => {
    expect(hiddenGoFields).toEqual(CLIENT_ONLY_FIELDS);
    for (const key of CLIENT_ONLY_FIELDS) {
      expect(goFields.has(key)).toBe(false);
      expect(tsLines.get(key)).toMatch(new RegExp(`^${key}\\?:`));
    }
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
    const goFields = deriveGoFields(
      trackHandlerSource,
      extractGoStruct(trackHandlerSource, 'ListTracksResponse'),
    );
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
    const goFields = deriveGoFields(
      trackHandlerSource,
      extractGoStruct(trackHandlerSource, 'CreateTrackRequest'),
    );
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
    const goFields = deriveGoFields(
      trackHandlerSource,
      extractGoStruct(trackHandlerSource, 'CreateTrackRequest'),
    );
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
    'PlaylistDetailResponse',
    'CreatePlaylistRequest',
    'AddTracksToPlaylistRequest',
    'AddTracksToPlaylistResponse',
    'RemoveTracksFromPlaylistRequest',
    'RemoveTracksFromPlaylistResponse',
  ])('%s has the same field set on both sides', (name) => {
    const goFields = deriveGoFields(
      playlistHandlerSource,
      extractGoStruct(playlistHandlerSource, name),
    );
    const tsLines = extractTsTypeLines(typesSource, name);

    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);
    expect([...tsLines.keys()].sort()).toEqual([...goFields.keys()].sort());
  });

  // handleList now emits httputil.NewList(...) -> httputil.List[PlaylistResponse],
  // so the mobile ListPlaylistsResponse must match the {items, total} envelope.
  it('ListPlaylistsResponse matches the httputil.List envelope of PlaylistResponse', () => {
    expectListEnvelope(extractTsTypeLines(typesSource, 'ListPlaylistsResponse'), 'PlaylistResponse');
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
  ])('%s (Go) has the same field set as %s (TS)', (goName, tsName) => {
    const goFields = deriveGoFields(
      libraryHandlerSource,
      extractGoStruct(libraryHandlerSource, goName),
    );
    const tsLines = extractTsTypeLines(librarySource, tsName);

    expect(goFields.size).toBeGreaterThan(0);
    expect(tsLines.size).toBeGreaterThan(0);
    expect([...tsLines.keys()].sort()).toEqual([...goFields.keys()].sort());
  });

  // handleAlbums/handleArtists now emit httputil.NewList(...) -> httputil.List[T],
  // so the mobile list types must match the {items, total} envelope of their DTO.
  it.each([
    ['ListAlbumsResponse', 'AlbumGroup'],
    ['ListArtistsResponse', 'ArtistGroup'],
  ])('%s matches the httputil.List envelope of %s', (tsName, itemType) => {
    expectListEnvelope(extractTsTypeLines(librarySource, tsName), itemType);
  });
});

describe('audioURLDTO (audio_url_handler.go) <-> ResolvedAudioUrl (audio.ts) — deliberate narrowing', () => {
  it('every ResolvedAudioUrl field maps to a Go audioURLDTO field, and expires_at is the one field dropped on purpose', () => {
    const audioUrlHandlerSource = fs.readFileSync(
      goPath('internal', 'catalog', 'adapters', 'handler', 'audio_url_handler.go'),
      'utf8',
    );
    const goFields = deriveGoFields(
      audioUrlHandlerSource,
      extractGoStruct(audioUrlHandlerSource, 'audioURLDTO'),
    );
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
    const goFields = deriveGoFields(
      queueHandlerSource,
      extractGoStruct(queueHandlerSource, 'queueStateResponse'),
    );
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
    const goFields = deriveGoFields(
      queueHandlerSource,
      extractGoStruct(queueHandlerSource, 'queueStateResponse'),
    );
    const tsLines = extractTsInterfaceLines(playbackSource, 'QueueStateResponse');

    const omitemptyFields = [...goFields.entries()].filter(([, f]) => f.omitempty).map(([k]) => k);
    expect(omitemptyFields.length).toBeGreaterThan(0);
    for (const key of omitemptyFields) {
      expect(isOptionalOrNullable(tsLines.get(key) ?? '')).toBe(true);
    }
  });

  it('SaveQueueStateRequest (TS) fields are a subset of saveQueueRequest (Go), dropping only the documented source_id carryover field', () => {
    const goFields = deriveGoFields(
      queueHandlerSource,
      extractGoStruct(queueHandlerSource, 'saveQueueRequest'),
    );
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
