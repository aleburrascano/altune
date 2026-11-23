const isNamedIdentifier = (node, name) => node.type === 'Identifier' && node.name === name;

const isRequireCall = ({ callee }) => isNamedIdentifier(callee, 'require');

const isJestCallTo =
  (methods) =>
  ({ callee }) =>
    callee.type === 'MemberExpression' &&
    isNamedIdentifier(callee.object, 'jest') &&
    callee.property.type === 'Identifier' &&
    methods.has(callee.property.name);

const stringLiteralValue = (node) =>
  node?.type === 'Literal' && typeof node.value === 'string' ? node.value : '';

const importSourceVisitors = (check, jestMethods = []) => {
  const isJestModuleCall = isJestCallTo(new Set(jestMethods));
  return {
    ImportDeclaration: (node) => check(node.source),
    ExportNamedDeclaration: (node) => check(node.source),
    ExportAllDeclaration: (node) => check(node.source),
    ImportExpression: (node) => check(node.source),
    CallExpression(node) {
      if (isRequireCall(node) || isJestModuleCall(node)) check(node.arguments[0]);
    },
  };
};

module.exports = { importSourceVisitors, stringLiteralValue };
