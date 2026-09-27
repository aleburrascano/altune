module.exports = {
  meta: {
    type: 'problem',
    messages: {
      comment: 'Comments are banned; say it in a name, a type or a test.',
    },
    schema: [],
  },
  create(context) {
    return {
      Program() {
        for (const comment of context.sourceCode.getAllComments()) {
          if (comment.type === 'Shebang') {
            continue;
          }
          context.report({ loc: comment.loc, messageId: 'comment' });
        }
      },
    };
  },
};
