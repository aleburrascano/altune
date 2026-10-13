const ALLOWED_VALUES = new Set([0, 1]);
const UNCONSTRAINED_PROPERTIES = new Set(['flex']);

const isStyleSheetCreate = (node) =>
  node.callee.type === 'MemberExpression' &&
  node.callee.object.name === 'StyleSheet' &&
  node.callee.property.name === 'create';

const isNumber = (node) => typeof node.value === 'number';

const owningPropertyName = (node) => {
  const property = node.parent.type === 'UnaryExpression' ? node.parent.parent : node.parent;
  return property.type === 'Property' ? property.key.name : undefined;
};

const insideStyleSheetCreate = (node) => {
  for (let up = node.parent; up; up = up.parent) {
    if (up.type === 'CallExpression' && isStyleSheetCreate(up)) return true;
  }
  return false;
};

const needsNoName = (node) =>
  ALLOWED_VALUES.has(node.value) || UNCONSTRAINED_PROPERTIES.has(owningPropertyName(node));

const isRawStyleNumber = (node) =>
  isNumber(node) && !needsNoName(node) && insideStyleSheetCreate(node);

module.exports = {
  meta: {
    type: 'suggestion',
    messages: {
      nameIt:
        'Name this number: a spacing or size token from @shared/ui/theme, or a named constant. Only 0, 1 and flex may be raw in StyleSheet.create.',
    },
    schema: [],
  },
  create(context) {
    return {
      Literal(node) {
        if (isRawStyleNumber(node)) context.report({ node, messageId: 'nameIt' });
      },
    };
  },
};
