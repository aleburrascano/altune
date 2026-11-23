const { importSourceVisitors, stringLiteralValue } = require('../importSources');

const PARENT_RELATIVE_PATH = /^\.\.(\/|$)/;
const JEST_MODULE_CALLS = ['mock', 'doMock', 'requireActual', 'requireMock', 'unmock'];

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
    return importSourceVisitors(check, JEST_MODULE_CALLS);
  },
};
