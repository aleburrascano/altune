const FOLDER_WITH_ITS_OWN_ALIAS = /^@\/(features|shared)\//;

module.exports = {
  meta: {
    type: 'problem',
    fixable: 'code',
    messages: {
      useOwnAlias: "Import '{{path}}' through @{{folder}}/, the one alias for src/{{folder}}.",
    },
    schema: [],
  },
  create(context) {
    return {
      Literal(node) {
        if (typeof node.value !== 'string') return;
        const match = FOLDER_WITH_ITS_OWN_ALIAS.exec(node.value);
        if (!match) return;
        const folder = match[1];
        const quote = node.raw[0];
        context.report({
          node,
          messageId: 'useOwnAlias',
          data: { path: node.value, folder },
          fix: (fixer) =>
            fixer.replaceText(node, `${quote}${node.value.replace('@/', '@')}${quote}`),
        });
      },
    };
  },
};
