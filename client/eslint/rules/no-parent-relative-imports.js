const PARENT_RELATIVE_PATH = /^\.\.(\/|$)/;
const JEST_MODULE_CALLS = new Set(['mock', 'doMock', 'requireActual', 'requireMock', 'unmock']);

const isRequireCall = (node) => node.callee.type === 'Identifier' && node.callee.name === 'require';

const isJestModuleCall = (node) =>
  node.callee.type === 'MemberExpression' &&
  node.callee.object.type === 'Identifier' &&
  node.callee.object.name === 'jest' &&
  node.callee.property.type === 'Identifier' &&
  JEST_MODULE_CALLS.has(node.callee.property.name);

module.exports = {
  meta: {
    type: 'problem',
    messages: {
      useAlias: "Import '{{path}}' through @features/, @shared/ or @/, never a relative parent path.",
    },
    schema: [],
  },
  create(context) {
    const check = (specifier) => {
      if (!specifier || specifier.type !== 'Literal') return;
      if (typeof specifier.value !== 'string' || !PARENT_RELATIVE_PATH.test(specifier.value)) return;
      context.report({ node: specifier, messageId: 'useAlias', data: { path: specifier.value } });
    };
    return {
      ImportDeclaration: (node) => check(node.source),
      ExportNamedDeclaration: (node) => check(node.source),
      ExportAllDeclaration: (node) => check(node.source),
      ImportExpression: (node) => check(node.source),
      CallExpression(node) {
        if (isRequireCall(node) || isJestModuleCall(node)) check(node.arguments[0]);
      },
    };
  },
};
