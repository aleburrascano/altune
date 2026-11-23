const { importSourceVisitors, stringLiteralValue } = require('../importSources');

const TEST_CODE_PATH = /(^|\/)__tests__(\/|$)|^@testing\//;

module.exports = {
  meta: {
    type: 'problem',
    messages: {
      noTestCode:
        "App code must not import '{{path}}': Metro never bundles __tests__ folders, and @testing/ holds test fakes. Import the fake in the test instead.",
    },
    schema: [],
  },
  create(context) {
    const check = (specifier) => {
      const path = stringLiteralValue(specifier);
      if (TEST_CODE_PATH.test(path)) {
        context.report({ node: specifier, messageId: 'noTestCode', data: { path } });
      }
    };
    return importSourceVisitors(check);
  },
};
