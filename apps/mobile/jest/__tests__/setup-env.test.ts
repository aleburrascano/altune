describe('setup-env', () => {
  it('gives tests the Hermes abort-controller polyfill, which has no throwIfAborted', () => {
    expect(new AbortController().signal.throwIfAborted).toBeUndefined();
  });

  it('gives tests an AbortSignal with no timeout or any, as on Hermes', () => {
    expect(AbortSignal.timeout).toBeUndefined();
    expect(AbortSignal.any).toBeUndefined();
  });
});
