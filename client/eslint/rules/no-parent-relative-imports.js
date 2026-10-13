const PARENT_RELATIVE_PATH = /^\.\.(\/|$)/;
const JEST_MODULE_CALLS = new Set(['mock', 'doMock', 'requireActual', 'requireMock', 'unmock']);

const isRequireCall = (node) => node.callee.type === 'Identifier' && node.callee.name === 'require';

const isNamedIdentifier = (node, name) => node.type === 'Identifier' && node.name === name;

const isJestModuleCall = ({ callee }) =>
  callee.type === 'MemberExpression' &&
  isNamedIdentifier(callee.object, 'jest') &&
  callee.property.type === 'Identifier' &&
  JEST_MODULE_CALLS.has(callee.property.name);

const stringLiteralValue = (node) =>
  node?.type === 'Literal' && typeof node.value === 'string' ? node.value : '';

module.exports = {
  meta: {
    type: 'problem',
    messages: {
      useAlias:
        "Import '{{path}}' through @features/, @shared/ or @/, never a relative parent path.",
    },
    schema: [],
  },
  create(context) {
    const check = (specifier) => {
      const path = stringLiteralValue(specifier);
      if (PARENT_RELATIVE_PATH.test(path)) {
        context.report({ node: specifier, messageId: 'useAlias', data: { path } });
      }
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
