module.exports = {
  id: 'MC-0',
  description: 'No debugger statements (self-test of the ratchet harness)',
  baseline: 0,
  config: [
    {
      rules: {
        'no-debugger': 'error',
      },
    },
  ],
  examples: {
    invalid: ['export function f() {\n  debugger;\n  return 1;\n}\n'],
    valid: ['export function f() {\n  return 1;\n}\n'],
  },
};
