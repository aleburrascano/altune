const FUNCTION_VALUES = new Set(['ArrowFunctionExpression', 'FunctionExpression']);
const TOP_LEVEL_STATEMENTS = new Set(['Program', 'ExportNamedDeclaration']);

const isTopLevelFunctionConst = (declarator) =>
  FUNCTION_VALUES.has(declarator.init?.type) &&
  TOP_LEVEL_STATEMENTS.has(declarator.parent.parent.type);

module.exports = {
  meta: {
    type: 'suggestion',
    messages: {
      useDeclaration: 'Write a top-level function as a declaration: function name() {}.',
    },
    schema: [],
  },
  create(context) {
    return {
      VariableDeclarator(node) {
        if (isTopLevelFunctionConst(node)) context.report({ node, messageId: 'useDeclaration' });
      },
    };
  },
};
