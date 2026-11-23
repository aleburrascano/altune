const { importSourceVisitors, stringLiteralValue } = require('../importSources');

const path = require('node:path');

const CLIENT_ROOT = path.resolve(__dirname, '..', '..');
const MODULE_FILE = /^src\/(?:(features|shared)\/([^/]+)|(mobile))(?:\/(.*))?$/;
const DOOR_REST = /^(index(\.tsx?)?)?$/;
const TEST_HELPER_REST = /(^|\/)__tests__\//;
const ALIAS_TARGETS = [
  ['@features/', 'src/features/'],
  ['@shared/', 'src/shared/'],
  ['@/', 'src/'],
];
const DOORS_A_ROLE_MAY_IMPORT = {
  features: new Set(['shared']),
  shared: new Set(['shared']),
  mobile: new Set(['features', 'shared']),
};
const JEST_MODULE_CALLS = ['requireActual', 'requireMock'];

const moduleOf = (file) => {
  const match = MODULE_FILE.exec(file);
  if (!match) return null;
  const [, role, name, mobile, rest = ''] = match;
  return mobile
    ? { role: 'mobile', name: 'mobile', door: '@/mobile', rest }
    : { role, name, door: `${role === 'features' ? '@features' : '@shared'}/${name}`, rest };
};

const targetFile = (importPath, importer) => {
  if (importPath.startsWith('.')) return path.posix.join(path.posix.dirname(importer), importPath);
  const alias = ALIAS_TARGETS.find(([prefix]) => importPath.startsWith(prefix));
  return alias ? path.posix.normalize(alias[1] + importPath.slice(alias[0].length)) : null;
};

const isAllowed = (own, target) => {
  if (target.role === own.role && target.name === own.name) {
    return DOOR_REST.test(target.rest) || TEST_HELPER_REST.test(target.rest);
  }
  return DOOR_REST.test(target.rest) && DOORS_A_ROLE_MAY_IMPORT[own.role].has(target.role);
};

module.exports = {
  meta: {
    type: 'problem',
    messages: {
      behindDoor:
        "A test in {{module}} must not import '{{path}}'. It reaches {{module}} only through its door {{door}}, other modules only through the doors {{module}} may import, and helpers through @testing/. Read client/src/README.md.",
    },
    schema: [],
  },
  create(context) {
    const importer = path.relative(CLIENT_ROOT, context.filename).split(path.sep).join('/');
    const own = moduleOf(importer);
    if (!own) return {};
    const check = (specifier) => {
      const importPath = stringLiteralValue(specifier);
      const file = importPath && targetFile(importPath, importer);
      const target = file && moduleOf(file);
      if (!file?.startsWith('src/') || (target && isAllowed(own, target))) return;
      context.report({
        node: specifier,
        messageId: 'behindDoor',
        data: { module: own.name, path: importPath, door: own.door },
      });
    };
    return importSourceVisitors(check, JEST_MODULE_CALLS);
  },
};
