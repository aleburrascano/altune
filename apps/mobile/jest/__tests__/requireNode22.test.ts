const { assertSupportedNode } = require('../requireNode22');

describe('assertSupportedNode', () => {
  it('throws naming the running version, Node 22 and worktree-deps.sh when below Node 22', () => {
    expect(() => assertSupportedNode('20.20.2')).toThrow(/20\.20\.2/);
    expect(() => assertSupportedNode('20.20.2')).toThrow(/Node 22/);
    expect(() => assertSupportedNode('20.20.2')).toThrow(/worktree-deps\.sh/);
  });

  it('does not throw for Node 22 or newer', () => {
    expect(() => assertSupportedNode('22.23.3')).not.toThrow();
    expect(() => assertSupportedNode('24.0.0')).not.toThrow();
  });
});
