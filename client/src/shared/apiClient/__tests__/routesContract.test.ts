import * as fs from 'fs';
import * as path from 'path';
import * as ts from 'typescript';
import { apiClientSourceFiles } from '@shared/apiClient/__tests__/apiClientSources';
import { wireFixture } from '@testing/wireFixtures';

type RouteEntry = { method: string; path: string };

const NON_PATH_IDENTIFIERS = new Set(['apiBase']);

const PATH_PARAMETER = /\{[^}]*\}/g;
const TRAILING_SLASH = /(.)\/$/;

function normalizePath(p: string): string {
  return p.replace(PATH_PARAMETER, '{}').replace(TRAILING_SLASH, '$1');
}

function isNonPathExpression(expression: ts.Expression): boolean {
  const named = ts.isCallExpression(expression) ? expression.expression : expression;
  return ts.isIdentifier(named) && NON_PATH_IDENTIFIERS.has(named.text);
}

function templateToPathPattern(head: string, spans: readonly ts.TemplateSpan[]): string {
  let result = head;
  for (const span of spans) {
    if (isNonPathExpression(span.expression)) {
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

function deriveClientCalls(): RouteEntry[] {
  const calls: RouteEntry[] = [];
  for (const filePath of apiClientSourceFiles()) {
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

function servedRoutes(): Set<string> {
  const routes = wireFixture('routes');
  if (!Array.isArray(routes)) throw new Error('routes fixture is not an array');
  const acquisition = wireFixture('acquisition_routes') as Record<string, number>;
  return new Set([...routes.map(String), ...Object.keys(acquisition)]);
}

describe('the routes fixture the Go router wrote', () => {
  it('lists the routes the production router serves', () => {
    expect(servedRoutes().size).toBeGreaterThan(0);
  });

  it('serves every path and method the apiClient sends', () => {
    const served = servedRoutes();
    const sent = deriveClientCalls();

    expect(sent.length).toBeGreaterThan(0);
    const unrouted = sent
      .map((call) => `${call.method} ${normalizePath(call.path)}`)
      .filter((entry) => !served.has(entry));

    expect(unrouted).toEqual([]);
  });
});
