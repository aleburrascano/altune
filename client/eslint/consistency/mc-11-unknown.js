'use strict';

const ERR = '/^_?(e|err|error|reason|cause|thrown|caught)$|Error$/';
const DECODER = '/^(decode|parse|as|narrow|is)[A-Z]/';

const ALLOWED_POSITIONS = [
  'CatchClause > Identifier > TSTypeAnnotation > TSUnknownKeyword',
  `Identifier[name=${ERR}] > TSTypeAnnotation > TSUnknownKeyword`,
  `TSPropertySignature[key.name=${ERR}] > TSTypeAnnotation > TSUnknownKeyword`,
  ":function[returnType.typeAnnotation.type='TSTypePredicate'] > Identifier > TSTypeAnnotation > TSUnknownKeyword",
  'TSTypePredicate TSUnknownKeyword',
  `:function[id.name=${DECODER}] TSUnknownKeyword`,
  `VariableDeclarator[id.name=${DECODER}] TSUnknownKeyword`,
  'TSTypeParameter TSUnknownKeyword',
  "TSTypeReference[typeName.name='Promise'] > TSTypeParameterInstantiation > TSUnknownKeyword",
  "VariableDeclarator[init.callee.object.name='JSON'][init.callee.property.name='parse'] > Identifier > TSTypeAnnotation > TSUnknownKeyword",
  "TSAsExpression[expression.callee.object.name='JSON'][expression.callee.property.name='parse'] > TSUnknownKeyword",
];

module.exports = {
  id: 'MC-11',
  description:
    'unknown only for caught errors, decoder and type-guard inputs, generic constraints, Promise<unknown> and JSON.parse results',
  config: [
    {
      ignores: ['**/__tests__/**', 'src/shared/apiClient/**', 'src/**/*Wire.ts'],
      rules: {
        'no-restricted-syntax': [
          'error',
          {
            selector: `TSUnknownKeyword:not(${ALLOWED_POSITIONS.join(', ')})`,
            message:
              'Name the type: unknown is only for caught errors, decoder and type-guard inputs, generic constraints and Promise<unknown> (MC-11, docs/conventions/unknown.md).',
          },
        ],
      },
    },
  ],
  examples: {
    invalid: [
      'export const key: readonly unknown[] = [];\n',
      'export type Listener = () => unknown;\n',
      'export type Payload = Record<string, unknown>;\n',
      'export const n = (v: string) => v as unknown as number;\n',
    ],
    valid: [
      'export function f() {\n  try {\n    return 1;\n  } catch (e: unknown) {\n    return e;\n  }\n}\n',
      'export type Failure = { error: unknown };\n',
      "export function isText(value: unknown): value is string {\n  return typeof value === 'string';\n}\n",
      'export function parseCount(raw: unknown): number {\n  return Number(raw);\n}\n',
      'export type Args<A extends unknown[]> = A;\n',
      'export const wait = (p: Promise<unknown>) => p;\n',
      "export const doc: unknown = JSON.parse('{}');\n",
      "export const raw = JSON.parse('{}') as unknown;\n",
    ],
  },
};
